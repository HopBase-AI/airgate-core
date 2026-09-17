package plugin

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	sdk "github.com/DouDOU-start/airgate-sdk/sdkgo"

	appsubscription "github.com/DouDOU-start/airgate-core/internal/app/subscription"
	"github.com/DouDOU-start/airgate-core/internal/billing"
)

// Trusted request-cost bounds for subscription admission.
//
// A subscription reservation is only meaningful if it is an upper bound on what
// the request can cost upstream. Reserving a flat per-request allowance is not:
// the allowance bounds what a customer may spend, not what the supplier may
// charge, and the difference is paid by us. This file derives the bound before
// the request is sent, from data the owning plugin declares in its catalog.
//
// The model catalog's price.* metadata was previously documented as a display
// contract. For subscription groups it is authoritative: a model whose catalog
// entry does not support a bound cannot be sold on a subscription, and its
// requests are denied with ErrRequestCostUnbounded rather than admitted at an
// unknown cost. Balance-billed groups are unaffected.
//
// Required catalog metadata per request kind:
//
//	chat   price.input and price.output (USD per 1M tokens) plus an output
//	       ceiling: ModelInfo.MaxOutputTokens, or a route-declared
//	       subscription_output_bound contract.
//	image  price.image.<bucket> (USD per image); the most expensive bucket is
//	       used and multiplied by the requested image count.
//	video  price.request_max (USD for one request), because duration and
//	       resolution are request-shaped and only the plugin can price them.
//
// price.request_max overrides the derivation for any kind. For image models it
// is read as the bound for one image.
const (
	// subscriptionCacheWriteSurcharge covers prompt-cache writes, which every
	// supported vendor prices above plain input, and small protocol overhead
	// that is charged as input tokens.
	subscriptionCacheWriteSurcharge = 1.25
	// subscriptionASCIIBytesPerToken is the usual ASCII packing density.
	// Non-ASCII runes are counted as one token each instead, because dividing
	// UTF-8 bytes by four underestimates Chinese prompts roughly threefold and
	// this product's customers write Chinese.
	subscriptionASCIIBytesPerToken = 4
	// subscriptionMaxOutputCeiling caps a route-declared ceiling so a malformed
	// declaration cannot produce a bound large enough to overflow accounting.
	subscriptionMaxOutputCeiling = 10_000_000
)

// subscriptionBoundRequest is one admission's inputs.
type subscriptionBoundRequest struct {
	Kind   billing.RequestKind
	Model  string
	Body   []byte
	Images int
	// Rate converts official USD into the balance unit the plan's credits are
	// defined in; it is the same effective group rate settlement uses.
	Rate       float64
	PluginName string
	Path       string
}

// ModelCatalogEntry returns the catalog entry a plugin declared for a model.
func (m *Manager) ModelCatalogEntry(modelID string) (sdk.ModelInfo, bool) {
	if m == nil || modelID == "" {
		return sdk.ModelInfo{}, false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, models := range m.modelCache {
		for i := range models {
			if strings.EqualFold(models[i].ID, modelID) {
				return models[i], true
			}
		}
	}
	return sdk.ModelInfo{}, false
}

// subscriptionRequestBound returns the maximum credits one request can consume,
// or ErrRequestCostUnbounded when the catalog cannot support a bound.
func (m *Manager) subscriptionRequestBound(req subscriptionBoundRequest, quotas billing.PlanQuotas) (int64, error) {
	if m == nil || req.Rate <= 0 || math.IsNaN(req.Rate) || math.IsInf(req.Rate, 0) {
		return 0, appsubscription.ErrRequestCostUnbounded
	}
	info, ok := m.ModelCatalogEntry(req.Model)
	if !ok {
		return 0, appsubscription.ErrRequestCostUnbounded
	}
	usd, err := m.subscriptionRequestCostUSD(info, req)
	if err != nil {
		return 0, err
	}
	credits := quotas.Credits(usd * req.Rate)
	if credits < 0 {
		return 0, appsubscription.ErrRequestCostUnbounded
	}
	return credits, nil
}

func (m *Manager) subscriptionRequestCostUSD(info sdk.ModelInfo, req subscriptionBoundRequest) (float64, error) {
	units := 1
	if req.Kind == billing.RequestKindImage {
		units = max(req.Images, 1)
	}
	if perUnit, ok := catalogPrice(info.Metadata, "price.request_max"); ok {
		return perUnit * float64(units), nil
	}
	switch req.Kind {
	case billing.RequestKindChat:
		return chatRequestCostUSD(m, info, req)
	case billing.RequestKindImage:
		unit, ok := maxCatalogPriceWithPrefix(info.Metadata, "price.image.")
		if !ok {
			return 0, appsubscription.ErrRequestCostUnbounded
		}
		// A supplied reference image is charged on top of generation.
		reference, _ := catalogPrice(info.Metadata, "price.image.input_reference")
		return (unit + reference) * float64(units), nil
	default:
		// Video duration and resolution are request-shaped. A plugin either
		// declares price.request_max, or declares on the submit route that it
		// quotes the bound itself when it creates the durable task — which is
		// the reservation that actually holds the customer's points for an
		// asynchronous job. Anything else is unbounded.
		if m.subscriptionDeferredBound(req.PluginName, req.Path) {
			return 0, nil
		}
		return 0, appsubscription.ErrRequestCostUnbounded
	}
}

// subscriptionDeferredBound reports whether a route binds its own reservation
// later, from the plugin's estimated_official_cost, instead of from the catalog.
// Admission then reserves nothing here; the task reservation refuses to admit
// without a positive estimate, so the request is still bounded before it is
// submitted upstream.
func (m *Manager) subscriptionDeferredBound(pluginName, path string) bool {
	if m == nil || pluginName == "" {
		return false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, route := range m.routeCache[m.resolveNameLocked(pluginName)] {
		if route.Metadata["subscription_deferred_bound"] != "true" {
			continue
		}
		if route.Path == path {
			return true
		}
	}
	return false
}

func chatRequestCostUSD(m *Manager, info sdk.ModelInfo, req subscriptionBoundRequest) (float64, error) {
	inputPrice, okIn := catalogPrice(info.Metadata, "price.input")
	outputPrice, okOut := catalogPrice(info.Metadata, "price.output")
	if !okIn || !okOut {
		return 0, appsubscription.ErrRequestCostUnbounded
	}
	outputTokens := m.subscriptionOutputCeiling(info, req)
	if outputTokens <= 0 {
		return 0, appsubscription.ErrRequestCostUnbounded
	}
	inputTokens := estimatePromptTokens(req.Body)
	inputUSD := float64(inputTokens) / 1e6 * inputPrice * subscriptionCacheWriteSurcharge
	outputUSD := float64(outputTokens) / 1e6 * outputPrice
	return inputUSD + outputUSD, nil
}

// subscriptionOutputCeiling is the largest number of output tokens the upstream
// can bill for this request. It is the model's declared ceiling, narrowed by a
// route-declared ceiling and by the caller's own smaller limit. Zero means no
// trusted ceiling exists.
func (m *Manager) subscriptionOutputCeiling(info sdk.ModelInfo, req subscriptionBoundRequest) int {
	contract := m.subscriptionOutputBound(req.PluginName, req.Path)
	ceiling := info.MaxOutputTokens
	if contract.Ceiling > 0 && (ceiling <= 0 || contract.Ceiling < ceiling) {
		ceiling = contract.Ceiling
	}
	if ceiling <= 0 || ceiling > subscriptionMaxOutputCeiling {
		if ceiling > subscriptionMaxOutputCeiling {
			return subscriptionMaxOutputCeiling
		}
		return 0
	}
	if requested := requestedOutputTokens(req.Body, contract.Fields); requested > 0 && requested < ceiling {
		return requested
	}
	return ceiling
}

// subscriptionOutputBoundContract is the route metadata a plugin declares so
// Core can read the caller's output limit out of a protocol it does not own.
type subscriptionOutputBoundContract struct {
	Fields  []string `json:"fields"`
	Ceiling int      `json:"ceiling"`
}

func (m *Manager) subscriptionOutputBound(pluginName, path string) subscriptionOutputBoundContract {
	var contract subscriptionOutputBoundContract
	if m == nil || pluginName == "" {
		return contract
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	prefixRaw := ""
	for _, route := range m.routeCache[m.resolveNameLocked(pluginName)] {
		raw := route.Metadata["subscription_output_bound"]
		if raw == "" {
			continue
		}
		if route.Path == path {
			prefixRaw = raw
			break
		}
		if prefixRaw == "" && matchRoutePath(route.Path, path) {
			prefixRaw = raw
		}
	}
	if prefixRaw == "" || json.Unmarshal([]byte(prefixRaw), &contract) != nil {
		return subscriptionOutputBoundContract{}
	}
	return contract
}

// requestedOutputTokens reads the caller's own output limit from the body.
func requestedOutputTokens(body []byte, fields []string) int {
	if len(body) == 0 || len(fields) == 0 {
		return 0
	}
	var decoded map[string]json.RawMessage
	if json.Unmarshal(body, &decoded) != nil {
		return 0
	}
	for _, field := range fields {
		raw, ok := decoded[strings.TrimSpace(field)]
		if !ok {
			continue
		}
		var value float64
		if json.Unmarshal(raw, &value) != nil || value <= 0 || value > subscriptionMaxOutputCeiling {
			continue
		}
		return int(math.Ceil(value))
	}
	return 0
}

// estimatePromptTokens conservatively counts the request body as prompt tokens.
// Non-ASCII runes count as a whole token each; ASCII packs four bytes per token.
func estimatePromptTokens(body []byte) int64 {
	if len(body) == 0 {
		return 0
	}
	asciiBytes := 0
	nonASCII := int64(0)
	for i := 0; i < len(body); {
		r, size := utf8.DecodeRune(body[i:])
		if r < unicode.MaxASCII && size == 1 {
			asciiBytes++
		} else {
			nonASCII++
		}
		i += size
	}
	tokens := nonASCII + int64(math.Ceil(float64(asciiBytes)/subscriptionASCIIBytesPerToken))
	if tokens < 1 {
		return 1
	}
	return tokens
}

// catalogPrice parses one declared USD price. Currency symbols and thousands
// separators are tolerated because the catalog is authored for display too.
func catalogPrice(metadata map[string]string, key string) (float64, bool) {
	if len(metadata) == 0 {
		return 0, false
	}
	raw := strings.TrimSpace(metadata[key])
	raw = strings.TrimSpace(strings.TrimPrefix(raw, "$"))
	raw = strings.ReplaceAll(raw, ",", "")
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil || value <= 0 || math.IsInf(value, 0) {
		return 0, false
	}
	return value, true
}

// maxCatalogPriceWithPrefix takes the most expensive declared bucket, so a
// request that does not name its bucket still cannot cost more than the bound.
func maxCatalogPriceWithPrefix(metadata map[string]string, prefix string) (float64, bool) {
	highest := 0.0
	found := false
	for key := range metadata {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		if value, ok := catalogPrice(metadata, key); ok && value > highest {
			highest, found = value, true
		}
	}
	return highest, found
}
