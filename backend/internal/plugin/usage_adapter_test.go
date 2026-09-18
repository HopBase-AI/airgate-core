package plugin

import (
	"math"
	"testing"

	sdk "github.com/DouDOU-start/airgate-sdk/sdkgo"

	"github.com/DouDOU-start/airgate-core/internal/billing"
	"github.com/DouDOU-start/airgate-core/internal/pkg/listprice"
)

func TestUsageSnapshotFromSDK_RecognizesClaudeCacheCreationKeys(t *testing.T) {
	usage := &sdk.Usage{
		Metrics: []sdk.UsageMetric{
			{Key: "input_tokens", Value: 2},
			{Key: "output_tokens", Value: 163},
			{Key: "cached_input_tokens", Value: 141779},
			{Key: "cache_creation_input_tokens", Value: 1756},
			{Key: "cache_creation_5m_input_tokens", Value: 1756},
			{Key: "cache_creation_1h_input_tokens", Value: 0},
		},
		CostDetails: []sdk.UsageCostDetail{
			{Key: "input_tokens", AccountCost: 0.00001, Metadata: map[string]string{"unit_price": "5"}},
			{Key: "cached_input_tokens", AccountCost: 0.0708895, Metadata: map[string]string{"unit_price": "0.5"}},
			{Key: "cache_creation_5m_input_tokens", AccountCost: 0.010975, Metadata: map[string]string{"unit_price": "6.25"}},
			{Key: "output_tokens", AccountCost: 0.004075, Metadata: map[string]string{"unit_price": "25"}},
		},
	}

	snap := usageSnapshotFromSDK(usage)

	if snap.CacheCreationTokens != 1756 {
		t.Fatalf("CacheCreationTokens = %d, want 1756", snap.CacheCreationTokens)
	}
	if snap.CacheCreation5mTokens != 1756 {
		t.Fatalf("CacheCreation5mTokens = %d, want 1756", snap.CacheCreation5mTokens)
	}
	if snap.CacheCreation1hTokens != 0 {
		t.Fatalf("CacheCreation1hTokens = %d, want 0", snap.CacheCreation1hTokens)
	}
	if math.Abs(snap.CacheCreationCost-0.010975) > 1e-12 {
		t.Fatalf("CacheCreationCost = %.12f, want %.12f", snap.CacheCreationCost, 0.010975)
	}
	if math.Abs(snap.CacheCreationPrice-6.25) > 1e-12 {
		t.Fatalf("CacheCreationPrice = %.12f, want 6.25", snap.CacheCreationPrice)
	}
}

// 缓存读走了与整单不同的倍率时，行级 usage_metadata 必须留下快照——
// 使用记录的验算块只认这个键，丢了它客户那边整块就不渲染了。
func TestWithCachedRateSnapshot(t *testing.T) {
	cases := []struct {
		name string
		calc billing.CalculateResult
		want string // "" = 不应写键
	}{
		{
			name: "缓存走基准倍率",
			calc: billing.CalculateResult{CachedInputCost: 0.05, CachedInputRate: 6.8, RateMultiplier: 4.42},
			want: "6.8",
		},
		{
			name: "两个倍率相同则不写",
			calc: billing.CalculateResult{CachedInputCost: 0.05, CachedInputRate: 4.42, RateMultiplier: 4.42},
		},
		{
			name: "本次没有缓存用量",
			calc: billing.CalculateResult{CachedInputCost: 0, CachedInputRate: 6.8, RateMultiplier: 4.42},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := withCachedRateSnapshot(map[string]string{"trace_id": "abc"}, tc.calc)
			if got["trace_id"] != "abc" {
				t.Fatalf("原有键被丢掉了: %+v", got)
			}
			if got[listprice.SnapshotCachedRate] != tc.want {
				t.Fatalf("%s = %q, want %q", listprice.SnapshotCachedRate, got[listprice.SnapshotCachedRate], tc.want)
			}
		})
	}
}

// 不写键时原样返回入参，不为一次 no-op 复制整个 map。
func TestWithCachedRateSnapshotNoCopyWhenUnchanged(t *testing.T) {
	in := map[string]string{"trace_id": "abc"}
	out := withCachedRateSnapshot(in, billing.CalculateResult{CachedInputCost: 0.05, CachedInputRate: 4.42, RateMultiplier: 4.42})
	in["probe"] = "1"
	if out["probe"] != "1" {
		t.Fatalf("no-op 时应原样返回入参，got 一份复制: %+v", out)
	}
}
