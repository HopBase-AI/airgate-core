package handler

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	appusage "github.com/DouDOU-start/airgate-core/internal/app/usage"
	"github.com/DouDOU-start/airgate-core/internal/server/middleware"
)

// usage_handler_routes_test.go —— 用户侧使用记录接口的保密边界：
// 上游账号身份既不能出现在响应体，也不能借 account_id 筛选被反推出来。

// stubUsageRepo 只记录 ListUser 收到的筛选条件并回放固定记录，
// 其余方法给零值——本文件只测用户侧列表接口。
type stubUsageRepo struct {
	lastFilter      appusage.ListFilter
	lastTrendFilter appusage.TrendFilter
	records         []appusage.LogRecord
}

func (s *stubUsageRepo) ListUser(_ context.Context, _ int64, filter appusage.ListFilter) ([]appusage.LogRecord, int64, error) {
	s.lastFilter = filter
	return s.records, int64(len(s.records)), nil
}

func (s *stubUsageRepo) ListAdmin(context.Context, appusage.ListFilter) ([]appusage.LogRecord, int64, error) {
	return nil, 0, nil
}

func (s *stubUsageRepo) SummaryUser(context.Context, int64, appusage.StatsFilter) (appusage.Summary, error) {
	return appusage.Summary{}, nil
}

func (s *stubUsageRepo) SummaryAdmin(context.Context, appusage.StatsFilter) (appusage.Summary, error) {
	return appusage.Summary{}, nil
}

func (s *stubUsageRepo) StatsByModel(context.Context, appusage.StatsFilter) ([]appusage.ModelStats, error) {
	return nil, nil
}

func (s *stubUsageRepo) StatsByUser(context.Context, appusage.StatsFilter) ([]appusage.UserStats, error) {
	return nil, nil
}

func (s *stubUsageRepo) StatsByAccount(context.Context, appusage.StatsFilter) ([]appusage.AccountStats, error) {
	return nil, nil
}

func (s *stubUsageRepo) StatsByGroup(context.Context, appusage.StatsFilter) ([]appusage.GroupStats, error) {
	return nil, nil
}

func (s *stubUsageRepo) TrendEntries(_ context.Context, filter appusage.TrendFilter) ([]appusage.TrendEntry, error) {
	s.lastTrendFilter = filter
	return nil, nil
}

// upstreamIdentityRecord 一条带完整上游账号身份的记录，用来验证脱敏。
func upstreamIdentityRecord() appusage.LogRecord {
	return appusage.LogRecord{
		ID:           9,
		UserID:       82,
		APIKeyID:     214,
		AccountID:    48,
		AccountName:  "upstream-pool-a",
		AccountEmail: "pool-a@upstream.example",
		GroupID:      1,
		Platform:     "openai",
		Model:        "gpt-5.6",
		Status:       appusage.StatusError,
		ErrorCode:    appusage.ErrorCodeStreamAborted,
		ErrorStatus:  502,
		ErrorMessage: "上游中继返回 upstream-pool-a 内部错误",
		CreatedAt:    "2026-09-01T04:39:05+08:00",
	}
}

// TestUserUsageHidesUpstreamIdentity 用户侧列表响应不得带上游账号身份。
// 前端只在 adminView 渲染这些字段，但字段留在响应体里，用户在 devtools 或
// 直接调接口就能看到是哪家上游在供货——脱敏必须落在服务端。
func TestUserUsageHidesUpstreamIdentity(t *testing.T) {
	repo := &stubUsageRepo{records: []appusage.LogRecord{upstreamIdentityRecord()}}
	handler := NewUsageHandler(appusage.NewService(repo))

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest("GET", "/usage?page=1&page_size=20", nil)
	c.Set("user_id", 82)

	handler.UserUsage(c)

	if recorder.Code != 200 {
		t.Fatalf("状态码 = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	body := recorder.Body.String()
	for _, leak := range []string{"account_id", "account_name", "account_email", "upstream-pool-a"} {
		if strings.Contains(body, leak) {
			t.Fatalf("用户侧响应泄漏上游账号信息 %q: %s", leak, body)
		}
	}
	// 失败分类仍要给到用户，只是不带原文与上游身份。
	if !strings.Contains(body, appusage.ErrorCodeStreamAborted) {
		t.Fatalf("用户侧响应应保留失败分类: %s", body)
	}
}

// TestUserUsageIgnoresAccountFilter 用户侧不接受 account_id 筛选。
// 响应体脱敏之后，若还认这个筛选参数，用户可以逐个 ID 试出
// 「哪条请求由哪个上游账号供货」，等于换个姿势拿回同样的信息。
func TestUserUsageIgnoresAccountFilter(t *testing.T) {
	repo := &stubUsageRepo{records: []appusage.LogRecord{upstreamIdentityRecord()}}
	handler := NewUsageHandler(appusage.NewService(repo))

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest("GET", "/usage?page=1&page_size=20&account_id=48", nil)
	c.Set("user_id", 82)

	handler.UserUsage(c)

	if recorder.Code != 200 {
		t.Fatalf("状态码 = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	if repo.lastFilter.AccountID != nil {
		t.Fatalf("account_id 筛选不应下传，实际 = %d", *repo.lastFilter.AccountID)
	}
}

// TestAdminUsageKeepsUpstreamIdentity 管理员视角必须保留上游账号身份，排障要靠它。
func TestAdminUsageKeepsUpstreamIdentity(t *testing.T) {
	resp := toUsageLogResp(upstreamIdentityRecord())
	if resp.AccountID != 48 || resp.AccountName != "upstream-pool-a" || resp.AccountEmail != "pool-a@upstream.example" {
		t.Fatalf("管理员视角应保留上游账号身份，实际 = %+v", resp)
	}
	if resp.ErrorMessage == "" {
		t.Fatal("管理员视角应保留失败原文")
	}
}

// TestUserUsageTrendFollowsMemberFilter 企业主给趋势筛成员时必须下传。
// 之前 dto 收了 member_id 但 handler 传的是 nil，参数是死的。
func TestUserUsageTrendFollowsMemberFilter(t *testing.T) {
	repo := &stubUsageRepo{}
	handler := NewUsageHandler(appusage.NewService(repo))

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest("GET", "/usage/trend?granularity=day&member_id=7&api_key_id=9", nil)
	c.Set("user_id", 82)

	handler.UserUsageTrend(c)

	if recorder.Code != 200 {
		t.Fatalf("状态码 = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	if repo.lastTrendFilter.MemberID == nil || *repo.lastTrendFilter.MemberID != 7 {
		t.Fatalf("member_id 未下传到趋势筛选: %+v", repo.lastTrendFilter.MemberID)
	}
	if repo.lastTrendFilter.APIKeyID == nil || *repo.lastTrendFilter.APIKeyID != 9 {
		t.Fatalf("api_key_id 未下传到趋势筛选: %+v", repo.lastTrendFilter.APIKeyID)
	}
}

// TestUserUsageTrendMemberSessionOverridesRequestedFilter 成员密钥会话收敛到本成员，
// 请求里带别人的 member_id 一律无效——否则成员能查到同事的用量。
func TestUserUsageTrendMemberSessionOverridesRequestedFilter(t *testing.T) {
	repo := &stubUsageRepo{}
	handler := NewUsageHandler(appusage.NewService(repo))

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest("GET", "/usage/trend?granularity=day&member_id=999", nil)
	c.Set("user_id", 82)
	c.Set(middleware.CtxKeyMemberID, 7)

	handler.UserUsageTrend(c)

	if recorder.Code != 200 {
		t.Fatalf("状态码 = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	if repo.lastTrendFilter.MemberID == nil || *repo.lastTrendFilter.MemberID != 7 {
		t.Fatalf("成员会话应被收敛到 member=7，实际 = %+v", repo.lastTrendFilter.MemberID)
	}
	if repo.lastTrendFilter.APIKeyID != nil {
		t.Fatalf("成员会话不该按单把密钥收敛，实际 = %d", *repo.lastTrendFilter.APIKeyID)
	}
}

// TestUserUsageExportFollowsMemberFilter 导出跟随成员筛选：企业主给成员出账单时
// 不能把整个账号的明细都导出去。
func TestUserUsageExportFollowsMemberFilter(t *testing.T) {
	repo := &stubUsageRepo{records: []appusage.LogRecord{upstreamIdentityRecord()}}
	handler := NewUsageHandler(appusage.NewService(repo))

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest("GET", "/usage/export?start_time=2026-09-01T00:00:00Z&member_id=7", nil)
	c.Set("user_id", 82)

	handler.UserUsageExport(c)

	if recorder.Code != 200 {
		t.Fatalf("状态码 = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	if repo.lastFilter.MemberID == nil || *repo.lastFilter.MemberID != 7 {
		t.Fatalf("member_id 未下传到导出筛选: %+v", repo.lastFilter.MemberID)
	}
}
