package billing

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/DouDOU-start/airgate-core/ent"
	entaccount "github.com/DouDOU-start/airgate-core/ent/account"
	entapikey "github.com/DouDOU-start/airgate-core/ent/apikey"
	entgroup "github.com/DouDOU-start/airgate-core/ent/group"
	entuser "github.com/DouDOU-start/airgate-core/ent/user"
	sdk "github.com/DouDOU-start/airgate-sdk/sdkgo"
)

const (
	defaultBufferSize = 1000 // 内存 channel 缓冲大小
	batchSize         = 100  // 批量写入阈值
	maxRetries        = 3    // 写入失败最大重试次数
)

// ErrInsufficientBalance is returned by synchronous custom usage charges when
// the user's balance cannot cover the requested amount. Model usage keeps the
// existing asynchronous/negative-balance semantics; only explicit product
// charges use this strict floor.
var ErrInsufficientBalance = errors.New("insufficient balance")

// flushInterval 定时刷新间隔（测试注入点）
var flushInterval = 5 * time.Second

// UsageRecord 使用记录
type UsageRecord struct {
	// RequestID 计费幂等 ID（UUID）。Record/RecordSync 入口自动补齐；
	// 落库带唯一索引，WAL 回放与重试据此去重，防重复入账/扣费。
	RequestID                    string
	UserID                       int
	UserEmail                    string
	APIKeyID                     int
	AccountID                    int
	GroupID                      int
	Platform                     string
	Model                        string
	InputTokens                  int
	OutputTokens                 int
	CachedInputTokens            int
	CacheCreationTokens          int
	CacheCreation5mTokens        int
	CacheCreation1hTokens        int
	ReasoningOutputTokens        int
	InputPrice                   float64
	OutputPrice                  float64
	CachedInputPrice             float64
	CacheCreationPrice           float64
	CacheCreation1hPrice         float64
	InputCost                    float64
	OutputCost                   float64
	CachedInputCost              float64
	CacheCreationCost            float64
	ImageCost                    float64
	ImageFixedPriceApplied       bool
	ImageFixedPriceReplacesTotal bool
	TotalCost                    float64
	ActualCost                   float64 // 平台真实成本（扣 reseller 余额）
	BilledCost                   float64 // 客户账面消耗（累加到 APIKey.used_quota）
	AccountCost                  float64 // 账号实际成本（仅服务"账号计费"统计）
	RateMultiplier               float64 // 快照：本次生效的平台计费倍率
	SellRate                     float64 // 快照：本次生效的销售倍率（0 表示未启用 markup）
	AccountRateMultiplier        float64 // 快照：本次生效的 account_rate
	ServiceTier                  string
	ImageSize                    string // 图像生成请求的实际出图尺寸（"WxH"），非图像请求留空
	Stream                       bool
	DurationMs                   int64
	FirstTokenMs                 int64
	UserAgent                    string
	IPAddress                    string
	Endpoint                     string
	ReasoningEffort              string
	UsageAttributes              []sdk.UsageAttribute
	UsageMetrics                 []sdk.UsageMetric
	UsageCostDetails             []sdk.UsageCostDetail
	UsageMetadata                map[string]string

	// Status 为 UsageStatusError 时表示这是一条失败请求记录：token 与费用均为 0，
	// 不扣余额、不计入成功请求统计，只供用户与管理员查询错误情况。
	// 零值（空串）等价于 UsageStatusSuccess，历史 WAL 回放据此保持旧语义。
	Status       string
	ErrorCode    string // 失败分类（转发判决 Kind 或 Core 侧拦截原因）
	ErrorStatus  int    // 上游 HTTP 状态码；无上游响应时回退 Core 对外状态码
	ErrorMessage string // 脱敏后的失败原因
}

const (
	// UsageStatusSuccess 正常计费的请求。
	UsageStatusSuccess = "success"
	// UsageStatusError 失败请求：落库留痕但不计费。
	UsageStatusError = "error"

	// usageErrorMessageMaxLen 失败原因落库上限（字节）。与 scheduler 的 error_msg 同口径。
	usageErrorMessageMaxLen = 500
	// usageErrorCodeMaxLen 失败分类上限（字节）。取值来自 Core 自己的常量，留足余量即可。
	usageErrorCodeMaxLen = 64

	// usageUnknownPlatform / usageUnknownModel 是 platform/model 的兜底值。
	// 两列在 schema 上是 NotEmpty，失败请求可能在解析出模型前就中断；
	// 空串会让整批 CreateBulk 失败并把同批正常记录一起拖进 WAL 死循环。
	usageUnknownPlatform = "unknown"
	usageUnknownModel    = "unknown"
)

// IsError 是否为失败请求记录。
func (r UsageRecord) IsError() bool { return r.Status == UsageStatusError }

// truncateBytesUTF8 按字节上限截断，并回退到合法 UTF-8 边界。
// 中文/emoji 被从 rune 中间切断会产生非法 UTF-8，Postgres 直接拒绝整条写入，
// 会让同批的正常计费记录一起进 WAL 反复重试（与 scheduler.truncateReason 同因）。
func truncateBytesUTF8(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	cut := s[:maxLen]
	for len(cut) > 0 && !utf8.ValidString(cut) {
		cut = cut[:len(cut)-1]
	}
	return cut
}

// fallbackNonEmpty 空串时给兜底值。
func fallbackNonEmpty(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

// normalizedStatus 归一化状态，空值按成功处理（兼容旧 WAL 记录）。
func (r UsageRecord) normalizedStatus() string {
	if r.Status == UsageStatusError {
		return UsageStatusError
	}
	return UsageStatusSuccess
}

// Recorder 异步记录器
// 使用 channel 缓冲，goroutine 批量写入
// 每 100 条或每 5 秒 flush 一次
//
// 丢账防护：主队列满 / 落库重试耗尽 / 停机窗口 三类记录经 WAL 落盘暂存
// （见 wal.go），后台回放去重后重新入库。未启用 WAL 时保持旧的丢弃行为。
type Recorder struct {
	db      *ent.Client
	ch      chan UsageRecord
	spillCh chan UsageRecord // 主队列满时的溢出队列，由专职协程落盘，请求协程不做磁盘 IO
	stopCh  chan struct{}
	stopped chan struct{}
	once    sync.Once

	// halted 置位后 Record 不再进 channel（停机 drain 已开始），直接同步落 WAL。
	// 消除旧实现"往已 close 的 channel 发送"的停机 panic。
	halted    atomic.Bool
	spillDone chan struct{}

	wal          *usageWAL
	replayCancel context.CancelFunc
	replayDone   chan struct{}

	// onNegativeBalance 批量扣费提交后发现余额已透支的用户回调（如失效其 API Key 缓存）。
	onNegativeBalance func(userIDs []int)

	spilledTotal atomic.Uint64 // 成功落 WAL 的记录数
	droppedTotal atomic.Uint64 // 最终仍被丢弃的记录数（WAL 未启用或落盘失败）
}

// NewRecorder 创建使用量记录器
func NewRecorder(db *ent.Client, bufferSize int) *Recorder {
	if bufferSize <= 0 {
		bufferSize = defaultBufferSize
	}
	return &Recorder{
		db:         db,
		ch:         make(chan UsageRecord, bufferSize),
		spillCh:    make(chan UsageRecord, bufferSize*4),
		stopCh:     make(chan struct{}),
		stopped:    make(chan struct{}),
		spillDone:  make(chan struct{}),
		replayDone: make(chan struct{}),
	}
}

// EnableWAL 启用计费 WAL（须在 Start 之前调用）。失败不致命：退化为旧丢弃行为。
func (r *Recorder) EnableWAL(dir string) error {
	wal, err := newUsageWAL(dir)
	if err != nil {
		return err
	}
	r.wal = wal
	return nil
}

// SetNegativeBalanceHook 注册负余额回调（须在 Start 之前调用）。
func (r *Recorder) SetNegativeBalanceHook(fn func(userIDs []int)) {
	r.onNegativeBalance = fn
}

// Record 提交使用记录（非阻塞）
func (r *Recorder) Record(record UsageRecord) {
	if record.RequestID == "" {
		record.RequestID = uuid.NewString()
	}
	if r.halted.Load() {
		// 停机窗口：run() 已在 drain，绕过 channel 直接落 WAL（此路径仅停机时走到，
		// 磁盘 IO 可接受）
		r.spillToWAL([]UsageRecord{record}, "recorder_halted")
		return
	}
	select {
	case r.ch <- record:
		return
	default:
	}
	// 主队列满：转溢出队列，由 spillLoop 批量落盘
	select {
	case r.spillCh <- record:
	default:
		r.drop(1, "spill_buffer_full", record.UserID, record.Model)
	}
}

// RecordSync 同步写入一条使用记录并返回 usage_log.id。
// 需要立即把 usage_id 关联到任务时使用；普通转发仍走异步 Record。
func (r *Recorder) RecordSync(ctx context.Context, record UsageRecord) (int, error) {
	if record.RequestID == "" {
		record.RequestID = uuid.NewString()
	}
	tx, err := r.db.Tx(ctx)
	if err != nil {
		return 0, fmt.Errorf("开启事务失败: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	refs, err := loadUsageLogRefs(ctx, tx, []UsageRecord{record})
	if err != nil {
		return 0, err
	}
	log, err := usageLogCreate(tx, record, refs).Save(ctx)
	if err != nil {
		return 0, fmt.Errorf("插入 UsageLog 失败: %w", err)
	}
	metered, err := resolveSubscriptionMetering(ctx, tx, []UsageRecord{record})
	if err != nil {
		return 0, err
	}
	if err := applyUsageCharges(ctx, tx, []UsageRecord{record}, refs, metered); err != nil {
		return 0, err
	}
	if err := applySubscriptionCharges(ctx, tx, metered); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("提交事务失败: %w", err)
	}
	return log.ID, nil
}

// RecordSyncCharge persists a non-model product usage record and charges the
// user's balance atomically. It is intentionally separate from RecordSync:
// model usage is allowed to settle asynchronously, while a fixed render fee
// must never be reported as successful after a failed charge.
func (r *Recorder) RecordSyncCharge(ctx context.Context, record UsageRecord) (int, error) {
	if record.RequestID == "" {
		record.RequestID = uuid.NewString()
	}
	tx, err := r.db.Tx(ctx)
	if err != nil {
		return 0, fmt.Errorf("开启事务失败: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	refs, err := loadUsageLogRefs(ctx, tx, []UsageRecord{record})
	if err != nil {
		return 0, err
	}
	if record.UserID <= 0 || !refs.hasUser(record.UserID) {
		return 0, fmt.Errorf("计费用户不存在 user_id=%d", record.UserID)
	}
	log, err := usageLogCreate(tx, record, refs).Save(ctx)
	if err != nil {
		return 0, fmt.Errorf("插入 UsageLog 失败: %w", err)
	}
	metered, err := resolveSubscriptionMetering(ctx, tx, []UsageRecord{record})
	if err != nil {
		return 0, err
	}
	if m, ok := metered[0]; ok {
		// 订阅制：从点数账本扣，余额不动。
		if err := chargeSubscriptionSync(ctx, tx, m); err != nil {
			return 0, err
		}
	} else if record.ActualCost > 0 && refs.hasUser(record.UserID) {
		updated, err := tx.User.Update().
			Where(entuser.IDEQ(record.UserID), entuser.BalanceGTE(record.ActualCost)).
			AddBalance(-record.ActualCost).
			Save(ctx)
		if err != nil {
			return 0, fmt.Errorf("扣减用户余额失败 user_id=%d cost=%.8f: %w", record.UserID, record.ActualCost, err)
		}
		if updated == 0 {
			return 0, ErrInsufficientBalance
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("提交计费事务失败: %w", err)
	}
	return log.ID, nil
}

// Start 启动后台写入 goroutine
func (r *Recorder) Start() {
	go r.run()
	go r.spillLoop()
	if r.wal != nil {
		ctx, cancel := context.WithCancel(context.Background())
		r.replayCancel = cancel
		go r.replayLoop(ctx)
	} else {
		close(r.replayDone)
	}
}

// Stop 停止写入，等待缓冲区清空
func (r *Recorder) Stop() {
	r.once.Do(func() {
		close(r.stopCh)
		<-r.stopped
		<-r.spillDone
		if r.replayCancel != nil {
			r.replayCancel()
		}
		<-r.replayDone
	})
}

// run 后台运行循环
func (r *Recorder) run() {
	defer close(r.stopped)

	ticker := time.NewTicker(flushInterval)
	defer ticker.Stop()

	batch := make([]UsageRecord, 0, batchSize)
	ctx := context.Background()

	for {
		select {
		case rec := <-r.ch:
			batch = append(batch, rec)
			if len(batch) >= batchSize {
				r.flush(ctx, batch)
				batch = batch[:0]
			}

		case <-ticker.C:
			if len(batch) > 0 {
				r.flush(ctx, batch)
				batch = batch[:0]
			}

		case <-r.stopCh:
			// 停止前处理剩余数据。不 close(r.ch)：置 halted 让新 Record 改走 WAL，
			// 避免"往已 close 的 channel 发送"panic（旧实现的停机丢账 bug）。
			r.halted.Store(true)
			r.drainAndFlush(ctx, batch)
			return
		}
	}
}

// drainAndFlush 停机收尾：非阻塞排空主队列并整批落库。
func (r *Recorder) drainAndFlush(ctx context.Context, batch []UsageRecord) {
	for {
		select {
		case rec := <-r.ch:
			batch = append(batch, rec)
		default:
			if len(batch) > 0 {
				r.flush(ctx, batch)
			}
			return
		}
	}
}

// flushSleep 重试间隔（测试注入点）
var flushSleep = time.Sleep

// flush 批量写入数据库，失败时重试；重试耗尽转 WAL 落盘（未启用 WAL 才丢弃）
func (r *Recorder) flush(ctx context.Context, batch []UsageRecord) {
	for attempt := 0; attempt < maxRetries; attempt++ {
		if err := r.batchInsert(ctx, batch); err != nil {
			slog.Error("billing_batch_flush_failed",
				"attempt", attempt+1,
				"count", len(batch),
				"error", err,
			)
			if attempt < maxRetries-1 {
				flushSleep(time.Duration(attempt+1) * time.Second)
				continue
			}
			r.spillToWAL(batch, "flush_retry_exhausted")
			return
		}
		slog.Debug("billing_batch_flush_succeeded", "count", len(batch))
		return
	}
}

// batchInsert 在同一事务中批量写入使用记录并扣费
// 保证 UsageLog 插入与余额扣减的原子性，避免记录成功但扣费失败
func (r *Recorder) batchInsert(ctx context.Context, batch []UsageRecord) error {
	tx, err := r.db.Tx(ctx)
	if err != nil {
		return fmt.Errorf("开启事务失败: %w", err)
	}
	defer func() {
		// 若事务未提交则回滚（Commit 后 Rollback 是 no-op）
		_ = tx.Rollback()
	}()

	refs, err := loadUsageLogRefs(ctx, tx, batch)
	if err != nil {
		return err
	}

	// 1. 批量写入 UsageLog（同时记录 actual_cost 和 billed_cost 双轨数据）
	builders := make([]*ent.UsageLogCreate, 0, len(batch))
	for _, rec := range batch {
		builders = append(builders, usageLogCreate(tx, rec, refs))
	}

	if _, err := tx.UsageLog.CreateBulk(builders...).Save(ctx); err != nil {
		return fmt.Errorf("批量插入 UsageLog 失败: %w", err)
	}

	// 2. 扣费：订阅制分组的记录进订阅点数账本，其余扣用户余额；API Key 累加器两边都记。
	metered, err := resolveSubscriptionMetering(ctx, tx, batch)
	if err != nil {
		return err
	}
	if err := applyUsageCharges(ctx, tx, batch, refs, metered); err != nil {
		return err
	}
	if err := applySubscriptionCharges(ctx, tx, metered); err != nil {
		return err
	}

	// 3. 提交事务
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("提交事务失败: %w", err)
	}

	// 4. 提交后检查本批扣费用户是否已透支（best-effort，失败仅记日志）
	r.notifyNegativeBalance(ctx, batch)
	return nil
}

// notifyNegativeBalance 找出本批扣费后余额已为负的用户并触发回调。
// 用途：立刻失效其 API Key 验证缓存，把"负余额仍可透支"的窗口从缓存 TTL 压到秒级。
func (r *Recorder) notifyNegativeBalance(ctx context.Context, batch []UsageRecord) {
	if r.onNegativeBalance == nil {
		return
	}
	charged := collectUsageIDs(batch, func(rec UsageRecord) int {
		if rec.ActualCost > 0 {
			return rec.UserID
		}
		return 0
	})
	if len(charged) == 0 {
		return
	}
	negative, err := r.db.User.Query().
		Where(entuser.IDIn(charged...), entuser.BalanceLT(0)).
		IDs(ctx)
	if err != nil {
		slog.Error("billing_negative_balance_query_failed", "error", err)
		return
	}
	if len(negative) > 0 {
		r.onNegativeBalance(negative)
	}
}

type usageLogRefs struct {
	users    map[int]struct{}
	apiKeys  map[int]struct{}
	accounts map[int]struct{}
	groups   map[int]struct{}
}

func loadUsageLogRefs(ctx context.Context, tx *ent.Tx, batch []UsageRecord) (*usageLogRefs, error) {
	refs := &usageLogRefs{}

	userIDs, err := existingUserIDs(ctx, tx, collectUsageIDs(batch, func(rec UsageRecord) int { return rec.UserID }))
	if err != nil {
		return nil, fmt.Errorf("查询 UsageLog 用户关联失败: %w", err)
	}
	refs.users = mapUsageIDs(userIDs)

	apiKeyIDs, err := existingAPIKeyIDs(ctx, tx, collectUsageIDs(batch, func(rec UsageRecord) int { return rec.APIKeyID }))
	if err != nil {
		return nil, fmt.Errorf("查询 UsageLog API Key 关联失败: %w", err)
	}
	refs.apiKeys = mapUsageIDs(apiKeyIDs)

	accountIDs, err := existingAccountIDs(ctx, tx, collectUsageIDs(batch, func(rec UsageRecord) int { return rec.AccountID }))
	if err != nil {
		return nil, fmt.Errorf("查询 UsageLog 账号关联失败: %w", err)
	}
	refs.accounts = mapUsageIDs(accountIDs)

	groupIDs, err := existingGroupIDs(ctx, tx, collectUsageIDs(batch, func(rec UsageRecord) int { return rec.GroupID }))
	if err != nil {
		return nil, fmt.Errorf("查询 UsageLog 分组关联失败: %w", err)
	}
	refs.groups = mapUsageIDs(groupIDs)

	return refs, nil
}

func existingUserIDs(ctx context.Context, tx *ent.Tx, ids []int) ([]int, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	return tx.User.Query().Where(entuser.IDIn(ids...)).IDs(ctx)
}

func existingAPIKeyIDs(ctx context.Context, tx *ent.Tx, ids []int) ([]int, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	return tx.APIKey.Query().Where(entapikey.IDIn(ids...)).IDs(ctx)
}

func existingAccountIDs(ctx context.Context, tx *ent.Tx, ids []int) ([]int, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	return tx.Account.Query().Where(entaccount.IDIn(ids...)).IDs(ctx)
}

func existingGroupIDs(ctx context.Context, tx *ent.Tx, ids []int) ([]int, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	return tx.Group.Query().Where(entgroup.IDIn(ids...)).IDs(ctx)
}

func collectUsageIDs(batch []UsageRecord, pick func(UsageRecord) int) []int {
	seen := make(map[int]struct{})
	ids := make([]int, 0, len(batch))
	for _, rec := range batch {
		id := pick(rec)
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	return ids
}

func mapUsageIDs(ids []int) map[int]struct{} {
	out := make(map[int]struct{}, len(ids))
	for _, id := range ids {
		out[id] = struct{}{}
	}
	return out
}

func (r *usageLogRefs) hasUser(id int) bool {
	if r == nil {
		return id > 0
	}
	return hasUsageID(r.users, id)
}

func (r *usageLogRefs) hasAPIKey(id int) bool {
	if r == nil {
		return id > 0
	}
	return hasUsageID(r.apiKeys, id)
}

func (r *usageLogRefs) hasAccount(id int) bool {
	if r == nil {
		return id > 0
	}
	return hasUsageID(r.accounts, id)
}

func (r *usageLogRefs) hasGroup(id int) bool {
	if r == nil {
		return id > 0
	}
	return hasUsageID(r.groups, id)
}

func hasUsageID(ids map[int]struct{}, id int) bool {
	if id <= 0 {
		return false
	}
	_, ok := ids[id]
	return ok
}

func usageLogCreate(tx *ent.Tx, rec UsageRecord, refs *usageLogRefs) *ent.UsageLogCreate {
	b := tx.UsageLog.Create().
		SetPlatform(fallbackNonEmpty(rec.Platform, usageUnknownPlatform)).
		SetModel(fallbackNonEmpty(rec.Model, usageUnknownModel)).
		SetInputTokens(rec.InputTokens).
		SetOutputTokens(rec.OutputTokens).
		SetCachedInputTokens(rec.CachedInputTokens).
		SetCacheCreationTokens(rec.CacheCreationTokens).
		SetCacheCreation5mTokens(rec.CacheCreation5mTokens).
		SetCacheCreation1hTokens(rec.CacheCreation1hTokens).
		SetReasoningOutputTokens(rec.ReasoningOutputTokens).
		SetInputPrice(rec.InputPrice).
		SetOutputPrice(rec.OutputPrice).
		SetCachedInputPrice(rec.CachedInputPrice).
		SetCacheCreationPrice(rec.CacheCreationPrice).
		SetCacheCreation1hPrice(rec.CacheCreation1hPrice).
		SetInputCost(rec.InputCost).
		SetOutputCost(rec.OutputCost).
		SetCachedInputCost(rec.CachedInputCost).
		SetCacheCreationCost(rec.CacheCreationCost).
		SetImageCost(rec.ImageCost).
		SetTotalCost(rec.TotalCost).
		SetActualCost(rec.ActualCost).
		SetBilledCost(rec.BilledCost).
		SetAccountCost(rec.AccountCost).
		SetRateMultiplier(rec.RateMultiplier).
		SetSellRate(rec.SellRate).
		SetAccountRateMultiplier(rec.AccountRateMultiplier).
		SetServiceTier(rec.ServiceTier).
		SetImageSize(rec.ImageSize).
		SetStream(rec.Stream).
		SetDurationMs(rec.DurationMs).
		SetFirstTokenMs(rec.FirstTokenMs).
		SetUserAgent(rec.UserAgent).
		SetIPAddress(rec.IPAddress).
		SetEndpoint(rec.Endpoint).
		SetReasoningEffort(rec.ReasoningEffort).
		SetUsageAttributes(rec.UsageAttributes).
		SetUsageMetrics(rec.UsageMetrics).
		SetUsageCostDetails(enrichUsageCostDetails(rec)).
		SetUsageMetadata(rec.UsageMetadata).
		SetUserIDSnapshot(rec.UserID).
		SetUserEmailSnapshot(rec.UserEmail).
		SetStatus(rec.normalizedStatus()).
		SetErrorCode(truncateBytesUTF8(rec.ErrorCode, usageErrorCodeMaxLen)).
		SetErrorStatus(rec.ErrorStatus).
		SetErrorMessage(truncateBytesUTF8(rec.ErrorMessage, usageErrorMessageMaxLen))
	if refs.hasUser(rec.UserID) {
		b.SetUserID(rec.UserID)
	}
	if refs.hasAccount(rec.AccountID) {
		b.SetAccountID(rec.AccountID)
	}
	if refs.hasGroup(rec.GroupID) {
		b.SetGroupID(rec.GroupID)
	}
	if refs.hasAPIKey(rec.APIKeyID) {
		b.SetAPIKeyID(rec.APIKeyID)
	}
	// 仅非空时写入：空值保持 NULL，避免历史调用方（未带幂等 ID）撞唯一索引
	if rec.RequestID != "" {
		b.SetRequestID(rec.RequestID)
	}
	return b
}

func enrichUsageCostDetails(rec UsageRecord) []sdk.UsageCostDetail {
	if len(rec.UsageCostDetails) == 0 {
		return rec.UsageCostDetails
	}

	items := make([]sdk.UsageCostDetail, len(rec.UsageCostDetails))
	copy(items, rec.UsageCostDetails)

	var imageCostSum float64
	var imageInputCostSum float64
	for _, item := range items {
		if item.AccountCost > 0 && isImageCostDetail(item) {
			imageCostSum += item.AccountCost
		}
		if item.AccountCost > 0 && isImageInputCostDetail(item) {
			imageInputCostSum += item.AccountCost
		}
	}
	rate := rec.RateMultiplier
	if rate <= 0 {
		rate = 1
	}
	nonImageBaseCost := rec.InputCost + rec.OutputCost + rec.CachedInputCost + rec.CacheCreationCost
	expectedTokenActualCost := (nonImageBaseCost + imageInputCostSum + imageCostSum) * rate
	fixedImagePricing := rec.ImageFixedPriceApplied || (imageCostSum > 0 && math.Abs(rec.ActualCost-expectedTokenActualCost) > 1e-9)
	imageUserCost := imageCostSum * rate
	if fixedImagePricing {
		imageUserCost = rec.ActualCost
		if !rec.ImageFixedPriceReplacesTotal {
			imageUserCost = rec.ActualCost - nonImageBaseCost*rate
			if imageUserCost < 0 {
				imageUserCost = 0
			}
		}
	}

	for i := range items {
		accountCost := items[i].AccountCost
		if accountCost <= 0 {
			items[i].BillingMultiplier = rate
			continue
		}
		if fixedImagePricing && isImageInputCostDetail(items[i]) {
			items[i].BillingMultiplier = 0
			items[i].UserCost = 0
			continue
		}
		if fixedImagePricing && rec.ImageFixedPriceReplacesTotal && !isImageCostDetail(items[i]) {
			items[i].BillingMultiplier = 0
			items[i].UserCost = 0
			continue
		}
		if isImageCostDetail(items[i]) {
			if imageCostSum > 0 && (fixedImagePricing || imageUserCost > 0) {
				if imageUserCost > 0 {
					items[i].UserCost = imageUserCost * accountCost / imageCostSum
					items[i].BillingMultiplier = items[i].UserCost / accountCost
				} else {
					items[i].UserCost = 0
					items[i].BillingMultiplier = 0
				}
				items[i].Metadata = cloneCostMetadata(items[i].Metadata)
				if fixedImagePricing {
					items[i].Metadata["billing_mode"] = "fixed_image_price"
					if imageCount := parseCostMetadataPositiveInt(items[i].Metadata, "image_count"); imageCount > 0 {
						items[i].Metadata["fixed_unit_price"] = fmt.Sprintf("%.10g", items[i].UserCost/float64(imageCount))
						items[i].Metadata["fixed_unit"] = "CNY/image"
					}
				} else {
					items[i].Metadata["billing_mode"] = "image_token"
				}
			} else {
				items[i].BillingMultiplier = rate
				items[i].UserCost = accountCost * rate
			}
			continue
		}
		items[i].BillingMultiplier = rate
		items[i].UserCost = accountCost * rate
	}

	if !fixedImagePricing {
		return mergeImageTokenCostDetails(items)
	}

	return items
}

func isImageCostDetail(item sdk.UsageCostDetail) bool {
	key := strings.ToLower(strings.TrimSpace(item.Key))
	key = strings.ReplaceAll(key, "-", "_")
	if strings.Contains(key, "input") {
		return false
	}
	switch key {
	case "image", "images", "image_generation", "image_tool", "image_output", "image_outputs", "image_output_tokens":
		return true
	default:
		return strings.Contains(key, "image")
	}
}

func isImageInputCostDetail(item sdk.UsageCostDetail) bool {
	key := strings.ToLower(strings.TrimSpace(item.Key))
	key = strings.ReplaceAll(key, "-", "_")
	label := strings.ToLower(strings.TrimSpace(item.Label))
	return (strings.Contains(key, "image") || strings.Contains(label, "图片")) &&
		(strings.Contains(key, "input") || strings.Contains(label, "输入"))
}

func mergeImageTokenCostDetails(items []sdk.UsageCostDetail) []sdk.UsageCostDetail {
	merged := make([]sdk.UsageCostDetail, 0, len(items))
	inputIndex := -1
	outputIndex := -1
	for _, item := range items {
		switch {
		case isImageInputCostDetail(item):
			detail := normalizeTokenCostDetail(item, "input_tokens", "输入 Token")
			if inputIndex >= 0 {
				merged[inputIndex] = mergeCostDetail(merged[inputIndex], detail)
			} else {
				inputIndex = len(merged)
				merged = append(merged, detail)
			}
		case isImageCostDetail(item):
			detail := normalizeTokenCostDetail(item, "output_tokens", "输出 Token")
			if outputIndex >= 0 {
				merged[outputIndex] = mergeCostDetail(merged[outputIndex], detail)
			} else {
				outputIndex = len(merged)
				merged = append(merged, detail)
			}
		default:
			detail := item
			if isInputTokenCostDetail(detail) {
				if inputIndex >= 0 {
					merged[inputIndex] = mergeCostDetail(merged[inputIndex], detail)
					continue
				}
				inputIndex = len(merged)
			}
			if isOutputTokenCostDetail(detail) {
				if outputIndex >= 0 {
					merged[outputIndex] = mergeCostDetail(merged[outputIndex], detail)
					continue
				}
				outputIndex = len(merged)
			}
			merged = append(merged, detail)
		}
	}
	return merged
}

func normalizeTokenCostDetail(item sdk.UsageCostDetail, key, label string) sdk.UsageCostDetail {
	item.Key = key
	item.Label = label
	item.Metadata = tokenCostMetadata(item.Metadata)
	return item
}

func mergeCostDetail(base, extra sdk.UsageCostDetail) sdk.UsageCostDetail {
	base.AccountCost += extra.AccountCost
	base.UserCost += extra.UserCost
	if base.Currency == "" {
		base.Currency = extra.Currency
	}
	if len(base.Metadata) == 0 {
		base.Metadata = extra.Metadata
	}
	if base.AccountCost > 0 && base.UserCost > 0 {
		base.BillingMultiplier = base.UserCost / base.AccountCost
	}
	return base
}

func isInputTokenCostDetail(item sdk.UsageCostDetail) bool {
	key := strings.ToLower(strings.TrimSpace(item.Key))
	key = strings.ReplaceAll(key, "-", "_")
	return (key == "input_tokens" || key == "input" || key == "prompt_tokens") && !isImageInputCostDetail(item)
}

func isOutputTokenCostDetail(item sdk.UsageCostDetail) bool {
	key := strings.ToLower(strings.TrimSpace(item.Key))
	key = strings.ReplaceAll(key, "-", "_")
	return (key == "output_tokens" || key == "output" || key == "completion_tokens") && !isImageCostDetail(item)
}

func tokenCostMetadata(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string)
	for _, key := range []string{"unit", "unit_price", "billing_model"} {
		if value := strings.TrimSpace(in[key]); value != "" {
			out[key] = value
		}
	}
	if _, ok := out["unit"]; ok {
		out["unit"] = "USD/1M tokens"
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func cloneCostMetadata(in map[string]string) map[string]string {
	out := make(map[string]string, len(in)+1)
	for k, v := range in {
		out[k] = v
	}
	return out
}

func parseCostMetadataPositiveInt(metadata map[string]string, key string) int {
	if len(metadata) == 0 {
		return 0
	}
	value, err := strconv.Atoi(strings.TrimSpace(metadata[key]))
	if err != nil || value <= 0 {
		return 0
	}
	return value
}

func applyUsageCharges(ctx context.Context, tx *ent.Tx, batch []UsageRecord, refs *usageLogRefs, metered map[int]meteredRecord) error {
	// 在同一事务中扣费 —— 三个独立累加器：
	// - User.balance：按 actual_cost 扣减（metered 中的记录已改记订阅账本，跳过）。
	// - APIKey.used_quota：按 billed_cost 累加。
	// - APIKey.used_quota_actual：按 actual_cost 累加。
	userActualCosts := make(map[int]float64)
	keyBilledCosts := make(map[int]float64)
	keyActualCosts := make(map[int]float64)

	for i, rec := range batch {
		if rec.ActualCost > 0 && refs.hasUser(rec.UserID) {
			if _, onSubscription := metered[i]; !onSubscription {
				userActualCosts[rec.UserID] += rec.ActualCost
			}
			if refs.hasAPIKey(rec.APIKeyID) {
				keyActualCosts[rec.APIKeyID] += rec.ActualCost
			}
		}
		if refs.hasAPIKey(rec.APIKeyID) && rec.BilledCost > 0 {
			keyBilledCosts[rec.APIKeyID] += rec.BilledCost
		}
	}

	for userID, cost := range userActualCosts {
		if err := tx.User.UpdateOneID(userID).
			AddBalance(-cost).
			Exec(ctx); err != nil {
			return fmt.Errorf("扣减用户余额失败 user_id=%d cost=%.8f: %w", userID, cost, err)
		}
	}

	// APIKey 双累加器：billed 和 actual 都更新（key 集合相同，合并一次 update 调用）
	// APIKeyID == 0 表示插件经 Host 调用发起的请求（无 API Key），跳过 APIKey 累加。
	keyIDs := make(map[int]struct{}, len(keyBilledCosts))
	for k := range keyBilledCosts {
		keyIDs[k] = struct{}{}
	}
	for k := range keyActualCosts {
		keyIDs[k] = struct{}{}
	}
	for keyID := range keyIDs {
		if keyID == 0 {
			continue
		}
		update := tx.APIKey.UpdateOneID(keyID)
		if billed := keyBilledCosts[keyID]; billed > 0 {
			update = update.AddUsedQuota(billed)
		}
		if actual := keyActualCosts[keyID]; actual > 0 {
			update = update.AddUsedQuotaActual(actual)
		}
		if err := update.Exec(ctx); err != nil {
			return fmt.Errorf("更新 API Key 用量失败 key_id=%d: %w", keyID, err)
		}
	}
	return nil
}
