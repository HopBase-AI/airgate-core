package handler

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"time"

	appaccount "github.com/DouDOU-start/airgate-core/internal/app/account"
	"github.com/DouDOU-start/airgate-core/internal/scheduler"
	"github.com/DouDOU-start/airgate-core/internal/server/dto"
)

// AccountHandler 上游账号管理 Handler。
//
// scheduler / concurrency 用来读 Redis 侧动态调度状态。后台列表会带上展示用
// family_cooldowns 和生产审计用 runtime_telemetry；依赖缺失时审计状态显式 unavailable。
type AccountHandler struct {
	service     *appaccount.Service
	scheduler   *scheduler.Scheduler
	concurrency *scheduler.ConcurrencyManager
}

// NewAccountHandler 创建 AccountHandler。动态依赖可为 nil；对应遥测会显式标为 unavailable。
func NewAccountHandler(service *appaccount.Service, sched *scheduler.Scheduler, concurrency *scheduler.ConcurrencyManager) *AccountHandler {
	return &AccountHandler{service: service, scheduler: sched, concurrency: concurrency}
}

// familyCooldownsFor 拉取指定账号在 Redis 上仍生效的家族冷却，转成 DTO。
// scheduler 为 nil 或没有冷却时返回 nil；不阻断主响应。
func (h *AccountHandler) familyCooldownsFor(ctx context.Context, accountID int) []dto.FamilyCooldownDTO {
	if h.scheduler == nil {
		return nil
	}
	entries := h.scheduler.ListFamilyCooldowns(ctx, accountID)
	if len(entries) == 0 {
		return nil
	}
	out := make([]dto.FamilyCooldownDTO, 0, len(entries))
	for _, e := range entries {
		out = append(out, dto.FamilyCooldownDTO{
			Family: e.Family,
			Until:  e.Until.UTC().Format(time.RFC3339),
			Reason: e.Reason,
		})
	}
	return out
}

func runtimeTime(value *time.Time) *string {
	if value == nil {
		return nil
	}
	formatted := value.UTC().Format(time.RFC3339Nano)
	return &formatted
}

// attachRuntimeTelemetry 叠加生产审计所需的权威 Redis 快照。
// 管理列表本身继续返回；任一动态读取失败时用 unavailable 明确阻止审计误判。
func (h *AccountHandler) attachRuntimeTelemetry(ctx context.Context, accountID int, resp *dto.AccountResp) {
	telemetry := &dto.RuntimeTelemetryDTO{
		ConcurrencyStatus: "unavailable",
		FamilyGatesStatus: "unavailable",
	}
	resp.RuntimeTelemetry = telemetry
	resp.FamilyGates = make([]dto.FamilyGateDTO, 0)

	if h.concurrency != nil {
		if count, err := h.concurrency.GetCurrentCountAuthoritative(ctx, accountID); err == nil {
			resp.CurrentConcurrency = count
			telemetry.ConcurrencyStatus = "ok"
		}
	}

	if h.scheduler == nil {
		return
	}
	gates, err := h.scheduler.ListFamilyGatesAuthoritative(ctx, accountID)
	if err != nil {
		return
	}
	telemetry.FamilyGatesStatus = "ok"
	for _, gate := range gates {
		resp.FamilyGates = append(resp.FamilyGates, dto.FamilyGateDTO{
			Family:        gate.Family,
			Phase:         gate.Phase,
			Kind:          gate.Kind,
			Reason:        gate.Reason,
			Until:         runtimeTime(gate.Until),
			ProbeInFlight: gate.ProbeInFlight,
			ProbeUntil:    runtimeTime(gate.ProbeUntil),
		})
	}
}

// parseAccountID 解析账号 ID，委托给公共 ParseID。
var parseAccountID = ParseID

func parseOptionalInt(raw string) *int {
	if raw == "" {
		return nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return nil
	}
	return &value
}

func parseOptionalBool(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "y", "on":
		return true
	default:
		return false
	}
}

// parseIDList 解析逗号分隔的整数列表（如 "1,2,3"），忽略空项与非法项。
func parseIDList(raw string) []int {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	ids := make([]int, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if v, err := strconv.Atoi(p); err == nil {
			ids = append(ids, v)
		}
	}
	return ids
}

func (h *AccountHandler) handleError(logMessage, publicMessage string, err error) (int, string) {
	switch {
	case errors.Is(err, appaccount.ErrAccountNotFound):
		return 404, err.Error()
	case errors.Is(err, appaccount.ErrPluginNotFound):
		return 500, err.Error()
	case errors.Is(err, appaccount.ErrReauthRequired):
		// 这里的"需要重新授权"说的是**上游账号**（OAuth）的凭证失效，不是当前
		// 登录用户的 session。绝对不能返回 401——前端 HTTP 客户端有全局拦截，
		// 看到 401 会把当前管理员踹出登录页。用 422 语义最贴切：请求合法但
		// 因账号状态无法处理。
		return 422, err.Error()
	case errors.Is(err, appaccount.ErrModelRequired),
		errors.Is(err, appaccount.ErrQuotaRefreshUnsupported),
		errors.Is(err, appaccount.ErrInvalidDateRange),
		errors.Is(err, appaccount.ErrInvalidConnectivityTestMode):
		return 400, err.Error()
	default:
		slog.Error(logMessage, "error", err)
		return 500, publicMessage
	}
}
