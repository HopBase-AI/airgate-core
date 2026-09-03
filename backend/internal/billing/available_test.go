package billing

import "testing"

func TestAvailableBalanceNeverRevealsOwnerBalanceWhenKeyHasQuota(t *testing.T) {
	cases := []struct {
		name                       string
		balance, quota, used       float64
		wantAvailable, wantKeyLeft float64
	}{
		{"无上限：回主账号余额", 12.5, 0, 0, 12.5, 12.5},
		{"无上限且余额为负：夹到 0", -3, 0, 0, 0, 0},
		{"有上限：只回 key 剩余，哪怕主账号余额更低", 1, 50, 20, 30, 30},
		{"有上限且已超：0", 100, 50, 60, 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			available, keyLeft := AvailableBalance(tc.balance, tc.quota, tc.used)
			if available != tc.wantAvailable || keyLeft != tc.wantKeyLeft {
				t.Fatalf("AvailableBalance(%v,%v,%v) = (%v,%v), want (%v,%v)", tc.balance, tc.quota, tc.used, available, keyLeft, tc.wantAvailable, tc.wantKeyLeft)
			}
		})
	}
}

func TestCapByMemberQuota(t *testing.T) {
	cases := []struct {
		name                     string
		available, mQuota, mUsed float64
		want                     float64
	}{
		{"成员不限额：原样", 30, 0, 999, 30},
		{"成员剩余更小：压到成员剩余", 30, 20, 5, 15},
		{"成员剩余更大：保持原值", 30, 100, 5, 30},
		{"成员超额：0", 30, 20, 25, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := CapByMemberQuota(tc.available, tc.mQuota, tc.mUsed); got != tc.want {
				t.Fatalf("CapByMemberQuota(%v,%v,%v) = %v, want %v", tc.available, tc.mQuota, tc.mUsed, got, tc.want)
			}
		})
	}
}
