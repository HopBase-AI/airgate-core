package handler

import (
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	apponeclick "github.com/DouDOU-start/airgate-core/internal/app/oneclick"
	"github.com/DouDOU-start/airgate-core/internal/server/dto"
	"github.com/DouDOU-start/airgate-core/internal/server/response"
)

// loadConfig 读取站点配置并在 BaseURL 为空时按当前请求 Host 推导兜底。
// 与 openclaw 域同款回退链：setting 显式值 → X-Forwarded-Proto/Host → Request.Host。
// 读取 setting 失败不阻断流程（脚本/兑换仍可用 Host 推导值工作），只记日志。
func (h *OneClickHandler) loadConfig(c *gin.Context) apponeclick.Config {
	cfg, err := h.service.Load(c.Request.Context())
	if err != nil {
		slog.Error("oneclick: 加载站点配置失败", "error", err)
	}
	if cfg.SiteName == "" {
		cfg.SiteName = "HopBase"
	}
	if cfg.BaseURL == "" {
		scheme := "http"
		if c.Request.TLS != nil || strings.EqualFold(c.GetHeader("X-Forwarded-Proto"), "https") {
			scheme = "https"
		}
		host := forwardedHost(c)
		if host == "" {
			host = "localhost"
		}
		cfg.BaseURL = fmt.Sprintf("%s://%s", scheme, host)
	}
	return cfg
}

// IssueSetupToken 为当前用户名下的密钥签发一次性接入令牌（JWT 鉴权）。
// 响应带按平台拼好的完整命令，前端直接展示复制。
func (h *OneClickHandler) IssueSetupToken(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		response.Unauthorized(c, "用户未认证")
		return
	}
	var req dto.OneClickIssueTokenReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BindError(c, err)
		return
	}

	token, err := h.service.IssueToken(c.Request.Context(), userID, int(req.KeyID))
	if err != nil {
		httpCode, message := h.handleError("签发一键接入令牌失败", "签发失败", err)
		response.Error(c, httpCode, httpCode, message)
		return
	}

	cfg := h.loadConfig(c)
	response.Success(c, dto.OneClickIssueTokenResp{
		Token:                  token,
		ExpiresInSeconds:       int(apponeclick.SetupTokenTTL().Seconds()),
		BaseURL:                cfg.BaseURL,
		CommandBash:            fmt.Sprintf("curl -fsSL %s/oneclick/setup.sh | bash -s -- %s", cfg.BaseURL, token),
		CommandPowerShell:      fmt.Sprintf("$env:HOPBASE_SETUP_TOKEN='%s'; irm %s/oneclick/setup.ps1 | iex", token, cfg.BaseURL),
		CommandCodexBash:       fmt.Sprintf("curl -fsSL %s/oneclick/setup-codex.sh | bash -s -- %s", cfg.BaseURL, token),
		CommandCodexPowerShell: fmt.Sprintf("$env:HOPBASE_SETUP_TOKEN='%s'; irm %s/oneclick/setup-codex.ps1 | iex", token, cfg.BaseURL),
	})
}

// SetupTokenStatus 轮询令牌状态（JWT 鉴权，仅签发者可见真实状态）。
func (h *OneClickHandler) SetupTokenStatus(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		response.Unauthorized(c, "用户未认证")
		return
	}
	status := h.service.Status(c.Request.Context(), userID, c.Param("token"))
	response.Success(c, dto.OneClickStatusResp{Status: status})
}

// HandleSetupScript 返回动态渲染好的 bash 接入脚本（公开路由）。
func (h *OneClickHandler) HandleSetupScript(c *gin.Context) {
	cfg := h.loadConfig(c)
	script, err := h.service.RenderSetupScript(cfg)
	if err != nil {
		slog.Error("oneclick: 渲染 setup.sh 失败", "error", err)
		c.String(http.StatusInternalServerError, "failed to render setup script")
		return
	}
	c.Header("Cache-Control", "no-store")
	c.Data(http.StatusOK, "text/x-shellscript; charset=utf-8", []byte(script))
}

// HandleSetupScriptPowerShell 返回 Windows PowerShell 版接入脚本（公开路由）。
func (h *OneClickHandler) HandleSetupScriptPowerShell(c *gin.Context) {
	cfg := h.loadConfig(c)
	script, err := h.service.RenderSetupScriptPowerShell(cfg)
	if err != nil {
		slog.Error("oneclick: 渲染 setup.ps1 失败", "error", err)
		c.String(http.StatusInternalServerError, "failed to render setup.ps1")
		return
	}
	c.Header("Cache-Control", "no-store")
	c.Data(http.StatusOK, "text/plain; charset=utf-8", []byte(script))
}

// HandleSetupCodexScript 返回 Codex CLI 的 bash 接入脚本（公开路由）。
func (h *OneClickHandler) HandleSetupCodexScript(c *gin.Context) {
	cfg := h.loadConfig(c)
	script, err := h.service.RenderSetupCodexScript(cfg)
	if err != nil {
		slog.Error("oneclick: 渲染 setup-codex.sh 失败", "error", err)
		c.String(http.StatusInternalServerError, "failed to render setup-codex script")
		return
	}
	c.Header("Cache-Control", "no-store")
	c.Data(http.StatusOK, "text/x-shellscript; charset=utf-8", []byte(script))
}

// HandleSetupCodexScriptPowerShell 返回 Codex CLI 的 Windows PowerShell 接入脚本（公开路由）。
func (h *OneClickHandler) HandleSetupCodexScriptPowerShell(c *gin.Context) {
	cfg := h.loadConfig(c)
	script, err := h.service.RenderSetupCodexScriptPowerShell(cfg)
	if err != nil {
		slog.Error("oneclick: 渲染 setup-codex.ps1 失败", "error", err)
		c.String(http.StatusInternalServerError, "failed to render setup-codex.ps1")
		return
	}
	c.Header("Cache-Control", "no-store")
	c.Data(http.StatusOK, "text/plain; charset=utf-8", []byte(script))
}

// HandleExchange 兑换一次性令牌，返回 key=value 纯文本（公开路由，脚本消费）。
//
// 用纯文本而非 JSON：bash 侧 `while IFS='=' read` 一行解析，无需 python3/jq
// （与 /openclaw/models.txt 同一设计取舍）。
func (h *OneClickHandler) HandleExchange(c *gin.Context) {
	result, err := h.service.Exchange(c.Request.Context(), c.PostForm("token"))
	if err != nil {
		httpCode, message := h.handleError("一键接入令牌兑换失败", "exchange failed", err)
		c.String(httpCode, message)
		return
	}
	cfg := h.loadConfig(c)

	var b strings.Builder
	fmt.Fprintf(&b, "api_key=%s\n", result.APIKey)
	fmt.Fprintf(&b, "key_name=%s\n", sanitizeMetaLine(result.KeyName))
	fmt.Fprintf(&b, "base_url=%s\n", cfg.BaseURL)
	c.Header("Cache-Control", "no-store")
	c.Data(http.StatusOK, "text/plain; charset=utf-8", []byte(b.String()))
}

// HandleVerify 脚本自检通过后的回执（公开路由）。
func (h *OneClickHandler) HandleVerify(c *gin.Context) {
	if err := h.service.Verify(c.Request.Context(), c.PostForm("token")); err != nil {
		httpCode, message := h.handleError("一键接入回执失败", "verify failed", err)
		c.String(httpCode, message)
		return
	}
	c.String(http.StatusOK, "ok")
}

// sanitizeMetaLine 清理写进 key=value 纯文本响应的元信息，防止换行破坏行协议。
func sanitizeMetaLine(s string) string {
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	return strings.TrimSpace(s)
}
