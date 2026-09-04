package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"entgo.io/ent/dialect/sql/schema"
	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"

	"github.com/DouDOU-start/airgate-core/ent/enttest"
	entmember "github.com/DouDOU-start/airgate-core/ent/member"
	corauth "github.com/DouDOU-start/airgate-core/internal/auth"
)

// 成员账号正常登录：中间件按 members.account 解析团队归属，写入成员 id / 企业主 id / 分组白名单；
// 成员被停用后已登录会话立即失效；普通用户与企业主本人不受影响。
func TestJWTUserAuthResolvesTeamMembership(t *testing.T) {
	db := enttest.Open(t, "sqlite3", "file:jwt_user_auth_team?mode=memory&cache=shared&_fk=1", enttest.WithMigrateOptions(schema.WithGlobalUniqueID(false)))
	t.Cleanup(func() { _ = db.Close() })
	ctx := t.Context()
	owner := db.User.Create().SetEmail("owner@example.com").SetPasswordHash("hash").SetIsEnterpriseOwner(true).SaveX(ctx)
	account := db.User.Create().SetEmail("member@example.com").SetPasswordHash("hash").SaveX(ctx)
	member := db.Member.Create().SetName("成员").SetOwner(owner).SetAccount(account).SetAllowedGroupIds([]int64{3, 5}).SaveX(ctx)
	jwtMgr := corauth.NewJWTManager("secret", 24)

	router := gin.New()
	router.Use(JWTUserAuth(jwtMgr, db))
	router.GET("/me", func(c *gin.Context) {
		userID, _ := c.Get(CtxKeyUserID)
		memberID, _ := c.Get(CtxKeyMemberID)
		ownerID, _ := c.Get(CtxKeyTeamOwnerID)
		billing, _ := BillingUserID(c)
		c.JSON(http.StatusOK, gin.H{
			"user_id": userID, "member_id": memberID, "owner_id": ownerID,
			"billing_user_id": billing, "allowed": MemberAllowedGroupIDs(c),
		})
	})
	call := func(userID int, email string) (*httptest.ResponseRecorder, map[string]any) {
		t.Helper()
		token, err := jwtMgr.GenerateToken(userID, "user", email)
		if err != nil {
			t.Fatalf("GenerateToken: %v", err)
		}
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/me", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		router.ServeHTTP(rec, req)
		var body map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		return rec, body
	}

	// 成员账号：user_id 仍是本人（资料/密钥/工作台资产按本人），付费主体是企业主
	rec, body := call(account.ID, account.Email)
	if rec.Code != http.StatusOK {
		t.Fatalf("member status=%d body=%s", rec.Code, rec.Body.String())
	}
	if body["user_id"] != float64(account.ID) || body["member_id"] != float64(member.ID) || body["owner_id"] != float64(owner.ID) || body["billing_user_id"] != float64(owner.ID) {
		t.Fatalf("member context = %s", rec.Body.String())
	}
	if allowed, _ := body["allowed"].([]any); len(allowed) != 2 {
		t.Fatalf("allowed groups = %v, want [3 5]", body["allowed"])
	}

	// 企业主本人：不是成员，付费主体是自己
	rec, body = call(owner.ID, owner.Email)
	if rec.Code != http.StatusOK || body["member_id"] != nil || body["billing_user_id"] != float64(owner.ID) {
		t.Fatalf("owner context = %s", rec.Body.String())
	}

	// 停用成员：会话失效（缓存先失效，模拟 5s 后）
	if err := db.Member.UpdateOneID(member.ID).SetStatus(entmember.StatusDisabled).Exec(ctx); err != nil {
		t.Fatalf("disable member: %v", err)
	}
	corauth.InvalidateTeamIdentity(account.ID)
	rec, _ = call(account.ID, account.Email)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("disabled member status=%d body=%s, want 401", rec.Code, rec.Body.String())
	}
}
