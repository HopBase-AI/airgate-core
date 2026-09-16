package handler

import (
	"encoding/csv"
	"math"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	sdk "github.com/DouDOU-start/airgate-sdk/sdkgo"

	appusage "github.com/DouDOU-start/airgate-core/internal/app/usage"
	"github.com/DouDOU-start/airgate-core/internal/i18n"
	"github.com/DouDOU-start/airgate-core/internal/server/dto"
)

// usage_handler_export_test.go —— 充值消耗明细导出：
// 区间解析与收窄、边界归属、成本口径、文件名安全、失败行措辞、官方牌价验算列与汇总口径，
// 以及整份 CSV（表头 / 说明栏 / 汇总标签 / 提示行）的五语覆盖。

// TestMain 载入内嵌翻译：表头走 i18n.Tc，不加载的话取到的是键名本身（"export.time"），
// 断言会全绿而线上给客户的是一份表头写着变量名的账单。
func TestMain(m *testing.M) {
	if err := i18n.LoadEmbedded(); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}

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
	return renderCSVLang(t, rows, notes, "zh-CN,zh;q=0.9")
}

// renderCSVLang 按指定 Accept-Language 渲染。缺省测试用中文：i18n.Tc 不带头时回退英文，
// 而绝大多数断言写的是中文文案。
func renderCSVLang(t *testing.T, rows []exportRow, notes exportNotes, acceptLanguage string) string {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/usage/export", nil)
	if acceptLanguage != "" {
		c.Request.Header.Set("Accept-Language", acceptLanguage)
	}
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
	c.Request = httptest.NewRequest("GET", "/usage/export", nil)
	c.Request.Header.Set("Accept-Language", "zh-CN,zh;q=0.9")
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
	if !strings.Contains(body, i18n.T("zh", exportFailedFreeNoteKey)) {
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
	if strings.Contains(body, i18n.T("zh", exportFailedFreeNoteKey)) {
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

// ── 官方牌价验算列（docs/pricing-list-verification-sop.md §4.4）────────────────

// exportCNYDetail 造一条带牌价快照的明细：list_unit_price ÷ list_fx = unit_price。
func exportCNYDetail(listUnitPrice, fx, usdUnitPrice, accountCost float64) sdk.UsageCostDetail {
	return sdk.UsageCostDetail{
		Key: "input", Label: "输入 Token", AccountCost: accountCost, Currency: "USD",
		Metadata: map[string]string{
			"unit_price":      strconv.FormatFloat(usdUnitPrice, 'f', -1, 64),
			"list_currency":   "CNY",
			"list_unit_price": strconv.FormatFloat(listUnitPrice, 'f', -1, 64),
			"list_fx":         strconv.FormatFloat(fx, 'f', -1, 64),
		},
	}
}

// exportRecord 造一条落在 [start, end) 内的用量记录。
func exportRecord(id int64, at time.Time, model string, actualCost, rate float64, details ...sdk.UsageCostDetail) appusage.LogRecord {
	return appusage.LogRecord{
		ID: id, CreatedAt: at.Format(time.RFC3339), Model: model,
		ActualCost: actualCost, RateMultiplier: rate, UsageCostDetails: details,
	}
}

// TestClassifyExportRowOfficialNative 有牌价快照的行带上验算块，没有的行必须是 nil
// （而不是一个零值块——写进 CSV 会变成"官方费用 0.0000"，客户读成"这次官方不要钱"）。
func TestClassifyExportRowOfficialNative(t *testing.T) {
	start := time.Date(2026, 9, 16, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	at := start.Add(time.Hour)

	// 通义：¥12 / 1M input、10,000 tokens、折 0.7（SOP §7 首行）。
	withPrice, verdict := classifyExportRow(
		exportRecord(1, at, "qwen3-max", 1.7647*0.01*0.7, 0.7, exportCNYDetail(12, 6.8, 1.7647, 1.7647*0.01)),
		start, end, false)
	if verdict != rowInWindow || withPrice.Official == nil {
		t.Fatalf("带牌价快照的行应产出验算块: %+v", withPrice)
	}
	if withPrice.Official.Currency != "CNY" || withPrice.Official.FX != 6.8 || withPrice.Official.Discount != 0.7 {
		t.Fatalf("验算块 = %+v", withPrice.Official)
	}
	if math.Abs(withPrice.Official.Cost-0.12) > 1e-6 {
		t.Fatalf("官方费用 = %g，期望 ¥0.12", withPrice.Official.Cost)
	}

	// 官方价本就是美元的模型 / 历史行：没有快照，不该凭空造块。
	noPrice, verdict := classifyExportRow(
		exportRecord(2, at, "gpt-5.6-sol", 0.03, 0.7,
			sdk.UsageCostDetail{Key: "input", AccountCost: 0.03, Currency: "USD",
				Metadata: map[string]string{"unit_price": "3"}}),
		start, end, false)
	if verdict != rowInWindow || noPrice.Official != nil {
		t.Fatalf("无牌价快照的行不应有验算块: %+v", noPrice.Official)
	}
}

// TestClassifyExportRowOfficialNativeScoped API Key 会话（分销商的终端客户）不能拿到验算块：
// discount 就是分销商的折扣，露出去等于把毛利写进账单；且该视图导出的是 billed_cost，
// "官方费用 × 折 ÷ 折算率 = 扣费"本就不成立。
func TestClassifyExportRowOfficialNativeScoped(t *testing.T) {
	start := time.Date(2026, 9, 16, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	record := exportRecord(1, start.Add(time.Hour), "qwen3-max", 0.0124, 0.7,
		exportCNYDetail(12, 6.8, 1.7647, 1.7647*0.01))
	record.BilledCost = 0.05

	row, verdict := classifyExportRow(record, start, end, true)
	if verdict != rowInWindow {
		t.Fatal("记录应入选")
	}
	if row.Official != nil {
		t.Fatalf("API Key 会话不应带验算块，实际 %+v", row.Official)
	}
}

// TestWriteUsageExportCSVMixedListPrice 有牌价行 + 无牌价行混排：
// 无牌价行的三列必须是空串，且不参与官方费用合计。
func TestWriteUsageExportCSVMixedListPrice(t *testing.T) {
	base := time.Date(2026, 9, 16, 16, 0, 0, 0, time.Local)
	rows := []exportRow{
		{CreatedAt: base, Model: "qwen3-max", Tokens: 10000, Cost: 0.012353,
			Official: &dto.OfficialNativeCostResp{Currency: "CNY", FX: 6.8, Cost: 0.12, Discount: 0.7}},
		{CreatedAt: base.Add(time.Minute), Model: "gpt-5.6-sol", Tokens: 500, Cost: 0.03},
	}
	records := parseExportCSV(t, renderCSV(t, rows, exportNotes{}))

	if got := len(records[1]); got != exportColumns {
		t.Fatalf("表头应为 %d 列，实际 %d: %v", exportColumns, got, records[0])
	}
	// 第 2 行（索引 1）= 通义：币种 / 官方费用 / 折扣 / 折算率 四列都在。
	if records[1][exportColListCurrency] != "CNY" || records[1][exportColOfficialCost] != "0.1200" ||
		records[1][exportColDiscount] != "0.7000" || records[1][exportColListFX] != "6.8" {
		t.Fatalf("有牌价行的四列错误: %v", records[1])
	}
	// 第 3 行（索引 2）= 美元基准价模型：四列留空，不能是 0.0000。
	for _, idx := range []int{exportColListCurrency, exportColOfficialCost, exportColDiscount, exportColListFX} {
		if records[2][idx] != "" {
			t.Fatalf("无牌价行第 %d 列应留空，实际 %q（写 0 会被读成官方不要钱）", idx, records[2][idx])
		}
	}
	// 扣费列仍在最后第二列。
	if records[1][exportColActualCost] != "0.0124" || records[2][exportColActualCost] != "0.0300" {
		t.Fatalf("扣费列错位: %v / %v", records[1], records[2])
	}
	// 官方费用合计只累加有快照的行。
	if !strings.Contains(strings.Join(records[len(records)-1], ","), "0.1200") {
		t.Fatalf("官方费用合计行缺失或口径错误:\n%v", records)
	}
}

// TestWriteUsageExportCSVOfficialTotalsByCurrency 多币种必须各出一行：
// ¥ 与 $ 相加得到的是一个没有量纲的数，比不给合计更糟。
func TestWriteUsageExportCSVOfficialTotalsByCurrency(t *testing.T) {
	base := time.Date(2026, 9, 16, 16, 0, 0, 0, time.Local)
	rows := []exportRow{
		{CreatedAt: base, Model: "qwen3-max", Tokens: 10000, Cost: 0.012353,
			Official: &dto.OfficialNativeCostResp{Currency: "CNY", FX: 6.8, Cost: 0.12, Discount: 0.7}},
		{CreatedAt: base.Add(time.Minute), Model: "kling-v3", Cost: 0.330882,
			Official: &dto.OfficialNativeCostResp{Currency: "CNY", FX: 6.8, Cost: 3.0, Discount: 0.75}},
		{CreatedAt: base.Add(2 * time.Minute), Model: "some-jpy-model", Cost: 0.1,
			Official: &dto.OfficialNativeCostResp{Currency: "JPY", FX: 150, Cost: 15, Discount: 1}},
	}
	records := parseExportCSV(t, renderCSV(t, rows, exportNotes{}))

	totals := officialTotalRowsOf(records)
	if len(totals) != 2 {
		t.Fatalf("两种币种应各出一行合计，实际 %v\n%v", totals, records)
	}
	// 0.12 + 3.0 = 3.12，绝不能与 JPY 的 15 相加成 18.12。
	if totals["CNY"] != "3.1200" {
		t.Fatalf("CNY 合计 = %q，期望 3.1200", totals["CNY"])
	}
	if totals["JPY"] != "15.0000" {
		t.Fatalf("JPY 合计 = %q，期望 15.0000", totals["JPY"])
	}
}

// TestWriteUsageExportCSVVerificationIdentity 验收口径（SOP §7 末行）：
// Σ官方费用(¥) × 折 ÷ 折算率 ≈ Σ扣费($)，容差 0.01。
// 整体成立的前提是这批行折扣相同（同一分组）；跨分组按行验，本例也顺手逐行核一遍。
func TestWriteUsageExportCSVVerificationIdentity(t *testing.T) {
	const fx, discount = 6.8, 0.7
	base := time.Date(2026, 9, 16, 16, 0, 0, 0, time.Local)
	rows := make([]exportRow, 0, 3)
	for i, nativeCost := range []float64{0.12, 3.0, 51.0} {
		charged := nativeCost * discount / fx
		rows = append(rows, exportRow{
			CreatedAt: base.Add(time.Duration(i) * time.Minute), Model: "m", Cost: charged,
			Official: &dto.OfficialNativeCostResp{Currency: "CNY", FX: fx, Cost: nativeCost, Discount: discount},
		})
	}
	records := parseExportCSV(t, renderCSV(t, rows, exportNotes{}))

	var sumNative, sumCharged float64
	var officialTotal, chargedTotal, totalFX float64
	for _, record := range records {
		if len(record) != exportColumns {
			continue
		}
		switch {
		case record[exportColListCurrency] == "CNY" && record[exportColModel] == "" && record[exportColActualCost] == "":
			officialTotal = mustParseFloat(t, record[exportColOfficialCost])
			totalFX = mustParseFloat(t, record[exportColListFX])
		case record[exportColTime] == i18n.T("zh", "export.summary_total"):
			chargedTotal = mustParseFloat(t, record[exportColActualCost])
		case record[exportColListCurrency] == "CNY":
			native := mustParseFloat(t, record[exportColOfficialCost])
			charged := mustParseFloat(t, record[exportColActualCost])
			sumNative += native
			sumCharged += charged
			// 逐行验算全部取自表格本身（含折算率列）：客户手里只有这张表，
			// 任何一个数要靠外部知识补，这层「可验算」就没落地。
			rowFX := mustParseFloat(t, record[exportColListFX])
			if math.Abs(native*mustParseFloat(t, record[exportColDiscount])/rowFX-charged) > 0.0001 {
				t.Fatalf("行级验算不闭合: %v", record)
			}
		}
	}
	if totalFX != fx {
		t.Fatalf("官方费用合计行的折算率 = %g，期望 %g", totalFX, fx)
	}
	if math.Abs(officialTotal-sumNative) > 1e-6 {
		t.Fatalf("官方费用合计 %g ≠ 明细累加 %g", officialTotal, sumNative)
	}
	if math.Abs(chargedTotal-sumCharged) > 1e-6 {
		t.Fatalf("扣费合计 %g ≠ 明细累加 %g", chargedTotal, sumCharged)
	}
	if got := officialTotal * discount / fx; math.Abs(got-chargedTotal) > 0.01 {
		t.Fatalf("Σ官方费用 × 折 ÷ 折算率 = %g，与 Σ扣费 %g 差超过容差", got, chargedTotal)
	}
}

// TestExportHeaderFiveLocales 表头五语齐活：任何一语缺 key 都会退化成键名（"export.time"），
// 客户拿到的就是一份表头写着变量名的账单。
func TestExportHeaderFiveLocales(t *testing.T) {
	want := map[string][]string{
		"zh":    {"时间", "模型", "用量", "官方牌价币种", "官方费用（原币）", "折扣", "折算率", "扣费金额（$）", "说明"},
		"zh-HK": {"時間", "模型", "用量", "官方牌價幣種", "官方費用（原幣）", "折扣", "折算率", "扣費金額（$）", "說明"},
		"en":    {"Time", "Model", "Usage", "Official List Price Currency", "Official Cost (List Currency)", "Discount", "Exchange Rate", "Charged (USD)", "Note"},
		"ja":    {"日時", "モデル", "使用量", "公式価格の通貨", "公式料金（現地通貨）", "割引率", "換算レート", "請求額（$）", "備考"},
		"es":    {"Fecha y hora", "Modelo", "Uso", "Moneda del precio oficial", "Importe oficial (moneda original)", "Descuento", "Tipo de cambio", "Importe cobrado ($)", "Nota"},
	}
	headers := map[string]string{"zh": "zh-CN,zh;q=0.9", "zh-HK": "zh-HK", "en": "en-US,en;q=0.9", "ja": "ja", "es": "es-ES,es;q=0.9"}

	for lang, expected := range want {
		gin.SetMode(gin.TestMode)
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("GET", "/usage/export", nil)
		c.Request.Header.Set("Accept-Language", headers[lang])

		got := exportHeader(c)
		if len(got) != exportColumns {
			t.Fatalf("%s 表头列数 = %d，期望 %d", lang, len(got), exportColumns)
		}
		for i, cell := range got {
			if cell != expected[i] {
				t.Errorf("%s 表头第 %d 列 = %q，期望 %q", lang, i, cell, expected[i])
			}
			if strings.HasPrefix(cell, "export.") {
				t.Errorf("%s 表头第 %d 列退化成键名 %q（locales/%s.json 缺 key）", lang, i, cell, lang)
			}
		}
	}
}

// TestWriteUsageExportCSVListFXFallsBackToBlank 折算率列与其余三列同生共死：
// 无牌价快照的行留空。单给 6.8 而没有官方费用，客户只会更糊涂。
func TestWriteUsageExportCSVListFXFallsBackToBlank(t *testing.T) {
	rows := []exportRow{{CreatedAt: time.Now(), Model: "gpt-5.6-sol", Tokens: 100, Cost: 0.03}}
	records := parseExportCSV(t, renderCSV(t, rows, exportNotes{}))
	if records[1][exportColListFX] != "" {
		t.Fatalf("无牌价行的折算率列应留空，实际 %q", records[1][exportColListFX])
	}
}

// TestExportFXKeepsSnapshotLiteral 折算率写快照原貌：6.8 不能被补成 "6.8000"
// （客户会以为折算率精确到万分位），150 也不该带小数尾巴。
func TestExportFXKeepsSnapshotLiteral(t *testing.T) {
	cases := map[float64]string{6.8: "6.8", 150: "150", 7.25: "7.25", 0: "", -1: ""}
	for in, want := range cases {
		if got := exportFX(in); got != want {
			t.Errorf("exportFX(%g) = %q，期望 %q", in, got, want)
		}
	}
}

// TestWriteUsageExportCSVOfficialTotalsMixedFX 同币种内折算率不一致（改价前后的行同框）时，
// 合计行的折算率必须留空：随手挑一个填进去，客户拿它去除合计只会算出一个对不上的数。
func TestWriteUsageExportCSVOfficialTotalsMixedFX(t *testing.T) {
	base := time.Date(2026, 9, 16, 16, 0, 0, 0, time.Local)
	rows := []exportRow{
		{CreatedAt: base, Model: "qwen3-max", Cost: 0.0124,
			Official: &dto.OfficialNativeCostResp{Currency: "CNY", FX: 6.8, Cost: 0.12, Discount: 0.7}},
		{CreatedAt: base.Add(time.Minute), Model: "qwen3-max", Cost: 0.0117,
			Official: &dto.OfficialNativeCostResp{Currency: "CNY", FX: 7.2, Cost: 0.12, Discount: 0.7}},
	}
	records := parseExportCSV(t, renderCSV(t, rows, exportNotes{}))

	for _, record := range records {
		if len(record) != exportColumns || record[exportColListCurrency] == "" || record[exportColActualCost] != "" {
			continue
		}
		if record[exportColOfficialCost] != "0.2400" {
			t.Fatalf("官方费用合计 = %q，期望 0.2400", record[exportColOfficialCost])
		}
		if record[exportColListFX] != "" {
			t.Fatalf("折算率不一致时合计行的折算率应留空，实际 %q", record[exportColListFX])
		}
		return
	}
	t.Fatalf("未找到官方费用合计行:\n%v", records)
}

// ── 整份 CSV 的五语覆盖 ───────────────────────────────────────

// exportFullSampleRows 一份把所有文案分支都踩到的样本：成功行、零费用失败行、
// 已计费的中断行、零 token 的按次计费行，外加牌价快照撑起官方费用合计。
func exportFullSampleRows() []exportRow {
	base := time.Date(2026, 9, 16, 16, 0, 0, 0, time.Local)
	return []exportRow{
		{CreatedAt: base, Model: "qwen3-max", Tokens: 10000, Cost: 0.012353,
			Official: &dto.OfficialNativeCostResp{Currency: "CNY", FX: 6.8, Cost: 0.12, Discount: 0.7}},
		{CreatedAt: base.Add(time.Minute), Model: "claude-sonnet-5", Failed: true},
		{CreatedAt: base.Add(2 * time.Minute), Model: "glm-5.3", Tokens: 800, Cost: 0.0676, Failed: true},
		{CreatedAt: base.Add(3 * time.Minute), Model: "gpt-image-2", Cost: 0.2},
	}
}

// TestWriteUsageExportCSVAllLocales 整份 CSV（表头 / 说明栏 / 汇总标签 / 截断与收窄提示）
// 五语齐活。这是 2026-09-16 的欠账：表头已经五语了，说明栏与汇总行还是硬编码中文，
// 日 / 西语客户导出拿到的是一张中英混排表。
func TestWriteUsageExportCSVAllLocales(t *testing.T) {
	headers := map[string]string{
		"zh": "zh-CN,zh;q=0.9", "zh-HK": "zh-HK", "en": "en-US,en;q=0.9", "ja": "ja", "es": "es-ES,es;q=0.9",
	}
	// 每种语言都必须出现的文案：说明栏两种失败措辞 + 按次计费 + 汇总标签 + 条数 +
	// 混合失败汇总 + 截断 + 收窄提示 + 官方费用合计。
	keys := []string{
		"export.note_failed_free", "export.note_failed_charged", "export.note_non_text",
		"export.summary_partial_total", "export.summary_row_count",
		"export.summary_failed_mixed", "export.summary_truncated",
		"export.note_hint", "export.note_window_clamped", "export.official_cost_total",
	}

	for lang, header := range headers {
		body := renderCSVLang(t, exportFullSampleRows(),
			exportNotes{Truncated: true, WindowClamped: true}, header)

		for _, key := range keys {
			text := i18n.T(lang, key)
			// T 在缺 key 时回退键名本身，这正是"表头写着变量名的账单"的成因。
			if text == key {
				t.Fatalf("locales/%s.json 缺 key %s", lang, key)
			}
			want := longestLiteral(text)
			if want == "" {
				// 纯占位符文案（ja 的 "%d 件"）没有可比对的固定片段，交给键名退化断言兜。
				continue
			}
			if !strings.Contains(body, want) {
				t.Errorf("%s 导出缺少 %s 对应文案 %q:\n%s", lang, key, want, body)
			}
		}
		// 汇总行的条数与失败统计按语言渲染，不能退化成键名。
		if strings.Contains(body, "export.summary_") || strings.Contains(body, "export.note_") {
			t.Errorf("%s 导出出现未翻译的键名:\n%s", lang, body)
		}
	}
}

// TestWriteUsageExportCSVNoChineseLeakInWesternLocales 英 / 西语导出里不许出现中日韩字符。
// 一个漏网的硬编码中文足以让整张账单在客户那里变成乱码格——这条断言就是防它回来的。
func TestWriteUsageExportCSVNoChineseLeakInWesternLocales(t *testing.T) {
	for lang, header := range map[string]string{"en": "en-US,en;q=0.9", "es": "es-ES,es;q=0.9"} {
		body := renderCSVLang(t, exportFullSampleRows(),
			exportNotes{Truncated: true, WindowClamped: true}, header)
		for _, r := range body {
			if isCJKRune(r) {
				t.Fatalf("%s 导出出现中日韩字符 %q（硬编码中文漏网）:\n%s", lang, string(r), body)
			}
		}
	}
}

// longestLiteral 取文案里最长的固定片段（去掉 %d 占位符与首尾空白），
// 用于在整份 CSV 里定位这条文案。ja 的 "%d 件" 这类几乎全是占位符的返回空串。
func longestLiteral(text string) string {
	longest := ""
	for _, part := range strings.Split(text, "%d") {
		part = strings.TrimSpace(part)
		if len(part) > len(longest) {
			longest = part
		}
	}
	return longest
}

// isCJKRune 覆盖汉字与中文全角标点（，；（）等）——后者最容易随句子一起漏进英文文案。
func isCJKRune(r rune) bool {
	switch {
	case r >= 0x4E00 && r <= 0x9FFF: // CJK 统一表意文字
		return true
	case r >= 0x3000 && r <= 0x303F: // CJK 标点
		return true
	case r >= 0xFF01 && r <= 0xFF60: // 全角 ASCII 变体（，：；（））
		return true
	}
	return false
}

// parseExportCSV 解析导出内容（去掉 BOM），返回全部行。
func parseExportCSV(t *testing.T, body string) [][]string {
	t.Helper()
	reader := csv.NewReader(strings.NewReader(strings.TrimPrefix(body, "\uFEFF")))
	// 汇总区前的空行不该被当成列数不符。
	reader.FieldsPerRecord = -1
	records, err := reader.ReadAll()
	if err != nil {
		t.Fatalf("导出内容不是合法 CSV: %v\n%s", err, body)
	}
	return records
}

// officialTotalRowsOf 提取「官方费用合计」行（币种 → 金额）。
// 判据不依赖标签文案（表头语言随 Accept-Language 变）：只有合计行同时满足
// 「币种列有值」且「扣费列为空」——明细行的扣费列一定有值，主汇总行的币种列一定为空。
func officialTotalRowsOf(records [][]string) map[string]string {
	out := map[string]string{}
	for _, record := range records {
		if len(record) != exportColumns {
			continue
		}
		if record[exportColListCurrency] != "" && record[exportColActualCost] == "" {
			out[record[exportColListCurrency]] = record[exportColOfficialCost]
		}
	}
	return out
}

func mustParseFloat(t *testing.T, raw string) float64 {
	t.Helper()
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		t.Fatalf("数值单元格 %q 无法解析: %v", raw, err)
	}
	return value
}
