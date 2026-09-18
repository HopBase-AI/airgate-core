package plugin

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"

	appusage "github.com/DouDOU-start/airgate-core/internal/app/usage"
	"github.com/DouDOU-start/airgate-core/internal/i18n"
	sdk "github.com/DouDOU-start/airgate-sdk/sdkgo"
)

// session_affinity.go —— Responses API 续聊的账号亲和。
//
// `previous_response_id` 是 OpenAI Responses API 的标准续聊锚点：上游把上一轮的
// 会话状态存在**自己那边**（火山方舟 store 默认 true、expire_at 默认 3 天），
// 客户端下一轮只递一个 id。它因此不只是请求体里的一个字段，而是一条调度约束——
// 这一轮必须回到产出那条 response 的账号，换号只会拿到 not found。
//
// 历史做法是让网关把这个字段直接剔掉：客户端拿到 200、却完全没有记忆，没有任何
// 提示，极难自查。现在改为按上游能力放行（插件侧按账号判定是否透传），由 core 负责
// 把带 id 的后续请求钉回原账号：
//
//   - 绑定写入：插件在拿到上游 response id 后经 Host `scheduler.bind_response_account`
//     登记（见 host_service.go）；存储复用 StickySession，只加 `resp:` 命名空间。
//   - 绑定读取：本文件。命中就把 AccountRequirements.PinnedAccountID 收敛成一个账号。
//   - 钉不住：明确失败（gw.session_account_unavailable，五语），绝不静默换号——
//     「200 但丢了上下文」是最难排查的一类故障。
//
// 没有绑定记录时不钉：可能是绑定过期，也可能是那一轮走的是不透传的账号（此时
// 插件本来就会把这个字段剥掉，行为与改动前一致）。保持正常调度，不引入回归。

// resolveResponseAffinity 解析本次请求的会话亲和目标账号。
// 无 previous_response_id、或没有绑定记录时不做任何事（state.pinnedAccountID 保持 0）。
func (f *Forwarder) resolveResponseAffinity(ctx context.Context, state *forwardState) {
	if state == nil || state.previousResponseID == "" || f.scheduler == nil || state.keyInfo == nil {
		return
	}
	accountID, found := f.scheduler.ResponseAffinityAccount(
		ctx,
		state.keyInfo.UserID,
		state.requestedPlatform,
		state.previousResponseID,
	)
	if !found || accountID <= 0 {
		return
	}
	state.pinnedAccountID = accountID
	state.accountReq.PinnedAccountID = accountID
}

// writeSessionAffinityUnavailable 会话钉住的账号本次拿不到时的对外报错。
//
// 不能回落到别的账号：上游那边的会话状态只在原账号上，换号等于悄悄清空上下文。
// 也不能沿用「全部上游失败、请稍后重试」——客户重试多少次都不会好，必须告诉他
// 这轮会话需要重开（不要再带 previous_response_id）。
// 文案走 gw.* 五语，且只说"会话"，不透露账号、上游或分组。
func writeSessionAffinityUnavailable(c *gin.Context) {
	const msgKey = "gw.session_account_unavailable"
	if streamHeartbeatOnlyWritten(c) {
		protocolStreamError(c, http.StatusServiceUnavailable, "server_error",
			appusage.ErrorCodeNoAvailableAccount, i18n.Tc(c, msgKey))
		return
	}
	protocolError(c, http.StatusServiceUnavailable, "server_error",
		appusage.ErrorCodeNoAvailableAccount, i18n.Tc(c, msgKey))
}

// sessionAffinityFailureUsage 落库记录（统一英文，与其它失败口径一致）。
func sessionAffinityFailureUsage() usageFailure {
	return usageFailure{
		code:    appusage.ErrorCodeNoAvailableAccount,
		status:  http.StatusServiceUnavailable,
		message: i18n.En("gw.session_account_unavailable_detail"),
	}
}

// logSessionAffinityPinned 记录一次会话亲和命中，便于事后核对「这一轮确实回到了原账号」。
func logSessionAffinityPinned(state *forwardState) []any {
	return []any{
		sdk.LogFieldAccountID, state.pinnedAccountID,
		sdk.LogFieldPlatform, state.requestedPlatform,
	}
}
