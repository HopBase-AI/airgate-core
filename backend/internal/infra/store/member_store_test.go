package store

import (
	"context"
	"errors"
	"testing"
	"time"

	appmember "github.com/DouDOU-start/airgate-core/internal/app/member"
)

func TestMemberStoreOwnershipIsolation(t *testing.T) {
	db := enttestOpen(t)
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	}()
	ctx := context.Background()
	owner := createTestUser(t, db, "member-owner@example.com")
	stranger := createTestUser(t, db, "member-stranger@example.com")
	store := NewMemberStore(db)

	name := "研发-张三"
	quota := 50.0
	monthly := appmember.QuotaPeriodMonthly
	now := time.Now()
	created, err := store.Create(ctx, appmember.Mutation{
		OwnerID: &owner.ID, Name: &name, QuotaUSD: &quota, QuotaPeriod: &monthly,
		PeriodAnchor: &now, PeriodStart: &now,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.OwnerID != owner.ID || created.Name != name || created.QuotaUSD != 50 || created.QuotaPeriod != monthly {
		t.Fatalf("created member mismatch: %+v", created)
	}

	// 别人的成员：查 / 改 / 删 / 重置 都按不存在处理
	if _, err := store.FindOwned(ctx, stranger.ID, created.ID); !errors.Is(err, appmember.ErrMemberNotFound) {
		t.Fatalf("FindOwned by stranger error = %v, want ErrMemberNotFound", err)
	}
	if _, err := store.UpdateOwned(ctx, stranger.ID, created.ID, appmember.Mutation{Name: &name}); !errors.Is(err, appmember.ErrMemberNotFound) {
		t.Fatalf("UpdateOwned by stranger error = %v, want ErrMemberNotFound", err)
	}
	if err := store.DeleteOwned(ctx, stranger.ID, created.ID); !errors.Is(err, appmember.ErrMemberNotFound) {
		t.Fatalf("DeleteOwned by stranger error = %v, want ErrMemberNotFound", err)
	}
	if _, err := store.ResetPeriodOwned(ctx, stranger.ID, created.ID, now); !errors.Is(err, appmember.ErrMemberNotFound) {
		t.Fatalf("ResetPeriodOwned by stranger error = %v, want ErrMemberNotFound", err)
	}

	list, total, err := store.ListByOwner(ctx, owner.ID, appmember.ListFilter{Page: 1, PageSize: 20})
	if err != nil || total != 1 || len(list) != 1 {
		t.Fatalf("ListByOwner = (%d items, total %d, err %v), want 1", len(list), total, err)
	}
	if _, total, err := store.ListByOwner(ctx, stranger.ID, appmember.ListFilter{Page: 1, PageSize: 20}); err != nil || total != 0 {
		t.Fatalf("stranger ListByOwner total = %d (err %v), want 0", total, err)
	}
	if _, total, err := store.ListByOwner(ctx, owner.ID, appmember.ListFilter{Page: 1, PageSize: 20, Keyword: "张三"}); err != nil || total != 1 {
		t.Fatalf("keyword search total = %d (err %v), want 1", total, err)
	}
}

func TestMemberStoreDeleteCascadesKeysButKeepsUsage(t *testing.T) {
	db := enttestOpen(t)
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	}()
	ctx := context.Background()
	owner := createTestUser(t, db, "member-cascade@example.com")
	member, err := db.Member.Create().SetName("王五").SetOwnerID(owner.ID).Save(ctx)
	if err != nil {
		t.Fatalf("create member: %v", err)
	}
	key, err := db.APIKey.Create().SetName("k1").SetKeyHint("sk-k1").SetKeyHash("hash-k1").SetUserID(owner.ID).SetMemberID(member.ID).Save(ctx)
	if err != nil {
		t.Fatalf("create key: %v", err)
	}
	other, err := db.APIKey.Create().SetName("k2").SetKeyHint("sk-k2").SetKeyHash("hash-k2").SetUserID(owner.ID).Save(ctx)
	if err != nil {
		t.Fatalf("create other key: %v", err)
	}
	usage, err := db.UsageLog.Create().SetPlatform("openai").SetModel("gpt-5").SetUserID(owner.ID).SetAPIKeyID(key.ID).SetMemberID(member.ID).SetActualCost(1).Save(ctx)
	if err != nil {
		t.Fatalf("create usage: %v", err)
	}

	store := NewMemberStore(db)
	hashes, err := store.KeyHashesByMember(ctx, member.ID)
	if err != nil || len(hashes) != 1 || hashes[0] != "hash-k1" {
		t.Fatalf("KeyHashesByMember = %v (err %v), want [hash-k1]", hashes, err)
	}
	counts, err := store.KeyCounts(ctx, []int{member.ID})
	if err != nil || counts[member.ID] != 1 {
		t.Fatalf("KeyCounts = %v (err %v), want {member:1}", counts, err)
	}

	if err := store.DeleteOwned(ctx, owner.ID, member.ID); err != nil {
		t.Fatalf("DeleteOwned: %v", err)
	}
	if exists, _ := db.APIKey.Query().Where().Exist(ctx); !exists {
		t.Fatal("unrelated key must survive member deletion")
	}
	if _, err := db.APIKey.Get(ctx, key.ID); err == nil {
		t.Fatal("member key must be deleted with the member")
	}
	if _, err := db.APIKey.Get(ctx, other.ID); err != nil {
		t.Fatalf("unrelated key deleted: %v", err)
	}
	reloaded, err := db.UsageLog.Get(ctx, usage.ID)
	if err != nil {
		t.Fatalf("usage log must be kept: %v", err)
	}
	if reloaded.MemberID != member.ID {
		t.Fatalf("usage member_id snapshot = %d, want %d", reloaded.MemberID, member.ID)
	}
}

func TestMemberStoreResetPeriodAndUsageAggregation(t *testing.T) {
	db := enttestOpen(t)
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	}()
	ctx := context.Background()
	owner := createTestUser(t, db, "member-reset@example.com")
	member, err := db.Member.Create().SetName("赵六").SetOwnerID(owner.ID).SetUsedQuota(42).SetUsedQuotaActual(30).Save(ctx)
	if err != nil {
		t.Fatalf("create member: %v", err)
	}
	store := NewMemberStore(db)

	now := time.Now().Truncate(time.Second)
	reset, err := store.ResetPeriodOwned(ctx, owner.ID, member.ID, now)
	if err != nil {
		t.Fatalf("ResetPeriodOwned: %v", err)
	}
	if reset.PeriodUsedBase != 42 || !reset.PeriodStart.Equal(now) {
		t.Fatalf("reset = base %v start %v, want base 42 start %v", reset.PeriodUsedBase, reset.PeriodStart, now)
	}

	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	for _, spec := range []struct {
		at   time.Time
		cost float64
	}{
		{todayStart.Add(time.Hour), 1.25},    // 今日
		{todayStart.AddDate(0, 0, -3), 2},    // 近 30 天
		{todayStart.AddDate(0, 0, -40), 100}, // 30 天外，不计
	} {
		if _, err := db.UsageLog.Create().SetPlatform("openai").SetModel("gpt-5").SetUserID(owner.ID).SetMemberID(member.ID).SetActualCost(spec.cost).SetCreatedAt(spec.at).Save(ctx); err != nil {
			t.Fatalf("create usage: %v", err)
		}
	}
	today, thirty, err := store.MemberUsage(ctx, []int{member.ID}, todayStart)
	if err != nil {
		t.Fatalf("MemberUsage: %v", err)
	}
	if today[member.ID] != 1.25 || thirty[member.ID] != 3.25 {
		t.Fatalf("MemberUsage = today %v / 30d %v, want 1.25 / 3.25", today[member.ID], thirty[member.ID])
	}
}
