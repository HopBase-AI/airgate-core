package member

import (
	"context"
	"errors"
	"testing"

	"github.com/DouDOU-start/airgate-core/internal/app/audit"
	"github.com/DouDOU-start/airgate-core/internal/app/teamscope"
)

// 部门负责人范围的测试基线（对齐生产：企业主 7、部门 4、负责人成员 15）。
const (
	scopeOwnerID     = 7
	managedDeptID    = 4
	otherDeptID      = 9
	managerMemberID  = 15
	peerMemberID     = 16
	foreignMemberID  = 17
	orphanMemberID   = 18 // 未分配部门
	nonExistentMemID = 99
)

func managerScope() teamscope.Scope {
	return teamscope.DepartmentManager(scopeOwnerID, managedDeptID, managerMemberID)
}

func ownerScope() teamscope.Scope { return teamscope.Owner(scopeOwnerID) }

// scopeRepo 在 stubRepo 之上补一张"按 id 取成员"的表，并记录列表筛选条件。
type scopeRepo struct {
	*stubRepo
	members    map[int]Member
	lastFilter ListFilter
}

func newScopeRepo() *scopeRepo {
	return &scopeRepo{
		stubRepo: &stubRepo{},
		members: map[int]Member{
			managerMemberID: {ID: managerMemberID, OwnerID: scopeOwnerID, Name: "负责人", DepartmentID: managedDeptID, QuotaUSD: 50, AccountUserID: 901, AccountEmail: "lead@example.com", Email: "lead@example.com"},
			peerMemberID:    {ID: peerMemberID, OwnerID: scopeOwnerID, Name: "本部门同事", DepartmentID: managedDeptID, QuotaUSD: 20, AccountUserID: 902, AccountEmail: "peer@example.com", Email: "peer@example.com"},
			foreignMemberID: {ID: foreignMemberID, OwnerID: scopeOwnerID, Name: "他部门同事", DepartmentID: otherDeptID, QuotaUSD: 20, AccountUserID: 903, AccountEmail: "other@example.com", Email: "other@example.com"},
			orphanMemberID:  {ID: orphanMemberID, OwnerID: scopeOwnerID, Name: "未分配", DepartmentID: 0, QuotaUSD: 20},
		},
	}
}

func (s *scopeRepo) FindOwned(_ context.Context, _ int, id int) (Member, error) {
	m, ok := s.members[id]
	if !ok {
		return Member{}, ErrMemberNotFound
	}
	return m, nil
}

func (s *scopeRepo) ListByOwner(_ context.Context, _ int, filter ListFilter) ([]Member, int64, error) {
	s.lastFilter = filter
	return nil, 0, nil
}

// DepartmentOwnedBy 企业主 7 名下有 4 与 9 两个部门。
func (s *scopeRepo) DepartmentOwnedBy(_ context.Context, _ int, id int) (bool, error) {
	return id == managedDeptID || id == otherDeptID, nil
}

// recordingAudit 收集审计条目，用于校验"负责人的操作记在企业主名下"。
type recordingAudit struct{ entries []audit.Entry }

func (r *recordingAudit) Record(ctx context.Context, e audit.Entry) {
	actor := audit.ActorFromContext(ctx)
	if e.ActorUserID == 0 {
		e.ActorUserID = actor.UserID
	}
	if e.ActorEmail == "" {
		e.ActorEmail = actor.Email
	}
	r.entries = append(r.entries, e)
}

func ptr[T any](v T) *T { return &v }

// 成员五个端点 × 范围的权限矩阵。nil = 应放行。
func TestMemberServiceScopeMatrix(t *testing.T) {
	deptOf := func(id int) *int64 { return ptr(int64(id)) }

	cases := []struct {
		name  string
		scope teamscope.Scope
		op    func(*Service, teamscope.Scope) error
		want  error
	}{
		// —— 负责人：本部门内放行 ——
		{"负责人列本部门成员", managerScope(), func(s *Service, sc teamscope.Scope) error {
			_, err := s.List(context.Background(), sc, ListFilter{}, "UTC")
			return err
		}, nil},
		{"负责人读本部门成员", managerScope(), func(s *Service, sc teamscope.Scope) error {
			_, err := s.Get(context.Background(), sc, peerMemberID)
			return err
		}, nil},
		{"负责人在本部门建成员", managerScope(), func(s *Service, sc teamscope.Scope) error {
			_, err := s.Create(context.Background(), sc, CreateInput{Name: "新人", QuotaUSD: 10, DepartmentID: deptOf(managedDeptID)})
			return err
		}, nil},
		{"负责人不传部门建成员默认落本部门", managerScope(), func(s *Service, sc teamscope.Scope) error {
			_, err := s.Create(context.Background(), sc, CreateInput{Name: "新人", QuotaUSD: 10})
			return err
		}, nil},
		{"负责人改本部门成员额度", managerScope(), func(s *Service, sc teamscope.Scope) error {
			_, err := s.Update(context.Background(), sc, peerMemberID, UpdateInput{QuotaUSD: ptr(30.0)})
			return err
		}, nil},
		{"负责人停用本部门成员", managerScope(), func(s *Service, sc teamscope.Scope) error {
			_, err := s.Update(context.Background(), sc, peerMemberID, UpdateInput{Status: ptr(StatusDisabled)})
			return err
		}, nil},
		{"负责人改本部门成员白名单", managerScope(), func(s *Service, sc teamscope.Scope) error {
			_, err := s.Update(context.Background(), sc, peerMemberID, UpdateInput{AllowedGroupIDs: ptr([]int64{1, 2})})
			return err
		}, nil},
		{"负责人原样回传邮箱不算改凭证", managerScope(), func(s *Service, sc teamscope.Scope) error {
			_, err := s.Update(context.Background(), sc, peerMemberID, UpdateInput{Email: ptr("Peer@Example.com"), QuotaUSD: ptr(30.0)})
			return err
		}, nil},
		{"负责人删本部门成员", managerScope(), func(s *Service, sc teamscope.Scope) error {
			return s.Delete(context.Background(), sc, peerMemberID)
		}, nil},
		{"负责人重置本部门成员本期", managerScope(), func(s *Service, sc teamscope.Scope) error {
			_, err := s.ResetPeriod(context.Background(), sc, peerMemberID)
			return err
		}, nil},

		// —— 负责人：跨部门按"不存在"处理 ——
		{"负责人读他部门成员", managerScope(), func(s *Service, sc teamscope.Scope) error {
			_, err := s.Get(context.Background(), sc, foreignMemberID)
			return err
		}, ErrMemberNotFound},
		{"负责人改他部门成员", managerScope(), func(s *Service, sc teamscope.Scope) error {
			_, err := s.Update(context.Background(), sc, foreignMemberID, UpdateInput{QuotaUSD: ptr(999.0)})
			return err
		}, ErrMemberNotFound},
		{"负责人删他部门成员", managerScope(), func(s *Service, sc teamscope.Scope) error {
			return s.Delete(context.Background(), sc, foreignMemberID)
		}, ErrMemberNotFound},
		{"负责人重置他部门成员本期", managerScope(), func(s *Service, sc teamscope.Scope) error {
			_, err := s.ResetPeriod(context.Background(), sc, foreignMemberID)
			return err
		}, ErrMemberNotFound},
		{"负责人改未分配部门的成员", managerScope(), func(s *Service, sc teamscope.Scope) error {
			_, err := s.Update(context.Background(), sc, orphanMemberID, UpdateInput{QuotaUSD: ptr(999.0)})
			return err
		}, ErrMemberNotFound},
		{"负责人删不存在的成员", managerScope(), func(s *Service, sc teamscope.Scope) error {
			return s.Delete(context.Background(), sc, nonExistentMemID)
		}, ErrMemberNotFound},

		// —— 负责人：跨部门调岗 / 建到别的部门 ——
		{"负责人建成员到他部门", managerScope(), func(s *Service, sc teamscope.Scope) error {
			_, err := s.Create(context.Background(), sc, CreateInput{Name: "新人", QuotaUSD: 10, DepartmentID: deptOf(otherDeptID)})
			return err
		}, ErrOutOfScope},
		{"负责人把成员调去他部门", managerScope(), func(s *Service, sc teamscope.Scope) error {
			_, err := s.Update(context.Background(), sc, peerMemberID, UpdateInput{DepartmentID: deptOf(otherDeptID)})
			return err
		}, ErrOutOfScope},
		{"负责人把成员调出部门", managerScope(), func(s *Service, sc teamscope.Scope) error {
			_, err := s.Update(context.Background(), sc, peerMemberID, UpdateInput{DepartmentID: deptOf(0)})
			return err
		}, ErrOutOfScope},

		// —— 负责人：不能碰自己那条记录 ——
		{"负责人改自己额度", managerScope(), func(s *Service, sc teamscope.Scope) error {
			_, err := s.Update(context.Background(), sc, managerMemberID, UpdateInput{QuotaUSD: ptr(9999.0)})
			return err
		}, ErrOutOfScope},
		{"负责人删自己", managerScope(), func(s *Service, sc teamscope.Scope) error {
			return s.Delete(context.Background(), sc, managerMemberID)
		}, ErrOutOfScope},
		{"负责人重置自己本期", managerScope(), func(s *Service, sc teamscope.Scope) error {
			_, err := s.ResetPeriod(context.Background(), sc, managerMemberID)
			return err
		}, ErrOutOfScope},
		{"负责人读得到自己那条成员记录", managerScope(), func(s *Service, sc teamscope.Scope) error {
			// 列表里本就有自己（同部门），单查也必须能读到，只是改不动。
			_, err := s.Get(context.Background(), sc, managerMemberID)
			return err
		}, nil},

		// —— 负责人：不能动他人登录凭证 ——
		{"负责人重置他人密码", managerScope(), func(s *Service, sc teamscope.Scope) error {
			_, err := s.Update(context.Background(), sc, peerMemberID, UpdateInput{Password: ptr("newpass6")})
			return err
		}, ErrOutOfScope},
		{"负责人改他人登录邮箱", managerScope(), func(s *Service, sc teamscope.Scope) error {
			_, err := s.Update(context.Background(), sc, peerMemberID, UpdateInput{Email: ptr("hijack@example.com")})
			return err
		}, ErrOutOfScope},

		// —— 企业主 / 管理员：一如既往 ——
		{"企业主改他部门成员", ownerScope(), func(s *Service, sc teamscope.Scope) error {
			_, err := s.Update(context.Background(), sc, foreignMemberID, UpdateInput{QuotaUSD: ptr(30.0)})
			return err
		}, nil},
		{"企业主改负责人本人", ownerScope(), func(s *Service, sc teamscope.Scope) error {
			_, err := s.Update(context.Background(), sc, managerMemberID, UpdateInput{QuotaUSD: ptr(80.0)})
			return err
		}, nil},
		{"企业主删负责人", ownerScope(), func(s *Service, sc teamscope.Scope) error {
			return s.Delete(context.Background(), sc, managerMemberID)
		}, nil},
		{"企业主跨部门调岗", ownerScope(), func(s *Service, sc teamscope.Scope) error {
			_, err := s.Update(context.Background(), sc, peerMemberID, UpdateInput{DepartmentID: deptOf(otherDeptID)})
			return err
		}, nil},
		{"企业主重置他人密码", ownerScope(), func(s *Service, sc teamscope.Scope) error {
			_, err := s.Update(context.Background(), sc, peerMemberID, UpdateInput{Password: ptr("newpass6")})
			return err
		}, nil},
		{"企业主重置未分配成员本期", ownerScope(), func(s *Service, sc teamscope.Scope) error {
			_, err := s.ResetPeriod(context.Background(), sc, orphanMemberID)
			return err
		}, nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := NewService(newScopeRepo(), nil)
			err := tc.op(svc, tc.scope)
			if tc.want == nil {
				if err != nil {
					t.Fatalf("应放行，实际 err = %v", err)
				}
				return
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

// 登录邮箱守卫只认 users.email（AccountEmail）：members.email 是展示列，与账号邮箱分两条
// 非事务语句写入，中途失败会漂移——拿漂移后的旧值当放行依据就是一条账号接管链
// （改掉他人登录邮箱 → 走找回密码）。
func TestManagerEmailGuardTrustsAccountEmailOnly(t *testing.T) {
	// members.email 停在旧值、users.email 才是真身份（模拟两条语句之间失败留下的漂移）
	diverged := Member{
		ID: peerMemberID, OwnerID: scopeOwnerID, Name: "本部门同事", DepartmentID: managedDeptID,
		QuotaUSD: 20, AccountUserID: 902,
		Email:        "stale@example.com",
		AccountEmail: "real@example.com",
	}
	newRepo := func() *scopeRepo {
		repo := newScopeRepo()
		repo.members[peerMemberID] = diverged
		return repo
	}

	t.Run("传漂移的展示邮箱=改登录邮箱，必须拒", func(t *testing.T) {
		repo := newRepo()
		_, err := NewService(repo, nil).Update(context.Background(), managerScope(), peerMemberID,
			UpdateInput{Email: ptr("stale@example.com")})
		if !errors.Is(err, ErrOutOfScope) {
			t.Fatalf("err = %v, want ErrOutOfScope", err)
		}
		if repo.accountPatch != nil || repo.updated.Email != nil {
			t.Fatalf("拒绝后不应有任何写入: account=%+v member=%+v", repo.accountPatch, repo.updated.Email)
		}
	})

	t.Run("传攻击者邮箱同样拒", func(t *testing.T) {
		repo := newRepo()
		_, err := NewService(repo, nil).Update(context.Background(), managerScope(), peerMemberID,
			UpdateInput{Email: ptr("attacker@example.com")})
		if !errors.Is(err, ErrOutOfScope) {
			t.Fatalf("err = %v, want ErrOutOfScope", err)
		}
		if repo.accountPatch != nil {
			t.Fatalf("拒绝后不应打账号补丁: %+v", repo.accountPatch)
		}
	})

	t.Run("原样回传登录邮箱（大小写不同）放行且不改凭证", func(t *testing.T) {
		repo := newRepo()
		if _, err := NewService(repo, nil).Update(context.Background(), managerScope(), peerMemberID,
			UpdateInput{Email: ptr("REAL@Example.com"), QuotaUSD: ptr(30.0)}); err != nil {
			t.Fatalf("同址回传应放行: %v", err)
		}
		if repo.accountPatch != nil {
			t.Fatalf("同址回传不应打账号补丁: %+v", repo.accountPatch)
		}
	})

	t.Run("老模型成员（无账号）按展示列比对", func(t *testing.T) {
		repo := newScopeRepo()
		repo.members[peerMemberID] = Member{
			ID: peerMemberID, OwnerID: scopeOwnerID, Name: "老成员", DepartmentID: managedDeptID,
			QuotaUSD: 20, Email: "legacy@example.com",
		}
		if _, err := NewService(repo, nil).Update(context.Background(), managerScope(), peerMemberID,
			UpdateInput{Email: ptr("legacy@example.com")}); err != nil {
			t.Fatalf("同址回传应放行: %v", err)
		}
		repo2 := newScopeRepo()
		repo2.members[peerMemberID] = repo.members[peerMemberID]
		if _, err := NewService(repo2, nil).Update(context.Background(), managerScope(), peerMemberID,
			UpdateInput{Email: ptr("other@example.com")}); !errors.Is(err, ErrOutOfScope) {
			t.Fatalf("err = %v, want ErrOutOfScope", err)
		}
	})
}

// 越界不能留下任何写入痕迹：拒绝必须发生在落库之前。
func TestMemberScopeDenialLeavesNoWrite(t *testing.T) {
	t.Run("改自己", func(t *testing.T) {
		repo := newScopeRepo()
		if _, err := NewService(repo, nil).Update(context.Background(), managerScope(), managerMemberID, UpdateInput{QuotaUSD: ptr(9999.0)}); !errors.Is(err, ErrOutOfScope) {
			t.Fatalf("err = %v", err)
		}
		if repo.updated.QuotaUSD != nil || repo.accountPatch != nil {
			t.Fatalf("越界不应落库: %+v / %+v", repo.updated, repo.accountPatch)
		}
	})
	t.Run("删他部门成员", func(t *testing.T) {
		repo := newScopeRepo()
		if err := NewService(repo, nil).Delete(context.Background(), managerScope(), foreignMemberID); !errors.Is(err, ErrMemberNotFound) {
			t.Fatalf("err = %v", err)
		}
		if repo.deleted != 0 {
			t.Fatalf("越界不应删除，实际删了 %d", repo.deleted)
		}
	})
	t.Run("重置他部门成员", func(t *testing.T) {
		repo := newScopeRepo()
		if _, err := NewService(repo, nil).ResetPeriod(context.Background(), managerScope(), foreignMemberID); !errors.Is(err, ErrMemberNotFound) {
			t.Fatalf("err = %v", err)
		}
		if repo.resetCalled {
			t.Fatalf("越界不应重置本期")
		}
	})
	t.Run("改他人密码", func(t *testing.T) {
		repo := newScopeRepo()
		if _, err := NewService(repo, nil).Update(context.Background(), managerScope(), peerMemberID, UpdateInput{Password: ptr("newpass6")}); !errors.Is(err, ErrOutOfScope) {
			t.Fatalf("err = %v", err)
		}
		if repo.accountPatch != nil {
			t.Fatalf("越界不应打账号补丁: %+v", repo.accountPatch)
		}
	})
}

// 列表：负责人传什么 department_id 都被钉回本部门；企业主保留原样（含"只看未分配"的 0）。
func TestMemberListDepartmentFilterPinnedForManager(t *testing.T) {
	cases := []struct {
		name  string
		scope teamscope.Scope
		in    *int
		want  *int
	}{
		{"负责人不传", managerScope(), nil, ptr(managedDeptID)},
		{"负责人传他部门", managerScope(), ptr(otherDeptID), ptr(managedDeptID)},
		{"负责人传未分配", managerScope(), ptr(0), ptr(managedDeptID)},
		{"企业主不传", ownerScope(), nil, nil},
		{"企业主传部门", ownerScope(), ptr(otherDeptID), ptr(otherDeptID)},
		{"企业主传未分配", ownerScope(), ptr(0), ptr(0)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := newScopeRepo()
			if _, err := NewService(repo, nil).List(context.Background(), tc.scope, ListFilter{DepartmentID: tc.in}, "UTC"); err != nil {
				t.Fatalf("List: %v", err)
			}
			got := repo.lastFilter.DepartmentID
			switch {
			case tc.want == nil && got != nil:
				t.Fatalf("department filter = %d, want 不限", *got)
			case tc.want != nil && (got == nil || *got != *tc.want):
				t.Fatalf("department filter = %v, want %d", got, *tc.want)
			}
		})
	}
}

// 建成员：负责人不传部门时必须钉死本部门（否则会建出一个"未分配"的成员，谁都管不着）。
func TestMemberCreateByManagerPinsDepartment(t *testing.T) {
	repo := newScopeRepo()
	if _, err := NewService(repo, nil).Create(context.Background(), managerScope(), CreateInput{Name: "新人", QuotaUSD: 10}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !repo.created.HasDepartmentID || repo.created.DepartmentID == nil || *repo.created.DepartmentID != managedDeptID {
		t.Fatalf("新成员应落在负责人的部门: %+v", repo.created)
	}
}

// 审计：负责人的操作记在**部门所属企业主**名下，actor 是负责人自己——企业主据此看得到下属做了什么。
func TestManagerActionsAuditedUnderOwnerWithManagerActor(t *testing.T) {
	rec := &recordingAudit{}
	repo := newScopeRepo()
	svc := NewService(repo, rec)
	ctx := audit.WithActor(context.Background(), audit.Actor{UserID: 901, Email: "lead@example.com", IP: "10.0.0.9", RequestID: "req-1"})

	if _, err := svc.Update(ctx, managerScope(), peerMemberID, UpdateInput{QuotaUSD: ptr(30.0)}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if _, err := svc.Create(ctx, managerScope(), CreateInput{Name: "新人", QuotaUSD: 10}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if len(rec.entries) != 2 {
		t.Fatalf("审计条数 = %d, want 2", len(rec.entries))
	}
	for _, e := range rec.entries {
		if e.OwnerID != scopeOwnerID {
			t.Fatalf("审计 owner_id = %d, want 部门所属企业主 %d", e.OwnerID, scopeOwnerID)
		}
		if e.ActorUserID != 901 || e.ActorEmail != "lead@example.com" {
			t.Fatalf("审计 actor = %d / %s, want 负责人本人", e.ActorUserID, e.ActorEmail)
		}
	}
}
