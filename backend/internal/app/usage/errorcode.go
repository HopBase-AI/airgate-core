package usage

// 失败分类。转发判决类的取 sdk.OutcomeKind.String()，与 SDK 保持一致；
// 其余是 Core 在打上游之前就拦掉的场景。
//
// 写入侧是转发管线（internal/plugin），读取侧是使用日志接口与前端文案，
// 因此常量放在领域包里由两边共用。新增取值须同步前端 i18n 的
// usage.error_code_* 键，否则前端只能回落到显示原始 code。
const (
	// ErrorCodeClientError 上游判定为客户端请求本身的问题（4xx）。
	// 与 sdk.OutcomeClientError.String() 取值一致。
	ErrorCodeClientError = "client_error"
	// ErrorCodeAccountRateLimited 上游账号被限流。
	ErrorCodeAccountRateLimited = "account_rate_limited"
	// ErrorCodeAccountDead 上游账号凭证失效/被封。
	ErrorCodeAccountDead = "account_dead"
	// ErrorCodeUpstreamTransient 上游抖动（5xx / 超时 / 连接失败）。
	ErrorCodeUpstreamTransient = "upstream_transient"
	// ErrorCodeStreamAborted 流式响应已开始写出后中断。
	ErrorCodeStreamAborted = "stream_aborted"

	// ErrorCodeInsufficientQuota 余额不足，请求未打上游。
	ErrorCodeInsufficientQuota = "insufficient_quota"
	// ErrorCodeInvalidRequest 请求体无法读取或格式不符合入口要求。
	ErrorCodeInvalidRequest = "invalid_request"
	// ErrorCodeRequestTooLarge 请求体超过 Core 的入口大小限制。
	ErrorCodeRequestTooLarge = "request_too_large"
	// ErrorCodeModelNotFound 请求模型不在该分组可服务范围内。
	ErrorCodeModelNotFound = "model_not_found"
	// ErrorCodeModelNotServed API Key 所属分组未提供请求模型。
	ErrorCodeModelNotServed = "model_not_served"
	// ErrorCodeGroupOffline API Key 所属分组已下线。
	ErrorCodeGroupOffline = "group_offline"
	// ErrorCodeConcurrencyLimit 用户或 API Key 并发已达上限。
	ErrorCodeConcurrencyLimit = "concurrency_limit"
	// ErrorCodeCapabilityDenied 分组不具备本次请求所需能力（如未开生图）。
	ErrorCodeCapabilityDenied = "capability_denied"
	// ErrorCodeRouteNotFound 已匹配平台，但该平台没有对应 API 路径。
	ErrorCodeRouteNotFound = "route_not_found"
	// ErrorCodePluginUnavailable 请求目标插件当前未运行。
	ErrorCodePluginUnavailable = "plugin_unavailable"
	// ErrorCodeMiddlewareDenied 请求被中间件策略明确拒绝。
	ErrorCodeMiddlewareDenied = "middleware_denied"
	// ErrorCodeNoAvailableRoute 分组内没有可用于本次请求的账号。
	ErrorCodeNoAvailableRoute = "no_available_route"
	// ErrorCodeNoAvailableAccount 候选账号全部不可用（已死/已禁用）。
	ErrorCodeNoAvailableAccount = "no_available_account"
	// ErrorCodeAllRoutesFailed 全部候选账号都失败且无更具体分类。
	ErrorCodeAllRoutesFailed = "all_routes_failed"
	// ErrorCodeAllRoutesRateLimited 全部候选账号都在限流冷却中。
	ErrorCodeAllRoutesRateLimited = "all_routes_rate_limited"
	// ErrorCodeUpstreamTimeout 上游请求超时。
	ErrorCodeUpstreamTimeout = "upstream_timeout"
	// ErrorCodeUpstreamError 上游服务不可用。
	ErrorCodeUpstreamError = "upstream_error"
	// ErrorCodePluginError 插件自身返回 error 且未声明判决。
	ErrorCodePluginError = "plugin_error"
	// ErrorCodeMetadataScopeFailed 元数据响应无法按分组权限收敛。
	ErrorCodeMetadataScopeFailed = "metadata_scope_failed"
	// ErrorCodeClientCanceled 客户端在请求完成前主动断开。
	ErrorCodeClientCanceled = "client_canceled"
	// ErrorCodeRequestTimeout 请求上下文在完成前超时。
	ErrorCodeRequestTimeout = "request_timeout"
)

// ErrorMessageVisibleToUser 失败原因能否把原文展示给终端用户。
//
// 客户端自身的错误（参数非法、模型不支持、余额不足、超并发）本来就会随响应体
// 原样返回给调用方，展示在使用日志里不增加信息泄露；上游账号/服务类故障属于内部
// 细节（可能夹带上游账号信息），用户侧只给分类。
func ErrorMessageVisibleToUser(code string) bool {
	switch code {
	case ErrorCodeClientError,
		ErrorCodeInvalidRequest,
		ErrorCodeRequestTooLarge,
		ErrorCodeModelNotFound,
		ErrorCodeModelNotServed,
		ErrorCodeGroupOffline,
		ErrorCodeInsufficientQuota,
		ErrorCodeCapabilityDenied,
		ErrorCodeConcurrencyLimit,
		ErrorCodeRouteNotFound,
		ErrorCodeMiddlewareDenied,
		ErrorCodeClientCanceled,
		ErrorCodeRequestTimeout:
		return true
	default:
		return false
	}
}
