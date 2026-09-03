package server

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"entgo.io/ent/dialect/sql/schema"
	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"

	"github.com/DouDOU-start/airgate-core/ent"
	"github.com/DouDOU-start/airgate-core/ent/apikey"
	"github.com/DouDOU-start/airgate-core/ent/enttest"
	entmember "github.com/DouDOU-start/airgate-core/ent/member"
	"github.com/DouDOU-start/airgate-core/internal/auth"
)

var ccCompatTestUserSeq int64

func TestCCCompatUserBalanceUsesUserBalanceForUnlimitedKey(t *testing.T) {
	db := openCCCompatTestDB(t)
	ctx := context.Background()
	key := createCCCompatTestKey(t, ctx, db, 12.34, 0, 99)

	resp := requestCCCompatBalance(t, db, key)
	requireStatus(t, resp, http.StatusOK)

	body := decodeCCCompatBody(t, resp)
	requireFloat(t, body["balance"], 12.34)
	requireFloat(t, body["remaining"], 12.34)
	if body["is_active"] != true {
		t.Fatalf("is_active = %v, want true", body["is_active"])
	}

	quota := body["quota"].(map[string]any)
	requireFloat(t, quota["remaining"], 12.34)
	if quota["unlimited"] != true {
		t.Fatalf("quota.unlimited = %v, want true", quota["unlimited"])
	}
}

func TestCCCompatUserBalanceUsesSmallerQuotaOrUserBalance(t *testing.T) {
	db := openCCCompatTestDB(t)
	ctx := context.Background()

	t.Run("quota remaining caps balance", func(t *testing.T) {
		key := createCCCompatTestKey(t, ctx, db, 50, 10, 3)
		resp := requestCCCompatBalance(t, db, key)
		requireStatus(t, resp, http.StatusOK)

		body := decodeCCCompatBody(t, resp)
		requireFloat(t, body["balance"], 7)
		requireFloat(t, body["remaining"], 7)

		quota := body["quota"].(map[string]any)
		requireFloat(t, quota["remaining"], 7)
		requireFloat(t, quota["total"], 10)
		requireFloat(t, quota["used"], 3)
	})

	// 主账号余额低于 key 剩余时不再取 min：取 min 会把 reseller / 企业主账号的余额原样
	// 露给下游 key 持有者。展示口径只认持有者自己的额度，主账号余额耗尽由转发 402 兜底。
	t.Run("owner balance never leaks below key remaining", func(t *testing.T) {
		key := createCCCompatTestKey(t, ctx, db, 2, 10, 3)
		resp := requestCCCompatBalance(t, db, key)
		requireStatus(t, resp, http.StatusOK)

		body := decodeCCCompatBody(t, resp)
		requireFloat(t, body["balance"], 7)
		requireFloat(t, body["remaining"], 7)

		quota := body["quota"].(map[string]any)
		requireFloat(t, quota["remaining"], 7)
		requireFloat(t, quota["api_key_remaining"], 7)
	})
}

func TestCCCompatUserBalanceHonorsMemberQuota(t *testing.T) {
	db := openCCCompatTestDB(t)
	ctx := context.Background()

	attachMember := func(t *testing.T, rawKey string, mutate func(*ent.MemberCreate)) {
		t.Helper()
		ak, err := db.APIKey.Query().Where(apikey.KeyHash(auth.HashAPIKey(rawKey))).WithUser().Only(ctx)
		if err != nil {
			t.Fatalf("load key: %v", err)
		}
		mc := db.Member.Create().SetName("成员").SetOwner(ak.Edges.User)
		mutate(mc)
		member, err := mc.Save(ctx)
		if err != nil {
			t.Fatalf("create member: %v", err)
		}
		if err := db.APIKey.UpdateOneID(ak.ID).SetMember(member).Exec(ctx); err != nil {
			t.Fatalf("attach member: %v", err)
		}
	}

	t.Run("member remaining caps key remaining", func(t *testing.T) {
		key := createCCCompatTestKey(t, ctx, db, 100, 10, 3)                                     // key 剩 7
		attachMember(t, key, func(mc *ent.MemberCreate) { mc.SetQuotaUsd(20).SetUsedQuota(16) }) // 成员剩 4
		resp := requestCCCompatBalance(t, db, key)
		requireStatus(t, resp, http.StatusOK)
		body := decodeCCCompatBody(t, resp)
		requireFloat(t, body["balance"], 4)
		requireFloat(t, body["remaining"], 4)
		quota := body["quota"].(map[string]any)
		requireFloat(t, quota["api_key_remaining"], 7)
	})

	t.Run("disabled member is inactive", func(t *testing.T) {
		key := createCCCompatTestKey(t, ctx, db, 100, 0, 0)
		attachMember(t, key, func(mc *ent.MemberCreate) { mc.SetStatus(entmember.StatusDisabled) })
		resp := requestCCCompatBalance(t, db, key)
		requireStatus(t, resp, http.StatusOK)
		body := decodeCCCompatBody(t, resp)
		if body["is_active"] != false {
			t.Fatalf("is_active = %v, want false for disabled member", body["is_active"])
		}
	})
}

func openCCCompatTestDB(t *testing.T) *ent.Client {
	t.Helper()
	db := enttest.Open(t, "sqlite3", "file:cc_compat?mode=memory&cache=shared&_fk=1", enttest.WithMigrateOptions(schema.WithGlobalUniqueID(false)))
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	})
	return db
}

func createCCCompatTestKey(t *testing.T, ctx context.Context, db *ent.Client, userBalance, quotaUSD, usedQuota float64) string {
	t.Helper()
	seq := atomic.AddInt64(&ccCompatTestUserSeq, 1)
	user, err := db.User.Create().
		SetEmail(fmt.Sprintf("cc-compat-%d@example.com", seq)).
		SetPasswordHash("hash").
		SetBalance(userBalance).
		Save(ctx)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	key, hash, err := auth.GenerateAPIKey()
	if err != nil {
		t.Fatalf("GenerateAPIKey: %v", err)
	}
	if _, err := db.APIKey.Create().
		SetName("cc-switch").
		SetKeyHash(hash).
		SetQuotaUsd(quotaUSD).
		SetUsedQuota(usedQuota).
		SetUser(user).
		Save(ctx); err != nil {
		t.Fatalf("create api key: %v", err)
	}
	return key
}

func requestCCCompatBalance(t *testing.T, db *ent.Client, key string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	s := &Server{db: db}
	router.GET("/v1/usage", s.handleCCCompatUserBalance)

	req := httptest.NewRequest(http.MethodGet, "/v1/usage", nil)
	req.Header.Set("Authorization", "Bearer "+key)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func decodeCCCompatBody(t *testing.T, resp *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, resp.Body.String())
	}
	return body
}

func requireStatus(t *testing.T, resp *httptest.ResponseRecorder, want int) {
	t.Helper()
	if resp.Code != want {
		t.Fatalf("status = %d, want %d; body=%s", resp.Code, want, resp.Body.String())
	}
}

func requireFloat(t *testing.T, got any, want float64) {
	t.Helper()
	value, ok := got.(float64)
	if !ok {
		t.Fatalf("value = %#v (%T), want float64 %.8f", got, got, want)
	}
	if math.Abs(value-want) > 1e-9 {
		t.Fatalf("value = %.8f, want %.8f", value, want)
	}
}
