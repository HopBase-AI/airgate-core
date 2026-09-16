package plugin

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/DouDOU-start/airgate-core/ent"
	enttask "github.com/DouDOU-start/airgate-core/ent/task"
	appusage "github.com/DouDOU-start/airgate-core/internal/app/usage"
	"github.com/DouDOU-start/airgate-core/internal/auth"
	"github.com/DouDOU-start/airgate-core/internal/billing"
	sdk "github.com/DouDOU-start/airgate-sdk/sdkgo"
)

// task_failure_usage.go —— 任务失败终态 → 零费用使用记录。
//
// 同步转发路径早就给失败请求落零费用使用记录（usage_failure.go），但工作坊与异步 API
// 任务（生图 / 视频）失败时此前只写 tasks 表：客户在「使用记录」里一条失败都看不到，
// 部门负责人与企业主更看不到（2026-09-16 testbird 反馈：73 条失败任务、0 条记录）。
// 这里把异步任务对齐同一口径：任何任务进入 failed 终态，就落一条 status=error、
// 费用全零的记录。零费用不触发扣费与配额累加（recorder 的 ActualCost>0 守卫）。
//
// 归属：任务行 user_id 记的是提交人（成员登录账号或企业主本人），落库前经
// ResolveTeamIdentity 映射成「企业主 + member_id + department_id」——与成功记录同一套
// 快照列，成员本人 / 部门负责人 / 企业主三种视角按既有谓词都能看到。

const (
	// taskFailureFallbackMessage 任务失败但没有留下任何文案时的兜底（英文，落库口径）。
	taskFailureFallbackMessage = "task failed"
	// taskFailureEndpointPrefix 失败记录的 endpoint：没有真实请求路径，用任务类型标识来源。
	taskFailureEndpointPrefix = "task:"
)

// RecordTaskFailureUsage 按任务 id 落失败记录：重新读取任务行，确认已是 failed 终态再写。
// 供 Manager 的各失败落点调用——它们用 CAS 更新，手里只有更新前的旧对象。
func (h *HostService) RecordTaskFailureUsage(ctx context.Context, taskID int) {
	if h == nil || h.db == nil || taskID <= 0 {
		return
	}
	t, err := h.db.Task.Get(ctx, taskID)
	if err != nil {
		slog.Warn("task_failure_usage_load_failed", "task_id", taskID, sdk.LogFieldError, err)
		return
	}
	h.recordTaskFailureUsage(ctx, t)
}

// recordTaskFailureUsage 给已进入 failed 终态的任务落一条零费用使用记录。
func (h *HostService) recordTaskFailureUsage(ctx context.Context, t *ent.Task) {
	if h == nil || h.recorder == nil || h.db == nil {
		return
	}
	record, ok := h.buildTaskFailureUsage(ctx, t)
	if !ok {
		return
	}
	h.recorder.Record(record)
	slog.Info("task_failure_usage_recorded",
		"task_id", t.ID, sdk.LogFieldPluginID, t.PluginID, "task_type", t.TaskType,
		sdk.LogFieldUserID, record.UserID, "member_id", record.MemberID, "department_id", record.DepartmentID,
		"error_code", record.ErrorCode)
}

// skipTaskFailureUsage 不该留记录的任务：
//   - 非 failed 终态；
//   - 已挂有计费记录（失败前已结算，记录本身就在）；
//   - 审计用途的任务行（seedance 的 video.attempt，attributes.audit_only）；
//   - 只负责镜像影子任务的工作坊任务（execution.shadow_public_id 非空）：影子任务自己失败时
//     已经落过一条，镜像再落就是同一次失败两条记录。提交期失败（还没拿到影子 id）照常记录。
func skipTaskFailureUsage(t *ent.Task) bool {
	if t == nil || t.Status != enttask.StatusFailed {
		return true
	}
	if t.UsageID != nil && *t.UsageID > 0 {
		return true
	}
	if audit, ok := t.Attributes["audit_only"].(bool); ok && audit {
		return true
	}
	if strings.TrimSpace(stringValue(t.Execution["shadow_public_id"])) != "" {
		return true
	}
	return false
}

// buildTaskFailureUsage 组装失败记录；ok=false 表示该任务不该留记录或归属解析失败。
func (h *HostService) buildTaskFailureUsage(ctx context.Context, t *ent.Task) (billing.UsageRecord, bool) {
	if skipTaskFailureUsage(t) {
		return billing.UsageRecord{}, false
	}
	if t.UserID <= 0 {
		return billing.UsageRecord{}, false
	}

	billingUserID := t.UserID
	memberID, departmentID := 0, 0
	var userEmail string
	identity, err := auth.ResolveTeamIdentity(ctx, h.db, t.UserID)
	if err != nil {
		slog.Warn("task_failure_usage_identity_failed", "task_id", t.ID, sdk.LogFieldUserID, t.UserID, sdk.LogFieldError, err)
		return billing.UsageRecord{}, false
	}
	if identity.IsMember() {
		billingUserID = identity.Owner.ID
		memberID = identity.Member.ID
		if identity.Department != nil {
			departmentID = identity.Department.ID
		}
		userEmail = identity.Owner.Email
	} else if u, err := h.db.User.Get(ctx, t.UserID); err == nil {
		userEmail = u.Email
	}

	message, _ := taskFailureMessage(t.ErrorMessage)
	if message == "" {
		message = taskFailureFallbackMessage
	}
	code := strings.TrimSpace(t.ErrorCode)
	if code == "" {
		code = appusage.ErrorCodePluginError
	}

	record := billing.UsageRecord{
		UserID:       billingUserID,
		UserEmail:    userEmail,
		MemberID:     memberID,
		DepartmentID: departmentID,
		APIKeyID:     taskIntField(t.Execution, "api_key_id"),
		AccountID:    taskIntField(t.Execution, "account_id"),
		GroupID:      firstPositive(taskIntField(t.Execution, "group_id"), taskIntField(t.Input, "group_id")),
		Platform:     h.taskFailurePlatform(t),
		Model:        taskFailureModel(t),
		DurationMs:   taskElapsedMs(t),
		Endpoint:     taskFailureEndpointPrefix + t.TaskType,
		UsageMetadata: map[string]string{
			"task_id":   strconv.Itoa(t.ID),
			"task_type": t.TaskType,
			"plugin_id": t.PluginID,
		},
		Status:       billing.UsageStatusError,
		ErrorCode:    code,
		ErrorStatus:  taskFailureStatus(code, t.ErrorType),
		ErrorMessage: message,
	}
	return record, true
}

// taskFailurePlatform 平台：任务属性里的 platform 优先（工作坊任务都带），否则按插件实例，
// 最后退到插件 id 去掉 gateway- 前缀（recorder 的 Platform 列不能为空）。
func (h *HostService) taskFailurePlatform(t *ent.Task) string {
	if p := strings.TrimSpace(stringValue(t.Attributes["platform"])); p != "" {
		return p
	}
	if h.manager != nil {
		if inst := h.manager.GetInstance(t.PluginID); inst != nil && inst.Platform != "" {
			return inst.Platform
		}
	}
	return strings.TrimPrefix(t.PluginID, "gateway-")
}

// taskFailureModel 模型：attributes.model → input.model → execution.model；都没有交 recorder 兜底。
func taskFailureModel(t *ent.Task) string {
	for _, source := range []map[string]interface{}{t.Attributes, t.Input, t.Execution} {
		if m := strings.TrimSpace(stringValue(source["model"])); m != "" {
			return m
		}
	}
	return ""
}

// taskFailureStatus 失败记录的 error_status：任务没有上游 HTTP 状态可记，按错误分类给一个
// 与同步路径口径一致的近似值（客户端/内容类 400、限流 429、鉴权 401、卡死 504、其余 502）。
func taskFailureStatus(code, errorType string) int {
	switch code {
	case "safety_rejected", "bad_request", "insufficient_balance",
		appusage.ErrorCodeClientError, appusage.ErrorCodeInvalidRequest, appusage.ErrorCodeInsufficientQuota:
		return http.StatusBadRequest
	case "rate_limited", appusage.ErrorCodeAccountRateLimited:
		return http.StatusTooManyRequests
	case "auth_failed", appusage.ErrorCodeAccountDead:
		return http.StatusUnauthorized
	case staleTaskErrorCode, appusage.ErrorCodeUpstreamTimeout:
		return http.StatusGatewayTimeout
	}
	switch strings.TrimSpace(errorType) {
	case "invalid_request", "validation_error":
		return http.StatusBadRequest
	}
	return http.StatusBadGateway
}

// taskElapsedMs 任务从创建到失败的耗时（毫秒），与同步路径「秒失败 vs 卡死」的诊断口径对齐。
func taskElapsedMs(t *ent.Task) int64 {
	if t.CreatedAt.IsZero() {
		return 0
	}
	end := time.Now()
	if t.CompletedAt != nil && !t.CompletedAt.IsZero() {
		end = *t.CompletedAt
	}
	if end.Before(t.CreatedAt) {
		return 0
	}
	return end.Sub(t.CreatedAt).Milliseconds()
}

// taskIntField JSON 里的整数字段：反序列化后可能是 float64 / int / int64 / json.Number / 字符串。
func taskIntField(m map[string]interface{}, key string) int {
	if m == nil {
		return 0
	}
	switch v := m[key].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case string:
		n, _ := strconv.Atoi(strings.TrimSpace(v))
		return n
	default:
		if s := strings.TrimSpace(stringValue(v)); s != "" {
			n, _ := strconv.Atoi(s)
			return n
		}
	}
	return 0
}

func firstPositive(values ...int) int {
	for _, v := range values {
		if v > 0 {
			return v
		}
	}
	return 0
}
