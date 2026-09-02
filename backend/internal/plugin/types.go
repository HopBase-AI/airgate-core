package plugin

import (
	"time"

	"github.com/DouDOU-start/airgate-core/ent"
	"github.com/DouDOU-start/airgate-core/internal/auth"
	"github.com/DouDOU-start/airgate-core/internal/routing"
	"github.com/DouDOU-start/airgate-core/internal/scheduler"
	sdk "github.com/DouDOU-start/airgate-sdk/sdkgo"
)

// forwardState 一次转发请求在 Core 内的上下文。
// 跨 failover attempt 稳定的字段（body / model / keyInfo / plugin）+ 每次 attempt 会被覆盖的字段（account / requestID）。
type forwardState struct {
	// attemptNo 本次 callPlugin 是整条 failover 链里的第几次尝试(1-based)。
	// 经 X-Airgate-Attempt 头交给插件:插件据此在重试时放宽首字看门狗——首次 30s 快速识别真卡死,
	// 换号重试时上游缓存必然未命中、合法首字更慢,同样的 30s 会把「慢而活着」的请求连杀三次后 502。
	attemptNo   int
	startedAt   time.Time
	requestPath string
	requestID   string

	// grpcCallAt 最后一次（成功的）gRPC 调插件的时刻。与 startedAt 之差即
	// core 前置耗时（鉴权/余额/调度/闸门，含 failover 排队），用于 TTFT 分段。
	grpcCallAt time.Time

	body  []byte
	model string
	// usageModel is a semantic operation label declared by model-less routes.
	// It is only used as a failure-log fallback and never participates in routing.
	usageModel string
	// schedulingModels 是调度层使用的模型候选。协议翻译入口里，客户端传入的
	// model 可能不是上游真实模型，例如 OpenAI 插件的 /v1/messages 会把
	// claude-* 映射到 GPT 模型后再调用上游。
	schedulingModels []string
	schedulingModel  string
	stream           bool
	realtime         bool
	sessionID        string

	// 推理强度档位快照。
	reasoningEffort string
	accountReq      scheduler.AccountRequirements

	// 缓存的 image tool payload，避免 forwarder 热路径上重复反序列化 body
	imageToolPayloadValid bool
	imageToolPayload      imageToolPayload

	requestedPlatform string
	selectedRoute     routing.Candidate

	keyInfo *auth.APIKeyInfo
	plugin  *PluginInstance
	account *ent.Account
}

// forwardExecution 一次 plugin.Forward 调用的结果。
// err 仅表示"插件自身崩了"；业务判决全在 outcome.Kind。
type forwardExecution struct {
	outcome  sdk.ForwardOutcome
	err      error
	duration time.Duration
}

// clientErrorReplay 快照一次 ClientError 尝试的执行结果与归属上下文。
// failover 穷尽后回放该 4xx 时，计费与日志必须落在产生这份响应的账号/分组上，
// 而 state 里的这些字段可能已被后续 pickAccount / 路由切换覆盖。
type clientErrorReplay struct {
	execution forwardExecution
	account   *ent.Account
	keyInfo   *auth.APIKeyInfo
	route     routing.Candidate
}

// parsedRequest 从 JSON body 提取的请求元信息。
type parsedRequest struct {
	Model           string
	Stream          bool
	SessionID       string
	ReasoningEffort string // 推理强度档位

	// 缓存 image tool payload 解析结果，避免 requestNeedsImage / accountRequirementsForRequest 重复反序列化 body
	imageToolPayloadValid bool
	imageToolPayload      imageToolPayload
}

// requestFields 一次性 Unmarshal 的 JSON 字段结构。
type requestFields struct {
	Model    string `json:"model"`
	Stream   bool   `json:"stream"`
	Metadata struct {
		UserID string `json:"user_id"`
	} `json:"metadata"`
	ReasoningEffort string `json:"reasoning_effort"`
	Reasoning       *struct {
		Effort string `json:"effort"`
	} `json:"reasoning"`
	OutputConfig *struct {
		Effort string `json:"effort"`
	} `json:"output_config"`
	Thinking *struct{} `json:"thinking"`
}

func (s *forwardState) schedulingModelCandidates() []string {
	if s == nil {
		return nil
	}
	if len(s.schedulingModels) > 0 {
		return s.schedulingModels
	}
	if s.model == "" {
		return nil
	}
	return []string{s.model}
}

func (s *forwardState) modelForScheduling() string {
	if s == nil {
		return ""
	}
	if s.schedulingModel != "" {
		return s.schedulingModel
	}
	if len(s.schedulingModels) > 0 {
		return s.schedulingModels[0]
	}
	return s.model
}
