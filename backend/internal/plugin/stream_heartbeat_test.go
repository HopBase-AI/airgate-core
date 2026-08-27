package plugin

import (
	"encoding/json"
	"io"
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
	f := &Forwarder{}
	retryable := []sdk.OutcomeKind{
		sdk.OutcomeAccountRateLimited,
		sdk.OutcomeAccountDead,
		sdk.OutcomeUpstreamTransient,
		sdk.OutcomeClientError,
	}
	for _, kind := range retryable {
		kind := kind
		t.Run(kind.String(), func(t *testing.T) {
			t.Parallel()
			c, _ := heartbeatContext(t, "/v1/responses")
			state := &forwardState{stream: true}
			execution := forwardExecution{outcome: sdk.ForwardOutcome{Kind: kind}}
			if !f.canFailover(c, state, execution) {
				t.Fatalf("heartbeat-only stream should allow %s failover", kind)
			}
		})
	}

	c, _ := heartbeatContext(t, "/v1/responses")
	state := &forwardState{stream: true}
	execution := forwardExecution{outcome: sdk.ForwardOutcome{Kind: sdk.OutcomeUpstreamTransient}}
	if _, err := c.Writer.Write([]byte("data: {\"type\":\"response.created\"}\n\n")); err != nil {
		t.Fatalf("write application event: %v", err)
	}
	if f.canFailover(c, state, execution) {
		t.Fatal("application SSE data should commit the selected account")
	}
}

func TestCanFailoverOutcomeMatrixBeforeResponseCommit(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		kind   sdk.OutcomeKind
		status int
		err    error
		want   bool
	}{
		{name: "429 account rate limit", kind: sdk.OutcomeAccountRateLimited, want: true},
		{name: "403 or 401 account dead", kind: sdk.OutcomeAccountDead, want: true},
		{name: "502 or 503 upstream transient", kind: sdk.OutcomeUpstreamTransient, want: true},
		{name: "client 4xx", kind: sdk.OutcomeClientError, status: http.StatusBadRequest, want: true},
		{name: "client 404 model not found", kind: sdk.OutcomeClientError, status: http.StatusNotFound, want: true},
		{name: "client 504 gateway timeout not replayable", kind: sdk.OutcomeClientError, status: http.StatusGatewayTimeout, want: false},
		{name: "committed stream abort", kind: sdk.OutcomeStreamAborted, want: false},
		{name: "success", kind: sdk.OutcomeSuccess, want: false},
		{name: "unknown without plugin error", kind: sdk.OutcomeUnknown, want: false},
		{name: "plugin transport failure", kind: sdk.OutcomeUnknown, err: io.ErrUnexpectedEOF, want: true},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			installTTFTWriter(c)
			got := (&Forwarder{}).canFailover(c, &forwardState{stream: true}, forwardExecution{
				outcome: sdk.ForwardOutcome{Kind: tt.kind, Upstream: sdk.UpstreamResponse{StatusCode: tt.status}},
				err:     tt.err,
			})
			if got != tt.want {
				t.Fatalf("canFailover(kind=%s, err=%v) = %v, want %v", tt.kind, tt.err, got, tt.want)
			}
		})
	}
}

func TestCanFailoverRejectsCommittedStreamEvenForRetryableFailure(t *testing.T) {
	t.Parallel()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	installTTFTWriter(c)
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	if _, err := c.Writer.Write([]byte("data: {\"type\":\"response.created\"}\n\n")); err != nil {
		t.Fatalf("write application data: %v", err)
	}

	for _, execution := range []forwardExecution{
		{outcome: sdk.ForwardOutcome{Kind: sdk.OutcomeAccountRateLimited}},
		{outcome: sdk.ForwardOutcome{Kind: sdk.OutcomeAccountDead}},
		{outcome: sdk.ForwardOutcome{Kind: sdk.OutcomeUpstreamTransient}},
		{outcome: sdk.ForwardOutcome{Kind: sdk.OutcomeClientError, Upstream: sdk.UpstreamResponse{StatusCode: http.StatusBadRequest}}},
		{err: io.ErrUnexpectedEOF},
	} {
		if (&Forwarder{}).canFailover(c, &forwardState{stream: true}, execution) {
			t.Fatalf("committed stream must not replay execution %+v", execution)
		}
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
