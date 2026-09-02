package subscription

import (
	"testing"
	"time"

	"github.com/DouDOU-start/airgate-core/internal/billing"
)

func date(y int, m time.Month, d, h int) time.Time {
	return time.Date(y, m, d, h, 0, 0, 0, time.UTC)
}

func TestAddMonthsClampsToMonthEnd(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		from time.Time
		n    int
		want time.Time
	}{
		{"平月末夹紧", date(2026, 1, 31, 9), 1, date(2026, 2, 28, 9)},
		{"跨过短月后回到 31", date(2026, 1, 31, 9), 2, date(2026, 3, 31, 9)},
		{"闰年 2 月", date(2028, 1, 31, 9), 1, date(2028, 2, 29, 9)},
		{"跨年", date(2026, 11, 15, 9), 3, date(2027, 2, 15, 9)},
		{"年付 12 个月", date(2026, 3, 31, 9), 12, date(2027, 3, 31, 9)},
		{"负数回退", date(2026, 3, 31, 9), -1, date(2026, 2, 28, 9)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := AddMonths(tc.from, tc.n); !got.Equal(tc.want) {
				t.Fatalf("AddMonths(%s, %d) = %s, want %s", tc.from, tc.n, got, tc.want)
			}
		})
	}
}

func TestPeriodContainingAnchorsOnEffectiveDate(t *testing.T) {
	t.Parallel()
	anchor := date(2026, 1, 31, 9)
	cases := []struct {
		name      string
		now       time.Time
		wantStart time.Time
		wantEnd   time.Time
	}{
		{"首期", date(2026, 2, 10, 0), date(2026, 1, 31, 9), date(2026, 2, 28, 9)},
		{"首期边界前一秒", date(2026, 2, 28, 8), date(2026, 1, 31, 9), date(2026, 2, 28, 9)},
		{"换期瞬间", date(2026, 2, 28, 9), date(2026, 2, 28, 9), date(2026, 3, 31, 9)},
		{"早于锚点仍算首期", date(2025, 12, 1, 0), date(2026, 1, 31, 9), date(2026, 2, 28, 9)},
		{"一年后", date(2027, 2, 5, 0), date(2027, 1, 31, 9), date(2027, 2, 28, 9)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			start, end := PeriodContaining(anchor, tc.now)
			if !start.Equal(tc.wantStart) || !end.Equal(tc.wantEnd) {
				t.Fatalf("PeriodContaining(now=%s) = [%s, %s), want [%s, %s)", tc.now, start, end, tc.wantStart, tc.wantEnd)
			}
			if tc.now.After(anchor) && (tc.now.Before(start) || !tc.now.Before(end)) {
				t.Fatalf("now %s 不在返回区间 [%s, %s) 内", tc.now, start, end)
			}
		})
	}
}

func TestCarryOverExtraEatsOverageFirst(t *testing.T) {
	t.Parallel()
	q := billing.PlanQuotas{MonthlyCredits: 1000}
	cases := []struct {
		name string
		sub  Subscription
		want float64
	}{
		{"未超额全额结转", Subscription{CreditsUsed: 400, ExtraCredits: 300}, 300},
		{"超额吃掉部分加购", Subscription{CreditsUsed: 1200, ExtraCredits: 300}, 100},
		{"超额吃光并透支归零", Subscription{CreditsUsed: 1500, ExtraCredits: 300}, 0},
		{"无加购", Subscription{CreditsUsed: 1500}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := carryOverExtra(q, tc.sub); got != tc.want {
				t.Fatalf("carryOverExtra = %v, want %v", got, tc.want)
			}
		})
	}
	if got := carryOverExtra(billing.PlanQuotas{}, Subscription{CreditsUsed: 99999, ExtraCredits: 50}); got != 50 {
		t.Fatalf("不限量套餐加购应原样结转，得到 %v", got)
	}
}
