package auth

import (
	"testing"
	"time"

	"github.com/DouDOU-start/airgate-core/ent"
	entmember "github.com/DouDOU-start/airgate-core/ent/member"
)

// 成员"余额"口径：0=不限 → 不受限（回落企业主余额）；有额度 → 本期剩余，
// monthly 未跨期扣本期已用、已跨期（鉴权尚未推进）按新期满额、none 扣累计 − 重置快照。
func TestMemberRemainingQuota(t *testing.T) {
	now := time.Date(2026, time.March, 15, 12, 0, 0, 0, time.UTC)
	anchor := time.Date(2026, time.January, 31, 9, 0, 0, 0, time.UTC)
	cases := []struct {
		name        string
		member      *ent.Member
		wantRemain  float64
		wantLimited bool
	}{
		{"nil 成员", nil, 0, false},
		{"不限额", &ent.Member{QuotaUsd: 0, QuotaPeriod: entmember.QuotaPeriodMonthly, UsedQuota: 30}, 0, false},
		{
			"monthly 本期内：30 − 10 已用，剩 100 − 20",
			&ent.Member{QuotaUsd: 100, QuotaPeriod: entmember.QuotaPeriodMonthly, PeriodAnchor: anchor,
				PeriodStart: time.Date(2026, time.February, 28, 9, 0, 0, 0, time.UTC), PeriodUsedBase: 10, UsedQuota: 30},
			80, true,
		},
		{
			"monthly 已跨期未推进：按新期满额",
			&ent.Member{QuotaUsd: 100, QuotaPeriod: entmember.QuotaPeriodMonthly, PeriodAnchor: anchor, PeriodStart: anchor, PeriodUsedBase: 0, UsedQuota: 60},
			100, true,
		},
		{
			"none：累计 − 重置快照",
			&ent.Member{QuotaUsd: 10, QuotaPeriod: entmember.QuotaPeriodNone, PeriodUsedBase: 5, UsedQuota: 8},
			7, true,
		},
		{
			"超支封底 0",
			&ent.Member{QuotaUsd: 10, QuotaPeriod: entmember.QuotaPeriodNone, PeriodUsedBase: 0, UsedQuota: 12},
			0, true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			remain, limited := MemberRemainingQuota(tc.member, now)
			if remain != tc.wantRemain || limited != tc.wantLimited {
				t.Fatalf("MemberRemainingQuota = (%v, %v), want (%v, %v)", remain, limited, tc.wantRemain, tc.wantLimited)
			}
		})
	}
}
