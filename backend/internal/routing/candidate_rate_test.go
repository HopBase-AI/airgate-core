package routing

import "testing"

// TestCandidateRateForModel 候选分组在知道模型后重放完整优先级链：
// 用户专属 > 分组按模型 > 分组；EffectiveRate 本身保持分组口径（自动选组排序用）。
func TestCandidateRateForModel(t *testing.T) {
	tests := []struct {
		name      string
		candidate Candidate
		model     string
		want      float64
	}{
		{
			name:      "model rate overrides group rate",
			candidate: Candidate{GroupID: 28, EffectiveRate: 6.8, GroupRateMultiplier: 6.8, GroupModelRates: map[string]float64{"deepseek-v4-pro": 3.74}},
			model:     "deepseek-v4-pro",
			want:      3.74,
		},
		{
			name:      "unlisted model keeps group rate",
			candidate: Candidate{GroupID: 28, EffectiveRate: 6.8, GroupRateMultiplier: 6.8, GroupModelRates: map[string]float64{"deepseek-v4-pro": 3.74}},
			model:     "deepseek-v4.1-flash",
			want:      6.8,
		},
		{
			name:      "user override beats model rate",
			candidate: Candidate{GroupID: 28, EffectiveRate: 2.0, GroupRateMultiplier: 6.8, UserGroupRate: 2.0, GroupModelRates: map[string]float64{"deepseek-v4-pro": 3.74}},
			model:     "deepseek-v4-pro",
			want:      2.0,
		},
		{
			name:      "empty model equals effective rate",
			candidate: Candidate{GroupID: 28, EffectiveRate: 6.8, GroupRateMultiplier: 6.8, GroupModelRates: map[string]float64{"deepseek-v4-pro": 3.74}},
			model:     "",
			want:      6.8,
		},
		{
			name:      "no rates at all defaults to 1.0",
			candidate: Candidate{GroupID: 28},
			model:     "deepseek-v4-pro",
			want:      1.0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.candidate.RateForModel(tt.model); got != tt.want {
				t.Errorf("RateForModel(%q) = %v, want %v", tt.model, got, tt.want)
			}
		})
	}
}
