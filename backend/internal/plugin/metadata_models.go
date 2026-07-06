package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"sort"
	"strings"
	"time"

	sdk "github.com/DouDOU-start/airgate-sdk/sdkgo"
)

func (f *Forwarder) scopeMetadataOnlyModels(ctx context.Context, state *forwardState, outcome *sdk.ForwardOutcome) error {
	if f == nil || state == nil || state.keyInfo == nil || outcome == nil {
		return nil
	}
	if !isModelListMetadataPath(state.requestPath) {
		return nil
	}
	status := outcome.Upstream.StatusCode
	if status != 0 && (status < 200 || status >= 300) {
		return nil
	}
	routing, err := f.groupModelRouting(ctx, state.keyInfo.GroupID)
	if err != nil {
		return err
	}
	if len(routing) == 0 {
		return nil
	}
	body, changed, err := scopeModelListResponseToRouting(outcome.Upstream.Body, routing)
	if err != nil {
		return err
	}
	if !changed {
		return nil
	}
	outcome.Upstream.Body = body
	if outcome.Upstream.Headers == nil {
		outcome.Upstream.Headers = http.Header{}
	}
	outcome.Upstream.Headers.Set("Content-Type", "application/json")
	return nil
}

func (f *Forwarder) groupModelRouting(ctx context.Context, groupID int) (map[string][]int64, error) {
	if groupID <= 0 || f == nil || f.db == nil {
		return nil, nil
	}
	grp, err := f.db.Group.Get(ctx, groupID)
	if err != nil {
		return nil, fmt.Errorf("load group model routing: %w", err)
	}
	return grp.ModelRouting, nil
}

func isModelListMetadataPath(path string) bool {
	path = strings.TrimSpace(path)
	if idx := strings.IndexByte(path, '?'); idx >= 0 {
		path = path[:idx]
	}
	return path == "/v1/models" || path == "/models"
}

func scopeModelListResponseToRouting(body []byte, routing map[string][]int64) ([]byte, bool, error) {
	if len(routing) == 0 {
		return body, false, nil
	}

	root, entriesByID, catalogIDs := parseModelListPayload(body)
	visibleIDs := visibleModelIDsForRouting(routing, catalogIDs)
	data := make([]any, 0, len(visibleIDs))
	for _, id := range visibleIDs {
		if entry, ok := entriesByID[id]; ok {
			data = append(data, cloneModelEntry(entry, id))
			continue
		}
		data = append(data, syntheticModelEntry(id))
	}
	if root == nil {
		root = map[string]any{"object": "list"}
	}
	if object, _ := root["object"].(string); strings.TrimSpace(object) == "" {
		root["object"] = "list"
	}
	root["data"] = data
	scoped, err := json.Marshal(root)
	if err != nil {
		return nil, false, err
	}
	return scoped, true, nil
}

func parseModelListPayload(body []byte) (map[string]any, map[string]map[string]any, []string) {
	var root map[string]any
	if len(body) == 0 || json.Unmarshal(body, &root) != nil {
		return nil, nil, nil
	}
	rawData, ok := root["data"].([]any)
	if !ok {
		return root, nil, nil
	}
	entriesByID := make(map[string]map[string]any, len(rawData))
	catalogIDs := make([]string, 0, len(rawData))
	for _, item := range rawData {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		id := strings.TrimSpace(stringValue(entry["id"]))
		if id == "" {
			continue
		}
		if _, exists := entriesByID[id]; exists {
			continue
		}
		entriesByID[id] = entry
		catalogIDs = append(catalogIDs, id)
	}
	return root, entriesByID, catalogIDs
}

func visibleModelIDsForRouting(routing map[string][]int64, catalogIDs []string) []string {
	if len(routing) == 0 {
		return nil
	}
	exact := make([]string, 0, len(routing))
	globs := make([]string, 0)
	for raw, accountIDs := range routing {
		model := strings.TrimSpace(raw)
		if model == "" || len(accountIDs) == 0 {
			continue
		}
		if isGlobModelPattern(model) {
			globs = append(globs, model)
		} else {
			exact = append(exact, model)
		}
	}
	sort.Strings(exact)
	sort.Strings(globs)

	catalogIDs = append([]string(nil), catalogIDs...)
	sort.Strings(catalogIDs)

	seen := make(map[string]bool, len(exact)+len(catalogIDs))
	out := make([]string, 0, len(exact)+len(catalogIDs))
	add := func(id string) {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			return
		}
		seen[id] = true
		out = append(out, id)
	}
	for _, id := range exact {
		add(id)
	}
	for _, pattern := range globs {
		for _, id := range catalogIDs {
			if matched, err := filepath.Match(pattern, id); err == nil && matched {
				add(id)
			}
		}
	}
	return out
}

func isGlobModelPattern(model string) bool {
	return strings.ContainsAny(model, "*?[")
}

func cloneModelEntry(entry map[string]any, id string) map[string]any {
	clone := make(map[string]any, len(entry)+1)
	for k, v := range entry {
		clone[k] = v
	}
	clone["id"] = id
	if object, _ := clone["object"].(string); strings.TrimSpace(object) == "" {
		clone["object"] = "model"
	}
	return clone
}

func syntheticModelEntry(id string) map[string]any {
	return map[string]any{
		"id":           id,
		"object":       "model",
		"created":      time.Now().Unix(),
		"owned_by":     "hopbase",
		"capabilities": []string{"chat"},
	}
}

func stringValue(value any) string {
	switch v := value.(type) {
	case string:
		return v
	default:
		return ""
	}
}
