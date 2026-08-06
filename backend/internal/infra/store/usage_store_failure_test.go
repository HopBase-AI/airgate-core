package store

import (
	"context"
	"testing"

	"github.com/DouDOU-start/airgate-core/ent"
	appusage "github.com/DouDOU-start/airgate-core/internal/app/usage"
)

// usage_store_failure_test.go —— 失败请求在读侧的两条口径：
// 列表按 error_code 筛选（含"被计费的 4xx"），统计按 status 排除失败行。

// seedUsageFailureFixtures 造三类记录：
//   - 纯成功：status=success、error_code 为空、有 token 与费用；
//   - 纯失败：status=error、error_code 非空、token 与费用为 0；
//   - 被计费的 4xx：status=success（费用与扣款一致）但带 error_code。
func seedUsageFailureFixtures(t *testing.T, db *ent.Client, user *ent.User) {
	t.Helper()
	ctx := context.Background()

	fixtures := []struct {
		model       string
		status      string
		errorCode   string
		errorStatus int
		inputTokens int
		totalCost   float64
		actualCost  float64
		billedCost  float64
	}{
		{
			model:       "gpt-5",
			status:      appusage.StatusSuccess,
			inputTokens: 100,
			totalCost:   2,
			actualCost:  1.5,
			billedCost:  3,
		},
		{
			model:       "gpt-5",
			status:      appusage.StatusError,
			errorCode:   appusage.ErrorCodeUpstreamTransient,
			errorStatus: 502,
		},
		{
			model:       "gpt-5-mini",
			status:      appusage.StatusSuccess,
			errorCode:   appusage.ErrorCodeClientError,
			errorStatus: 400,
			inputTokens: 20,
			totalCost:   0.5,
			actualCost:  0.25,
			billedCost:  0.75,
		},
	}

	for _, item := range fixtures {
		if _, err := db.UsageLog.Create().
			SetPlatform("openai").
			SetModel(item.model).
			SetUserID(user.ID).
			SetUserIDSnapshot(user.ID).
			SetUserEmailSnapshot(user.Email).
			SetStatus(item.status).
			SetErrorCode(item.errorCode).
			SetErrorStatus(item.errorStatus).
			SetInputTokens(item.inputTokens).
			SetTotalCost(item.totalCost).
			SetActualCost(item.actualCost).
			SetBilledCost(item.billedCost).
			Save(ctx); err != nil {
			t.Fatalf("create usage log: %v", err)
		}
	}
}

// TestUsageStoreResultFilter 列表结果筛选按 error_code 判定：
// 「只看失败」必须能捞出那条被上游计了费的 4xx（status 仍是 success）。
func TestUsageStoreResultFilter(t *testing.T) {
	db := enttestOpen(t)
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	}()

	ctx := context.Background()
	user := createTestUser(t, db, "usage-result-filter@example.com")
	seedUsageFailureFixtures(t, db, user)
	store := NewUsageStore(db)

	tests := []struct {
		name          string
		result        string
		wantTotal     int64
		wantErrorCode []string // 期望命中的 error_code 集合（顺序无关）
	}{
		{
			name:          "空筛选返回全部",
			result:        "",
			wantTotal:     3,
			wantErrorCode: []string{"", appusage.ErrorCodeUpstreamTransient, appusage.ErrorCodeClientError},
		},
		{
			name:          "只看失败含被计费的 4xx",
			result:        appusage.ResultFilterError,
			wantTotal:     2,
			wantErrorCode: []string{appusage.ErrorCodeUpstreamTransient, appusage.ErrorCodeClientError},
		},
		{
			name:          "只看成功仅 error_code 为空",
			result:        appusage.ResultFilterSuccess,
			wantTotal:     1,
			wantErrorCode: []string{""},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filter := appusage.ListFilter{Page: 1, PageSize: 20, Result: tt.result}

			// 管理员视图与用户视图共用同一个筛选实现，两条链路都断言。
			adminItems, adminTotal, err := store.ListAdmin(ctx, filter)
			if err != nil {
				t.Fatalf("ListAdmin returned error: %v", err)
			}
			assertUsageResultSet(t, "ListAdmin", adminItems, adminTotal, tt.wantTotal, tt.wantErrorCode)

			userItems, userTotal, err := store.ListUser(ctx, int64(user.ID), filter)
			if err != nil {
				t.Fatalf("ListUser returned error: %v", err)
			}
			assertUsageResultSet(t, "ListUser", userItems, userTotal, tt.wantTotal, tt.wantErrorCode)
		})
	}
}

func assertUsageResultSet(t *testing.T, label string, items []appusage.LogRecord, total, wantTotal int64, wantCodes []string) {
	t.Helper()
	if total != wantTotal {
		t.Fatalf("%s total = %d, want %d", label, total, wantTotal)
	}
	if int64(len(items)) != wantTotal {
		t.Fatalf("%s len = %d, want %d", label, len(items), wantTotal)
	}
	got := make(map[string]int, len(items))
	for _, item := range items {
		got[item.ErrorCode]++
		// Failed() 以 error_code 为判据：被计费的 4xx 同样算失败。
		if want := item.ErrorCode != ""; item.Failed() != want {
			t.Fatalf("%s Failed() = %v, want %v（error_code=%q）", label, item.Failed(), want, item.ErrorCode)
		}
	}
	want := make(map[string]int, len(wantCodes))
	for _, code := range wantCodes {
		want[code]++
	}
	for code, count := range want {
		if got[code] != count {
			t.Fatalf("%s error_code=%q 命中 %d 条, want %d 条（全部命中：%v）", label, code, got[code], count, got)
		}
	}
	if len(got) != len(want) {
		t.Fatalf("%s 命中 error_code 集合 = %v, want %v", label, got, want)
	}
}

// TestUsageStoreSummaryExcludesFailedRequests 统计口径：
// TotalRequests 只数 status!=error 的行，FailedRequests 只数 status=error 的行，
// 费用与 token 都不含失败行的贡献。
func TestUsageStoreSummaryExcludesFailedRequests(t *testing.T) {
	db := enttestOpen(t)
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	}()

	ctx := context.Background()
	user := createTestUser(t, db, "usage-summary-failed@example.com")
	seedUsageFailureFixtures(t, db, user)
	store := NewUsageStore(db)

	// 计数按 error_code 划分，与列表筛选同口径：成功 1 条，失败 2 条
	// （零费用失败行 + 被上游计费的 4xx），两者相加等于总行数 3。
	//
	// 费用/token 只排除 status=error 的零费用行，因此仍含被计费 4xx 的贡献：
	// 成功行(100 token / total 2 / actual 1.5 / billed 3)
	// + 被计费的 4xx(20 token / total 0.5 / actual 0.25 / billed 0.75)。
	want := appusage.Summary{
		TotalRequests:   1,
		FailedRequests:  2,
		TotalTokens:     120,
		TotalCost:       2.5,
		TotalActualCost: 1.75,
		TotalBilledCost: 3.75,
	}

	adminSummary, err := store.SummaryAdmin(ctx, appusage.StatsFilter{})
	if err != nil {
		t.Fatalf("SummaryAdmin returned error: %v", err)
	}
	if adminSummary != want {
		t.Fatalf("SummaryAdmin = %+v, want %+v", adminSummary, want)
	}

	userSummary, err := store.SummaryUser(ctx, int64(user.ID), appusage.StatsFilter{})
	if err != nil {
		t.Fatalf("SummaryUser returned error: %v", err)
	}
	if userSummary != want {
		t.Fatalf("SummaryUser = %+v, want %+v", userSummary, want)
	}

	// 不变量：失败数必须等于「只看失败」列表的条数，否则前端的失败卡片会与
	// 筛选出来的记录对不上（两处曾按 status / error_code 两种口径实现过）。
	_, failedTotal, err := store.ListAdmin(ctx, appusage.ListFilter{
		Page: 1, PageSize: 50, Result: appusage.ResultFilterError,
	})
	if err != nil {
		t.Fatalf("ListAdmin returned error: %v", err)
	}
	if failedTotal != adminSummary.FailedRequests {
		t.Fatalf("失败列表条数 = %d, 汇总失败数 = %d，两者必须一致", failedTotal, adminSummary.FailedRequests)
	}
	_, successTotal, err := store.ListAdmin(ctx, appusage.ListFilter{
		Page: 1, PageSize: 50, Result: appusage.ResultFilterSuccess,
	})
	if err != nil {
		t.Fatalf("ListAdmin returned error: %v", err)
	}
	if successTotal != adminSummary.TotalRequests {
		t.Fatalf("成功列表条数 = %d, 汇总成功数 = %d，两者必须一致", successTotal, adminSummary.TotalRequests)
	}

	// 分组统计同样排除失败行：gpt-5 只剩成功那一条。
	byModel, err := store.StatsByModel(ctx, appusage.StatsFilter{})
	if err != nil {
		t.Fatalf("StatsByModel returned error: %v", err)
	}
	requests := make(map[string]int64, len(byModel))
	for _, item := range byModel {
		requests[item.Model] = item.Requests
	}
	if requests["gpt-5"] != 1 || requests["gpt-5-mini"] != 1 {
		t.Fatalf("StatsByModel requests = %v, want gpt-5:1 gpt-5-mini:1（失败行不计入）", requests)
	}
}
