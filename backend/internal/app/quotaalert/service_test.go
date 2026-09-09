package quotaalert_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	entsql "entgo.io/ent/dialect/sql"
	_ "github.com/mattn/go-sqlite3"

	"github.com/DouDOU-start/airgate-core/ent"
	"github.com/DouDOU-start/airgate-core/ent/enttest"
	entmember "github.com/DouDOU-start/airgate-core/ent/member"
	"github.com/DouDOU-start/airgate-core/ent/migrate"
	entnotification "github.com/DouDOU-start/airgate-core/ent/usernotification"
	appnotification "github.com/DouDOU-start/airgate-core/internal/app/notification"
	"github.com/DouDOU-start/airgate-core/internal/app/quotaalert"
	"github.com/DouDOU-start/airgate-core/internal/billing"
	"github.com/DouDOU-start/airgate-core/internal/infra/store"
)

// 内存 SQLite 单连接打开（cache=shared 多连接会偶发 SQLITE_LOCKED），模板见 scheduler/events_test.go。
func openTestDB(t *testing.T, name string) *ent.Client {
	t.Helper()
	drv, err := entsql.Open("sqlite3", "file:"+name+"?mode=memory&cache=shared&_fk=1")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	drv.DB().SetMaxOpenConns(1)
	db := enttest.NewClient(t,
		enttest.WithOptions(ent.Driver(drv)),
		enttest.WithMigrateOptions(migrate.WithGlobalUniqueID(false)))
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// mailbox 收集邮件（并发安全）。
type mailbox struct {
	mu   sync.Mutex
	sent []string // "to|subject"
}

func (m *mailbox) send(to, subject, _ string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sent = append(m.sent, to+"|"+subject)
}

func (m *mailbox) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.sent)
}

type fixture struct {
	db      *ent.Client
	svc     *quotaalert.Service
	mail    *mailbox
	owner   *ent.User
	account *ent.User
	now     time.Time
}

func newFixture(t *testing.T, name string) *fixture {
	t.Helper()
	db := openTestDB(t, name)
	ctx := context.Background()
	owner, err := db.User.Create().SetEmail("owner-" + name + "@example.com").SetPasswordHash("x").Save(ctx)
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	account, err := db.User.Create().SetEmail("member-" + name + "@example.com").SetPasswordHash("x").Save(ctx)
	if err != nil {
		t.Fatalf("create member account: %v", err)
	}
	mail := &mailbox{}
	notifier := appnotification.NewService(store.NewNotificationStore(db))
	svc := quotaalert.NewService(store.NewQuotaAlertStore(db), notifier)
	svc.SetEmailSender(mail.send)
	return &fixture{db: db, svc: svc, mail: mail, owner: owner, account: account, now: time.Now()}
}

func (f *fixture) createMember(t *testing.T, quota, used float64, withAccount bool) *ent.Member {
	t.Helper()
	builder := f.db.Member.Create().SetName("张三").SetOwnerID(f.owner.ID).
		SetQuotaUsd(quota).SetUsedQuota(used).
		SetPeriodAnchor(f.now.Add(-time.Hour)).SetPeriodStart(f.now.Add(-time.Hour))
	if withAccount {
		builder = builder.SetAccount(f.account)
	}
	m, err := builder.Save(context.Background())
	if err != nil {
		t.Fatalf("create member: %v", err)
	}
	return m
}

func (f *fixture) notifications(t *testing.T, userID int) []*ent.UserNotification {
	t.Helper()
	rows, err := f.db.UserNotification.Query().
		Where(entnotification.UserIDEQ(userID)).
		Order(ent.Asc(entnotification.FieldID)).
		All(context.Background())
	if err != nil {
		t.Fatalf("query notifications: %v", err)
	}
	return rows
}

func TestMemberThresholds(t *testing.T) {
	cases := []struct {
		name        string
		quota, used float64
		withAccount bool
		wantLevel   string // "" = 不预警
		wantTitle   string // 企业主标题片段
		wantSelf    string // 成员自己标题片段
	}{
		{"85% → warning，企业主与成员账号各一条", 10, 8.5, true, "warning", "成员 张三 本期额度已用 85%", "您的本期额度已用 85%"},
		{"100% → danger 双方", 10, 10, true, "danger", "成员 张三 本期额度已用尽", "您的本期额度已用尽"},
		{"超额 120% 仍 danger", 10, 12, true, "danger", "成员 张三 本期额度已用尽", "您的本期额度已用尽"},
		{"79% 不预警", 10, 7.9, true, "", "", ""},
		{"额度 0（不限）不预警", 0, 999, true, "", "", ""},
		{"老模型成员无登录账号：只投企业主", 10, 9, false, "warning", "成员 张三 本期额度已用 90%", ""},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t, "quota_member_"+string(rune('a'+i)))
			m := f.createMember(t, tc.quota, tc.used, tc.withAccount)
			f.svc.Process(context.Background(), billing.ChargeEvent{MemberIDs: []int{m.ID}})

			ownerRows := f.notifications(t, f.owner.ID)
			selfRows := f.notifications(t, f.account.ID)
			if tc.wantLevel == "" {
				if len(ownerRows)+len(selfRows) != 0 || f.mail.count() != 0 {
					t.Fatalf("expected no alert, got owner=%d self=%d mail=%d", len(ownerRows), len(selfRows), f.mail.count())
				}
				return
			}
			if len(ownerRows) != 1 {
				t.Fatalf("owner notifications = %d, want 1", len(ownerRows))
			}
			row := ownerRows[0]
			if string(row.Level) != tc.wantLevel || row.Kind != appnotification.KindQuotaAlert || row.Link != "/team" {
				t.Fatalf("owner row = level %s kind %s link %s", row.Level, row.Kind, row.Link)
			}
			if !strings.Contains(row.Title, tc.wantTitle) {
				t.Fatalf("owner title = %q, want contains %q", row.Title, tc.wantTitle)
			}
			if !strings.Contains(row.Content, "请在团队管理中调整额度") || !strings.Contains(row.Content, "周期截止") {
				t.Fatalf("owner content = %q", row.Content)
			}
			wantMails := 1
			if tc.withAccount {
				wantMails = 2
				if len(selfRows) != 1 {
					t.Fatalf("member-self notifications = %d, want 1", len(selfRows))
				}
				self := selfRows[0]
				if string(self.Level) != tc.wantLevel || self.Link != "/usage" || !strings.Contains(self.Title, tc.wantSelf) ||
					!strings.Contains(self.Content, "请联系企业管理员") {
					t.Fatalf("self row = level %s link %s title %q content %q", self.Level, self.Link, self.Title, self.Content)
				}
			} else if len(selfRows) != 0 {
				t.Fatalf("member without account must not receive, got %d", len(selfRows))
			}
			if f.mail.count() != wantMails {
				t.Fatalf("mails = %d, want %d", f.mail.count(), wantMails)
			}

			// 同一期再次触发：dedupe 拦下，站内信与邮件都不重复。
			f.svc.Process(context.Background(), billing.ChargeEvent{MemberIDs: []int{m.ID}})
			if n := len(f.notifications(t, f.owner.ID)) + len(f.notifications(t, f.account.ID)); n != len(ownerRows)+len(selfRows) {
				t.Fatalf("duplicate alert: rows = %d", n)
			}
			if f.mail.count() != wantMails {
				t.Fatalf("duplicate mail: %d", f.mail.count())
			}
		})
	}
}

// 85% 先发 warning，随后用到 100% 再发 danger（不同级别是不同事件）。
func TestMemberEscalatesFromWarningToDanger(t *testing.T) {
	f := newFixture(t, "quota_escalate")
	ctx := context.Background()
	m := f.createMember(t, 10, 8.5, true)
	f.svc.Process(ctx, billing.ChargeEvent{MemberIDs: []int{m.ID}})
	if _, err := f.db.Member.UpdateOneID(m.ID).SetUsedQuota(10).Save(ctx); err != nil {
		t.Fatalf("update member: %v", err)
	}
	f.svc.Process(ctx, billing.ChargeEvent{MemberIDs: []int{m.ID}})
	rows := f.notifications(t, f.owner.ID)
	if len(rows) != 2 || rows[0].Level != entnotification.LevelWarning || rows[1].Level != entnotification.LevelDanger {
		t.Fatalf("owner rows = %d (%v)", len(rows), rows)
	}
	if f.mail.count() != 4 {
		t.Fatalf("mails = %d, want 4", f.mail.count())
	}
}

// period_start 变化（换期 / 手动重置）后同级别重新预警：去重钥匙含本期起点。
func TestMemberRealertsAfterPeriodStartChanges(t *testing.T) {
	f := newFixture(t, "quota_period")
	ctx := context.Background()
	m := f.createMember(t, 10, 8.5, false)
	f.svc.Process(ctx, billing.ChargeEvent{MemberIDs: []int{m.ID}})
	if n := len(f.notifications(t, f.owner.ID)); n != 1 {
		t.Fatalf("first alert rows = %d, want 1", n)
	}
	// 手动重置本期：period_start 前移到现在、快照 base；此时本期已用 0 → 不预警。
	if _, err := f.db.Member.UpdateOneID(m.ID).SetPeriodStart(f.now).SetPeriodUsedBase(8.5).Save(ctx); err != nil {
		t.Fatalf("reset period: %v", err)
	}
	f.svc.Process(ctx, billing.ChargeEvent{MemberIDs: []int{m.ID}})
	if n := len(f.notifications(t, f.owner.ID)); n != 1 {
		t.Fatalf("after reset rows = %d, want 1", n)
	}
	// 新期再用到 85%：新钥匙 → 再预警一次。
	if _, err := f.db.Member.UpdateOneID(m.ID).SetUsedQuota(17).Save(ctx); err != nil {
		t.Fatalf("spend in new period: %v", err)
	}
	f.svc.Process(ctx, billing.ChargeEvent{MemberIDs: []int{m.ID}})
	rows := f.notifications(t, f.owner.ID)
	if len(rows) != 2 {
		t.Fatalf("after new period rows = %d, want 2", len(rows))
	}
	if rows[0].DedupeKey == rows[1].DedupeKey {
		t.Fatalf("dedupe keys must differ across periods: %q", rows[0].DedupeKey)
	}
}

// monthly 已跨期但鉴权尚未推进 period_start：本期已用从 0 起算，不按旧期用量误报。
func TestMemberRolledPeriodNotYetAdvanced(t *testing.T) {
	f := newFixture(t, "quota_rolled")
	ctx := context.Background()
	anchor := f.now.AddDate(0, -2, 0)
	m, err := f.db.Member.Create().SetName("李四").SetOwnerID(f.owner.ID).
		SetQuotaUsd(10).SetUsedQuota(9).SetQuotaPeriod(entmember.QuotaPeriodMonthly).
		SetPeriodAnchor(anchor).SetPeriodStart(anchor).Save(ctx)
	if err != nil {
		t.Fatalf("create member: %v", err)
	}
	f.svc.Process(ctx, billing.ChargeEvent{MemberIDs: []int{m.ID}})
	if n := len(f.notifications(t, f.owner.ID)); n != 0 {
		t.Fatalf("rolled period must not alert on stale usage, got %d", n)
	}
}

func TestDepartmentThresholds(t *testing.T) {
	cases := []struct {
		name        string
		quota, used float64
		wantLevel   string
		wantTitle   string
	}{
		{"100% → danger 给企业主", 50, 50, "danger", "部门 研发部 本期额度已用尽"},
		{"80% → warning", 50, 40, "warning", "部门 研发部 本期额度已用 80%"},
		{"50% 不预警", 50, 25, "", ""},
		{"额度 0 不预警", 0, 100, "", ""},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t, "quota_dept_"+string(rune('a'+i)))
			ctx := context.Background()
			d, err := f.db.Department.Create().SetName("研发部").SetOwnerID(f.owner.ID).
				SetQuotaUsd(tc.quota).SetUsedQuota(tc.used).
				SetPeriodAnchor(f.now.Add(-time.Hour)).SetPeriodStart(f.now.Add(-time.Hour)).Save(ctx)
			if err != nil {
				t.Fatalf("create department: %v", err)
			}
			f.svc.Process(ctx, billing.ChargeEvent{DepartmentIDs: []int{d.ID}})
			rows := f.notifications(t, f.owner.ID)
			if tc.wantLevel == "" {
				if len(rows) != 0 || f.mail.count() != 0 {
					t.Fatalf("expected no alert, got rows=%d mail=%d", len(rows), f.mail.count())
				}
				return
			}
			if len(rows) != 1 || string(rows[0].Level) != tc.wantLevel || rows[0].Link != "/team" ||
				!strings.Contains(rows[0].Title, tc.wantTitle) {
				t.Fatalf("rows = %d %+v", len(rows), rows)
			}
			if f.mail.count() != 1 || !strings.HasPrefix(f.mail.sent[0], f.owner.Email+"|") {
				t.Fatalf("mail = %v", f.mail.sent)
			}
			f.svc.Process(ctx, billing.ChargeEvent{DepartmentIDs: []int{d.ID}})
			if len(f.notifications(t, f.owner.ID)) != 1 || f.mail.count() != 1 {
				t.Fatalf("duplicate department alert")
			}
		})
	}
}

// OnCharged 异步：空事件不起协程；有事件时最终落库。
func TestOnChargedAsync(t *testing.T) {
	f := newFixture(t, "quota_async")
	m := f.createMember(t, 10, 9, false)
	ctx, cancel := context.WithCancel(context.Background())
	f.svc.OnCharged(ctx, billing.ChargeEvent{MemberIDs: []int{m.ID}})
	cancel() // 请求 context 取消不影响后台复核
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if len(f.notifications(t, f.owner.ID)) == 1 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("async alert not delivered")
}
