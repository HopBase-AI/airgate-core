package store

import (
	"context"
	"testing"

	_ "github.com/mattn/go-sqlite3"

	"github.com/DouDOU-start/airgate-core/ent"
	entaccount "github.com/DouDOU-start/airgate-core/ent/account"
	"github.com/DouDOU-start/airgate-core/ent/enttest"
	"github.com/DouDOU-start/airgate-core/ent/migrate"
	"github.com/DouDOU-start/airgate-core/internal/app/account"
)

func TestAccountStoreKeywordSearchMatchesOAuthEmail(t *testing.T) {
	db := enttestOpen(t)
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	}()

	ctx := context.Background()
	if _, err := db.Account.Create().
		SetName("Claude Key").
		SetPlatform("openai").
		SetType("oauth").
		SetCredentials(map[string]string{"email": "claude@example.com", "access_token": "token"}).
		Save(ctx); err != nil {
		t.Fatalf("create oauth account: %v", err)
	}
	if _, err := db.Account.Create().
		SetName("Other Key").
		SetPlatform("openai").
		SetType("apikey").
		SetCredentials(map[string]string{"api_key": "sk-test"}).
		Save(ctx); err != nil {
		t.Fatalf("create api key account: %v", err)
	}

	store := NewAccountStore(db)
	items, total, err := store.List(ctx, account.ListFilter{Page: 1, PageSize: 20, Keyword: "claude@"})
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if total != 1 {
		t.Fatalf("total = %d, want 1", total)
	}
	if len(items) != 1 || items[0].Name != "Claude Key" {
		t.Fatalf("items = %+v", items)
	}
}

func TestAccountStoreCredentialStringFilterMatchesPluginDeclaredPlan(t *testing.T) {
	db := enttestOpen(t)
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	}()

	ctx := context.Background()
	cases := []struct {
		name        string
		platform    string
		accountType string
		credentials map[string]string
	}{
		{name: "OpenAI OAuth Free", platform: "openai", accountType: "oauth", credentials: map[string]string{"plan_type": "free"}},
		{name: "Claude OAuth Plus", platform: "claude", accountType: "oauth", credentials: map[string]string{"plan_type": "Claude Plus"}},
		{name: "Kiro OAuth Pro", platform: "kiro", accountType: "oauth", credentials: map[string]string{"plan_type": "Builder Id Pro"}},
		{name: "Claude OAuth Unknown", platform: "claude", accountType: "oauth", credentials: map[string]string{}},
		{name: "Kiro API Key", platform: "kiro", accountType: "apikey", credentials: map[string]string{"plan_type": "Builder Id Plus"}},
	}
	for _, item := range cases {
		if _, err := db.Account.Create().
			SetName(item.name).
			SetPlatform(item.platform).
			SetType(item.accountType).
			SetCredentials(item.credentials).
			Save(ctx); err != nil {
			t.Fatalf("create account %q: %v", item.name, err)
		}
	}

	store := NewAccountStore(db)
	items, total, err := store.List(ctx, account.ListFilter{
		Page:     1,
		PageSize: 20,
		Credential: &account.CredentialStringFilter{
			Platform:    "claude",
			AccountType: "oauth",
			Key:         "plan_type",
			Values:      []string{"Claude Plus"},
			MatchMode:   "exact",
		},
	})
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if total != 1 || len(items) != 1 || items[0].Name != "Claude OAuth Plus" || items[0].Platform != "claude" {
		t.Fatalf("exact credential filter items = %+v total = %d, want only Claude OAuth Plus", items, total)
	}

	items, total, err = store.List(ctx, account.ListFilter{
		Page:     1,
		PageSize: 20,
		Platform: "kiro",
		Credential: &account.CredentialStringFilter{
			Platform:    "kiro",
			AccountType: "oauth",
			Key:         "plan_type",
			Values:      []string{"Pro"},
			MatchMode:   "contains",
		},
	})
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if total != 1 || len(items) != 1 || items[0].Name != "Kiro OAuth Pro" {
		t.Fatalf("contains credential filter items = %+v total = %d, want only Kiro OAuth Pro", items, total)
	}

	items, total, err = store.List(ctx, account.ListFilter{
		Page:     1,
		PageSize: 20,
		Platform: "openai",
		Credential: &account.CredentialStringFilter{
			Platform:    "claude",
			AccountType: "oauth",
			Key:         "plan_type",
			Values:      []string{"Claude Plus"},
			MatchMode:   "exact",
		},
	})
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if total != 0 || len(items) != 0 {
		t.Fatalf("conflicting platform filter items = %+v total = %d, want no matches", items, total)
	}

	items, total, err = store.List(ctx, account.ListFilter{Page: 1, PageSize: 20, AccountType: "oauth"})
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if total != 4 || len(items) != 4 {
		t.Fatalf("oauth total = %d len = %d, want all four OAuth accounts", total, len(items))
	}

	all, err := store.ListAll(ctx, account.ListFilter{
		Credential: &account.CredentialStringFilter{
			Platform:    "openai",
			AccountType: "oauth",
			Key:         "plan_type",
			Values:      []string{"free"},
			MatchMode:   "exact",
		},
	})
	if err != nil {
		t.Fatalf("ListAll returned error: %v", err)
	}
	if len(all) != 1 || all[0].Name != "OpenAI OAuth Free" {
		t.Fatalf("ListAll credential filter items = %+v, want only OpenAI OAuth Free", all)
	}
}

func TestAccountStoreStateFilterErrorMatchesDisabledWithErrorMsg(t *testing.T) {
	db := enttestOpen(t)
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	}()

	ctx := context.Background()
	cases := []struct {
		name     string
		state    entaccount.State
		errorMsg string
	}{
		{name: "活跃账号", state: entaccount.StateActive},
		{name: "异常账号", state: entaccount.StateDisabled, errorMsg: "OAuth token 刷新失败"},
		{name: "手动禁用无原因", state: entaccount.StateDisabled},
	}
	for _, item := range cases {
		builder := db.Account.Create().
			SetName(item.name).
			SetPlatform("claude").
			SetType("oauth").
			SetCredentials(map[string]string{}).
			SetState(item.state)
		if item.errorMsg != "" {
			builder = builder.SetErrorMsg(item.errorMsg)
		}
		if _, err := builder.Save(ctx); err != nil {
			t.Fatalf("create account %q: %v", item.name, err)
		}
	}

	store := NewAccountStore(db)

	// 伪状态 error：只命中 disabled 且 error_msg 非空的账号。
	items, total, err := store.List(ctx, account.ListFilter{Page: 1, PageSize: 20, State: account.StateFilterError})
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if total != 1 || len(items) != 1 || items[0].Name != "异常账号" {
		t.Fatalf("state=error items = %+v total = %d, want only 异常账号", items, total)
	}

	// 普通 disabled：两个禁用账号都命中。
	items, total, err = store.List(ctx, account.ListFilter{Page: 1, PageSize: 20, State: "disabled"})
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if total != 2 || len(items) != 2 {
		t.Fatalf("state=disabled total = %d len = %d, want both disabled accounts", total, len(items))
	}
}

func enttestOpen(t *testing.T) *ent.Client {
	t.Helper()
	return enttest.Open(t, "sqlite3", "file:account_store?mode=memory&cache=shared&_fk=1", enttest.WithMigrateOptions(migrate.WithGlobalUniqueID(false)))
}
