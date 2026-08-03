package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	appuser "github.com/DouDOU-start/airgate-core/internal/app/user"
	"github.com/DouDOU-start/airgate-core/internal/server/middleware"
)

type getMeAPIKeyRepo struct {
	appuser.Repository
	brief      appuser.APIKeyBrief
	err        error
	findErr    error
	findCalled bool
}

func (r *getMeAPIKeyRepo) FindByID(context.Context, int, bool) (appuser.User, error) {
	r.findCalled = true
	item := appuser.User{
		ID: 77, Email: "reseller@example.com", Username: "owner", Balance: 999,
		MaxConcurrency: 88, Status: "active",
	}
	return item, r.findErr
}

func (r *getMeAPIKeyRepo) GetAPIKeyInfo(_ context.Context, userID, keyID int) (appuser.APIKeyBrief, error) {
	if userID != 77 || keyID != 12 {
		return appuser.APIKeyBrief{}, appuser.ErrInvalidAPIKeySession
	}
	return r.brief, r.err
}

func TestGetMeAPIKeySessionReturnsOnlyKeyProjection(t *testing.T) {
	gin.SetMode(gin.TestMode)
	expiresAt := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	repo := &getMeAPIKeyRepo{brief: appuser.APIKeyBrief{
		Name: "customer-key", QuotaUSD: 25, UsedQuota: 4.5, ExpiresAt: &expiresAt,
		SellRate: 2.8, GroupRate: 3.1, Platform: "openai",
	}}
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(middleware.CtxKeyUserID, 77)
		c.Set(middleware.CtxKeyAPIKeyID, 12)
	})
	router.GET("/users/me", NewUserHandler(appuser.NewService(repo), nil).GetMe)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/users/me", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var envelope struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("解析 /users/me 响应失败: %v", err)
	}
	assertNoOwnerFields(t, envelope.Data, rec.Body.String())
	if repo.findCalled {
		t.Fatal("API Key /users/me 不应读取完整 owner 用户对象")
	}
	if envelope.Data["api_key_id"] != float64(12) || envelope.Data["api_key_name"] != "customer-key" ||
		envelope.Data["api_key_quota_usd"] != float64(25) || envelope.Data["api_key_used_quota"] != 4.5 ||
		envelope.Data["api_key_rate"] != 2.8 || envelope.Data["api_key_platform"] != "openai" {
		t.Fatalf("Key 级字段异常: %s", rec.Body.String())
	}
}

func TestGetMeAPIKeySessionFailsClosedWhenKeyIsInvalid(t *testing.T) {
	repo := &getMeAPIKeyRepo{err: appuser.ErrInvalidAPIKeySession}
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(middleware.CtxKeyUserID, 77)
		c.Set(middleware.CtxKeyAPIKeyID, 12)
	})
	router.GET("/users/me", NewUserHandler(appuser.NewService(repo), nil).GetMe)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/users/me", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if repo.findCalled {
		t.Fatal("失效 API Key 不应回退读取 owner 用户对象")
	}
}

func TestGetMeAPIKeySessionKeepsTransientStoreErrorsRetryable(t *testing.T) {
	repo := &getMeAPIKeyRepo{err: errors.New("db unavailable")}
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(middleware.CtxKeyUserID, 77)
		c.Set(middleware.CtxKeyAPIKeyID, 12)
	})
	router.GET("/users/me", NewUserHandler(appuser.NewService(repo), nil).GetMe)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/users/me", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestGetMeUserSessionKeepsTransientStoreErrorsRetryable(t *testing.T) {
	repo := &getMeAPIKeyRepo{findErr: errors.New("db unavailable")}
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(middleware.CtxKeyUserID, 77)
	})
	router.GET("/users/me", NewUserHandler(appuser.NewService(repo), nil).GetMe)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/users/me", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestGetMeUserSessionRejectsDeletedOwner(t *testing.T) {
	repo := &getMeAPIKeyRepo{findErr: appuser.ErrUserNotFound}
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(middleware.CtxKeyUserID, 77)
	})
	router.GET("/users/me", NewUserHandler(appuser.NewService(repo), nil).GetMe)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/users/me", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}
