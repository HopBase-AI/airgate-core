package department

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DouDOU-start/airgate-core/internal/app/teamscope"
)

// 生产基线：企业主 7、部门 4（负责人 15 管的）、部门 9（别人的）。
const (
	scopeOwnerID    = 7
	managedDeptID   = 4
	otherDeptID     = 9
	managerMemberID = 15
)

func managerScope() teamscope.Scope {
	return teamscope.DepartmentManager(scopeOwnerID, managedDeptID, managerMemberID)
}

func ownerScope() teamscope.Scope { return teamscope.Owner(scopeOwnerID) }

func scopedRepo() *stubRepo {
	now := time.Date(2026, 9, 11, 8, 0, 0, 0, time.UTC)
	return &stubRepo{
		anchor: time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC),
		find: Department{
			ID: managedDeptID, Name: "研发部", QuotaUSD: 600, QuotaPeriod: QuotaPeriodMonthly,
			PeriodAnchor: now.AddDate(0, 0, -22), PeriodStart: now.AddDate(0, 0, -22), UsedQuota: 40,
		},
		all: []Department{
			{ID: managedDeptID, Name: "研发部", QuotaPeriod: QuotaPeriodMonthly},
			{ID: otherDeptID, Name: "销售部", QuotaPeriod: QuotaPeriodMonthly},
		},
	}
}

// 组织结构与账期只对企业主开放；只读路径按范围收敛。
func TestDepartmentServiceScopeMatrix(t *testing.T) {
	cases := []struct {
		name string
		op   func(*Service, teamscope.Scope) error
		want map[bool]error // key: 是否负责人范围
	}{
		{"建部门", func(s *Service, sc teamscope.Scope) error {
			_, err := s.Create(context.Background(), sc, CreateInput{Name: "新部门", QuotaUSD: 10})
			return err
		}, map[bool]error{true: ErrOutOfScope, false: nil}},
		{"改部门（含额度天花板 / 换负责人）", func(s *Service, sc teamscope.Scope) error {
			quota := 9999.0
			_, err := s.Update(context.Background(), sc, managedDeptID, UpdateInput{QuotaUSD: &quota})
			return err
		}, map[bool]error{true: ErrOutOfScope, false: nil}},
		{"删部门", func(s *Service, sc teamscope.Scope) error {
			return s.Delete(context.Background(), sc, managedDeptID)
		}, map[bool]error{true: ErrOutOfScope, false: nil}},
		{"重置部门本期", func(s *Service, sc teamscope.Scope) error {
			_, err := s.ResetPeriod(context.Background(), sc, managedDeptID)
			return err
		}, map[bool]error{true: ErrOutOfScope, false: nil}},
		{"改企业账期日", func(s *Service, sc teamscope.Scope) error {
			_, err := s.SetBillingDay(context.Background(), sc, 15, "UTC")
			return err
		}, map[bool]error{true: ErrOutOfScope, false: nil}},
		{"读本部门", func(s *Service, sc teamscope.Scope) error {
			_, err := s.Get(context.Background(), sc, managedDeptID)
			return err
		}, map[bool]error{true: nil, false: nil}},
		{"读他部门", func(s *Service, sc teamscope.Scope) error {
			_, err := s.Get(context.Background(), sc, otherDeptID)
			return err
		}, map[bool]error{true: ErrDepartmentNotFound, false: nil}},
		{"总览", func(s *Service, sc teamscope.Scope) error {
			_, err := s.Overview(context.Background(), sc, "UTC")
			return err
		}, map[bool]error{true: nil, false: nil}},
	}

	for _, tc := range cases {
		for _, isManager := range []bool{true, false} {
			scope := ownerScope()
			label := "企业主"
			if isManager {
				scope, label = managerScope(), "部门负责人"
			}
			t.Run(tc.name+"/"+label, func(t *testing.T) {
				repo := scopedRepo()
				err := tc.op(NewService(repo, repo), scope)
				want := tc.want[isManager]
				if want == nil {
					if err != nil {
						t.Fatalf("应放行，实际 err = %v", err)
					}
					return
				}
				if !errors.Is(err, want) {
					t.Fatalf("err = %v, want %v", err, want)
				}
				if len(repo.audits) != 0 {
					t.Fatalf("被拒的操作不应留下审计: %+v", repo.audits)
				}
				if repo.deleted != 0 || repo.created.Name != nil || repo.updated.QuotaUSD != nil || repo.setAnchor != nil {
					t.Fatalf("被拒的操作不应落库: deleted=%d created=%+v updated=%+v anchor=%v",
						repo.deleted, repo.created, repo.updated, repo.setAnchor)
				}
			})
		}
	}
}

// 负责人的列表 / 下拉里只有自己那个部门。
func TestDepartmentListScopedToManagedDepartment(t *testing.T) {
	repo := scopedRepo()
	svc := NewService(repo, nil)

	result, err := svc.List(context.Background(), managerScope(), ListFilter{}, "UTC")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if result.Total != 1 || len(result.List) != 1 || result.List[0].ID != managedDeptID {
		t.Fatalf("负责人列表 = %+v, want 只有部门 %d", result.List, managedDeptID)
	}
	if result.List[0].MemberCount != 2 {
		t.Fatalf("派生字段未补齐: %+v", result.List[0])
	}

	all, err := svc.All(context.Background(), managerScope(), "UTC")
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if len(all) != 1 || all[0].ID != managedDeptID {
		t.Fatalf("负责人下拉 = %+v", all)
	}

	ownerList, err := svc.List(context.Background(), ownerScope(), ListFilter{}, "UTC")
	if err != nil {
		t.Fatalf("List(owner): %v", err)
	}
	if ownerList.Total != 2 {
		t.Fatalf("企业主应看到全部部门，实际 %d", ownerList.Total)
	}
}

// 负责人总览 = 部门口径：不含企业余额，本期消耗按部门聚合。
func TestOverviewScopedForDepartmentManager(t *testing.T) {
	repo := scopedRepo()
	svc := NewService(repo, nil)

	got, err := svc.Overview(context.Background(), managerScope(), "UTC")
	if err != nil {
		t.Fatalf("Overview: %v", err)
	}
	if got.Balance != 0 {
		t.Fatalf("企业余额不能对部门负责人暴露，实际 %v", got.Balance)
	}
	if got.DepartmentCount != 1 || got.MemberCount != 2 {
		t.Fatalf("总览应是部门口径: %+v", got)
	}
	if got.DepartmentQuotaTotal != 600 || got.MemberQuotaTotal != 120 {
		t.Fatalf("额度口径 = %v / %v, want 600 / 120", got.DepartmentQuotaTotal, got.MemberQuotaTotal)
	}
	if got.UnassignedMemberQuota != 0 {
		t.Fatalf("未分配部门的额度与负责人无关，实际 %v", got.UnassignedMemberQuota)
	}
	if got.PeriodUsedActual != 11 || got.PeriodUsedBilled != 12 {
		t.Fatalf("本期消耗应取部门聚合（11/12），实际 %v / %v", got.PeriodUsedActual, got.PeriodUsedBilled)
	}
	if repo.periodUsageDept != managedDeptID {
		t.Fatalf("本期消耗问的是部门 %d，want %d", repo.periodUsageDept, managedDeptID)
	}
	// 租户闸门：department_id 是裸快照列，聚合必须同时按企业主限定。
	if repo.periodUsageOwner != scopeOwnerID {
		t.Fatalf("本期消耗未带企业主闸门: ownerID = %d, want %d", repo.periodUsageOwner, scopeOwnerID)
	}
	// 账期沿用企业口径（三层同窗）
	if got.BillingDay != repo.anchor.Day() {
		t.Fatalf("账期日 = %d, want 企业锚点日 %d", got.BillingDay, repo.anchor.Day())
	}

	// 企业主口径不受影响：余额、全部部门与企业级消耗照旧
	ownerOverview, err := svc.Overview(context.Background(), ownerScope(), "UTC")
	if err != nil {
		t.Fatalf("Overview(owner): %v", err)
	}
	if ownerOverview.Balance != 1000 || ownerOverview.DepartmentCount != 2 || ownerOverview.PeriodUsedActual != 55 {
		t.Fatalf("企业主总览被改坏了: %+v", ownerOverview)
	}
}
