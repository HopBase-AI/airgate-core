package plugin

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"entgo.io/ent/dialect/sql/schema"
	"github.com/DouDOU-start/airgate-core/ent/enttest"
	"github.com/DouDOU-start/airgate-core/internal/auth"
	sdk "github.com/DouDOU-start/airgate-sdk/sdkgo"
	_ "github.com/mattn/go-sqlite3"
)

func TestScopeModelListResponseToRoutingSynthesizesUnknownExplicitModel(t *testing.T) {
	body := []byte(`{"object":"list","data":[{"id":"gpt-5.4","object":"model"},{"id":"gpt-image-2","object":"model"}]}`)
	scoped, changed, err := scopeModelListResponseToRouting(body, map[string][]int64{
		"GLM-5.2": {101},
	})
	if err != nil {
		t.Fatalf("scopeModelListResponseToRouting returned error: %v", err)
	}
	if !changed {
		t.Fatal("scopeModelListResponseToRouting changed = false, want true")
	}
	ids := decodeModelIDs(t, scoped)
	if len(ids) != 1 || ids[0] != "GLM-5.2" {
		t.Fatalf("model ids = %v, want [GLM-5.2]", ids)
	}
}

func TestScopeModelListResponseToRoutingPreservesKnownModelMetadata(t *testing.T) {
	body := []byte(`{"object":"list","data":[{"id":"gpt-5.4","object":"model","context_window":200000},{"id":"gpt-5.5","object":"model"}]}`)
	scoped, _, err := scopeModelListResponseToRouting(body, map[string][]int64{
		"gpt-5.4": {101},
	})
	if err != nil {
		t.Fatalf("scopeModelListResponseToRouting returned error: %v", err)
	}
	var payload struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal(scoped, &payload); err != nil {
		t.Fatalf("decode scoped response: %v", err)
	}
	if len(payload.Data) != 1 {
		t.Fatalf("models count = %d, want 1", len(payload.Data))
	}
	if payload.Data[0]["id"] != "gpt-5.4" {
		t.Fatalf("id = %v, want gpt-5.4", payload.Data[0]["id"])
	}
	if got := int(payload.Data[0]["context_window"].(float64)); got != 200000 {
		t.Fatalf("context_window = %d, want 200000", got)
	}
}

func TestScopeModelListResponseToRoutingExpandsWildcardFromCatalog(t *testing.T) {
	body := []byte(`{"object":"list","data":[{"id":"gpt-5.4","object":"model"},{"id":"o3","object":"model"}]}`)
	scoped, _, err := scopeModelListResponseToRouting(body, map[string][]int64{
		"GLM-5.2": {},
		"gpt-*":   {101},
	})
	if err != nil {
		t.Fatalf("scopeModelListResponseToRouting returned error: %v", err)
	}
	ids := decodeModelIDs(t, scoped)
	if len(ids) != 1 || ids[0] != "gpt-5.4" {
		t.Fatalf("model ids = %v, want [gpt-5.4]", ids)
	}
}

func TestScopeMetadataOnlyModelsUsesCurrentGroupRouting(t *testing.T) {
	ctx := context.Background()
	db := enttest.Open(t, "sqlite3", "file:metadata_models?mode=memory&cache=shared&_fk=1", enttest.WithMigrateOptions(schema.WithGlobalUniqueID(false)))
	t.Cleanup(func() { _ = db.Close() })
	group := db.Group.Create().
		SetName("glm").
		SetPlatform("openai").
		SetModelRouting(map[string][]int64{"GLM-5.2": {101}}).
		SaveX(ctx)

	f := &Forwarder{db: db}
	outcome := sdk.ForwardOutcome{
		Upstream: sdk.UpstreamResponse{
			StatusCode: http.StatusOK,
			Headers:    http.Header{"Content-Type": {"application/json"}},
			Body:       []byte(`{"object":"list","data":[{"id":"gpt-5.4","object":"model"}]}`),
		},
	}
	state := &forwardState{
		requestPath: "/v1/models",
		keyInfo:     &auth.APIKeyInfo{GroupID: group.ID},
	}
	if err := f.scopeMetadataOnlyModels(ctx, state, &outcome); err != nil {
		t.Fatalf("scopeMetadataOnlyModels returned error: %v", err)
	}
	ids := decodeModelIDs(t, outcome.Upstream.Body)
	if len(ids) != 1 || ids[0] != "GLM-5.2" {
		t.Fatalf("model ids = %v, want [GLM-5.2]", ids)
	}
}

func TestIsModelListMetadataPath(t *testing.T) {
	cases := map[string]bool{
		"/v1/models":       true,
		"/v1/models?limit": true,
		"/models":          true,
		"/models?limit":    true,
		"/v1/models/foo":   false,
		"/v1/images":       false,
	}
	for path, want := range cases {
		if got := isModelListMetadataPath(path); got != want {
			t.Fatalf("isModelListMetadataPath(%q) = %v, want %v", path, got, want)
		}
	}
}

func decodeModelIDs(t *testing.T, body []byte) []string {
	t.Helper()
	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode model list: %v; body=%s", err, body)
	}
	ids := make([]string, 0, len(payload.Data))
	for _, item := range payload.Data {
		ids = append(ids, item.ID)
	}
	return ids
}
