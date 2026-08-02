package scheduler

import (
	"context"
	"errors"
	"testing"

	"github.com/DouDOU-start/airgate-core/ent"
	"github.com/DouDOU-start/airgate-core/ent/account"
)

func TestExcludeAccountsDoesNotMutateCandidates(t *testing.T) {
	t.Parallel()

	candidates := []*ent.Account{{ID: 1}, {ID: 2}, {ID: 3}}
	got := excludeAccounts(candidates, []int{2})

	if len(got) != 2 || got[0].ID != 1 || got[1].ID != 3 {
		t.Fatalf("excludeAccounts result = %+v, want IDs [1 3]", got)
	}
	if len(candidates) != 3 || candidates[0].ID != 1 || candidates[1].ID != 2 || candidates[2].ID != 3 {
		t.Fatalf("candidates mutated to %+v, want original IDs [1 2 3]", candidates)
	}
}

func TestNormalizeGroupLookupErrorPreservesCancellation(t *testing.T) {
	t.Parallel()

	for _, err := range []error{context.Canceled, context.DeadlineExceeded} {
		got := normalizeGroupLookupError(err)
		if !errors.Is(got, err) {
			t.Fatalf("normalizeGroupLookupError(%v) = %v, want original error", err, got)
		}
	}
}

func TestNormalizeGroupLookupErrorWrapsGenericError(t *testing.T) {
	t.Parallel()

	orig := errors.New("db offline")
	got := normalizeGroupLookupError(orig)
	if errors.Is(got, ErrGroupNotFound) {
		t.Fatalf("normalizeGroupLookupError(%v) = %v, want generic query error", orig, got)
	}
	if got.Error() != "查询分组失败: db offline" {
		t.Fatalf("normalizeGroupLookupError(%v) = %q, want %q", orig, got.Error(), "查询分组失败: db offline")
	}
}

func TestNormalizeGroupAccountsLookupErrorPreservesCancellation(t *testing.T) {
	t.Parallel()

	for _, err := range []error{context.Canceled, context.DeadlineExceeded} {
		got := normalizeGroupAccountsLookupError(err)
		if !errors.Is(got, err) {
			t.Fatalf("normalizeGroupAccountsLookupError(%v) = %v, want original error", err, got)
		}
	}
}

func TestNormalizeGroupAccountsLookupErrorWrapsGenericError(t *testing.T) {
	t.Parallel()

	orig := errors.New("db offline")
	got := normalizeGroupAccountsLookupError(orig)
	if got.Error() != "查询分组账户失败: db offline" {
		t.Fatalf("normalizeGroupAccountsLookupError(%v) = %q, want %q", orig, got.Error(), "查询分组账户失败: db offline")
	}
}

func TestClassifyRoutedAccounts(t *testing.T) {
	t.Parallel()

	active := func(id int) *ent.Account {
		return &ent.Account{ID: id, State: account.StateActive}
	}
	disabled := func(id int) *ent.Account {
		return &ent.Account{ID: id, State: account.StateDisabled}
	}

	tests := []struct {
		name     string
		accounts []*ent.Account
		routing  map[string][]int64
		model    string
		wantErr  error
		wantIDs  []int
	}{
		{
			name:     "empty group is offline",
			accounts: nil,
			wantErr:  ErrGroupOffline,
		},
		{
			// Norman 的真实场景：分组成员还在，但被逐个 disabled 后整组归零。
			name:     "all members disabled is offline",
			accounts: []*ent.Account{disabled(1), disabled(2)},
			wantErr:  ErrGroupOffline,
		},
		{
			name:     "routing filters everything out",
			accounts: []*ent.Account{active(1)},
			routing:  map[string][]int64{"other-model": {1}},
			model:    "claude-opus-5",
			wantErr:  ErrModelNotServed,
		},
		{
			name:     "one active member is servable",
			accounts: []*ent.Account{disabled(1), active(2)},
			wantIDs:  []int{1, 2},
		},
		{
			// rate_limited / degraded 会自行到期恢复，属于容量问题，不能判成永久下线。
			name:     "rate limited member is not offline",
			accounts: []*ent.Account{{ID: 1, State: account.StateRateLimited}},
			wantIDs:  []int{1},
		},
		{
			name:     "degraded member is not offline",
			accounts: []*ent.Account{{ID: 1, State: account.StateDegraded}},
			wantIDs:  []int{1},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := classifyRoutedAccounts(tt.accounts, tt.routing, tt.model)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("err = %v, want %v", err, tt.wantErr)
				}
				// 分类错误必须仍满足既有的 ErrNoAvailableAccount 判定。
				if !errors.Is(err, ErrNoAvailableAccount) {
					t.Fatalf("err = %v, want it to wrap ErrNoAvailableAccount", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("err = %v, want nil", err)
			}
			if len(got) != len(tt.wantIDs) {
				t.Fatalf("got %d accounts, want %d", len(got), len(tt.wantIDs))
			}
			for i, id := range tt.wantIDs {
				if got[i].ID != id {
					t.Fatalf("account[%d].ID = %d, want %d", i, got[i].ID, id)
				}
			}
		})
	}
}

func TestClassifyRoutedAccountTiers(t *testing.T) {
	t.Parallel()

	pool := func(id int, state account.State) *ent.Account {
		return &ent.Account{ID: id, State: state, UpstreamIsPool: true}
	}
	regular := func(id int, state account.State) *ent.Account {
		return &ent.Account{ID: id, State: state}
	}

	tests := []struct {
		name             string
		accounts         []*ent.Account
		routing          map[string][]int64
		wantPrimary      []int
		wantPoolFallback []int
		wantErr          error
	}{
		{
			name:             "pool route exposes same group pool standby",
			accounts:         []*ent.Account{pool(1, account.StateActive), pool(2, account.StateActive), regular(3, account.StateActive)},
			routing:          map[string][]int64{"gpt-5.6": {1}},
			wantPrimary:      []int{1},
			wantPoolFallback: []int{2},
		},
		{
			name:             "disabled pool primary is recoverable through active standby",
			accounts:         []*ent.Account{pool(1, account.StateDisabled), pool(2, account.StateActive)},
			routing:          map[string][]int64{"gpt-5.6": {1}},
			wantPrimary:      []int{1},
			wantPoolFallback: []int{2},
		},
		{
			name:        "regular route remains strict",
			accounts:    []*ent.Account{regular(1, account.StateActive), pool(2, account.StateActive)},
			routing:     map[string][]int64{"gpt-5.6": {1}},
			wantPrimary: []int{1},
		},
		{
			name:     "explicitly disabled model stays disabled",
			accounts: []*ent.Account{pool(1, account.StateActive), pool(2, account.StateActive)},
			routing:  map[string][]int64{"gpt-5.6": {}},
			wantErr:  ErrModelNotServed,
		},
		{
			name:     "all pool candidates disabled is offline",
			accounts: []*ent.Account{pool(1, account.StateDisabled), pool(2, account.StateDisabled)},
			routing:  map[string][]int64{"gpt-5.6": {1}},
			wantErr:  ErrGroupOffline,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tiers, err := classifyRoutedAccountTiers(tt.accounts, tt.routing, "gpt-5.6")
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("err = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("classify tiers: %v", err)
			}
			assertAccountIDs(t, tiers.primary, tt.wantPrimary)
			assertAccountIDs(t, tiers.poolFallback, tt.wantPoolFallback)
		})
	}
}

func assertAccountIDs(t *testing.T, accounts []*ent.Account, want []int) {
	t.Helper()
	if len(accounts) != len(want) {
		t.Fatalf("account count = %d, want %d; accounts=%+v", len(accounts), len(want), accounts)
	}
	for i, id := range want {
		if accounts[i].ID != id {
			t.Fatalf("accounts[%d].ID = %d, want %d", i, accounts[i].ID, id)
		}
	}
}
