package plugin

import (
	"fmt"
	"math"
	"testing"

	"github.com/DouDOU-start/airgate-core/internal/billing"
	sdk "github.com/DouDOU-start/airgate-sdk/sdkgo"
)

func TestImageOutputBillingOverride_UsesConfiguredTier(t *testing.T) {
	usage := &sdk.Usage{
		Model: "gpt-5.5",
		Attributes: []sdk.UsageAttribute{
			{Key: "image_size", Value: "1672x941"},
		},
		Metrics: []sdk.UsageMetric{
			{Key: "images", Kind: "image", Value: 2},
		},
		CostDetails: []sdk.UsageCostDetail{
			{Key: "images", AccountCost: 0.40},
		},
	}
	settings := map[string]map[string]string{
		"openai": {
			"image_price_2k": "0.08",
		},
	}

	got, ok := imageOutputBillingOverride(usage, nil, settings)
	if !ok {
		t.Fatal("expected override")
	}
	if math.Abs(got.cost-0.16) > 1e-9 {
		t.Fatalf("override = %v, want 0.16 for two 2K images", got.cost)
	}
	if got.replacesTotal {
		t.Fatal("responses model fixed image price should not replace token costs")
	}
}

func TestImageOutputBillingOverride_ImageModelReplacesTotal(t *testing.T) {
	usage := &sdk.Usage{
		Model: "gpt-image-2",
		Attributes: []sdk.UsageAttribute{
			{Key: "image_size", Value: "1024x1024"},
		},
		Metrics: []sdk.UsageMetric{
			{Key: "images", Kind: "image", Value: 1},
		},
		CostDetails: []sdk.UsageCostDetail{
			{Key: "images", AccountCost: 0.40},
		},
	}
	settings := map[string]map[string]string{
		"openai": {
			"image_price_1k": "0.10",
		},
	}

	got, ok := imageOutputBillingOverride(usage, nil, settings)
	if !ok {
		t.Fatal("expected override")
	}
	if math.Abs(got.cost-0.10) > 1e-9 {
		t.Fatalf("override = %v, want 0.10", got.cost)
	}
	if !got.replacesTotal {
		t.Fatal("image model fixed image price should replace the whole request")
	}
}

func TestImageOutputBillingOverride_CurrentImage2Prices(t *testing.T) {
	settings := map[string]map[string]string{
		"openai": {
			"image_price_1k": "0.08",
			"image_price_2k": "0.12",
			"image_price_4k": "0.15",
		},
	}
	tests := []struct {
		name string
		size string
		want float64
	}{
		{name: "1k", size: "1024x1024", want: 0.08},
		{name: "2k", size: "2048x1152", want: 0.12},
		{name: "4k", size: "3840x2160", want: 0.15},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			usage := &sdk.Usage{
				Model: "gpt-image-2",
				Attributes: []sdk.UsageAttribute{
					{Key: "image_size", Value: tt.size},
				},
				Metrics: []sdk.UsageMetric{
					{Key: "images", Kind: "image", Value: 1},
				},
				CostDetails: []sdk.UsageCostDetail{
					{Key: "images", AccountCost: 0.40},
				},
			}

			got, ok := imageOutputBillingOverride(usage, nil, settings)
			if !ok {
				t.Fatal("expected override")
			}
			if math.Abs(got.cost-tt.want) > 1e-9 {
				t.Fatalf("override = %v, want %v", got.cost, tt.want)
			}
			if !got.replacesTotal {
				t.Fatal("image model fixed image price should replace the whole request")
			}
		})
	}
}

func TestApplyImageBillingOverride_SeparatesRetailFromAccountCost(t *testing.T) {
	tests := []struct {
		name         string
		references   int
		upstreamCost float64
		retailCost   float64
	}{
		{name: "no references", references: 0, upstreamCost: 0.09, retailCost: 0.09},
		{name: "first reference", references: 1, upstreamCost: 0.09, retailCost: 0.093},
		{name: "multiple references", references: 3, upstreamCost: 0.096, retailCost: 0.099},
	}
	paths := []struct {
		name     string
		sellRate float64
	}{
		{name: "toc host forwarding"},
		{name: "tob public forwarding", sellRate: 3},
	}

	for _, path := range paths {
		path := path
		t.Run(path.name, func(t *testing.T) {
			for _, tt := range tests {
				tt := tt
				t.Run(tt.name, func(t *testing.T) {
					usage := &sdk.Usage{
						AccountCost: tt.upstreamCost,
						Metrics: []sdk.UsageMetric{
							{Key: "images", Kind: "image", Value: 1, AccountCost: 0.09},
						},
						Metadata: map[string]string{
							imageBillingBaseCostOverrideMetadataKey: fmt.Sprintf("%g", tt.retailCost),
							"reference_image_count":                 fmt.Sprintf("%d", tt.references),
						},
					}
					snap := usageSnapshotFromSDK(usage)
					input := billing.CalculateInput{
						InputCost:         snap.InputCost,
						ImageInputCost:    snap.ImageInputCost,
						OutputCost:        snap.OutputCost,
						CachedInputCost:   snap.CachedInputCost,
						CacheCreationCost: snap.CacheCreationCost,
						ImageCost:         snap.ImageCost,
						BillingRate:       2,
						SellRate:          path.sellRate,
						AccountRate:       0.5,
					}
					applied, replacesTotal := applyImageBillingOverride(&input, usage, nil, nil)
					if !applied || !replacesTotal {
						t.Fatalf("override applied=%v replacesTotal=%v, want true/true", applied, replacesTotal)
					}

					got := billing.NewCalculator().Calculate(input)
					if math.Abs(got.TotalCost-tt.upstreamCost) > 1e-9 {
						t.Fatalf("TotalCost = %v, want upstream %v", got.TotalCost, tt.upstreamCost)
					}
					if math.Abs(got.AccountCost-tt.upstreamCost*0.5) > 1e-9 {
						t.Fatalf("AccountCost = %v, want %v", got.AccountCost, tt.upstreamCost*0.5)
					}
					if math.Abs(got.ActualCost-tt.retailCost*2) > 1e-9 {
						t.Fatalf("ActualCost = %v, want %v", got.ActualCost, tt.retailCost*2)
					}
					wantBilled := got.ActualCost
					if path.sellRate > 0 {
						wantBilled = tt.retailCost * path.sellRate
					}
					if math.Abs(got.BilledCost-wantBilled) > 1e-9 {
						t.Fatalf("BilledCost = %v, want %v", got.BilledCost, wantBilled)
					}
				})
			}
		})
	}
}

func TestImageOutputBillingOverride_RejectsInvalidPluginBaseCost(t *testing.T) {
	for _, value := range []string{"", "0", "-0.003", "NaN", "Inf", "not-a-number"} {
		t.Run(value, func(t *testing.T) {
			usage := &sdk.Usage{
				Metrics:  []sdk.UsageMetric{{Key: "images", Kind: "image", Value: 1}},
				Metadata: map[string]string{imageBillingBaseCostOverrideMetadataKey: value},
			}
			if got, ok := imageOutputBillingOverride(usage, nil, nil); ok {
				t.Fatalf("override = %+v, want invalid metadata to be ignored", got)
			}
		})
	}
}

func TestImageOutputBillingOverride_FallsBackWhenTierUnset(t *testing.T) {
	usage := &sdk.Usage{
		Attributes: []sdk.UsageAttribute{
			{Key: "image_size", Value: "3840x2160"},
		},
		Metrics: []sdk.UsageMetric{
			{Key: "images", Kind: "image", Value: 1},
		},
		CostDetails: []sdk.UsageCostDetail{
			{Key: "images", AccountCost: 0.40},
		},
	}
	settings := map[string]map[string]string{
		"openai": {
			"image_price_2k": "0.08",
		},
	}

	if got, ok := imageOutputBillingOverride(usage, nil, settings); ok {
		t.Fatalf("override = %+v, want fallback", got)
	}
}

func TestImageTierForSize(t *testing.T) {
	tests := []struct {
		size     string
		wantTier string
	}{
		{size: "1024x1024", wantTier: "1k"},
		{size: "1672x941", wantTier: "2k"},
		{size: "3840x2160", wantTier: "4k"},
	}

	for _, tt := range tests {
		t.Run(tt.size, func(t *testing.T) {
			tier, ok := billing.ImageTierForSize(tt.size)
			if !ok {
				t.Fatal("expected tier")
			}
			if tier != tt.wantTier {
				t.Fatalf("ImageTierForSize() = %q, want %q", tier, tt.wantTier)
			}
		})
	}
}

func TestShouldForwardPluginSetting_HidesImagePrices(t *testing.T) {
	if shouldForwardPluginSetting("openai", "image_price_1k") {
		t.Fatal("image price settings should stay inside core")
	}
	if !shouldForwardPluginSetting("openai", "image_enabled") {
		t.Fatal("image_enabled should still be forwarded to the plugin")
	}
	if !shouldForwardPluginSetting("claude", "claude_code_only") {
		t.Fatal("non-openai plugin settings should still be forwarded")
	}
}
