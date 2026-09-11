package handler

import (
	"strings"
	"testing"

	"github.com/DouDOU-start/airgate-core/internal/server/dto"
)

// 筛选权限的唯一判定处，四种身份逐个钉住。前端只照着渲染，改权限只改这里。
func TestUsageFilterFields(t *testing.T) {
	cases := []struct {
		name      string
		resp      dto.UserResp
		isManager bool
		want      []string
	}{
		{
			name: "管理员：成员与部门都能筛",
			resp: dto.UserResp{Role: "admin"},
			want: []string{dto.UsageFilterAPIKey, dto.UsageFilterMember, dto.UsageFilterDepartment},
		},
		{
			name: "企业主：成员与部门都能筛",
			resp: dto.UserResp{Role: "user", IsEnterpriseOwner: true},
			want: []string{dto.UsageFilterAPIKey, dto.UsageFilterMember, dto.UsageFilterDepartment},
		},
		{
			name:      "部门负责人：只加成员，不给部门",
			resp:      dto.UserResp{Role: "user", MemberID: 7, ManagedDepartmentID: 3},
			isManager: true,
			want:      []string{dto.UsageFilterAPIKey, dto.UsageFilterMember},
		},
		{
			name: "普通成员：只有 API Key",
			resp: dto.UserResp{Role: "user", MemberID: 7},
			want: []string{dto.UsageFilterAPIKey},
		},
		{
			name: "普通用户：只有 API Key",
			resp: dto.UserResp{Role: "user"},
			want: []string{dto.UsageFilterAPIKey},
		},
		{
			// 成员账号被误置企业主标志时，不能因此拿到全企业筛选
			name: "成员误置企业主标志：仍按成员判",
			resp: dto.UserResp{Role: "user", MemberID: 7, IsEnterpriseOwner: true},
			want: []string{dto.UsageFilterAPIKey},
		},
		{
			name:      "负责人误置企业主标志：仍不给部门筛选",
			resp:      dto.UserResp{Role: "user", MemberID: 7, ManagedDepartmentID: 3, IsEnterpriseOwner: true},
			isManager: true,
			want:      []string{dto.UsageFilterAPIKey, dto.UsageFilterMember},
		},
		{
			// 「管多个部门」是产品造不出来的脏数据，middleware 按 len==1 fail-closed，
			// 到这里 isManager 已经是 false，与团队页 403 同一条规则。
			name:      "负责多部门（脏数据）：按非负责人处理，不给成员筛选",
			resp:      dto.UserResp{Role: "user", MemberID: 7, ManagedDepartmentID: 0},
			isManager: false,
			want:      []string{dto.UsageFilterAPIKey},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := usageFilterFields(c.resp, c.isManager)
			if strings.Join(got, ",") != strings.Join(c.want, ",") {
				t.Errorf("usageFilterFields = %v, want %v", got, c.want)
			}
		})
	}
}
