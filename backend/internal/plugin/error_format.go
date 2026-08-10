package plugin

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/DouDOU-start/airgate-core/internal/scheduler"
)

// error_format.go：对外错误体格式选择（tech-debt #1 治理）。
//
// Core 不硬编码外部协议的错误形态。网关插件经 Metadata 约定声明自己的协议
// 错误格式，Core 据此选择格式化器：
//   - RouteDefinition.Metadata["error_format"]（路由级，优先；适合 openai 插件
//     这类同时暴露 OpenAI 与 Anthropic 兼容端点的混合网关）
//   - PluginInfo.Metadata["error_format"]（插件级）
//   - 未声明 → OpenAI 兼容格式（历史默认，保证既有客户端不回归）
//
// 当前支持的取值：openai（默认）/ anthropic。

// ginCtxKeyErrorFormat 请求归属插件解析成功后写入，之后所有错误写出按此格式。
const ginCtxKeyErrorFormat = "airgate.error_format"

const (
	errorFormatOpenAI    = "openai"
	errorFormatAnthropic = "anthropic"
)

// setRequestErrorFormat 在解析出目标插件后记录其声明的错误格式。
// format 为空（插件未声明）时不写入，保持 OpenAI 默认。
func setRequestErrorFormat(c *gin.Context, format string) {
	if format != "" {
		c.Set(ginCtxKeyErrorFormat, format)
	}
}

func requestErrorFormat(c *gin.Context) string {
	if v, ok := c.Get(ginCtxKeyErrorFormat); ok {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
	}
	return errorFormatOpenAI
}

// protocolError 按请求归属插件声明的格式写错误响应。
// 插件未解析（如鉴权前、路由未命中）或未声明格式时回退 OpenAI 兼容格式。
func protocolError(c *gin.Context, status int, errType, code, message string) {
	switch requestErrorFormat(c) {
	case errorFormatAnthropic:
		c.JSON(status, gin.H{
			"type": "error",
			"error": gin.H{
				"type":    anthropicErrorType(errType, status),
				"message": message,
			},
		})
	default:
		c.JSON(status, gin.H{
			"error": gin.H{
				"message": message,
				"type":    errType,
				"code":    code,
			},
		})
	}
}

// protocolStreamError reports a failure after an SSE heartbeat already
// committed HTTP 200. The HTTP status can no longer change, so the error must
// use the selected wire protocol and terminate the existing stream cleanly.
func protocolStreamError(c *gin.Context, status int, errType, code, message string) {
	var prefix string
	var payload any
	if requestErrorFormat(c) == errorFormatAnthropic {
		prefix = "event: error\ndata: "
		payload = gin.H{
			"type": "error",
			"error": gin.H{
				"type":    anthropicErrorType(errType, status),
				"message": message,
			},
		}
	} else if isResponsesStreamRequest(c) {
		prefix = "event: response.failed\ndata: "
		payload = responsesFailedStreamPayload(c, status, errType, code, message)
	} else {
		prefix = "data: "
		payload = gin.H{
			"error": gin.H{
				"message": message,
				"type":    errType,
				"code":    code,
			},
		}
	}
	body, _ := json.Marshal(payload)
	_, _ = c.Writer.WriteString(prefix + string(body) + "\n\n")
	c.Writer.Flush()
}

func isResponsesStreamRequest(c *gin.Context) bool {
	if c == nil || c.Request == nil || c.Request.URL == nil {
		return false
	}
	path := c.Request.URL.Path
	return pathHasAPIPrefix(path, "/v1/responses") || pathHasAPIPrefix(path, "/responses")
}

// responsesFailedStreamPayload uses the Responses API terminal failure event
// instead of a generic OpenAI error object. Codex clients use response.failed
// to stop the stream and surface the server-provided message.
func responsesFailedStreamPayload(c *gin.Context, status int, errType, code, message string) gin.H {
	model := "unknown"
	if value, ok := c.Get(ginCtxKeyModel); ok {
		if candidate, ok := value.(string); ok && candidate != "" {
			model = candidate
		}
	}
	responseCode := code
	switch {
	case status == http.StatusTooManyRequests || errType == "rate_limit_error":
		responseCode = "rate_limit_exceeded"
	case status >= http.StatusInternalServerError || errType == "server_error":
		responseCode = "server_error"
	case errType == "invalid_request_error":
		responseCode = "invalid_prompt"
	}
	return gin.H{
		"type":            "response.failed",
		"sequence_number": 0,
		"response": gin.H{
			"id":                   "resp_hopbase_error",
			"object":               "response",
			"created_at":           time.Now().Unix(),
			"status":               "failed",
			"background":           false,
			"error":                gin.H{"code": responseCode, "message": message},
			"incomplete_details":   nil,
			"instructions":         nil,
			"max_output_tokens":    nil,
			"model":                model,
			"output":               []any{},
			"parallel_tool_calls":  true,
			"previous_response_id": nil,
			"reasoning":            gin.H{"effort": "medium", "summary": nil},
			"service_tier":         "default",
			"store":                false,
			"temperature":          1,
			"text":                 gin.H{"format": gin.H{"type": "text"}, "verbosity": "medium"},
			"tool_choice":          "auto",
			"tools":                []any{},
			"top_logprobs":         0,
			"top_p":                1,
			"truncation":           "disabled",
			"usage":                nil,
			"user":                 nil,
			"metadata":             gin.H{},
		},
	}
}

// protocolRateLimitError 写 429 + Retry-After 头 + 协议对应的限流错误体。
// retryAfter < 1s 一律向上取整到 1s（客户端 SDK 普遍按整数秒读 Retry-After）。
// 同时输出 retry-after-ms 头，便于精度敏感的客户端做更细粒度的退避。
func protocolRateLimitError(c *gin.Context, status int, code, message string, retryAfter time.Duration) {
	retryAfter = scheduler.ClampRateLimitRetryAfter(retryAfter)
	if retryAfter > 0 {
		secs := int64((retryAfter + time.Second - 1) / time.Second)
		if secs < 1 {
			secs = 1
		}
		c.Writer.Header().Set("Retry-After", strconv.FormatInt(secs, 10))
		c.Writer.Header().Set("Retry-After-Ms", strconv.FormatInt(retryAfter.Milliseconds(), 10))
	}
	protocolError(c, status, "rate_limit_error", code, message)
}

// anthropicErrorType 把内部 errType 收敛到 Anthropic 错误类型集合，
// 未知类型按 HTTP 状态码归类。
func anthropicErrorType(errType string, status int) string {
	switch errType {
	case "invalid_request_error", "authentication_error", "permission_error",
		"not_found_error", "rate_limit_error", "api_error", "overloaded_error":
		return errType
	}
	switch {
	case status == http.StatusTooManyRequests:
		return "rate_limit_error"
	case status == http.StatusNotFound:
		return "not_found_error"
	case status == http.StatusServiceUnavailable:
		return "overloaded_error"
	case status >= 500:
		return "api_error"
	default:
		return "invalid_request_error"
	}
}
