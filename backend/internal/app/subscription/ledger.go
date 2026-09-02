package subscription

import (
	"time"

	"github.com/DouDOU-start/airgate-core/internal/billing"
)

// AddMonths 按「同日对齐、月末夹紧」推进 n 个月：1/31 +1 → 2/28（29），+2 → 3/31。
// 与 time.AddDate 的溢出滚动（1/31 +1 → 3/3）不同，订阅期必须落在锚定日或月末。
func AddMonths(t time.Time, n int) time.Time {
	y, m, d := t.Date()
	h, mi, s := t.Clock()
	first := time.Date(y, m+time.Month(n), 1, h, mi, s, t.Nanosecond(), t.Location())
	if last := daysIn(first.Year(), first.Month()); d > last {
		d = last
	}
	return time.Date(first.Year(), first.Month(), d, h, mi, s, t.Nanosecond(), t.Location())
}

func daysIn(year int, month time.Month) int {
	return time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

// PeriodContaining 返回以 anchor 逐月推进后包含 now 的计量期 [start, end)。
// now 早于 anchor 时返回首期。
func PeriodContaining(anchor, now time.Time) (time.Time, time.Time) {
	if !now.After(anchor) {
		return anchor, AddMonths(anchor, 1)
	}
	n := (now.Year()-anchor.Year())*12 + int(now.Month()-anchor.Month())
	start := AddMonths(anchor, n)
	for start.After(now) {
		n--
		start = AddMonths(anchor, n)
	}
	end := AddMonths(anchor, n+1)
	for !end.After(now) {
		n++
		start = end
		end = AddMonths(anchor, n+1)
	}
	return start, end
}

// remainingCredits 剩余点数 = 月额度 + 加购 − 已用（可为负：最后一笔允许透支，与余额语义一致）。
func remainingCredits(q billing.PlanQuotas, sub Subscription) float64 {
	return q.MonthlyCredits + sub.ExtraCredits - sub.CreditsUsed
}

// carryOverExtra 期满结转：本期超出月额度的消耗先吃加购包，剩余加购点数带入下期。
func carryOverExtra(q billing.PlanQuotas, sub Subscription) float64 {
	extra := sub.ExtraCredits
	if q.MonthlyCredits > 0 && sub.CreditsUsed > q.MonthlyCredits {
		extra -= sub.CreditsUsed - q.MonthlyCredits
	}
	if extra < 0 {
		extra = 0
	}
	return extra
}

// cycleMonths 购买周期对应的月数；非法周期返回 0。
func cycleMonths(cycle string) int {
	switch cycle {
	case BillingCycleMonthly:
		return 1
	case BillingCycleAnnual:
		return 12
	default:
		return 0
	}
}
