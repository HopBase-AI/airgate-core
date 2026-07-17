package plugin

import (
	"fmt"

	"github.com/DouDOU-start/airgate-core/internal/scheduler"
)

// precheckModelServed 转发入口的「模型-分组预校验」：请求模型明确不在该分组可服务
// 范围时快速拦截（调用方回 404），不再打上游、不再进入调度/failover 链路空转出 503。
//
// 返回 (拦截原因, 是否放行)。整体 fail-open：任何"不确定"的场景一律放行，交给
// 后续调度与上游判决——预校验只拦"确定不可能被服务"的请求。
//
// 判定用调度候选（schedulingModelCandidates）而非裸 state.model：协议翻译入口
// （如 OpenAI 插件的 /v1/messages）会把 claude-* 翻译成 gpt-* 再调度，
// 用裸名校验会误杀这类请求。
//
// fail-open 边界（按顺序）：
//  1. model 为空 / 无调度候选 → 放行（非常规请求体，交给插件处理）；
//  2. 平台模型目录未就绪（插件刚启动、目录未推送）→ 放行；
//  3. 分组配置了 model_routing：任一候选命中（语义同调度层 applyModelRouting）→ 放行；
//     全部未命中 → 拦（主目标：GLM 分组请求 gpt-5.5 得到清晰 404 而非 503）；
//  4. 分组未配 model_routing：任一候选在本平台目录 → 放行；候选全不在本平台时，
//     仅当至少一个候选能在其他平台目录中找到（跨平台错配，如 claude 分组请求
//     gpt-5.5）才拦；哪个平台目录都查不到的未知模型（如 codex-auto-review，
//     上游实际在服务、走关键字兜底计费）必须放行——严禁拦目录外未知模型。
func (f *Forwarder) precheckModelServed(state *forwardState) (string, bool) {
	if state == nil || state.keyInfo == nil || f.manager == nil {
		return "", true
	}
	if state.model == "" {
		return "", true
	}
	candidates := state.schedulingModelCandidates()
	if len(candidates) == 0 {
		return "", true
	}

	// 目标平台以匹配到的插件为准（X-Airgate-Platform 指定时两者一致），
	// 兜底用 parseRequest 解析的 requestedPlatform。
	platform := state.requestedPlatform
	if state.plugin != nil && state.plugin.Platform != "" {
		platform = state.plugin.Platform
	}
	// 平台不明或目录未就绪 → fail-open
	if platform == "" || !f.manager.HasModelsForPlatform(platform) {
		return "", true
	}

	blockReason := fmt.Sprintf("当前分组不支持所请求的模型: %s", state.model)

	// 分支 a：分组配置了 model_routing —— 以路由规则为准（与调度层同一匹配源）。
	if routing := state.keyInfo.GroupModelRouting; len(routing) > 0 {
		for _, cand := range candidates {
			if scheduler.ModelRoutingServes(routing, cand) {
				return "", true
			}
		}
		return blockReason, false
	}

	// 分支 b：未配 model_routing —— 以平台目录为准。
	for _, cand := range candidates {
		if f.manager.PlatformHasModel(platform, cand) {
			return "", true
		}
	}
	// 候选全不在本平台目录：仅当能在其他平台目录中找到（跨平台错配）才拦；
	// 全平台未知的模型放行（现网存在目录外真实流量）。
	for _, cand := range candidates {
		if p := f.manager.FindPlatformByModelFold(cand); p != "" && p != platform {
			return blockReason, false
		}
	}
	return "", true
}
