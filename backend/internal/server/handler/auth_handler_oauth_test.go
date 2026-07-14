package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	appauth "github.com/DouDOU-start/airgate-core/internal/app/auth"
	"github.com/DouDOU-start/airgate-core/internal/auth"
)

// oauthSettingsStub 提供 OAuth 回跳测试所需的最小设置分组。
type oauthSettingsStub struct{ data map[string][]appauth.Setting }

func (s oauthSettingsStub) List(_ context.Context, group string) ([]appauth.Setting, error) {
	return s.data[group], nil
}

func newOAuthCallbackHandler(t *testing.T) (*AuthHandler, *appauth.Service) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	jwtMgr := auth.NewJWTManager("secret", 24)
	svc := appauth.NewService(stubAuthRepo{}, jwtMgr)
	svc.SetSettingsLister(oauthSettingsStub{data: map[string][]appauth.Setting{
		"site": {{Key: "api_base_url", Value: "https://api.essevin.com/"}},
		"oauth": {
			{Key: "oauth_google_enabled", Value: "true"},
			{Key: "oauth_google_client_id", Value: "cid"},
			{Key: "oauth_google_client_secret", Value: "secret"},
		},
	}})
	return NewAuthHandler(svc, jwtMgr), svc
}

// signedStateFor 走 authorize 签发一个真实 state，取出其中的 state 参数（携带回跳源）。
func signedStateFor(t *testing.T, svc *appauth.Service, returnOrigin string) string {
	t.Helper()
	authorizeURL, err := svc.OAuthAuthorizeURL(context.Background(), "google", appauth.OAuthAttribution{
		ReturnOrigin: returnOrigin,
	})
	if err != nil {
		t.Fatalf("OAuthAuthorizeURL error: %v", err)
	}
	u, err := url.Parse(authorizeURL)
	if err != nil {
		t.Fatalf("parse authorize url: %v", err)
	}
	state := u.Query().Get("state")
	if state == "" {
		t.Fatal("authorize URL 缺少 state")
	}
	return state
}

func callbackLocation(t *testing.T, h *AuthHandler, query string) string {
	t.Helper()
	router := gin.New()
	router.GET("/api/v1/auth/oauth/:provider/callback", h.OAuthCallback)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/oauth/google/callback?"+query, nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("callback status = %d, want 302; body=%s", rec.Code, rec.Body.String())
	}
	return rec.Header().Get("Location")
}

// 跨域控制台：从 console 域发起，回调应跳回 console 域（否则登录态落在 api 回调域、回原域显示未登录）。
func TestOAuthCallbackReturnsToConsoleOrigin(t *testing.T) {
	h, svc := newOAuthCallbackHandler(t)
	state := signedStateFor(t, svc, "https://console.essevin.com")
	loc := callbackLocation(t, h, "error=access_denied&state="+url.QueryEscape(state))
	if !strings.HasPrefix(loc, "https://console.essevin.com/login") {
		t.Fatalf("回调未跳回控制台域: Location = %q", loc)
	}
}

// 外域回跳源被后端白名单拒绝（不签进 state），回调回退相对跳转，绝不把 token/错误带向恶意域。
func TestOAuthCallbackRejectsForeignOrigin(t *testing.T) {
	h, svc := newOAuthCallbackHandler(t)
	state := signedStateFor(t, svc, "https://evil.com")
	loc := callbackLocation(t, h, "error=access_denied&state="+url.QueryEscape(state))
	if strings.HasPrefix(loc, "http://") || strings.HasPrefix(loc, "https://") {
		t.Fatalf("外域回跳源不应被采用，应为相对路径: Location = %q", loc)
	}
	if !strings.HasPrefix(loc, "/login") {
		t.Fatalf("应回退相对 /login: Location = %q", loc)
	}
}

// 无回跳源（ToB：控制台即 api 域）时回退相对跳转，保持原行为。
func TestOAuthCallbackNoOriginFallsBackRelative(t *testing.T) {
	h, svc := newOAuthCallbackHandler(t)
	state := signedStateFor(t, svc, "")
	loc := callbackLocation(t, h, "error=access_denied&state="+url.QueryEscape(state))
	if !strings.HasPrefix(loc, "/login") {
		t.Fatalf("无回跳源应相对跳转: Location = %q", loc)
	}
}
