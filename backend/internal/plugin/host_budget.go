package plugin

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/DouDOU-start/airgate-core/ent"
	enttask "github.com/DouDOU-start/airgate-core/ent/task"
	"github.com/DouDOU-start/airgate-core/ent/user"
	"github.com/DouDOU-start/airgate-core/internal/auth"
	"github.com/DouDOU-start/airgate-core/internal/billing"
	"github.com/DouDOU-start/airgate-core/internal/routing"
	sdk "github.com/DouDOU-start/airgate-sdk/sdkgo"
)

// host_budget.go —— 异步任务提交侧的「预算预留」门禁。
//
// 视频类任务是后付费的：钱在上游出片、结算写 usage 那一刻才扣。提交口不拦，用户就能在
// 几分钟里连提数条把余额打穿——2026-09-04 某用户充 $50 后 7 分钟内提交三条 Seedance
// （$21 + $14 + $21），全部完成，余额落到 −$20。
//
// 做法是提交时一次性判定：
//
//	可用 − Σ(在途预估) − 本条预估 ≥ 0
//
// 「在途预留」不另建台账——非终态任务行上的 estimated_cost 就是预留，任务进终态即自然
// 释放。代价是卡死的任务会把预留一直占着，所以必须有 task_stale_sweep.go 的兜底扫描。
// 批量提交只要钱够就照样放行：这里拦的是超额，不是并发。

// budgetCurrencyUSD 余额、预留、预估统一按美元记（与 users.balance 同单位）。
const budgetCurrencyUSD = "USD"

// budgetDecision 预算判定结果。纯计算、不碰 DB，让 gateway.forward 的门禁与
// billing.budget 的查询共用同一套算术和同一份文案，不会出现「查得到、提交被拒」的错位。
type budgetDecision struct {
	Balance        float64
	Reserved       float64
	Limited        bool
	QuotaRemaining float64
	Estimate       float64
	// Available = min(余额, 成员本期剩余额度) − 在途预留；可能为负。
	Available  float64
	Sufficient bool
	// Message 仅在不足时非空，直接面向终端用户。
	Message string
}

// evaluateBudget 余额与成员额度两道口子都要过：企业主余额够但成员本期额度不够，
// 一样得拦——否则成员额度形同虚设。
func evaluateBudget(balance, reserved float64, limited bool, quotaRemaining, estimate float64) budgetDecision {
	d := budgetDecision{
		Balance:        balance,
		Reserved:       reserved,
		Limited:        limited,
		QuotaRemaining: quotaRemaining,
		Estimate:       estimate,
	}
	ceiling := balance
	if limited && quotaRemaining < ceiling {
		ceiling = quotaRemaining
	}
	d.Available = ceiling - reserved

	balanceShort := balance-reserved-estimate < 0
	quotaShort := limited && quotaRemaining-reserved-estimate < 0
	d.Sufficient = !balanceShort && !quotaShort
	switch {
	case balanceShort:
		d.Message = insufficientBudgetMessage(false, balance, reserved, estimate)
	case quotaShort:
		d.Message = insufficientBudgetMessage(true, quotaRemaining, reserved, estimate)
	}
	return d
}

// insufficientBudgetMessage 拒绝文案。三个数字都要露出来——只说「余额不足」用户会以为
// 是系统问题（他刚充过钱），把在途预留摆出来才解释得清钱去哪了。
func insufficientBudgetMessage(quotaCase bool, avail, reserved, estimate float64) string {
	prefix := "余额不足"
	if quotaCase {
		prefix = "额度不足"
	}
	return fmt.Sprintf("%s：可用 $%.2f，在途预留 $%.2f，本条预估 $%.2f", prefix, avail, reserved, estimate)
}

// reservedInFlightCost 汇总某账号名下所有非终态任务的预估 = 在途预留。
//
// excludeTaskID > 0 时排除自身：任务行常常先建、再转发提交，不排除会把自己算两遍。
// taskInFlightStatuses 「在途」= 全部非终态状态,与 store 层既有口径一致
// (generationtask_store 的活跃任务集合也是这四个)。
//
// retrying 必须算在内:插件单次 attempt 到点让位重排队时任务就落在 retrying,
// 视频任务大部分寿命都在这个状态上(2026-09-04 那条 8 次 attempt 的 Seedance 视频即是)。
// 漏掉它等于重试窗口内的预留凭空消失,用户可以在这个缝里继续超额提交。
// cancelling 同理:取消没走完之前上游仍可能出账。
var taskInFlightStatuses = []enttask.Status{
	enttask.StatusPending,
	enttask.StatusProcessing,
	enttask.StatusRetrying,
	enttask.StatusCancelling,
}

// 这里取行在 Go 里累加而不是 SQL SUM——非终态任务数被 24h sweep 兜住（量级是个位数），
// 而 SUM 无行时返回 NULL，扫进 float64 会直接报错。
func (h *HostService) reservedInFlightCost(ctx context.Context, userID int, excludeTaskID int64) (float64, error) {
	if h.db == nil || userID <= 0 {
		return 0, nil
	}
	q := h.db.Task.Query().Where(
		enttask.UserIDEQ(userID),
		enttask.StatusIn(taskInFlightStatuses...),
	)
	if excludeTaskID > 0 {
		q = q.Where(enttask.IDNEQ(int(excludeTaskID)))
	}
	values, err := q.Select(enttask.FieldEstimatedCost).Float64s(ctx)
	if err != nil {
		return 0, err
	}
	total := 0.0
	for _, v := range values {
		if v > 0 {
			total += v
		}
	}
	return total, nil
}

// checkSubmissionBudget 新提交的预算门禁；只在自动选组的新提交上跑，钉选账号的后续
// 请求（查询进度 / 取产物 / 结算）一律不拦——那笔钱在提交时就已经决定花了。
//
// rate 取路由首候选的 EffectiveRate：用户价 = 官方价 × 分组倍率，与真正落账的口径一致。
func (h *HostService) checkSubmissionBudget(ctx context.Context, req *hostForwardRequest, rate float64) error {
	if req == nil || req.EstimatedOfficialCost <= 0 {
		return nil
	}
	if rate <= 0 {
		rate = 1
	}
	estimate := req.EstimatedOfficialCost * rate

	// 预留按「发起提交的那个账号」统计：成员账号的 req.UserID 在身份解析时已被改写成
	// 企业主，而任务行的 user_id 记的仍是成员本人，按企业主查会一条都查不到。
	submitter := req.submitterID
	if submitter <= 0 {
		submitter = int(req.UserID)
	}
	reserved, err := h.reservedInFlightCost(ctx, submitter, req.TaskID)
	if err != nil {
		if cerr := hostContextError(err); cerr != nil {
			return cerr
		}
		slog.Error("host_forward_budget_reserved_query_failed",
			sdk.LogFieldUserID, req.UserID, "task_id", req.TaskID, sdk.LogFieldError, err)
		return hostForwardGenericError()
	}

	u, err := h.db.User.Query().Where(user.IDEQ(int(req.UserID))).Only(ctx)
	if err != nil {
		if cerr := hostContextError(err); cerr != nil {
			return cerr
		}
		if ent.IsNotFound(err) {
			return status.Error(codes.NotFound, "用户不存在")
		}
		slog.Error("host_forward_budget_user_lookup_failed",
			sdk.LogFieldUserID, req.UserID, sdk.LogFieldError, err)
		return hostForwardGenericError()
	}

	limited, quotaRemaining := false, 0.0
	if req.member != nil {
		quotaRemaining, limited = auth.MemberRemainingQuota(req.member, time.Now())
	}

	decision := evaluateBudget(u.Balance, reserved, limited, quotaRemaining, estimate)
	if !decision.Sufficient {
		slog.Warn("host_forward_budget_rejected",
			sdk.LogFieldUserID, req.UserID,
			"task_id", req.TaskID,
			"available", decision.Available,
			"reserved", reserved,
			"estimate", estimate,
		)
		return status.Error(codes.ResourceExhausted, decision.Message)
	}

	// 过闸后立刻把预估写回任务行——这一行就是预留，下一条提交据此累加。
	h.persistTaskEstimatedCost(ctx, req.TaskID, estimate)
	return nil
}

// persistTaskEstimatedCost 尽力而为：写失败只记日志。已经判定钱够的提交不该因为
// 记账失败而被拒——那会让用户对着一条明明能跑的任务干瞪眼。
func (h *HostService) persistTaskEstimatedCost(ctx context.Context, taskID int64, estimate float64) {
	if h.db == nil || taskID <= 0 {
		return
	}
	if err := h.db.Task.UpdateOneID(int(taskID)).SetEstimatedCost(estimate).Exec(ctx); err != nil {
		slog.Warn("host_forward_budget_persist_failed",
			"task_id", taskID, "estimate", estimate, sdk.LogFieldError, err)
	}
}

// hostBillingBudgetRequest billing.budget 的入参。
//
// estimated_official_cost 是插件按自己价目表算出的**官方基准价**（倍率前），
// core 负责按路由倍率换算成用户价——倍率的权威在 core，插件不该也算不准。
type hostBillingBudgetRequest struct {
	UserID   int64  `json:"user_id"`
	Platform string `json:"platform"`
	// GroupID > 0 时按该分组的倍率算；否则取自动选组的首候选（与 forward 同口径）。
	GroupID               int64   `json:"group_id"`
	EstimatedOfficialCost float64 `json:"estimated_official_cost"`
}

// billingBudget 提交前的预算体检：插件/前端拿它做「还能不能提交」的预判与文案提示，
// 真正的拦截仍在 gateway.forward 里（这里查完到那里提交之间会有并发窗口）。
func (h *HostService) billingBudget(ctx context.Context, req hostBillingBudgetRequest) (map[string]interface{}, error) {
	if req.UserID <= 0 {
		return nil, status.Error(codes.InvalidArgument, "user_id 必须 > 0")
	}
	identity, err := auth.ResolveTeamIdentity(ctx, h.db, int(req.UserID))
	if err != nil {
		if cerr := hostContextError(err); cerr != nil {
			return nil, cerr
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	// 成员账号：钱看企业主，额度看成员本期，分组受成员白名单约束。
	billingUserID := int(req.UserID)
	var allowedGroups []int64
	limited, quotaRemaining := false, 0.0
	if identity.IsMember() {
		billingUserID = identity.Owner.ID
		allowedGroups = identity.Member.AllowedGroupIds
		quotaRemaining, limited = auth.MemberRemainingQuota(identity.Member, time.Now())
	}

	u, err := h.db.User.Query().Where(user.IDEQ(billingUserID)).Only(ctx)
	if err != nil {
		if cerr := hostContextError(err); cerr != nil {
			return nil, cerr
		}
		if ent.IsNotFound(err) {
			return nil, status.Error(codes.NotFound, "用户不存在")
		}
		return nil, status.Error(codes.Internal, err.Error())
	}

	// 在途预留按调用方传进来的账号统计（任务行 user_id 记的是提交人，成员就是成员本人）。
	reserved, err := h.reservedInFlightCost(ctx, int(req.UserID), 0)
	if err != nil {
		if cerr := hostContextError(err); cerr != nil {
			return nil, cerr
		}
		return nil, status.Error(codes.Internal, err.Error())
	}

	estimate := 0.0
	if req.EstimatedOfficialCost > 0 {
		rate, err := h.resolveBudgetRate(ctx, u, req.Platform, req.GroupID, allowedGroups)
		if err != nil {
			return nil, err
		}
		estimate = req.EstimatedOfficialCost * rate
	}

	decision := evaluateBudget(u.Balance, reserved, limited, quotaRemaining, estimate)
	return map[string]interface{}{
		"balance":         u.Balance,
		"reserved":        reserved,
		"available":       decision.Available,
		"currency":        budgetCurrencyUSD,
		"limited":         limited,
		"quota_remaining": quotaRemaining,
		"estimate":        estimate,
		"sufficient":      decision.Sufficient,
		"message":         decision.Message,
	}, nil
}

// resolveBudgetRate 与 hostForwardRoutes 同口径地取倍率，否则预判出来的数会和提交时
// 实际用的不一样，用户就会看到「查着够、提交被拒」。
func (h *HostService) resolveBudgetRate(ctx context.Context, u *ent.User, platform string, groupID int64, allowedGroups []int64) (float64, error) {
	if groupID > 0 {
		g, err := h.db.Group.Get(ctx, int(groupID))
		if err != nil {
			if cerr := hostContextError(err); cerr != nil {
				return 0, cerr
			}
			if ent.IsNotFound(err) {
				return 0, status.Error(codes.NotFound, "分组不存在")
			}
			return 0, status.Error(codes.Internal, err.Error())
		}
		return billing.ResolveBillingRateForGroup(u.GroupRates, g.ID, g.RateMultiplier), nil
	}
	if strings.TrimSpace(platform) == "" {
		return 0, status.Error(codes.InvalidArgument, "platform 不能为空")
	}
	routes, err := routing.ListEligibleGroups(ctx, h.db, u.ID, platform, u.GroupRates, u.GroupPluginSettings, routing.Requirements{})
	if err != nil {
		if cerr := hostContextError(err); cerr != nil {
			return 0, cerr
		}
		return 0, status.Error(codes.Internal, err.Error())
	}
	routes = filterCandidatesByMemberGroups(routes, allowedGroups)
	if len(routes) == 0 {
		return 0, status.Error(codes.FailedPrecondition, "没有可用的分组")
	}
	return routes[0].EffectiveRate, nil
}
