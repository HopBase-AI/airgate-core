package period

import (
	"testing"
	"time"
)

// 企业主改账期日后锚点晚于现在：当前期延续到新锚点，不算跨期。
func TestWindowFutureAnchorExtendsCurrentPeriod(t *testing.T) {
	periodStart := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	anchor := time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC)
	now := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	start, end, rolled := Window(anchor, periodStart, now)
	if rolled {
		t.Fatal("future anchor must not roll the period")
	}
	if !start.Equal(periodStart) || !end.Equal(anchor) {
		t.Fatalf("window = [%v, %v), want [%v, %v)", start, end, periodStart, anchor)
	}
	// 锚点到了之后按新账期日按月推进。
	later := time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC)
	start, end, rolled = Window(anchor, periodStart, later)
	if !rolled || !start.Equal(anchor) || !end.Equal(time.Date(2026, 10, 15, 10, 0, 0, 0, time.UTC)) {
		t.Fatalf("after anchor: start=%v end=%v rolled=%v", start, end, rolled)
	}
}

func TestAnchorForDay(t *testing.T) {
	now := time.Date(2026, 9, 9, 8, 30, 0, 0, time.UTC)
	cases := []struct {
		day  int
		want time.Time
	}{
		{15, time.Date(2026, 9, 15, 8, 30, 0, 0, time.UTC)},
		{9, time.Date(2026, 10, 9, 8, 30, 0, 0, time.UTC)}, // 当天已过（严格晚于 now）→ 下月
		{1, time.Date(2026, 10, 1, 8, 30, 0, 0, time.UTC)},
		{31, time.Date(2026, 9, 30, 8, 30, 0, 0, time.UTC)}, // 月末夹紧
	}
	for _, tc := range cases {
		if got := AnchorForDay(now, tc.day); !got.Equal(tc.want) {
			t.Fatalf("AnchorForDay(day=%d) = %v, want %v", tc.day, got, tc.want)
		}
	}
}
