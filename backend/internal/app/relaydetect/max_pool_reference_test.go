package relaydetect

import "testing"

func referenceCatalog(models ...string) modelListResult {
	return modelListResult{route: "/v1/models", statusCode: 200, models: models}
}

func referenceModel(model string, available bool, status int) ModelResult {
	return ModelResult{
		Model:            model,
		RequestedModel:   model,
		ReturnedModel:    model,
		ResponseID:       "msg_reference",
		ResponseIDPrefix: "msg",
		Family:           "claude",
		Protocol:         "anthropic",
		Available:        available,
		HTTPStatus:       status,
		UsageFields:      []string{"input_tokens", "output_tokens"},
		Quality: QualityProbe{Tested: true, Applicable: true, OK: true, Cases: []QualityCase{
			{ID: "strict_json", OK: true, HTTPStatus: 200},
			{ID: "forced_tool_call", OK: true, HTTPStatus: 200},
		}},
		Stream:   StreamProbe{Tested: true, OK: true, HTTPStatus: 200},
		Thinking: ThinkingProbe{Tested: true, Supported: true, HasThinkingContent: true, HasSignatureDelta: true},
	}
}

func findReferenceModelComparison(t *testing.T, report MaxPoolReferenceComparison, name string) ReferenceModelComparison {
	t.Helper()
	for _, item := range report.Models {
		if item.ReferenceModel == name {
			return item
		}
	}
	t.Fatalf("reference model %q not found in %#v", name, report.Models)
	return ReferenceModelComparison{}
}

func findReferenceCheck(t *testing.T, model ReferenceModelComparison, id string) ReferenceCheckResult {
	t.Helper()
	for _, item := range model.Checks {
		if item.ID == id {
			return item
		}
	}
	t.Fatalf("reference check %q not found in %#v", id, model.Checks)
	return ReferenceCheckResult{}
}

func TestCompareMaxPoolReferenceTreatsNewAPIGatewayAsNeutral(t *testing.T) {
	model := referenceModel("claude-opus-4-8", true, 200)
	model.Transport.ResponseHeaders = map[string]string{
		"x-new-api-version":   "v1.0.0-rc.10",
		"x-oneapi-request-id": "req-gateway",
	}
	report := compareMaxPoolReference(referenceCatalog(
		"claude-fable-5",
		"claude-haiku-4-5-20251001",
		"claude-opus-4-6",
		"claude-opus-4-7",
		"claude-opus-4-8",
		"claude-opus-5",
		"claude-sonnet-4-6",
		"claude-sonnet-5",
	), []ModelResult{model})

	if !report.GatewayLayerDetected || len(report.GatewayProducts) != 1 || report.GatewayProducts[0] != "new_api/one_api" {
		t.Fatalf("gateway evidence = %#v, want neutral New API marker", report)
	}
	opus := findReferenceModelComparison(t, report, "claude-opus-4-8")
	if opus.Status != "meets_reference" {
		t.Fatalf("opus comparison = %#v, want meets_reference", opus)
	}
	for _, check := range opus.Checks {
		if check.Status == "regression" {
			t.Fatalf("gateway headers must not create a regression: %#v", opus.Checks)
		}
	}
}

func TestCompareMaxPoolReferenceMarksGenerationFailureAsRegression(t *testing.T) {
	report := compareMaxPoolReference(
		referenceCatalog("claude-opus-4-8"),
		[]ModelResult{{
			Model:          "claude-opus-4-8",
			RequestedModel: "claude-opus-4-8",
			Family:         "claude",
			Protocol:       "anthropic",
			HTTPStatus:     500,
			Error:          "Failed to validate API key",
		}},
	)
	opus := findReferenceModelComparison(t, report, "claude-opus-4-8")
	if opus.Status != "regression" {
		t.Fatalf("failed generation comparison = %#v, want regression", opus)
	}
	effective := findReferenceCheck(t, opus, "effective_response")
	if effective.Status != "regression" || effective.Observed != false {
		t.Fatalf("effective response check = %#v, want failed response regression", effective)
	}
}

func TestCompareMaxPoolReferenceAcceptsKnownDatedHaikuAlias(t *testing.T) {
	model := referenceModel("claude-haiku-4-5-20251001", true, 200)
	report := compareMaxPoolReference(referenceCatalog("claude-haiku-4-5-20251001"), []ModelResult{model})
	haiku := findReferenceModelComparison(t, report, "claude-haiku-4-5")
	if haiku.Status != "meets_reference" {
		t.Fatalf("haiku alias comparison = %#v, want meets_reference", haiku)
	}
	modelCheck := findReferenceCheck(t, haiku, "response_model")
	if modelCheck.Status != "meets_reference" {
		t.Fatalf("haiku response model check = %#v, want meets_reference", modelCheck)
	}
}

func TestCompareMaxPoolReferenceKeepsProviderIDVariantInformational(t *testing.T) {
	model := referenceModel("claude-opus-4-8", true, 200)
	model.ResponseID = "msg_bdrk_reference"
	model.ResponseIDPrefix = "msg_bdrk"
	report := compareMaxPoolReference(referenceCatalog("claude-opus-4-8"), []ModelResult{model})
	opus := findReferenceModelComparison(t, report, "claude-opus-4-8")
	if opus.Status != "meets_reference" {
		t.Fatalf("provider id variant should remain comparable, got %#v", opus)
	}
	idCheck := findReferenceCheck(t, opus, "response_id_prefix")
	if idCheck.Status != "not_comparable" {
		t.Fatalf("id variant check = %#v, want not_comparable", idCheck)
	}
}
