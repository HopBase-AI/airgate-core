package handler

import (
	"testing"
	"time"

	appgenerationtask "github.com/DouDOU-start/airgate-core/internal/app/generationtask"
)

func TestToGenerationTaskRespCarriesAdminDiagnostics(t *testing.T) {
	createdAt := time.Date(2026, 8, 7, 1, 2, 3, 4, time.FixedZone("UTC+8", 8*60*60))
	item := appgenerationtask.Task{
		ID:                96,
		PublicTaskID:      "seedance-failure:request-502",
		PluginID:          "airgate-seedance",
		TaskType:          "asset.attempt",
		Kind:              "asset",
		Status:            "failed",
		Stage:             "routing",
		UserID:            103,
		ErrorType:         "upstream_error",
		ErrorCode:         "account_unavailable",
		ErrorMessage:      "分组不支持该模型",
		RequestID:         "request-502",
		GroupID:           21,
		APIKeyID:          206,
		AccountID:         33,
		UpstreamStatus:    502,
		UpstreamErrorCode: "model_not_served",
		CreatedAt:         createdAt,
		UpdatedAt:         createdAt,
	}

	resp := toGenerationTaskResp(item)
	if resp.RequestID != item.RequestID || resp.GroupID != 21 || resp.APIKeyID != 206 ||
		resp.AccountID != 33 || resp.UpstreamStatus != 502 || resp.UpstreamErrorCode != "model_not_served" {
		t.Fatalf("admin diagnostics = %+v", resp)
	}
	if resp.Stage != "routing" || resp.ErrorMessage != "分组不支持该模型" {
		t.Fatalf("failure details = %+v", resp)
	}
	if resp.CreatedAt != "2026-08-06T17:02:03.000000004Z" {
		t.Fatalf("CreatedAt = %q", resp.CreatedAt)
	}
}
