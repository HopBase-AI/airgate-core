package subscription

import (
	"context"
	"time"
)

// 旧桩仓储补齐新增接口方法：默认按「无数据」语义返回，具体用例用 memoryRepository。

func (s subscriptionStubRepository) FindByID(context.Context, int) (Subscription, error) {
	return Subscription{}, ErrSubscriptionNotFound
}

func (s subscriptionStubRepository) FindActiveByUserGroup(context.Context, int, int) (Subscription, error) {
	return Subscription{}, ErrSubscriptionNotFound
}

func (s subscriptionStubRepository) FindPlan(context.Context, int) (Plan, error) {
	return Plan{}, ErrPlanNotFound
}

func (s subscriptionStubRepository) ListPlans(context.Context) ([]Plan, error) {
	return nil, nil
}

func (s subscriptionStubRepository) ApplyRollover(context.Context, int, time.Time, RolloverInput) (bool, error) {
	return false, nil
}

func (s subscriptionStubRepository) MarkExpired(context.Context, int) error {
	return nil
}

func (s subscriptionStubRepository) Purchase(context.Context, PurchaseTx) (Subscription, error) {
	return Subscription{}, ErrPlanNotFound
}

func (s subscriptionStubRepository) Topup(context.Context, TopupTx) (Subscription, error) {
	return Subscription{}, ErrSubscriptionNotFound
}
