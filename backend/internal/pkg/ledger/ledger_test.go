package ledger

import (
	"math"
	"testing"
)

const epsilon = 1e-9

// RateBase 与 Currency 是同一个账本口径的两个副本，割接时必须同批翻转。
// 只改一半会让验算块显示「¥ 官方费用 → $ 实扣」这种自相矛盾的等式。
func TestRateBaseAndCurrencyAgree(t *testing.T) {
	if (RateBase == 1) != (Currency == "USD") {
		t.Fatalf("账本口径只改了一半：RateBase = %g, Currency = %q", RateBase, Currency)
	}
	if RateBase <= 0 {
		t.Fatalf("RateBase 必须为正，got %g", RateBase)
	}
}

func TestDiscountRestoresZhe(t *testing.T) {
	cases := []struct {
		name  string
		rate  float64
		want  float64
		about string
	}{
		// 割接脚本把分组倍率整体 ÷6.8 后，倍率就是折本身：
		// 组 27 可灵 5.1 → 0.75、组 30 MiniMax H3 5.44 → 0.8。
		{"可灵 75 折", 0.75, 0.75 / RateBase, "组 27"},
		{"MiniMax H3 8 折", 0.8, 0.8 / RateBase, "组 30"},
		{"倍率缺失按原价", 0, 1 / RateBase, "固定图价分组配 0"},
		{"负值同样按原价", -1, 1 / RateBase, "脏数据"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if got := Discount(tt.rate); math.Abs(got-tt.want) > epsilon {
				t.Fatalf("Discount(%g) = %g, want %g（%s）", tt.rate, got, tt.want, tt.about)
			}
		})
	}
}

// 除数是 listFX ÷ RateBase，不是「¥ 账本恒为 1」。
func TestDivisorIsListFXOverRateBase(t *testing.T) {
	if got := Divisor(RateBase); math.Abs(got-1) > epsilon {
		t.Fatalf("牌价折算率与账本口径相同时除数应为 1, got %g", got)
	}
	// 日元牌价（listFX 150）：除数始终是 listFX ÷ RateBase——写死 1 的实现会在这里红。
	if got, want := Divisor(150), 150/RateBase; math.Abs(got-want) > epsilon {
		t.Fatalf("Divisor(150) = %g, want %g", got, want)
	}
	if got := Divisor(0); got != 0 {
		t.Fatalf("快照缺失应返回 0, got %g", got)
	}
	if got := Divisor(-6.8); got != 0 {
		t.Fatalf("非法折算率应返回 0, got %g", got)
	}
}

// 不变式：官方费用 × 折 ÷ 除数 ≡ 官方费用 ÷ listFX × 倍率（= actual_cost 的定义式）。
// 与 RateBase 取值无关，这正是「两种账本都成立」的形式化表述。
func TestVerificationIdentityHoldsForAnyRateBase(t *testing.T) {
	const officialCost = 6.0 // ¥6.00
	for _, listFX := range []float64{6.8, 150} {
		for _, rate := range []float64{5.44, 0.8, 1} {
			actual := officialCost / listFX * EffectiveRate(rate)
			verified := officialCost * Discount(rate) / Divisor(listFX)
			if math.Abs(verified-actual) > 1e-9 {
				t.Fatalf("listFX=%g rate=%g: 验算 %g ≠ actual %g", listFX, rate, verified, actual)
			}
		}
	}
}
