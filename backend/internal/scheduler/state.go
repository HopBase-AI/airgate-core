package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/redis/go-redis/v9"

	"github.com/DouDOU-start/airgate-core/ent"
	"github.com/DouDOU-start/airgate-core/ent/account"
	"github.com/DouDOU-start/airgate-core/ent/accountevent"
	sdk "github.com/DouDOU-start/airgate-sdk/sdkgo"
)

// 状态机使用的默认窗口。
const (
	// rateLimitedDefault 上游没给 RetryAfter 时的兜底冷却。
	rateLimitedDefault = 60 * time.Second
	// rateLimitedMin 最小冷却下限。OpenAI 共享 org 限流时给的 RetryAfter 经常是 15ms~50ms，
	// 跟着这种瞬时值会让账号刚解锁就被同请求或并发请求立刻再撞墙。设一个下限让实际放出
	// 的请求拉开间隔，配合上游窗口老化才有效果。
	rateLimitedMin = 200 * time.Millisecond
	// rateLimitedMax OAuth 某些限流可能长达数天，设上限防止异常值。
	rateLimitedMax = 7 * 24 * time.Hour
	// degradedDefault 池账号抖动时的软降级窗口。
	degradedDefault = 60 * time.Second
	// degradedMax 池账号最长降级窗口。
	degradedMax = 10 * time.Minute

	// poolDeadStreakThreshold 池账号透传死信（非 401 的 AccountDead）连击升级阈值。
	// 单次 403 多是上游池内某个子账号被封，不动状态是对的；但欠费/封禁类故障会连续
	// 出现——不改状态时每个新请求都先撞一次死信再 failover，白白多一跳延迟，直到
	// 人工禁用（2026-07-25 lin-pro 欠费 403 四分钟连撞 42 次即此案）。连续达到阈值
	// 后软降级，窗口逐次翻倍（60s → 2m → 4m → … → degradedMax 封顶），期间账号仅作
	// StickyOnly 兜底；任何一次成功判决清零连击。
	poolDeadStreakThreshold = 3
	// poolDeadStreakTTL 连击计数的滑动窗口：每次连击续期，安静该时长后自动清零，
	// 给上游自愈（充值/解封）留机会。须大于 degradedMax，否则降级到期前计数先过期。
	poolDeadStreakTTL = 30 * time.Minute
	// probeReleaseTimeout bounds best-effort Redis cleanup on the request path.
	// A failed release only leaves the lease to expire naturally; it must never
	// hold a customer request behind the general DB timeout.
	probeReleaseTimeout = 750 * time.Millisecond
)

// ClampRateLimitRetryAfter keeps a client-visible rate-limit delay within the
// same upper bound used by the account state machine. It deliberately has no
// minimum: response formatting must preserve a valid sub-second upstream hint.
func ClampRateLimitRetryAfter(retryAfter time.Duration) time.Duration {
	if retryAfter <= 0 {
		return 0
	}
	if retryAfter > rateLimitedMax {
		return rateLimitedMax
	}
	return retryAfter
}

// Judgment forwarder 对一次调用的判决，交给状态机做状态转移。
type Judgment struct {
	Kind             sdk.OutcomeKind
	RetryAfter       time.Duration
	Reason           string
	Duration         time.Duration // 仅用于日志 / 指标
	IsPool           bool          // 池账号（upstream_is_pool）走豁免路径
	Family           string        // 模型家族键（见 ModelFamily）。非空时 RateLimited 走 Redis 家族冷却而非账号级 DB state，避免 gpt-image 限流误伤 chat。
	UpstreamStatus   int           // 上游 HTTP 状态码，用于池账号区分 401（自身凭证无效）和 403（透传上游错误）。
	UserID           int           // 触发本次判决请求的终端用户 ID（API Key 归属），无用户上下文为 0。
	APIKeyID         int           // 触发本次判决请求所用的 API Key ID，无则为 0。
	ProbeToken       string        // 选号阶段抢到的 half-open lease token；只有匹配 token 的 Success 才能关闸。
	AttemptStartedAt time.Time     // 实际 Gateway.Forward 开始前的时间；用于防止旧成功清除后来池账号死信连击。零值保留 report 兼容语义。
}

// StateMachine 账号状态机。所有状态转移必须通过 Apply 入口。
//
// 职责：
//   - 把 forwarder 的 Judgment 翻译成 DB 字段变更（state / state_until / error_msg / last_used_at）
//   - 关键转移（Active ↔ Disabled）通知上游清 route 缓存
//
// 只有确定性的账号级信号才动 state：AccountRateLimited / AccountDead。
// UpstreamTransient（SSE EOF、上游 5xx、连接抖动）是上游锅，不扣账号分——让 failover 兜底。
type StateMachine struct {
	db             *ent.Client
	rdb            *redis.Client
	familyCooldown *FamilyCooldown

	// eventWG 追踪异步事件写入（recordEvent 的 goroutine），测试经 waitEvents 同步。
	eventWG sync.WaitGroup
	// eventSlots 限制在飞的事件写入并发（见 recordEvent）：DB 连接池要优先
	// 服务转发主链路，事件写入最多占少量连接，槽位满直接丢弃。
	eventSlots chan struct{}

	// onCriticalTransition Active ↔ Disabled 转移后的回调（由 Scheduler 注入）。
	// 用来清 route 缓存，让下次 SelectAccount 立刻看到新状态；
	// RateLimited / Degraded 这种"带 state_until 的临时状态"不走这里，由 TTL 兜底。
	onCriticalTransition func()
}

// NewStateMachine 构造状态机。fc 提供 (account, family) 维度的限流冷却，
// nil 时退化为旧行为：所有 RateLimited 都写账号级 DB state。
func NewStateMachine(db *ent.Client, rdb *redis.Client, fc *FamilyCooldown) *StateMachine {
	return &StateMachine{db: db, rdb: rdb, familyCooldown: fc, eventSlots: make(chan struct{}, eventWriteConcurrency)}
}

// notifyCritical 发出关键状态变更事件。nil 回调时安静跳过。
func (sm *StateMachine) notifyCritical() {
	if sm.onCriticalTransition != nil {
		sm.onCriticalTransition()
	}
}

// Apply 把一次判决施加到账号状态机。只产生副作用，不返回要写给客户端的内容。
//
// 语义：
//
//	Success             → state=active，清 state_until，last_used_at=now
//	AccountRateLimited  → state=rate_limited，state_until=now+RetryAfter
//	AccountDead         → state=disabled（凭证失效，需人工介入）；
//	                      池账号非 401 只留痕不动状态，连击达阈值后软降级（见 poolDeadStreakThreshold）
//	UpstreamTransient   → 非池：**不动状态**（上游抖动不扣账号分，靠当前请求 failover）；池：state=degraded，窗口优先使用 RetryAfter
//	ClientError / StreamAborted / Unknown → 不改状态（账号无辜）
func (sm *StateMachine) Apply(ctx context.Context, accountID int, j Judgment) {
	switch j.Kind {
	case sdk.OutcomeSuccess:
		if j.Family != "" && j.ProbeToken != "" && sm.familyCooldown != nil {
			sm.familyCooldown.Recover(ctx, accountID, j.Family, j.ProbeToken)
		}
		sm.transitionActive(ctx, accountID, eventSourceForward)
		// The Redis marker compares this attempt's start time to the latest pool
		// failure, so it is safe to reset a recovered pool's streak even while a
		// live degraded TTL remains conservatively in place.
		if j.IsPool {
			sm.resetPoolDeadStreak(accountID, j.AttemptStartedAt)
		}

	case sdk.OutcomeAccountRateLimited:
		dur := j.RetryAfter
		if dur <= 0 {
			dur = rateLimitedDefault
		}
		if dur < rateLimitedMin {
			dur = rateLimitedMin
		}
		dur = ClampRateLimitRetryAfter(dur)
		until := time.Now().Add(dur)
		// 有 Family 信息时走家族级冷却：撞 gpt-image 4000/min 不会让同账号 chat 被跳过。
		// 无 Family（admin 巡检 / 老插件）保留账号级 rate_limited 兜底，行为与改造前一致。
		if j.Family != "" && sm.familyCooldown != nil {
			sm.familyCooldown.Mark(ctx, accountID, j.Family, until, j.Reason)
			slog.Info("scheduler_family_cooldown",
				sdk.LogFieldAccountID, accountID,
				"family", j.Family,
				"until", until,
				sdk.LogFieldReason, j.Reason,
			)
			sm.recordEvent(accountID, j.UserID, j.APIKeyID, accountevent.EventTypeRateLimited, j.Reason, j.Family, eventSourceForward, j.UpstreamStatus, &until)
			return
		}
		if sm.transition(ctx, accountID, account.StateRateLimited, &until, j.Reason) {
			sm.recordEvent(accountID, j.UserID, j.APIKeyID, accountevent.EventTypeRateLimited, j.Reason, "", eventSourceForward, j.UpstreamStatus, &until)
		}

	case sdk.OutcomeAccountDead:
		if j.IsPool && j.UpstreamStatus != 401 {
			// 池账号的 403 等是上游透传的错误，池子本身没问题，
			// 不动状态，靠 failover 重试消化。仍记一条上游侧事件供异常监控追溯。
			// 401 表示池子自身的凭证无效，仍需禁用并说明原因。
			//
			// 例外：连击达到阈值说明不是偶发（欠费/封禁类持续故障），升级为软降级，
			// 避免后续每个请求都先撞一次死信才 failover（见 poolDeadStreakThreshold）。
			if streak := sm.bumpPoolDeadStreak(ctx, accountID); streak >= poolDeadStreakThreshold {
				dj := j
				dj.Reason = fmt.Sprintf("连续 %d 次上游透传错误：%s", streak, j.Reason)
				sm.applyDegradedFor(ctx, accountID, dj, poolDeadDegradeWindow(streak))
				sm.releaseOwnedProbe(accountID, j)
				return
			}
			sm.recordEvent(accountID, j.UserID, j.APIKeyID, accountevent.EventTypeUpstreamError, j.Reason, j.Family, eventSourceForward, j.UpstreamStatus, nil)
			sm.releaseOwnedProbe(accountID, j)
			return
		}
		if sm.transition(ctx, accountID, account.StateDisabled, nil, j.Reason) {
			sm.recordEvent(accountID, j.UserID, j.APIKeyID, accountevent.EventTypeDisabled, j.Reason, "", eventSourceForward, j.UpstreamStatus, nil)
		}
		sm.releaseOwnedProbe(accountID, j)

	case sdk.OutcomeUpstreamTransient:
		// 按定义，UpstreamTransient 是"上游侧瞬时故障"（SSE 提前断流、网络抖动、上游 5xx 等），
		// 账号本身没问题——只在当前请求内 failover，不能把一次上游抖动放大为跨请求硬封。
		// 但事件要留痕：异常监控靠它回答"是不是上游不稳定"。
		//
		// Pool 保留稳定版本的软降级：健康账号优先；若全池都降级，StickyOnly
		// 仍保留最后的受控尝试。窗口跟随上游 RetryAfter，缺失时用本地兜底。
		if j.IsPool {
			sm.applyDegraded(ctx, accountID, j)
		} else {
			sm.recordEvent(accountID, j.UserID, j.APIKeyID, accountevent.EventTypeUpstreamError, j.Reason, j.Family, eventSourceForward, j.UpstreamStatus, nil)
		}
		sm.releaseOwnedProbe(accountID, j)

	case sdk.OutcomeClientError, sdk.OutcomeStreamAborted, sdk.OutcomeUnknown:
		// 账号无辜，不改状态；若本次是 half-open probe，则精确释放自己的
		// lease，让下一个请求立即验证，不必空等 lease TTL。
		sm.releaseOwnedProbe(accountID, j)
	}
}

// releaseOwnedProbe releases only the half-open lease owned by this attempt.
// A transient probe cannot prove recovery, but it also must not block every
// later request for the full lease after its upstream call has already ended.
func (sm *StateMachine) releaseOwnedProbe(accountID int, j Judgment) {
	if j.Family == "" || j.ProbeToken == "" || sm.familyCooldown == nil {
		return
	}
	releaseCtx, cancel := probeCleanupContext()
	defer cancel()
	sm.familyCooldown.ReleaseProbe(releaseCtx, accountID, j.Family, j.ProbeToken)
}

func probeCleanupContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), probeReleaseTimeout)
}

// applyDegraded 池账号软降级。state_until 到期后调度器看到就恢复 active。
func (sm *StateMachine) applyDegraded(ctx context.Context, accountID int, j Judgment) {
	sm.applyDegradedFor(ctx, accountID, j, poolTransientDegradeDuration(j.RetryAfter))
}

func poolTransientDegradeDuration(retryAfter time.Duration) time.Duration {
	if retryAfter <= 0 {
		return degradedDefault
	}
	if retryAfter > degradedMax {
		return degradedMax
	}
	return retryAfter
}

// applyDegradedFor 以指定窗口软降级，窗口封顶 degradedMax。
func (sm *StateMachine) applyDegradedFor(ctx context.Context, accountID int, j Judgment, dur time.Duration) {
	if dur > degradedMax {
		dur = degradedMax
	}
	until := time.Now().Add(dur)
	if sm.transition(ctx, accountID, account.StateDegraded, &until, j.Reason) {
		sm.recordEvent(accountID, j.UserID, j.APIKeyID, accountevent.EventTypeDegraded, j.Reason, j.Family, eventSourceForward, j.UpstreamStatus, &until)
	}
}

// poolDeadStreakKey 命名沿用 family-cooldown 的 `<purpose>:v<version>:<id>` 风格。
func poolDeadStreakKey(accountID int) string {
	return fmt.Sprintf("pool-dead-streak:v1:%d", accountID)
}

// poolDeadStreakMarkerKey records when the numeric streak was last advanced.
// It lets a late success clear only streaks that existed before its upstream
// attempt began, rather than deleting a newer failure's evidence.
func poolDeadStreakMarkerKey(accountID int) string {
	return fmt.Sprintf("pool-dead-streak-at:v1:%d", accountID)
}

func poolDeadStreakTimestamp(t time.Time) string {
	return fmt.Sprintf("%019d", t.UnixNano())
}

// bumpPoolDeadStreak 连击 +1 并返回当前连击数。
// Redis 不可用时返回 0（fail-open：维持"只留痕不动状态"的旧行为），保证主链路可用性。
func (sm *StateMachine) bumpPoolDeadStreak(ctx context.Context, accountID int) int {
	if sm.rdb == nil {
		return 0
	}
	const bump = `
		local n = redis.call('INCR', KEYS[1])
		redis.call('PEXPIRE', KEYS[1], ARGV[1])
		redis.call('SET', KEYS[2], ARGV[2], 'PX', ARGV[1])
		return n
	`
	n, err := sm.rdb.Eval(
		ctx,
		bump,
		[]string{poolDeadStreakKey(accountID), poolDeadStreakMarkerKey(accountID)},
		poolDeadStreakTTL.Milliseconds(),
		poolDeadStreakTimestamp(time.Now()),
	).Int()
	if err != nil {
		slog.Warn("scheduler_pool_dead_streak_incr_failed",
			sdk.LogFieldAccountID, accountID, sdk.LogFieldError, err)
		return 0
	}
	return int(n)
}

// resetPoolDeadStreak clears a pool streak after success. A zero attempt time
// is an admin/report compatibility path and preserves the historical
// unconditional reset. Forward success uses its start time so an old response
// cannot delete a dead-streak that was advanced by a later request.
func (sm *StateMachine) resetPoolDeadStreak(accountID int, attemptStartedAt time.Time) {
	if sm.rdb == nil {
		return
	}
	rctx, cancel := probeCleanupContext()
	defer cancel()
	keys := []string{poolDeadStreakKey(accountID), poolDeadStreakMarkerKey(accountID)}
	var err error
	if attemptStartedAt.IsZero() {
		err = sm.rdb.Del(rctx, keys...).Err()
	} else {
		const resetIfNotNewer = `
			local marker = redis.call('GET', KEYS[2])
			if not marker or marker <= ARGV[1] then
				return redis.call('DEL', KEYS[1], KEYS[2])
			end
			return 0
		`
		err = sm.rdb.Eval(rctx, resetIfNotNewer, keys, poolDeadStreakTimestamp(attemptStartedAt)).Err()
	}
	if err != nil {
		slog.Warn("scheduler_pool_dead_streak_reset_failed",
			sdk.LogFieldAccountID, accountID, sdk.LogFieldError, err)
	}
}

// poolDeadDegradeWindow 连击第 streak 次（>= 阈值）对应的软降级窗口：
// 阈值次 60s 起步，此后逐次翻倍，degradedMax 封顶。
func poolDeadDegradeWindow(streak int) time.Duration {
	dur := degradedDefault
	for i := poolDeadStreakThreshold; i < streak && dur < degradedMax; i++ {
		dur *= 2
	}
	if dur > degradedMax {
		dur = degradedMax
	}
	return dur
}

// transitionActive 成功时回到 active：清 state_until、清 reason、清失败计数、更新 last_used_at。
// source 标记恢复的触发方（转发成功 / 巡检 / 管理员清标记），仅用于事件留痕。
//
// disabled 状态受保护：只有管理员操作（ManualRecover / ToggleScheduling）才能解除，
// forwarder 的 Success 判决不会覆盖它——防止在飞请求的成功回调把手动禁用的账号重新激活。
// 对 forward 成功，仍生效的 rate_limited / degraded 都保留到 TTL 到期，避免一条
// 并发成功把刚写入的保护状态提前清掉。管理员和健康巡检仍通过显式 source 恢复。
func (sm *StateMachine) transitionActive(ctx context.Context, accountID int, source string) bool {
	now := time.Now()
	dbCtx, cancel := context.WithTimeout(context.Background(), dbTimeout)
	defer cancel()

	existing, err := sm.db.Account.Get(dbCtx, accountID)
	if err != nil {
		slog.Warn("scheduler_state_active_read_failed",
			sdk.LogFieldAccountID, accountID, sdk.LogFieldError, err)
		return false
	}
	prevState := existing.State

	if prevState == account.StateDisabled {
		return false
	}

	upd := sm.db.Account.Update().
		Where(account.IDEQ(accountID), account.StateNEQ(account.StateDisabled)).
		SetState(account.StateActive).
		ClearStateUntil().
		SetErrorMsg("").
		SetLastUsedAt(now)
	if source == eventSourceForward {
		upd.Where(account.Or(
			account.StateEQ(account.StateActive),
			account.And(
				account.StateIn(account.StateRateLimited, account.StateDegraded),
				account.Or(account.StateUntilIsNil(), account.StateUntilLTE(now)),
			),
		))
	}
	affected, err := upd.Save(dbCtx)
	if err != nil {
		slog.Warn("scheduler_state_active_failed",
			sdk.LogFieldAccountID, accountID, sdk.LogFieldError, err)
		return false
	}
	if affected == 0 {
		// A live rate limit, newer degraded state, or manual disable won the
		// race. This is normal for overlapping forwards and preserves the
		// account protection until it is safe to recover.
		return false
	}
	if prevState != account.StateActive {
		sm.notifyCritical()
	}
	// 只有从异常态（rate_limited / degraded）回来才留"已恢复"事件，
	// active → active 的常规成功不落事件（避免每次请求都写一条）。
	if prevState == account.StateRateLimited || prevState == account.StateDegraded {
		sm.recordEvent(accountID, 0, 0, accountevent.EventTypeRecovered, "", "", source, 0, nil)
	}
	return true
}

// transition 把账号转到指定状态。stateUntil=nil 表示无到期（disabled）或清空。
func (sm *StateMachine) transition(ctx context.Context, accountID int, newState account.State, stateUntil *time.Time, reason string) bool {
	dbCtx, cancel := context.WithTimeout(context.Background(), dbTimeout)
	defer cancel()

	upd := sm.db.Account.Update().
		Where(account.IDEQ(accountID)).
		SetState(newState).
		SetErrorMsg(truncateReason(reason))
	if newState != account.StateDisabled {
		// Automatic rate-limit/degraded transitions must never make an account
		// schedulable again after an operator has disabled it.
		upd.Where(account.StateNEQ(account.StateDisabled))
	}
	if stateUntil == nil {
		upd = upd.ClearStateUntil()
	} else {
		upd = upd.SetStateUntil(*stateUntil)
	}

	affected, err := upd.Save(dbCtx)
	if err != nil {
		slog.Error("scheduler_state_transition_failed",
			sdk.LogFieldAccountID, accountID,
			"target_state", newState,
			sdk.LogFieldError, err,
		)
		return false
	}
	if affected == 0 {
		return false
	}
	slog.Info("scheduler_state_transition",
		sdk.LogFieldAccountID, accountID,
		"state", newState,
		"until", stateUntil,
		sdk.LogFieldReason, reason,
	)

	// Disabled 是关键转移：缓存里还挂着 active 的快照会让调度器反复选它、白白浪费 failover。
	// RateLimited / Degraded 有 state_until，缓存 3s 陈旧期可接受。
	if newState == account.StateDisabled {
		sm.notifyCritical()
	}
	return true
}

// SchedulabilityOf 根据当前状态 + 到期时间判断账号是否可调度。
//
// rate_limited / degraded 到期后**不会**自动写 DB（由下一次 Success 判决统一回收），
// 但调度器读到 state_until <= now 就会把它视为 active / StickyOnly，不再排除。
func SchedulabilityOf(acc *ent.Account, now time.Time) Schedulability {
	switch acc.State {
	case account.StateActive:
		return Normal
	case account.StateDisabled:
		return NotSchedulable
	case account.StateRateLimited:
		if acc.StateUntil != nil && acc.StateUntil.After(now) {
			return NotSchedulable
		}
		return Normal // 已到期，lazy 回收
	case account.StateDegraded:
		if acc.StateUntil != nil && acc.StateUntil.After(now) {
			return StickyOnly // 只在没有 Normal 账号时兜底
		}
		return Normal
	default:
		// 未知状态值：保守按不可用处理
		return NotSchedulable
	}
}

// truncateReason 限制 error_msg 长度，防止异常文本把列撑爆。
// 按字节截断后回退到合法 UTF-8 边界：中文/emoji 被从 rune 中间切断会产生非法
// UTF-8，Postgres 直接拒绝写入，导致最需要留痕的长报错反而丢失。
func truncateReason(s string) string {
	const maxLen = 500
	if len(s) <= maxLen {
		return s
	}
	cut := s[:maxLen]
	for len(cut) > 0 && !utf8.ValidString(cut) {
		cut = cut[:len(cut)-1]
	}
	return cut
}
