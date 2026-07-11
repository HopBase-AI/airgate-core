package scheduler

import (
	"context"
	"sort"
	"time"

	"github.com/DouDOU-start/airgate-core/ent/account"
	"github.com/DouDOU-start/airgate-core/ent/accountevent"
	entgroup "github.com/DouDOU-start/airgate-core/ent/group"
)

// 管理员 / 配额巡检的状态写入口。这些调用不经过 Apply —— 它们是"外部已知事实"
// 的直接落库，不需要 RPM 回退、失败计数等逻辑。

// ManualRecover 运维手动把账号恢复到 active：清状态、清到期、清原因，并立即刷新路由缓存。
func (s *Scheduler) ManualRecover(ctx context.Context, accountID int) error {
	dbCtx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()
	err := s.db.Account.UpdateOneID(accountID).
		SetState(account.StateActive).
		ClearStateUntil().
		SetErrorMsg("").
		Exec(dbCtx)
	if err == nil {
		_ = s.sanitizeModelRoutingForAccount(ctx, accountID)
		s.routeCache.InvalidateAll()
		s.state.recordEvent(accountID, accountevent.EventTypeManualRecovered, "", "", eventSourceManual, 0, nil)
	}
	return err
}

// ManualDisable 运维手动禁用账号（语义等同自动 disabled，需要再次 ManualRecover 才能恢复）。
func (s *Scheduler) ManualDisable(ctx context.Context, accountID int, reason string) error {
	dbCtx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()
	err := s.db.Account.UpdateOneID(accountID).
		SetState(account.StateDisabled).
		ClearStateUntil().
		SetErrorMsg(truncateReason(reason)).
		Exec(dbCtx)
	if err == nil {
		_ = s.sanitizeModelRoutingForAccount(ctx, accountID)
		s.routeCache.InvalidateAll()
		s.state.recordEvent(accountID, accountevent.EventTypeManualDisabled, reason, "", eventSourceManual, 0, nil)
	}
	return err
}

// MarkRateLimited 配额巡检发现额度窗口已满时打入 rate_limited 直到 until。
// 事件只在"进入"限流态时记一条：巡检每轮都会重打 until，
// 若每轮都落事件，持续限流的账号会把异常监控刷成同一条记录的重复噪声。
func (s *Scheduler) MarkRateLimited(ctx context.Context, accountID int, until time.Time, reason string) {
	alreadyIn := s.accountInState(ctx, accountID, account.StateRateLimited)
	s.state.transition(ctx, accountID, account.StateRateLimited, &until, reason)
	if !alreadyIn {
		s.state.recordEvent(accountID, accountevent.EventTypeRateLimited, reason, "", eventSourceProbe, 0, &until)
	}
}

// ClearRateLimited 配额巡检发现已恢复时清限流态回到 active。
func (s *Scheduler) ClearRateLimited(ctx context.Context, accountID int) {
	s.state.transitionActive(ctx, accountID, eventSourceProbe)
}

// ClearRateLimitMarkers 清除账号上的临时限流标记，不会恢复手动禁用的账号。
func (s *Scheduler) ClearRateLimitMarkers(ctx context.Context, accountID int) int {
	cleared := s.ClearFamilyCooldowns(ctx, accountID)
	dbCtx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()
	item, err := s.db.Account.Get(dbCtx, accountID)
	if err != nil {
		return cleared
	}
	if item.State == account.StateRateLimited || item.State == account.StateDegraded {
		s.state.transitionActive(ctx, accountID, eventSourceManual)
		cleared++
	}
	return cleared
}

// MarkDisabled 把账号标记为 disabled（凭证失效等确定性错误）。
// 与 MarkRateLimited 同理，只在进入 disabled 时落事件。
func (s *Scheduler) MarkDisabled(ctx context.Context, accountID int, reason string) {
	alreadyIn := s.accountInState(ctx, accountID, account.StateDisabled)
	s.state.transition(ctx, accountID, account.StateDisabled, nil, reason)
	if !alreadyIn {
		s.state.recordEvent(accountID, accountevent.EventTypeDisabled, reason, "", eventSourceProbe, 0, nil)
	}
	_ = s.sanitizeModelRoutingForAccount(ctx, accountID)
	s.routeCache.InvalidateAll()
}

// accountInState 读取账号当前是否已处于目标状态（读失败按"不在"处理，宁多记不漏记）。
func (s *Scheduler) accountInState(ctx context.Context, accountID int, target account.State) bool {
	dbCtx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()
	item, err := s.db.Account.Get(dbCtx, accountID)
	return err == nil && item.State == target
}

func (s *Scheduler) sanitizeModelRoutingForAccount(ctx context.Context, accountID int) error {
	dbCtx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	groups, err := s.db.Group.Query().
		Where(entgroup.HasAccountsWith(account.IDEQ(accountID))).
		All(dbCtx)
	if err != nil {
		return err
	}
	for _, group := range groups {
		if len(group.ModelRouting) == 0 {
			continue
		}
		accountIDs, err := s.db.Account.Query().
			Where(
				account.HasGroupsWith(entgroup.IDEQ(group.ID)),
				account.StateNEQ(account.StateDisabled),
			).
			IDs(dbCtx)
		if err != nil {
			return err
		}
		available := make(map[int64]struct{}, len(accountIDs))
		for _, id := range accountIDs {
			available[int64(id)] = struct{}{}
		}
		cleaned := sanitizeModelRouting(group.ModelRouting, available)
		if modelRoutingEqual(group.ModelRouting, cleaned) {
			continue
		}
		if err := s.db.Group.UpdateOneID(group.ID).SetModelRouting(cleaned).Exec(dbCtx); err != nil {
			return err
		}
	}
	return nil
}

func sanitizeModelRouting(input map[string][]int64, availableAccountIDs map[int64]struct{}) map[string][]int64 {
	if input == nil {
		return nil
	}
	cleaned := make(map[string][]int64, len(input))
	fallback := sortedAccountIDs(availableAccountIDs)
	for model, ids := range input {
		if len(ids) == 0 {
			cleaned[model] = []int64{}
			continue
		}
		kept := make([]int64, 0, len(ids))
		seen := make(map[int64]struct{}, len(ids))
		for _, id := range ids {
			if _, ok := availableAccountIDs[id]; !ok {
				continue
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			kept = append(kept, id)
		}
		if len(kept) == 0 && len(fallback) > 0 {
			kept = append([]int64(nil), fallback...)
		}
		cleaned[model] = kept
	}
	return cleaned
}

func sortedAccountIDs(ids map[int64]struct{}) []int64 {
	result := make([]int64, 0, len(ids))
	for id := range ids {
		result = append(result, id)
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}

func modelRoutingEqual(a, b map[string][]int64) bool {
	if len(a) != len(b) {
		return false
	}
	for key, av := range a {
		bv, ok := b[key]
		if !ok || len(av) != len(bv) {
			return false
		}
		for i := range av {
			if av[i] != bv[i] {
				return false
			}
		}
	}
	return true
}
