package handler

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	appusage "github.com/DouDOU-start/airgate-core/internal/app/usage"
)

// usage_handler_export_test.go —— 充值消耗明细导出：
// 区间解析与收窄、边界归属、成本口径、文件名安全、失败行措辞与汇总口径。

func exportCtx(rawQuery string) *gin.Context {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/usage/export?"+rawQuery, nil)
	return c
}

func TestParseExportRange(t *testing.T) {
	t.Run("缺 start_time 报错", func(t *testing.T) {
		if _, _, _, err := parseExportRange(exportCtx("")); err == nil {
			t.Fatal("期望报错，实际通过")
		}
	})

	t.Run("start_time 非 RFC3339 报错", func(t *testing.T) {
		if _, _, _, err := parseExportRange(exportCtx("start_time=2026-08-12")); err == nil {
			t.Fatal("期望报错，实际通过")
		}
	})

	t.Run("缺 end_time 时右边界取当前时刻", func(t *testing.T) {
		start, end, clamped, err := parseExportRange(exportCtx("start_time=2026-08-12T15:22:37%2B08:00"))
		if err != nil {
			t.Fatalf("解析失败: %v", err)
		}
		if clamped {
			t.Fatal("区间未超长，不应被收窄")
		}
		if !start.Equal(time.Date(2026, 8, 12, 15, 22, 37, 0, time.FixedZone("", 8*3600))) {
			t.Fatalf("start 解析错误: %v", start)
		}
		if time.Since(end) > time.Minute {
			t.Fatalf("end 应约等于当前时刻，实际 %v", end)
		}
	})

	t.Run("毫秒被截到整秒", func(t *testing.T) {
		// 充值到账时刻带毫秒而使用记录只有秒级精度，不对齐会把充值后
		// 同一秒内的调用划进上一笔账单。
		start, end, _, err := parseExportRange(exportCtx(
			"start_time=2026-08-12T15:22:37.628Z&end_time=2026-08-20T10:00:00.900Z"))
		if err != nil {
			t.Fatalf("解析失败: %v", err)
		}
		if start.Nanosecond() != 0 || end.Nanosecond() != 0 {
			t.Fatalf("边界应截断到整秒: start=%v end=%v", start, end)
		}
		if !start.Equal(time.Date(2026, 8, 12, 15, 22, 37, 0, time.UTC)) {
			t.Fatalf("start 截断错误: %v", start)
		}
	})

	t.Run("start 不早于 end 时报错", func(t *testing.T) {
		q := "start_time=2026-08-12T15:00:00Z&end_time=2026-08-12T15:00:00Z"
		if _, _, _, err := parseExportRange(exportCtx(q)); err == nil {
			t.Fatal("期望报错，实际通过")
		}
	})

	t.Run("超长区间收窄而非拒绝", func(t *testing.T) {
		// 硬拒会让"最后一笔充值太久"的老账户永远导不出来。
		q := "start_time=1970-01-01T00:00:00Z&end_time=2026-08-28T00:00:00Z"
		start, end, clamped, err := parseExportRange(exportCtx(q))
		if err != nil {
			t.Fatalf("超长区间应收窄而不是报错: %v", err)
		}
		if !clamped {
			t.Fatal("应标记为已收窄")
		}
		if got := end.Sub(start); got != exportMaxWindow {
			t.Fatalf("收窄后的区间应为 %v，实际 %v", exportMaxWindow, got)
		}
	})
}

// TestIsSafeFileToken 订单号会进 Content-Disposition，必须挡住注入字符。
func TestIsSafeFileToken(t *testing.T) {
	safe := []string{"AG20260812151815b3f94b50", "abc-123", "a_b"}
	for _, s := range safe {
		if !isSafeFileToken(s) {
			t.Errorf("%q 应被放行", s)
		}
	}
	unsafe := []string{`a"b`, "a b", "a/b", "a\nb", "中文", strings.Repeat("a", 65)}
	for _, s := range unsafe {
		if isSafeFileToken(s) {
			t.Errorf("%q 应被拒绝", s)
		}
	}
}

func TestExportFileName(t *testing.T) {
	start := time.Date(2026, 8, 12, 15, 22, 37, 0, time.UTC)
	if got := exportFileName("AG20260812151815b3f94b50", start); got != "usage-AG20260812151815b3f94b50.csv" {
		t.Errorf("订单号命名错误: %s", got)
	}
	// 非法订单号不能进文件名，退回日期命名。
	if got := exportFileName(`evil"name`, start); got != "usage-20260812.csv" {
		t.Errorf("非法订单号应退回日期命名，实际: %s", got)
	}
	if got := exportFileName("", start); got != "usage-20260812.csv" {
		t.Errorf("空订单号应退回日期命名，实际: %s", got)
	}
}

// TestClassifyExportRowBoundary 区间左闭右开：下一笔充值到账那一刻起算下一个区间，
// 否则同一天内连充两次时，两份明细会互相串行。
func TestClassifyExportRowBoundary(t *testing.T) {
	start := time.Date(2026, 8, 12, 15, 22, 37, 0, time.UTC)
	end := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	at := func(tm time.Time) appusage.LogRecord {
		return appusage.LogRecord{ID: 1, CreatedAt: tm.Format(time.RFC3339), Model: "m"}
	}

	if _, v := classifyExportRow(at(start.Add(-time.Second)), start, end, false); v != rowTooOld {
		t.Error("早于 start 的记录应判为 rowTooOld（提前收工的依据）")
	}
	if _, v := classifyExportRow(at(start), start, end, false); v != rowInWindow {
		t.Error("start 当刻应入选（左闭）")
	}
	if _, v := classifyExportRow(at(end), start, end, false); v != rowTooNew {
		t.Error("end 当刻不应入选（右开），否则会与下一笔充值的明细重复")
	}
	if _, v := classifyExportRow(at(end.Add(-time.Second)), start, end, false); v != rowInWindow {
		t.Error("end 前一秒应入选")
	}
	if _, v := classifyExportRow(appusage.LogRecord{ID: 2, CreatedAt: "不是时间"}, start, end, false); v != rowSkip {
		t.Error("时间不可解析的记录应为 rowSkip（跳过但不提前收工）")
	}
}

// TestClassifyExportRowCostBasis 余额按 actual_cost 扣减；billed_cost 是分销加价口径。
// 控制台用户的对账单必须用 actual_cost，否则配了 sell_rate 的用户会看到比扣款更大的合计。
func TestClassifyExportRowCostBasis(t *testing.T) {
	start := time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	rec := appusage.LogRecord{
		ID: 1, CreatedAt: start.Add(time.Hour).Format(time.RFC3339),
		ActualCost: 0.05, BilledCost: 0.15, // sell_rate 3 倍加价
	}

	row, v := classifyExportRow(rec, start, end, false)
	if v != rowInWindow || row.Cost != 0.05 {
		t.Errorf("控制台会话应导出 actual_cost=0.05，实际 %v", row.Cost)
	}
	row, v = classifyExportRow(rec, start, end, true)
	if v != rowInWindow || row.Cost != 0.15 {
		t.Errorf("API Key 会话应导出 billed_cost=0.15，实际 %v", row.Cost)
	}
}

// TestClassifyExportRowFailureDetection 判据是 error_code 而非 status：
// 上游对失败请求也计费时记录会以 status=success 落库，只看 status 会漏判。
func TestClassifyExportRowFailureDetection(t *testing.T) {
	start := time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	ts := start.Add(time.Hour).Format(time.RFC3339)

	row, v := classifyExportRow(appusage.LogRecord{
		ID: 1, CreatedAt: ts, Status: appusage.StatusSuccess,
		ErrorCode: "stream_aborted", ActualCost: 0.0676,
	}, start, end, false)
	if v != rowInWindow || !row.Failed {
		t.Error("status=success 但带错误码的记录应判为未成功")
	}

	row, v = classifyExportRow(appusage.LogRecord{
		ID: 2, CreatedAt: ts, Status: appusage.StatusSuccess,
	}, start, end, false)
	if v != rowInWindow || row.Failed {
		t.Error("正常成功记录不应判为未成功")
	}
}

// TestClassifyExportRowTokens tokens 是输入+缓存(读取与写入)+输出的合计——
// 缓存不计入的话，长会话行会呈现"几个 token 收几毛钱"。
func TestClassifyExportRowTokens(t *testing.T) {
	start := time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	row, v := classifyExportRow(appusage.LogRecord{
		ID: 1, CreatedAt: start.Add(time.Hour).Format(time.RFC3339),
		InputTokens: 2, CachedInputTokens: 1000, CacheCreationTokens: 250, OutputTokens: 8,
	}, start, end, false)
	if v != rowInWindow {
		t.Fatal("记录应入选")
	}
	if row.Tokens != 1260 {
		t.Errorf("tokens 应为 2+1000+250+8=1260，实际 %d", row.Tokens)
	}
}

// TestSortExportRows 账单需按时间正序；同一秒内用 ID 保持稳定次序。
func TestSortExportRows(t *testing.T) {
	base := time.Date(2026, 8, 12, 16, 0, 0, 0, time.UTC)
	rows := []exportRow{
		{ID: 9, CreatedAt: base.Add(2 * time.Second)},
		{ID: 5, CreatedAt: base},
		{ID: 3, CreatedAt: base},
		{ID: 7, CreatedAt: base.Add(time.Second)},
	}
	sortExportRows(rows)

	wantIDs := []int64{3, 5, 7, 9}
	for i, want := range wantIDs {
		if rows[i].ID != want {
			t.Fatalf("排序结果错误，第 %d 行应为 ID=%d，实际 %d", i, want, rows[i].ID)
		}
	}
}

// TestCSVSafeCell 模型名来自客户请求原文，以公式前缀开头的值在 Excel 里会被执行。
func TestCSVSafeCell(t *testing.T) {
	if got := csvSafeCell(`=HYPERLINK("https://evil")`); !strings.HasPrefix(got, "'") {
		t.Errorf("公式前缀应被转义为文本，实际 %q", got)
	}
	for _, s := range []string{"+1", "-1", "@cmd", "\tx"} {
		if got := csvSafeCell(s); !strings.HasPrefix(got, "'") {
			t.Errorf("%q 应被转义，实际 %q", s, got)
		}
	}
	if got := csvSafeCell("gpt-5.6-sol"); got != "gpt-5.6-sol" {
		t.Errorf("正常模型名不应被改写，实际 %q", got)
	}
	if got := csvSafeCell(""); got != "" {
		t.Errorf("空串应原样返回，实际 %q", got)
	}
}

func renderCSV(t *testing.T, rows []exportRow, notes exportNotes) string {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	writeUsageExportCSV(c, "usage-test.csv", rows, notes, time.Local)
	return w.Body.String()
}

// TestWriteUsageExportCSV 失败行必须是委婉措辞，且只在真的没扣费时才说"未计费"。
func TestWriteUsageExportCSV(t *testing.T) {
	base := time.Date(2026, 8, 12, 16, 0, 0, 0, time.Local)
	rows := []exportRow{
		{CreatedAt: base, Model: "gpt-5.6-sol", Tokens: 12258, Cost: 0.027},
		{CreatedAt: base.Add(time.Minute), Model: "claude-sonnet-5", Failed: true},
	}

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	writeUsageExportCSV(c, "usage-test.csv", rows, exportNotes{}, time.Local)

	body := w.Body.String()
	if !strings.HasPrefix(body, "\uFEFF") {
		t.Error("缺少 UTF-8 BOM，Excel 打开会乱码")
	}
	if got := w.Header().Get("Content-Disposition"); got != `attachment; filename="usage-test.csv"` {
		t.Errorf("Content-Disposition 错误: %s", got)
	}
	if !strings.Contains(body, "12258") {
		t.Error("缺少 tokens 数值")
	}
	if !strings.Contains(body, exportFailedFreeNote) {
		t.Error("零费用失败行应标注未计费")
	}
	// 不得泄漏 error_code / 上游报错原文。
	for _, leak := range []string{"upstream", "error_code", "client_error"} {
		if strings.Contains(body, leak) {
			t.Errorf("导出内容泄漏了内部错误信息: %s", leak)
		}
	}
	if !strings.Contains(body, "其中 1 条请求未成功，均未计费") {
		t.Errorf("汇总行口径错误:\n%s", body)
	}
}

// TestWriteUsageExportCSVChargedFailure 上游对失败也计费时，不能宣称未计费。
func TestWriteUsageExportCSVChargedFailure(t *testing.T) {
	rows := []exportRow{
		{CreatedAt: time.Now(), Model: "m", Cost: 0.5, Failed: true},
	}
	body := renderCSV(t, rows, exportNotes{})
	if strings.Contains(body, exportFailedFreeNote) {
		t.Error("已计费的失败行不应标注未计费")
	}
	if !strings.Contains(body, "另 1 条中途中断按已产生用量计费") {
		t.Errorf("汇总行应说明已计费条数:\n%s", body)
	}
}

// TestWriteUsageExportCSVAllSuccess 全部成功时不应出现"其中 0 条未成功"这种废话。
func TestWriteUsageExportCSVAllSuccess(t *testing.T) {
	rows := []exportRow{{CreatedAt: time.Now(), Model: "m", Tokens: 12, Cost: 0.01}}
	if body := renderCSV(t, rows, exportNotes{}); strings.Contains(body, "0 条请求未成功") {
		t.Errorf("无失败记录时不应输出失败说明:\n%s", body)
	}
}

// TestWriteUsageExportCSVMoneyPrecision 金额 4 位小数；合计基于未舍入值累加。
func TestWriteUsageExportCSVMoneyPrecision(t *testing.T) {
	rows := []exportRow{
		{CreatedAt: time.Now(), Model: "m", Cost: 0.0271},
		{CreatedAt: time.Now(), Model: "m", Cost: 0.0405},
	}
	body := renderCSV(t, rows, exportNotes{})
	if !strings.Contains(body, "0.0271") || !strings.Contains(body, "0.0405") {
		t.Errorf("明细金额应为 4 位小数:\n%s", body)
	}
	if !strings.Contains(body, "0.0676") {
		t.Errorf("合计应为未舍入累加:\n%s", body)
	}
}

// TestWriteUsageExportCSVNonTextNote 图像/视频调用没有 token，需要说明按次计费。
func TestWriteUsageExportCSVNonTextNote(t *testing.T) {
	rows := []exportRow{{CreatedAt: time.Now(), Model: "gpt-image-2", Cost: 0.2}}
	if body := renderCSV(t, rows, exportNotes{}); !strings.Contains(body, "非文本调用，按次计费") {
		t.Errorf("零 token 且有费用的行应说明按次计费:\n%s", body)
	}
}

// TestWriteUsageExportCSVTruncated 截断时金额不完整，必须显式声明。
func TestWriteUsageExportCSVTruncated(t *testing.T) {
	rows := []exportRow{{CreatedAt: time.Now(), Model: "m", Cost: 1}}
	body := renderCSV(t, rows, exportNotes{Truncated: true})
	if !strings.Contains(body, "部分合计") || !strings.Contains(body, "金额不代表该区间全部消耗") {
		t.Errorf("截断时应声明金额不完整:\n%s", body)
	}
}

// TestWriteUsageExportCSVWindowClamped 区间被收窄时要告知覆盖范围。
func TestWriteUsageExportCSVWindowClamped(t *testing.T) {
	rows := []exportRow{{CreatedAt: time.Now(), Model: "m", Cost: 1}}
	body := renderCSV(t, rows, exportNotes{WindowClamped: true})
	if !strings.Contains(body, "导出区间过长") {
		t.Errorf("区间收窄时应提示覆盖范围:\n%s", body)
	}
}
