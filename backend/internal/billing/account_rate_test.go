package billing

import (
	"encoding/json"
	"testing"
)

func TestResolveAccountRateForModel(t *testing.T) {
	extra := map[string]interface{}{
		AccountModelRateMultipliersKey: map[string]interface{}{
			"dreamina-seedance-2-0-mini-hc": 0.336,
			"Dreamina-Seedance-2-0-Fast-HC": 0.63,
			"model-int":                     1,
			"model-json-number":             json.Number("0.84"),
			"model-bad-type":                "0.5",
			"model-zero":                    0.0,
			"model-negative":                -1.2,
		},
	}
	cases := []struct {
		name     string
		extra    map[string]interface{}
		model    string
		fallback float64
		want     float64
	}{
		{"exact match", extra, "dreamina-seedance-2-0-mini-hc", 0.79, 0.336},
		{"case insensitive both sides", extra, "dreamina-seedance-2-0-fast-hc", 0.79, 0.63},
		{"whitespace trimmed", extra, "  dreamina-seedance-2-0-mini-hc ", 0.79, 0.336},
		{"int value", extra, "model-int", 0.79, 1},
		{"json.Number value", extra, "model-json-number", 0.79, 0.84},
		{"string value falls back", extra, "model-bad-type", 0.79, 0.79},
		{"zero falls back", extra, "model-zero", 0.79, 0.79},
		{"negative falls back", extra, "model-negative", 0.79, 0.79},
		{"unlisted model falls back", extra, "gpt-5.6", 0.79, 0.79},
		{"empty model falls back", extra, "", 0.79, 0.79},
		{"nil extra falls back", nil, "dreamina-seedance-2-0-mini-hc", 0.79, 0.79},
		{"missing key falls back", map[string]interface{}{"other": 1}, "m", 0.79, 0.79},
		{"malformed overrides falls back", map[string]interface{}{
			AccountModelRateMultipliersKey: []interface{}{"x"},
		}, "m", 0.79, 0.79},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ResolveAccountRateForModel(tc.extra, tc.model, tc.fallback); got != tc.want {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}
