package relaydetect

import (
	"fmt"
	"strings"
)

// maxPoolReferenceID is the active functional/protocol baseline. It is not an
// upstream provenance claim; matching it only means that the observed gateway
// behavior is comparable to the frozen reference channel.
const maxPoolReferenceID = "derouter-20260807T170935+0800"
const maxPoolReferenceSHA256 = "d01107558b526015b54bb7d32cfff68d6c908336bc0392d02ec09ce5aca2149b"

type maxPoolReferenceModel struct {
	Name     string
	Thinking string
}

type ReferenceCheckResult struct {
	ID       string `json:"id"`
	Status   string `json:"status"`
	Expected any    `json:"expected,omitempty"`
	Observed any    `json:"observed,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

type ReferenceModelComparison struct {
	ReferenceModel string                 `json:"reference_model"`
	ObservedModel  string                 `json:"observed_model,omitempty"`
	Status         string                 `json:"status"`
	Checks         []ReferenceCheckResult `json:"checks"`
}

type MaxPoolReferenceComparison struct {
	BaselineID             string                     `json:"baseline_id"`
	BaselineSHA256         string                     `json:"baseline_sha256"`
	Source                 string                     `json:"source"`
	Status                 string                     `json:"status"`
	Models                 []ReferenceModelComparison `json:"models"`
	ExpectedModelCount     int                        `json:"expected_model_count"`
	ObservedCatalogCount   int                        `json:"observed_catalog_count"`
	ObservedUnlistedModels []string                   `json:"observed_unlisted_models,omitempty"`
	GatewayLayerDetected   bool                       `json:"gateway_layer_detected"`
	GatewayProducts        []string                   `json:"gateway_products,omitempty"`
	Notes                  []string                   `json:"notes,omitempty"`
}

func maxPoolReferenceModels() []maxPoolReferenceModel {
	return []maxPoolReferenceModel{
		{Name: "claude-fable-5", Thinking: "pass"},
		{Name: "claude-haiku-4-5", Thinking: "pass"},
		{Name: "claude-opus-4-6", Thinking: "pass"},
		{Name: "claude-opus-4-7", Thinking: "known_exception"},
		{Name: "claude-opus-4-8", Thinking: "pass"},
		{Name: "claude-opus-5", Thinking: "pass"},
		{Name: "claude-sonnet-4-6", Thinking: "pass"},
		{Name: "claude-sonnet-5", Thinking: "known_exception"},
	}
}

func canonicalReferenceModel(model string) string {
	return normalizeModelAlias(strings.ToLower(strings.TrimSpace(model)))
}

func referenceCheck(id, status string, expected, observed any, reason string) ReferenceCheckResult {
	return ReferenceCheckResult{
		ID:       id,
		Status:   status,
		Expected: expected,
		Observed: observed,
		Reason:   reason,
	}
}

func findReferenceResult(referenceModel string, models []ModelResult) *ModelResult {
	for i := range models {
		item := &models[i]
		for _, candidate := range []string{item.RequestedModel, item.ReturnedModel, item.Model} {
			if canonicalReferenceModel(candidate) == referenceModel {
				return item
			}
		}
	}
	return nil
}

func catalogHasReferenceModel(referenceModel string, catalog modelListResult) bool {
	for _, model := range catalog.models {
		if canonicalReferenceModel(model) == referenceModel {
			return true
		}
	}
	return false
}

func qualityCaseFor(model ModelResult, id string) (QualityCase, bool) {
	for _, item := range model.Quality.Cases {
		if item.ID == id {
			return item, true
		}
	}
	return QualityCase{}, false
}

func referenceModelStatus(checks []ReferenceCheckResult) string {
	hasNotTested := false
	hasBetter := false
	for _, check := range checks {
		switch check.Status {
		case "regression":
			return "regression"
		case "not_tested":
			hasNotTested = true
		case "better_than_reference":
			hasBetter = true
		}
	}
	if hasNotTested {
		return "not_tested"
	}
	if hasBetter {
		return "better_than_reference"
	}
	return "meets_reference"
}

func referenceAggregateStatus(models []ReferenceModelComparison) string {
	hasNotTested := false
	hasBetter := false
	for _, model := range models {
		switch model.Status {
		case "regression":
			return "regression"
		case "not_tested":
			hasNotTested = true
		case "better_than_reference":
			hasBetter = true
		}
	}
	if hasNotTested {
		return "not_tested"
	}
	if hasBetter {
		return "better_than_reference"
	}
	return "meets_reference"
}

func gatewayProducts(models []ModelResult) []string {
	seen := map[string]bool{}
	for _, model := range models {
		for key := range model.Transport.ResponseHeaders {
			switch strings.ToLower(key) {
			case "x-new-api-version", "x-oneapi-request-id":
				seen["new_api/one_api"] = true
			}
		}
	}
	products := make([]string, 0, len(seen))
	for product := range seen {
		products = append(products, product)
	}
	return products
}

func compareMaxPoolReference(catalog modelListResult, models []ModelResult) MaxPoolReferenceComparison {
	referenceModels := maxPoolReferenceModels()
	comparison := MaxPoolReferenceComparison{
		BaselineID:           maxPoolReferenceID,
		BaselineSHA256:       maxPoolReferenceSHA256,
		Source:               "Derouter functional/protocol reference",
		ExpectedModelCount:   len(referenceModels),
		ObservedCatalogCount: len(catalog.models),
		Models:               make([]ReferenceModelComparison, 0, len(referenceModels)),
		GatewayProducts:      gatewayProducts(models),
		Notes: []string{
			"New API/One API gateway headers are transport evidence and are neutral for this functional comparison.",
			"Latency, message/request id appearance, and model provenance are informational or out of scope for this baseline.",
		},
	}
	comparison.GatewayLayerDetected = len(comparison.GatewayProducts) > 0

	for _, expected := range referenceModels {
		checks := make([]ReferenceCheckResult, 0, 9)
		observed := findReferenceResult(expected.Name, models)
		catalogPresent := catalogHasReferenceModel(expected.Name, catalog)
		catalogStatus := "not_tested"
		catalogReason := "模型目录未形成可判定结果"
		if catalog.statusCode >= 200 && catalog.statusCode < 300 {
			catalogStatus = "meets_reference"
			catalogReason = "模型目录包含参考模型或其已知 dated alias"
			if !catalogPresent {
				catalogStatus = "regression"
				catalogReason = "模型目录缺少激活参考中的模型"
			}
		}
		checks = append(checks, referenceCheck("catalog_presence", catalogStatus, true, catalogPresent, catalogReason))

		observedModel := ""
		if observed != nil {
			observedModel = firstNonEmpty(observed.ReturnedModel, observed.Model)
		}
		effectiveStatus := "not_tested"
		effectiveReason := "该模型尚未执行生成探针"
		if observed != nil {
			switch {
			case observed.Available && observed.HTTPStatus >= 200 && observed.HTTPStatus < 300:
				effectiveStatus = "meets_reference"
				effectiveReason = "返回了有效且非空的模型响应"
			case observed.HTTPStatus == 0:
				effectiveReason = "请求未形成 HTTP 结果"
			default:
				effectiveStatus = "regression"
				effectiveReason = fmtReferenceHTTPFailure(observed.HTTPStatus, observed.Error)
			}
		}
		observedAvailable := observed != nil && observed.Available
		checks = append(checks, referenceCheck("effective_response", effectiveStatus, "non_empty_response", observedAvailable, effectiveReason))

		modelStatus := "not_tested"
		modelReason := "没有可比较的 response.model"
		if observed != nil && observed.Available {
			if observedModel == "" {
				modelStatus = "regression"
				modelReason = "有效响应缺少 response.model"
			} else if canonicalReferenceModel(observedModel) == expected.Name {
				modelStatus = "meets_reference"
				modelReason = "response.model 与请求 alias 或已知 dated alias 一致"
			} else {
				modelStatus = "regression"
				modelReason = "response.model 与请求模型不一致"
			}
		}
		checks = append(checks, referenceCheck("response_model", modelStatus, expected.Name, observedModel, modelReason))

		idStatus := "not_tested"
		idReason := "没有有效响应可比较 message id"
		idObserved := ""
		if observed != nil && observed.Available {
			idObserved = observed.ResponseIDPrefix
			switch {
			case idObserved == "msg":
				idStatus = "meets_reference"
				idReason = "message id 前缀与参考一致"
			case idObserved != "":
				// The reference policy treats id appearance as informational. Keep
				// valid gateway/provider variants visible without failing the model.
				idStatus = "not_comparable"
				idReason = "message id 前缀与参考不同，但该字段仅作信息性协议指纹"
			default:
				idStatus = "regression"
				idReason = "有效响应缺少 message id 前缀"
			}
		}
		checks = append(checks, referenceCheck("response_id_prefix", idStatus, "msg", idObserved, idReason))

		if observed == nil || !observed.Available {
			checks = append(checks,
				referenceCheck("usage_shape", "not_tested", []string{"input_tokens", "output_tokens"}, nil, "没有有效响应可比较 usage"),
				referenceCheck("strict_json", "not_tested", true, nil, "没有执行成功的质量 smoke"),
				referenceCheck("custom_tool", "not_tested", true, nil, "没有执行成功的工具 smoke"),
				referenceCheck("sse", "not_tested", true, nil, "没有执行成功的 SSE 探针"),
			)
		} else {
			usageOK := containsString(observed.UsageFields, "input_tokens") && containsString(observed.UsageFields, "output_tokens")
			checks = append(checks, referenceCheck("usage_shape", map[bool]string{true: "meets_reference", false: "regression"}[usageOK], []string{"input_tokens", "output_tokens"}, observed.UsageFields, "Messages usage 基础字段"))
			jsonCase, jsonTested := qualityCaseFor(*observed, "strict_json")
			checks = append(checks, referenceCheck("strict_json", map[bool]string{true: "meets_reference", false: "regression"}[jsonTested && jsonCase.OK], true, jsonCase.OK, "严格 JSON smoke"))
			toolCase, toolTested := qualityCaseFor(*observed, "forced_tool_call")
			checks = append(checks, referenceCheck("custom_tool", map[bool]string{true: "meets_reference", false: "regression"}[toolTested && toolCase.OK], true, toolCase.OK, "强制工具调用 smoke"))
			checks = append(checks, referenceCheck("sse", map[bool]string{true: "meets_reference", false: "regression"}[observed.Stream.Tested && observed.Stream.OK], true, observed.Stream.OK, "Anthropic SSE 事件序列"))
		}

		thinkingStatus := "not_tested"
		thinkingObserved := any(nil)
		thinkingReason := "未执行 thinking 探针"
		if observed != nil && observed.Thinking.Tested {
			thinkingObserved = map[string]any{
				"supported": observed.Thinking.Supported,
				"content":   observed.Thinking.HasThinkingContent,
				"signature": observed.Thinking.HasSignatureDelta,
			}
			thinkingOK := observed.Thinking.Supported && observed.Thinking.HasThinkingContent && observed.Thinking.HasSignatureDelta
			switch {
			case expected.Thinking == "known_exception" && thinkingOK:
				thinkingStatus = "better_than_reference"
				thinkingReason = "被测结果优于参考基准的已知 thinking 例外"
			case expected.Thinking == "known_exception":
				thinkingStatus = "not_comparable"
				thinkingReason = "参考基准本身记录了该模型 thinking 波动/静默关闭"
			case thinkingOK:
				thinkingStatus = "meets_reference"
				thinkingReason = "thinking 内容与 signature 均存在"
			default:
				thinkingStatus = "regression"
				thinkingReason = "参考基准要求 thinking 内容与 signature"
			}
		}
		checks = append(checks, referenceCheck("thinking", thinkingStatus, expected.Thinking, thinkingObserved, thinkingReason))

		comparison.Models = append(comparison.Models, ReferenceModelComparison{
			ReferenceModel: expected.Name,
			ObservedModel:  observedModel,
			Status:         referenceModelStatus(checks),
			Checks:         checks,
		})
	}
	for _, item := range catalog.models {
		canonical := canonicalReferenceModel(item)
		known := false
		for _, expected := range referenceModels {
			if expected.Name == canonical {
				known = true
				break
			}
		}
		if !known && !containsString(comparison.ObservedUnlistedModels, item) {
			comparison.ObservedUnlistedModels = append(comparison.ObservedUnlistedModels, item)
		}
	}
	comparison.Status = referenceAggregateStatus(comparison.Models)
	return comparison
}

func fmtReferenceHTTPFailure(status int, err string) string {
	if status > 0 {
		if err != "" {
			return "HTTP " + fmt.Sprint(status) + ": " + truncateText(err, 180)
		}
		return "HTTP " + fmt.Sprint(status) + " 未返回有效模型内容"
	}
	return "请求未返回有效模型内容"
}
