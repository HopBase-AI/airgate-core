package billing

import "testing"

func TestIsFixedImageTierPricingModelMatchesBillingContract(t *testing.T) {
	tests := []struct {
		model string
		want  bool
	}{
		{model: "gpt-image-2", want: true},
		{model: "dall-e-3", want: true},
		{model: "gemini-3-pro-image", want: true},
		{model: "gemini-3.1-flash-image-preview", want: true},
		{model: "gemini-3.5-flash", want: false},
		{model: "seedream-5-0-pro", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			if got := IsFixedImageTierPricingModel(tt.model); got != tt.want {
				t.Fatalf("IsFixedImageTierPricingModel(%q) = %v, want %v", tt.model, got, tt.want)
			}
		})
	}
}

func TestImageTierPriceFromSettingsAcceptsFiniteNonNegativePrices(t *testing.T) {
	tests := []struct {
		name  string
		raw   string
		want  float64
		valid bool
	}{
		{name: "zero is a valid free price", raw: "0", want: 0, valid: true},
		{name: "finite price", raw: "0.08", want: 0.08, valid: true},
		{name: "nan", raw: "NaN", valid: false},
		{name: "positive infinity", raw: "+Inf", valid: false},
		{name: "negative infinity", raw: "-Inf", valid: false},
		{name: "negative", raw: "-0.01", valid: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			settings := map[string]map[string]string{
				"openai": {ImagePrice1KKey: tt.raw},
			}
			got, ok := ImageTierPriceFromSettings(settings, "1k")
			if ok != tt.valid || (ok && got != tt.want) {
				t.Fatalf("ImageTierPriceFromSettings(%q) = (%v, %v), want (%v, %v)", tt.raw, got, ok, tt.want, tt.valid)
			}
		})
	}
}
