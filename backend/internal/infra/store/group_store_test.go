package store

import (
	"context"
	"testing"

	appgroup "github.com/DouDOU-start/airgate-core/internal/app/group"
)

// groupIDSet 收集分组 ID 便于断言可见性。
func groupIDSet(groups []appgroup.Group) map[int]bool {
	out := make(map[int]bool, len(groups))
	for _, g := range groups {
		out[g.ID] = true
	}
	return out
}

func mustCreateGroup(t *testing.T, store *GroupStore, input appgroup.CreateInput) appgroup.Group {
	t.Helper()
	g, err := store.Create(context.Background(), input)
	if err != nil {
		t.Fatalf("create group %q: %v", input.Name, err)
	}
	return g
}

// TestGroupStoreVisibility 覆盖分组可见性三态：公开 / 指定用户 / 仅管理员，
// 这正是用户报告的两个 bug 的核心：专属分组分配给某用户后该用户应可见，
// 未分配任何人的专属分组对所有普通用户均不可见。
func TestGroupStoreVisibility(t *testing.T) {
	db := enttestOpen(t)
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	}()
	ctx := context.Background()
	store := NewGroupStore(db)

	alice := createTestUser(t, db, "alice-vis@example.com")
	bob := createTestUser(t, db, "bob-vis@example.com")

	pub := mustCreateGroup(t, store, appgroup.CreateInput{
		Name: "public", Platform: "openai", RateMultiplier: 1, StatusVisible: true, SubscriptionType: "standard",
	})
	vip := mustCreateGroup(t, store, appgroup.CreateInput{
		Name: "vip", Platform: "openai", RateMultiplier: 1, IsExclusive: true,
		AllowedUserIDs: []int64{int64(alice.ID)}, StatusVisible: true, SubscriptionType: "standard",
	})
	adminOnly := mustCreateGroup(t, store, appgroup.CreateInput{
		Name: "secret", Platform: "openai", RateMultiplier: 1, IsExclusive: true,
		StatusVisible: true, SubscriptionType: "standard",
	})

	// Create 返回应回填 allowed_users（bug 1 的回填来源）。
	if len(vip.AllowedUsers) != 1 || vip.AllowedUsers[0].Email != alice.Email {
		t.Fatalf("vip.AllowedUsers = %+v, want [alice]", vip.AllowedUsers)
	}
	if len(adminOnly.AllowedUsers) != 0 {
		t.Fatalf("adminOnly.AllowedUsers = %+v, want empty", adminOnly.AllowedUsers)
	}

	// Bug 1：被分配的用户（alice）应能看到 vip；bug 2：未分配任何人的专属分组（secret）谁都看不到。
	aliceVisible, _, err := store.ListAvailable(ctx, appgroup.AvailableFilter{UserID: alice.ID, Page: 1, PageSize: 50})
	if err != nil {
		t.Fatalf("list available for alice: %v", err)
	}
	got := groupIDSet(aliceVisible)
	if !got[pub.ID] || !got[vip.ID] {
		t.Fatalf("alice should see public+vip, got ids %v", got)
	}
	if got[adminOnly.ID] {
		t.Fatalf("alice should NOT see admin-only group, got ids %v", got)
	}

	// bob 未被分配 vip，只应看到公开分组。
	bobVisible, _, err := store.ListAvailable(ctx, appgroup.AvailableFilter{UserID: bob.ID, Page: 1, PageSize: 50})
	if err != nil {
		t.Fatalf("list available for bob: %v", err)
	}
	got = groupIDSet(bobVisible)
	if !got[pub.ID] {
		t.Fatalf("bob should see public group, got ids %v", got)
	}
	if got[vip.ID] || got[adminOnly.ID] {
		t.Fatalf("bob should only see public group, got ids %v", got)
	}
}

// TestGroupStoreUpdateAllowedUsers 覆盖编辑时改授权用户：HasAllowedUserIDs 语义。
func TestGroupStoreUpdateAllowedUsers(t *testing.T) {
	db := enttestOpen(t)
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	}()
	ctx := context.Background()
	store := NewGroupStore(db)

	alice := createTestUser(t, db, "alice-upd@example.com")
	bob := createTestUser(t, db, "bob-upd@example.com")

	vip := mustCreateGroup(t, store, appgroup.CreateInput{
		Name: "vip", Platform: "openai", RateMultiplier: 1, IsExclusive: true,
		AllowedUserIDs: []int64{int64(alice.ID)}, StatusVisible: true, SubscriptionType: "standard",
	})

	// 改派给 bob：HasAllowedUserIDs=true 覆盖列表。
	updated, err := store.Update(ctx, vip.ID, appgroup.UpdateInput{
		HasAllowedUserIDs: true,
		AllowedUserIDs:    []int64{int64(bob.ID)},
	})
	if err != nil {
		t.Fatalf("update allowed users: %v", err)
	}
	if len(updated.AllowedUsers) != 1 || updated.AllowedUsers[0].Email != bob.Email {
		t.Fatalf("after update AllowedUsers = %+v, want [bob]", updated.AllowedUsers)
	}
	aliceVisible, _, _ := store.ListAvailable(ctx, appgroup.AvailableFilter{UserID: alice.ID, Page: 1, PageSize: 50})
	if groupIDSet(aliceVisible)[vip.ID] {
		t.Fatalf("alice should no longer see vip after reassignment")
	}
	bobVisible, _, _ := store.ListAvailable(ctx, appgroup.AvailableFilter{UserID: bob.ID, Page: 1, PageSize: 50})
	if !groupIDSet(bobVisible)[vip.ID] {
		t.Fatalf("bob should see vip after reassignment")
	}

	// HasAllowedUserIDs=true + 空列表 → 清空，变成仅管理员可见。
	cleared, err := store.Update(ctx, vip.ID, appgroup.UpdateInput{
		HasAllowedUserIDs: true,
		AllowedUserIDs:    nil,
	})
	if err != nil {
		t.Fatalf("clear allowed users: %v", err)
	}
	if len(cleared.AllowedUsers) != 0 {
		t.Fatalf("after clear AllowedUsers = %+v, want empty", cleared.AllowedUsers)
	}
	bobVisible, _, _ = store.ListAvailable(ctx, appgroup.AvailableFilter{UserID: bob.ID, Page: 1, PageSize: 50})
	if groupIDSet(bobVisible)[vip.ID] {
		t.Fatalf("bob should not see vip after clearing allowed users")
	}

	// HasAllowedUserIDs=false → 不改动授权用户（此处再设回 alice 验证保留语义）。
	if _, err := store.Update(ctx, vip.ID, appgroup.UpdateInput{
		HasAllowedUserIDs: true,
		AllowedUserIDs:    []int64{int64(alice.ID)},
	}); err != nil {
		t.Fatalf("reassign to alice: %v", err)
	}
	noTouch, err := store.Update(ctx, vip.ID, appgroup.UpdateInput{Name: ptr("vip-renamed")})
	if err != nil {
		t.Fatalf("rename without touching users: %v", err)
	}
	if len(noTouch.AllowedUsers) != 1 || noTouch.AllowedUsers[0].Email != alice.Email {
		t.Fatalf("rename should keep allowed users, got %+v", noTouch.AllowedUsers)
	}
}

func ptr[T any](v T) *T { return &v }
