package handler

import (
	"log/slog"
	"sort"

	"github.com/gin-gonic/gin"

	appaccount "github.com/DouDOU-start/airgate-core/internal/app/account"
	appgroup "github.com/DouDOU-start/airgate-core/internal/app/group"
	appuser "github.com/DouDOU-start/airgate-core/internal/app/user"
	"github.com/DouDOU-start/airgate-core/internal/server/dto"
	"github.com/DouDOU-start/airgate-core/internal/server/response"
)

// PricingHandler 价格总览：把分散在分组、账号、用户三处的定价信息聚合成一张表。
//
// 建这个接口是因为「谁按什么价」此前只能靠翻三个页面拼：分组页看标准倍率、
// 账号页看成本倍率、用户页只显示「专属倍率 · N 组」却看不到是哪组、什么价。
type PricingHandler struct {
	groups   *appgroup.Service
	users    *appuser.Service
	accounts *appaccount.Service
}

// NewPricingHandler 创建 PricingHandler。
func NewPricingHandler(groups *appgroup.Service, users *appuser.Service, accounts *appaccount.Service) *PricingHandler {
	return &PricingHandler{groups: groups, users: users, accounts: accounts}
}

const pricingListPageSize = 500

// Overview 返回全部分组的定价快照。
// GET /api/v1/admin/pricing/overview
func (h *PricingHandler) Overview(c *gin.Context) {
	ctx := c.Request.Context()

	groupResult, err := h.groups.List(ctx, appgroup.ListFilter{Page: 1, PageSize: pricingListPageSize})
	if err != nil {
		slog.Error("pricing_overview_list_groups_failed", "error", err)
		response.Error(c, 500, 500, "查询分组失败")
		return
	}
	accountResult, err := h.accounts.List(ctx, appaccount.ListFilter{Page: 1, PageSize: pricingListPageSize})
	if err != nil {
		slog.Error("pricing_overview_list_accounts_failed", "error", err)
		response.Error(c, 500, 500, "查询账号失败")
		return
	}
	overrides, err := h.users.ListAllGroupRateOverrides(ctx)
	if err != nil {
		slog.Error("pricing_overview_list_overrides_failed", "error", err)
		response.Error(c, 500, 500, "查询专属倍率失败")
		return
	}
	userResult, err := h.users.List(ctx, appuser.ListFilter{Page: 1, PageSize: pricingListPageSize})
	if err != nil {
		slog.Error("pricing_overview_list_users_failed", "error", err)
		response.Error(c, 500, 500, "查询用户失败")
		return
	}
	pricingModeByUser := make(map[int]string, len(userResult.List))
	for _, u := range userResult.List {
		pricingModeByUser[u.ID] = u.PricingMode
	}

	accountByID := make(map[int64]appaccount.Account, len(accountResult.List))
	for _, a := range accountResult.List {
		accountByID[int64(a.ID)] = a
	}

	groups := make([]dto.PricingGroupResp, 0, len(groupResult.List))
	for _, g := range groupResult.List {
		item := dto.PricingGroupResp{
			ID:             int64(g.ID),
			Name:           g.Name,
			Platform:       g.Platform,
			RateMultiplier: g.RateMultiplier,
			IsExclusive:    g.IsExclusive,
			Delisted:       g.Delisted,
			ModelCount:     len(g.ModelRouting),
		}
		item.CostAccountID, item.CostAccountName, item.CostMultiplier, item.RoutedAccounts =
			primaryCostAccount(g, accountByID)

		for _, o := range overrides[int64(g.ID)] {
			item.Overrides = append(item.Overrides, dto.PricingOverrideResp{
				UserID:      int64(o.UserID),
				Email:       o.Email,
				Username:    o.Username,
				Rate:        o.Rate,
				PricingMode: pricingModeByUser[o.UserID],
			})
		}
		sort.Slice(item.Overrides, func(i, j int) bool { return item.Overrides[i].Rate < item.Overrides[j].Rate })
		groups = append(groups, item)
	}

	response.Success(c, dto.PricingOverviewResp{Groups: groups})
}

// primaryCostAccount 找出该分组真正承接流量的账号，用它的倍率作为成本口径。
//
// 选取规则与调度一致：只看 model_routing 里显式列出的账号（非空 routing 即白名单），
// 在 active 账号中取 priority 最高的一个；priority 相同取 ID 小的，保证结果稳定。
// routing 为空表示不限制模型，此时退回该分组关联的全部 active 账号里 priority 最高者。
func primaryCostAccount(g appgroup.Group, accountByID map[int64]appaccount.Account) (int64, string, float64, int) {
	routed := make(map[int64]struct{})
	for _, ids := range g.ModelRouting {
		for _, id := range ids {
			routed[id] = struct{}{}
		}
	}
	candidates := make([]appaccount.Account, 0, len(routed))
	if len(routed) > 0 {
		for id := range routed {
			if a, ok := accountByID[id]; ok && a.State == "active" {
				candidates = append(candidates, a)
			}
		}
	} else {
		for _, a := range accountByID {
			if a.State != "active" {
				continue
			}
			for _, gid := range a.GroupIDs {
				if gid == int64(g.ID) {
					candidates = append(candidates, a)
					break
				}
			}
		}
	}
	if len(candidates) == 0 {
		return 0, "", 0, 0
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Priority != candidates[j].Priority {
			return candidates[i].Priority > candidates[j].Priority
		}
		return candidates[i].ID < candidates[j].ID
	})
	top := candidates[0]
	return int64(top.ID), top.Name, top.RateMultiplier, len(candidates)
}
