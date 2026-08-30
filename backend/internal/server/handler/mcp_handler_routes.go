package handler

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/DouDOU-start/airgate-core/ent"
	entapikey "github.com/DouDOU-start/airgate-core/ent/apikey"
	appmodelpricing "github.com/DouDOU-start/airgate-core/internal/app/modelpricing"
	appusage "github.com/DouDOU-start/airgate-core/internal/app/usage"
	"github.com/DouDOU-start/airgate-core/internal/auth"
	"github.com/DouDOU-start/airgate-core/internal/server/dto"
)

// MCPHandler 无状态 MCP(Model Context Protocol) Streamable HTTP 服务端。
//
// 定位是「管理面」：让 Claude Code / Cursor 等 MCP 客户端用一把 sk- key
// 直接查余额、Key 配额、可用模型与实付价、用量统计——推理流量仍走
// 各协议兼容端点，这里只读不写。
//
// 协议实现取舍：
//   - 纯无状态:不发 Mcp-Session-Id,不支持 SSE 流(GET 返回 405),每个
//     JSON-RPC 请求独立应答 application/json;通知(无 id)返回 202。
//   - 鉴权同 cc_compat(/v1/usage)的轻量模式而非 middleware.APIKeyAuth:
//     余额耗尽(402)恰恰是用户最需要经 MCP 看到的状态,不能被鉴权层拦截;
//     查询本身也不需要绑定 group 的计费链路。
//   - 数据口径一律按「key 持有者 = end customer」的最小视图:用量只出
//     billed_cost 且锁定本 key(与控制台 API Key 会话同口径),不泄漏
//     reseller 的 actual_cost 或账号下其他 key 的数据。
type MCPHandler struct {
	db      *ent.Client
	pricing *appmodelpricing.Service
	usage   *appusage.Service
}

func NewMCPHandler(db *ent.Client, pricing *appmodelpricing.Service, usage *appusage.Service) *MCPHandler {
	return &MCPHandler{db: db, pricing: pricing, usage: usage}
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
	mcpErrInternal       = -32603
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

// authenticateMCP 轻量 key 鉴权:失败时写 401(网关标准错误体)并返回 false。
func (h *MCPHandler) authenticateMCP(c *gin.Context) (*ent.APIKey, bool) {
	key := strings.TrimSpace(c.GetHeader("x-api-key"))
	if key == "" {
		header := c.GetHeader("Authorization")
		if len(header) > 7 && strings.EqualFold(header[:7], "Bearer ") {
			key = strings.TrimSpace(header[7:])
		}
	}
	if key == "" || !strings.HasPrefix(key, "sk-") {
		mcpAbortAuth(c, "missing_api_key", "缺少 API Key")
		return nil, false
	}

	ak, err := h.db.APIKey.Query().
		Where(
			entapikey.KeyHash(auth.HashAPIKey(key)),
			entapikey.StatusEQ(entapikey.StatusActive),
		).
		WithUser().
		WithGroup().
		Only(c.Request.Context())
	if err != nil {
		mcpAbortAuth(c, "invalid_api_key", "无效的 API Key")
		return nil, false
	}
	if ak.ExpiresAt != nil && ak.ExpiresAt.Before(time.Now()) {
		mcpAbortAuth(c, "invalid_api_key", "API Key 已过期")
		return nil, false
	}
	if _, err := ak.Edges.UserOrErr(); err != nil {
		mcpAbortAuth(c, "invalid_api_key", "无效的 API Key 归属")
		return nil, false
	}
	return ak, true
}

func mcpAbortAuth(c *gin.Context, code, message string) {
	c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
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
	ak, ok := h.authenticateMCP(c)
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
		h.handleToolCall(c, ak, req)
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
			"description": "查询当前 API Key 的请求数、token 数与费用统计(默认最近 7 天),可按日期范围与模型过滤。",
			"inputSchema": gin.H{
				"type": "object",
				"properties": gin.H{
					"start_date": gin.H{"type": "string", "description": "起始日期 YYYY-MM-DD(含当日),默认最近 7 天"},
					"end_date":   gin.H{"type": "string", "description": "结束日期 YYYY-MM-DD(含当日),默认今天"},
					"tz":         gin.H{"type": "string", "description": "IANA 时区名,用于解释日期,默认 UTC"},
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

func (h *MCPHandler) handleToolCall(c *gin.Context, ak *ent.APIKey, req mcpRequest) {
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
		result, err = mcpToolText(mcpBalancePayload(ak))
	case "get_key_info":
		result, err = mcpToolText(mcpKeyInfoPayload(ak))
	case "list_models":
		result, err = h.callListModels(ctx, ak)
	case "get_usage":
		result, err = h.callGetUsage(ctx, ak, params.Arguments)
	default:
		mcpWriteError(c, req.ID, mcpErrInvalidParams, "unknown tool: "+params.Name)
		return
	}
	if err != nil {
		mcpWriteResult(c, req.ID, mcpToolError("查询失败,请稍后重试"))
		return
	}
	mcpWriteResult(c, req.ID, result)
}

func mcpBalancePayload(ak *ent.APIKey) gin.H {
	balance := ak.Edges.User.Balance
	if balance < 0 {
		balance = 0
	}
	available := balance
	keyRemaining := 0.0
	if ak.QuotaUsd > 0 {
		keyRemaining = ak.QuotaUsd - ak.UsedQuota
		if keyRemaining < 0 {
			keyRemaining = 0
		}
		if keyRemaining < available {
			available = keyRemaining
		}
	}
	payload := gin.H{
		"balance_usd":   balance,
		"available_usd": available,
		"key_quota_usd": gin.H{
			"total":     ak.QuotaUsd,
			"used":      ak.UsedQuota,
			"unlimited": ak.QuotaUsd <= 0,
		},
	}
	if ak.QuotaUsd > 0 {
		payload["key_quota_usd"].(gin.H)["remaining"] = keyRemaining
	}
	return payload
}

func mcpKeyInfoPayload(ak *ent.APIKey) gin.H {
	payload := gin.H{
		"name":            ak.Name,
		"key_hint":        ak.KeyHint,
		"status":          ak.Status,
		"quota_usd":       ak.QuotaUsd,
		"used_quota_usd":  ak.UsedQuota,
		"max_concurrency": ak.MaxConcurrency,
		"created_at":      ak.CreatedAt.UTC().Format(time.RFC3339),
	}
	if ak.ExpiresAt != nil {
		payload["expires_at"] = ak.ExpiresAt.UTC().Format(time.RFC3339)
	} else {
		payload["expires_at"] = nil
	}
	if group := ak.Edges.Group; group != nil {
		payload["group"] = gin.H{"id": group.ID, "name": group.Name}
	} else {
		payload["group"] = nil
	}
	return payload
}

func (h *MCPHandler) callListModels(ctx context.Context, ak *ent.APIKey) (gin.H, error) {
	result, err := h.pricing.APIKeyPricing(ctx, ak.Edges.User.ID, ak.ID)
	if err != nil {
		return nil, err
	}
	return mcpToolText(toMyModelPricingResp(result))
}

func (h *MCPHandler) callGetUsage(ctx context.Context, ak *ent.APIKey, rawArgs json.RawMessage) (gin.H, error) {
	var args struct {
		StartDate string `json:"start_date"`
		EndDate   string `json:"end_date"`
		TZ        string `json:"tz"`
		Platform  string `json:"platform"`
		Model     string `json:"model"`
	}
	if len(rawArgs) > 0 {
		if err := json.Unmarshal(rawArgs, &args); err != nil {
			return nil, err
		}
	}
	loc := time.UTC
	if args.TZ != "" {
		if parsed, err := time.LoadLocation(args.TZ); err == nil {
			loc = parsed
		}
	}
	now := time.Now().In(loc)
	if args.EndDate == "" {
		args.EndDate = now.Format("2006-01-02")
	}
	if args.StartDate == "" {
		args.StartDate = now.AddDate(0, 0, -6).Format("2006-01-02")
	}

	keyID := int64(ak.ID)
	result, err := h.usage.UserStatsWithModels(ctx, int64(ak.Edges.User.ID), appusage.StatsFilter{
		APIKeyID:    &keyID,
		Platform:    args.Platform,
		Model:       args.Model,
		StartDate:   args.StartDate,
		EndDate:     args.EndDate,
		TZ:          args.TZ,
		ScopedToKey: true,
	})
	if err != nil {
		return nil, err
	}

	// 与控制台 API Key 会话同口径:只暴露 billed_cost,锁定本 key。
	resp := dto.UsageStatsResp{
		TotalRequests:   result.Summary.TotalRequests,
		FailedRequests:  result.Summary.FailedRequests,
		TotalTokens:     result.Summary.TotalTokens,
		TotalBilledCost: result.Summary.TotalBilledCost,
	}
	for _, m := range result.ByModel {
		resp.ByModel = append(resp.ByModel, dto.ModelStats{
			Model:      m.Model,
			Requests:   m.Requests,
			Tokens:     m.Tokens,
			BilledCost: m.BilledCost,
		})
	}
	return mcpToolText(gin.H{
		"start_date": args.StartDate,
		"end_date":   args.EndDate,
		"tz":         args.TZ,
		"stats":      resp,
	})
}
