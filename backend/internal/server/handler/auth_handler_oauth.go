package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"net/url"

	"github.com/gin-gonic/gin"

	appauth "github.com/DouDOU-start/airgate-core/internal/app/auth"
)

// OAuthAuthorize 跳转第三方授权页。浏览器直接导航到本端点，失败也用重定向回登录页
// 携带 oauth_error 提示，而非返回 JSON（用户看不到 XHR 响应）。
func (h *AuthHandler) OAuthAuthorize(c *gin.Context) {
	provider := c.Param("provider")
	authorizeURL, err := h.service.OAuthAuthorizeURL(c.Request.Context(), provider)
	if err != nil {
		c.Redirect(http.StatusFound, oauthErrorRedirect(err))
		return
	}
	c.Redirect(http.StatusFound, authorizeURL)
}

// OAuthCallback 第三方授权回调：完成登录后把 JWT 放在 URL fragment 里带回登录页
// （fragment 不会出现在服务端访问日志与 Referer 中）。
func (h *AuthHandler) OAuthCallback(c *gin.Context) {
	provider := c.Param("provider")

	// 用户在授权页点了拒绝等场景，第三方以 error 参数回调
	if errCode := c.Query("error"); errCode != "" {
		c.Redirect(http.StatusFound, "/login?oauth_error="+url.QueryEscape("第三方授权已取消"))
		return
	}

	result, err := h.service.OAuthLogin(c.Request.Context(), provider, c.Query("code"), c.Query("state"))
	if err != nil {
		c.Redirect(http.StatusFound, oauthErrorRedirect(err))
		return
	}
	c.Redirect(http.StatusFound, "/login#oauth_token="+url.QueryEscape(result.Token))
}

// oauthErrorRedirect 把服务错误映射为登录页可展示的提示；内部错误不外露细节。
func oauthErrorRedirect(err error) string {
	message := "第三方登录失败，请稍后重试"
	switch {
	case errors.Is(err, appauth.ErrOAuthProviderUnknown),
		errors.Is(err, appauth.ErrOAuthProviderDisabled),
		errors.Is(err, appauth.ErrOAuthNotConfigured),
		errors.Is(err, appauth.ErrOAuthStateInvalid),
		errors.Is(err, appauth.ErrOAuthExchangeFailed),
		errors.Is(err, appauth.ErrOAuthEmailRequired),
		errors.Is(err, appauth.ErrRegistrationDisabled),
		errors.Is(err, appauth.ErrUserDisabled):
		message = err.Error()
	default:
		slog.Error("第三方登录失败", "error", err)
	}
	return "/login?oauth_error=" + url.QueryEscape(message)
}
