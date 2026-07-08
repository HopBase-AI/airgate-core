package billing

import (
	"context"
	"testing"

	"entgo.io/ent/dialect/sql/schema"
	_ "github.com/mattn/go-sqlite3"

	"github.com/DouDOU-start/airgate-core/ent"
	"github.com/DouDOU-start/airgate-core/ent/enttest"
)

func TestRecordSyncPersistsUserEmailSnapshot(t *testing.T) {
	db := enttest.Open(t, "sqlite3", "file:billing_recorder?mode=memory&cache=shared&_fk=1", enttest.WithMigrateOptions(schema.WithGlobalUniqueID(false)))
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	}()

	ctx := context.Background()
	user := createBillingTestUser(t, ctx, db, "billing-snapshot@example.com")
	group, err := db.Group.Create().
		SetName("OpenAI").
		SetPlatform("openai").
		Save(ctx)
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	account, err := db.Account.Create().
		SetName("acc").
		SetPlatform("openai").
		Save(ctx)
	if err != nil {
		t.Fatalf("create account: %v", err)
	}

	recorder := NewRecorder(db, 0)
	usageID, err := recorder.RecordSync(ctx, UsageRecord{
		UserID:    user.ID,
		UserEmail: user.Email,
		AccountID: account.ID,
		GroupID:   group.ID,
		Platform:  "openai",
		Model:     "gpt-5",
	})
	if err != nil {
		t.Fatalf("RecordSync returned error: %v", err)
	}

	log, err := db.UsageLog.Get(ctx, usageID)
	if err != nil {
		t.Fatalf("get usage log: %v", err)
	}
	if log.UserIDSnapshot != user.ID || log.UserEmailSnapshot != user.Email {
		t.Fatalf("usage snapshot = (%d, %q), want (%d, %q)", log.UserIDSnapshot, log.UserEmailSnapshot, user.ID, user.Email)
	}
}

func TestRecordSyncKeepsUsageWhenAPIKeyWasDeletedBeforeFlush(t *testing.T) {
	db := enttest.Open(t, "sqlite3", "file:billing_deleted_key?mode=memory&cache=shared&_fk=1", enttest.WithMigrateOptions(schema.WithGlobalUniqueID(false)))
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	}()

	ctx := context.Background()
	user := createBillingTestUser(t, ctx, db, "billing-deleted-key@example.com")
	group, err := db.Group.Create().
		SetName("Claude").
		SetPlatform("claude").
		Save(ctx)
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	account, err := db.Account.Create().
		SetName("acc").
		SetPlatform("claude").
		Save(ctx)
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	key, err := db.APIKey.Create().
		SetName("temp").
		SetKeyHash("hash").
		SetUserID(user.ID).
		SetGroupID(group.ID).
		Save(ctx)
	if err != nil {
		t.Fatalf("create api key: %v", err)
	}
	if err := db.APIKey.DeleteOneID(key.ID).Exec(ctx); err != nil {
		t.Fatalf("delete api key: %v", err)
	}

	recorder := NewRecorder(db, 0)
	usageID, err := recorder.RecordSync(ctx, UsageRecord{
		UserID:      user.ID,
		UserEmail:   user.Email,
		APIKeyID:    key.ID,
		AccountID:   account.ID,
		GroupID:     group.ID,
		Platform:    "claude",
		Model:       "claude-haiku",
		ActualCost:  1.25,
		BilledCost:  1.5,
		InputTokens: 12,
	})
	if err != nil {
		t.Fatalf("RecordSync returned error: %v", err)
	}

	log, err := db.UsageLog.Get(ctx, usageID)
	if err != nil {
		t.Fatalf("get usage log: %v", err)
	}
	if log.UserIDSnapshot != user.ID || log.UserEmailSnapshot != user.Email {
		t.Fatalf("usage snapshot = (%d, %q), want (%d, %q)", log.UserIDSnapshot, log.UserEmailSnapshot, user.ID, user.Email)
	}
	if exists, err := log.QueryAPIKey().Exist(ctx); err != nil {
		t.Fatalf("query usage api key: %v", err)
	} else if exists {
		t.Fatalf("deleted api key should not be attached to usage log")
	}
	if exists, err := log.QueryUser().Exist(ctx); err != nil {
		t.Fatalf("query usage user: %v", err)
	} else if !exists {
		t.Fatalf("existing user should remain attached to usage log")
	}
	updatedUser, err := db.User.Get(ctx, user.ID)
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	if got, want := updatedUser.Balance, -1.25; got != want {
		t.Fatalf("user balance = %.2f, want %.2f", got, want)
	}
}

func createBillingTestUser(t *testing.T, ctx context.Context, db *ent.Client, email string) *ent.User {
	t.Helper()
	user, err := db.User.Create().
		SetEmail(email).
		SetPasswordHash("secret").
		Save(ctx)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	return user
}
