package handler

import (
	"strings"
	"testing"

	"github.com/DouDOU-start/airgate-core/internal/server/dto"
)

// 筛选权限的唯一判定处，四种身份逐个钉住。前端只照着渲染，改权限只改这里。
func TestUsageFilterFields(t *testing.T) {
	cases := []struct {
		name string
		resp dto.UserResp
		want []string
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
			name: "部门负责人：只加成员，不给部门",
			resp: dto.UserResp{Role: "user", MemberID: 7, ManagedDepartmentID: 3},
			want: []string{dto.UsageFilterAPIKey, dto.UsageFilterMember},
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
			name: "负责人误置企业主标志：仍不给部门筛选",
			resp: dto.UserResp{Role: "user", MemberID: 7, ManagedDepartmentID: 3, IsEnterpriseOwner: true},
			want: []string{dto.UsageFilterAPIKey, dto.UsageFilterMember},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := usageFilterFields(c.resp)
			if strings.Join(got, ",") != strings.Join(c.want, ",") {
				t.Errorf("usageFilterFields = %v, want %v", got, c.want)
			}
		})
	}
}
