package plugin

import (
	"context"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"

	sdk "github.com/DouDOU-start/airgate-sdk/sdkgo"
)

// 对冲重试(hedged failover)
//
// 生产上最伤用户的失败形态是「上游接了连接却迟迟不吐字」:2026-09-02 实测贾克斯/卡卡两家
// 中继在拥堵时首字 20~60s,首跳看门狗 30s 才判失败,串行换号第二跳又放宽到 60s、第三跳 90s,
// 一条请求最坏要等 180s 才 502,客户端早就自己断了。
//
// 对冲的思路:主尝试超过 hedgeDelay 仍没向客户端提交任何应用数据时,**并行**向组内另一个账号
// 再发一份同样的请求;两路各自缓冲,谁先产出真实内容就把谁提交给客户端,另一路立刻取消。
// 这样慢首字的尾部从「看门狗 × 跳数」压到「hedgeDelay + 第二路首字」。
//
// 边界(都是有意为之):
//   - 只对流式(realtime)请求对冲:非流式由插件整体返回,没有「已提交」的中间态可仲裁;
//   - 同一时刻最多两路在飞(主 + 一路对冲),对冲算一次 failover attempt,受 maxFailoverAttempts 约束;
//   - 只有在没有任何应用数据提交给客户端时才会发起对冲;SSE 注释心跳不算应用数据;
//   - 输家若是自己先失败的,判决照常落到调度器;若是被我们取消的,分两种:跑满了 hedgeDelay 仍零输出
//     → 等价于首字超时,按瞬时故障落判决(否则卡死账号永不 degraded,之后每条请求都白付对冲延迟);
//     没跑满就被赢家淘汰 → 不落判决(那不是它的错)。
//
// 代价:对冲触发时上游会多承受一份请求(以及被取消那路已产生的少量 token)。触发率由
// hedgeDelay 控制——按当天生产分布,15s 约命中 p90 以外的请求。

// defaultForwardHedgeDelay 主尝试无应用数据多久后发起对冲;0 表示关闭。
const defaultForwardHedgeDelay = 15 * time.Second

// hedgeLoserDrainTimeout 赢家确定后,等待被取消那路退出的上限;超时则让它自己收尾。
const hedgeLoserDrainTimeout = 3 * time.Second

var forwardHedgeDelayNanos atomic.Int64

func init() {
	forwardHedgeDelayNanos.Store(int64(defaultForwardHedgeDelay))
}

// SetForwardHedgeDelay 设置对冲延迟;<=0 关闭对冲。
func SetForwardHedgeDelay(d time.Duration) {
	if d < 0 {
		d = 0
	}
	forwardHedgeDelayNanos.Store(int64(d))
}

// ForwardHedgeDelay 当前对冲延迟。
func ForwardHedgeDelay() time.Duration {
	return time.Duration(forwardHedgeDelayNanos.Load())
}

// hedgeArbiter 仲裁多路尝试对同一个客户端 writer 的提交权:第一路写出应用数据的尝试胜出,
// 其余尝试的输出全部丢弃。SSE 注释心跳(协议中立,不属于任何账号)在无人提交前来自任一路都放行,
// 让客户端与 CF 边缘的连接保持活跃;提交后仅赢家可写。
type hedgeArbiter struct {
	target gin.ResponseWriter

	mu           sync.Mutex
	committedID  int
	headerCopied bool
	headerSent   bool
}

func newHedgeArbiter(target gin.ResponseWriter) *hedgeArbiter {
	return &hedgeArbiter{target: target}
}

// committedAttempt 已提交的尝试序号;0 表示尚无。
func (a *hedgeArbiter) committedAttempt() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.committedID
}

func (a *hedgeArbiter) copyHeadersLocked(hdr http.Header) {
	if a.headerCopied || a.headerSent {
		return
	}
	dst := a.target.Header()
	for k, values := range hdr {
		dst[k] = append([]string(nil), values...)
	}
	a.headerCopied = len(hdr) > 0
}

// writeHeartbeat 无人提交时把某一路的 SSE 注释心跳透传给客户端。
func (a *hedgeArbiter) writeHeartbeat(hdr http.Header, data []byte) (int, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.committedID != 0 {
		return len(data), nil
	}
	a.copyHeadersLocked(hdr)
	n, err := a.target.Write(data)
	a.headerSent = true
	a.target.Flush()
	return n, err
}

// tryCommit 尝试让 id 这一路成为赢家:成功则把它缓冲的响应头/状态/数据一次性写给客户端。
func (a *hedgeArbiter) tryCommit(id int, hdr http.Header, status int, pending [][]byte) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.committedID != 0 {
		return false
	}
	a.committedID = id
	a.copyHeadersLocked(hdr)
	if status <= 0 {
		status = http.StatusOK
	}
	if !a.headerSent {
		a.target.WriteHeader(status)
		a.headerSent = true
	}
	for _, d := range pending {
		_, _ = a.target.Write(d)
	}
	if len(pending) > 0 {
		a.target.Flush()
	}
	return true
}

// hedgeAttemptWriter 一路尝试的私有 writer:提交前缓冲全部应用数据,心跳经仲裁器透传;
// 赢得仲裁后直写客户端;输掉仲裁后丢弃一切写入。
//
// 与插件契约的配合:openai 插件在拿到第一个真实 token 之前不会写任何应用数据(只发心跳),
// 所以这里的缓冲通常只有「首帧」大小;上游 4xx/5xx 由插件经 outcome 返回而不经 writer。
type hedgeAttemptWriter struct {
	arb *hedgeArbiter
	id  int

	mu        sync.Mutex
	hdr       http.Header
	status    int
	pending   [][]byte
	committed bool
	discarded bool
}

func newHedgeAttemptWriter(arb *hedgeArbiter, id int) *hedgeAttemptWriter {
	return &hedgeAttemptWriter{arb: arb, id: id, hdr: make(http.Header)}
}

func (w *hedgeAttemptWriter) Header() http.Header {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.committed {
		return w.arb.target.Header()
	}
	return w.hdr
}

func (w *hedgeAttemptWriter) WriteHeader(statusCode int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.committed {
		w.arb.target.WriteHeader(statusCode)
		return
	}
	if w.status == 0 {
		w.status = statusCode
	}
}

func (w *hedgeAttemptWriter) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.committed {
		return w.arb.target.Write(data)
	}
	if w.discarded || len(data) == 0 {
		return len(data), nil
	}
	if isSSECommentOnly(data) {
		return w.arb.writeHeartbeat(w.hdr, data)
	}
	// 非 2xx 的响应体不争夺提交权:failover 判决与最终错误响应都由 core 自己写。
	if w.status >= http.StatusBadRequest {
		w.pending = append(w.pending, cloneBytes(data))
		return len(data), nil
	}
	if w.arb.tryCommit(w.id, w.hdr, w.status, w.pending) {
		w.committed = true
		w.pending = nil
		return w.arb.target.Write(data)
	}
	w.discarded = true
	w.pending = nil
	return len(data), nil
}

func (w *hedgeAttemptWriter) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.committed {
		w.arb.target.Flush()
	}
}

// Committed 本路是否已赢得仲裁并向客户端提交了应用数据。
func (w *hedgeAttemptWriter) Committed() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.committed
}

func cloneBytes(data []byte) []byte {
	buf := make([]byte, len(data))
	copy(buf, data)
	return buf
}

// hedgeAttempt 一路尝试的完整上下文:独立的 state 副本、独立的取消句柄、独立的 writer。
type hedgeAttempt struct {
	id     int
	state  *forwardState
	writer *hedgeAttemptWriter

	releaseSlot    func()
	stopProbeLease func()

	cancel    context.CancelFunc
	done      chan struct{}
	execution forwardExecution
	startedAt time.Time

	// canceledByHedge 输给了另一路而被我们主动取消——它的判决不该落到调度器。
	canceledByHedge atomic.Bool
	// abandoned 协调器不再等待它退出——goroutine 结束时自行释放并发槽。
	abandoned atomic.Bool
}

func (f *Forwarder) newHedgeAttempt(id int, state *forwardState, arb *hedgeArbiter, releaseSlot, stopProbeLease func()) *hedgeAttempt {
	var releaseOnce sync.Once
	release := func() {
		releaseOnce.Do(func() {
			if releaseSlot != nil {
				releaseSlot()
			}
		})
	}
	if stopProbeLease == nil {
		stopProbeLease = func() {}
	}
	return &hedgeAttempt{
		id:             id,
		state:          state,
		writer:         newHedgeAttemptWriter(arb, id),
		releaseSlot:    release,
		stopProbeLease: stopProbeLease,
		done:           make(chan struct{}),
	}
}

func (a *hedgeAttempt) accountID() int {
	if a == nil || a.state == nil || a.state.account == nil {
		return 0
	}
	return a.state.account.ID
}

// attemptRunner 执行一路尝试;生产实现是 callPluginWith,测试可注入假实现。
type attemptRunner func(ctx context.Context, c *gin.Context, state *forwardState, w http.ResponseWriter) forwardExecution

// callPluginWith 把请求发给插件——与 callPlugin 相同,但 ctx 与 writer 由调用方指定,
// 让每一路尝试可独立取消、独立缓冲。
func (f *Forwarder) callPluginWith(ctx context.Context, c *gin.Context, state *forwardState, w http.ResponseWriter) forwardExecution {
	state.grpcCallAt = time.Now()
	req := buildPluginRequest(c, state)
	req.Writer = w
	outcome, err := state.plugin.Gateway.Forward(ctx, req)
	return forwardExecution{
		outcome:  outcome,
		err:      err,
		duration: time.Since(state.startedAt),
	}
}

// startHedgeAttempt 在独立 goroutine 里执行一路尝试。
func startHedgeAttempt(c *gin.Context, a *hedgeAttempt, run attemptRunner) {
	attemptCtx, cancel := context.WithCancel(c.Request.Context())
	a.cancel = cancel
	a.startedAt = time.Now()
	go func() {
		defer close(a.done)
		defer cancel()
		a.execution = run(attemptCtx, c, a.state, a.writer)
		a.stopProbeLease()
		if a.abandoned.Load() {
			a.releaseSlot()
		}
	}()
}

// hedgeCallbacks 协调器回调:都在请求 goroutine 上串行执行,可安全触碰 Forward 循环的局部变量。
type hedgeCallbacks struct {
	// acquireHedge 为对冲挑选并锁定另一个账号;拿不到返回 nil(不对冲,继续等主尝试)。
	acquireHedge func() *hedgeAttempt
	// onLoserFailed 某一路在赢家出现前自己失败了(可 failover 的失败):落判决、记排除。
	onLoserFailed func(a *hedgeAttempt)
	// onLoserTimedOut 某一路被赢家淘汰,且它跑满了 hedgeDelay 仍零输出:等价于一次首字超时,
	// 应当按瞬时故障落判决,让调度器在接下来的窗口里避开它——否则卡死的账号永远不会被
	// 标 degraded,之后每条请求都要白付一次对冲延迟,上游也要多扛一倍请求。
	onLoserTimedOut func(a *hedgeAttempt, ranFor time.Duration)
	// retryable 该路的失败是否允许换号——不允许的失败即为最终结果。
	retryable func(a *hedgeAttempt) bool
}

// runHedgedAttempts 驱动主尝试与(至多一路)对冲尝试,返回胜出的一路与实际发起的尝试数。
//
// 「胜出」= 已向客户端提交应用数据,或其结果不允许再换号(成功/不可重试的失败)。
// 两路都以可重试的方式失败时,后失败的那路作为返回值交回主循环,由主循环按既有逻辑处理。
func (f *Forwarder) runHedgedAttempts(c *gin.Context, primary *hedgeAttempt, delay time.Duration, run attemptRunner, cb hedgeCallbacks) (*hedgeAttempt, int) {
	logger := sdk.LoggerFromContext(c.Request.Context())
	ctx := c.Request.Context()
	arb := primary.writer.arb

	startHedgeAttempt(c, primary, run)
	launched := 1

	var hedge *hedgeAttempt
	var hedgeDone chan struct{}
	var timerC <-chan time.Time
	if delay > 0 && cb.acquireHedge != nil {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		timerC = timer.C
	}
	primaryDone := primary.done

	// finish 赢家确定:取消另一路并等它退出。
	finish := func(winner, other *hedgeAttempt) (*hedgeAttempt, int) {
		if other != nil {
			other.canceledByHedge.Store(true)
			if other.cancel != nil {
				other.cancel()
			}
			select {
			case <-other.done:
				other.releaseSlot()
				ranFor := time.Since(other.startedAt)
				if !other.writer.Committed() && ranFor >= delay && cb.onLoserTimedOut != nil {
					// 它得到了不少于 hedgeDelay 的机会仍一字未出——按首字超时落判决。
					cb.onLoserTimedOut(other, ranFor)
				} else {
					f.releaseFamilyProbe(other.state)
				}
			case <-time.After(hedgeLoserDrainTimeout):
				other.abandoned.Store(true)
				f.releaseFamilyProbe(other.state)
			}
			logger.Info("forward_hedge_resolved",
				"winner_attempt", winner.id,
				"winner_account_id", winner.accountID(),
				"canceled_account_id", other.accountID(),
				"winner_committed", winner.writer.Committed(),
				sdk.LogFieldDurationMs, time.Since(primary.startedAt).Milliseconds(),
			)
		}
		return winner, launched
	}

	// settle 一路尝试结束了:要么它就是最终结果,要么记为输家继续等另一路。
	settle := func(a, other *hedgeAttempt, otherRunning bool) (*hedgeAttempt, int, bool) {
		final := a.writer.Committed() || cb.retryable == nil || !cb.retryable(a)
		if final {
			if otherRunning {
				winner, n := finish(a, other)
				return winner, n, true
			}
			return a, launched, true
		}
		if !otherRunning {
			// 另一路要么没发起、要么已作为输家处理过:本路交回主循环按常规失败处理。
			return a, launched, true
		}
		if cb.onLoserFailed != nil {
			cb.onLoserFailed(a)
		}
		return nil, 0, false
	}

	for {
		select {
		case <-timerC:
			timerC = nil
			if arb.committedAttempt() != 0 || primaryDone == nil {
				continue
			}
			if h := cb.acquireHedge(); h != nil {
				hedge = h
				hedgeDone = h.done
				launched++
				startHedgeAttempt(c, hedge, run)
				logger.Info("forward_hedge_started",
					"primary_account_id", primary.accountID(),
					"hedge_account_id", hedge.accountID(),
					"delay_ms", delay.Milliseconds(),
				)
			}
		case <-primaryDone:
			primaryDone = nil
			if winner, n, ok := settle(primary, hedge, hedgeDone != nil); ok {
				return winner, n
			}
		case <-hedgeDone:
			hedgeDone = nil
			if winner, n, ok := settle(hedge, primary, primaryDone != nil); ok {
				return winner, n
			}
		case <-ctx.Done():
			// 客户端走了:两路都会随父 ctx 结束,等主尝试退出后交回主循环走取消路径。
			if hedge != nil && hedgeDone != nil {
				hedge.canceledByHedge.Store(true)
				select {
				case <-hedge.done:
					hedge.releaseSlot()
					f.releaseFamilyProbe(hedge.state)
				case <-time.After(hedgeLoserDrainTimeout):
					hedge.abandoned.Store(true)
				}
			}
			if primaryDone != nil {
				<-primary.done
				return primary, launched
			}
			// 主尝试已作为输家处理过,对冲那路就是最后的结果。
			return hedge, launched
		}
	}
}
