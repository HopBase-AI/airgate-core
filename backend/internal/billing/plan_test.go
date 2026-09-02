package billing

import (
	"encoding/json"
	"testing"
)

func TestParsePlanQuotasDefaultsAndTypes(t *testing.T) {
	t.Parallel()

	empty := ParsePlanQuotas(nil)
	if !empty.VideoEnabled || !empty.Unlimited() || empty.CreditsPerUnitOrDefault() != DefaultCreditsPerUnit {
		t.Fatalf("空配置应为不限量 + 视频开放 + 默认换算率，得到 %+v", empty)
	}

	q := ParsePlanQuotas(map[string]any{
		"monthly_credits":     json.Number("1000000"),
		"credits_per_unit":    "11494",
		"per_request_credits": 20000,
		"image_monthly_limit": float64(20),
		"video_enabled":       "false",
		"price_monthly":       128.0,
		"price_annual":        int64(1308),
		"topup_credits":       150000,
		"topup_price":         20,
		"daily":               5, // 老键忽略
	})
	if q.MonthlyCredits != 1_000_000 || q.CreditsPerUnit != 11494 || q.PerRequestCredits != 20000 {
		t.Fatalf("数值字段解析错误: %+v", q)
	}
	if q.ImageMonthlyLimit != 20 || q.VideoEnabled {
		t.Fatalf("张数/视频开关解析错误: %+v", q)
	}
	if !q.Purchasable() || !q.TopupAvailable() || q.Unlimited() {
		t.Fatalf("可购/可加购判定错误: %+v", q)
	}
	if got := q.Credits(0.87); got < 9999.7 || got > 10000.3 {
		t.Fatalf("¥0.87 应≈10000 点，得到 %v", got)
	}
	if q.Credits(-1) != 0 || q.Credits(0) != 0 {
		t.Fatal("非正费用应折算 0 点")
	}

	back := ParsePlanQuotas(q.ToMap())
	if back != q {
		t.Fatalf("ToMap 往返不一致: %+v vs %+v", back, q)
	}
}

func TestParsePlanQuotasRejectsGarbage(t *testing.T) {
	t.Parallel()
	q := ParsePlanQuotas(map[string]any{
		"monthly_credits":  "abc",
		"credits_per_unit": -5,
		"video_enabled":    "maybe",
	})
	if q.MonthlyCredits != 0 || q.CreditsPerUnitOrDefault() != DefaultCreditsPerUnit || !q.VideoEnabled {
		t.Fatalf("非法值应回落默认: %+v", q)
	}
}
