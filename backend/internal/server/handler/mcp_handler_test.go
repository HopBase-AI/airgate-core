package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"entgo.io/ent/dialect/sql/schema"
	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"

	"github.com/DouDOU-start/airgate-core/ent"
	"github.com/DouDOU-start/airgate-core/ent/enttest"
	"github.com/DouDOU-start/airgate-core/internal/auth"
)

var mcpTestUserSeq int64

// 内存 SQLite 必须单连接打开，见根 CLAUDE.md「已知环境坑」。
func openMCPTestDB(t *testing.T) *ent.Client {
	t.Helper()
	db := enttest.Open(t, "sqlite3", "file:mcp_handler?mode=memory&cache=shared&_fk=1", enttest.WithMigrateOptions(schema.WithGlobalUniqueID(false)))
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	})
	return db
}

func createMCPTestKey(t *testing.T, ctx context.Context, db *ent.Client, userBalance, quotaUSD, usedQuota float64) string {
	t.Helper()
	seq := atomic.AddInt64(&mcpTestUserSeq, 1)
	user, err := db.User.Create().
		SetEmail(fmt.Sprintf("mcp-%d@example.com", seq)).
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
		SetName("mcp-test").
		SetKeyHash(hash).
		SetQuotaUsd(quotaUSD).
		SetUsedQuota(usedQuota).
		SetUser(user).
		Save(ctx); err != nil {
		t.Fatalf("create api key: %v", err)
	}
	return key
}

func requestMCP(t *testing.T, db *ent.Client, key string, payload string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	h := NewMCPHandler(db, nil, nil)
	router.POST("/mcp", h.Handle)

	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewBufferString(payload))
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func decodeMCPBody(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v (%s)", err, w.Body.String())
	}
	return body
}

// mcpToolResultText 从 tools/call 响应里取出 content[0].text 并解析为 JSON。
func mcpToolResultText(t *testing.T, body map[string]any) map[string]any {
	t.Helper()
	result, ok := body["result"].(map[string]any)
	if !ok {
		t.Fatalf("no result in %v", body)
	}
	if result["isError"] == true {
		t.Fatalf("tool returned isError: %v", result)
	}
	content := result["content"].([]any)
	text := content[0].(map[string]any)["text"].(string)
	var payload map[string]any
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		t.Fatalf("parse tool text: %v (%s)", err, text)
	}
	return payload
}

func TestMCPRequiresAPIKey(t *testing.T) {
	db := openMCPTestDB(t)
	w := requestMCP(t, db, "", `{"jsonrpc":"2.0","id":1,"method":"initialize"}`)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
	body := decodeMCPBody(t, w)
	errObj := body["error"].(map[string]any)
	if errObj["code"] != "missing_api_key" {
		t.Fatalf("error.code = %v, want missing_api_key", errObj["code"])
	}
}

func TestMCPRejectsUnknownKey(t *testing.T) {
	db := openMCPTestDB(t)
	w := requestMCP(t, db, "sk-not-a-real-key", `{"jsonrpc":"2.0","id":1,"method":"ping"}`)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}

func TestMCPInitializeNegotiatesProtocolVersion(t *testing.T) {
	db := openMCPTestDB(t)
	ctx := context.Background()
	key := createMCPTestKey(t, ctx, db, 10, 0, 0)

	tests := []struct {
		requested string
		want      string
	}{
		{"2025-03-26", "2025-03-26"},
		{"2025-06-18", "2025-06-18"},
		{"1999-01-01", mcpLatestProtocolVersion},
		{"", mcpLatestProtocolVersion},
	}
	for _, tt := range tests {
		payload := fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":%q}}`, tt.requested)
		w := requestMCP(t, db, key, payload)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", w.Code)
		}
		body := decodeMCPBody(t, w)
		result := body["result"].(map[string]any)
		if result["protocolVersion"] != tt.want {
			t.Fatalf("protocolVersion(%q) = %v, want %v", tt.requested, result["protocolVersion"], tt.want)
		}
		if _, ok := result["serverInfo"].(map[string]any); !ok {
			t.Fatalf("serverInfo missing in %v", result)
		}
	}
}

func TestMCPNotificationReturns202(t *testing.T) {
	db := openMCPTestDB(t)
	ctx := context.Background()
	key := createMCPTestKey(t, ctx, db, 10, 0, 0)

	w := requestMCP(t, db, key, `{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	if w.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", w.Code)
	}
}

func TestMCPBatchRejected(t *testing.T) {
	db := openMCPTestDB(t)
	ctx := context.Background()
	key := createMCPTestKey(t, ctx, db, 10, 0, 0)

	w := requestMCP(t, db, key, `[{"jsonrpc":"2.0","id":1,"method":"ping"}]`)
	body := decodeMCPBody(t, w)
	errObj := body["error"].(map[string]any)
	if errObj["code"].(float64) != mcpErrInvalidRequest {
		t.Fatalf("error.code = %v, want %d", errObj["code"], mcpErrInvalidRequest)
	}
}

func TestMCPUnknownMethod(t *testing.T) {
	db := openMCPTestDB(t)
	ctx := context.Background()
	key := createMCPTestKey(t, ctx, db, 10, 0, 0)

	w := requestMCP(t, db, key, `{"jsonrpc":"2.0","id":7,"method":"resources/list"}`)
	body := decodeMCPBody(t, w)
	errObj := body["error"].(map[string]any)
	if errObj["code"].(float64) != mcpErrMethodNotFound {
		t.Fatalf("error.code = %v, want %d", errObj["code"], mcpErrMethodNotFound)
	}
}

func TestMCPToolsListExposesFourReadOnlyTools(t *testing.T) {
	db := openMCPTestDB(t)
	ctx := context.Background()
	key := createMCPTestKey(t, ctx, db, 10, 0, 0)

	w := requestMCP(t, db, key, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	body := decodeMCPBody(t, w)
	tools := body["result"].(map[string]any)["tools"].([]any)
	if len(tools) != 4 {
		t.Fatalf("len(tools) = %d, want 4", len(tools))
	}
	names := map[string]bool{}
	for _, tool := range tools {
		names[tool.(map[string]any)["name"].(string)] = true
	}
	for _, want := range []string{"get_balance", "get_key_info", "list_models", "get_usage"} {
		if !names[want] {
			t.Fatalf("tool %q missing in %v", want, names)
		}
	}
}

func TestMCPGetBalanceCapsAvailableByKeyQuota(t *testing.T) {
	db := openMCPTestDB(t)
	ctx := context.Background()
	key := createMCPTestKey(t, ctx, db, 50, 10, 3)

	w := requestMCP(t, db, key, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"get_balance"}}`)
	payload := mcpToolResultText(t, decodeMCPBody(t, w))
	if payload["balance_usd"].(float64) != 50 {
		t.Fatalf("balance_usd = %v, want 50", payload["balance_usd"])
	}
	if payload["available_usd"].(float64) != 7 {
		t.Fatalf("available_usd = %v, want 7 (quota 10 - used 3)", payload["available_usd"])
	}
	quota := payload["key_quota_usd"].(map[string]any)
	if quota["unlimited"] != false || quota["remaining"].(float64) != 7 {
		t.Fatalf("key_quota_usd = %v, want remaining 7 / unlimited false", quota)
	}
}

func TestMCPGetBalanceUnlimitedKeyUsesUserBalance(t *testing.T) {
	db := openMCPTestDB(t)
	ctx := context.Background()
	key := createMCPTestKey(t, ctx, db, 12.5, 0, 0)

	w := requestMCP(t, db, key, `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"get_balance"}}`)
	payload := mcpToolResultText(t, decodeMCPBody(t, w))
	if payload["available_usd"].(float64) != 12.5 {
		t.Fatalf("available_usd = %v, want 12.5", payload["available_usd"])
	}
	quota := payload["key_quota_usd"].(map[string]any)
	if quota["unlimited"] != true {
		t.Fatalf("unlimited = %v, want true", quota["unlimited"])
	}
}

func TestMCPGetKeyInfo(t *testing.T) {
	db := openMCPTestDB(t)
	ctx := context.Background()
	key := createMCPTestKey(t, ctx, db, 10, 20, 5)

	w := requestMCP(t, db, key, `{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"get_key_info"}}`)
	payload := mcpToolResultText(t, decodeMCPBody(t, w))
	if payload["name"] != "mcp-test" {
		t.Fatalf("name = %v, want mcp-test", payload["name"])
	}
	if payload["quota_usd"].(float64) != 20 || payload["used_quota_usd"].(float64) != 5 {
		t.Fatalf("quota fields = %v/%v, want 20/5", payload["quota_usd"], payload["used_quota_usd"])
	}
	if payload["status"] != "active" {
		t.Fatalf("status = %v, want active", payload["status"])
	}
}

func TestMCPUnknownToolReturnsInvalidParams(t *testing.T) {
	db := openMCPTestDB(t)
	ctx := context.Background()
	key := createMCPTestKey(t, ctx, db, 10, 0, 0)

	w := requestMCP(t, db, key, `{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"drop_tables"}}`)
	body := decodeMCPBody(t, w)
	errObj := body["error"].(map[string]any)
	if errObj["code"].(float64) != mcpErrInvalidParams {
		t.Fatalf("error.code = %v, want %d", errObj["code"], mcpErrInvalidParams)
	}
}
