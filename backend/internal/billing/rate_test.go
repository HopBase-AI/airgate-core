package billing

import (
	"testing"

	"github.com/DouDOU-start/airgate-core/internal/auth"
)

func TestResolveBillingRateForGroup_PriorityChain(t *testing.T) {
	tests := []struct {
		name           string
		userGroupRates map[int64]float64
		groupID        int
		groupRate      float64
		want           float64
	}{
		{name: "user override wins", userGroupRates: map[int64]float64{5: 0.2}, groupID: 5, groupRate: 0.5, want: 0.2},
		{name: "group rate fallback", userGroupRates: map[int64]float64{6: 0.2}, groupID: 5, groupRate: 0.5, want: 0.5},
		{name: "default fallback", groupID: 5, want: 1.0},
		{name: "non-positive override falls through", userGroupRates: map[int64]float64{5: 0}, groupID: 5, groupRate: 0.4, want: 0.4},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveBillingRateForGroup(tt.userGroupRates, tt.groupID, tt.groupRate)
			if got != tt.want {
				t.Errorf("ResolveBillingRateForGroup() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestResolveBillingRate_PriorityChain(t *testing.T) {
	tests := []struct {
		name string
		info *auth.APIKeyInfo
		want float64
	}{
		{
			name: "nil keyInfo defaults to 1.0",
			info: nil,
			want: 1.0,
		},
		{
			name: "user.group_rates wins over group.rate_multiplier",
			info: &auth.APIKeyInfo{
				GroupID:             5,
				GroupRateMultiplier: 0.5,
				UserGroupRates:      map[int64]float64{5: 0.2},
			},
			want: 0.2,
		},
		{
			name: "user.group_rates miss falls back to group.rate_multiplier",
			info: &auth.APIKeyInfo{
				GroupID:             5,
				GroupRateMultiplier: 0.5,
				UserGroupRates:      map[int64]float64{6: 0.2}, // 不同 group
			},
			want: 0.5,
		},
		{
			name: "no overrides falls back to group.rate_multiplier",
			info: &auth.APIKeyInfo{
				GroupID:             5,
				GroupRateMultiplier: 0.7,
			},
			want: 0.7,
		},
		{
			name: "everything zero defaults to 1.0",
			info: &auth.APIKeyInfo{
				GroupID:             5,
				GroupRateMultiplier: 0,
			},
			want: 1.0,
		},
		{
			name: "sell_rate is NOT in priority chain — should be ignored",
			info: &auth.APIKeyInfo{
				GroupID:             5,
				GroupRateMultiplier: 0.3,
				SellRate:            0.99, // 不应影响 billing rate
			},
			want: 0.3,
		},
		{
			name: "user.group_rates with non-positive value falls through",
			info: &auth.APIKeyInfo{
				GroupID:             5,
				GroupRateMultiplier: 0.4,
				UserGroupRates:      map[int64]float64{5: 0}, // 显式 0 视为未设置
			},
			want: 0.4,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveBillingRate(tt.info)
			if got != tt.want {
				t.Errorf("ResolveBillingRate() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestResolveBillingRateForGroupModel_PriorityChain 按模型倍率的完整优先级链：
// 用户专属 > 分组按模型 > 分组 > 1.0。
func TestResolveBillingRateForGroupModel_PriorityChain(t *testing.T) {
	modelRates := map[string]float64{"deepseek-v4-pro": 3.74, " GPT-5.5 ": 2.0, "bad-zero": 0, "bad-neg": -1}
	tests := []struct {
		name           string
		userGroupRates map[int64]float64
		groupID        int
		groupRate      float64
		modelRates     map[string]float64
		model          string
		want           float64
	}{
		{name: "model rate overrides group rate", groupID: 5, groupRate: 6.8, modelRates: modelRates, model: "deepseek-v4-pro", want: 3.74},
		{name: "user override wins over model rate", userGroupRates: map[int64]float64{5: 1.5}, groupID: 5, groupRate: 6.8, modelRates: modelRates, model: "deepseek-v4-pro", want: 1.5},
		{name: "user override for another group does not apply", userGroupRates: map[int64]float64{6: 1.5}, groupID: 5, groupRate: 6.8, modelRates: modelRates, model: "deepseek-v4-pro", want: 3.74},
		{name: "unlisted model falls back to group rate", groupID: 5, groupRate: 6.8, modelRates: modelRates, model: "deepseek-v4.1-flash", want: 6.8},
		{name: "case-insensitive trimmed key match", groupID: 5, groupRate: 6.8, modelRates: modelRates, model: "gpt-5.5", want: 2.0},
		{name: "no glob or prefix match", groupID: 5, groupRate: 6.8, modelRates: map[string]float64{"gpt-*": 1.0, "deepseek": 1.0}, model: "deepseek-v4-pro", want: 6.8},
		{name: "zero model rate is treated as unset", groupID: 5, groupRate: 6.8, modelRates: modelRates, model: "bad-zero", want: 6.8},
		{name: "negative model rate is treated as unset", groupID: 5, groupRate: 6.8, modelRates: modelRates, model: "bad-neg", want: 6.8},
		{name: "empty model skips model rates", groupID: 5, groupRate: 6.8, modelRates: modelRates, model: "", want: 6.8},
		{name: "nil model rates behaves like group resolver", groupID: 5, groupRate: 0.5, model: "deepseek-v4-pro", want: 0.5},
		{name: "model rate applies even when group rate is zero sentinel", groupID: 5, groupRate: 0, modelRates: modelRates, model: "deepseek-v4-pro", want: 3.74},
		{name: "everything unset defaults to 1.0", groupID: 5, model: "deepseek-v4-pro", want: 1.0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveBillingRateForGroupModel(tt.userGroupRates, tt.groupID, tt.groupRate, tt.modelRates, tt.model)
			if got != tt.want {
				t.Errorf("ResolveBillingRateForGroupModel() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestResolveBillingRateForModel_UsesKeyInfoModelRates(t *testing.T) {
	info := &auth.APIKeyInfo{
		GroupID:             5,
		GroupRateMultiplier: 6.8,
		GroupModelRates:     map[string]float64{"deepseek-v4-pro": 3.74},
	}
	tests := []struct {
		name  string
		info  *auth.APIKeyInfo
		model string
		want  float64
	}{
		{name: "nil keyInfo defaults to 1.0", info: nil, model: "deepseek-v4-pro", want: 1.0},
		{name: "listed model takes model rate", info: info, model: "deepseek-v4-pro", want: 3.74},
		{name: "unlisted model takes group rate", info: info, model: "deepseek-v4.1-flash", want: 6.8},
		{name: "group-level resolver ignores model rates", info: info, model: "", want: 6.8},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ResolveBillingRateForModel(tt.info, tt.model); got != tt.want {
				t.Errorf("ResolveBillingRateForModel() = %v, want %v", got, tt.want)
			}
		})
	}
	// 旧签名保持分组口径，不受按模型倍率影响
	if got := ResolveBillingRate(info); got != 6.8 {
		t.Errorf("ResolveBillingRate() = %v, want 6.8", got)
	}
}
