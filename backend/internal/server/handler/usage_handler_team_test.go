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

// 部门负责人登录：可见范围放宽到「本人 ∪ 所负责部门全员」，而不再钉死在自己一个人。
// 这是负责人这个角色的全部意义——之前他只能收额度预警邮件，查不到组员任何用量。
func TestUserUsageTrendDepartmentManagerWidensToManagedDepartments(t *testing.T) {
	repo := &stubUsageRepo{}
	handler := NewUsageHandler(appusage.NewService(repo))

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest("GET", "/usage/trend?granularity=day", nil)
	c.Set("user_id", 82)
	c.Set(middleware.CtxKeyMemberID, 7)
	c.Set(middleware.CtxKeyTeamOwnerID, 50)
	c.Set(middleware.CtxKeyManagedDepartmentIDs, []int{3, 9})

	handler.UserUsageTrend(c)

	if recorder.Code != 200 {
		t.Fatalf("状态码 = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	f := repo.lastTrendFilter
	if f.UserID == nil || *f.UserID != 50 {
		t.Fatalf("仍应按企业主 50 查，实际 = %+v", f.UserID)
	}
	if f.Manager == nil {
		t.Fatal("负责人会话应带上 ManagerScope")
	}
	if f.Manager.MemberID != 7 {
		t.Errorf("ManagerScope.MemberID = %d, want 7（本人始终可见）", f.Manager.MemberID)
	}
	if len(f.Manager.DepartmentIDs) != 2 || f.Manager.DepartmentIDs[0] != 3 || f.Manager.DepartmentIDs[1] != 9 {
		t.Errorf("ManagerScope.DepartmentIDs = %v, want [3 9]", f.Manager.DepartmentIDs)
	}
	// 没有显式下钻时不该再钉死成员，否则又退回「只看自己」
	if f.MemberID != nil {
		t.Errorf("未指定下钻时 MemberID 应为空，实际 = %d", *f.MemberID)
	}
}

// 负责人按成员下钻：请求里的 member_id 原样传下去，与 ManagerScope 一起作为 AND 条件。
// 越权筛选（组外成员）靠 ManagerScope 这层外约束自然落空，不需要额外校验。
func TestUserUsageTrendDepartmentManagerKeepsMemberDrilldown(t *testing.T) {
	repo := &stubUsageRepo{}
	handler := NewUsageHandler(appusage.NewService(repo))

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest("GET", "/usage/trend?granularity=day&member_id=12", nil)
	c.Set("user_id", 82)
	c.Set(middleware.CtxKeyMemberID, 7)
	c.Set(middleware.CtxKeyTeamOwnerID, 50)
	c.Set(middleware.CtxKeyManagedDepartmentIDs, []int{3})

	handler.UserUsageTrend(c)

	f := repo.lastTrendFilter
	if f.Manager == nil {
		t.Fatal("下钻时仍须保留 ManagerScope 外约束")
	}
	if f.MemberID == nil || *f.MemberID != 12 {
		t.Fatalf("应按请求下钻到成员 12，实际 = %+v", f.MemberID)
	}
}

// 密钥会话即使属于负责人也不放开：key 登录是客户视角，
// 放开等于让一把 key 看到同部门其他人的消耗。
func TestUserUsageTrendManagerScopeNotAppliedToKeySession(t *testing.T) {
	repo := &stubUsageRepo{}
	handler := NewUsageHandler(appusage.NewService(repo))

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest("GET", "/usage/trend?granularity=day", nil)
	c.Set("user_id", 82)
	c.Set(middleware.CtxKeyMemberID, 7)
	c.Set(middleware.CtxKeyTeamOwnerID, 50)
	c.Set(middleware.CtxKeyAPIKeyID, 4321)
	c.Set(middleware.CtxKeyManagedDepartmentIDs, []int{3})

	handler.UserUsageTrend(c)

	f := repo.lastTrendFilter
	if f.Manager != nil {
		t.Fatal("密钥会话不该带 ManagerScope")
	}
	if f.MemberID == nil || *f.MemberID != 7 {
		t.Fatalf("密钥会话仍应收敛到本成员 7，实际 = %+v", f.MemberID)
	}
	if !f.ScopedToKey {
		t.Error("密钥会话应保持客户视角")
	}
}

// 普通成员（不负责任何部门）行为不变：仍然只看自己。
func TestUserUsageTrendPlainMemberUnchanged(t *testing.T) {
	repo := &stubUsageRepo{}
	handler := NewUsageHandler(appusage.NewService(repo))

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest("GET", "/usage/trend?granularity=day", nil)
	c.Set("user_id", 82)
	c.Set(middleware.CtxKeyMemberID, 7)
	c.Set(middleware.CtxKeyTeamOwnerID, 50)

	handler.UserUsageTrend(c)

	f := repo.lastTrendFilter
	if f.Manager != nil {
		t.Fatal("非负责人不该带 ManagerScope")
	}
	if f.MemberID == nil || *f.MemberID != 7 {
		t.Fatalf("应仍只看自己，实际 = %+v", f.MemberID)
	}
}
