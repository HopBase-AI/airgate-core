package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"entgo.io/ent/dialect/sql/schema"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	_ "github.com/mattn/go-sqlite3"

	"github.com/DouDOU-start/airgate-core/ent/enttest"
	corauth "github.com/DouDOU-start/airgate-core/internal/auth"
)

func newAuthContext(method, target string) (*gin.Context, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(method, target, nil)
	return c, w
}

func TestExtractBearerTokenAndHasAPIKey(t *testing.T) {
	tests := []struct {
		name          string
		authorization string
		apiKey        string
		wantToken     string
		wantHasKey    bool
	}{
		{"authorization_bearer_api_key", "Bearer sk-test", "", "sk-test", true},
		{"authorization_admin_api_key", "Bearer admin-test", "", "admin-test", true},
		{"authorization_jwt_not_api_key", "bearer token-123", "", "token-123", false},
		{"authorization_trim_space_jwt_not_api_key", "Bearer   token-123  ", "", "token-123", false},
		{"x_api_key_fallback", "", "sk-from-header", "sk-from-header", true},
		{"x_api_key_when_auth_not_bearer", "Basic abc", "sk-from-header", "sk-from-header", true},
		{"missing", "", "", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := newAuthContext(http.MethodGet, "/v1/chat/completions")
			if tt.authorization != "" {
				c.Request.Header.Set("Authorization", tt.authorization)
			}
			if tt.apiKey != "" {
				c.Request.Header.Set("x-api-key", tt.apiKey)
			}

			if got := extractBearerToken(c); got != tt.wantToken {
				t.Fatalf("token = %q，期望 %q", got, tt.wantToken)
			}
			if got := HasAPIKey(c); got != tt.wantHasKey {
				t.Fatalf("HasAPIKey = %v，期望 %v", got, tt.wantHasKey)
			}
		})
	}
}

func TestJWTUserAuthResolvesAPIKeyOwnerInternallyWithoutEmailContext(t *testing.T) {
	db := enttest.Open(t, "sqlite3", "file:jwt_user_auth_api_key?mode=memory&cache=shared&_fk=1", enttest.WithMigrateOptions(schema.WithGlobalUniqueID(false)))
	t.Cleanup(func() { _ = db.Close() })
	ctx := t.Context()
	owner := db.User.Create().SetEmail("owner@example.com").SetPasswordHash("hash").SaveX(ctx)
	key := db.APIKey.Create().SetName("customer").SetKeyHash("hash").SetUser(owner).SaveX(ctx)
	jwtMgr := corauth.NewJWTManager("secret", 24)
	// 旧版 Token 携带错误的 owner/admin 身份；中间件只能信任 api_key_id，
	// 并必须从数据库恢复真实 owner，同时清空 email。
	legacyClaims := corauth.Claims{
		UserID: 999, Role: corauth.APIKeySessionRole, Email: "leaked-owner@example.com", APIKeyID: key.ID,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    "airgate",
		},
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, legacyClaims).SignedString([]byte("secret"))
	if err != nil {
		t.Fatalf("签发 API Key JWT 失败: %v", err)
	}

	router := gin.New()
	router.Use(JWTUserAuth(jwtMgr, db))
	router.GET("/me", func(c *gin.Context) {
		userID, _ := c.Get(CtxKeyUserID)
		email, _ := c.Get(CtxKeyEmail)
		c.JSON(http.StatusOK, gin.H{"user_id": userID, "email": email})
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/me", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	if body["user_id"] != float64(owner.ID) || body["email"] != "" {
		t.Fatalf("API Key 内部身份恢复异常或泄漏 email: %s", rec.Body.String())
	}
}

func TestAdminOnlyRejectsMissingOrNonAdminRole(t *testing.T) {
	tests := []struct {
		name string
		role string
	}{
		{"missing_role", ""},
		{"user_role", "user"},
		{"api_key_role", "api_key"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := gin.New()
			router.Use(func(c *gin.Context) {
				if tt.role != "" {
					c.Set(CtxKeyRole, tt.role)
				}
			})
			router.Use(AdminOnly())
			router.GET("/admin", func(c *gin.Context) {
				c.String(http.StatusOK, "ok")
			})

			w := httptest.NewRecorder()
			router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/admin", nil))

			if w.Code != http.StatusForbidden {
				t.Fatalf("状态码 = %d，期望 %d", w.Code, http.StatusForbidden)
			}
		})
	}
}

func TestRequireRoles(t *testing.T) {
	tests := []struct {
		name     string
		role     string
		wantCode int
	}{
		{"missing_role", "", http.StatusForbidden},
		{"api_key_role", "api_key", http.StatusForbidden},
		{"user_role", "user", http.StatusOK},
		{"admin_role", "admin", http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := gin.New()
			router.Use(func(c *gin.Context) {
				if tt.role != "" {
					c.Set(CtxKeyRole, tt.role)
				}
			})
			router.Use(RequireRoles("admin", "user"))
			router.GET("/account", func(c *gin.Context) {
				c.String(http.StatusOK, "ok")
			})

			w := httptest.NewRecorder()
			router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/account", nil))

			if w.Code != tt.wantCode {
				t.Fatalf("状态码 = %d，期望 %d", w.Code, tt.wantCode)
			}
		})
	}
}

func TestAdminOnlyAllowsAdminRole(t *testing.T) {
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(CtxKeyRole, "admin")
	})
	router.Use(AdminOnly())
	router.GET("/admin", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/admin", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("状态码 = %d，期望 %d", w.Code, http.StatusOK)
	}
}

func TestAbortWithOpenAIError(t *testing.T) {
	c, w := newAuthContext(http.MethodGet, "/v1/models")

	abortWithOpenAIError(c, http.StatusPaymentRequired, "insufficient_quota", "额度不足")

	if w.Code != http.StatusPaymentRequired {
		t.Fatalf("状态码 = %d，期望 %d", w.Code, http.StatusPaymentRequired)
	}
	var got map[string]map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("响应 JSON 解析失败: %v", err)
	}
	errBody := got["error"]
	if errBody["message"] != "额度不足" || errBody["type"] != "authentication_error" || errBody["code"] != "insufficient_quota" {
		t.Fatalf("错误响应异常: %#v", errBody)
	}
	if !c.IsAborted() {
		t.Fatal("请求应该被终止")
	}
}
