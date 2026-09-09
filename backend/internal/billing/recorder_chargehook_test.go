package billing

import (
	"context"
	"reflect"
	"sync"
	"testing"

	"entgo.io/ent/dialect/sql/schema"
	_ "github.com/mattn/go-sqlite3"

	"github.com/DouDOU-start/airgate-core/ent"
	"github.com/DouDOU-start/airgate-core/ent/enttest"
)

type chargeHookSpy struct {
	mu     sync.Mutex
	events []ChargeEvent
}

func (s *chargeHookSpy) hook(_ context.Context, ev ChargeEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, ev)
}

func (s *chargeHookSpy) all() []ChargeEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]ChargeEvent(nil), s.events...)
}

type chargeHookFixture struct {
	db     *ent.Client
	user   *ent.User
	dept   *ent.Department
	member *ent.Member
	key    *ent.APIKey
	group  *ent.Group
}

func newChargeHookFixture(t *testing.T, name string) chargeHookFixture {
	t.Helper()
	db := enttest.Open(t, "sqlite3", "file:"+name+"?mode=memory&cache=shared&_fk=1", enttest.WithMigrateOptions(schema.WithGlobalUniqueID(false)))
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	user := createBillingTestUser(t, ctx, db, name+"@example.com")
	group, err := db.Group.Create().SetName("OpenAI").SetPlatform("openai").Save(ctx)
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	dept, err := db.Department.Create().SetName("研发部").SetOwnerID(user.ID).SetQuotaUsd(50).Save(ctx)
	if err != nil {
		t.Fatalf("create department: %v", err)
	}
	member, err := db.Member.Create().SetName("李四").SetOwnerID(user.ID).SetQuotaUsd(10).SetDepartment(dept).Save(ctx)
	if err != nil {
		t.Fatalf("create member: %v", err)
	}
	key, err := db.APIKey.Create().SetName("k").SetKeyHash("hash-" + name).SetUserID(user.ID).SetGroupID(group.ID).SetMemberID(member.ID).Save(ctx)
	if err != nil {
		t.Fatalf("create api key: %v", err)
	}
	if _, err := db.User.UpdateOneID(user.ID).SetBalance(100).Save(ctx); err != nil {
		t.Fatalf("fund user: %v", err)
	}
	return chargeHookFixture{db: db, user: user, dept: dept, member: member, key: key, group: group}
}

func (f chargeHookFixture) record(cost float64) UsageRecord {
	return UsageRecord{
		UserID: f.user.ID, UserEmail: f.user.Email, APIKeyID: f.key.ID, MemberID: f.member.ID, DepartmentID: f.dept.ID,
		GroupID: f.group.ID, Platform: "openai", Model: "gpt-5", TotalCost: cost, ActualCost: cost, BilledCost: cost,
	}
}

// 扣费提交后回调收到本批实际发生扣费/累加的成员 / 部门 / 用户 id；零费用与失败记录不触发。
func TestChargeHookReceivesChargedIDs(t *testing.T) {
	cases := []struct {
		name    string
		records []UsageRecord
		sync    bool
		want    []ChargeEvent
	}{
		{"RecordSync 单条", nil, true, []ChargeEvent{{UserIDs: []int{1}, MemberIDs: []int{1}, DepartmentIDs: []int{1}}}},
		{"批量刷盘去重后只回调一次", nil, false, []ChargeEvent{{UserIDs: []int{1}, MemberIDs: []int{1}, DepartmentIDs: []int{1}}}},
		{"零费用不触发", nil, true, nil},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newChargeHookFixture(t, "billing_chargehook_"+string(rune('a'+i)))
			spy := &chargeHookSpy{}
			recorder := NewRecorder(f.db, 0)
			recorder.SetChargeHook(spy.hook)
			ctx := context.Background()
			cost := 2.0
			if tc.want == nil {
				cost = 0
			}
			if tc.sync {
				if _, err := recorder.RecordSync(ctx, f.record(cost)); err != nil {
					t.Fatalf("RecordSync: %v", err)
				}
			} else if err := recorder.batchInsert(ctx, []UsageRecord{f.record(cost), f.record(cost)}); err != nil {
				t.Fatalf("batchInsert: %v", err)
			}
			got := spy.all()
			// fixture 内 id 都从 1 起（GlobalUniqueID 关闭）；按实际 id 换算期望值。
			for j := range tc.want {
				tc.want[j] = ChargeEvent{UserIDs: []int{f.user.ID}, MemberIDs: []int{f.member.ID}, DepartmentIDs: []int{f.dept.ID}}
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("events = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// 回调 panic 被吞掉，不影响计费结果；未注册回调也不报错。
func TestChargeHookPanicDoesNotFailBilling(t *testing.T) {
	f := newChargeHookFixture(t, "billing_chargehook_panic")
	recorder := NewRecorder(f.db, 0)
	recorder.SetChargeHook(func(context.Context, ChargeEvent) { panic("boom") })
	ctx := context.Background()
	if _, err := recorder.RecordSync(ctx, f.record(1)); err != nil {
		t.Fatalf("RecordSync with panicking hook: %v", err)
	}
	reloaded, err := f.db.Member.Get(ctx, f.member.ID)
	if err != nil || reloaded.UsedQuota != 1 {
		t.Fatalf("member used_quota = %v err=%v, want 1", reloaded.UsedQuota, err)
	}
	plain := NewRecorder(f.db, 0)
	if _, err := plain.RecordSync(ctx, f.record(1)); err != nil {
		t.Fatalf("RecordSync without hook: %v", err)
	}
}
