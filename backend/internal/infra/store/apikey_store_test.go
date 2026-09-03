package store

import (
	"context"
	"reflect"
	"sort"
	"testing"
	"time"

	entapikey "github.com/DouDOU-start/airgate-core/ent/apikey"
	appapikey "github.com/DouDOU-start/airgate-core/internal/app/apikey"
)

// TestAPIKeyStoreListAdminSearchScope 验证 search_scope 控制是否按用户邮箱模糊匹配。
//
// 业务背景：管理员通用搜索想同时支持 name/key_hint/user_email；但
// "Usage 页面通过 API Key 选择器搜索"这一场景里，邮箱模糊匹配会带回大量
// 同邮箱所属的其它 Key，造成噪音。前端在该场景下传 search_scope=api_key
// 让 store 跳过邮箱谓词。
func TestAPIKeyStoreListAdminSearchScope(t *testing.T) {
	db := enttestOpen(t)
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	}()
	ctx := context.Background()

	user := createTestUser(t, db, "scope-target@example.com")
	if _, err := db.APIKey.Create().
		SetName("billing-runner").
		SetKeyHint("sk-bill-001").
		SetKeyHash("hash-1").
		SetUserID(user.ID).
		Save(ctx); err != nil {
		t.Fatalf("create api key: %v", err)
	}

	store := NewAPIKeyStore(db)

	t.Run("default scope matches user email", func(t *testing.T) {
		_, total, err := store.ListAdmin(ctx, appapikey.ListFilter{
			Page: 1, PageSize: 20, Keyword: "scope-target",
		})
		if err != nil {
			t.Fatalf("ListAdmin returned error: %v", err)
		}
		if total != 1 {
			t.Fatalf("default scope total = %d, want 1 (email predicate must apply)", total)
		}
	})

	t.Run("api_key scope skips user email predicate", func(t *testing.T) {
		_, total, err := store.ListAdmin(ctx, appapikey.ListFilter{
			Page: 1, PageSize: 20, Keyword: "scope-target",
			SearchScope: appapikey.SearchScopeAPIKey,
		})
		if err != nil {
			t.Fatalf("ListAdmin returned error: %v", err)
		}
		if total != 0 {
			t.Fatalf("api_key scope total = %d, want 0 (email predicate must be skipped)", total)
		}
	})

	t.Run("api_key scope still matches name", func(t *testing.T) {
		_, total, err := store.ListAdmin(ctx, appapikey.ListFilter{
			Page: 1, PageSize: 20, Keyword: "billing",
			SearchScope: appapikey.SearchScopeAPIKey,
		})
		if err != nil {
			t.Fatalf("ListAdmin returned error: %v", err)
		}
		if total != 1 {
			t.Fatalf("api_key scope name match total = %d, want 1", total)
		}
	})

	t.Run("api_key scope still matches key_hint", func(t *testing.T) {
		_, total, err := store.ListAdmin(ctx, appapikey.ListFilter{
			Page: 1, PageSize: 20, Keyword: "sk-bill",
			SearchScope: appapikey.SearchScopeAPIKey,
		})
		if err != nil {
			t.Fatalf("ListAdmin returned error: %v", err)
		}
		if total != 1 {
			t.Fatalf("api_key scope key_hint match total = %d, want 1", total)
		}
	})
}

func TestAPIKeyStoreGetGroupAccessRejectsDelistedGroup(t *testing.T) {
	db := enttestOpen(t)
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	}()
	ctx := context.Background()
	user := createTestUser(t, db, "delisted-key-user@example.com")
	normal := db.Group.Create().SetName("normal").SetPlatform("openai").SaveX(ctx)
	delisted := db.Group.Create().SetName("delisted").SetPlatform("openai").SetDelisted(true).SaveX(ctx)
	store := NewAPIKeyStore(db)

	access, err := store.GetGroupAccess(ctx, user.ID, normal.ID)
	if err != nil {
		t.Fatalf("normal group access: %v", err)
	}
	if !access.Exists || !access.Allowed {
		t.Fatalf("normal group access = %+v, want exists and allowed", access)
	}

	access, err = store.GetGroupAccess(ctx, user.ID, delisted.ID)
	if err != nil {
		t.Fatalf("delisted group access: %v", err)
	}
	if !access.Exists || access.Allowed {
		t.Fatalf("delisted group access = %+v, want exists but forbidden", access)
	}

	access, err = store.GetGroupAccess(ctx, user.ID, 999999)
	if err != nil {
		t.Fatalf("missing group access: %v", err)
	}
	if access.Exists || access.Allowed {
		t.Fatalf("missing group access = %+v, want not exists", access)
	}
}

// TestAPIKeyStoreListByUserFilters 验证用户侧密钥列表的成员/分组/状态筛选。
//
// 状态口径与控制台表格一致:过期优先于启用/停用——过期的启用 key 不算 active,
// 停用且过期的 key 归 expired。
func TestAPIKeyStoreListByUserFilters(t *testing.T) {
	db := enttestOpen(t)
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	}()
	ctx := context.Background()

	owner := createTestUser(t, db, "keys-filter@example.com")
	other := createTestUser(t, db, "keys-filter-other@example.com")
	groupA, err := db.Group.Create().SetName("A").SetPlatform("openai").Save(ctx)
	if err != nil {
		t.Fatalf("create group A: %v", err)
	}
	groupB, err := db.Group.Create().SetName("B").SetPlatform("claude").Save(ctx)
	if err != nil {
		t.Fatalf("create group B: %v", err)
	}
	member, err := db.Member.Create().SetName("张三").SetOwnerID(owner.ID).Save(ctx)
	if err != nil {
		t.Fatalf("create member: %v", err)
	}

	past := time.Now().Add(-time.Hour)
	future := time.Now().Add(24 * time.Hour)
	mk := func(name string, groupID int, memberID *int, status entapikey.Status, expiresAt *time.Time) {
		t.Helper()
		builder := db.APIKey.Create().
			SetName(name).
			SetKeyHash("hash-" + name).
			SetKeyHint("sk-" + name).
			SetUserID(owner.ID).
			SetGroupID(groupID).
			SetStatus(status)
		if memberID != nil {
			builder = builder.SetMemberID(*memberID)
		}
		if expiresAt != nil {
			builder = builder.SetExpiresAt(*expiresAt)
		}
		if _, err := builder.Save(ctx); err != nil {
			t.Fatalf("create key %s: %v", name, err)
		}
	}
	mk("member-active", groupA.ID, &member.ID, entapikey.StatusActive, nil)
	mk("member-expired", groupA.ID, &member.ID, entapikey.StatusActive, &past)
	mk("solo-active", groupB.ID, nil, entapikey.StatusActive, &future)
	mk("solo-disabled", groupB.ID, nil, entapikey.StatusDisabled, nil)
	mk("solo-disabled-expired", groupB.ID, nil, entapikey.StatusDisabled, &past)
	// 他人的 key:任何筛选都不该带出来
	if _, err := db.APIKey.Create().SetName("stranger").SetKeyHash("hash-stranger").SetUserID(other.ID).SetGroupID(groupA.ID).Save(ctx); err != nil {
		t.Fatalf("create stranger key: %v", err)
	}

	store := NewAPIKeyStore(db)
	names := func(filter appapikey.ListFilter) []string {
		t.Helper()
		filter.Page, filter.PageSize = 1, 50
		list, total, err := store.ListByUser(ctx, owner.ID, filter)
		if err != nil {
			t.Fatalf("ListByUser: %v", err)
		}
		if int(total) != len(list) {
			t.Fatalf("total %d != len(list) %d", total, len(list))
		}
		out := make([]string, 0, len(list))
		for _, item := range list {
			out = append(out, item.Name)
		}
		sort.Strings(out)
		return out
	}

	cases := []struct {
		name   string
		filter appapikey.ListFilter
		want   []string
	}{
		{"无筛选只看自己的", appapikey.ListFilter{}, []string{"member-active", "member-expired", "solo-active", "solo-disabled", "solo-disabled-expired"}},
		{"按成员", appapikey.ListFilter{MemberID: &member.ID}, []string{"member-active", "member-expired"}},
		{"未归属成员", appapikey.ListFilter{MemberUnassigned: true}, []string{"solo-active", "solo-disabled", "solo-disabled-expired"}},
		{"按分组", appapikey.ListFilter{GroupID: &groupB.ID}, []string{"solo-active", "solo-disabled", "solo-disabled-expired"}},
		{"状态启用排除已过期", appapikey.ListFilter{Status: appapikey.StatusFilterActive}, []string{"member-active", "solo-active"}},
		{"状态停用排除已过期", appapikey.ListFilter{Status: appapikey.StatusFilterDisabled}, []string{"solo-disabled"}},
		{"状态已过期不看启停", appapikey.ListFilter{Status: appapikey.StatusFilterExpired}, []string{"member-expired", "solo-disabled-expired"}},
		{"成员与分组叠加", appapikey.ListFilter{MemberID: &member.ID, GroupID: &groupA.ID}, []string{"member-active", "member-expired"}},
		{"成员与状态叠加", appapikey.ListFilter{MemberID: &member.ID, Status: appapikey.StatusFilterActive}, []string{"member-active"}},
		{"member_id 优先于 unassigned", appapikey.ListFilter{MemberID: &member.ID, MemberUnassigned: true}, []string{"member-active", "member-expired"}},
		{"关键词叠加筛选", appapikey.ListFilter{Keyword: "solo", Status: appapikey.StatusFilterActive}, []string{"solo-active"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := names(tc.filter)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}
