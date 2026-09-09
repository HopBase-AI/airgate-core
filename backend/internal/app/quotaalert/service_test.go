package quotaalert_test

import (
	"context"
	"strconv"
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
	account *ent.User // 被预警成员自己的登录账号
	manager *ent.User // 部门负责人的登录账号
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
	manager, err := db.User.Create().SetEmail("manager-" + name + "@example.com").SetPasswordHash("x").Save(ctx)
	if err != nil {
		t.Fatalf("create manager account: %v", err)
	}
	mail := &mailbox{}
	notifier := appnotification.NewService(store.NewNotificationStore(db))
	svc := quotaalert.NewService(store.NewQuotaAlertStore(db), notifier)
	svc.SetEmailSender(mail.send)
	return &fixture{db: db, svc: svc, mail: mail, owner: owner, account: account, manager: manager, now: time.Now()}
}

// createDepartment 建部门（quota 0 = 不限）。
func (f *fixture) createDepartment(t *testing.T, quota, used float64) *ent.Department {
	t.Helper()
	d, err := f.db.Department.Create().SetName("研发部").SetOwnerID(f.owner.ID).
		SetQuotaUsd(quota).SetUsedQuota(used).
		SetPeriodAnchor(f.now.Add(-time.Hour)).SetPeriodStart(f.now.Add(-time.Hour)).Save(context.Background())
	if err != nil {
		t.Fatalf("create department: %v", err)
	}
	return d
}

// setManager 建一名挂在 f.manager 账号上的部门成员并设为负责人。
func (f *fixture) setManager(t *testing.T, d *ent.Department) *ent.Member {
	t.Helper()
	ctx := context.Background()
	m, err := f.db.Member.Create().SetName("负责人").SetOwnerID(f.owner.ID).SetDepartment(d).SetAccount(f.manager).
		SetPeriodAnchor(f.now.Add(-time.Hour)).SetPeriodStart(f.now.Add(-time.Hour)).Save(ctx)
	if err != nil {
		t.Fatalf("create manager member: %v", err)
	}
	if _, err := f.db.Department.UpdateOneID(d.ID).SetManager(m).Save(ctx); err != nil {
		t.Fatalf("set manager: %v", err)
	}
	return m
}

func (f *fixture) createMember(t *testing.T, quota, used float64, withAccount bool) *ent.Member {
	return f.createMemberIn(t, nil, quota, used, withAccount)
}

// createMemberIn 建成员并挂到部门（dept 为 nil = 未分配）。
func (f *fixture) createMemberIn(t *testing.T, dept *ent.Department, quota, used float64, withAccount bool) *ent.Member {
	t.Helper()
	builder := f.db.Member.Create().SetName("张三").SetOwnerID(f.owner.ID).
		SetQuotaUsd(quota).SetUsedQuota(used).
		SetPeriodAnchor(f.now.Add(-time.Hour)).SetPeriodStart(f.now.Add(-time.Hour))
	if withAccount {
		builder = builder.SetAccount(f.account)
	}
	if dept != nil {
		builder = builder.SetDepartment(dept)
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

// 成员预警收件人 = 企业主 + 所属部门负责人；成员本人不再收到自己的预警。
func TestMemberThresholds(t *testing.T) {
	cases := []struct {
		name        string
		quota, used float64
		withAccount bool
		withManager bool   // 成员挂在有负责人的部门下
		wantLevel   string // "" = 不预警
		wantTitle   string // 标题片段（企业主与负责人同题）
	}{
		{"85% → warning，企业主与负责人各一条，成员本人不收", 10, 8.5, true, true, "warning", "成员 张三 本期额度已用 85%"},
		{"100% → danger", 10, 10, true, true, "danger", "成员 张三 本期额度已用尽"},
		{"超额 120% 仍 danger", 10, 12, true, true, "danger", "成员 张三 本期额度已用尽"},
		{"79% 不预警", 10, 7.9, true, true, "", ""},
		{"额度 0（不限）不预警", 0, 999, true, true, "", ""},
		{"无负责人：只投企业主", 10, 9, true, false, "warning", "成员 张三 本期额度已用 90%"},
		{"老模型成员无登录账号：企业主 + 负责人", 10, 9, false, true, "warning", "成员 张三 本期额度已用 90%"},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t, "quota_member_"+string(rune('a'+i)))
			d := f.createDepartment(t, 0, 0)
			if tc.withManager {
				f.setManager(t, d)
			}
			m := f.createMemberIn(t, d, tc.quota, tc.used, tc.withAccount)
			f.svc.Process(context.Background(), billing.ChargeEvent{MemberIDs: []int{m.ID}})

			ownerRows := f.notifications(t, f.owner.ID)
			selfRows := f.notifications(t, f.account.ID)
			managerRows := f.notifications(t, f.manager.ID)
			if len(selfRows) != 0 {
				t.Fatalf("member self must not receive its own alert, got %d", len(selfRows))
			}
			if tc.wantLevel == "" {
				if len(ownerRows)+len(managerRows) != 0 || f.mail.count() != 0 {
					t.Fatalf("expected no alert, got owner=%d manager=%d mail=%d", len(ownerRows), len(managerRows), f.mail.count())
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
			if tc.withManager {
				wantMails = 2
				if len(managerRows) != 1 {
					t.Fatalf("manager notifications = %d, want 1", len(managerRows))
				}
				mgr := managerRows[0]
				wantLink := "/usage?department_id=" + strconv.Itoa(d.ID)
				if string(mgr.Level) != tc.wantLevel || mgr.Link != wantLink || !strings.Contains(mgr.Title, tc.wantTitle) ||
					!strings.Contains(mgr.Content, "请关注本部门用量，如需调整额度请联系企业管理员") {
					t.Fatalf("manager row = level %s link %s title %q content %q", mgr.Level, mgr.Link, mgr.Title, mgr.Content)
				}
				if mgr.DedupeKey == row.DedupeKey || !strings.HasSuffix(mgr.DedupeKey, ":"+strconv.Itoa(f.manager.ID)) {
					t.Fatalf("manager dedupe key = %q (owner %q)", mgr.DedupeKey, row.DedupeKey)
				}
			} else if len(managerRows) != 0 {
				t.Fatalf("no manager must mean no manager row, got %d", len(managerRows))
			}
			if f.mail.count() != wantMails {
				t.Fatalf("mails = %d, want %d", f.mail.count(), wantMails)
			}

			// 同一期再次触发：dedupe 拦下，站内信与邮件都不重复。
			f.svc.Process(context.Background(), billing.ChargeEvent{MemberIDs: []int{m.ID}})
			if n := len(f.notifications(t, f.owner.ID)) + len(f.notifications(t, f.manager.ID)) + len(f.notifications(t, f.account.ID)); n != len(ownerRows)+len(managerRows) {
				t.Fatalf("duplicate alert: rows = %d", n)
			}
			if f.mail.count() != wantMails {
				t.Fatalf("duplicate mail: %d", f.mail.count())
			}
		})
	}
}

// 负责人就是被预警的成员本人：本人不收（成员自身预警已取消），只有企业主一条。
func TestMemberAlertSkipsManagerWhoIsTheMember(t *testing.T) {
	f := newFixture(t, "quota_member_is_manager")
	ctx := context.Background()
	d := f.createDepartment(t, 0, 0)
	m, err := f.db.Member.Create().SetName("张三").SetOwnerID(f.owner.ID).SetDepartment(d).SetAccount(f.manager).
		SetQuotaUsd(10).SetUsedQuota(9).
		SetPeriodAnchor(f.now.Add(-time.Hour)).SetPeriodStart(f.now.Add(-time.Hour)).Save(ctx)
	if err != nil {
		t.Fatalf("create member: %v", err)
	}
	if _, err := f.db.Department.UpdateOneID(d.ID).SetManager(m).Save(ctx); err != nil {
		t.Fatalf("set manager: %v", err)
	}
	f.svc.Process(ctx, billing.ChargeEvent{MemberIDs: []int{m.ID}})
	if n := len(f.notifications(t, f.owner.ID)); n != 1 {
		t.Fatalf("owner rows = %d, want 1", n)
	}
	if n := len(f.notifications(t, f.manager.ID)); n != 0 {
		t.Fatalf("manager who is the member must not receive, got %d", n)
	}
	if f.mail.count() != 1 {
		t.Fatalf("mails = %d, want 1", f.mail.count())
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
	// 成员本人不再收信：两级各一封给企业主。
	if f.mail.count() != 2 {
		t.Fatalf("mails = %d, want 2", f.mail.count())
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

// 部门预警收件人 = 企业主 + 部门负责人（如有）。
func TestDepartmentThresholds(t *testing.T) {
	cases := []struct {
		name        string
		quota, used float64
		withManager bool
		wantLevel   string
		wantTitle   string
	}{
		{"100% → danger 企业主 + 负责人", 50, 50, true, "danger", "部门 研发部 本期额度已用尽"},
		{"80% → warning 企业主 + 负责人", 50, 40, true, "warning", "部门 研发部 本期额度已用 80%"},
		{"无负责人：只投企业主", 50, 40, false, "warning", "部门 研发部 本期额度已用 80%"},
		{"50% 不预警", 50, 25, true, "", ""},
		{"额度 0 不预警", 0, 100, true, "", ""},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t, "quota_dept_"+string(rune('a'+i)))
			ctx := context.Background()
			d := f.createDepartment(t, tc.quota, tc.used)
			if tc.withManager {
				f.setManager(t, d)
			}
			f.svc.Process(ctx, billing.ChargeEvent{DepartmentIDs: []int{d.ID}})
			rows := f.notifications(t, f.owner.ID)
			managerRows := f.notifications(t, f.manager.ID)
			if tc.wantLevel == "" {
				if len(rows)+len(managerRows) != 0 || f.mail.count() != 0 {
					t.Fatalf("expected no alert, got rows=%d manager=%d mail=%d", len(rows), len(managerRows), f.mail.count())
				}
				return
			}
			if len(rows) != 1 || string(rows[0].Level) != tc.wantLevel || rows[0].Link != "/team" ||
				!strings.Contains(rows[0].Title, tc.wantTitle) {
				t.Fatalf("rows = %d %+v", len(rows), rows)
			}
			if !strings.HasSuffix(rows[0].DedupeKey, ":"+strconv.Itoa(f.owner.ID)) {
				t.Fatalf("owner dedupe key must end with recipient: %q", rows[0].DedupeKey)
			}
			wantMails := 1
			if tc.withManager {
				wantMails = 2
				wantLink := "/usage?department_id=" + strconv.Itoa(d.ID)
				if len(managerRows) != 1 || string(managerRows[0].Level) != tc.wantLevel || managerRows[0].Link != wantLink ||
					!strings.Contains(managerRows[0].Title, tc.wantTitle) ||
					!strings.Contains(managerRows[0].Content, "请关注本部门用量，如需调整额度请联系企业管理员") {
					t.Fatalf("manager rows = %d %+v", len(managerRows), managerRows)
				}
				if !strings.HasSuffix(managerRows[0].DedupeKey, ":"+strconv.Itoa(f.manager.ID)) {
					t.Fatalf("manager dedupe key = %q", managerRows[0].DedupeKey)
				}
			} else if len(managerRows) != 0 {
				t.Fatalf("no manager must mean no manager row, got %d", len(managerRows))
			}
			if f.mail.count() != wantMails || !strings.HasPrefix(f.mail.sent[0], f.owner.Email+"|") {
				t.Fatalf("mail = %v", f.mail.sent)
			}
			f.svc.Process(ctx, billing.ChargeEvent{DepartmentIDs: []int{d.ID}})
			if len(f.notifications(t, f.owner.ID)) != 1 || len(f.notifications(t, f.manager.ID)) != len(managerRows) || f.mail.count() != wantMails {
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
