package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/DouDOU-start/airgate-core/ent/account"
)

// ModelFamily 把 (platform, model) 折叠成上游限流共享的"家族"键（硬编码兜底）。
//
// 优先级：调用方应先通过插件目录查询 Metadata["family"]（Manager.ModelFamily /
// Scheduler.resolveModelFamily），仅在插件未声明时才回退到此函数。
//
// 兜底规则（向后兼容）：
//   - gpt-image-* 系列 → "gpt-image"
//   - 其它非空 model → model 本身（每个模型独立冷却）
//   - model 为空 → platform
//
// 新增家族折叠应由插件在 ModelInfo.Metadata["family"] 声明，勿在此扩展硬编码。
func ModelFamily(platform, model string) string {
	m := strings.ToLower(strings.TrimSpace(model))
	if strings.HasPrefix(m, "gpt-image") {
		return "gpt-image"
	}
	if m != "" {
		return m
	}
	return strings.ToLower(strings.TrimSpace(platform))
}

// resolveModelFamily 从插件目录回调优先获取家族键，未命中时回退到硬编码规则。
func (s *Scheduler) resolveModelFamily(platform, model string) string {
	if s.modelFamilyFunc != nil {
		if family := s.modelFamilyFunc(model); family != "" {
			return family
		}
	}
	return ModelFamily(platform, model)
}

// FamilyCooldown 维护"账号 × 模型家族"的 circuit。active cooldown 按 TTL
// 进入 half-open；recovery marker 会一直保留到 token 所有者成功或管理员清除。
//
// 与 DB 上的 Account.State 区别：
//   - DB state（rate_limited / disabled / degraded）是账号级，影响整账号所有调用。
//   - FamilyCooldown 是 (account, family) 级；撞 gpt-image 不会让 chat 流量受牵连。
//
// 短时高频（200ms~60s）的限流非常适合放 Redis：写读都廉价；active TTL
// 过期后仅允许一个 probe，不能因时间经过直接恢复全量。
//
// Redis 不可用时退化为"不冷却"（fail-open），保证主链路可用性。
type FamilyCooldown struct {
	rdb *redis.Client
}

const (
	// familyProbeLease 覆盖常见 GPT/Claude 首字节窗口，避免长响应每 5 秒放出一个
	// 并行 probe。长请求由 MaintainFamilyProbe 续租；卡死请求停止续租后最多 45 秒放行下一次探测。
	familyProbeLease = 45 * time.Second
	familyProbeRenew = 15 * time.Second
	// familyProbeRenewRetry keeps a live probe from losing its lease after one
	// transient Redis failure. A failed renewal never reacquires an expired token.
	familyProbeRenewRetry = time.Second
)

type familyCircuitKind string

const (
	familyCircuitRateLimit familyCircuitKind = "rate_limit"
	familyCircuitTransient familyCircuitKind = "transient"
)

type familyCircuitStatus struct {
	blocked      bool
	halfOpen     bool
	probeClaimed bool
	until        time.Time
	kind         familyCircuitKind
}

// AccountGateReason describes why an exact-account request may or may not
// proceed. It is provider-neutral and shared by pinned chat, image, video, and
// other plugin-hosted workloads.
type AccountGateReason string

const (
	AccountGateAllowed     AccountGateReason = "allowed"
	AccountGateRateLimited AccountGateReason = "rate_limited"
	AccountGateTransient   AccountGateReason = "transient"
	AccountGateUnavailable AccountGateReason = "unavailable"
)

// AccountGateDecision is the exact-account counterpart to normal scheduling.
// RetryAt is populated for active cooldowns and competing half-open probes.
// ProbeClaimed means the caller owns the half-open lease and must either pass
// ProbeToken to Apply or release it before returning locally.
type AccountGateDecision struct {
	Reason       AccountGateReason
	RetryAt      time.Time
	ProbeClaimed bool
}

func (d AccountGateDecision) Allowed() bool {
	return d.Reason == AccountGateAllowed
}

type familyProbeTokenContextKey struct{}

// WithFamilyProbeToken binds a unique upstream-attempt token to account selection.
// A half-open circuit may only be closed by a success carrying the same token.
func WithFamilyProbeToken(ctx context.Context, token string) context.Context {
	if token == "" {
		return ctx
	}
	return context.WithValue(ctx, familyProbeTokenContextKey{}, token)
}

func familyProbeTokenFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	token, _ := ctx.Value(familyProbeTokenContextKey{}).(string)
	return token
}

// NewFamilyCooldown 构造家族冷却管理器。rdb=nil 时所有方法 no-op。
func NewFamilyCooldown(rdb *redis.Client) *FamilyCooldown {
	return &FamilyCooldown{rdb: rdb}
}

// familyCooldownKey 与 concurrency.go / sticky 等命名风格保持一致：
// `<purpose>:v<version>:<id>:<sub>`，便于运维快速摸 key pattern。
func familyCooldownKey(accountID int, family string) string {
	return fmt.Sprintf("family-cooldown:v1:%d:%s", accountID, family)
}

func familyRecoveryKey(accountID int, family string) string {
	return fmt.Sprintf("family-recovery:v1:%d:%s", accountID, family)
}

func familyProbeKey(accountID int, family string) string {
	return fmt.Sprintf("family-probe:v1:%d:%s", accountID, family)
}

func encodeFamilyCircuitValue(kind familyCircuitKind, reason string) string {
	return string(kind) + "\n" + reason
}

func decodeFamilyCircuitValue(value string) (familyCircuitKind, string) {
	kind, reason, found := strings.Cut(value, "\n")
	if found {
		switch familyCircuitKind(kind) {
		case familyCircuitRateLimit, familyCircuitTransient:
			return familyCircuitKind(kind), reason
		}
	}
	// Keys written before circuit kinds existed were all rate-limit cooldowns.
	return familyCircuitRateLimit, value
}

// Mark 把 (account, family) 写入冷却，TTL = until - now（最少 1ms）。
// 并发上游返回的 Retry-After 可能不一致，所以只保留更长的冷却：短值不得缩短已有保护窗口。
func (fc *FamilyCooldown) Mark(ctx context.Context, accountID int, family string, until time.Time, reason string) {
	fc.markCircuit(ctx, accountID, family, until, reason, familyCircuitRateLimit)
}

// MarkTransient opens a short family-scoped circuit for generic upstream 5xx or
// transport failures. Transient circuits never participate in the 429 contract.
func (fc *FamilyCooldown) MarkTransient(ctx context.Context, accountID int, family string, until time.Time, reason string) {
	fc.markCircuit(ctx, accountID, family, until, reason, familyCircuitTransient)
}

func (fc *FamilyCooldown) markCircuit(ctx context.Context, accountID int, family string, until time.Time, reason string, kind familyCircuitKind) {
	if fc == nil || fc.rdb == nil || family == "" {
		return
	}
	ttl := time.Until(until)
	if ttl <= 0 {
		ttl = time.Millisecond
	}
	ttlMilliseconds := ttl.Milliseconds()
	if ttlMilliseconds < 1 {
		ttlMilliseconds = 1
	}
	// 后到的短窗口不能缩短已有保护。transient 在同一未恢复 circuit 中优先于
	// rate_limit 分类，确保混合故障永远不会被误报为“所有账号限流”。每次 Mark
	// 都删除旧 probe token，使旧在飞请求无法关闭新 circuit。
	const keepLongestCooldown = `
		local current = redis.call('PTTL', KEYS[1])
		local current_value = redis.call('GET', KEYS[1])
		if not current_value then
			current_value = redis.call('GET', KEYS[2])
		end
		local incoming = tonumber(ARGV[2])
		local incoming_kind = ARGV[3]
		local effective = incoming
		local permanent = current == -1
		if not permanent and current > effective then
			effective = current
		end
		local value = ARGV[1]
		local current_is_transient = current_value and string.sub(current_value, 1, 10) == 'transient' .. string.char(10)
		if current_is_transient and (incoming_kind ~= 'transient' or permanent or current > incoming) then
			value = current_value
		elseif incoming_kind ~= 'transient' and (permanent or current > incoming) and current_value then
			value = current_value
		end
		if permanent then
			redis.call('SET', KEYS[1], value)
		else
			redis.call('SET', KEYS[1], value, 'PX', effective)
		end
		-- Recovery is deliberately persistent: normal time passage must never
		-- restore full traffic without a successful token-owned probe.
		redis.call('SET', KEYS[2], value)
		redis.call('DEL', KEYS[3])
		return effective
	`
	keys := []string{
		familyCooldownKey(accountID, family),
		familyRecoveryKey(accountID, family),
		familyProbeKey(accountID, family),
	}
	value := encodeFamilyCircuitValue(kind, reason)
	if _, err := fc.rdb.Eval(ctx, keepLongestCooldown, keys, value, ttlMilliseconds, string(kind)).Result(); err != nil {
		slog.Debug("写入家族冷却失败",
			"account_id", accountID, "family", family, "kind", kind, "ttl_ms", ttlMilliseconds, "error", err)
	}
}

// Until 只读查询 (account, family) 是否仍受 circuit 保护。active cooldown 和
// half-open recovery 都返回 blocked；真正的 probe 抢占由 ClaimProbe 在调度器
// 最终选中账号后执行，避免遍历候选时空抢所有账号的 lease。
func (fc *FamilyCooldown) Until(ctx context.Context, accountID int, family string) (time.Time, bool) {
	status := fc.peekCircuitStatus(ctx, accountID, family)
	return status.until, status.blocked || status.halfOpen
}

func (fc *FamilyCooldown) peekCircuitStatus(ctx context.Context, accountID int, family string) familyCircuitStatus {
	if fc == nil || fc.rdb == nil || family == "" {
		return familyCircuitStatus{}
	}
	const peekCircuit = `
		local cooldown = redis.call('PTTL', KEYS[1])
		local value = redis.call('GET', KEYS[1])
		if not value then
			value = redis.call('GET', KEYS[2]) or ''
		end
		if cooldown == -1 then
			return {1, 0, value}
		end
		if cooldown > 0 then
			return {1, cooldown, value}
		end
		if redis.call('EXISTS', KEYS[2]) == 0 then
			return {0, 0, ''}
		end
		return {2, 0, value}
	`
	keys := []string{
		familyCooldownKey(accountID, family),
		familyRecoveryKey(accountID, family),
	}
	values, err := fc.rdb.Eval(ctx, peekCircuit, keys).Slice()
	if err != nil || len(values) < 3 {
		return familyCircuitStatus{}
	}
	state, _ := values[0].(int64)
	ttlMilliseconds, _ := values[1].(int64)
	encoded, _ := values[2].(string)
	kind, _ := decodeFamilyCircuitValue(encoded)
	status := familyCircuitStatus{blocked: state == 1, halfOpen: state == 2, kind: kind}
	if (status.blocked || status.halfOpen) && ttlMilliseconds > 0 {
		status.until = time.Now().Add(time.Duration(ttlMilliseconds) * time.Millisecond)
	}
	return status
}

// ClaimProbe atomically claims the half-open lease for the account actually
// selected by the scheduler. It is idempotent for the same token. A circuit
// opened between peek and claim blocks the request and is returned to selection
// for failover; a circuit whose recovery grace expired needs no probe.
func (fc *FamilyCooldown) ClaimProbe(ctx context.Context, accountID int, family, probeToken string) familyCircuitStatus {
	if fc == nil || fc.rdb == nil || family == "" {
		return familyCircuitStatus{}
	}
	const claimHalfOpenProbe = `
		local cooldown = redis.call('PTTL', KEYS[1])
		local lease = tonumber(ARGV[1])
		local token = ARGV[2]
			local value = redis.call('GET', KEYS[1])
			if not value then
				value = redis.call('GET', KEYS[2]) or ''
			end
			if cooldown == -1 then
				return {1, 0, value, 0}
		end
		if cooldown > 0 then
			return {1, cooldown, value, 0}
		end
		if redis.call('EXISTS', KEYS[2]) == 0 then
			return {0, 0, '', 0}
		end
		if token == '' then
			return {1, lease, value, 0}
		end
		local existing = redis.call('GET', KEYS[3])
		if existing == token then
			return {0, 0, value, 1}
		end
		local acquired = redis.call('SET', KEYS[3], token, 'PX', lease, 'NX')
		if acquired then
			return {0, 0, value, 1}
		end
		local probe = redis.call('PTTL', KEYS[3])
		if probe > 0 then
			return {1, probe, value, 0}
		end
		return {1, lease, value, 0}
	`
	keys := []string{
		familyCooldownKey(accountID, family),
		familyRecoveryKey(accountID, family),
		familyProbeKey(accountID, family),
	}
	values, err := fc.rdb.Eval(ctx, claimHalfOpenProbe, keys, familyProbeLease.Milliseconds(), probeToken).Slice()
	if err != nil || len(values) < 4 {
		return familyCircuitStatus{}
	}
	blocked, _ := values[0].(int64)
	ttlMilliseconds, _ := values[1].(int64)
	encoded, _ := values[2].(string)
	probeClaimed, _ := values[3].(int64)
	kind, _ := decodeFamilyCircuitValue(encoded)
	status := familyCircuitStatus{blocked: blocked == 1, probeClaimed: probeClaimed == 1, kind: kind}
	if status.blocked && ttlMilliseconds > 0 {
		status.until = time.Now().Add(time.Duration(ttlMilliseconds) * time.Millisecond)
	}
	return status
}

// ReleaseProbe releases an unused lease only if it still belongs to probeToken.
// It is used when session/local capacity rejects an account after selection.
func (fc *FamilyCooldown) ReleaseProbe(ctx context.Context, accountID int, family, probeToken string) {
	if fc == nil || fc.rdb == nil || family == "" || probeToken == "" {
		return
	}
	const releaseProbe = `
		if redis.call('GET', KEYS[1]) == ARGV[1] then
			return redis.call('DEL', KEYS[1])
		end
		return 0
	`
	_ = fc.rdb.Eval(ctx, releaseProbe, []string{familyProbeKey(accountID, family)}, probeToken).Err()
}

// RenewProbe extends a live half-open lease only when probeToken is still the
// exact owner and no newer cooldown has opened. It never reacquires an expired
// lease, so stale requests cannot keep or close a newer circuit.
func (fc *FamilyCooldown) RenewProbe(ctx context.Context, accountID int, family, probeToken string) (bool, error) {
	if fc == nil || fc.rdb == nil || family == "" || probeToken == "" {
		return false, nil
	}
	const renewProbe = `
		local cooldown = redis.call('PTTL', KEYS[1])
		if cooldown == -1 or cooldown > 0 then
			return 0
		end
		if redis.call('EXISTS', KEYS[2]) == 0 then
			return 0
		end
		if redis.call('GET', KEYS[3]) ~= ARGV[1] then
			return 0
		end
		redis.call('PEXPIRE', KEYS[3], ARGV[2])
		return 1
	`
	keys := []string{
		familyCooldownKey(accountID, family),
		familyRecoveryKey(accountID, family),
		familyProbeKey(accountID, family),
	}
	renewed, err := fc.rdb.Eval(ctx, renewProbe, keys, probeToken, familyProbeLease.Milliseconds()).Int()
	if err != nil {
		slog.Debug("续租家族 half-open probe 失败", "account_id", accountID, "family", family, "error", err)
		return false, err
	}
	return renewed == 1, nil
}

// ClaimAccountGate checks a pinned/exact account immediately before an
// upstream call. Unlike SelectAccount it cannot fail over, but it honors the
// same DB and family-circuit health contract and atomically claims half-open.
func (s *Scheduler) ClaimAccountGate(ctx context.Context, accountID int, platform, model, probeToken string) (AccountGateDecision, error) {
	unavailable := AccountGateDecision{Reason: AccountGateUnavailable}
	if s == nil || s.db == nil {
		return unavailable, fmt.Errorf("scheduler account gate is unavailable")
	}
	acc, err := s.db.Account.Get(ctx, accountID)
	if err != nil {
		return unavailable, fmt.Errorf("query account %d for gate: %w", accountID, err)
	}
	if platform != "" && acc.Platform != platform {
		return unavailable, nil
	}

	now := time.Now()
	base := SchedulabilityOf(acc, now)
	if base == NotSchedulable && (acc.State != account.StateRateLimited || acc.StateUntil == nil || !acc.StateUntil.After(now)) {
		return unavailable, nil
	}

	family := s.resolveModelFamily(acc.Platform, model)
	if acc.State == account.StateRateLimited && acc.StateUntil != nil && acc.StateUntil.After(now) {
		decision := AccountGateDecision{Reason: AccountGateRateLimited, RetryAt: *acc.StateUntil}
		// The DB state already blocks this request, so inspect the family circuit
		// atomically without claiming or fabricating a probe lease. An idle half-open
		// marker must not extend a short DB RetryAt to familyProbeLease.
		circuit := s.familyCooldown.peekCircuitStatus(ctx, accountID, family)
		if circuit.blocked || circuit.halfOpen {
			if circuit.kind == familyCircuitTransient {
				decision.Reason = AccountGateTransient
			}
			if circuit.until.After(decision.RetryAt) {
				decision.RetryAt = circuit.until
			}
		}
		return decision, nil
	}

	// ClaimProbe is also the healthy-path gate. Keeping the check and claim in a
	// single Lua script closes the window where another request opens a circuit
	// after a read-only peek but before this pinned request reaches upstream.
	claim := s.familyCooldown.ClaimProbe(ctx, accountID, family, probeToken)
	if claim.blocked {
		return accountGateCircuitDecision(claim), nil
	}
	return AccountGateDecision{Reason: AccountGateAllowed, ProbeClaimed: claim.probeClaimed}, nil
}

func accountGateCircuitDecision(status familyCircuitStatus) AccountGateDecision {
	reason := AccountGateRateLimited
	if status.kind == familyCircuitTransient {
		reason = AccountGateTransient
	}
	return AccountGateDecision{Reason: reason, RetryAt: status.until}
}

// ReleaseFamilyProbe releases an unused half-open lease for a selected attempt.
func (s *Scheduler) ReleaseFamilyProbe(ctx context.Context, accountID int, platform, model, probeToken string) {
	if s == nil || s.familyCooldown == nil || probeToken == "" {
		return
	}
	family := s.resolveModelFamily(platform, model)
	if family == "" {
		return
	}
	s.familyCooldown.ReleaseProbe(ctx, accountID, family, probeToken)
}

// MaintainFamilyProbe keeps a selected half-open request exclusive while it is
// in flight. The returned stop function is idempotent and waits for the renewal
// goroutine to exit, preventing a late renewal after the outcome is applied.
func (s *Scheduler) MaintainFamilyProbe(ctx context.Context, accountID int, platform, model, probeToken string) func() {
	if s == nil || s.familyCooldown == nil || probeToken == "" {
		return func() {}
	}
	family := s.resolveModelFamily(platform, model)
	if family == "" {
		return func() {}
	}
	owned, renewErr := s.familyCooldown.RenewProbe(ctx, accountID, family, probeToken)
	if renewErr == nil && !owned {
		return func() {}
	}

	maintainCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		delay := familyProbeRenew
		if renewErr != nil {
			delay = familyProbeRenewRetry
		}
		for {
			timer := time.NewTimer(delay)
			select {
			case <-maintainCtx.Done():
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				return
			case <-timer.C:
				stillOwned, err := s.familyCooldown.RenewProbe(maintainCtx, accountID, family, probeToken)
				if err != nil {
					delay = familyProbeRenewRetry
					continue
				}
				if !stillOwned {
					return
				}
				delay = familyProbeRenew
			}
		}
	}()

	var once sync.Once
	return func() {
		once.Do(func() {
			cancel()
			<-done
		})
	}
}

// Recover closes half-open only when the successful request still owns the
// current probe lease. Lease expiry deliberately invalidates a long-running
// probe; a new request may probe, while the late success cannot close the gate.
func (fc *FamilyCooldown) Recover(ctx context.Context, accountID int, family, probeToken string) bool {
	if fc == nil || fc.rdb == nil || family == "" || probeToken == "" {
		return false
	}
	const closeHalfOpen = `
		local cooldown = redis.call('PTTL', KEYS[1])
		if cooldown == -1 or cooldown > 0 then
			return 0
		end
		if redis.call('EXISTS', KEYS[2]) == 0 then
			return 0
		end
		if redis.call('GET', KEYS[3]) ~= ARGV[1] then
			return 0
		end
		redis.call('DEL', KEYS[2], KEYS[3])
		return 1
	`
	keys := []string{
		familyCooldownKey(accountID, family),
		familyRecoveryKey(accountID, family),
		familyProbeKey(accountID, family),
	}
	closed, err := fc.rdb.Eval(ctx, closeHalfOpen, keys, probeToken).Int()
	if err != nil {
		slog.Debug("关闭家族 half-open 失败", "account_id", accountID, "family", family, "error", err)
		return false
	}
	return closed == 1
}

// Clear 清除指定家族的冷却。管理员强制解封 / 测试场景使用。
// 业务正常路径不需要主动清，TTL 到期自动清掉。
func (fc *FamilyCooldown) Clear(ctx context.Context, accountID int, family string) {
	if fc == nil || fc.rdb == nil || family == "" {
		return
	}
	_ = fc.rdb.Del(ctx,
		familyCooldownKey(accountID, family),
		familyRecoveryKey(accountID, family),
		familyProbeKey(accountID, family),
	).Err()
}

// ClearAccount 清除账号下所有家族冷却。管理员刷新额度或手动解除限流标记时使用。
func (fc *FamilyCooldown) ClearAccount(ctx context.Context, accountID int) int {
	if fc == nil || fc.rdb == nil {
		return 0
	}
	cleared := 0
	prefixes := []string{
		familyCooldownKey(accountID, ""),
		familyRecoveryKey(accountID, ""),
		familyProbeKey(accountID, ""),
	}
	for prefixIndex, prefix := range prefixes {
		var cursor uint64
		for {
			keys, next, err := fc.rdb.Scan(ctx, cursor, prefix+"*", 32).Result()
			if err != nil {
				return cleared
			}
			if len(keys) > 0 {
				if n, err := fc.rdb.Del(ctx, keys...).Result(); err == nil && prefixIndex == 0 {
					cleared += int(n)
				}
			}
			if next == 0 {
				break
			}
			cursor = next
		}
	}
	return cleared
}

// FamilyCooldownEntry 描述一条仍在生效的家族冷却。给后台展示用。
type FamilyCooldownEntry struct {
	Family string
	Until  time.Time
	Reason string
}

// List 列出指定账号当前所有家族冷却。供后台账号管理页展示用。
//
// 实现：用 SCAN 走一遍 `family-cooldown:v1:<acc>:*` 模式，对每个 key 取 TTL + GET 拿原因。
// 一个账号通常只有 0~3 个家族在冷却，COUNT=32 一轮就回。Redis 不可用 / 报错时返回部分结果，
// 后台只用来展示，不要求完整。
func (fc *FamilyCooldown) List(ctx context.Context, accountID int) []FamilyCooldownEntry {
	if fc == nil || fc.rdb == nil {
		return nil
	}
	prefix := familyCooldownKey(accountID, "")
	pattern := prefix + "*"
	var entries []FamilyCooldownEntry
	var cursor uint64
	for {
		keys, next, err := fc.rdb.Scan(ctx, cursor, pattern, 32).Result()
		if err != nil {
			return entries
		}
		for _, key := range keys {
			ttl, err := fc.rdb.PTTL(ctx, key).Result()
			if err != nil || ttl <= 0 {
				continue
			}
			value, _ := fc.rdb.Get(ctx, key).Result()
			_, reason := decodeFamilyCircuitValue(value)
			entries = append(entries, FamilyCooldownEntry{
				Family: strings.TrimPrefix(key, prefix),
				Until:  time.Now().Add(ttl),
				Reason: reason,
			})
		}
		if next == 0 {
			break
		}
		cursor = next
	}
	return entries
}
