package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"entgo.io/ent/dialect/sql/schema"
	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"

	"github.com/DouDOU-start/airgate-core/ent"
	"github.com/DouDOU-start/airgate-core/ent/enttest"
	entuser "github.com/DouDOU-start/airgate-core/ent/user"
	"github.com/DouDOU-start/airgate-core/internal/app/teamscope"
	corauth "github.com/DouDOU-start/airgate-core/internal/auth"
)

// enttestOpenScope 内存 SQLite 必须单连接（cache=shared 下多连接会偶发 SQLITE_LOCKED），
// 模板见 internal/scheduler/events_test.go。
func enttestOpenScope(t *testing.T, name string) *ent.Client {
	t.Helper()
	drv, err := entsql.Open(dialect.SQLite, "file:"+name+"?mode=memory&cache=shared&_fk=1")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	drv.DB().SetMaxOpenConns(1)
	client := enttest.NewClient(t, enttest.WithOptions(ent.Driver(drv)),
		enttest.WithMigrateOptions(schema.WithGlobalUniqueID(false)))
	t.Cleanup(func() {
		if err := client.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	})
	return client
}

// teamScopeFixture 一个企业主 + 两个部门 + 四类会话身份。
type teamScopeFixture struct {
	db            *ent.Client
	owner         *ent.User
	plain         *ent.User
	managerUser   *ent.User // 部门 A 的负责人（成员账号）
	plainMember   *ent.User // 普通成员账号（不负责任何部门）
	multiUser     *ent.User // 同时挂在两个部门负责人位上的脏数据
	disabledUser  *ent.User // 被停用的负责人
	deptA, deptB  *ent.Department
	managerMember *ent.Member
}

func newTeamScopeFixture(t *testing.T, name string) teamScopeFixture {
	t.Helper()
	db := enttestOpenScope(t, name)
	ctx := t.Context()

	mkUser := func(email string, enterprise bool) *ent.User {
		u, err := db.User.Create().SetEmail(email).SetPasswordHash("h").SetIsEnterpriseOwner(enterprise).Save(ctx)
		if err != nil {
			t.Fatalf("create user %s: %v", email, err)
		}
		return u
	}
	owner := mkUser("owner@example.com", true)
	plain := mkUser("plain@example.com", false)
	managerUser := mkUser("lead@example.com", false)
	plainMemberUser := mkUser("staff@example.com", false)
	multiUser := mkUser("multi@example.com", false)
	disabledUser := mkUser("disabled@example.com", false)

	mkDept := func(o *ent.User, name string) *ent.Department {
		d, err := db.Department.Create().SetOwnerID(o.ID).SetName(name).Save(ctx)
		if err != nil {
			t.Fatalf("create department %s: %v", name, err)
		}
		return d
	}
	deptA := mkDept(owner, "研发部")
	deptB := mkDept(owner, "销售部")

	mkMember := func(o *ent.User, name string, account *ent.User, dept *ent.Department, status string) *ent.Member {
		b := db.Member.Create().SetOwnerID(o.ID).SetName(name)
		if account != nil {
			b = b.SetAccountID(account.ID)
		}
		if dept != nil {
			b = b.SetDepartmentID(dept.ID)
		}
		if status == "disabled" {
			b = b.SetStatus("disabled")
		}
		m, err := b.Save(ctx)
		if err != nil {
			t.Fatalf("create member %s: %v", name, err)
		}
		return m
	}
	managerMember := mkMember(owner, "负责人", managerUser, deptA, "active")
	mkMember(owner, "普通成员", plainMemberUser, deptA, "active")
	multiMember := mkMember(owner, "双岗负责人", multiUser, deptA, "active")
	disabledMember := mkMember(owner, "停用负责人", disabledUser, deptB, "disabled")

	if err := db.Department.UpdateOneID(deptA.ID).SetManagerID(managerMember.ID).Exec(ctx); err != nil {
		t.Fatalf("set manager: %v", err)
	}
	if err := db.Department.UpdateOneID(deptB.ID).SetManagerID(disabledMember.ID).Exec(ctx); err != nil {
		t.Fatalf("set disabled manager: %v", err)
	}
	// 脏数据：同一成员挂在两个部门的负责人位上（DB 未对 department_manager 加唯一约束）。
	dirtyA := mkDept(owner, "脏数据部门 A")
	dirtyB := mkDept(owner, "脏数据部门 B")
	for _, d := range []*ent.Department{dirtyA, dirtyB} {
		if err := db.Department.UpdateOneID(d.ID).SetManagerID(multiMember.ID).Exec(ctx); err != nil {
			t.Fatalf("set dirty manager: %v", err)
		}
	}

	return teamScopeFixture{
		db: db, owner: owner, plain: plain, managerUser: managerUser,
		plainMember: plainMemberUser, multiUser: multiUser, disabledUser: disabledUser,
		deptA: deptA, deptB: deptB, managerMember: managerMember,
	}
}

// runScopeGuard 跑一遍门禁，返回状态码与放行时写入的范围。
func runScopeGuard(t *testing.T, guard gin.HandlerFunc, setup func(*gin.Context)) (int, teamscope.Scope, bool) {
	t.Helper()
	corauth.InvalidateAllTeamIdentities()
	gin.SetMode(gin.TestMode)
	var got teamscope.Scope
	var passed bool
	router := gin.New()
	router.Use(func(c *gin.Context) { setup(c) })
	router.Use(guard)
	router.GET("/api/v1/members", func(c *gin.Context) {
		got, passed = TeamScopeFrom(c)
		c.String(http.StatusOK, "ok")
	})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/members", nil))
	return w.Code, got, passed
}

// RequireTeamScope：管理员 / 企业主拿全企业范围，部门负责人拿单部门范围，其余一律 403。
func TestRequireTeamScopeResolution(t *testing.T) {
	f := newTeamScopeFixture(t, "mw_team_scope")

	cases := []struct {
		name     string
		setup    func(*gin.Context)
		wantCode int
		// wantOwner / wantDept：放行时期望的范围（wantDept 0 = 全企业）
		wantOwner  int
		wantDept   int
		wantMember int
	}{
		{"管理员=全企业", func(c *gin.Context) {
			c.Set(CtxKeyRole, "admin")
			c.Set(CtxKeyUserID, 0)
		}, http.StatusOK, 0, 0, 0},
		{"企业主=全企业", func(c *gin.Context) {
			c.Set(CtxKeyRole, "user")
			c.Set(CtxKeyUserID, f.owner.ID)
		}, http.StatusOK, f.owner.ID, 0, 0},
		{"部门负责人=本部门", func(c *gin.Context) {
			c.Set(CtxKeyRole, "user")
			c.Set(CtxKeyUserID, f.managerUser.ID)
		}, http.StatusOK, f.owner.ID, f.deptA.ID, f.managerMember.ID},
		{"普通成员无部门负责人身份", func(c *gin.Context) {
			c.Set(CtxKeyRole, "user")
			c.Set(CtxKeyUserID, f.plainMember.ID)
		}, http.StatusForbidden, 0, 0, 0},
		{"普通用户", func(c *gin.Context) {
			c.Set(CtxKeyRole, "user")
			c.Set(CtxKeyUserID, f.plain.ID)
		}, http.StatusForbidden, 0, 0, 0},
		{"停用的负责人", func(c *gin.Context) {
			c.Set(CtxKeyRole, "user")
			c.Set(CtxKeyUserID, f.disabledUser.ID)
		}, http.StatusForbidden, 0, 0, 0},
		{"负责多个部门=范围不唯一拒绝", func(c *gin.Context) {
			c.Set(CtxKeyRole, "user")
			c.Set(CtxKeyUserID, f.multiUser.ID)
		}, http.StatusForbidden, 0, 0, 0},
		{"缺少用户上下文", func(c *gin.Context) { c.Set(CtxKeyRole, "user") }, http.StatusForbidden, 0, 0, 0},
		{"用户不存在", func(c *gin.Context) {
			c.Set(CtxKeyRole, "user")
			c.Set(CtxKeyUserID, 999999)
		}, http.StatusForbidden, 0, 0, 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, scope, passed := runScopeGuard(t, RequireTeamScope(f.db), tc.setup)
			if code != tc.wantCode {
				t.Fatalf("状态码 = %d, want %d", code, tc.wantCode)
			}
			if tc.wantCode != http.StatusOK {
				return
			}
			if !passed {
				t.Fatal("放行时必须写入范围")
			}
			if scope.OwnerID != tc.wantOwner {
				t.Fatalf("scope.OwnerID = %d, want %d", scope.OwnerID, tc.wantOwner)
			}
			if tc.wantDept == 0 {
				if scope.IsDepartmentManager() {
					t.Fatalf("企业主/管理员不该被限定到部门: %+v", scope)
				}
				return
			}
			if scope.ScopedDepartmentID() != tc.wantDept {
				t.Fatalf("scope 部门 = %d, want %d", scope.ScopedDepartmentID(), tc.wantDept)
			}
			if scope.ActorMemberID != tc.wantMember {
				t.Fatalf("scope.ActorMemberID = %d, want %d", scope.ActorMemberID, tc.wantMember)
			}
		})
	}
}

// 范围里的 OwnerID 必须是**部门所属企业主**，不是负责人自己的 user id：
// service 全部按 ownerID 限定归属，取错会静默读写另一个租户。
func TestDepartmentManagerScopeUsesDepartmentOwner(t *testing.T) {
	f := newTeamScopeFixture(t, "mw_team_scope_owner")
	code, scope, _ := runScopeGuard(t, RequireTeamScope(f.db), func(c *gin.Context) {
		c.Set(CtxKeyRole, "user")
		c.Set(CtxKeyUserID, f.managerUser.ID)
	})
	if code != http.StatusOK {
		t.Fatalf("状态码 = %d", code)
	}
	if scope.OwnerID == f.managerUser.ID {
		t.Fatalf("OwnerID 取成了负责人自己的 user id(%d)——会读到错误的租户", scope.OwnerID)
	}
	if scope.OwnerID != f.owner.ID {
		t.Fatalf("OwnerID = %d, want 部门所属企业主 %d", scope.OwnerID, f.owner.ID)
	}
}

// 企业主被停用 → 负责人的管理能力一并失效。
func TestDepartmentManagerScopeDeniedWhenOwnerDisabled(t *testing.T) {
	f := newTeamScopeFixture(t, "mw_team_scope_owner_disabled")
	if err := f.db.User.UpdateOneID(f.owner.ID).SetStatus(entuser.StatusDisabled).Exec(t.Context()); err != nil {
		t.Fatalf("disable owner: %v", err)
	}
	code, _, _ := runScopeGuard(t, RequireTeamScope(f.db), func(c *gin.Context) {
		c.Set(CtxKeyRole, "user")
		c.Set(CtxKeyUserID, f.managerUser.ID)
	})
	if code != http.StatusForbidden {
		t.Fatalf("状态码 = %d, want 403", code)
	}
}

// RequireEnterpriseOwner 保持原口径：部门负责人进不去（组织结构写操作 / 账期 / 审计都在这道门后）。
func TestRequireEnterpriseOwnerStillRejectsDepartmentManager(t *testing.T) {
	f := newTeamScopeFixture(t, "mw_team_scope_owner_only")

	cases := []struct {
		name     string
		userID   int
		role     string
		wantCode int
	}{
		{"管理员放行", 0, "admin", http.StatusOK},
		{"企业主放行", f.owner.ID, "user", http.StatusOK},
		{"部门负责人拒绝", f.managerUser.ID, "user", http.StatusForbidden},
		{"普通成员拒绝", f.plainMember.ID, "user", http.StatusForbidden},
		{"普通用户拒绝", f.plain.ID, "user", http.StatusForbidden},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, scope, passed := runScopeGuard(t, RequireEnterpriseOwner(f.db), func(c *gin.Context) {
				c.Set(CtxKeyRole, tc.role)
				c.Set(CtxKeyUserID, tc.userID)
			})
			if code != tc.wantCode {
				t.Fatalf("状态码 = %d, want %d", code, tc.wantCode)
			}
			if tc.wantCode == http.StatusOK {
				if !passed || scope.IsDepartmentManager() {
					t.Fatalf("企业主门禁必须写入全企业范围: %+v", scope)
				}
				if scope.OwnerID != tc.userID {
					t.Fatalf("scope.OwnerID = %d, want %d", scope.OwnerID, tc.userID)
				}
			}
		})
	}
}
