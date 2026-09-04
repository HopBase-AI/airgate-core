package member

import (
	"context"
	"errors"
	"testing"
	"time"
)

type stubRepo struct {
	created     Mutation
	updated     Mutation
	listed      []Member
	find        Member
	deleted     int
	resetCalled bool
	// 成员账号相关记录
	accountCreated *AccountInput
	accountPatch   *AccountPatch
	emailTaken     bool
}

func (s *stubRepo) ListByOwner(_ context.Context, _ int, _ ListFilter) ([]Member, int64, error) {
	return s.listed, int64(len(s.listed)), nil
}
func (s *stubRepo) FindOwned(_ context.Context, _ int, _ int) (Member, error) { return s.find, nil }
func (s *stubRepo) Create(_ context.Context, m Mutation) (Member, error) {
	s.created = m
	out := Member{ID: 1, Name: *m.Name, QuotaUSD: *m.QuotaUSD, QuotaPeriod: *m.QuotaPeriod, PeriodAnchor: *m.PeriodAnchor, PeriodStart: *m.PeriodStart}
	if m.Email != nil {
		out.Email = *m.Email
	}
	return out, nil
}
func (s *stubRepo) UpdateOwned(_ context.Context, _ int, id int, m Mutation) (Member, error) {
	s.updated = m
	return Member{ID: id, QuotaPeriod: QuotaPeriodNone}, nil
}
func (s *stubRepo) DeleteOwned(_ context.Context, _ int, id int) error { s.deleted = id; return nil }
func (s *stubRepo) ResetPeriodOwned(_ context.Context, _ int, id int, _ time.Time) (Member, error) {
	s.resetCalled = true
	return Member{ID: id, QuotaPeriod: QuotaPeriodNone}, nil
}
func (s *stubRepo) KeyCounts(_ context.Context, ids []int) (map[int]int, error) {
	out := map[int]int{}
	for _, id := range ids {
		out[id] = 2
	}
	return out, nil
}
func (s *stubRepo) MemberUsage(_ context.Context, ids []int, _ time.Time) (map[int]float64, map[int]float64, error) {
	today, thirty := map[int]float64{}, map[int]float64{}
	for _, id := range ids {
		today[id] = 0.5
		thirty[id] = 7
	}
	return today, thirty, nil
}
func (s *stubRepo) KeyHashesByMember(_ context.Context, _ int) ([]string, error) { return nil, nil }

func TestCreateDefaultsToMonthlyAndTrimsName(t *testing.T) {
	repo := &stubRepo{}
	svc := NewService(repo)
	item, err := svc.Create(context.Background(), 7, CreateInput{Name: "  张三 ", Email: " a@b.c ", QuotaUSD: 50})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if item.Name != "张三" || item.Email != "a@b.c" || item.QuotaPeriod != QuotaPeriodMonthly {
		t.Fatalf("created = %+v", item)
	}
	if repo.created.OwnerID == nil || *repo.created.OwnerID != 7 {
		t.Fatalf("owner not propagated: %+v", repo.created)
	}
	if repo.created.PeriodAnchor == nil || !repo.created.PeriodAnchor.Equal(*repo.created.PeriodStart) {
		t.Fatalf("anchor and period start must both be set to now: %+v", repo.created)
	}
	if item.PeriodEnd == nil {
		t.Fatal("monthly member must expose period end")
	}
}

func TestCreateAndUpdateValidation(t *testing.T) {
	svc := NewService(&stubRepo{})
	ctx := context.Background()
	if _, err := svc.Create(ctx, 1, CreateInput{Name: "   "}); !errors.Is(err, ErrNameRequired) {
		t.Fatalf("blank name error = %v", err)
	}
	if _, err := svc.Create(ctx, 1, CreateInput{Name: "x", QuotaUSD: -1}); !errors.Is(err, ErrInvalidQuota) {
		t.Fatalf("negative quota error = %v", err)
	}
	if _, err := svc.Create(ctx, 1, CreateInput{Name: "x", QuotaPeriod: "weekly"}); !errors.Is(err, ErrInvalidQuotaPeriod) {
		t.Fatalf("bad period error = %v", err)
	}
	bad := "paused"
	if _, err := svc.Update(ctx, 1, 2, UpdateInput{Status: &bad}); !errors.Is(err, ErrInvalidStatus) {
		t.Fatalf("bad status error = %v", err)
	}
	empty := ""
	if _, err := svc.Update(ctx, 1, 2, UpdateInput{Name: &empty}); !errors.Is(err, ErrNameRequired) {
		t.Fatalf("empty name on update error = %v", err)
	}
}

func TestListDecoratesDerivedFields(t *testing.T) {
	now := time.Date(2026, time.March, 15, 12, 0, 0, 0, time.UTC)
	anchor := time.Date(2026, time.January, 31, 9, 0, 0, 0, time.UTC)
	repo := &stubRepo{listed: []Member{
		// 未跨期：本期已用 = 30 − 10
		{ID: 1, QuotaPeriod: QuotaPeriodMonthly, PeriodAnchor: anchor, PeriodStart: time.Date(2026, time.February, 28, 9, 0, 0, 0, time.UTC), PeriodUsedBase: 10, UsedQuota: 30},
		// 已跨期但鉴权尚未推进：展示按新期从 0 起算
		{ID: 2, QuotaPeriod: QuotaPeriodMonthly, PeriodAnchor: anchor, PeriodStart: anchor, PeriodUsedBase: 0, UsedQuota: 60},
		// 一次性：本期已用 = 累计 − 手动重置快照
		{ID: 3, QuotaPeriod: QuotaPeriodNone, PeriodUsedBase: 5, UsedQuota: 8},
	}}
	svc := NewService(repo)
	svc.now = func() time.Time { return now }
	result, err := svc.List(context.Background(), 1, ListFilter{}, "UTC")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	got := result.List
	if got[0].PeriodUsed != 20 || got[0].PeriodEnd == nil || !got[0].PeriodEnd.Equal(time.Date(2026, time.March, 31, 9, 0, 0, 0, time.UTC)) {
		t.Fatalf("member 1 derived = used %v end %v", got[0].PeriodUsed, got[0].PeriodEnd)
	}
	if got[1].PeriodUsed != 0 {
		t.Fatalf("member 2 rolled-over period used = %v, want 0", got[1].PeriodUsed)
	}
	if got[2].PeriodUsed != 3 || got[2].PeriodEnd != nil {
		t.Fatalf("member 3 derived = used %v end %v, want 3 / nil", got[2].PeriodUsed, got[2].PeriodEnd)
	}
	if got[0].KeyCount != 2 || got[0].TodayCost != 0.5 || got[0].ThirtyDayCost != 7 {
		t.Fatalf("aggregates not attached: %+v", got[0])
	}
}

func (s *stubRepo) CreateWithAccount(ctx context.Context, mutation Mutation, account AccountInput) (Member, error) {
	item, err := s.Create(ctx, mutation)
	if err != nil {
		return Member{}, err
	}
	s.accountCreated = &account
	item.AccountUserID = 900000 + item.ID
	item.AccountEmail = account.Email
	item.AllowedGroupIDs = mutation.AllowedGroupIDs
	return item, nil
}

func (s *stubRepo) AccountEmailExists(context.Context, string) (bool, error) {
	return s.emailTaken, nil
}

func (s *stubRepo) UpdateAccountOwned(_ context.Context, _ int, _ int, patch AccountPatch) error {
	s.accountPatch = &patch
	return nil
}

func (s *stubRepo) OwnerVisibleGroupIDs(context.Context, int) ([]int64, error) {
	return []int64{1, 2, 3}, nil
}
