package relaydetect

import (
	"fmt"
	"strings"
)

func compareOfficialSpecBaselines(registry ScenarioRegistry, models []ModelResult) []BaselineDiff {
	diffs := make([]BaselineDiff, 0, len(models))
	for _, model := range models {
		protocol := ""
		switch model.Family {
		case "claude":
			protocol = "anthropic"
		case "gpt":
			protocol = "openai"
		}
		if protocol == "" || model.Protocol != protocol {
			continue
		}
		spec, ok := registry.SpecForProtocol(protocol)
		if !ok {
			continue
		}
		diff := compareModelToSpec(model, spec)
		diffs = append(diffs, diff)
	}
	return diffs
}

func compareModelToSpec(model ModelResult, spec OfficialSpecBaseline) BaselineDiff {
	differences := make([]string, 0)
	metrics := map[string]any{
		"usage_fields":       model.UsageFields,
		"stream_events":      model.Stream.Events,
		"response_id_prefix": model.ResponseIDPrefix,
	}
	for _, field := range spec.RequiredUsageFields {
		if !containsString(model.UsageFields, field) {
			differences = append(differences, "missing_usage_field:"+field)
		}
	}
	for _, event := range spec.RequiredStreamEvents {
		if !containsString(model.Stream.Events, event) {
			differences = append(differences, "missing_stream_event:"+event)
		}
	}
	if len(spec.ExpectedResponseIDPrefixes) > 0 && model.ResponseIDPrefix != "" && !containsString(spec.ExpectedResponseIDPrefixes, model.ResponseIDPrefix) {
		differences = append(differences, "unexpected_response_id_prefix:"+model.ResponseIDPrefix)
	}
	if spec.Protocol == "anthropic" {
		metrics["thinking_events"] = model.Thinking.Events
		if model.Thinking.Tested && model.Thinking.Supported {
			if !model.Thinking.HasThinkingContent {
				differences = append(differences, "missing_thinking_delta")
			}
			if !model.Thinking.HasSignatureDelta {
				differences = append(differences, "missing_signature_delta")
			}
			if !model.Thinking.SignatureStructureOK {
				differences = append(differences, "invalid_signature_structure")
			}
			if !model.Thinking.EventOrderOK {
				differences = append(differences, "invalid_thinking_event_order")
			}
			if !model.Thinking.FakeSignatureRejected {
				differences = append(differences, "fake_signature_not_rejected")
			}
		}
		cacheFieldsSeen := model.Cache.HasCacheFields || model.CacheCreationTokens > 0 || model.CacheReadTokens > 0
		if model.Cache.Tested && !cacheFieldsSeen {
			differences = append(differences, "missing_cache_usage_fields")
		}
	}
	status := "pass"
	severity := "low"
	conclusion := fmt.Sprintf("%s/%s 结构符合官方文档基线。", spec.Provider, spec.Protocol)
	if len(differences) > 0 {
		status = "fail"
		severity = baselineSeverity(differences)
		conclusion = fmt.Sprintf("%s/%s 与官方文档基线存在 %d 项差异。", spec.Provider, spec.Protocol, len(differences))
	}
	return BaselineDiff{
		Kind:        "official_spec",
		Provider:    spec.Provider,
		Protocol:    spec.Protocol,
		Source:      spec.Source,
		Model:       model.Model,
		Status:      status,
		Severity:    severity,
		Conclusion:  conclusion,
		Differences: differences,
		Metrics:     metrics,
	}
}

func baselineSeverity(differences []string) string {
	for _, item := range differences {
		if strings.Contains(item, "signature") || strings.Contains(item, "stream_event") || strings.Contains(item, "response_id_prefix") {
			return "high"
		}
	}
	return "medium"
}
