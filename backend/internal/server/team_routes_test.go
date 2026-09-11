package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"entgo.io/ent/dialect/sql/schema"
	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"

	"github.com/DouDOU-start/airgate-core/ent"
	"github.com/DouDOU-start/airgate-core/ent/enttest"
	appaudit "github.com/DouDOU-start/airgate-core/internal/app/audit"
	appdepartment "github.com/DouDOU-start/airgate-core/internal/app/department"
	appmember "github.com/DouDOU-start/airgate-core/internal/app/member"
	corauth "github.com/DouDOU-start/airgate-core/internal/auth"
	"github.com/DouDOU-start/airgate-core/internal/infra/store"
	"github.com/DouDOU-start/airgate-core/internal/server/handler"
	"github.com/DouDOU-start/airgate-core/internal/server/middleware"
)

// 端到端跑真实路由表 + 真实 store：证明"部门负责人"的边界落在端点上，而不只是 service 里。
//
// 内存 SQLite 单连接（cache=shared 下多连接会偶发 SQLITE_LOCKED）。
type teamRoutesEnv struct {
	engine     *gin.Engine
	db         *ent.Client
	ownerID    int
	managerUID int // 负责人的登录账号 user id
	staffUID   int // 普通成员账号
	deptA      int // 负责人管的部门
	deptB      int
	managerMID int // 负责人自己的成员 id
	peerMID    int // 本部门同事
	foreignMID int // 他部门成员
	auditEntry func(t *testing.T) []*ent.TeamAuditLog
}

func newTeamRoutesEnv(t *testing.T) teamRoutesEnv {
	t.Helper()
	drv, err := entsql.Open(dialect.SQLite, "file:server_team_routes_"+t.Name()+"?mode=memory&cache=shared&_fk=1")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	drv.DB().SetMaxOpenConns(1)
	db := enttest.NewClient(t, enttest.WithOptions(ent.Driver(drv)),
		enttest.WithMigrateOptions(schema.WithGlobalUniqueID(false)))
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	})
	ctx := t.Context()

	owner := mustUser(t, db, "owner@example.com", true)
	managerUser := mustUser(t, db, "lead@example.com", false)
	staffUser := mustUser(t, db, "staff@example.com", false)

	deptA, err := db.Department.Create().SetOwnerID(owner.ID).SetName("研发部").SetQuotaUsd(600).Save(ctx)
	if err != nil {
		t.Fatalf("create dept A: %v", err)
	}
	deptB, err := db.Department.Create().SetOwnerID(owner.ID).SetName("销售部").SetQuotaUsd(300).Save(ctx)
	if err != nil {
		t.Fatalf("create dept B: %v", err)
	}
	manager, err := db.Member.Create().SetOwnerID(owner.ID).SetName("负责人").SetQuotaUsd(50).
		SetAccountID(managerUser.ID).SetDepartmentID(deptA.ID).Save(ctx)
	if err != nil {
		t.Fatalf("create manager member: %v", err)
	}
	peer, err := db.Member.Create().SetOwnerID(owner.ID).SetName("本部门同事").SetQuotaUsd(20).
		SetAccountID(staffUser.ID).SetDepartmentID(deptA.ID).Save(ctx)
	if err != nil {
		t.Fatalf("create peer member: %v", err)
	}
	foreign, err := db.Member.Create().SetOwnerID(owner.ID).SetName("他部门同事").SetQuotaUsd(20).
		SetDepartmentID(deptB.ID).Save(ctx)
	if err != nil {
		t.Fatalf("create foreign member: %v", err)
	}
	if err := db.Department.UpdateOneID(deptA.ID).SetManagerID(manager.ID).Exec(ctx); err != nil {
		t.Fatalf("set manager: %v", err)
	}

	auditService := appaudit.NewService(store.NewTeamAuditStore(db))
	memberHandler := handler.NewMemberHandler(appmember.NewService(store.NewMemberStore(db), auditService))
	departmentHandler := handler.NewDepartmentHandler(
		appdepartment.NewService(store.NewDepartmentStore(db), auditService), auditService)

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	v1 := engine.Group("/api/v1")
	// 会话由测试头注入，其余与生产一致（真实门禁 + 真实 handler + 真实 store）。
	v1.Use(func(c *gin.Context) {
		corauth.InvalidateAllTeamIdentities()
		if id, err := strconv.Atoi(c.GetHeader("X-Test-User")); err == nil {
			c.Set(middleware.CtxKeyUserID, id)
		}
		role := c.GetHeader("X-Test-Role")
		if role == "" {
			role = "user"
		}
		c.Set(middleware.CtxKeyRole, role)
		c.Set(middleware.CtxKeyEmail, c.GetHeader("X-Test-Email"))
		c.Next()
	})
	registerTeamRoutes(v1, db, memberHandler, departmentHandler)

	return teamRoutesEnv{
		engine: engine, db: db, ownerID: owner.ID, managerUID: managerUser.ID, staffUID: staffUser.ID,
		deptA: deptA.ID, deptB: deptB.ID, managerMID: manager.ID, peerMID: peer.ID, foreignMID: foreign.ID,
		auditEntry: func(t *testing.T) []*ent.TeamAuditLog {
			logs, err := db.TeamAuditLog.Query().All(t.Context())
			if err != nil {
				t.Fatalf("query audit: %v", err)
			}
			return logs
		},
	}
}

func mustUser(t *testing.T, db *ent.Client, email string, enterprise bool) *ent.User {
	t.Helper()
	u, err := db.User.Create().SetEmail(email).SetPasswordHash("h").SetIsEnterpriseOwner(enterprise).Save(t.Context())
	if err != nil {
		t.Fatalf("create user %s: %v", email, err)
	}
	return u
}

func (e teamRoutesEnv) do(t *testing.T, userID int, role, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf *bytes.Buffer
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		buf = bytes.NewBuffer(raw)
	} else {
		buf = bytes.NewBuffer(nil)
	}
	req := httptest.NewRequest(method, path, buf)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Test-User", strconv.Itoa(userID))
	req.Header.Set("X-Test-Role", role)
	w := httptest.NewRecorder()
	e.engine.ServeHTTP(w, req)
	return w
}

// pageQuery 分页参数：dto.PageReq 的 page / page_size 是必填（binding min=1）。
const pageQuery = "?page=1&page_size=50"

// 端点级权限矩阵：负责人 / 企业主 各跑一遍同一批请求。
func TestTeamRoutesPermissionMatrix(t *testing.T) {
	env := newTeamRoutesEnv(t)

	cases := []struct {
		name        string
		method      string
		path        func() string
		body        any
		wantManager int
		wantOwner   int
	}{
		{"列成员", http.MethodGet, func() string { return "/api/v1/members" + pageQuery }, nil, http.StatusOK, http.StatusOK},
		{"列部门", http.MethodGet, func() string { return "/api/v1/departments" + pageQuery }, nil, http.StatusOK, http.StatusOK},
		{"企业总览", http.MethodGet, func() string { return "/api/v1/team/overview" }, nil, http.StatusOK, http.StatusOK},
		{"改本部门成员额度", http.MethodPut, func() string { return "/api/v1/members/" + strconv.Itoa(env.peerMID) },
			map[string]any{"quota_usd": 30}, http.StatusOK, http.StatusOK},
		{"重置本部门成员本期", http.MethodPost, func() string { return "/api/v1/members/" + strconv.Itoa(env.peerMID) + "/reset-period" },
			nil, http.StatusOK, http.StatusOK},
		{"改他部门成员额度", http.MethodPut, func() string { return "/api/v1/members/" + strconv.Itoa(env.foreignMID) },
			map[string]any{"quota_usd": 999}, http.StatusNotFound, http.StatusOK},
		{"重置他部门成员本期", http.MethodPost, func() string { return "/api/v1/members/" + strconv.Itoa(env.foreignMID) + "/reset-period" },
			nil, http.StatusNotFound, http.StatusOK},
		{"改自己那条成员记录", http.MethodPut, func() string { return "/api/v1/members/" + strconv.Itoa(env.managerMID) },
			map[string]any{"quota_usd": 9999}, http.StatusForbidden, http.StatusOK},
		{"把成员调去他部门", http.MethodPut, func() string { return "/api/v1/members/" + strconv.Itoa(env.peerMID) },
			map[string]any{"department_id": env.deptB}, http.StatusForbidden, http.StatusOK},
		{"建部门", http.MethodPost, func() string { return "/api/v1/departments" },
			map[string]any{"name": "新部门 " + strconv.Itoa(env.deptB)}, http.StatusForbidden, http.StatusOK},
		{"改部门额度天花板", http.MethodPut, func() string { return "/api/v1/departments/" + strconv.Itoa(env.deptA) },
			map[string]any{"quota_usd": 99999}, http.StatusForbidden, http.StatusOK},
		{"换部门负责人", http.MethodPut, func() string { return "/api/v1/departments/" + strconv.Itoa(env.deptA) },
			map[string]any{"manager_member_id": env.managerMID}, http.StatusForbidden, http.StatusOK},
		{"重置部门本期", http.MethodPost, func() string { return "/api/v1/departments/" + strconv.Itoa(env.deptA) + "/reset-period" },
			nil, http.StatusForbidden, http.StatusOK},
		{"改企业账期", http.MethodPut, func() string { return "/api/v1/team/billing-period" },
			map[string]any{"billing_day": 15}, http.StatusForbidden, http.StatusOK},
		{"查操作审计", http.MethodGet, func() string { return "/api/v1/team/audit-logs" + pageQuery }, nil, http.StatusForbidden, http.StatusOK},
	}

	for _, tc := range cases {
		t.Run(tc.name+"/部门负责人", func(t *testing.T) {
			w := env.do(t, env.managerUID, "user", tc.method, tc.path(), tc.body)
			if w.Code != tc.wantManager {
				t.Fatalf("状态码 = %d, want %d: %s", w.Code, tc.wantManager, w.Body.String())
			}
		})
		t.Run(tc.name+"/企业主", func(t *testing.T) {
			w := env.do(t, env.ownerID, "user", tc.method, tc.path(), tc.body)
			if w.Code != tc.wantOwner {
				t.Fatalf("状态码 = %d, want %d: %s", w.Code, tc.wantOwner, w.Body.String())
			}
		})
	}
}

// 删成员：单独一组（会真的删掉数据，不能混进矩阵里重复跑）。
func TestTeamRoutesDeleteMemberScope(t *testing.T) {
	env := newTeamRoutesEnv(t)

	if w := env.do(t, env.managerUID, "user", http.MethodDelete, "/api/v1/members/"+strconv.Itoa(env.foreignMID), nil); w.Code != http.StatusNotFound {
		t.Fatalf("删他部门成员状态码 = %d, want 404: %s", w.Code, w.Body.String())
	}
	if w := env.do(t, env.managerUID, "user", http.MethodDelete, "/api/v1/members/"+strconv.Itoa(env.managerMID), nil); w.Code != http.StatusForbidden {
		t.Fatalf("删自己状态码 = %d, want 403: %s", w.Code, w.Body.String())
	}
	if exists, _ := env.db.Member.Query().Where().IDs(t.Context()); len(exists) != 3 {
		t.Fatalf("被拒的删除不应动数据，剩余成员 = %d", len(exists))
	}
	if w := env.do(t, env.managerUID, "user", http.MethodDelete, "/api/v1/members/"+strconv.Itoa(env.peerMID), nil); w.Code != http.StatusOK {
		t.Fatalf("删本部门成员状态码 = %d, want 200: %s", w.Code, w.Body.String())
	}
	if _, err := env.db.Member.Get(t.Context(), env.peerMID); !ent.IsNotFound(err) {
		t.Fatalf("本部门成员应已删除: %v", err)
	}
}

// 建成员：负责人建的人自动落在本部门；显式指定别的部门 403。
func TestTeamRoutesCreateMemberScope(t *testing.T) {
	env := newTeamRoutesEnv(t)

	w := env.do(t, env.managerUID, "user", http.MethodPost, "/api/v1/members",
		map[string]any{"name": "新人", "quota_usd": 10, "department_id": env.deptB})
	if w.Code != http.StatusForbidden {
		t.Fatalf("跨部门建成员状态码 = %d, want 403: %s", w.Code, w.Body.String())
	}

	w = env.do(t, env.managerUID, "user", http.MethodPost, "/api/v1/members",
		map[string]any{"name": "新人", "quota_usd": 10})
	if w.Code != http.StatusOK {
		t.Fatalf("本部门建成员状态码 = %d, want 200: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Data struct {
			ID           int64 `json:"id"`
			DepartmentID int64 `json:"department_id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析响应: %v (%s)", err, w.Body.String())
	}
	if int(resp.Data.DepartmentID) != env.deptA {
		t.Fatalf("新成员部门 = %d, want %d", resp.Data.DepartmentID, env.deptA)
	}
}

// 列表与总览：负责人只看得到本部门的人和本部门口径的总览（不含企业余额）。
func TestTeamRoutesReadsScopedToDepartment(t *testing.T) {
	env := newTeamRoutesEnv(t)
	if err := env.db.User.UpdateOneID(env.ownerID).SetBalance(1234).Exec(t.Context()); err != nil {
		t.Fatalf("set balance: %v", err)
	}

	// 成员列表：即便显式要求看别的部门，也只会拿到本部门的人。
	w := env.do(t, env.managerUID, "user", http.MethodGet, "/api/v1/members"+pageQuery+"&department_id="+strconv.Itoa(env.deptB), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("列成员状态码 = %d: %s", w.Code, w.Body.String())
	}
	var list struct {
		Data struct {
			List []struct {
				ID           int64 `json:"id"`
				DepartmentID int64 `json:"department_id"`
			} `json:"list"`
			Total int64 `json:"total"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatalf("解析列表: %v (%s)", err, w.Body.String())
	}
	if list.Data.Total != 2 {
		t.Fatalf("负责人应只看到本部门 2 人，实际 %d: %s", list.Data.Total, w.Body.String())
	}
	for _, item := range list.Data.List {
		if int(item.DepartmentID) != env.deptA {
			t.Fatalf("列表里混进了他部门成员: %+v", item)
		}
	}

	// 部门列表：只有自己那一个。
	w = env.do(t, env.managerUID, "user", http.MethodGet, "/api/v1/departments"+pageQuery, nil)
	var depts struct {
		Data struct {
			List []struct {
				ID int64 `json:"id"`
			} `json:"list"`
			Total int64 `json:"total"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &depts); err != nil {
		t.Fatalf("解析部门列表: %v (%s)", err, w.Body.String())
	}
	if depts.Data.Total != 1 || len(depts.Data.List) != 1 || int(depts.Data.List[0].ID) != env.deptA {
		t.Fatalf("负责人部门列表 = %s", w.Body.String())
	}

	// 总览：不含企业余额。
	w = env.do(t, env.managerUID, "user", http.MethodGet, "/api/v1/team/overview", nil)
	var overview struct {
		Data struct {
			Balance         float64 `json:"balance"`
			DepartmentCount int     `json:"department_count"`
			MemberCount     int     `json:"member_count"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &overview); err != nil {
		t.Fatalf("解析总览: %v (%s)", err, w.Body.String())
	}
	if overview.Data.Balance != 0 {
		t.Fatalf("企业余额不该出现在负责人总览里: %s", w.Body.String())
	}
	if overview.Data.DepartmentCount != 1 || overview.Data.MemberCount != 2 {
		t.Fatalf("负责人总览应是部门口径: %s", w.Body.String())
	}

	// 企业主总览照旧。
	w = env.do(t, env.ownerID, "user", http.MethodGet, "/api/v1/team/overview", nil)
	if err := json.Unmarshal(w.Body.Bytes(), &overview); err != nil {
		t.Fatalf("解析企业主总览: %v", err)
	}
	if overview.Data.Balance != 1234 || overview.Data.DepartmentCount != 2 || overview.Data.MemberCount != 3 {
		t.Fatalf("企业主总览被改坏了: %s", w.Body.String())
	}
}

// 审计：负责人的改动记在企业主名下、actor 是负责人自己。
func TestTeamRoutesAuditAttribution(t *testing.T) {
	env := newTeamRoutesEnv(t)

	w := env.do(t, env.managerUID, "user", http.MethodPut, "/api/v1/members/"+strconv.Itoa(env.peerMID),
		map[string]any{"quota_usd": 30})
	if w.Code != http.StatusOK {
		t.Fatalf("改额度状态码 = %d: %s", w.Code, w.Body.String())
	}
	logs := env.auditEntry(t)
	if len(logs) != 1 {
		t.Fatalf("审计条数 = %d, want 1", len(logs))
	}
	entry := logs[0]
	if entry.OwnerID != env.ownerID {
		t.Fatalf("审计 owner_id = %d, want 部门所属企业主 %d", entry.OwnerID, env.ownerID)
	}
	if entry.ActorUserID != env.managerUID {
		t.Fatalf("审计 actor_user_id = %d, want 负责人 %d", entry.ActorUserID, env.managerUID)
	}
	if entry.Action != appaudit.ActionMemberUpdate || entry.TargetID != env.peerMID {
		t.Fatalf("审计内容 = %+v", entry)
	}
}
