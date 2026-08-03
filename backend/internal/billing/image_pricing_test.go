package billing

import "testing"

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
