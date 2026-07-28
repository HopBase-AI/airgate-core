package plugin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	sdk "github.com/DouDOU-start/airgate-sdk/sdkgo"
)

const testSSEHeartbeat = ": hopbase-keepalive\n\n"

func heartbeatContext(t *testing.T, path string) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, path, nil)
	installTTFTWriter(c)
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.WriteHeader(http.StatusOK)
	if _, err := c.Writer.Write([]byte(testSSEHeartbeat)); err != nil {
		t.Fatalf("write heartbeat: %v", err)
	}
	return c, recorder
}

func TestSSECommentOnlyClassification(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		body string
		want bool
	}{
		{name: "comment", body: testSSEHeartbeat, want: true},
		{name: "multiple comments", body: ": one\n: two\n\n", want: true},
		{name: "data", body: "data: {}\n\n", want: false},
		{name: "event and comment", body: ": one\nevent: ping\n\n", want: false},
		{name: "blank", body: "\n\n", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isSSECommentOnly([]byte(tt.body)); got != tt.want {
				t.Fatalf("isSSECommentOnly(%q) = %v, want %v", tt.body, got, tt.want)
			}
		})
	}
}

func TestCanFailoverAfterHeartbeatUntilApplicationData(t *testing.T) {
	t.Parallel()
	c, _ := heartbeatContext(t, "/v1/responses")
	f := &Forwarder{}
	state := &forwardState{stream: true}
	execution := forwardExecution{outcome: sdk.ForwardOutcome{Kind: sdk.OutcomeUpstreamTransient}}

	if !f.canFailover(c, state, execution) {
		t.Fatal("heartbeat-only stream should remain failover-eligible")
	}
	if _, err := c.Writer.Write([]byte("data: {\"type\":\"response.created\"}\n\n")); err != nil {
		t.Fatalf("write application event: %v", err)
	}
	if f.canFailover(c, state, execution) {
		t.Fatal("application SSE data should commit the selected account")
	}
}

func TestWriteAllRoutesFailedAfterHeartbeatUsesOpenAIStreamError(t *testing.T) {
	t.Parallel()
	c, recorder := heartbeatContext(t, "/v1/responses")
	c.Set(ginCtxKeyModel, "gpt-5.6-sol")
	var summary allRoutesFailureSummary
	summary.recordExecution(forwardExecution{outcome: sdk.ForwardOutcome{Kind: sdk.OutcomeUpstreamTransient}})

	writeAllRoutesFailed(c, summary)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want already-committed 200", recorder.Code)
	}
	body := recorder.Body.String()
	if !strings.HasPrefix(body, testSSEHeartbeat+"event: response.failed\ndata: ") {
		t.Fatalf("body = %q, want heartbeat followed by Responses API failure", body)
	}
	data := strings.TrimSpace(strings.SplitN(body, "data: ", 2)[1])
	var event map[string]any
	if err := json.Unmarshal([]byte(data), &event); err != nil {
		t.Fatalf("decode response.failed event: %v", err)
	}
	if event["type"] != "response.failed" {
		t.Fatalf("event type = %v, want response.failed", event["type"])
	}
	response, _ := event["response"].(map[string]any)
	if response["status"] != "failed" || response["model"] != "gpt-5.6-sol" {
		t.Fatalf("response = %#v, want failed response for requested model", response)
	}
	errorBody, _ := response["error"].(map[string]any)
	if errorBody["code"] != "server_error" {
		t.Fatalf("error = %#v, want Responses API server_error code", errorBody)
	}
}

func TestWriteAllRoutesFailedAfterHeartbeatUsesChatCompletionsError(t *testing.T) {
	t.Parallel()
	c, recorder := heartbeatContext(t, "/v1/chat/completions")
	var summary allRoutesFailureSummary
	summary.recordExecution(forwardExecution{outcome: sdk.ForwardOutcome{Kind: sdk.OutcomeUpstreamTransient}})

	writeAllRoutesFailed(c, summary)

	body := recorder.Body.String()
	if !strings.HasPrefix(body, testSSEHeartbeat) || !strings.Contains(body, `data: {"error":`) {
		t.Fatalf("body = %q, want heartbeat followed by OpenAI-compatible SSE error", body)
	}
}

func TestWriteAllRoutesFailedAfterHeartbeatUsesAnthropicStreamError(t *testing.T) {
	t.Parallel()
	c, recorder := heartbeatContext(t, "/v1/messages")
	setRequestErrorFormat(c, errorFormatAnthropic)
	var summary allRoutesFailureSummary
	summary.recordExecution(forwardExecution{outcome: sdk.ForwardOutcome{Kind: sdk.OutcomeUpstreamTransient}})

	writeAllRoutesFailed(c, summary)

	body := recorder.Body.String()
	if !strings.Contains(body, "event: error\n") || !strings.Contains(body, `"type":"error"`) {
		t.Fatalf("body = %q, want Anthropic SSE error", body)
	}
}

func TestFailoverStreamWriterForwardsCommentWithoutCommittingAccount(t *testing.T) {
	t.Parallel()
	target := httptest.NewRecorder()
	w := &failoverStreamWriter{target: target}
	w.Header().Set("Content-Type", "text/event-stream")

	if _, err := w.Write([]byte(testSSEHeartbeat)); err != nil {
		t.Fatalf("write heartbeat: %v", err)
	}
	if w.committed {
		t.Fatal("heartbeat must not commit the selected account")
	}
	if body := target.Body.String(); body != testSSEHeartbeat {
		t.Fatalf("target body = %q, want heartbeat", body)
	}

	w.WriteHeader(http.StatusOK)
	if !w.committed {
		t.Fatal("successful application response header should commit account")
	}
	if _, err := w.Write([]byte("data: output\n\n")); err != nil {
		t.Fatalf("write application output: %v", err)
	}
	if body := target.Body.String(); !strings.HasSuffix(body, "data: output\n\n") {
		t.Fatalf("target body = %q, want application output", body)
	}
}
