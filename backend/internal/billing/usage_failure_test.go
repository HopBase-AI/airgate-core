package billing

import (
	"context"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	entusagelog "github.com/DouDOU-start/airgate-core/ent/usagelog"
)

// usage_failure_test.go —— 「失败请求也落使用日志」的写入侧测试：
// 零费用记录不扣费 / platform-model 空值兜底 / 错误文本按 UTF-8 边界截断。

// TestRecordFailureUsageDoesNotCharge 失败记录只留痕不扣费：
// 用户余额、APIKey 的 used_quota 与 used_quota_actual 都不许动。
func TestRecordFailureUsageDoesNotCharge(t *testing.T) {
	db := newBillingTestDB(t, "usage_failure_no_charge")
	ctx := context.Background()
	user := createBillingTestUser(t, ctx, db, "failure-no-charge@example.com")
	if err := db.User.UpdateOneID(user.ID).SetBalance(100).Exec(ctx); err != nil {
		t.Fatalf("set balance: %v", err)
	}
	key := createBillingTestAPIKey(t, ctx, db, user)

	r := NewRecorder(db, 10)
	t.Cleanup(func() { flushInterval = 5 * time.Second })
	flushInterval = 10 * time.Millisecond
	r.Start()
	t.Cleanup(r.Stop)

	r.Record(UsageRecord{
		UserID:       user.ID,
		UserEmail:    user.Email,
		APIKeyID:     key.ID,
		Platform:     "claude",
		Model:        "claude-opus-5",
		Status:       UsageStatusError,
		ErrorCode:    "upstream_transient",
		ErrorStatus:  502,
		ErrorMessage: "上游连接中断",
		DurationMs:   1234,
	})

	// 异步落库：轮询而非 sleep。
	waitFor(t, 3*time.Second, func() bool { return countUsageLogs(t, db) == 1 }, "失败记录落库")

	log, err := db.UsageLog.Query().Only(ctx)
	if err != nil {
		t.Fatalf("query usage log: %v", err)
	}
	if log.Status != UsageStatusError {
		t.Fatalf("status = %q, want %q", log.Status, UsageStatusError)
	}
	if log.ErrorCode != "upstream_transient" || log.ErrorStatus != 502 || log.ErrorMessage != "上游连接中断" {
		t.Fatalf("error 字段 = (%q, %d, %q), want (upstream_transient, 502, 上游连接中断)",
			log.ErrorCode, log.ErrorStatus, log.ErrorMessage)
	}
	if log.ActualCost != 0 || log.BilledCost != 0 || log.TotalCost != 0 {
		t.Fatalf("失败记录费用应全为 0，实际 actual=%v billed=%v total=%v",
			log.ActualCost, log.BilledCost, log.TotalCost)
	}

	updatedUser, err := db.User.Get(ctx, user.ID)
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	if updatedUser.Balance != 100 {
		t.Fatalf("balance = %v, want 100（失败请求不扣余额）", updatedUser.Balance)
	}
	updatedKey, err := db.APIKey.Get(ctx, key.ID)
	if err != nil {
		t.Fatalf("get api key: %v", err)
	}
	if updatedKey.UsedQuota != 0 || updatedKey.UsedQuotaActual != 0 {
		t.Fatalf("api key 用量 = (%v, %v), want (0, 0)（失败请求不累加配额）",
			updatedKey.UsedQuota, updatedKey.UsedQuotaActual)
	}
}

// TestUsageLogFallsBackToUnknownPlatformAndModel 失败请求可能在解析出平台/模型前就中断，
// 空串会被 schema 的 NotEmpty 拒绝并拖垮整批 CreateBulk，因此必须落 "unknown"。
func TestUsageLogFallsBackToUnknownPlatformAndModel(t *testing.T) {
	db := newBillingTestDB(t, "usage_failure_unknown")
	ctx := context.Background()
	user := createBillingTestUser(t, ctx, db, "failure-unknown@example.com")

	r := NewRecorder(db, 0)
	id, err := r.RecordSync(ctx, UsageRecord{
		UserID:       user.ID,
		UserEmail:    user.Email,
		Platform:     "", // 尚未选出插件
		Model:        "", // 请求体还没解析出模型
		Status:       UsageStatusError,
		ErrorCode:    "plugin_error",
		ErrorStatus:  502,
		ErrorMessage: "plugin crashed",
	})
	if err != nil {
		t.Fatalf("RecordSync 不该失败（空 platform/model 须被兜底）: %v", err)
	}

	log, err := db.UsageLog.Get(ctx, id)
	if err != nil {
		t.Fatalf("get usage log: %v", err)
	}
	if log.Platform != usageUnknownPlatform || log.Model != usageUnknownModel {
		t.Fatalf("platform/model = (%q, %q), want (%q, %q)",
			log.Platform, log.Model, usageUnknownPlatform, usageUnknownModel)
	}
	if log.Status != UsageStatusError {
		t.Fatalf("status = %q, want %q", log.Status, UsageStatusError)
	}
}

// TestBatchInsertKeepsSuccessAndFailureIndependent 同一批里既有正常计费记录、
// 又有 platform/model 为空的失败记录时，两条都要写进去且互不拖累：
// 正常记录照常扣费，失败记录不产生任何扣费。
func TestBatchInsertKeepsSuccessAndFailureIndependent(t *testing.T) {
	db := newBillingTestDB(t, "usage_failure_mixed_batch")
	ctx := context.Background()
	user := createBillingTestUser(t, ctx, db, "failure-mixed@example.com")
	if err := db.User.UpdateOneID(user.ID).SetBalance(100).Exec(ctx); err != nil {
		t.Fatalf("set balance: %v", err)
	}
	key := createBillingTestAPIKey(t, ctx, db, user)

	r := NewRecorder(db, 0)
	err := r.batchInsert(ctx, []UsageRecord{
		{
			RequestID:   "req-success",
			UserID:      user.ID,
			UserEmail:   user.Email,
			APIKeyID:    key.ID,
			Platform:    "openai",
			Model:       "gpt-5",
			InputTokens: 10,
			ActualCost:  1.5,
			BilledCost:  2,
			TotalCost:   2,
		},
		{
			RequestID:    "req-failure",
			UserID:       user.ID,
			UserEmail:    user.Email,
			APIKeyID:     key.ID,
			Platform:     "",
			Model:        "",
			Status:       UsageStatusError,
			ErrorCode:    "client_error",
			ErrorStatus:  400,
			ErrorMessage: "model 参数缺失",
		},
	})
	if err != nil {
		t.Fatalf("batchInsert 不该失败（空 platform 的失败记录须被兜底）: %v", err)
	}

	if got := countUsageLogs(t, db); got != 2 {
		t.Fatalf("usage_log 行数 = %d, want 2（两条都要落库）", got)
	}
	success, err := db.UsageLog.Query().Where(entusagelog.RequestIDEQ("req-success")).Only(ctx)
	if err != nil {
		t.Fatalf("query success log: %v", err)
	}
	if success.Status != UsageStatusSuccess || success.Platform != "openai" || success.Model != "gpt-5" {
		t.Fatalf("正常记录 = (%q, %q, %q), want (success, openai, gpt-5)",
			success.Status, success.Platform, success.Model)
	}
	failure, err := db.UsageLog.Query().Where(entusagelog.RequestIDEQ("req-failure")).Only(ctx)
	if err != nil {
		t.Fatalf("query failure log: %v", err)
	}
	if failure.Status != UsageStatusError || failure.Platform != usageUnknownPlatform || failure.Model != usageUnknownModel {
		t.Fatalf("失败记录 = (%q, %q, %q), want (error, unknown, unknown)",
			failure.Status, failure.Platform, failure.Model)
	}

	// 扣费只来自正常记录。
	updatedUser, err := db.User.Get(ctx, user.ID)
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	if updatedUser.Balance != 98.5 {
		t.Fatalf("balance = %v, want 98.5（只扣正常记录的 1.5）", updatedUser.Balance)
	}
	updatedKey, err := db.APIKey.Get(ctx, key.ID)
	if err != nil {
		t.Fatalf("get api key: %v", err)
	}
	if updatedKey.UsedQuota != 2 || updatedKey.UsedQuotaActual != 1.5 {
		t.Fatalf("api key 用量 = (%v, %v), want (2, 1.5)", updatedKey.UsedQuota, updatedKey.UsedQuotaActual)
	}
}

// TestTruncateBytesUTF8 按字节截断且必须回退到合法 UTF-8 边界：
// 中文被从 rune 中间切断会产生非法 UTF-8，Postgres 会拒绝整条写入。
func TestTruncateBytesUTF8(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		maxLen int
		want   string
	}{
		{name: "短串原样返回", input: "boom", maxLen: 10, want: "boom"},
		{name: "空串原样返回", input: "", maxLen: 10, want: ""},
		{name: "恰好等于上限不截断", input: "abcde", maxLen: 5, want: "abcde"},
		{name: "超长英文按字节截断", input: strings.Repeat("a", 600), maxLen: 500, want: strings.Repeat("a", 500)},
		{name: "中文切在 rune 中间回退一字节", input: "中中中", maxLen: 4, want: "中"},
		{name: "中文切在 rune 中间回退两字节", input: "中中中", maxLen: 5, want: "中"},
		{name: "中文正好切在边界", input: "中中中", maxLen: 6, want: "中中"},
		{name: "超长中文截断后仍合法", input: strings.Repeat("中", 300), maxLen: 500, want: strings.Repeat("中", 166)},
		{name: "emoji 不被腰斩", input: "错误：🚀🚀", maxLen: 11, want: "错误："},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateBytesUTF8(tt.input, tt.maxLen)
			if got != tt.want {
				t.Fatalf("truncateBytesUTF8(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.want)
			}
			if !utf8.ValidString(got) {
				t.Fatalf("截断结果不是合法 UTF-8: %q", got)
			}
			if len(got) > tt.maxLen && len(tt.input) > tt.maxLen {
				t.Fatalf("截断结果长度 %d 超过上限 %d", len(got), tt.maxLen)
			}
			if !strings.HasPrefix(tt.input, got) {
				t.Fatalf("截断结果 %q 不是原串 %q 的前缀", got, tt.input)
			}
		})
	}
}

// TestUsageRecordStatusHelpers 状态归一化：空值等价成功（兼容旧 WAL 记录）。
func TestUsageRecordStatusHelpers(t *testing.T) {
	tests := []struct {
		name       string
		status     string
		wantIsErr  bool
		wantNormal string
	}{
		{name: "空值按成功", status: "", wantIsErr: false, wantNormal: UsageStatusSuccess},
		{name: "显式成功", status: UsageStatusSuccess, wantIsErr: false, wantNormal: UsageStatusSuccess},
		{name: "失败", status: UsageStatusError, wantIsErr: true, wantNormal: UsageStatusError},
		{name: "未知取值按成功", status: "weird", wantIsErr: false, wantNormal: UsageStatusSuccess},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := UsageRecord{Status: tt.status}
			if got := rec.IsError(); got != tt.wantIsErr {
				t.Fatalf("IsError() = %v, want %v", got, tt.wantIsErr)
			}
			if got := rec.normalizedStatus(); got != tt.wantNormal {
				t.Fatalf("normalizedStatus() = %q, want %q", got, tt.wantNormal)
			}
		})
	}
}

// TestFallbackNonEmpty 空串兜底。
func TestFallbackNonEmpty(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		fallback string
		want     string
	}{
		{name: "非空原样返回", value: "openai", fallback: "unknown", want: "openai"},
		{name: "空串取兜底", value: "", fallback: "unknown", want: "unknown"},
		{name: "空格不算空串", value: " ", fallback: "unknown", want: " "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := fallbackNonEmpty(tt.value, tt.fallback); got != tt.want {
				t.Fatalf("fallbackNonEmpty(%q, %q) = %q, want %q", tt.value, tt.fallback, got, tt.want)
			}
		})
	}
}
