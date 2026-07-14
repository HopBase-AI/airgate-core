package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"

	appauth "github.com/DouDOU-start/airgate-core/internal/app/auth"
)

// OAuthAuthorize 跳转第三方授权页。浏览器直接导航到本端点，失败也用重定向回登录页
// 携带 oauth_error 提示，而非返回 JSON（用户看不到 XHR 响应）。
// 注册归因（来源站/邀请码）由前端经 query 传入，服务层签进 state 往返穿透。
func (h *AuthHandler) OAuthAuthorize(c *gin.Context) {
	provider := c.Param("provider")
	authorizeURL, err := h.service.OAuthAuthorizeURL(c.Request.Context(), provider, appauth.OAuthAttribution{
		SourceSite:   c.Query("source_site"),
		InviteCode:   c.Query("invite_code"),
		ReturnOrigin: c.Query("return_origin"),
	})
	if err != nil {
		c.Redirect(http.StatusFound, oauthRedirect("", oauthErrorPath(err)))
		return
	}
	c.Redirect(http.StatusFound, authorizeURL)
}

// OAuthCallback 第三方授权回调：完成登录后把 JWT 放在 URL fragment 里带回登录页
// （fragment 不会出现在服务端访问日志与 Referer 中）。
// 回跳源从已签名 state 解出（校验通过才回原域，否则相对跳转落回调域），
// 解决控制台与 api 不同域时登录态落错域的问题。
func (h *AuthHandler) OAuthCallback(c *gin.Context) {
	provider := c.Param("provider")
	returnOrigin := h.service.OAuthReturnOriginFromState(c.Request.Context(), c.Query("state"))

	// 用户在授权页点了拒绝等场景，第三方以 error 参数回调
	if errCode := c.Query("error"); errCode != "" {
		c.Redirect(http.StatusFound, oauthRedirect(returnOrigin, "/login?oauth_error="+url.QueryEscape("第三方授权已取消")))
		return
	}

	result, err := h.service.OAuthLogin(c.Request.Context(), provider, c.Query("code"), c.Query("state"))
	if err != nil {
		c.Redirect(http.StatusFound, oauthRedirect(returnOrigin, oauthErrorPath(err)))
		return
	}
	c.Redirect(http.StatusFound, oauthRedirect(returnOrigin, "/login#oauth_token="+url.QueryEscape(result.Token)))
}

// oauthRedirect 拼接回跳地址：origin 非空则跳回该源（跨域控制台场景），否则相对跳转（落回调域）。
func oauthRedirect(origin, path string) string {
	if origin == "" {
		return path
	}
	return strings.TrimRight(origin, "/") + path
}

// oauthErrorPath 把服务错误映射为登录页可展示的提示路径；内部错误不外露细节。
func oauthErrorPath(err error) string {
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
