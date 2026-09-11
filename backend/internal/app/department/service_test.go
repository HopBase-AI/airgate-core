package department

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DouDOU-start/airgate-core/internal/app/audit"
	"github.com/DouDOU-start/airgate-core/internal/app/teamscope"
)

type stubRepo struct {
	created   Mutation
	updated   Mutation
	find      Department
	all       []Department
	nameTaken bool
	inDept    bool
	anchor    time.Time
	setAnchor *time.Time
	deleted   int
	audits    []audit.Entry
	// periodUsageDept 记录 DepartmentPeriodUsage 被问到的部门，校验负责人总览按部门聚合。
	periodUsageDept int
}

func (s *stubRepo) ListByOwner(_ context.Context, _ int, _ ListFilter) ([]Department, int64, error) {
	return s.all, int64(len(s.all)), nil
}
func (s *stubRepo) AllByOwner(_ context.Context, _ int) ([]Department, error) { return s.all, nil }
func (s *stubRepo) FindOwned(_ context.Context, _ int, id int) (Department, error) {
	if s.find.ID == 0 {
		return Department{}, ErrDepartmentNotFound
	}
	return s.find, nil
}
func (s *stubRepo) Create(_ context.Context, m Mutation) (Department, error) {
	s.created = m
	return Department{ID: 1, Name: *m.Name, QuotaUSD: *m.QuotaUSD, QuotaPeriod: *m.QuotaPeriod, PeriodAnchor: *m.PeriodAnchor, PeriodStart: *m.PeriodStart}, nil
}
func (s *stubRepo) UpdateOwned(_ context.Context, _ int, id int, m Mutation) (Department, error) {
	s.updated = m
	out := s.find
	out.ID = id
	if m.Name != nil {
		out.Name = *m.Name
	}
	if m.QuotaUSD != nil {
		out.QuotaUSD = *m.QuotaUSD
	}
	return out, nil
}
func (s *stubRepo) DeleteOwned(_ context.Context, _ int, id int) error { s.deleted = id; return nil }
func (s *stubRepo) ResetPeriodOwned(_ context.Context, _ int, id int, _ time.Time) (Department, error) {
	return Department{ID: id, QuotaPeriod: QuotaPeriodNone}, nil
}
func (s *stubRepo) NameTaken(_ context.Context, _ int, _ string, _ int) (bool, error) {
	return s.nameTaken, nil
}
func (s *stubRepo) MemberInDepartment(_ context.Context, _ int, _ int, _ int) (bool, error) {
	return s.inDept, nil
}
func (s *stubRepo) Counts(_ context.Context, ids []int) (map[int]int, map[int]int, map[int]float64, error) {
	m, k, q := map[int]int{}, map[int]int{}, map[int]float64{}
	for _, id := range ids {
		m[id], k[id], q[id] = 2, 3, 120
	}
	return m, k, q, nil
}
func (s *stubRepo) Usage(_ context.Context, ids []int, _ time.Time) (map[int]float64, map[int]float64, error) {
	today, thirty := map[int]float64{}, map[int]float64{}
	for _, id := range ids {
		today[id], thirty[id] = 1, 9
	}
	return today, thirty, nil
}
func (s *stubRepo) KeyHashesByDepartment(_ context.Context, _ int) ([]string, error) { return nil, nil }
func (s *stubRepo) MemberAccountIDs(_ context.Context, _ int) ([]int, error)         { return nil, nil }
func (s *stubRepo) OwnerBillingAnchor(_ context.Context, _ int) (time.Time, error) {
	if s.setAnchor != nil {
		return *s.setAnchor, nil
	}
	return s.anchor, nil
}
func (s *stubRepo) SetOwnerBillingAnchor(_ context.Context, _ int, anchor time.Time) error {
	s.setAnchor = &anchor
	return nil
}
func (s *stubRepo) OwnerOverview(_ context.Context, _ int) (float64, int, float64, float64, error) {
	return 1000, 4, 700, 100, nil
}
func (s *stubRepo) OwnerPeriodUsage(_ context.Context, _ int, _ time.Time) (float64, float64, error) {
	return 55, 60, nil
}
func (s *stubRepo) DepartmentPeriodUsage(_ context.Context, id int, _ time.Time) (float64, float64, error) {
	s.periodUsageDept = id
	return 11, 12, nil
}
func (s *stubRepo) Record(_ context.Context, e audit.Entry) { s.audits = append(s.audits, e) }

func TestCreateInheritsOwnerAnchorAndAlignsPeriodStart(t *testing.T) {
	anchor := time.Date(2026, 7, 3, 14, 22, 0, 0, time.UTC)
	repo := &stubRepo{anchor: anchor}
	svc := NewService(repo, repo)
	svc.now = func() time.Time { return time.Date(2026, 9, 9, 10, 0, 0, 0, time.UTC) }
	item, err := svc.Create(context.Background(), teamscope.Owner(7), CreateInput{Name: " 研发部 ", QuotaUSD: 600})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if item.Name != "研发部" || item.QuotaPeriod != QuotaPeriodMonthly {
		t.Fatalf("created = %+v", item)
	}
	if !repo.created.PeriodAnchor.Equal(anchor) {
		t.Fatalf("anchor = %v, want owner anchor %v", repo.created.PeriodAnchor, anchor)
	}
	// 本期起点按锚点逐月对齐：2026-09-03 14:22
	if want := time.Date(2026, 9, 3, 14, 22, 0, 0, time.UTC); !repo.created.PeriodStart.Equal(want) {
		t.Fatalf("period start = %v, want %v", repo.created.PeriodStart, want)
	}
	if item.PeriodEnd == nil || !item.PeriodEnd.Equal(time.Date(2026, 10, 3, 14, 22, 0, 0, time.UTC)) {
		t.Fatalf("period end = %v", item.PeriodEnd)
	}
	if len(repo.audits) != 1 || repo.audits[0].Action != audit.ActionDepartmentCreate || repo.audits[0].OwnerID != 7 {
		t.Fatalf("audit = %+v", repo.audits)
	}
}

func TestCreateRejectsDuplicateNameAndBadInput(t *testing.T) {
	repo := &stubRepo{nameTaken: true, anchor: time.Now()}
	svc := NewService(repo, nil)
	if _, err := svc.Create(context.Background(), teamscope.Owner(7), CreateInput{Name: "研发部"}); !errors.Is(err, ErrNameTaken) {
		t.Fatalf("err = %v, want ErrNameTaken", err)
	}
	if _, err := svc.Create(context.Background(), teamscope.Owner(7), CreateInput{Name: "  "}); !errors.Is(err, ErrNameRequired) {
		t.Fatalf("err = %v, want ErrNameRequired", err)
	}
	if _, err := svc.Create(context.Background(), teamscope.Owner(7), CreateInput{Name: "x", QuotaUSD: -1}); !errors.Is(err, ErrInvalidQuota) {
		t.Fatalf("err = %v, want ErrInvalidQuota", err)
	}
	if _, err := svc.Create(context.Background(), teamscope.Owner(7), CreateInput{Name: "x", QuotaPeriod: "weekly"}); !errors.Is(err, ErrInvalidQuotaPeriod) {
		t.Fatalf("err = %v, want ErrInvalidQuotaPeriod", err)
	}
}

func TestListDecoratesCountsAndUsage(t *testing.T) {
	now := time.Date(2026, 9, 9, 10, 0, 0, 0, time.UTC)
	anchor := time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)
	repo := &stubRepo{all: []Department{
		{ID: 1, Name: "A", QuotaUSD: 100, QuotaPeriod: QuotaPeriodMonthly, PeriodAnchor: anchor, PeriodStart: anchor, PeriodUsedBase: 10, UsedQuota: 40},
		// 已跨期但未推进：本期已用按 0 起算
		{ID: 2, Name: "B", QuotaUSD: 100, QuotaPeriod: QuotaPeriodMonthly, PeriodAnchor: anchor.AddDate(0, -2, 0), PeriodStart: anchor.AddDate(0, -2, 0), UsedQuota: 70},
	}}
	svc := NewService(repo, nil)
	svc.now = func() time.Time { return now }
	result, err := svc.List(context.Background(), teamscope.Owner(7), ListFilter{}, "Asia/Shanghai")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if got := result.List[0]; got.PeriodUsed != 30 || got.MemberCount != 2 || got.KeyCount != 3 || got.MemberQuotaTotal != 120 || got.TodayCost != 1 || got.ThirtyDayCost != 9 {
		t.Fatalf("decorated = %+v", got)
	}
	if got := result.List[1]; got.PeriodUsed != 0 || got.PeriodEnd == nil {
		t.Fatalf("rolled department = %+v", got)
	}
}

func TestSetBillingDayValidatesAndSyncsAnchor(t *testing.T) {
	repo := &stubRepo{anchor: time.Date(2026, 7, 3, 14, 22, 0, 0, time.UTC)}
	svc := NewService(repo, repo)
	svc.now = func() time.Time { return time.Date(2026, 9, 9, 10, 0, 0, 0, time.UTC) }
	if _, err := svc.SetBillingDay(context.Background(), teamscope.Owner(7), 31, "UTC"); !errors.Is(err, ErrInvalidBillingDay) {
		t.Fatalf("day 31 must be rejected: %v", err)
	}
	overview, err := svc.SetBillingDay(context.Background(), teamscope.Owner(7), 15, "Asia/Shanghai")
	if err != nil {
		t.Fatalf("SetBillingDay: %v", err)
	}
	if repo.setAnchor == nil || repo.setAnchor.After(svc.now()) == false {
		t.Fatalf("anchor = %v, want after now", repo.setAnchor)
	}
	// 上海时区 15 日零点 = UTC 14 日 16:00；账期日按展示时区取
	if got := repo.setAnchor.In(time.FixedZone("CST", 8*3600)); got.Day() != 15 || got.Hour() != 0 {
		t.Fatalf("anchor in Asia/Shanghai = %v, want 15th 00:00", got)
	}
	if overview.BillingDay != 15 || overview.Balance != 1000 || overview.MemberCount != 4 || overview.PeriodUsedActual != 55 {
		t.Fatalf("overview = %+v", overview)
	}
	// 过渡态：当前期延续到新锚点
	if !overview.PeriodEnd.Equal(*repo.setAnchor) {
		t.Fatalf("period end = %v, want new anchor %v", overview.PeriodEnd, repo.setAnchor)
	}
	if len(repo.audits) != 1 || repo.audits[0].Action != audit.ActionTeamBillingPeriod {
		t.Fatalf("audit = %+v", repo.audits)
	}
}

func TestUpdateAndDeleteRecordAudit(t *testing.T) {
	repo := &stubRepo{find: Department{ID: 3, Name: "旧名", QuotaUSD: 10, QuotaPeriod: QuotaPeriodMonthly, PeriodAnchor: time.Now(), PeriodStart: time.Now()}}
	svc := NewService(repo, repo)
	name := "新名"
	quota := 20.0
	if _, err := svc.Update(context.Background(), teamscope.Owner(7), 3, UpdateInput{Name: &name, QuotaUSD: &quota}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if err := svc.Delete(context.Background(), teamscope.Owner(7), 3); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if repo.deleted != 3 || len(repo.audits) != 2 {
		t.Fatalf("deleted=%d audits=%d", repo.deleted, len(repo.audits))
	}
	if before, after := repo.audits[0].Before["name"], repo.audits[0].After["name"]; before != "旧名" || after != "新名" {
		t.Fatalf("update audit before/after = %v / %v", before, after)
	}
	if repo.audits[1].Action != audit.ActionDepartmentDelete || repo.audits[1].TargetName != "旧名" {
		t.Fatalf("delete audit = %+v", repo.audits[1])
	}
}

// 负责人必须是当前在本部门的成员：不在 → ErrManagerNotInDepartment；0 = 清空；在 → 写入并进审计快照。
func TestUpdateManagerMustBeMemberOfDepartment(t *testing.T) {
	repo := &stubRepo{find: Department{ID: 3, Name: "研发部", QuotaPeriod: QuotaPeriodMonthly, PeriodAnchor: time.Now(), PeriodStart: time.Now()}}
	svc := NewService(repo, repo)
	ctx := context.Background()
	outsider := int64(9)
	if _, err := svc.Update(ctx, teamscope.Owner(7), 3, UpdateInput{ManagerMemberID: &outsider}); !errors.Is(err, ErrManagerNotInDepartment) {
		t.Fatalf("err = %v, want ErrManagerNotInDepartment", err)
	}
	if repo.updated.HasManagerMemberID {
		t.Fatalf("rejected manager must not reach the store")
	}
	repo.inDept = true
	if _, err := svc.Update(ctx, teamscope.Owner(7), 3, UpdateInput{ManagerMemberID: &outsider}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if !repo.updated.HasManagerMemberID || repo.updated.ManagerMemberID == nil || *repo.updated.ManagerMemberID != 9 {
		t.Fatalf("mutation = %+v", repo.updated)
	}
	if got := repo.audits[len(repo.audits)-1].Before["manager_member_id"]; got != 0 {
		t.Fatalf("audit snapshot must carry manager_member_id, got %v", got)
	}
	clear := int64(0)
	if _, err := svc.Update(ctx, teamscope.Owner(7), 3, UpdateInput{ManagerMemberID: &clear}); err != nil {
		t.Fatalf("Update clear: %v", err)
	}
	if !repo.updated.HasManagerMemberID || repo.updated.ManagerMemberID != nil {
		t.Fatalf("clear mutation = %+v", repo.updated)
	}
	// nil = 不动
	if _, err := svc.Update(ctx, teamscope.Owner(7), 3, UpdateInput{}); err != nil {
		t.Fatalf("Update noop: %v", err)
	}
	if repo.updated.HasManagerMemberID {
		t.Fatalf("nil manager must not touch the store")
	}
}
