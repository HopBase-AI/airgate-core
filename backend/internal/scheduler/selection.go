package scheduler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	sdk "github.com/DouDOU-start/airgate-sdk/sdkgo"

	"github.com/DouDOU-start/airgate-core/ent"
	"github.com/DouDOU-start/airgate-core/ent/account"
)

// SelectAccount 选一个可用账户。流程：
//
//	模型路由 → 状态过滤 → 软约束过滤（RPM / window / session）→
//	粘性会话 → 负载均衡。
//
// excludeIDs 为 failover 时已尝试过的账户。
func (s *Scheduler) SelectAccount(ctx context.Context, platform, model string, userID, groupID int, sessionID string, excludeIDs ...int) (*ent.Account, error) {
	return s.SelectAccountWithRequirements(ctx, platform, model, userID, groupID, sessionID, AccountRequirements{}, excludeIDs...)
}

func (s *Scheduler) SelectAccountWithRequirements(ctx context.Context, platform, model string, userID, groupID int, sessionID string, req AccountRequirements, excludeIDs ...int) (*ent.Account, error) {
	tiers, err := s.routeAccountTiers(ctx, platform, model, groupID)
	if err != nil {
		return nil, err
	}
	filterTier := func(candidates []*ent.Account) []*ent.Account {
		candidates = excludeAccounts(candidates, excludeIDs)
		candidates = filterAccountsByRequirements(candidates, req)
		if fn, ok := s.accountFilters[platform]; ok {
			candidates = fn(candidates, model)
		}
		return candidates
	}
	primaryCandidates := filterTier(tiers.primary)
	fallbackCandidates := filterTier(tiers.poolFallback)
	if len(primaryCandidates) == 0 && len(fallbackCandidates) == 0 {
		return nil, ErrNoAvailableAccount
	}

	now := time.Now()

	// 预获取所有候选账号的并发负载，避免 checkSchedulability 和 selectByLoadBalance 重复查 Redis
	loadCache := make(map[int]int, len(primaryCandidates)+len(fallbackCandidates))
	for _, acc := range appendAccountSlices(primaryCandidates, fallbackCandidates) {
		loadCache[acc.ID] = s.getCurrentLoad(ctx, acc.ID)
	}

	primaryNormal, primaryStickyOnly, primaryUnavailable := s.partitionSchedulableDetailed(ctx, primaryCandidates, model, now, loadCache)
	fallbackNormal, fallbackStickyOnly, fallbackUnavailable := s.partitionSchedulableDetailed(ctx, fallbackCandidates, model, now, loadCache)
	unavailable := primaryUnavailable.merge(fallbackUnavailable)
	sessionCapacityExhausted := false
	previousStickyAccountID := 0

	selectionGroups := []candidateTier{
		{accounts: primaryNormal},
		{accounts: fallbackNormal, poolFallback: true},
		{accounts: primaryStickyOnly, degraded: true},
		{accounts: fallbackStickyOnly, poolFallback: true, degraded: true},
	}
	selectionOrder := make([]candidateTier, 0, len(selectionGroups))
	for _, group := range selectionGroups {
		for _, accounts := range accountPriorityTiers(group.accounts) {
			selectionOrder = append(selectionOrder, candidateTier{
				accounts:     accounts,
				poolFallback: group.poolFallback,
				degraded:     group.degraded,
			})
		}
	}

	// Normal sticky 只能命中当前最优健康层：当高优先级主账号恢复后，之前临时切到备用账号的会话会回迁，
	// 避免 sticky 续期让备用账号变成永久流量源。StickyOnly 会话则保留其软容量豁免，除非出现同路由下更优的 Normal 层。
	if sessionID != "" {
		if accountID, found := s.sticky.Get(ctx, userID, platform, sessionID); found {
			previousStickyAccountID = accountID
			binding := findStickyCandidate(accountID, primaryNormal, fallbackNormal, primaryStickyOnly, fallbackStickyOnly)
			winningNormal := firstNormalCandidateTier(selectionOrder)
			if canReuseStickyCandidate(binding, winningNormal) {
				acc := binding.account
				if !s.claimSelectedAccountGate(ctx, acc, model, &unavailable) {
					// 另一个请求已抢到 half-open probe；当前请求继续走备用。
				} else if s.registerSelectedSession(ctx, previousStickyAccountID, acc, userID, platform, sessionID) {
					return acc, nil
				} else {
					s.ReleaseFamilyProbe(ctx, acc.ID, acc.Platform, model, familyProbeTokenFromContext(ctx))
					sessionCapacityExhausted = true
				}
			}
		}
	}
	for _, tier := range selectionOrder {
		remaining := append([]*ent.Account(nil), tier.accounts...)
		for len(remaining) > 0 {
			selected := s.selectByLoadBalanceWithCache(ctx, remaining, now, loadCache)
			if selected == nil {
				break
			}
			remaining = removeAccountByID(remaining, selected.ID)
			if !s.claimSelectedAccountGate(ctx, selected, model, &unavailable) {
				continue
			}
			if !s.registerSelectedSession(ctx, previousStickyAccountID, selected, userID, platform, sessionID) {
				s.ReleaseFamilyProbe(ctx, selected.ID, selected.Platform, model, familyProbeTokenFromContext(ctx))
				sessionCapacityExhausted = true
				continue
			}
			if tier.poolFallback {
				slog.Warn("scheduler_pool_failover_account",
					sdk.LogFieldAccountID, selected.ID,
					sdk.LogFieldGroupID, groupID,
					sdk.LogFieldPlatform, platform,
					sdk.LogFieldModel, model,
				)
			}
			if tier.degraded {
				slog.Warn("scheduler_fallback_degraded_account",
					sdk.LogFieldAccountID, selected.ID,
					sdk.LogFieldPlatform, platform,
					sdk.LogFieldModel, model,
				)
			}
			return selected, nil
		}
	}
	if !sessionCapacityExhausted && unavailable.allRecoverableAccountsRateLimited() {
		return nil, &AccountsRateLimitedError{RetryAt: unavailable.earliestRateLimit}
	}
	if unavailable.transientSeen {
		return nil, ErrTransientCandidatesUnavailable
	}
	if sessionCapacityExhausted || unavailable.localCapacitySeen {
		return nil, ErrLocalCapacityUnavailable
	}
	if unavailable.terminalSeen || len(selectionOrder) > 0 {
		return nil, ErrNonRateLimitedCandidatesUnavailable
	}
	return nil, ErrNoAvailableAccount
}

func firstNormalCandidateTier(tiers []candidateTier) candidateTier {
	for _, tier := range tiers {
		if len(tier.accounts) > 0 && !tier.degraded {
			return tier
		}
	}
	return candidateTier{}
}

func appendAccountSlices(first, second []*ent.Account) []*ent.Account {
	combined := make([]*ent.Account, 0, len(first)+len(second))
	combined = append(combined, first...)
	combined = append(combined, second...)
	return combined
}

func (s *Scheduler) partitionSchedulable(ctx context.Context, candidates []*ent.Account, model string, now time.Time, loadCache map[int]int) (normal, stickyOnly []*ent.Account) {
	normal, stickyOnly, _ = s.partitionSchedulableDetailed(ctx, candidates, model, now, loadCache)
	return normal, stickyOnly
}

type schedulabilityDecision struct {
	state            Schedulability
	unavailableUntil time.Time
	circuitKind      familyCircuitKind
	cause            candidateUnavailabilityCause
}

type candidateUnavailabilityCause uint8

const (
	candidateUnavailableNone candidateUnavailabilityCause = iota
	candidateUnavailableLocalCapacity
	candidateUnavailableTerminal
)

type unavailabilitySummary struct {
	rateLimitedSeen   bool
	transientSeen     bool
	localCapacitySeen bool
	terminalSeen      bool
	earliestRateLimit time.Time
}

type candidateTier struct {
	accounts     []*ent.Account
	poolFallback bool
	degraded     bool
}

type stickyCandidate struct {
	account        *ent.Account
	schedulability Schedulability
	poolFallback   bool
}

func findStickyCandidate(accountID int, primaryNormal, fallbackNormal, primaryStickyOnly, fallbackStickyOnly []*ent.Account) stickyCandidate {
	groups := []stickyCandidate{
		{schedulability: Normal},
		{schedulability: Normal, poolFallback: true},
		{schedulability: StickyOnly},
		{schedulability: StickyOnly, poolFallback: true},
	}
	accounts := [][]*ent.Account{primaryNormal, fallbackNormal, primaryStickyOnly, fallbackStickyOnly}
	for i, candidates := range accounts {
		for _, acc := range candidates {
			if acc.ID == accountID {
				groups[i].account = acc
				return groups[i]
			}
		}
	}
	return stickyCandidate{}
}

func canReuseStickyCandidate(binding stickyCandidate, winningNormal candidateTier) bool {
	if binding.account == nil {
		return false
	}
	if len(winningNormal.accounts) == 0 {
		return true
	}
	if binding.schedulability == Normal {
		for _, acc := range winningNormal.accounts {
			if acc.ID == binding.account.ID {
				return true
			}
		}
		return false
	}

	// 既有 StickyOnly 会话可以留在显式主 route，不被 pool fallback 的新流量策略迁走。
	if binding.poolFallback != winningNormal.poolFallback {
		return !binding.poolFallback
	}
	return binding.account.Priority >= winningNormal.accounts[0].Priority
}

func (s unavailabilitySummary) merge(other unavailabilitySummary) unavailabilitySummary {
	merged := unavailabilitySummary{
		rateLimitedSeen:   s.rateLimitedSeen || other.rateLimitedSeen,
		transientSeen:     s.transientSeen || other.transientSeen,
		localCapacitySeen: s.localCapacitySeen || other.localCapacitySeen,
		terminalSeen:      s.terminalSeen || other.terminalSeen,
		earliestRateLimit: s.earliestRateLimit,
	}
	if !other.earliestRateLimit.IsZero() && (merged.earliestRateLimit.IsZero() || other.earliestRateLimit.Before(merged.earliestRateLimit)) {
		merged.earliestRateLimit = other.earliestRateLimit
	}
	return merged
}

func (s unavailabilitySummary) allRecoverableAccountsRateLimited() bool {
	return s.rateLimitedSeen && !s.transientSeen && !s.localCapacitySeen && !s.terminalSeen && !s.earliestRateLimit.IsZero()
}

func (s *unavailabilitySummary) addCircuit(kind familyCircuitKind, until time.Time) {
	if kind != familyCircuitRateLimit {
		s.transientSeen = true
		return
	}
	s.rateLimitedSeen = true
	if !until.IsZero() && (s.earliestRateLimit.IsZero() || until.Before(s.earliestRateLimit)) {
		s.earliestRateLimit = until
	}
}

func (s *unavailabilitySummary) addCause(cause candidateUnavailabilityCause) {
	switch cause {
	case candidateUnavailableLocalCapacity:
		s.localCapacitySeen = true
	case candidateUnavailableTerminal:
		s.terminalSeen = true
	}
}

// claimSelectedAccountGate is deliberately unconditional. Candidate scanning is
// read-only, so a concurrent failure may open a circuit before the selected
// account reaches this point. ClaimProbe atomically rechecks healthy/active/
// half-open state and claims the single recovery token when needed.
func (s *Scheduler) claimSelectedAccountGate(ctx context.Context, acc *ent.Account, model string, unavailable *unavailabilitySummary) bool {
	if acc == nil || s.familyCooldown == nil {
		return true
	}
	family := s.resolveModelFamily(acc.Platform, model)
	if family == "" {
		return true
	}
	status := s.familyCooldown.ClaimProbe(ctx, acc.ID, family, familyProbeTokenFromContext(ctx))
	if !status.blocked {
		return true
	}
	unavailable.addCircuit(status.kind, status.until)
	return false
}

// registerSelectedSession atomically moves a sticky session's capacity slot to
// the selected account. The sticky binding is updated only after the target slot
// is secured, so a failed migration leaves both the old binding and slot intact.
func (s *Scheduler) registerSelectedSession(ctx context.Context, previousAccountID int, selected *ent.Account, userID int, platform, sessionID string) bool {
	if sessionID == "" {
		return true
	}
	maxSessions := ExtraInt(selected.Extra, "max_sessions")
	idleTimeout := time.Duration(ExtraInt(selected.Extra, "session_idle_timeout")) * time.Second
	if idleTimeout <= 0 {
		idleTimeout = defaultSessionIdleTimeout
	}
	allowed, err := s.session.MigrateSession(ctx, previousAccountID, selected.ID, sessionID, maxSessions, idleTimeout)
	if err != nil {
		slog.Debug("迁移会话并发槽失败",
			"previous_account_id", previousAccountID,
			sdk.LogFieldAccountID, selected.ID,
			"session_id", sessionID,
			sdk.LogFieldError, err,
		)
		return true // Redis 不可用时与现有会话限制一致，失败开放。
	}
	if !allowed {
		return false
	}
	s.sticky.Set(ctx, userID, platform, sessionID, selected.ID, s.sticky.stickyTTLFromExtra(selected.Extra))
	return true
}

func (s *Scheduler) partitionSchedulableDetailed(ctx context.Context, candidates []*ent.Account, model string, now time.Time, loadCache map[int]int) (normal, stickyOnly []*ent.Account, unavailable unavailabilitySummary) {
	normal = make([]*ent.Account, 0, len(candidates))
	stickyOnly = make([]*ent.Account, 0, len(candidates))
	for _, acc := range candidates {
		decision := s.evaluateSchedulabilityWithLoad(ctx, acc, model, now, loadCache[acc.ID])
		switch decision.state {
		case Normal:
			normal = append(normal, acc)
		case StickyOnly:
			stickyOnly = append(stickyOnly, acc)
		case NotSchedulable:
			if decision.circuitKind != "" {
				unavailable.addCircuit(decision.circuitKind, decision.unavailableUntil)
			} else {
				unavailable.addCause(decision.cause)
			}
		}
	}
	return normal, stickyOnly, unavailable
}

func removeAccountByID(accounts []*ent.Account, accountID int) []*ent.Account {
	filtered := accounts[:0]
	for _, acc := range accounts {
		if acc.ID != accountID {
			filtered = append(filtered, acc)
		}
	}
	return filtered
}

// accountPriorityTiers 把已通过 route / requirements / schedulability 的候选按 priority
// 降序分层。调度器必须耗尽当前层后才能降级到下一层，避免低质量备用
// 账号在主账号健康时分走新请求。
func accountPriorityTiers(candidates []*ent.Account) [][]*ent.Account {
	if len(candidates) == 0 {
		return nil
	}
	byPriority := make(map[int][]*ent.Account)
	priorities := make([]int, 0)
	for _, acc := range candidates {
		if _, exists := byPriority[acc.Priority]; !exists {
			priorities = append(priorities, acc.Priority)
		}
		byPriority[acc.Priority] = append(byPriority[acc.Priority], acc)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(priorities)))
	tiers := make([][]*ent.Account, 0, len(priorities))
	for _, priority := range priorities {
		tiers = append(tiers, byPriority[priority])
	}
	return tiers
}

// excludeAccounts 过滤掉 excludeIDs 中的账号（failover 已尝试过的）。
func excludeAccounts(candidates []*ent.Account, excludeIDs []int) []*ent.Account {
	if len(excludeIDs) == 0 {
		return candidates
	}
	excludeSet := make(map[int]struct{}, len(excludeIDs))
	for _, id := range excludeIDs {
		excludeSet[id] = struct{}{}
	}
	filtered := make([]*ent.Account, 0, len(candidates))
	for _, acc := range candidates {
		if _, excluded := excludeSet[acc.ID]; !excluded {
			filtered = append(filtered, acc)
		}
	}
	return filtered
}

// maybeRegisterSession 有 sessionID 时登记会话；session 数超限换一个候选重试。
func (s *Scheduler) maybeRegisterSession(ctx context.Context, selected *ent.Account, userID int, platform, sessionID string, pool []*ent.Account, now time.Time) (*ent.Account, error) {
	if sessionID == "" {
		return selected, nil
	}
	remaining := append([]*ent.Account(nil), pool...)
	for selected != nil {
		if s.RegisterSession(ctx, selected.ID, sessionID, selected.Extra) {
			s.sticky.Set(ctx, userID, platform, sessionID, selected.ID, s.sticky.stickyTTLFromExtra(selected.Extra))
			return selected, nil
		}
		retry := remaining[:0]
		for _, acc := range remaining {
			if acc.ID != selected.ID {
				retry = append(retry, acc)
			}
		}
		remaining = retry
		selected = s.selectByLoadBalance(ctx, remaining, now)
	}
	return nil, ErrNoAvailableAccount
}

// routeAccounts 取分组下匹配模型路由的账号；状态过滤延到 checkSchedulability。
//
// 不按 state 过滤的原因：新账号刚解除 disabled 后可立即被调度，不用等缓存失效。
//
// 首层命中 routeCache（key = (groupID, platform)）；miss 才查 DB。Model routing
// 规则与账号列表一起缓存，按 model 过滤的动作每次都重新跑——避免"不同 model 复用同一条缓存"
// 带来的错配。
type routedAccountTiers struct {
	primary      []*ent.Account
	poolFallback []*ent.Account
}

func (s *Scheduler) routeAccountTiers(ctx context.Context, platform, model string, groupID int) (routedAccountTiers, error) {
	if accounts, routing, ok := s.routeCache.Get(groupID, platform); ok {
		return classifyRoutedAccountTiers(accounts, routing, model)
	}

	grp, err := s.db.Group.Get(ctx, groupID)
	if err != nil {
		return routedAccountTiers{}, normalizeGroupLookupError(err)
	}

	accounts, err := grp.QueryAccounts().
		Where(account.PlatformEQ(platform)).
		WithProxy().
		All(ctx)
	if err != nil {
		return routedAccountTiers{}, normalizeGroupAccountsLookupError(err)
	}

	// 缓存全量 platform 账号（包含所有 state）+ group 的 ModelRouting
	s.routeCache.Set(groupID, platform, accounts, grp.ModelRouting)

	return classifyRoutedAccountTiers(accounts, grp.ModelRouting, model)
}

// classifyRoutedAccounts 在候选为空时区分"结构性不可用"与"暂时不可用"，让上层能给客户端
// 一个不可重试的 4xx，而不是让它对着一个永远不会恢复的分组无限退避重试。
//
// 判定顺序即语义优先级：
//  1. 分组在本平台下没有任何账号        → ErrGroupOffline
//  2. model_routing 把候选过滤空了      → ErrModelNotServed（分组还在，只是不供这个模型）
//  3. 候选全是 disabled                 → ErrGroupOffline（管理员下线，或账号全部判死）
//
// 第 3 步只认 disabled 这一个终态：rate_limited / degraded 都会自行到期恢复，属于容量问题，
// 必须继续走 ErrNoAvailableAccount 的 503 重试路径。读的 State 与 SchedulabilityOf 同源
// （同一批 routeCache 对象，TTL 3s 且 admin 改动即失效），不引入额外的陈旧判断。
func classifyRoutedAccounts(accounts []*ent.Account, routing map[string][]int64, model string) ([]*ent.Account, error) {
	tiers, err := classifyRoutedAccountTiers(accounts, routing, model)
	if err != nil {
		return nil, err
	}
	return tiers.primary, nil
}

// classifyRoutedAccountTiers keeps model_routing as the primary routing contract,
// while treating other upstream pool accounts in the same group as standbys. A
// standby is considered only when all explicit candidates are excluded or not
// schedulable; regular accounts remain strictly constrained by model_routing.
func classifyRoutedAccountTiers(accounts []*ent.Account, routing map[string][]int64, model string) (routedAccountTiers, error) {
	if len(accounts) == 0 {
		return routedAccountTiers{}, ErrGroupOffline
	}
	routed := applyModelRouting(accounts, routing, model)
	if len(routed) == 0 {
		return routedAccountTiers{}, ErrModelNotServed
	}
	var fallback []*ent.Account
	if model != "" {
		fallback = poolFallbackAccounts(accounts, routed)
	}
	for _, acc := range appendAccountSlices(routed, fallback) {
		if acc.State != account.StateDisabled {
			return routedAccountTiers{primary: routed, poolFallback: fallback}, nil
		}
	}
	return routedAccountTiers{}, ErrGroupOffline
}

func poolFallbackAccounts(accounts, routed []*ent.Account) []*ent.Account {
	routedIDs := make(map[int]struct{}, len(routed))
	hasPoolPrimary := false
	for _, acc := range routed {
		routedIDs[acc.ID] = struct{}{}
		hasPoolPrimary = hasPoolPrimary || acc.UpstreamIsPool
	}
	if !hasPoolPrimary {
		return nil
	}
	fallback := make([]*ent.Account, 0, len(accounts)-len(routed))
	for _, acc := range accounts {
		if _, exists := routedIDs[acc.ID]; exists || !acc.UpstreamIsPool {
			continue
		}
		fallback = append(fallback, acc)
	}
	return fallback
}

func normalizeGroupLookupError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if ent.IsNotFound(err) {
		return fmt.Errorf("%w: %v", ErrGroupNotFound, err)
	}
	return fmt.Errorf("查询分组失败: %w", err)
}

func normalizeGroupAccountsLookupError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return fmt.Errorf("查询分组账户失败: %w", err)
}

// applyModelRouting 按 model 过滤候选账号。routing 为 nil/空时原样返回；有规则但未命中时无候选。
func applyModelRouting(accounts []*ent.Account, routing map[string][]int64, model string) []*ent.Account {
	if len(routing) == 0 {
		return accounts
	}
	allowedIDs := matchModelRouting(routing, model)
	if allowedIDs == nil {
		return nil
	}
	if len(allowedIDs) == 0 {
		return nil
	}
	idSet := make(map[int64]bool, len(allowedIDs))
	for _, id := range allowedIDs {
		idSet[id] = true
	}
	// 不能原地复用 accounts slice：那是缓存共享的底层数组，别处还在读
	filtered := make([]*ent.Account, 0, len(accounts))
	for _, acc := range accounts {
		if idSet[int64(acc.ID)] {
			filtered = append(filtered, acc)
		}
	}
	return filtered
}

// ModelRoutingServes 判断分组的 model_routing 规则下该模型是否有可服务的账号。
// 语义与 applyModelRouting 完全一致（同一匹配源 matchModelRouting）：
//   - routing 为空 → 不限制，视为可服务；
//   - 命中规则（精确键或 glob）且账号列表非空 → 可服务；
//   - 命中但账号列表为空（显式禁用）或未命中任何规则 → 不可服务。
//
// 供转发入口的「模型-分组预校验」复用，勿在别处复制匹配逻辑。
func ModelRoutingServes(routing map[string][]int64, model string) bool {
	if len(routing) == 0 {
		return true
	}
	return len(matchModelRouting(routing, model)) > 0
}

// ModelRoutingServesAccounts verifies that a model route reaches at least one
// currently usable account that is actually bound to the group. The caller
// supplies the non-disabled, same-platform account snapshot from the store.
func ModelRoutingServesAccounts(routing map[string][]int64, model string, accountIDs []int64) bool {
	if len(accountIDs) == 0 {
		return false
	}
	if len(routing) == 0 {
		return true
	}
	allowed := matchModelRouting(routing, model)
	if len(allowed) == 0 {
		return false
	}
	available := make(map[int64]struct{}, len(accountIDs))
	for _, id := range accountIDs {
		available[id] = struct{}{}
	}
	for _, id := range allowed {
		if _, ok := available[id]; ok {
			return true
		}
	}
	return false
}

// matchModelRouting 匹配模型路由规则，返回允许的账号 ID 列表。nil 或空表示不限制。
// 空 model 表示模型无关操作，使用全部路由账号的并集，避免绕过 model_routing
// 选中未登记账号。
func matchModelRouting(routing map[string][]int64, model string) []int64 {
	if model == "" {
		seen := make(map[int64]struct{})
		for _, ids := range routing {
			for _, id := range ids {
				seen[id] = struct{}{}
			}
		}
		ids := make([]int64, 0, len(seen))
		for id := range seen {
			ids = append(ids, id)
		}
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		return ids
	}
	if ids, ok := routing[model]; ok {
		return ids
	}
	bestPattern := ""
	for pattern := range routing {
		if matched, _ := filepath.Match(pattern, model); matched &&
			(bestPattern == "" || modelRoutingPatternPrecedes(pattern, bestPattern)) {
			bestPattern = pattern
		}
	}
	if bestPattern != "" {
		return routing[bestPattern]
	}
	return nil
}

// Longer glob patterns are treated as more specific; lexical order breaks ties.
// Exact model keys always win before this comparison.
func modelRoutingPatternPrecedes(left, right string) bool {
	if len(left) != len(right) {
		return len(left) > len(right)
	}
	return left < right
}

// evaluateSchedulabilityWithLoad 在三态结果之外保留“为什么不可调度”。只有账号级
// rate_limited 和 family cooldown 携带可信的恢复时间；并发/RPM/window/session
// 等本地容量约束不能被误报为上游 429。
func (s *Scheduler) evaluateSchedulabilityWithLoad(ctx context.Context, acc *ent.Account, model string, now time.Time, load int) schedulabilityDecision {
	family := s.resolveModelFamily(acc.Platform, model)
	circuit := familyCircuitStatus{}
	if family != "" && s.familyCooldown != nil {
		// Candidate enumeration is read-only. A half-open lease is claimed only
		// after this account wins priority/load/session selection.
		circuit = s.familyCooldown.peekCircuitStatus(ctx, acc.ID, family)
	}

	base := SchedulabilityOf(acc, now)
	if base == NotSchedulable {
		decision := schedulabilityDecision{state: NotSchedulable, cause: candidateUnavailableTerminal}
		if acc.State == account.StateRateLimited && acc.StateUntil != nil && acc.StateUntil.After(now) {
			decision.circuitKind = familyCircuitRateLimit
			decision.unavailableUntil = *acc.StateUntil
			if circuit.blocked || circuit.halfOpen {
				// A transient family circuit takes precedence over an overlapping
				// account-level 429, otherwise a mixed outage can be misreported as
				// "all accounts rate limited".
				if circuit.kind == familyCircuitTransient {
					decision.circuitKind = familyCircuitTransient
				}
				if circuit.until.After(decision.unavailableUntil) {
					decision.unavailableUntil = circuit.until
				}
			}
		}
		return decision
	}
	worst := base
	// 家族级冷却：撞过这个 family 的账号在冷却期内对该 family 不可调度，
	// 但对其它 family 仍可用。冷却到期后仅放出一个 half-open probe；Redis
	// 不可用时退化为不冷却，不阻断主链路。
	if circuit.blocked {
		return schedulabilityDecision{
			state:            NotSchedulable,
			unavailableUntil: circuit.until,
			circuitKind:      circuit.kind,
		}
	}
	if sched := concurrencySchedulabilityFromLoad(acc, load); sched > worst {
		worst = sched
	}
	if worst == NotSchedulable {
		return schedulabilityDecision{state: worst, cause: candidateUnavailableLocalCapacity}
	}
	if sched := s.windowCost.GetSchedulability(ctx, acc.ID, acc.Extra); sched > worst {
		worst = sched
	}
	if worst == NotSchedulable {
		return schedulabilityDecision{state: worst, cause: candidateUnavailableLocalCapacity}
	}
	if sched := s.rpm.GetSchedulability(ctx, acc.ID, ExtraInt(acc.Extra, "max_rpm")); sched > worst {
		worst = sched
	}
	if worst == NotSchedulable {
		return schedulabilityDecision{state: worst, cause: candidateUnavailableLocalCapacity}
	}
	if sched := s.session.GetSchedulability(ctx, acc.ID, acc.Extra); sched > worst {
		worst = sched
	}
	if worst == NotSchedulable {
		return schedulabilityDecision{state: worst, cause: candidateUnavailableLocalCapacity}
	}
	return schedulabilityDecision{state: worst}
}

// concurrencySchedulabilityFromLoad 根据当前并发用量返回调度约束：
//
//	load >= 100% → NotSchedulable（调度器直接跳过，避免下游 acquireSlot 失败浪费 failover）
//	load >=  80% → StickyOnly（软降级：只有粘性会话能选中，新请求优先换账号）
//	否则         → Normal
//
// 存在 TOCTOU（这里看没满、下一瞬 acquireSlot 却满）：forwarder 会 failover 到下一个账号兜底。
func concurrencySchedulabilityFromLoad(acc *ent.Account, load int) Schedulability {
	maxConc := acc.MaxConcurrency
	if maxConc <= 0 {
		maxConc = DefaultAccountMaxConcurrency
	}
	if load >= maxConc {
		return NotSchedulable
	}
	if float64(load)/float64(maxConc) >= 0.8 {
		return StickyOnly
	}
	return Normal
}

// selectByLoadBalance 严格按优先级分层：只从最高优先级层选账号，
// 同层内按 (1-load)*100 + lru_score 打分做加权随机。
//
// 低优先级账号只有在高优先级全部被 checkSchedulability 过滤掉后才能被选中。
// 同层内从 top-N 随机选一个，避免高并发下全部命中同一账号。
func (s *Scheduler) selectByLoadBalance(ctx context.Context, candidates []*ent.Account, now time.Time) *ent.Account {
	loadCache := make(map[int]int, len(candidates))
	for _, acc := range candidates {
		loadCache[acc.ID] = s.getCurrentLoad(ctx, acc.ID)
	}
	return s.selectByLoadBalanceWithCache(ctx, candidates, now, loadCache)
}

// selectByLoadBalanceWithCache 复用预获取的并发负载缓存，避免重复查 Redis。
func (s *Scheduler) selectByLoadBalanceWithCache(_ context.Context, candidates []*ent.Account, now time.Time, loadCache map[int]int) *ent.Account {
	if len(candidates) == 0 {
		return nil
	}
	if len(candidates) == 1 {
		return candidates[0]
	}

	// 找到最高优先级，只保留该层候选
	maxPriority := candidates[0].Priority
	for _, acc := range candidates[1:] {
		if acc.Priority > maxPriority {
			maxPriority = acc.Priority
		}
	}
	tier := make([]*ent.Account, 0, len(candidates))
	for _, acc := range candidates {
		if acc.Priority == maxPriority {
			tier = append(tier, acc)
		}
	}
	if len(tier) == 1 {
		return tier[0]
	}

	// 同优先级内按负载 + LRU 打分
	type scored struct {
		acc   *ent.Account
		score float64
	}
	items := make([]scored, 0, len(tier))

	for _, acc := range tier {
		maxConc := acc.MaxConcurrency
		if maxConc <= 0 {
			maxConc = DefaultAccountMaxConcurrency
		}
		loadRate := float64(loadCache[acc.ID]) / float64(maxConc)
		if loadRate > 1 {
			loadRate = 1
		}

		lruScore := 100.0
		if acc.LastUsedAt != nil {
			if elapsed := now.Sub(*acc.LastUsedAt).Minutes(); elapsed < 100 {
				lruScore = elapsed
			}
		}
		items = append(items, scored{
			acc:   acc,
			score: (1-loadRate)*100 + lruScore,
		})
	}

	sort.Slice(items, func(i, j int) bool { return items[i].score > items[j].score })

	const maxTopN = 32
	topN := len(items)
	if topN > maxTopN {
		topN = maxTopN
	}
	return items[rand.Intn(topN)].acc
}

// getCurrentLoad 从 Redis ZSET 读账号当前"有效"并发数（过滤僵尸 slot）。
//
// 用 ZCount + score > (now - slotTTL) 只计算未过期的 slot，避免 release 异常的
// 僵尸 slot 把账号一直标满（下次 acquire 会清理它们，但 selection 不能等）。
// key 必须与 concurrency.go 的 concurrencyKey 保持一致（`concurrency:v2:<id>`）。
func (s *Scheduler) getCurrentLoad(ctx context.Context, accountID int) int {
	if s.rdb == nil {
		return 0
	}
	cutoff := time.Now().Add(-defaultSlotTTL).Unix()
	min := "(" + strconv.FormatInt(cutoff, 10) // 开区间：严格 > cutoff
	n, err := s.rdb.ZCount(ctx, concurrencyKey(accountID), min, "+inf").Result()
	if err != nil {
		return 0
	}
	return int(n)
}
