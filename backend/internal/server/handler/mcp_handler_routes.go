package handler

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	sdk "github.com/DouDOU-start/airgate-sdk/sdkgo"

	appmcp "github.com/DouDOU-start/airgate-core/internal/app/mcp"
	"github.com/DouDOU-start/airgate-core/internal/auth"
)

// MCPHandler 无状态 MCP(Model Context Protocol) Streamable HTTP 服务端的传输层。
//
// 定位是「管理面」：让 Claude Code / Cursor 等 MCP 客户端用一把 sk- key
// 直接查余额、Key 配额、可用模型与实付价、用量统计——推理流量仍走
// 各协议兼容端点，这里只读不写。业务口径全部在 app/mcp;本层只做
// JSON-RPC 编解码、鉴权错误映射与 dto 转换。
//
// 协议实现取舍：
//   - 纯无状态:不发 Mcp-Session-Id,不支持 SSE 流(GET 返回 405),每个
//     JSON-RPC 请求独立应答 application/json;通知(无 id)返回 202。
//   - 鉴权走 auth.ValidateAPIKeyForManagement 而非 middleware.APIKeyAuth:
//     配额耗尽(402)恰恰是用户最需要经 MCP 看到的状态,查询也不需要绑定
//     group 的计费链路;但用户被禁用、DB 瞬时故障的语义与转发路径一致
//     (403 / 503,绝不把服务端故障误报成凭证无效)。
type MCPHandler struct {
	service *appmcp.Service
}

func NewMCPHandler(service *appmcp.Service) *MCPHandler {
	return &MCPHandler{service: service}
}

const mcpLatestProtocolVersion = "2025-06-18"

// mcpSupportedProtocolVersions 客户端请求这些版本时原样回显,否则回落最新版。
var mcpSupportedProtocolVersions = map[string]bool{
	"2025-06-18": true,
	"2025-03-26": true,
	"2024-11-05": true,
}

// negotiateMCPProtocolVersion 按 MCP 规范协商协议版本。
func negotiateMCPProtocolVersion(requested string) string {
	if mcpSupportedProtocolVersions[requested] {
		return requested
	}
	return mcpLatestProtocolVersion
}

type mcpRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

// isNotification 判断消息是否为 JSON-RPC 通知(无 id / id 为 null)。
func (r *mcpRequest) isNotification() bool {
	trimmed := strings.TrimSpace(string(r.ID))
	return trimmed == "" || trimmed == "null"
}

const (
	mcpErrParse          = -32700
	mcpErrInvalidRequest = -32600
	mcpErrMethodNotFound = -32601
	mcpErrInvalidParams  = -32602
)

func mcpWriteResult(c *gin.Context, id json.RawMessage, result any) {
	c.JSON(http.StatusOK, gin.H{"jsonrpc": "2.0", "id": id, "result": result})
}

func mcpWriteError(c *gin.Context, id json.RawMessage, code int, message string) {
	if len(id) == 0 {
		id = json.RawMessage("null")
	}
	c.JSON(http.StatusOK, gin.H{"jsonrpc": "2.0", "id": id, "error": gin.H{"code": code, "message": message}})
}

// authenticateMCP 管理面 key 鉴权。失败时写网关标准错误体并返回 false;
// 事件名沿用 api_key_validation_failed,让既有告警覆盖 MCP 面的爆破/异常。
func (h *MCPHandler) authenticateMCP(c *gin.Context) (*auth.APIKeyInfo, bool) {
	key := strings.TrimSpace(c.GetHeader("x-api-key"))
	if key == "" {
		header := c.GetHeader("Authorization")
		if len(header) > 7 && strings.EqualFold(header[:7], "Bearer ") {
			key = strings.TrimSpace(header[7:])
		}
	}
	if key == "" || !strings.HasPrefix(key, "sk-") {
		slog.Warn("api_key_validation_failed", sdk.LogFieldReason, "mcp_missing_api_key")
		mcpAbortAuth(c, http.StatusUnauthorized, "missing_api_key", "缺少 API Key")
		return nil, false
	}

	info, err := h.service.Authenticate(c.Request.Context(), key)
	if err != nil {
		code := "invalid_api_key"
		status := http.StatusUnauthorized
		reason := "mcp_invalid_key"
		switch {
		case errors.Is(err, auth.ErrInvalidAPIKey):
			// 维持默认 401 / invalid_api_key
		case errors.Is(err, auth.ErrAPIKeyExpired):
			code, reason = "api_key_expired", "mcp_expired"
		case errors.Is(err, auth.ErrUserDisabled):
			code, status, reason = "account_disabled", http.StatusForbidden, "mcp_user_disabled"
		default:
			// DB 超时 / 连接池满等服务端侧问题:必须 503,不能让客户端以为 key 被吊销。
			code, status, reason = "service_unavailable", http.StatusServiceUnavailable, "mcp_service_unavailable"
		}
		slog.Warn("api_key_validation_failed", sdk.LogFieldReason, reason, sdk.LogFieldError, err, sdk.LogFieldStatus, status)
		mcpAbortAuth(c, status, code, err.Error())
		return nil, false
	}
	return info, true
}

func mcpAbortAuth(c *gin.Context, status int, code, message string) {
	c.AbortWithStatusJSON(status, gin.H{
		"error": gin.H{"message": message, "type": "authentication_error", "code": code},
	})
}

// HandleMethodNotAllowed 处理 GET/DELETE /mcp:无状态实现不提供 SSE 流与会话。
func (h *MCPHandler) HandleMethodNotAllowed(c *gin.Context) {
	c.Header("Allow", "POST")
	c.JSON(http.StatusMethodNotAllowed, gin.H{
		"jsonrpc": "2.0", "id": nil,
		"error": gin.H{"code": mcpErrInvalidRequest, "message": "stateless MCP server: use POST with a single JSON-RPC message"},
	})
}

// Handle 处理 POST /mcp 上的单条 JSON-RPC 消息。
func (h *MCPHandler) Handle(c *gin.Context) {
	info, ok := h.authenticateMCP(c)
	if !ok {
		return
	}

	body, err := io.ReadAll(io.LimitReader(c.Request.Body, 1<<20))
	if err != nil {
		mcpWriteError(c, nil, mcpErrParse, "read body failed")
		return
	}
	if trimmed := strings.TrimSpace(string(body)); strings.HasPrefix(trimmed, "[") {
		// 2025-06-18 已移除 batch;为简化实现统一不支持。
		mcpWriteError(c, nil, mcpErrInvalidRequest, "batch requests are not supported")
		return
	}
	var req mcpRequest
	if err := json.Unmarshal(body, &req); err != nil {
		mcpWriteError(c, nil, mcpErrParse, "parse error")
		return
	}
	if req.JSONRPC != "2.0" {
		mcpWriteError(c, req.ID, mcpErrInvalidRequest, "jsonrpc must be \"2.0\"")
		return
	}
	if req.isNotification() {
		// 通知(如 notifications/initialized)无需应答内容。
		c.Status(http.StatusAccepted)
		return
	}

	switch req.Method {
	case "initialize":
		var params struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(req.Params, &params)
		mcpWriteResult(c, req.ID, gin.H{
			"protocolVersion": negotiateMCPProtocolVersion(params.ProtocolVersion),
			"capabilities":    gin.H{"tools": gin.H{}},
			"serverInfo": gin.H{
				"name":    "hopbase-gateway",
				"title":   "HopBase Gateway Management",
				"version": "1.0.0",
			},
			"instructions": "Read-only management tools scoped to the calling HopBase API key: " +
				"balance, key quota, available models with effective pricing, and usage stats. " +
				"Scope follows the key in the Authorization (or x-api-key) header; inference traffic " +
				"stays on the protocol-compatible endpoints described at https://hop-base.com/llms.txt.",
		})
	case "ping":
		mcpWriteResult(c, req.ID, gin.H{})
	case "tools/list":
		mcpWriteResult(c, req.ID, gin.H{"tools": mcpToolDefinitions()})
	case "tools/call":
		h.handleToolCall(c, info, req)
	default:
		mcpWriteError(c, req.ID, mcpErrMethodNotFound, "method not found: "+req.Method)
	}
}

func mcpToolDefinitions() []gin.H {
	emptySchema := gin.H{"type": "object", "properties": gin.H{}, "additionalProperties": false}
	return []gin.H{
		{
			"name":        "get_balance",
			"title":       "查询余额",
			"description": "查询当前 API Key 归属账户的可用余额(USD),以及本 Key 的花费上限使用情况。",
			"inputSchema": emptySchema,
		},
		{
			"name":        "get_key_info",
			"title":       "查询 Key 信息",
			"description": "查询当前 API Key 的名称、所属套餐分组、花费上限、并发上限、过期时间与状态。",
			"inputSchema": emptySchema,
		},
		{
			"name":        "list_models",
			"title":       "查询可用模型与价格",
			"description": "查询当前 API Key 可调用的模型清单与实付价(按 Key 所属分组与用户专属价折算)。选模型前必查;调用模型请走协议兼容端点而不是 MCP。",
			"inputSchema": emptySchema,
		},
		{
			"name":        "get_usage",
			"title":       "查询用量统计",
			"description": "查询当前 API Key 的请求数、token 数与账面费用统计(默认最近 7 天,最长 92 天),可按日期范围与模型过滤。日期必须是 YYYY-MM-DD。",
			"inputSchema": gin.H{
				"type": "object",
				"properties": gin.H{
					"start_date": gin.H{"type": "string", "description": "起始日期 YYYY-MM-DD(含当日),默认最近 7 天"},
					"end_date":   gin.H{"type": "string", "description": "结束日期 YYYY-MM-DD(含当日),默认今天"},
					"tz":         gin.H{"type": "string", "description": "IANA 时区名如 Asia/Shanghai,默认 UTC"},
					"platform":   gin.H{"type": "string", "description": "按平台过滤,如 claude / openai / gemini"},
					"model":      gin.H{"type": "string", "description": "按模型 ID 过滤"},
				},
				"additionalProperties": false,
			},
		},
	}
}

// mcpToolText 把工具结果包成 MCP content(文本承载紧凑 JSON)。
func mcpToolText(v any) (gin.H, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return gin.H{
		"content": []gin.H{{"type": "text", "text": string(data)}},
		"isError": false,
	}, nil
}

func mcpToolError(message string) gin.H {
	return gin.H{
		"content": []gin.H{{"type": "text", "text": message}},
		"isError": true,
	}
}

func (h *MCPHandler) handleToolCall(c *gin.Context, info *auth.APIKeyInfo, req mcpRequest) {
	var params struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil || params.Name == "" {
		mcpWriteError(c, req.ID, mcpErrInvalidParams, "tools/call requires params.name")
		return
	}

	ctx := c.Request.Context()
	var (
		result gin.H
		err    error
	)
	switch params.Name {
	case "get_balance":
		result, err = mcpToolText(toMCPBalanceResp(h.service.Balance(info)))
	case "get_key_info":
		result, err = mcpToolText(toMCPKeyInfoResp(h.service.KeyInfo(info)))
	case "list_models":
		var pricing any
		if r, perr := h.service.Models(ctx, info); perr != nil {
			err = perr
		} else {
			pricing = toMyModelPricingResp(r)
		}
		if err == nil {
			result, err = mcpToolText(pricing)
		}
	case "get_usage":
		var args appmcp.UsageArgs
		if len(params.Arguments) > 0 {
			var raw struct {
				StartDate string `json:"start_date"`
				EndDate   string `json:"end_date"`
				TZ        string `json:"tz"`
				Platform  string `json:"platform"`
				Model     string `json:"model"`
			}
			if uerr := json.Unmarshal(params.Arguments, &raw); uerr != nil {
				mcpWriteError(c, req.ID, mcpErrInvalidParams, "get_usage arguments must be an object")
				return
			}
			args = appmcp.UsageArgs{StartDate: raw.StartDate, EndDate: raw.EndDate, TZ: raw.TZ, Platform: raw.Platform, Model: raw.Model}
		}
		var usage appmcp.UsageResult
		if usage, err = h.service.Usage(ctx, info, args); err == nil {
			result, err = mcpToolText(toMCPUsageResp(usage))
		}
	default:
		mcpWriteError(c, req.ID, mcpErrInvalidParams, "unknown tool: "+params.Name)
		return
	}
	if err != nil {
		// 参数类错误原文回给调用方(可修复);服务端错误只给通用提示并记日志。
		if errors.Is(err, appmcp.ErrInvalidUsageArgs) {
			mcpWriteResult(c, req.ID, mcpToolError(err.Error()))
			return
		}
		slog.Error("mcp_tool_call_failed", "tool", params.Name, sdk.LogFieldError, err)
		mcpWriteResult(c, req.ID, mcpToolError("查询失败,请稍后重试"))
		return
	}
	mcpWriteResult(c, req.ID, result)
}
