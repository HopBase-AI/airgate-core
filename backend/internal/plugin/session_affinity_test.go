package plugin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"

	"github.com/DouDOU-start/airgate-core/internal/auth"
	"github.com/DouDOU-start/airgate-core/internal/i18n"
	"github.com/DouDOU-start/airgate-core/internal/scheduler"
)

func newAffinityScheduler(t *testing.T) *scheduler.Scheduler {
	t.Helper()
	m := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: m.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	return scheduler.NewScheduler(nil, rdb)
}

// previous_response_id 是 Responses API 的标准字段，core 必须与 model / stream 一样
// 在解析阶段就拿到——它同时是一条调度约束，晚于选号拿到就没意义了。
func TestParseBodyExtractsPreviousResponseID(t *testing.T) {
	t.Parallel()

	parsed := parseBody([]byte(`{"model":"deepseek-v4.1-flash","input":"hi","previous_response_id":" resp_abc "}`), "application/json")
	if parsed.PreviousResponseID != "resp_abc" {
		t.Fatalf("PreviousResponseID = %q, want %q", parsed.PreviousResponseID, "resp_abc")
	}

	// chat/completions 这类不带该字段的请求不受影响。
	plain := parseBody([]byte(`{"model":"gpt-4.1","messages":[]}`), "application/json")
	if plain.PreviousResponseID != "" {
		t.Fatalf("无该字段时应为空，got %q", plain.PreviousResponseID)
	}
}

func TestResolveResponseAffinityPinsBoundAccount(t *testing.T) {
	sched := newAffinityScheduler(t)
	f := &Forwarder{scheduler: sched}
	ctx := context.Background()

	sched.BindResponseAffinity(ctx, 81, "openai", "resp_abc", 124, nil)

	state := &forwardState{
		requestedPlatform:  "openai",
		previousResponseID: "resp_abc",
		keyInfo:            &auth.APIKeyInfo{UserID: 81},
	}
	f.resolveResponseAffinity(ctx, state)
	if state.pinnedAccountID != 124 || state.accountReq.PinnedAccountID != 124 {
		t.Fatalf("命中绑定后应钉住账号 124，got pinned=%d req=%d",
			state.pinnedAccountID, state.accountReq.PinnedAccountID)
	}
}

// 没有绑定记录时保持原调度：可能是绑定过期，也可能上一轮走的是不透传的账号
// （插件会把该字段剥掉，行为与改动前一致）。这里绝不能把请求判死。
func TestResolveResponseAffinityWithoutBindingDoesNotPin(t *testing.T) {
	sched := newAffinityScheduler(t)
	f := &Forwarder{scheduler: sched}
	ctx := context.Background()

	state := &forwardState{
		requestedPlatform:  "openai",
		previousResponseID: "resp_unknown",
		keyInfo:            &auth.APIKeyInfo{UserID: 81},
	}
	f.resolveResponseAffinity(ctx, state)
	if state.pinnedAccountID != 0 || state.accountReq.PinnedAccountID != 0 {
		t.Fatalf("无绑定时不应钉账号，got %d", state.pinnedAccountID)
	}

	// 别的用户拿到同一个 id 也钉不过来。
	sched.BindResponseAffinity(ctx, 81, "openai", "resp_abc", 124, nil)
	other := &forwardState{
		requestedPlatform:  "openai",
		previousResponseID: "resp_abc",
		keyInfo:            &auth.APIKeyInfo{UserID: 82},
	}
	f.resolveResponseAffinity(ctx, other)
	if other.pinnedAccountID != 0 {
		t.Fatalf("跨用户不应命中会话亲和，got %d", other.pinnedAccountID)
	}
}

func TestResolveResponseAffinityNoopWithoutPreviousResponseID(t *testing.T) {
	sched := newAffinityScheduler(t)
	f := &Forwarder{scheduler: sched}
	state := &forwardState{
		requestedPlatform: "openai",
		keyInfo:           &auth.APIKeyInfo{UserID: 81},
	}
	f.resolveResponseAffinity(context.Background(), state)
	if state.pinnedAccountID != 0 {
		t.Fatalf("普通请求不应被钉住")
	}
}

// 钉不住必须是「会话要重开」的明确报错，且五语都有文案、都不泄露账号/上游信息。
func TestWriteSessionAffinityUnavailableSpeaksClientLanguage(t *testing.T) {
	gin.SetMode(gin.TestMode)

	for _, lang := range []string{"en", "zh", "zh-HK", "ja", "es"} {
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
		c.Request.Header.Set("Accept-Language", lang)

		writeSessionAffinityUnavailable(c)

		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s: status = %d, want 503", lang, rec.Code)
		}
		var payload struct {
			Error struct {
				Message string `json:"message"`
				Code    string `json:"code"`
			} `json:"error"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
			t.Fatalf("%s: 响应不是合法 JSON: %v", lang, err)
		}
		if payload.Error.Message == "" || payload.Error.Message == "gw.session_account_unavailable" {
			t.Fatalf("%s: 缺少该语言的文案，got %q", lang, payload.Error.Message)
		}
		for _, leaked := range []string{"account", "upstream", "账号", "上游", "volces", "ark"} {
			if containsFold(payload.Error.Message, leaked) {
				t.Fatalf("%s: 对客文案不得暴露供给信息（命中 %q）: %s", lang, leaked, payload.Error.Message)
			}
		}
	}

	// 落库文案统一英文，与其它失败口径一致。
	detail := i18n.En("gw.session_account_unavailable_detail")
	if detail == "" || detail == "gw.session_account_unavailable_detail" {
		t.Fatalf("落库英文文案缺失: %q", detail)
	}
}

func containsFold(s, sub string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(sub))
}

var _ = scheduler.AccountRequirements{}
