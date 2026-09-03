package period

import (
	"testing"
	"time"
)

func date(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 10, 30, 0, 0, time.UTC)
}

func TestAddMonthsClampsToMonthEnd(t *testing.T) {
	cases := []struct {
		name string
		from time.Time
		n    int
		want time.Time
	}{
		{"1/31 +1 → 2/28", date(2026, time.January, 31), 1, date(2026, time.February, 28)},
		{"1/31 +2 → 3/31", date(2026, time.January, 31), 2, date(2026, time.March, 31)},
		{"闰年 1/31 +1 → 2/29", date(2028, time.January, 31), 1, date(2028, time.February, 29)},
		{"跨年 11/30 +3 → 2/28", date(2026, time.November, 30), 3, date(2027, time.February, 28)},
		{"普通日 3/15 +1 → 4/15", date(2026, time.March, 15), 1, date(2026, time.April, 15)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := AddMonths(tc.from, tc.n); !got.Equal(tc.want) {
				t.Fatalf("AddMonths(%v, %d) = %v, want %v", tc.from, tc.n, got, tc.want)
			}
		})
	}
}

func TestContaining(t *testing.T) {
	anchor := date(2026, time.January, 31)
	cases := []struct {
		name      string
		now       time.Time
		wantStart time.Time
		wantEnd   time.Time
	}{
		{"now 早于锚点 → 首期", date(2025, time.December, 1), anchor, date(2026, time.February, 28)},
		{"锚点当天 → 首期", anchor, anchor, date(2026, time.February, 28)},
		{"首期内", date(2026, time.February, 10), anchor, date(2026, time.February, 28)},
		{"换期日当天算新期", date(2026, time.February, 28), date(2026, time.February, 28), date(2026, time.March, 31)},
		{"第三期", date(2026, time.April, 1), date(2026, time.March, 31), date(2026, time.April, 30)},
		{"跨年", date(2027, time.January, 5), date(2026, time.December, 31), date(2027, time.January, 31)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			start, end := Containing(anchor, tc.now)
			if !start.Equal(tc.wantStart) || !end.Equal(tc.wantEnd) {
				t.Fatalf("Containing(now=%v) = [%v, %v), want [%v, %v)", tc.now, start, end, tc.wantStart, tc.wantEnd)
			}
			if !tc.now.Before(end) || tc.now.Before(start) && tc.now.After(anchor) {
				t.Fatalf("now %v 不在返回区间 [%v, %v) 内", tc.now, start, end)
			}
		})
	}
}
