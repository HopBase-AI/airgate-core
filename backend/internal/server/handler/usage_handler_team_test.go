package handler

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	appusage "github.com/DouDOU-start/airgate-core/internal/app/usage"
	"github.com/DouDOU-start/airgate-core/internal/server/middleware"
)

// 成员账号本人登录（非密钥会话）：用量按企业主（usage_logs.user=owner）查、按成员收敛，
// 且不打开 scoped——成员是正常账号，保留完整的用户视角费用拆分。
func TestUserUsageTrendMemberAccountSessionQueriesOwnerScopedToMember(t *testing.T) {
	repo := &stubUsageRepo{}
	handler := NewUsageHandler(appusage.NewService(repo))

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest("GET", "/usage/trend?granularity=day&member_id=999", nil)
	c.Set("user_id", 82)
	c.Set(middleware.CtxKeyMemberID, 7)
	c.Set(middleware.CtxKeyTeamOwnerID, 50)

	handler.UserUsageTrend(c)

	if recorder.Code != 200 {
		t.Fatalf("状态码 = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	f := repo.lastTrendFilter
	if f.UserID == nil || *f.UserID != 50 {
		t.Fatalf("成员账号的用量应按企业主 50 查，实际 = %+v", f.UserID)
	}
	if f.MemberID == nil || *f.MemberID != 7 {
		t.Fatalf("应收敛到本成员 7，实际 = %+v", f.MemberID)
	}
	if f.ScopedToKey {
		t.Fatalf("成员账号会话不应打开 scoped（那是密钥会话的客户视角）")
	}
}
