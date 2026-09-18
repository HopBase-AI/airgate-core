package handler

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	appusage "github.com/DouDOU-start/airgate-core/internal/app/usage"
	"github.com/DouDOU-start/airgate-core/internal/i18n"
	"github.com/DouDOU-start/airgate-core/internal/pkg/ledger"
	"github.com/DouDOU-start/airgate-core/internal/pkg/timezone"
	"github.com/DouDOU-start/airgate-core/internal/server/dto"
	"github.com/DouDOU-start/airgate-core/internal/server/response"
)

// 导出相关约束。
const (
	// exportPageSize 是向 service 分页取数的批大小，仅影响内存占用，不影响结果。
	exportPageSize = 1000
	// exportMaxRows 是单次导出的行数上限。底层按 created_at 倒序取数，
	// 触顶时留下的是最近的 N 条，末行会注明。
	exportMaxRows = 50000
	// exportMaxWindow 是允许导出的最长区间。超出时右边界被收窄（而不是报错——
	// 老账户最后一笔充值可能已经消耗了一年以上，硬拒会让导出按钮永远失败）。
	exportMaxWindow = 400 * 24 * time.Hour
)

// 失败请求对客户的措辞：只说明结果，不暴露 error_code / 上游报错原文。
//
// 绝大多数失败行费用为 0；但客户端中途断开（client_canceled / stream_aborted）时
// token 已经产出，这类记录会以 status=success 带错误码落库且确实计了费
// （见 plugin/outcome.go recordUsageWithFailureOverride）。
// 对这类行宣称"未计费"会让明细与扣费金额对不上，因此措辞按实际扣费分两种。
//
// ⚠️ 文案一律经 key 取（locales/*.json 五份），禁止在本文件里写裸中文：
// 表头是英文、说明栏是中文的混排表，日/西语客户拿到手根本读不了（2026-09-16）。
const (
	exportFailedFreeNoteKey = "export.note_failed_free"
	exportFailedNoteKey     = "export.note_failed_charged"
	exportNonTextNoteKey    = "export.note_non_text"
)

// UserUsageExport 导出当前用户在指定时间区间内的使用明细（CSV）。
//
// 场景：客户拿到一笔充值后想知道"这笔钱花在哪了"。前端（含支付插件的订单页）
// 传入该笔充值的到账时刻与下一笔充值的到账时刻作为区间边界。
//
// 时间边界用 RFC3339，区间取左闭右开 [start, end)，这样同一天内的多笔充值
// 不会互相串行。底层 ListUser 只支持按日期过滤，因此先用日期把范围收窄，
// 再在内存里按精确时刻裁剪。
func (h *UsageHandler) UserUsageExport(c *gin.Context) {
	userID, ok := usageUserID(c)
	if !ok {
		response.Unauthorized(c, "用户未认证")
		return
	}

	start, end, windowClamped, err := parseExportRange(c)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	tz := c.Query("tz")
	loc := timezone.Resolve(tz)

	// 导出跟随页面上的筛选：企业主筛了某个成员再导出，就该只拿那个成员的明细
	// （典型场景是给成员出月度账单），而不是把整个账号的记录全导出去。
	var filters dto.UsageExportFilterQuery
	if err := c.ShouldBindQuery(&filters); err != nil {
		response.BindError(c, err)
		return
	}

	// API Key 登录场景沿用列表接口的收敛规则：只能导出该 Key（或所属成员）自己的记录，
	// 请求里带的筛选一概忽略。
	apiKeyFilter, memberFilter, scoped := sessionUsageScope(c, filters.APIKeyID, filters.MemberID)
	// 部门负责人导出的也是本人 ∪ 所负责部门，请求里的成员筛选作为下钻叠加。
	managerScope := sessionManagerScope(c)
	if managerScope != nil {
		memberFilter = filters.MemberID
	}

	rows, truncated, err := h.collectExportRows(c, exportCollectParams{
		userID:       int64(userID),
		start:        start,
		end:          end,
		loc:          loc,
		tz:           tz,
		apiKeyFilter: apiKeyFilter,
		memberFilter: memberFilter,
		deptFilter:   sessionDepartmentFilter(scoped, filters.DepartmentID),
		scoped:       scoped,
		managerScope: managerScope,
	})
	if err != nil {
		handleUsageError("导出用户使用明细失败", err)
		response.InternalError(c, "导出失败")
		return
	}

	writeUsageExportCSV(c, exportFileName(c.Query("order_no"), start), rows, exportNotes{
		Truncated:     truncated,
		WindowClamped: windowClamped,
	}, loc)
}

type exportCollectParams struct {
	userID       int64
	start        time.Time
	end          time.Time
	loc          *time.Location
	tz           string
	apiKeyFilter *int64
	memberFilter *int64
	deptFilter   *int64
	managerScope *appusage.ManagerScope
	scoped       bool
}

// exportRow 是落到 CSV 的一行，字段刻意精简：客户只关心时间、模型、用量、扣费。
type exportRow struct {
	ID        int64
	CreatedAt time.Time
	Model     string
	// Tokens 是输入 + 缓存（读取与写入）+ 输出的合计。缓存必须计入：
	// 长会话里它常是输入输出的几十倍、费用主要由它驱动，
	// 漏掉的话客户会觉得"几个 token 收几毛钱"。
	Tokens int
	Cost   float64
	Failed bool
	// Official 是厂商官方牌价口径的只读验算块（币种 / 折算率 / 原币费用 / 折扣），
	// 与用户侧使用记录 tooltip 同一份计算（officialNativeCost），三处展示不会各算一遍。
	//
	// nil = 本行没有牌价快照：历史行、官方价本就是美元的模型、以及 API Key 会话。
	// 此时对应的四列（币种 / 官方费用 / 折扣 / 折算率）**留空**而不是写 0——
	// "官方费用 0.0000" 会被读成"这次官方不要钱"。
	Official *dto.OfficialNativeCostResp
}

// exportRowVerdict 是一条记录相对导出区间的判定结果。
type exportRowVerdict int

const (
	rowInWindow exportRowVerdict = iota
	// rowTooNew 晚于区间右界。倒序结果的头部，继续向旧翻。
	rowTooNew
	// rowTooOld 早于区间左界。倒序结果已越过区间，之后只会更旧，可提前收工。
	rowTooOld
	// rowSkip 时间不可解析，无法归属到任何充值区间，跳过而不是错误落盘。
	rowSkip
)

// exportNotes 汇聚需要写进 CSV 尾部的提示。
type exportNotes struct {
	Truncated     bool
	WindowClamped bool
}

// collectExportRows 分页取数并按精确时刻裁剪，返回按时间升序排列的明细。
// 第二个返回值表示是否因触顶 exportMaxRows 而截断。
func (h *UsageHandler) collectExportRows(c *gin.Context, p exportCollectParams) ([]exportRow, bool, error) {
	// ListUser 只认日期（见 timezone.ParseDate），这里放宽到整日再在内存里精确裁剪。
	startDate := p.start.In(p.loc).Format("2006-01-02")
	endDate := p.end.In(p.loc).Format("2006-01-02")

	rows := make([]exportRow, 0, 256)
	// 底层按 created_at 倒序 + offset 分页；导出期间若有新请求写入，整个结果集会
	// 向后推一格，翻到下一页时会重复看到上一页末尾的记录。按 ID 去重兜住这种漂移。
	seen := make(map[int64]struct{}, 256)
	truncated := false

collect:
	for page := 1; ; page++ {
		result, err := h.service.ListUser(c.Request.Context(), p.userID, appusage.ListFilter{
			Page:         page,
			PageSize:     exportPageSize,
			APIKeyID:     p.apiKeyFilter,
			MemberID:     p.memberFilter,
			DepartmentID: p.deptFilter,
			Manager:      p.managerScope,
			StartDate:    startDate,
			EndDate:      endDate,
			TZ:           p.tz,
			ScopedToKey:  p.scoped,
		})
		if err != nil {
			return nil, false, err
		}
		if len(result.List) == 0 {
			break
		}

		for _, item := range result.List {
			if _, dup := seen[item.ID]; dup {
				continue
			}
			row, verdict := classifyExportRow(item, p.start, p.end, p.scoped)
			switch verdict {
			case rowTooOld:
				// 倒序结果已越过区间左界，后面的页只会更旧——提前收工，
				// 避免同日两笔充值时把当天几十万行全翻一遍。
				break collect
			case rowTooNew, rowSkip:
				continue
			}
			// 上限判定放在收下第 N+1 条之前：恰好 N 条的完整导出不该背"已截断"的提示。
			if len(rows) >= exportMaxRows {
				truncated = true
				break collect
			}
			seen[item.ID] = struct{}{}
			rows = append(rows, row)
		}

		if len(result.List) < exportPageSize {
			break
		}
	}

	sortExportRows(rows)
	return rows, truncated, nil
}

// classifyExportRow 判定一条记录与 [start, end) 的关系；在窗内时同时构造导出行。
func classifyExportRow(item appusage.LogRecord, start, end time.Time, scoped bool) (exportRow, exportRowVerdict) {
	createdAt, err := time.Parse(time.RFC3339, item.CreatedAt)
	if err != nil {
		return exportRow{}, rowSkip
	}
	// 左闭右开：下一笔充值到账的那一刻起算下一个区间。
	// end 缺省为请求发起时刻，导出期间新产生的调用天然被排除在外。
	if createdAt.Before(start) {
		return exportRow{}, rowTooOld
	}
	if !createdAt.Before(end) {
		return exportRow{}, rowTooNew
	}

	// 余额按 actual_cost 扣减（billing/recorder.go AddBalance(-ActualCost)），
	// billed_cost 是分销加价后的口径、只对 API Key 会话（end customer）成立。
	// 对账单必须与真实扣款一致，否则配了 sell_rate 的用户会看到比扣款更大的合计。
	cost := item.ActualCost
	if scoped {
		cost = item.BilledCost
	}

	// 牌价验算块对 API Key 会话一律不给（与 CustomerUsageLogResp 同口径，SOP §6.3）：
	// discount 就是分销商拿到的折扣，露给终端客户等于把毛利写进账单；而且该视图导出的是
	// billed_cost，"官方费用 × 折 ÷ 折算率 = 扣费"这条等式本来也不成立。
	var official *dto.OfficialNativeCostResp
	if !scoped {
		official = officialNativeCost(item)
	}

	return exportRow{
		ID:        item.ID,
		CreatedAt: createdAt,
		Model:     item.Model,
		Tokens:    item.InputTokens + item.CachedInputTokens + item.CacheCreationTokens + item.OutputTokens,
		Cost:      cost,
		Official:  official,
		// 与控制台「只看失败」同口径：判据是 error_code 而非 status。
		// 上游对失败请求也计费时，记录会以 status=success 落库但带错误码，
		// 只看 status 会把这类行当成正常调用展示给客户。
		Failed: item.ErrorCode != "",
	}, rowInWindow
}

// sortExportRows 取数是倒序的，账单按时间正序读起来才顺；ID 作为同一秒内记录的稳定次序。
func sortExportRows(rows []exportRow) {
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].CreatedAt.Equal(rows[j].CreatedAt) {
			return rows[i].ID < rows[j].ID
		}
		return rows[i].CreatedAt.Before(rows[j].CreatedAt)
	})
}

// parseExportRange 解析并校验导出区间。end 缺省为当前时刻（最新一笔充值仍在消耗中）。
// 第三个返回值表示区间因超长而被收窄。
//
// 边界统一截断到整秒：使用记录的时间经 RFC3339 序列化后只有秒级精度，而充值
// 到账时刻带毫秒；不对齐的话，充值后同一秒内的调用会被划进上一笔的账单。
func parseExportRange(c *gin.Context) (time.Time, time.Time, bool, error) {
	raw := c.Query("start_time")
	if raw == "" {
		return time.Time{}, time.Time{}, false, fmt.Errorf("缺少 start_time")
	}
	start, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, time.Time{}, false, fmt.Errorf("start_time 需为 RFC3339 时间")
	}

	end := time.Now()
	if rawEnd := c.Query("end_time"); rawEnd != "" {
		end, err = time.Parse(time.RFC3339, rawEnd)
		if err != nil {
			return time.Time{}, time.Time{}, false, fmt.Errorf("end_time 需为 RFC3339 时间")
		}
	}
	start = start.Truncate(time.Second)
	end = end.Truncate(time.Second)
	if !start.Before(end) {
		return time.Time{}, time.Time{}, false, fmt.Errorf("start_time 必须早于 end_time")
	}
	// 超长区间收窄而非拒绝：该端点无限流，start_time=1970 这类请求会把整张表
	// 扫一遍；但硬拒会让"最后一笔充值太久"的老账户永远导不出来。
	clamped := false
	if end.Sub(start) > exportMaxWindow {
		end = start.Add(exportMaxWindow)
		clamped = true
	}
	return start, end, clamped, nil
}

// exportFileName 用订单号命名，便于客户把文件与充值单据对上；订单号缺省时退回日期。
func exportFileName(orderNo string, start time.Time) string {
	if orderNo != "" && isSafeFileToken(orderNo) {
		return "usage-" + orderNo + ".csv"
	}
	return "usage-" + start.Format("20060102") + ".csv"
}

// isSafeFileToken 只放行字母数字与连字符，避免订单号参数污染 Content-Disposition。
func isSafeFileToken(s string) bool {
	if len(s) > 64 {
		return false
	}
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r == '-', r == '_':
		default:
			return false
		}
	}
	return true
}

// csvSafeCell 防 CSV 公式注入：model 名来自客户请求原文并原样落库，
// 以 = + - @ 等开头的值在 Excel / WPS 里会被当公式执行。前缀单引号使其成为文本。
func csvSafeCell(s string) string {
	if s == "" {
		return s
	}
	switch s[0] {
	case '=', '+', '-', '@', '\t', '\r':
		return "'" + s
	}
	return s
}

// exportMoney 金额统一 4 位小数；合计基于未舍入值累加，与扣款分毫对齐。
func exportMoney(v float64) string {
	return strconv.FormatFloat(v, 'f', 4, 64)
}

// exportRate 折扣同样定长 4 位小数：0.7 这类值直接 FormatFloat(-1) 会被浮点误差
// 写成 0.7000000000000001，客户看到的验算式就不像人算得出来的。
func exportRate(v float64) string {
	return strconv.FormatFloat(v, 'f', 4, 64)
}

// exportDivisor 折算率列的值：验算式里真正要除的那个数（账本除数 = 牌价折算率 ÷ 账本口径，
// 见 internal/pkg/ledger）。用最短表示——它本质是个常数（"6.8" / "150" / "22.0588"），
// 补成 "6.8000" 只会让人误以为折算率精确到万分位。
//
// 除数为 1 时留空而不写 "1"：账本币种与牌价币种相同，本来就不发生折算，
// 表里多一列「÷1」只会让客户以为自己漏读了什么。
func exportDivisor(v float64) string {
	if v <= 0 || v == 1 {
		return ""
	}
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// exportHeader 表头（九列，列序见 docs/pricing-list-verification-sop.md §4.4，勿随意调换：
// epay 订单页「导出明细」复用同一端点，客户也按列序对账）。
//
// 「折算率」夹在折扣与扣费金额之间，是为了让四列从左到右正好读成验算式本身：
//
//	官方费用（原币） × 折扣 ÷ 折算率 = 扣费金额（账本币种）
//
// 少了折算率这一列，牌价币种与账本币种不同时客户手里只有两头、缺中间那个除数，
// 验算根本做不下去——整层「可验算账单」也就白做了。两币种相同时该列留空
// （除数为 1，不发生折算），三列照样读成 官方费用 × 折扣 = 扣费金额。
//
// 语言按 Accept-Language 解析（i18n.Tc → 默认英文），整份 CSV（含说明栏、汇总标签、
// 提示行）统一走 key，不留硬编码中文。
func exportHeader(c *gin.Context) []string {
	return []string{
		i18n.Tc(c, "export.time"),
		i18n.Tc(c, "export.model"),
		i18n.Tc(c, "export.usage"),
		i18n.Tc(c, "export.list_currency"),
		i18n.Tc(c, "export.official_cost_native"),
		i18n.Tc(c, "export.discount"),
		i18n.Tc(c, "export.list_fx"),
		// 扣费金额的币种随账本走，不写死 $：¥ 账本下扣的是人民币，标成 $ 会让
		// 「¥官方费用 × 折 = $扣费」这条等式在同一行里自相矛盾。
		i18n.Tc(c, "export.actual_cost", ledger.Currency),
		i18n.Tc(c, "export.note"),
	}
}

// 列索引：汇总行与提示行按下标填格，散在各处写字面量早晚错位。
const (
	exportColTime = iota
	exportColModel
	exportColUsage
	exportColListCurrency
	exportColOfficialCost
	exportColDiscount
	exportColListFX
	exportColActualCost
	exportColNote
	// exportColumns 表头列数，汇总行按它对齐——写少了 Excel 会把后面的列错位读成上一列。
	exportColumns
)

// officialTotals 按币种累加官方牌价费用。
//
// **必须按币种分桶**：不同币种相加得到的是一个没有量纲的数字，比不给合计更糟。
// 现网只有 CNY 一种，但别的原生币牌价随时可能进来，届时不该悄悄加成一堆。
type officialTotals map[string]*officialTotal

// officialTotal 一个币种的官方费用累计，外加该币种的账本除数。
type officialTotal struct {
	cost float64
	// divisor 该币种各行的账本除数；mixedDivisor 为真时这些行并不一致。
	divisor float64
	// mixedDivisor 同币种内出现了不同除数（改价前后的行同框）。此时合计行的折算率列
	// 留空：随手挑一个填进去，客户用它去除合计只会算出一个对不上的数。
	mixedDivisor bool
}

func (t officialTotals) add(block *dto.OfficialNativeCostResp) {
	if block == nil || block.Currency == "" {
		return
	}
	entry, ok := t[block.Currency]
	if !ok {
		entry = &officialTotal{}
		t[block.Currency] = entry
	}
	entry.cost += block.TotalCost()
	switch {
	case block.Divisor <= 0:
	case entry.divisor == 0:
		entry.divisor = block.Divisor
	case entry.divisor != block.Divisor:
		entry.mixedDivisor = true
	}
}

// currencies 返回按字母序排好的币种，保证同一份数据每次导出的行序一致（便于 diff 对账）。
func (t officialTotals) currencies() []string {
	out := make([]string, 0, len(t))
	for currency := range t {
		out = append(out, currency)
	}
	sort.Strings(out)
	return out
}

func writeUsageExportCSV(c *gin.Context, filename string, rows []exportRow, notes exportNotes, loc *time.Location) {
	if loc == nil {
		loc = time.Local
	}
	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	c.Status(http.StatusOK)

	// UTF-8 BOM：没有它 Excel 打开中文表头会是乱码。
	_, _ = c.Writer.WriteString("\uFEFF")

	w := csv.NewWriter(c.Writer)

	_ = w.Write(exportHeader(c))

	var total float64
	failed := 0
	failedCharged := 0
	totals := officialTotals{}
	for _, row := range rows {
		note := ""
		if row.Failed {
			failed++
			if row.Cost > 0 {
				note = i18n.Tc(c, exportFailedNoteKey)
				failedCharged++
			} else {
				note = i18n.Tc(c, exportFailedFreeNoteKey)
			}
		}
		// 图像 / 视频等非文本调用没有 token，只看数字会像是凭空扣费。
		if note == "" && row.Cost > 0 && row.Tokens == 0 {
			note = i18n.Tc(c, exportNonTextNoteKey)
		}
		total += row.Cost

		// 四列同生共死：没有牌价快照、或计费本就不满足验算等式（固定图价等）的行
		// 一律留空，不写 0（见 exportRow.Official）。折算率也在其中——单给一个 6.8
		// 而没有官方费用，客户只会更糊涂。
		listCurrency, officialCost, discount, divisor := "", "", "", ""
		if row.Official != nil {
			listCurrency = row.Official.Currency
			// 全额 + 摊回后的折：缓存读不吃折扣的行在这里仍要满足「官方费用 × 折扣 = 扣费」，
			// CSV 只有一格折扣可填，分档等式塞不进去。
			officialCost = exportMoney(row.Official.TotalCost())
			discount = exportRate(row.Official.EffectiveDiscount())
			divisor = exportDivisor(row.Official.Divisor)
			totals.add(row.Official)
		}

		_ = w.Write([]string{
			// 用调用方声明的时区渲染，与前端表格里显示的时间保持一致。
			row.CreatedAt.In(loc).Format("2006-01-02 15:04:05"),
			csvSafeCell(row.Model),
			strconv.Itoa(row.Tokens),
			listCurrency,
			officialCost,
			discount,
			divisor,
			exportMoney(row.Cost),
			note,
		})
	}

	// 汇总行：客户最常问的就是"一共花了多少、有多少次没成功"。
	// 全部成功时不写"其中 0 条未成功"这种废话。
	summary := ""
	switch {
	case failedCharged > 0:
		summary = i18n.Tc(c, "export.summary_failed_mixed", failed-failedCharged, failedCharged)
	case failed > 0:
		summary = i18n.Tc(c, "export.summary_failed_free", failed)
	}
	label := i18n.Tc(c, "export.summary_total")
	if notes.Truncated {
		// 取数是倒序的，触顶时留下的是最近一批，最早的那段被丢掉了——
		// 必须说清楚，否则客户会拿这个偏小的金额来质疑扣费。
		label = i18n.Tc(c, "export.summary_partial_total")
		if summary != "" {
			// 分隔符随语言走：中文用「；」、英文用「; 」，硬编码一个就会在另一种语言里刺眼。
			summary += i18n.Tc(c, "export.summary_separator")
		}
		summary += i18n.Tc(c, "export.summary_truncated", exportMaxRows)
	}
	_ = w.Write(nil)
	summaryRow := make([]string, exportColumns)
	summaryRow[exportColTime] = label
	summaryRow[exportColModel] = i18n.Tc(c, "export.summary_row_count", len(rows))
	summaryRow[exportColActualCost] = exportMoney(total)
	summaryRow[exportColNote] = summary
	_ = w.Write(summaryRow)

	// 官方费用合计按币种各出一行，落在「官方牌价币种 / 官方费用(原币) / 折算率」三列下。
	// 验算口径：Σ官方费用(原币) × 折 ÷ 除数 ≈ Σ扣费——整体成立的前提是这批行折扣相同
	// （同一分组），跨分组请按行验（SOP §7 末行的注）。折扣列不填：合计行的折扣无法定义。
	for _, currency := range totals.currencies() {
		entry := totals[currency]
		officialRow := make([]string, exportColumns)
		officialRow[exportColTime] = i18n.Tc(c, "export.official_cost_total")
		officialRow[exportColListCurrency] = currency
		officialRow[exportColOfficialCost] = exportMoney(entry.cost)
		if !entry.mixedDivisor {
			officialRow[exportColListFX] = exportDivisor(entry.divisor)
		}
		_ = w.Write(officialRow)
	}

	if notes.WindowClamped {
		clampRow := make([]string, exportColumns)
		clampRow[exportColTime] = i18n.Tc(c, "export.note_hint")
		clampRow[exportColModel] = i18n.Tc(c, "export.note_window_clamped", int(exportMaxWindow.Hours()/24))
		_ = w.Write(clampRow)
	}

	// 状态码在首字节前已提交，中途断开无法改写响应；至少把写入失败记进日志，
	// 否则客户拿到一份缺尾的"完整"账单时服务端毫无线索。
	w.Flush()
	if err := w.Error(); err != nil {
		handleUsageError("导出 CSV 写出中断", err)
	}
}
