// Package period 提供「按月对齐、月末夹紧」的计量期推进。
//
// 团队成员的月度额度与订阅制的月点数都按同一规则换期：以锚点日逐月推进，
// 1/31 的下一期是 2/28（29）、再下一期回到 3/31，而不是 time.AddDate 的溢出滚动。
// 与 app/subscription 的同名函数口径一致；放在 pkg 层是因为鉴权热路径
// （internal/auth）也要用它，而 auth 不能反向依赖 app/billing。
package period

import "time"

// AddMonths 按「同日对齐、月末夹紧」推进 n 个月：1/31 +1 → 2/28（29），+2 → 3/31。
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

// Containing 返回以 anchor 逐月推进后包含 now 的计量期 [start, end)。
// now 早于 anchor 时返回首期。
func Containing(anchor, now time.Time) (start, end time.Time) {
	if !now.After(anchor) {
		return anchor, AddMonths(anchor, 1)
	}
	n := (now.Year()-anchor.Year())*12 + int(now.Month()-anchor.Month())
	start = AddMonths(anchor, n)
	for start.After(now) {
		n--
		start = AddMonths(anchor, n)
	}
	end = AddMonths(anchor, n+1)
	for !end.After(now) {
		n++
		start = end
		end = AddMonths(anchor, n+1)
	}
	return start, end
}

// Window 返回 monthly 口径下包含 now 的计量期 [start, end)，以及相对已落库的
// periodStart 是否已跨期（跨期意味着本期已用应从当前累计值重新起算）。
//
// 锚点晚于 now 是「企业主改了账期日」的过渡态：改动自新账期日起生效，当前期延续到那一天
// ——此时返回 [periodStart, anchor) 且不算跨期，而不是把首期当成新期立刻清零。
func Window(anchor, periodStart, now time.Time) (start, end time.Time, rolled bool) {
	if now.Before(anchor) {
		return periodStart, anchor, false
	}
	start, end = Containing(anchor, now)
	return start, end, start.After(periodStart)
}

// AnchorForDay 返回以 dayOfMonth 为账期日、严格晚于 now 的下一个锚点时刻（时分秒取 now 的），
// 月份天数不足时夹紧到月末。企业主改账期日就用它：当前期延续到新锚点，之后按新账期日按月推进。
func AnchorForDay(now time.Time, dayOfMonth int) time.Time {
	if dayOfMonth < 1 {
		dayOfMonth = 1
	}
	if dayOfMonth > 31 {
		dayOfMonth = 31
	}
	h, mi, s := now.Clock()
	candidate := func(year int, month time.Month) time.Time {
		d := dayOfMonth
		if last := daysIn(year, month); d > last {
			d = last
		}
		return time.Date(year, month, d, h, mi, s, now.Nanosecond(), now.Location())
	}
	next := candidate(now.Year(), now.Month())
	if !next.After(now) {
		first := time.Date(now.Year(), now.Month()+1, 1, 0, 0, 0, 0, now.Location())
		next = candidate(first.Year(), first.Month())
	}
	return next
}
