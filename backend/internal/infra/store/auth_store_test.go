package store

import (
	"errors"
	"testing"

	"entgo.io/ent/dialect/sql/schema"
	_ "github.com/mattn/go-sqlite3"

	"github.com/DouDOU-start/airgate-core/ent/enttest"
	appauth "github.com/DouDOU-start/airgate-core/internal/app/auth"
)

func TestValidateAPIKeyForLoginDistinguishesMissingKeyFromStoreFailure(t *testing.T) {
	t.Run("missing key", func(t *testing.T) {
		db := enttest.Open(t, "sqlite3", "file:auth_store_missing?mode=memory&cache=shared&_fk=1",
			enttest.WithMigrateOptions(schema.WithGlobalUniqueID(false)))
		_, err := NewAuthStore(db).ValidateAPIKeyForLogin(t.Context(), "sk-missing")
		if !errors.Is(err, appauth.ErrInvalidAPIKey) {
			t.Fatalf("error = %v, want ErrInvalidAPIKey", err)
		}
	})

	t.Run("store failure", func(t *testing.T) {
		db := enttest.Open(t, "sqlite3", "file:auth_store_failure?mode=memory&cache=shared&_fk=1",
			enttest.WithMigrateOptions(schema.WithGlobalUniqueID(false)))
		if err := db.Close(); err != nil {
			t.Fatalf("close test database: %v", err)
		}
		_, err := NewAuthStore(db).ValidateAPIKeyForLogin(t.Context(), "sk-any")
		if err == nil || errors.Is(err, appauth.ErrInvalidAPIKey) {
			t.Fatalf("store failure must remain retryable, got %v", err)
		}
	})
}
