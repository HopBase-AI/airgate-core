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
		// 插件任务的可行动失败码同口径放行（参数非法 / 素材问题 / 内容审核 / 余额）。
		return PluginClientActionableErrorCode(code)
	}
}

// pluginClientActionableErrorCodes 插件（视频/生图类任务）写回的失败码里，属于
// 「调用方自己能改对」的那一类：参数不被支持、参考素材有问题、提示词为空、内容
// 审核未过、余额不足、提交被拒。
//
// 为什么要单独登记：这些码由插件产生，core 的常量表里没有，于是过去一律落进
// default 分支 → 用户侧既拿不到原文、前端也只显示一个中性的「服务繁忙,请稍后
// 重试」。2026-09-18 生产上就有客户连续三次提交同一组非法参数：每次都被上游按
// InvalidParameter 拒掉，而使用记录只告诉他「服务繁忙」，于是原样重试。
//
// 判据是「这条信息能不能指导用户改下一次请求」，不是「谁的锅」。上游账号/调度/
// 服务故障仍然只给分类，原文不出网（见 ErrorMessageVisibleToUser 的注释）。
// 新增取值要同步前端 ERROR_CODE_META 与 usage.error_hint_* 文案。
var pluginClientActionableErrorCodes = map[string]struct{}{
	// 上游在提交/生成阶段判定请求参数非法
	"upstream_invalid_request": {},
	"submission_rejected":      {},
	"upstream_submit_rejected": {},
	"bad_request":              {},
	"invalid_asset_duration":   {},
	// 内容审核（输入与输出两侧）
	"safety_rejected":        {},
	"input_sensitive":        {},
	"output_video_sensitive": {},
	"output_video_copyright": {},
	"output_audio_sensitive": {},
	"output_audio_copyright": {},
	// 提示词 / 参考素材
	"prompt_required":             {},
	"missing_prompt":              {},
	"reference_image_invalid":     {},
	"reference_input_invalid":     {},
	"reference_image_required":    {},
	"reference_image_unsupported": {},
	"reference_media_unsupported": {},
	"reference_image_too_many":    {},
	"too_many_images":             {},
	"mask_unsupported":            {},
	// 模型 / 任务类型 / 分组选择
	"unsupported_model":     {},
	"model_not_in_catalog":  {},
	"wrong_model_kind":      {},
	"unsupported_task_type": {},
	"group_missing":         {},
	"missing_billing_group": {},
	// 余额预检与用户主动取消
	"insufficient_balance": {},
	"task_canceled":        {},
}

// PluginClientActionableErrorCode 该失败码是否属于插件侧「调用方可自行改对」的一类。
func PluginClientActionableErrorCode(code string) bool {
	_, ok := pluginClientActionableErrorCodes[code]
	return ok
}
