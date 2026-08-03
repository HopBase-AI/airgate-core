package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	appauth "github.com/DouDOU-start/airgate-core/internal/app/auth"
	"github.com/DouDOU-start/airgate-core/internal/auth"
	"github.com/DouDOU-start/airgate-core/internal/infra/mailer"
)

func TestLoginByAPIKeyResponseAndJWTDoNotExposeOwner(t *testing.T) {
	gin.SetMode(gin.TestMode)
	expiresAt := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	repo := apiKeyLoginAuthRepo{
		user: appauth.User{
			ID: 77, Email: "reseller@example.com", Username: "reseller-owner",
			Balance: 999, Role: "admin", MaxConcurrency: 88, Status: "active",
			CreatedAt: time.Now(), UpdatedAt: time.Now(),
		},
		info: appauth.APIKeyLoginInfo{KeyID: 12, KeyName: "customer-key", UserID: 77},
		brief: appauth.APIKeyBrief{
			QuotaUSD: 25, UsedQuota: 4.5, ExpiresAt: &expiresAt,
			SellRate: 2.8, GroupRate: 3.1, Platform: "openai",
		},
	}
	jwtMgr := auth.NewJWTManager("secret", 24)
	router := gin.New()
	router.POST("/login-apikey", NewAuthHandler(appauth.NewService(repo, jwtMgr), jwtMgr).LoginByAPIKey)

	req := httptest.NewRequest(http.MethodPost, "/login-apikey", strings.NewReader(`{"key":"sk-valid"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}

	var envelope struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("解析登录响应失败: %v", err)
	}
	user, ok := envelope.Data["user"].(map[string]any)
	if !ok {
		t.Fatalf("user 响应异常: %s", rec.Body.String())
	}
	assertNoOwnerFields(t, user, rec.Body.String())
	if user["api_key_id"] != float64(12) || user["api_key_name"] != "customer-key" ||
		user["api_key_quota_usd"] != float64(25) || user["api_key_used_quota"] != 4.5 ||
		user["api_key_rate"] != 2.8 || user["api_key_platform"] != "openai" {
		t.Fatalf("Key 级字段异常: %s", rec.Body.String())
	}
	token, _ := envelope.Data["token"].(string)
	claims, err := jwtMgr.ParseToken(token)
	if err != nil {
		t.Fatalf("解析 API Key JWT 失败: %v", err)
	}
	if claims.UserID != 0 || claims.Email != "" || claims.APIKeyID != 12 {
		t.Fatalf("API Key JWT 泄漏 owner 身份: %+v", claims)
	}
}

func assertNoOwnerFields(t *testing.T, fields map[string]any, body string) {
	t.Helper()
	for _, forbidden := range []string{
		"id", "email", "username", "display_badge", "balance", "can_author_blog",
		"max_concurrency", "group_rates", "group_plugin_settings", "allowed_group_ids",
		"balance_alert_threshold", "status", "signup_source", "created_at", "updated_at",
	} {
		if _, exists := fields[forbidden]; exists {
			t.Fatalf("响应泄漏 owner 字段 %q: %s", forbidden, body)
		}
	}
}

func TestVerifyCodeRejectsInvalidCode(t *testing.T) {
	gin.SetMode(gin.TestMode)

	store := mailer.NewVerifyCodeStore()
	code := store.Generate("user@example.com")
	wrongCode := "000000"
	if code == wrongCode {
		wrongCode = "111111"
	}

	// 构造 auth service 并注入验证码存储
	jwtMgr := auth.NewJWTManager("secret", 24)
	authService := appauth.NewService(stubAuthRepo{}, jwtMgr)
	authService.SetVerifyCodeStore(store)

	router := gin.New()
	handler := NewAuthHandler(authService, jwtMgr)
	router.POST("/verify-code", handler.VerifyCode)

	body := `{"email":"user@example.com","code":"` + wrongCode + `"}`
	req := httptest.NewRequest(http.MethodPost, "/verify-code", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("VerifyCode invalid code status = %d, want %d; body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	if !store.Check("user@example.com", code) {
		t.Fatal("invalid verification attempt should not consume the valid code")
	}
}

func TestVerifyCodeDoesNotConsumeValidCode(t *testing.T) {
	gin.SetMode(gin.TestMode)

	store := mailer.NewVerifyCodeStore()
	code := store.Generate("user@example.com")

	// 构造 auth service 并注入验证码存储
	jwtMgr := auth.NewJWTManager("secret", 24)
	authService := appauth.NewService(stubAuthRepo{}, jwtMgr)
	authService.SetVerifyCodeStore(store)

	router := gin.New()
	handler := NewAuthHandler(authService, jwtMgr)
	router.POST("/verify-code", handler.VerifyCode)

	body := `{"email":"user@example.com","code":"` + code + `"}`
	req := httptest.NewRequest(http.MethodPost, "/verify-code", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("VerifyCode valid code status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if !store.Check("user@example.com", code) {
		t.Fatal("first-step verification should not consume the code; register still needs to verify it")
	}
	if !store.Verify("user@example.com", code) {
		t.Fatal("registration should still be able to consume the verified code")
	}
	if store.Check("user@example.com", code) {
		t.Fatal("registration verification should consume the code")
	}
}

// stubAuthRepo 空仓储桩（测试中不需要访问数据库）。
type stubAuthRepo struct{}

type apiKeyLoginAuthRepo struct {
	stubAuthRepo
	user  appauth.User
	info  appauth.APIKeyLoginInfo
	brief appauth.APIKeyBrief
}

func (r apiKeyLoginAuthRepo) ValidateAPIKeyForLogin(_ context.Context, _ string) (appauth.APIKeyLoginInfo, error) {
	return r.info, nil
}

func (r apiKeyLoginAuthRepo) FindByID(_ context.Context, _ int, _ bool) (appauth.User, error) {
	return r.user, nil
}

func (r apiKeyLoginAuthRepo) GetAPIKeyBrief(_ context.Context, _ int) (appauth.APIKeyBrief, error) {
	return r.brief, nil
}

func (stubAuthRepo) FindByEmail(_ context.Context, _ string) (appauth.User, error) {
	return appauth.User{}, appauth.ErrUserNotFound
}
func (stubAuthRepo) EmailExists(_ context.Context, _ string) (bool, error) {
	return false, nil
}
func (stubAuthRepo) Create(_ context.Context, _ appauth.CreateUserInput) (appauth.User, error) {
	return appauth.User{}, nil
}
func (stubAuthRepo) FindByID(_ context.Context, _ int, _ bool) (appauth.User, error) {
	return appauth.User{}, appauth.ErrUserNotFound
}
func (stubAuthRepo) ValidateAPIKeySession(_ context.Context, _ int) (appauth.User, error) {
	return appauth.User{}, appauth.ErrInvalidAPIKeySession
}
func (stubAuthRepo) ValidateAPIKeyForLogin(_ context.Context, _ string) (appauth.APIKeyLoginInfo, error) {
	return appauth.APIKeyLoginInfo{}, appauth.ErrInvalidAPIKey
}
func (stubAuthRepo) GetAPIKeyBrief(_ context.Context, _ int) (appauth.APIKeyBrief, error) {
	return appauth.APIKeyBrief{}, nil
}
func (stubAuthRepo) FindUserByIdentity(_ context.Context, _, _ string) (appauth.User, error) {
	return appauth.User{}, appauth.ErrUserNotFound
}
func (stubAuthRepo) LinkIdentity(_ context.Context, _ int, _ appauth.IdentityInput) error {
	return nil
}
func (stubAuthRepo) FindUserIDByInviteCode(_ context.Context, _ string) (int, error) {
	return 0, appauth.ErrUserNotFound
}
