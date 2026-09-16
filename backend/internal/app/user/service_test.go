package user

import (
	"context"
	"testing"
	"time"
)

func TestAdjustBalanceRejectsInvalidAction(t *testing.T) {
	service := NewService(stubRepository{
		findByID: func() (User, error) {
			return User{ID: 1, Balance: 10}, nil
		},
	})

	_, err := service.AdjustBalance(t.Context(), 1, BalanceChange{Action: "noop", Amount: 1})
	if err != ErrInvalidBalanceAction {
		t.Fatalf("expected ErrInvalidBalanceAction, got %v", err)
	}
}

// TestAdjustBalanceRejectsInsufficientBalance 余额不足由 store 在原子 SQL 里判定，
// service 只负责把哨兵错误原样透出（不再自己读余额比较）。
func TestAdjustBalanceRejectsInsufficientBalance(t *testing.T) {
	service := NewService(stubRepository{
		updateBalance: func(_ int, update BalanceUpdate) (BalanceChangeResult, error) {
			if update.Action != "subtract" || update.Amount != 10 {
				t.Fatalf("UpdateBalance received %+v, want subtract 10", update)
			}
			return BalanceChangeResult{}, ErrInsufficientBalance
		},
	})

	_, err := service.AdjustBalance(t.Context(), 1, BalanceChange{Action: "subtract", Amount: 10})
	if err != ErrInsufficientBalance {
		t.Fatalf("expected ErrInsufficientBalance, got %v", err)
	}
}

// TestAdjustBalanceDelegatesDeltaToRepository service 不再预读余额、不在 Go 里算 after，
// 动作与金额原样交给 store 做原子增量；返回值取 store 事务内读回的结果。
func TestAdjustBalanceDelegatesDeltaToRepository(t *testing.T) {
	findCalls := 0
	service := NewService(stubRepository{
		findByID: func() (User, error) {
			findCalls++
			return User{ID: 1, Balance: 999}, nil
		},
		updateBalance: func(id int, update BalanceUpdate) (BalanceChangeResult, error) {
			if id != 1 || update.Action != "add" || update.Amount != 2.5 || update.Remark != "recharge" || update.IdempotencyKey != "epay:1" {
				t.Fatalf("UpdateBalance received id=%d %+v", id, update)
			}
			return BalanceChangeResult{User: User{ID: 1, Balance: 12.5}, BeforeBalance: 10, AfterBalance: 12.5}, nil
		},
	})

	got, err := service.AdjustBalance(t.Context(), 1, BalanceChange{Action: "add", Amount: 2.5, Remark: "recharge", IdempotencyKey: "epay:1"})
	if err != nil {
		t.Fatalf("AdjustBalance returned error: %v", err)
	}
	if got.Balance != 12.5 {
		t.Fatalf("balance = %v, want 12.5 (store result)", got.Balance)
	}
	if findCalls != 0 {
		t.Fatalf("FindByID called %d times before update, want 0 (no read-compute-write)", findCalls)
	}
}

// TestAdjustBalanceIdempotentHitReturnsCurrentUser 幂等命中不报错，回读当前用户。
func TestAdjustBalanceIdempotentHitReturnsCurrentUser(t *testing.T) {
	service := NewService(stubRepository{
		findByID: func() (User, error) {
			return User{ID: 1, Balance: 7}, nil
		},
		updateBalance: func(_ int, _ BalanceUpdate) (BalanceChangeResult, error) {
			return BalanceChangeResult{}, ErrDuplicateBalanceChange
		},
	})

	got, err := service.AdjustBalance(t.Context(), 1, BalanceChange{Action: "add", Amount: 1, IdempotencyKey: "k"})
	if err != nil {
		t.Fatalf("AdjustBalance returned error: %v", err)
	}
	if got.Balance != 7 {
		t.Fatalf("balance = %v, want current 7", got.Balance)
	}
}

func TestListAPIKeysNormalizesPagination(t *testing.T) {
	service := NewService(stubRepository{
		listAPIKeys: func(_ context.Context, _ int, page, pageSize int) ([]APIKey, int64, error) {
			if page != 1 || pageSize != 20 {
				t.Fatalf("ListAPIKeys received page=%d pageSize=%d, want 1 and 20", page, pageSize)
			}
			return []APIKey{{ID: 1}}, 1, nil
		},
	})

	result, err := service.ListAPIKeys(t.Context(), 7, 0, 0, "")
	if err != nil {
		t.Fatalf("ListAPIKeys returned error: %v", err)
	}
	if result.Page != 1 || result.PageSize != 20 || result.Total != 1 || len(result.List) != 1 {
		t.Fatalf("unexpected ListAPIKeys result: %+v", result)
	}
}

type stubRepository struct {
	findByID      func() (User, error)
	updateBalance func(int, BalanceUpdate) (BalanceChangeResult, error)
	listAPIKeys   func(context.Context, int, int, int) ([]APIKey, int64, error)
}

func (s stubRepository) FindByID(_ context.Context, _ int, _ bool) (User, error) {
	if s.findByID == nil {
		return User{}, nil
	}
	return s.findByID()
}

func (s stubRepository) List(_ context.Context, _ ListFilter) ([]User, int64, error) {
	return nil, 0, nil
}
func (s stubRepository) EmailExists(_ context.Context, _ string) (bool, error) { return false, nil }
func (s stubRepository) ListWithGroupRateOverride(_ context.Context, _ int64) ([]GroupRateOverride, error) {
	return nil, nil
}

func (s stubRepository) ListAllGroupRateOverrides(_ context.Context) (map[int64][]GroupRateOverride, error) {
	return map[int64][]GroupRateOverride{}, nil
}
func (s stubRepository) Create(_ context.Context, _ Mutation) (User, error) { return User{}, nil }
func (s stubRepository) Update(_ context.Context, _ int, _ Mutation) (User, error) {
	return User{}, nil
}
func (s stubRepository) UpdateBalance(_ context.Context, id int, update BalanceUpdate) (BalanceChangeResult, error) {
	if s.updateBalance == nil {
		return BalanceChangeResult{}, nil
	}
	return s.updateBalance(id, update)
}
func (s stubRepository) Delete(_ context.Context, _ int) error { return nil }
func (s stubRepository) ListBalanceLogs(_ context.Context, _ int, _, _ int) ([]BalanceLog, int64, error) {
	return nil, 0, nil
}
func (s stubRepository) UpdateBalanceAlert(_ context.Context, _ int, _ float64) error { return nil }
func (s stubRepository) SetBalanceAlertNotified(_ context.Context, _ int, _ bool) error {
	return nil
}
func (s stubRepository) ListAPIKeys(ctx context.Context, userID, page, pageSize int, _ time.Time) ([]APIKey, int64, error) {
	if s.listAPIKeys == nil {
		return nil, 0, nil
	}
	return s.listAPIKeys(ctx, userID, page, pageSize)
}
func (s stubRepository) GetAPIKeyName(_ context.Context, _ int) (string, error) {
	return "", nil
}
func (s stubRepository) GetAPIKeyInfo(_ context.Context, _, _ int) (APIKeyBrief, error) {
	return APIKeyBrief{}, nil
}

func (s stubRepository) MembershipBrief(context.Context, int) (MembershipBrief, bool, error) {
	return MembershipBrief{}, false, nil
}
