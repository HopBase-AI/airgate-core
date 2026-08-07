package auth

import (
	"context"
	"testing"

	"entgo.io/ent/dialect/sql/schema"
	_ "github.com/mattn/go-sqlite3"

	"github.com/DouDOU-start/airgate-core/ent/enttest"
	entuser "github.com/DouDOU-start/airgate-core/ent/user"
)

func TestValidateAPIKeyIncludesUserEmail(t *testing.T) {
	db := enttest.Open(t, "sqlite3", "file:apikey_validate?mode=memory&cache=shared&_fk=1", enttest.WithMigrateOptions(schema.WithGlobalUniqueID(false)))
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	}()

	ctx := context.Background()
	user, err := db.User.Create().
		SetEmail("apikey-user@example.com").
		SetPasswordHash("secret").
		Save(ctx)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	group, err := db.Group.Create().
		SetName("OpenAI").
		SetPlatform("openai").
		Save(ctx)
	if err != nil {
		t.Fatalf("create group: %v", err)
	}

	key, hash, err := GenerateAPIKey()
	if err != nil {
		t.Fatalf("GenerateAPIKey: %v", err)
	}
	if _, err := db.APIKey.Create().
		SetName("key").
		SetKeyHash(hash).
		SetUser(user).
		SetGroup(group).
		Save(ctx); err != nil {
		t.Fatalf("create api key: %v", err)
	}

	info, err := ValidateAPIKey(ctx, db, key)
	if err != nil {
		t.Fatalf("ValidateAPIKey returned error: %v", err)
	}
	if info.UserID != user.ID || info.UserEmail != user.Email {
		t.Fatalf("ValidateAPIKey user info = (%d, %q), want (%d, %q)", info.UserID, info.UserEmail, user.ID, user.Email)
	}
}

func TestValidateAPIKeyKeepsExistingKeyUsableAfterGroupDelisted(t *testing.T) {
	db := enttest.Open(t, "sqlite3", "file:apikey_delisted_group?mode=memory&cache=shared&_fk=1", enttest.WithMigrateOptions(schema.WithGlobalUniqueID(false)))
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	}()
	InvalidateAPIKeyCache("")

	ctx := context.Background()
	user, err := db.User.Create().
		SetEmail("delisted-key-user@example.com").
		SetPasswordHash("secret").
		Save(ctx)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	group, err := db.Group.Create().
		SetName("delisted group").
		SetPlatform("openai").
		SetDelisted(true).
		Save(ctx)
	if err != nil {
		t.Fatalf("create delisted group: %v", err)
	}

	key, hash, err := GenerateAPIKey()
	if err != nil {
		t.Fatalf("GenerateAPIKey: %v", err)
	}
	if _, err := db.APIKey.Create().
		SetName("legacy-key").
		SetKeyHash(hash).
		SetUser(user).
		SetGroup(group).
		Save(ctx); err != nil {
		t.Fatalf("create api key: %v", err)
	}

	info, err := ValidateAPIKey(ctx, db, key)
	if err != nil {
		t.Fatalf("ValidateAPIKey returned error: %v", err)
	}
	if info.GroupID != group.ID || info.GroupPlatform != group.Platform {
		t.Fatalf("validated group = (%d, %q), want (%d, %q)", info.GroupID, info.GroupPlatform, group.ID, group.Platform)
	}
}

func TestValidateAPIKeyRejectsDisabledUser(t *testing.T) {
	db := enttest.Open(t, "sqlite3", "file:apikey_disabled_user?mode=memory&cache=shared&_fk=1", enttest.WithMigrateOptions(schema.WithGlobalUniqueID(false)))
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	}()
	// 缓存是包级全局状态，清掉避免跨用例污染
	InvalidateAPIKeyCache("")

	ctx := context.Background()
	user, err := db.User.Create().
		SetEmail("disabled-user@example.com").
		SetPasswordHash("secret").
		SetStatus(entuser.StatusDisabled).
		Save(ctx)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	group, err := db.Group.Create().
		SetName("OpenAI").
		SetPlatform("openai").
		Save(ctx)
	if err != nil {
		t.Fatalf("create group: %v", err)
	}

	key, hash, err := GenerateAPIKey()
	if err != nil {
		t.Fatalf("GenerateAPIKey: %v", err)
	}
	if _, err := db.APIKey.Create().
		SetName("key").
		SetKeyHash(hash).
		SetUser(user).
		SetGroup(group).
		Save(ctx); err != nil {
		t.Fatalf("create api key: %v", err)
	}

	// 用户被禁用：即便 API Key 本身 active，转发鉴权也必须拒绝。
	if _, err := ValidateAPIKey(ctx, db, key); err != ErrUserDisabled {
		t.Fatalf("ValidateAPIKey error = %v, want ErrUserDisabled", err)
	}
}
