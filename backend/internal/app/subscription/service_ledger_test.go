package subscription

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DouDOU-start/airgate-core/internal/billing"
)

// memoryRepository 内存仓储：覆盖点数账本相关用例（到期/换期/购买/加购/准入）。
type memoryRepository struct {
	subs     map[int]*Subscription
	plans    map[int]Plan
	balance  map[int]float64
	nextID   int
	rollover int
	debits   []float64
}

func newMemoryRepository() *memoryRepository {
	return &memoryRepository{subs: map[int]*Subscription{}, plans: map[int]Plan{}, balance: map[int]float64{}, nextID: 1}
}

func (m *memoryRepository) put(sub Subscription) int {
	if sub.ID == 0 {
		sub.ID = m.nextID
		m.nextID++
	}
	if sub.Status == "" {
		sub.Status = "active"
	}
	if plan, ok := m.plans[sub.GroupID]; ok {
		sub.GroupName = plan.Name
		sub.GroupQuotas = plan.Quotas
	}
	copy := sub
	m.subs[sub.ID] = &copy
	return sub.ID
}

func (m *memoryRepository) ListByUser(context.Context, UserListFilter) ([]Subscription, int64, error) {
	return nil, 0, nil
}
func (m *memoryRepository) ListAdmin(context.Context, AdminListFilter) ([]Subscription, int64, error) {
	return nil, 0, nil
}
func (m *memoryRepository) Create(context.Context, CreateInput) (Subscription, error) {
	return Subscription{}, errors.New("unused")
}
func (m *memoryRepository) BulkCreate(context.Context, BulkCreateInput) (int, error) {
	return 0, errors.New("unused")
}
func (m *memoryRepository) Update(context.Context, int, UpdateInput) (Subscription, error) {
	return Subscription{}, errors.New("unused")
}
func (m *memoryRepository) ListActiveByUser(_ context.Context, userID int) ([]Subscription, error) {
	var out []Subscription
	for id := m.nextID - 1; id >= 1; id-- {
		sub, ok := m.subs[id]
		if ok && sub.UserID == userID && sub.Status == "active" {
			out = append(out, *sub)
		}
	}
	return out, nil
}
func (m *memoryRepository) FindByID(_ context.Context, id int) (Subscription, error) {
	if sub, ok := m.subs[id]; ok {
		return *sub, nil
	}
	return Subscription{}, ErrSubscriptionNotFound
}
func (m *memoryRepository) FindActiveByUserGroup(_ context.Context, userID, groupID int) (Subscription, error) {
	for id := m.nextID - 1; id >= 1; id-- {
		sub, ok := m.subs[id]
		if ok && sub.UserID == userID && sub.GroupID == groupID && sub.Status != "expired" {
			return *sub, nil
		}
	}
	return Subscription{}, ErrSubscriptionNotFound
}
func (m *memoryRepository) FindPlan(_ context.Context, groupID int) (Plan, error) {
	if plan, ok := m.plans[groupID]; ok {
		return plan, nil
	}
	return Plan{}, ErrPlanNotFound
}
func (m *memoryRepository) ListPlans(context.Context) ([]Plan, error) {
	out := make([]Plan, 0, len(m.plans))
	for _, plan := range m.plans {
		if !plan.Delisted {
			out = append(out, plan)
		}
	}
	return out, nil
}
func (m *memoryRepository) ApplyRollover(_ context.Context, id int, expect time.Time, input RolloverInput) (bool, error) {
	sub, ok := m.subs[id]
	if !ok || !sub.PeriodEnd.Equal(expect) {
		return false, nil
	}
	m.rollover++
	sub.PeriodStart, sub.PeriodEnd = input.PeriodStart, input.PeriodEnd
	sub.CreditsUsed, sub.ImagesUsed = 0, 0
	sub.ExtraCredits = input.ExtraCredits
	return true, nil
}
func (m *memoryRepository) MarkExpired(_ context.Context, id int) error {
	if sub, ok := m.subs[id]; ok {
		sub.Status = "expired"
		return nil
	}
	return ErrSubscriptionNotFound
}
func (m *memoryRepository) debit(userID int, price float64) error {
	if m.balance[userID] < price {
		return ErrInsufficientBalance
	}
	m.balance[userID] -= price
	m.debits = append(m.debits, price)
	return nil
}
func (m *memoryRepository) Purchase(_ context.Context, tx PurchaseTx) (Subscription, error) {
	if err := m.debit(tx.UserID, tx.Price); err != nil {
		return Subscription{}, err
	}
	if tx.ExistingID > 0 {
		sub := m.subs[tx.ExistingID]
		sub.ExpiresAt = tx.ExpiresAt
		sub.BillingCycle = tx.BillingCycle
		return *sub, nil
	}
	id := m.put(Subscription{
		UserID: tx.UserID, GroupID: tx.GroupID, EffectiveAt: tx.EffectiveAt, ExpiresAt: tx.ExpiresAt,
		PeriodStart: tx.PeriodStart, PeriodEnd: tx.PeriodEnd, BillingCycle: tx.BillingCycle,
	})
	return *m.subs[id], nil
}
func (m *memoryRepository) Topup(_ context.Context, tx TopupTx) (Subscription, error) {
	if err := m.debit(tx.UserID, tx.Price); err != nil {
		return Subscription{}, err
	}
	sub := m.subs[tx.SubscriptionID]
	sub.ExtraCredits += tx.Credits
	return *sub, nil
}

var testPlanQuotas = billing.PlanQuotas{
	MonthlyCredits: 1000, CreditsPerUnit: 10000, PerRequestCredits: 50, ImageMonthlyLimit: 2, VideoEnabled: false,
	PriceMonthly: 128, PriceAnnual: 1308, TopupCredits: 150, TopupPrice: 20,
}

func newLedgerService(t *testing.T, now time.Time) (*Service, *memoryRepository) {
	t.Helper()
	repo := newMemoryRepository()
	repo.plans[7] = Plan{GroupID: 7, Name: "主力", Quotas: testPlanQuotas.ToMap()}
	svc := NewService(repo)
	svc.now = func() time.Time { return now }
	return svc, repo
}

func TestEntitleRequiresActiveSubscription(t *testing.T) {
	now := date(2026, 3, 10, 12)
	svc, repo := newLedgerService(t, now)
	ctx := context.Background()

	if _, err := svc.Entitle(ctx, 1, 7, testPlanQuotas, billing.RequestKindChat); !errors.Is(err, ErrSubscriptionRequired) {
		t.Fatalf("无订阅应拒绝 ErrSubscriptionRequired，得到 %v", err)
	}

	id := repo.put(Subscription{UserID: 1, GroupID: 7, EffectiveAt: date(2026, 1, 15, 0), ExpiresAt: date(2026, 3, 1, 0)})
	if _, err := svc.Entitle(ctx, 1, 7, testPlanQuotas, billing.RequestKindChat); !errors.Is(err, ErrSubscriptionExpired) {
		t.Fatalf("过期应拒绝 ErrSubscriptionExpired，得到 %v", err)
	}
	if repo.subs[id].Status != "expired" {
		t.Fatal("到期应惰性落库为 expired")
	}

	repo.put(Subscription{UserID: 1, GroupID: 7, EffectiveAt: date(2026, 1, 15, 0), ExpiresAt: date(2026, 6, 1, 0), Status: "suspended"})
	if _, err := svc.Entitle(ctx, 1, 7, testPlanQuotas, billing.RequestKindChat); !errors.Is(err, ErrSubscriptionSuspended) {
		t.Fatalf("暂停应拒绝，得到 %v", err)
	}
}

func TestEntitleRollsOverPeriodAndChecksQuotas(t *testing.T) {
	now := date(2026, 3, 10, 12)
	svc, repo := newLedgerService(t, now)
	ctx := context.Background()
	id := repo.put(Subscription{
		UserID: 1, GroupID: 7, EffectiveAt: date(2026, 1, 15, 0), ExpiresAt: date(2027, 1, 15, 0),
		PeriodStart: date(2026, 1, 15, 0), PeriodEnd: date(2026, 2, 15, 0),
		CreditsUsed: 1300, ExtraCredits: 500, ImagesUsed: 2,
	})

	ent, err := svc.Entitle(ctx, 1, 7, testPlanQuotas, billing.RequestKindChat)
	if err != nil {
		t.Fatalf("换期后应放行，得到 %v", err)
	}
	sub := repo.subs[id]
	if !sub.PeriodStart.Equal(date(2026, 2, 15, 0)) || !sub.PeriodEnd.Equal(date(2026, 3, 15, 0)) {
		t.Fatalf("计量期应推进到 [2-15, 3-15)，得到 [%s, %s)", sub.PeriodStart, sub.PeriodEnd)
	}
	if sub.CreditsUsed != 0 || sub.ImagesUsed != 0 {
		t.Fatalf("换期应归零本期用量: %+v", sub)
	}
	if sub.ExtraCredits != 200 {
		t.Fatalf("超额 300 应先吃加购包，剩 200，得到 %v", sub.ExtraCredits)
	}
	if ent.Remaining != 1200 || ent.Unlimited {
		t.Fatalf("剩余应为 1000+200=1200，得到 %+v", ent)
	}
	if repo.rollover != 1 {
		t.Fatalf("应只换期一次，得到 %d", repo.rollover)
	}

	// 同期内再次准入不再换期
	if _, err := svc.Entitle(ctx, 1, 7, testPlanQuotas, billing.RequestKindChat); err != nil || repo.rollover != 1 {
		t.Fatalf("同期内不应重复换期: err=%v rollover=%d", err, repo.rollover)
	}

	// 点数用尽
	sub.CreditsUsed = 1200
	if _, err := svc.Entitle(ctx, 1, 7, testPlanQuotas, billing.RequestKindChat); !errors.Is(err, ErrCreditsExhausted) {
		t.Fatalf("点数用尽应拒绝，得到 %v", err)
	}
	sub.CreditsUsed = 10

	// 视频未开放 / 张数上限
	if _, err := svc.Entitle(ctx, 1, 7, testPlanQuotas, billing.RequestKindVideo); !errors.Is(err, ErrVideoNotIncluded) {
		t.Fatalf("视频未开放应拒绝，得到 %v", err)
	}
	sub.ImagesUsed = 2
	if _, err := svc.Entitle(ctx, 1, 7, testPlanQuotas, billing.RequestKindImage); !errors.Is(err, ErrImageLimitReached) {
		t.Fatalf("张数达上限应拒绝，得到 %v", err)
	}
	sub.ImagesUsed = 1
	if _, err := svc.Entitle(ctx, 1, 7, testPlanQuotas, billing.RequestKindImage); err != nil {
		t.Fatalf("张数未达上限应放行，得到 %v", err)
	}

	// 不限量套餐：点数不判
	unlimited := billing.PlanQuotas{VideoEnabled: true}
	sub.CreditsUsed = 999999
	if ent, err := svc.Entitle(ctx, 1, 7, unlimited, billing.RequestKindVideo); err != nil || !ent.Unlimited {
		t.Fatalf("不限量应放行: err=%v ent=%+v", err, ent)
	}
}

func TestEntitleInitializesLegacyRowsWithoutPeriod(t *testing.T) {
	now := date(2026, 3, 10, 12)
	svc, repo := newLedgerService(t, now)
	id := repo.put(Subscription{UserID: 1, GroupID: 7, EffectiveAt: date(2026, 1, 31, 9), ExpiresAt: date(2027, 1, 1, 0)})
	if _, err := svc.Entitle(context.Background(), 1, 7, testPlanQuotas, billing.RequestKindChat); err != nil {
		t.Fatalf("历史行应惰性初始化后放行，得到 %v", err)
	}
	sub := repo.subs[id]
	if !sub.PeriodStart.Equal(date(2026, 2, 28, 9)) || !sub.PeriodEnd.Equal(date(2026, 3, 31, 9)) {
		t.Fatalf("历史行计量期应按 1/31 锚定到 [2-28, 3-31)，得到 [%s, %s)", sub.PeriodStart, sub.PeriodEnd)
	}
}

func TestPurchaseCreatesThenRenews(t *testing.T) {
	now := date(2026, 3, 10, 12)
	svc, repo := newLedgerService(t, now)
	ctx := context.Background()
	repo.balance[1] = 200

	if _, err := svc.Purchase(ctx, PurchaseInput{UserID: 1, GroupID: 7, Cycle: "weekly"}); !errors.Is(err, ErrInvalidBillingCycle) {
		t.Fatalf("非法周期应拒绝，得到 %v", err)
	}
	if _, err := svc.Purchase(ctx, PurchaseInput{UserID: 1, GroupID: 99, Cycle: BillingCycleMonthly}); !errors.Is(err, ErrPlanNotFound) {
		t.Fatalf("不存在的套餐应拒绝，得到 %v", err)
	}
	if _, err := svc.Purchase(ctx, PurchaseInput{UserID: 1, GroupID: 7, Cycle: BillingCycleAnnual}); !errors.Is(err, ErrInsufficientBalance) {
		t.Fatalf("余额 200 买不起年付 1308，得到 %v", err)
	}

	sub, err := svc.Purchase(ctx, PurchaseInput{UserID: 1, GroupID: 7, Cycle: BillingCycleMonthly})
	if err != nil {
		t.Fatalf("月付购买失败: %v", err)
	}
	if repo.balance[1] != 72 {
		t.Fatalf("应扣 128，余额应为 72，得到 %v", repo.balance[1])
	}
	if !sub.EffectiveAt.Equal(now) || !sub.ExpiresAt.Equal(date(2026, 4, 10, 12)) {
		t.Fatalf("新订阅期限错误: %s → %s", sub.EffectiveAt, sub.ExpiresAt)
	}
	if !sub.PeriodStart.Equal(now) || !sub.PeriodEnd.Equal(date(2026, 4, 10, 12)) || sub.BillingCycle != BillingCycleMonthly {
		t.Fatalf("首个计量期错误: %+v", sub)
	}

	// 续期：在原到期日上顺延，账本不动
	repo.balance[1] = 2000
	repo.subs[sub.ID].CreditsUsed = 321
	renewed, err := svc.Purchase(ctx, PurchaseInput{UserID: 1, GroupID: 7, Cycle: BillingCycleAnnual})
	if err != nil {
		t.Fatalf("续期失败: %v", err)
	}
	if renewed.ID != sub.ID || !renewed.ExpiresAt.Equal(date(2027, 4, 10, 12)) || renewed.BillingCycle != BillingCycleAnnual {
		t.Fatalf("续期应延长同一条订阅 12 个月: %+v", renewed)
	}
	if renewed.CreditsUsed != 321 {
		t.Fatal("续期不应动本期账本")
	}
	if repo.balance[1] != 2000-1308 {
		t.Fatalf("年付应扣 1308，余额 %v", repo.balance[1])
	}

	// 下架套餐不可购买
	plan := repo.plans[7]
	plan.Delisted = true
	repo.plans[7] = plan
	if _, err := svc.Purchase(ctx, PurchaseInput{UserID: 1, GroupID: 7, Cycle: BillingCycleMonthly}); !errors.Is(err, ErrPlanNotPurchasable) {
		t.Fatalf("下架套餐应拒绝，得到 %v", err)
	}
}

func TestPurchaseAfterExpiryStartsFreshSubscription(t *testing.T) {
	now := date(2026, 3, 10, 12)
	svc, repo := newLedgerService(t, now)
	repo.balance[1] = 500
	oldID := repo.put(Subscription{UserID: 1, GroupID: 7, EffectiveAt: date(2026, 1, 1, 0), ExpiresAt: date(2026, 2, 1, 0), CreditsUsed: 900})

	sub, err := svc.Purchase(context.Background(), PurchaseInput{UserID: 1, GroupID: 7, Cycle: BillingCycleMonthly})
	if err != nil {
		t.Fatalf("过期后重购失败: %v", err)
	}
	if sub.ID == oldID || sub.CreditsUsed != 0 || !sub.EffectiveAt.Equal(now) {
		t.Fatalf("过期后应新开订阅并重置账本: %+v", sub)
	}
	if repo.subs[oldID].Status != "expired" {
		t.Fatal("旧订阅应标记 expired")
	}
}

func TestTopupAddsExtraCreditsAndGuardsOwnership(t *testing.T) {
	now := date(2026, 3, 10, 12)
	svc, repo := newLedgerService(t, now)
	ctx := context.Background()
	repo.balance[1] = 30
	id := repo.put(Subscription{
		UserID: 1, GroupID: 7, EffectiveAt: date(2026, 3, 1, 0), ExpiresAt: date(2026, 4, 1, 0),
		PeriodStart: date(2026, 3, 1, 0), PeriodEnd: date(2026, 4, 1, 0), ExtraCredits: 10,
	})

	if _, err := svc.Topup(ctx, TopupInput{UserID: 2, SubscriptionID: id}); !errors.Is(err, ErrSubscriptionNotFound) {
		t.Fatalf("别人的订阅应当不存在，得到 %v", err)
	}
	sub, err := svc.Topup(ctx, TopupInput{UserID: 1, SubscriptionID: id})
	if err != nil {
		t.Fatalf("加购失败: %v", err)
	}
	if sub.ExtraCredits != 160 || repo.balance[1] != 10 {
		t.Fatalf("加购应 +150 点并扣 20：%+v 余额 %v", sub, repo.balance[1])
	}
	if _, err := svc.Topup(ctx, TopupInput{UserID: 1, SubscriptionID: id}); !errors.Is(err, ErrInsufficientBalance) {
		t.Fatalf("余额 10 买不起 20 的加购包，得到 %v", err)
	}

	noPack := repo.plans[7]
	noPack.Quotas = billing.PlanQuotas{MonthlyCredits: 1000, PriceMonthly: 1}.ToMap()
	repo.plans[7] = noPack
	repo.subs[id].GroupQuotas = noPack.Quotas
	repo.balance[1] = 1000
	if _, err := svc.Topup(ctx, TopupInput{UserID: 1, SubscriptionID: id}); !errors.Is(err, ErrTopupUnavailable) {
		t.Fatalf("无加购包应拒绝，得到 %v", err)
	}
}

func TestProgressAndPlansReflectLedger(t *testing.T) {
	now := date(2026, 3, 10, 12)
	svc, repo := newLedgerService(t, now)
	ctx := context.Background()
	repo.plans[8] = Plan{GroupID: 8, Name: "下架", Delisted: true, Quotas: testPlanQuotas.ToMap()}
	repo.put(Subscription{
		UserID: 1, GroupID: 7, EffectiveAt: date(2026, 3, 1, 0), ExpiresAt: date(2026, 4, 1, 0),
		PeriodStart: date(2026, 3, 1, 0), PeriodEnd: date(2026, 4, 1, 0), CreditsUsed: 250, ExtraCredits: 150, ImagesUsed: 1,
	})
	repo.put(Subscription{UserID: 1, GroupID: 7, EffectiveAt: date(2025, 1, 1, 0), ExpiresAt: date(2025, 2, 1, 0)})

	progress, err := svc.SubscriptionProgress(ctx, 1)
	if err != nil {
		t.Fatalf("进度查询失败: %v", err)
	}
	if len(progress) != 1 {
		t.Fatalf("过期订阅应被剔除，只剩 1 条，得到 %d", len(progress))
	}
	p := progress[0]
	if p.Credits.Used != 250 || p.Credits.Limit != 1000 || !p.Credits.Reset.Equal(date(2026, 4, 1, 0)) || p.ExtraCredits != 150 {
		t.Fatalf("点数窗口错误: %+v", p)
	}
	if p.Images == nil || p.Images.Used != 1 || p.Images.Limit != 2 || p.VideoEnabled || !p.TopupAvailable || p.PerRequestCredits != 50 {
		t.Fatalf("权益投影错误: %+v", p)
	}

	plans, err := svc.Plans(ctx, 1)
	if err != nil {
		t.Fatalf("套餐查询失败: %v", err)
	}
	if len(plans) != 1 || plans[0].GroupID != 7 || plans[0].Current == nil || plans[0].Current.CreditsUsed != 250 {
		t.Fatalf("套餐列表应只含上架套餐并附带当前订阅: %+v", plans)
	}
}
