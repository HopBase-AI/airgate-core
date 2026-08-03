package store

import (
	"context"
	"testing"

	entaccount "github.com/DouDOU-start/airgate-core/ent/account"
	appaccount "github.com/DouDOU-start/airgate-core/internal/app/account"
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

func TestGroupStoreLoadsRoutableAccountSnapshot(t *testing.T) {
	db := enttestOpen(t)
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	}()
	ctx := context.Background()
	store := NewGroupStore(db)

	viewer := createTestUser(t, db, "pricing-snapshot@example.com")
	group := mustCreateGroup(t, store, appgroup.CreateInput{
		Name: "image-provider", Platform: "openai", RateMultiplier: 1,
		StatusVisible: true, SubscriptionType: "standard",
	})
	active, err := db.Account.Create().
		SetName("active").
		SetPlatform("openai").
		SetType("apikey").
		SetCredentials(map[string]string{}).
		AddGroupIDs(group.ID).
		Save(ctx)
	if err != nil {
		t.Fatalf("create active account: %v", err)
	}
	degraded, err := db.Account.Create().
		SetName("degraded").
		SetPlatform("openai").
		SetType("apikey").
		SetState(entaccount.StateDegraded).
		SetCredentials(map[string]string{}).
		AddGroupIDs(group.ID).
		Save(ctx)
	if err != nil {
		t.Fatalf("create degraded account: %v", err)
	}
	if _, err := db.Account.Create().
		SetName("disabled").
		SetPlatform("openai").
		SetType("apikey").
		SetState(entaccount.StateDisabled).
		SetCredentials(map[string]string{}).
		AddGroupIDs(group.ID).
		Save(ctx); err != nil {
		t.Fatalf("create disabled account: %v", err)
	}
	if _, err := db.Account.Create().
		SetName("cross-platform").
		SetPlatform("claude").
		SetType("apikey").
		SetCredentials(map[string]string{}).
		AddGroupIDs(group.ID).
		Save(ctx); err != nil {
		t.Fatalf("create cross-platform account: %v", err)
	}

	assertSnapshot := func(label string, got appgroup.Group) {
		t.Helper()
		if !got.AccountAvailabilityKnown {
			t.Fatalf("%s account availability is unknown", label)
		}
		want := []int64{int64(active.ID), int64(degraded.ID)}
		if len(got.RoutableAccountIDs) != len(want) {
			t.Fatalf("%s routable account IDs = %v, want %v", label, got.RoutableAccountIDs, want)
		}
		for i := range want {
			if got.RoutableAccountIDs[i] != want[i] {
				t.Fatalf("%s routable account IDs = %v, want %v", label, got.RoutableAccountIDs, want)
			}
		}
	}

	found, err := store.FindByID(ctx, group.ID)
	if err != nil {
		t.Fatalf("find group: %v", err)
	}
	assertSnapshot("FindByID", found)

	available, _, err := store.ListAvailable(ctx, appgroup.AvailableFilter{
		UserID: viewer.ID, Page: 1, PageSize: 50,
	})
	if err != nil {
		t.Fatalf("list available groups: %v", err)
	}
	for _, item := range available {
		if item.ID == group.ID {
			assertSnapshot("ListAvailable", item)
			return
		}
	}
	t.Fatalf("ListAvailable did not return group %d", group.ID)
}

func TestGroupStoreUpdateSanitizesModelRoutingToBoundPlatformAccounts(t *testing.T) {
	db := enttestOpen(t)
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	}()
	ctx := context.Background()
	store := NewGroupStore(db)

	g := mustCreateGroup(t, store, appgroup.CreateInput{
		Name: "codex-pro", Platform: "openai", RateMultiplier: 1, StatusVisible: true, SubscriptionType: "standard",
	})
	activeA, err := db.Account.Create().
		SetName("active-a").
		SetPlatform("openai").
		SetType("apikey").
		SetCredentials(map[string]string{"api_key": "sk-a"}).
		AddGroupIDs(g.ID).
		Save(ctx)
	if err != nil {
		t.Fatalf("create active account a: %v", err)
	}
	activeB, err := db.Account.Create().
		SetName("active-b").
		SetPlatform("openai").
		SetType("apikey").
		SetCredentials(map[string]string{"api_key": "sk-b"}).
		AddGroupIDs(g.ID).
		Save(ctx)
	if err != nil {
		t.Fatalf("create active account b: %v", err)
	}
	disabled, err := db.Account.Create().
		SetName("disabled").
		SetPlatform("openai").
		SetType("apikey").
		SetState(entaccount.StateDisabled).
		SetCredentials(map[string]string{"api_key": "sk-disabled"}).
		AddGroupIDs(g.ID).
		Save(ctx)
	if err != nil {
		t.Fatalf("create disabled account: %v", err)
	}

	updated, err := store.Update(ctx, g.ID, appgroup.UpdateInput{
		ModelRouting: map[string][]int64{
			"gpt-5.5":       {int64(disabled.ID), 999999},
			"gpt-5.4":       {int64(activeB.ID), int64(activeA.ID), int64(activeB.ID), 999999},
			"disabled-only": {},
		},
	})
	if err != nil {
		t.Fatalf("update model routing: %v", err)
	}

	if got := updated.ModelRouting["gpt-5.5"]; len(got) != 1 || got[0] != int64(disabled.ID) {
		t.Fatalf("disabled bound account route = %v, want structurally preserved account [%d]", got, disabled.ID)
	}
	if got := updated.ModelRouting["gpt-5.4"]; len(got) != 2 || got[0] != int64(activeB.ID) || got[1] != int64(activeA.ID) {
		t.Fatalf("mixed route cleanup = %v, want submitted active order without duplicates [%d %d]", got, activeB.ID, activeA.ID)
	}
	if got := updated.ModelRouting["disabled-only"]; len(got) != 0 {
		t.Fatalf("explicit empty route = %v, want empty", got)
	}
}

func TestAccountStoreCreateReconcilesGroupModelRouting(t *testing.T) {
	db := enttestOpen(t)
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	}()
	ctx := context.Background()
	groupStore := NewGroupStore(db)
	accountStore := NewAccountStore(db)

	g := mustCreateGroup(t, groupStore, appgroup.CreateInput{
		Name: "codex-plus", Platform: "openai", RateMultiplier: 1, StatusVisible: true, SubscriptionType: "standard",
		ModelRouting: map[string][]int64{"gpt-5.6": {}, "gpt-5.6-sol": {}},
	})
	created, err := accountStore.Create(ctx, appaccount.CreateInput{
		Name: "plus-a", Platform: "openai", Type: "oauth", Credentials: map[string]string{}, GroupIDs: []int64{int64(g.ID)},
	})
	if err != nil {
		t.Fatalf("create account with group: %v", err)
	}

	after, err := groupStore.FindByID(ctx, g.ID)
	if err != nil {
		t.Fatalf("find group: %v", err)
	}
	for _, model := range []string{"gpt-5.6", "gpt-5.6-sol"} {
		if got := after.ModelRouting[model]; len(got) != 1 || got[0] != int64(created.ID) {
			t.Fatalf("%s route after account create = %v, want [%d]", model, got, created.ID)
		}
	}
}

func TestGroupStoreBackfillEmptyModelRouting(t *testing.T) {
	db := enttestOpen(t)
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	}()
	ctx := context.Background()
	store := NewGroupStore(db)

	g := mustCreateGroup(t, store, appgroup.CreateInput{
		Name: "codex-plus", Platform: "openai", RateMultiplier: 1, StatusVisible: true, SubscriptionType: "standard",
		ModelRouting: map[string][]int64{"gpt-5.6": {}, "gpt-5.6-sol": {}},
	})
	active, err := db.Account.Create().
		SetName("active").
		SetPlatform("openai").
		SetType("oauth").
		SetCredentials(map[string]string{}).
		AddGroupIDs(g.ID).
		Save(ctx)
	if err != nil {
		t.Fatalf("create active account: %v", err)
	}
	disabled, err := db.Account.Create().
		SetName("disabled").
		SetPlatform("openai").
		SetType("oauth").
		SetState(entaccount.StateDisabled).
		SetCredentials(map[string]string{}).
		AddGroupIDs(g.ID).
		Save(ctx)
	if err != nil {
		t.Fatalf("create disabled account: %v", err)
	}
	crossPlatform, err := db.Account.Create().
		SetName("claude").
		SetPlatform("claude").
		SetType("oauth").
		SetCredentials(map[string]string{}).
		AddGroupIDs(g.ID).
		Save(ctx)
	if err != nil {
		t.Fatalf("create cross-platform account: %v", err)
	}
	if _, err := store.Update(ctx, g.ID, appgroup.UpdateInput{
		ModelRouting: map[string][]int64{
			"gpt-5.6":     {},
			"gpt-5.6-sol": {int64(active.ID)},
		},
	}); err != nil {
		t.Fatalf("seed mixed model routing: %v", err)
	}

	groups, routes, err := store.BackfillEmptyModelRouting(ctx)
	if err != nil {
		t.Fatalf("backfill empty model routing: %v", err)
	}
	if groups != 1 || routes != 1 {
		t.Fatalf("backfill counts = groups:%d routes:%d, want 1/1", groups, routes)
	}
	after, err := store.FindByID(ctx, g.ID)
	if err != nil {
		t.Fatalf("find group: %v", err)
	}
	if got := after.ModelRouting["gpt-5.6"]; len(got) != 1 || got[0] != int64(active.ID) {
		t.Fatalf("backfilled route = %v, want active same-platform account [%d]", got, active.ID)
	}
	if got := after.ModelRouting["gpt-5.6-sol"]; len(got) != 1 || got[0] != int64(active.ID) {
		t.Fatalf("existing route = %v, want unchanged [%d]", got, active.ID)
	}
	for _, ids := range after.ModelRouting {
		for _, id := range ids {
			if id == int64(disabled.ID) {
				t.Fatalf("disabled account %d was added before recovery: %v", disabled.ID, after.ModelRouting)
			}
			if id == int64(crossPlatform.ID) {
				t.Fatalf("cross-platform account %d was added to routing %v", crossPlatform.ID, after.ModelRouting)
			}
		}
	}

	groups, routes, err = store.BackfillEmptyModelRouting(ctx)
	if err != nil {
		t.Fatalf("repeat backfill: %v", err)
	}
	if groups != 0 || routes != 0 {
		t.Fatalf("repeat backfill counts = groups:%d routes:%d, want 0/0", groups, routes)
	}
}

func TestAccountStoreGroupBindingReconcilesWithoutDuplicates(t *testing.T) {
	db := enttestOpen(t)
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	}()
	ctx := context.Background()
	groupStore := NewGroupStore(db)
	accountStore := NewAccountStore(db)

	g := mustCreateGroup(t, groupStore, appgroup.CreateInput{
		Name: "codex-plus", Platform: "openai", RateMultiplier: 1, StatusVisible: true, SubscriptionType: "standard",
		ModelRouting: map[string][]int64{"gpt-5.6": {}},
	})
	accA, err := accountStore.Create(ctx, appaccount.CreateInput{
		Name: "a", Platform: "openai", Type: "apikey", Credentials: map[string]string{"api_key": "sk-a"}, GroupIDs: []int64{int64(g.ID)},
	})
	if err != nil {
		t.Fatalf("create account a: %v", err)
	}
	accB, err := accountStore.Create(ctx, appaccount.CreateInput{
		Name: "b", Platform: "openai", Type: "apikey", Credentials: map[string]string{"api_key": "sk-b"},
	})
	if err != nil {
		t.Fatalf("create account b: %v", err)
	}

	bind := appaccount.UpdateInput{HasGroupIDs: true, GroupIDs: []int64{int64(g.ID)}}
	if _, err := accountStore.Update(ctx, accB.ID, bind); err != nil {
		t.Fatalf("bind account b: %v", err)
	}
	if _, err := accountStore.Update(ctx, accB.ID, bind); err != nil {
		t.Fatalf("repeat account b binding: %v", err)
	}

	after, err := groupStore.FindByID(ctx, g.ID)
	if err != nil {
		t.Fatalf("find group after binding: %v", err)
	}
	if got := after.ModelRouting["gpt-5.6"]; len(got) != 2 || got[0] != int64(accA.ID) || got[1] != int64(accB.ID) {
		t.Fatalf("route after repeated binding = %v, want [%d %d]", got, accA.ID, accB.ID)
	}

	if _, err := accountStore.Update(ctx, accB.ID, appaccount.UpdateInput{HasGroupIDs: true}); err != nil {
		t.Fatalf("unbind account b: %v", err)
	}
	after, err = groupStore.FindByID(ctx, g.ID)
	if err != nil {
		t.Fatalf("find group after unbinding: %v", err)
	}
	if got := after.ModelRouting["gpt-5.6"]; len(got) != 1 || got[0] != int64(accA.ID) {
		t.Fatalf("route after unbinding = %v, want [%d]", got, accA.ID)
	}
}

func TestAccountStoreReconcileSkipsDisabledAndCrossPlatformAccounts(t *testing.T) {
	db := enttestOpen(t)
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	}()
	ctx := context.Background()
	groupStore := NewGroupStore(db)
	accountStore := NewAccountStore(db)

	g := mustCreateGroup(t, groupStore, appgroup.CreateInput{
		Name: "codex-plus", Platform: "openai", RateMultiplier: 1, StatusVisible: true, SubscriptionType: "standard",
		ModelRouting: map[string][]int64{"gpt-5.6": {}},
	})
	disabled, err := db.Account.Create().
		SetName("disabled").
		SetPlatform("openai").
		SetType("oauth").
		SetState(entaccount.StateDisabled).
		SetCredentials(map[string]string{}).
		Save(ctx)
	if err != nil {
		t.Fatalf("create disabled account: %v", err)
	}
	otherPlatform, err := db.Account.Create().
		SetName("claude").
		SetPlatform("claude").
		SetType("oauth").
		SetCredentials(map[string]string{}).
		Save(ctx)
	if err != nil {
		t.Fatalf("create cross-platform account: %v", err)
	}

	bind := appaccount.UpdateInput{HasGroupIDs: true, GroupIDs: []int64{int64(g.ID)}}
	if _, err := accountStore.Update(ctx, disabled.ID, bind); err != nil {
		t.Fatalf("bind disabled account: %v", err)
	}
	if _, err := accountStore.Update(ctx, otherPlatform.ID, bind); err != nil {
		t.Fatalf("bind cross-platform account: %v", err)
	}
	after, err := groupStore.FindByID(ctx, g.ID)
	if err != nil {
		t.Fatalf("find group before recovery: %v", err)
	}
	if got := after.ModelRouting["gpt-5.6"]; len(got) != 0 {
		t.Fatalf("route before recovery = %v, want empty", got)
	}

	active := string(entaccount.StateActive)
	if _, err := accountStore.Update(ctx, disabled.ID, appaccount.UpdateInput{State: &active}); err != nil {
		t.Fatalf("recover disabled account: %v", err)
	}
	after, err = groupStore.FindByID(ctx, g.ID)
	if err != nil {
		t.Fatalf("find group after recovery: %v", err)
	}
	if got := after.ModelRouting["gpt-5.6"]; len(got) != 1 || got[0] != int64(disabled.ID) {
		t.Fatalf("route after recovery = %v, want only recovered account [%d]", got, disabled.ID)
	}
}

func TestAccountStoreUpdateGroupsSanitizesAffectedModelRouting(t *testing.T) {
	db := enttestOpen(t)
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	}()
	ctx := context.Background()
	groupStore := NewGroupStore(db)
	accountStore := NewAccountStore(db)

	g := mustCreateGroup(t, groupStore, appgroup.CreateInput{
		Name: "codex-plus", Platform: "openai", RateMultiplier: 1, StatusVisible: true, SubscriptionType: "standard",
	})
	accA, err := db.Account.Create().
		SetName("a").
		SetPlatform("openai").
		SetType("apikey").
		SetCredentials(map[string]string{"api_key": "sk-a"}).
		AddGroupIDs(g.ID).
		Save(ctx)
	if err != nil {
		t.Fatalf("create account a: %v", err)
	}
	accB, err := db.Account.Create().
		SetName("b").
		SetPlatform("openai").
		SetType("apikey").
		SetCredentials(map[string]string{"api_key": "sk-b"}).
		AddGroupIDs(g.ID).
		Save(ctx)
	if err != nil {
		t.Fatalf("create account b: %v", err)
	}
	if _, err := groupStore.Update(ctx, g.ID, appgroup.UpdateInput{
		ModelRouting: map[string][]int64{"gpt-5.5": {int64(accA.ID), int64(accB.ID)}},
	}); err != nil {
		t.Fatalf("seed model routing: %v", err)
	}

	if _, err := accountStore.Update(ctx, accA.ID, appaccount.UpdateInput{
		HasGroupIDs: true,
		GroupIDs:    nil,
	}); err != nil {
		t.Fatalf("remove account from group: %v", err)
	}

	after, err := groupStore.FindByID(ctx, g.ID)
	if err != nil {
		t.Fatalf("find group: %v", err)
	}
	if got := after.ModelRouting["gpt-5.5"]; len(got) != 1 || got[0] != int64(accB.ID) {
		t.Fatalf("routing after account unbind = %v, want [%d]", got, accB.ID)
	}
}

func ptr[T any](v T) *T { return &v }
