package plugin

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"entgo.io/ent/dialect/sql/schema"
	_ "github.com/mattn/go-sqlite3"

	"github.com/DouDOU-start/airgate-core/ent"
	"github.com/DouDOU-start/airgate-core/ent/enttest"
	enttask "github.com/DouDOU-start/airgate-core/ent/task"
	"github.com/DouDOU-start/airgate-core/internal/billing"
)

// task_failure_usage_test.go —— 任务失败终态落零费用使用记录的组装规则：
// 归属映射到企业主 + member_id + department_id、字段来源、跳过规则、error_status 口径。

func openTaskFailureDB(t *testing.T, name string) *ent.Client {
	t.Helper()
	db := enttest.Open(t, "sqlite3", "file:"+name+"?mode=memory&cache=shared&_fk=1",
		enttest.WithMigrateOptions(schema.WithGlobalUniqueID(false)))
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// 成员在工作坊提交、上游拒绝：记录挂企业主，member_id / department_id 齐全，费用全零。
func TestBuildTaskFailureUsageMapsMemberToOwner(t *testing.T) {
	ctx := context.Background()
	db := openTaskFailureDB(t, "task_failure_member")
	owner := db.User.Create().SetEmail("owner@example.com").SetPasswordHash("h").SetBalance(10).SaveX(ctx)
	account := db.User.Create().SetEmail("member@example.com").SetPasswordHash("h").SaveX(ctx)
	dept := db.Department.Create().SetName("Boom 项目组").SetOwner(owner).SaveX(ctx)
	member := db.Member.Create().SetName("成员").SetOwner(owner).SetAccount(account).SetDepartment(dept).SaveX(ctx)

	created := time.Now().Add(-70 * time.Second)
	completed := time.Now()
	task := db.Task.Create().SetPluginID("gateway-openai").SetTaskType("image.generate").
		SetUserID(account.ID).SetStatus(enttask.StatusFailed).
		SetInput(map[string]interface{}{"model": "gpt-image-2", "group_id": float64(15), "prompt": "x"}).
		SetAttributes(map[string]interface{}{"kind": "image", "platform": "openai", "model": "gpt-image-2"}).
		SetExecution(map[string]interface{}{"upstream_task_id": "442"}).
		SetErrorType("invalid_request").SetErrorCode("safety_rejected").
		SetErrorMessage("Your request was rejected by the content safety system. api_key=sk-abcdefghijklmnop please modify your prompt").
		SetCreatedAt(created).SetCompletedAt(completed).SaveX(ctx)

	host := &HostService{db: db}
	record, ok := host.buildTaskFailureUsage(ctx, task)
	if !ok {
		t.Fatalf("expected a failure record")
	}
	if record.UserID != owner.ID || record.UserEmail != "owner@example.com" {
		t.Fatalf("user = %d %q, want owner %d", record.UserID, record.UserEmail, owner.ID)
	}
	if record.MemberID != member.ID || record.DepartmentID != dept.ID {
		t.Fatalf("member/department = %d/%d, want %d/%d", record.MemberID, record.DepartmentID, member.ID, dept.ID)
	}
	if record.Status != billing.UsageStatusError || record.ErrorCode != "safety_rejected" || record.ErrorStatus != http.StatusBadRequest {
		t.Fatalf("status = %q code = %q http = %d", record.Status, record.ErrorCode, record.ErrorStatus)
	}
	if record.Platform != "openai" || record.Model != "gpt-image-2" || record.GroupID != 15 {
		t.Fatalf("platform/model/group = %q/%q/%d", record.Platform, record.Model, record.GroupID)
	}
	if record.Endpoint != "task:image.generate" || record.UsageMetadata["task_type"] != "image.generate" {
		t.Fatalf("endpoint = %q metadata = %v", record.Endpoint, record.UsageMetadata)
	}
	if record.ActualCost != 0 || record.BilledCost != 0 || record.TotalCost != 0 {
		t.Fatalf("failure record must be zero-cost: %+v", record)
	}
	if record.DurationMs < 60_000 || record.DurationMs > 80_000 {
		t.Fatalf("DurationMs = %d, want ~70s", record.DurationMs)
	}
	// 凭证抹掉，原因保留
	if got := record.ErrorMessage; got == "" || strings.Contains(got, "sk-abcdefghijklmnop") || !strings.Contains(got, "content safety system") {
		t.Fatalf("ErrorMessage = %q", got)
	}
}

// 企业主本人的任务：归属就是本人，member/department 为 0；缺 error_code 时退 plugin_error，
// 平台从插件 id 推导，模型从 execution 兜底。
func TestBuildTaskFailureUsagePlainUserFallbacks(t *testing.T) {
	ctx := context.Background()
	db := openTaskFailureDB(t, "task_failure_plain")
	user := db.User.Create().SetEmail("plain@example.com").SetPasswordHash("h").SaveX(ctx)
	task := db.Task.Create().SetPluginID("gateway-bailian").SetTaskType("video.generate").
		SetUserID(user.ID).SetStatus(enttask.StatusFailed).
		SetInput(map[string]interface{}{"prompt": "x"}).
		SetExecution(map[string]interface{}{"model": "wan3.0-video", "group_id": int64(36), "account_id": float64(68), "api_key_id": "12"}).
		SetErrorMessage("rpc error: code = ResourceExhausted desc = insufficient balance").
		SaveX(ctx)

	host := &HostService{db: db}
	record, ok := host.buildTaskFailureUsage(ctx, task)
	if !ok {
		t.Fatalf("expected a failure record")
	}
	if record.UserID != user.ID || record.MemberID != 0 || record.DepartmentID != 0 {
		t.Fatalf("attribution = %d/%d/%d", record.UserID, record.MemberID, record.DepartmentID)
	}
	if record.Platform != "bailian" || record.Model != "wan3.0-video" {
		t.Fatalf("platform/model = %q/%q", record.Platform, record.Model)
	}
	if record.GroupID != 36 || record.AccountID != 68 || record.APIKeyID != 12 {
		t.Fatalf("group/account/key = %d/%d/%d", record.GroupID, record.AccountID, record.APIKeyID)
	}
	if record.ErrorCode != "plugin_error" || record.ErrorStatus != http.StatusBadGateway {
		t.Fatalf("code/status = %q/%d", record.ErrorCode, record.ErrorStatus)
	}
	if record.ErrorMessage != "insufficient balance" {
		t.Fatalf("ErrorMessage = %q, want gRPC envelope stripped", record.ErrorMessage)
	}
}

// 跳过规则：非 failed、已有计费记录、审计任务、只镜像影子的工作坊任务。
func TestBuildTaskFailureUsageSkips(t *testing.T) {
	ctx := context.Background()
	db := openTaskFailureDB(t, "task_failure_skip")
	user := db.User.Create().SetEmail("skip@example.com").SetPasswordHash("h").SaveX(ctx)
	host := &HostService{db: db}

	base := func() *ent.TaskCreate {
		return db.Task.Create().SetPluginID("gateway-seedance").SetTaskType("video.generate").
			SetUserID(user.ID).SetStatus(enttask.StatusFailed).SetInput(map[string]interface{}{"model": "m"})
	}
	usageID := 77
	cases := map[string]*ent.Task{
		"completed":   base().SetStatus(enttask.StatusCompleted).SaveX(ctx),
		"has_usage":   base().SetUsageID(usageID).SaveX(ctx),
		"audit_only":  base().SetAttributes(map[string]interface{}{"audit_only": true}).SaveX(ctx),
		"mirror_only": base().SetExecution(map[string]interface{}{"shadow_public_id": "blt68x1", "created_via": "studio"}).SaveX(ctx),
	}
	for name, task := range cases {
		if _, ok := host.buildTaskFailureUsage(ctx, task); ok {
			t.Fatalf("%s: should be skipped", name)
		}
	}
	// 对照：提交期失败（没有影子 id）要记录
	if _, ok := host.buildTaskFailureUsage(ctx, base().SetExecution(map[string]interface{}{"created_via": "studio"}).SaveX(ctx)); !ok {
		t.Fatalf("submit-time failure must be recorded")
	}
}

// error_status 口径：校验类一律 4xx（禁止 5xx）、余额与同步路径同为 402、取消 499、中断 503、
// http_<n> 透传解析、其余上游类 502。
func TestTaskFailureStatus(t *testing.T) {
	cases := []struct {
		code, errType string
		want          int
	}{
		// 确定性校验 / 客户端类 → 400（显式集合）
		{"model_not_in_catalog", "", http.StatusBadRequest},
		{"wrong_model_kind", "", http.StatusBadRequest},
		{"prompt_required", "", http.StatusBadRequest},
		{"prompt_too_long", "", http.StatusBadRequest},
		{"group_missing", "", http.StatusBadRequest},
		{"invalid_request", "", http.StatusBadRequest},
		{"bad_request", "", http.StatusBadRequest},
		{"safety_rejected", "invalid_request", http.StatusBadRequest},
		{"submission_rejected", "", http.StatusBadRequest},
		{"mask_unsupported", "", http.StatusBadRequest},
		{"unsupported_task_type", "", http.StatusBadRequest},
		{"input_sensitive", "content_policy", http.StatusBadRequest},
		{"client_error", "", http.StatusBadRequest},
		// 参考素材类 → 400（reference_ 前缀，不靠枚举）
		{"reference_image_invalid", "", http.StatusBadRequest},
		{"reference_image_unsupported", "", http.StatusBadRequest},
		{"reference_image_too_many", "", http.StatusBadRequest},
		{"reference_image_required", "", http.StatusBadRequest},
		{"reference_input_invalid", "", http.StatusBadRequest},
		{"reference_media_unsupported", "", http.StatusBadRequest},
		{"reference_media_invalid", "", http.StatusBadRequest},
		{"reference_media_too_many", "", http.StatusBadRequest},
		{"reference_whatever_new_code", "upstream_error", http.StatusBadRequest},
		{"  reference_image_invalid  ", "", http.StatusBadRequest},
		// 余额 / 额度不足 → 402（与同步路径 quota.go 同码）
		{"insufficient_balance", "validation_error", http.StatusPaymentRequired},
		{"insufficient_quota", "", http.StatusPaymentRequired},
		// 限流 / 鉴权 / 超时
		{"rate_limited", "", http.StatusTooManyRequests},
		{"account_rate_limited", "", http.StatusTooManyRequests},
		{"auth_failed", "", http.StatusUnauthorized},
		{"account_dead", "", http.StatusUnauthorized},
		{staleTaskErrorCode, "", http.StatusGatewayTimeout},
		{"upstream_timeout", "", http.StatusGatewayTimeout},
		{"task_timeout", "timeout", http.StatusGatewayTimeout},
		// 取消 / 中断
		{"task_canceled", "task_state", statusClientClosedRequest},
		{"task_interrupted", "", http.StatusServiceUnavailable},
		// http_<n> 透传解析
		{"http_429", "upstream_error", http.StatusTooManyRequests},
		{"http_400", "upstream_error", http.StatusBadRequest},
		{"http_503", "upstream_error", http.StatusServiceUnavailable},
		{"http_999", "upstream_error", http.StatusBadGateway}, // 不是合法状态：不认
		{"http_abc", "upstream_error", http.StatusBadGateway},
		{"http_", "upstream_error", http.StatusBadGateway},
		// errorType 兜底 → 400
		{"", "validation_error", http.StatusBadRequest},
		{"something_new", "invalid_request", http.StatusBadRequest},
		// 其余上游类 → 502
		{"server_error", "", http.StatusBadGateway},
		{"upstream_error", "", http.StatusBadGateway},
		{"upstream_task_not_found", "", http.StatusBadGateway},
		{"no_output", "", http.StatusBadGateway},
		{"image_store_failed", "", http.StatusBadGateway},
		{"plugin_error", "", http.StatusBadGateway},
		{"output_video_sensitive", "content_policy", http.StatusBadGateway},
		{"", "", http.StatusBadGateway},
	}
	for _, c := range cases {
		if got := taskFailureStatus(c.code, c.errType); got != c.want {
			t.Fatalf("taskFailureStatus(%q,%q) = %d, want %d", c.code, c.errType, got, c.want)
		}
	}
	// 校验类失败绝不能落 5xx：这是本口径的硬约束
	for code := range taskFailureClientErrorCodes {
		if got := taskFailureStatus(code, ""); got >= http.StatusInternalServerError {
			t.Fatalf("client error %q mapped to 5xx (%d)", code, got)
		}
	}
}
