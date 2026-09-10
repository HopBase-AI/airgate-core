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

// TestSortCandidatesForModel 两组同平台：分组级 A 便宜；但对 deepseek-v4-pro 组 B 配了按模型倍率更便宜，
// 知道模型后自动选组顺序必须翻转，与模型广场选价一致；模型为空或不涉及按模型倍率时顺序不变。
func TestSortCandidatesForModel(t *testing.T) {
	build := func() []Candidate {
		return []Candidate{
			{GroupID: 1, EffectiveRate: 5.0, GroupRateMultiplier: 5.0},
			{GroupID: 2, EffectiveRate: 6.8, GroupRateMultiplier: 6.8, GroupModelRates: map[string]float64{"deepseek-v4-pro": 3.74}},
		}
	}
	ids := func(cs []Candidate) []int {
		out := make([]int, 0, len(cs))
		for _, c := range cs {
			out = append(out, c.GroupID)
		}
		return out
	}
	tests := []struct {
		name  string
		model string
		want  []int
	}{
		{name: "model rate flips order", model: "deepseek-v4-pro", want: []int{2, 1}},
		{name: "unlisted model keeps group order", model: "deepseek-v4.1-flash", want: []int{1, 2}},
		{name: "empty model keeps group order", model: "", want: []int{1, 2}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cs := build()
			SortCandidatesForModel(cs, tt.model)
			got := ids(cs)
			if len(got) != len(tt.want) || got[0] != tt.want[0] || got[1] != tt.want[1] {
				t.Fatalf("order = %v, want %v", got, tt.want)
			}
			// EffectiveRate 本身不被改写（仍是分组口径）
			for _, c := range cs {
				if c.GroupID == 2 && c.EffectiveRate != 6.8 {
					t.Fatalf("EffectiveRate mutated: %+v", c)
				}
			}
		})
	}
	// 稳定性：同价候选保持原次序（权重/ID 规则由 CandidatePrecedes 兜底）
	same := []Candidate{{GroupID: 9, EffectiveRate: 1}, {GroupID: 3, EffectiveRate: 1}}
	SortCandidatesForModel(same, "any")
	if got := ids(same); got[0] != 3 || got[1] != 9 {
		t.Fatalf("tie-break order = %v, want [3 9]", got)
	}
}
