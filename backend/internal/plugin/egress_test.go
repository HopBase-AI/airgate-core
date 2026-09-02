package plugin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	"github.com/DouDOU-start/airgate-core/ent"
	sdk "github.com/DouDOU-start/airgate-sdk/sdkgo"
)

// egress_test.go —— 出网身份闸门：
// 「正常响应」必须原样放行（协议头、限流头、成功体），
// 「错误响应」必须剥掉供应商标识但保留客户端要用的报错语义。
//
// 泄漏样本取自生产实测：Caddy 访问日志里我们实际发给客户的响应头，
// 以及 usage_logs 里客户实际看到过的 4xx 文案。

// leakyUpstreamHeaders 生产实测中真实出现过的上游响应头。
func leakyUpstreamHeaders() http.Header {
	return http.Header{
		// —— 必须剥掉：上游基础设施与供应商身份 ——
		"Cf-Ray":                        []string{"9c1f2a3b4c5d6e7f-HKG"},
		"Cf-Cache-Status":               []string{"DYNAMIC"},
		"Report-To":                     []string{`{"group":"cf-nel"}`},
		"Nel":                           []string{`{"report_to":"cf-nel"}`},
		"X-Envoy-Upstream-Service-Time": []string{"1873"},
		"Grpc-Encoding":                 []string{"identity"},
		"Grpc-Accept-Encoding":          []string{"gzip"},
		"Req-Arrive-Time":               []string{"1788209314123"},
		"Req-Cost-Time":                 []string{"2412"},
		"Resp-Start-Time":               []string{"1788209316535"},
		"Alb_request_id":                []string{"6b0a1f0e17882093141234567890"},
		"X-Gemini-Service-Tier":         []string{"paid"},
		"Trace-Id":                      []string{"64e70fa8bf4f4b1bac03ddef29e30070"},
		"X-Client-Request-Id":           []string{"cli-9931"},
		"Server-Timing":                 []string{"upstream;dur=1873"},
		"X-New-Api-Version":             []string{"0.8.4.1"},
		"Via":                           []string{"1.1 google"},
		"X-Powered-By":                  []string{"Express"},
		"Server":                        []string{"nginx/1.27.5"},
		"X-Frame-Options":               []string{"DENY"},
		"Strict-Transport-Security":     []string{"max-age=31536000"},

		// —— 必须放行：协议 + 客户端功能 ——
		"Content-Type":                           []string{"application/json; charset=utf-8"},
		"Cache-Control":                          []string{"no-cache"},
		"Retry-After":                            []string{"12"},
		"X-Should-Retry":                         []string{"true"},
		"X-Ratelimit-Remaining-Requests":         []string{"4999"},
		"X-Ratelimit-Reset-Tokens":               []string{"6m0s"},
		"Anthropic-Ratelimit-Requests-Remaining": []string{"49"},
		"X-Codex-Primary-Used-Percent":           []string{"37.2"},
		"Openai-Model":                           []string{"gpt-5.6"},
		"X-Accel-Buffering":                      []string{"no"},
	}
}

var (
	mustStripHeaders = []string{
		"Cf-Ray", "Cf-Cache-Status", "Report-To", "Nel",
		"X-Envoy-Upstream-Service-Time", "Grpc-Encoding", "Grpc-Accept-Encoding",
		"Req-Arrive-Time", "Req-Cost-Time", "Resp-Start-Time", "Alb_request_id",
		"X-Gemini-Service-Tier", "Trace-Id", "X-Client-Request-Id", "Server-Timing",
		"X-New-Api-Version", "Via", "X-Powered-By", "Server",
		"X-Frame-Options", "Strict-Transport-Security",
	}
	mustKeepHeaders = map[string]string{
		"Content-Type":                           "application/json; charset=utf-8",
		"Cache-Control":                          "no-cache",
		"Retry-After":                            "12",
		"X-Should-Retry":                         "true",
		"X-Ratelimit-Remaining-Requests":         "4999",
		"X-Ratelimit-Reset-Tokens":               "6m0s",
		"Anthropic-Ratelimit-Requests-Remaining": "49",
		"X-Codex-Primary-Used-Percent":           "37.2",
		"Openai-Model":                           "gpt-5.6",
		"X-Accel-Buffering":                      "no",
	}
)

// newGatedContext 构造一个「core 中间件已设好我方头 + 闸门已安装」的上下文。
func newGatedContext(t *testing.T, extraAllow []string) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	// 模拟 RequestLogger / CORS 中间件
	c.Writer.Header().Set("X-Request-Id", "ours-7f3c")
	c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
	c.Writer.Header().Set("Access-Control-Expose-Headers", "*")
	installEgressWriter(c, extraAllow)
	installTTFTWriter(c)
	return c, recorder
}

func assertHeaderPolicy(t *testing.T, recorder *httptest.ResponseRecorder, wantRequestID string) {
	t.Helper()
	for _, name := range mustStripHeaders {
		if got := recorder.Header().Get(name); got != "" {
			t.Errorf("上游身份头未剥离: %s = %q", name, got)
		}
	}
	for name, want := range mustKeepHeaders {
		if got := recorder.Header().Get(name); got != want {
			t.Errorf("客户端必需头被误剥: %s = %q, want %q", name, got, want)
		}
	}
	if got := recorder.Header().Get("X-Request-Id"); got != wantRequestID {
		t.Errorf("X-Request-Id = %q, want 我方生成的 %s", got, wantRequestID)
	}
	if got := recorder.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("CORS 头被上游覆盖: %q", got)
	}
}

// TestEgressWriter_NonStreamSuccess 正常非流式响应：上游身份头剥净、协议与限流头放行、响应体原样。
func TestEgressWriter_NonStreamSuccess(t *testing.T) {
	c, recorder := newGatedContext(t, nil)

	body := []byte(`{"id":"chatcmpl-abc","model":"gpt-5.6","choices":[{"message":{"content":"hi"}}]}`)
	writeUpstream(c, sdk.UpstreamResponse{
		StatusCode: http.StatusOK,
		Headers:    leakyUpstreamHeaders(),
		Body:       body,
	})

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	assertHeaderPolicy(t, recorder, "ours-7f3c")
	if got := recorder.Body.String(); got != string(body) {
		t.Fatalf("成功响应体被改动:\n got=%s\nwant=%s", got, body)
	}
}

// TestEgressWriter_StreamSuccess 流式：插件经 ForwardRequest.Writer 直接写出，
// 不经过 core 的 copyUpstreamHeaders——闸门必须同样生效。
func TestEgressWriter_StreamSuccess(t *testing.T) {
	c, recorder := newGatedContext(t, nil)

	// 插件侧行为：把上游头搬到客户端响应上，然后直接写 SSE
	for key, values := range leakyUpstreamHeaders() {
		for _, v := range values {
			c.Writer.Header().Set(key, v)
		}
	}
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	if _, err := c.Writer.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n")); err != nil {
		t.Fatalf("写 SSE 失败: %v", err)
	}
	c.Writer.Flush()
	if _, err := c.Writer.Write([]byte("data: [DONE]\n\n")); err != nil {
		t.Fatalf("写 SSE 失败: %v", err)
	}

	for _, name := range mustStripHeaders {
		if got := recorder.Header().Get(name); got != "" {
			t.Errorf("流式响应上游身份头未剥离: %s = %q", name, got)
		}
	}
	if got := recorder.Header().Get("Content-Type"); got != "text/event-stream" {
		t.Errorf("Content-Type = %q, want text/event-stream", got)
	}
	if got := recorder.Header().Get("X-Accel-Buffering"); got != "no" {
		t.Errorf("X-Accel-Buffering 被误剥，SSE 会被反代缓冲")
	}
	if got := recorder.Header().Get("X-Request-Id"); got != "ours-7f3c" {
		t.Errorf("X-Request-Id = %q, want ours-7f3c", got)
	}
	if !strings.Contains(recorder.Body.String(), "[DONE]") {
		t.Errorf("SSE 体被改动: %q", recorder.Body.String())
	}
}

// TestEgressWriter_PluginDeclaredAllowList 插件经 Metadata 声明的额外放行头生效，
// 精确名与前缀通配都支持；未声明的仍然剥掉。
func TestEgressWriter_PluginDeclaredAllowList(t *testing.T) {
	c, recorder := newGatedContext(t, []string{"x-vendor-quota", "x-feature-*"})

	c.Writer.Header().Set("X-Vendor-Quota", "88")
	c.Writer.Header().Set("X-Feature-Beta", "on")
	c.Writer.Header().Set("X-Secret-Upstream", "leak")
	c.Writer.WriteHeader(http.StatusOK)

	if got := recorder.Header().Get("X-Vendor-Quota"); got != "88" {
		t.Errorf("插件声明的精确放行头被剥: %q", got)
	}
	if got := recorder.Header().Get("X-Feature-Beta"); got != "on" {
		t.Errorf("插件声明的前缀放行头被剥: %q", got)
	}
	if got := recorder.Header().Get("X-Secret-Upstream"); got != "" {
		t.Errorf("未声明的头未被剥: %q", got)
	}
}

// TestEgressWriter_UpstreamCannotOverrideOwnHeaders 上游同名头不得覆盖我方头。
// 这条同时守住两个既有缺陷：上游 X-Request-Id 盖掉我们的请求 ID、上游 CORS 盖掉我们的 CORS。
func TestEgressWriter_UpstreamCannotOverrideOwnHeaders(t *testing.T) {
	c, recorder := newGatedContext(t, nil)

	c.Writer.Header().Set("X-Request-Id", "USA-20434252906100")
	c.Writer.Header().Set("Access-Control-Allow-Origin", "https://relay.example.com")
	c.Writer.WriteHeader(http.StatusOK)

	if got := recorder.Header().Get("X-Request-Id"); got != "ours-7f3c" {
		t.Fatalf("X-Request-Id = %q, want ours-7f3c（上游 ID 不得盖掉我方 ID）", got)
	}
	if got := recorder.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("CORS = %q, want *（上游不得改我方 CORS）", got)
	}
}

// TestEgressWriter_ParseHeaderAllowList Metadata 声明的解析：大小写、空白、空项。
func TestEgressWriter_ParseHeaderAllowList(t *testing.T) {
	got := parseHeaderAllowList(" X-A , x-b-* ,, ")
	want := []string{"x-a", "x-b-*"}
	if len(got) != len(want) {
		t.Fatalf("parseHeaderAllowList = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("parseHeaderAllowList = %v, want %v", got, want)
		}
	}
	if parseHeaderAllowList("  ") != nil {
		t.Fatalf("空声明应返回 nil")
	}
}

// testScrubber 模拟本次请求命中的上游账号：中继 api.aijws.com，账号名「贾克斯-pro-0.15」。
func testScrubber() *identityScrubber {
	return newIdentityScrubber(&ent.Account{
		Name: "贾克斯-pro-0.15",
		Credentials: map[string]string{
			"base_url": "https://api.aijws.com",
			"email":    "pool@aijws.com",
		},
	}, "gpt-5.6")
}

// TestScrubText_ProductionSamples 生产实测的客户可见 4xx 文案：
// 供应商标识必须消失，客户排障需要的语义必须留下。
func TestScrubText_ProductionSamples(t *testing.T) {
	scrubber := testScrubber()

	tests := []struct {
		name        string
		input       string
		mustGone    []string
		mustKeep    []string
		wantEmpty   bool
		wantExactly string
	}{
		{
			name:     "中继产品名前缀",
			input:    "Error from provider (Console Go): Upstream request failed: [invalid_request_error] GLM-5.3 is a thinking-only model; disabling thinking is not supported",
			mustGone: []string{"Console Go", "console go", "provider"},
			mustKeep: []string{"GLM-5.3 is a thinking-only model", "disabling thinking is not supported"},
		},
		{
			name:     "供应商工单号尾注",
			input:    "Multi Agent requests are not allowed on chat completions (Request-ID: USA-20434252906100)",
			mustGone: []string{"Request-ID", "USA-20434252906100"},
			mustKeep: []string{"Multi Agent requests are not allowed on chat completions"},
		},
		{
			name:     "厂商专有错误码前缀",
			input:    "<400> InternalError.Algo.InvalidParameter: Normal mode does not support Code interpreter. Please set enable_thinking to true.",
			mustGone: []string{"InternalError.Algo", "<400>"},
			mustKeep: []string{"Normal mode does not support Code interpreter", "Please set enable_thinking to true"},
		},
		{
			name:     "账号名与上游域名",
			input:    "贾克斯-pro-0.15 rejected the request at https://api.aijws.com/v1/chat/completions",
			mustGone: []string{"贾克斯", "aijws", "api.aijws.com", "https://"},
			mustKeep: []string{"rejected the request"},
		},
		{
			name:     "中继自称",
			input:    "new-api channel error: model not found",
			mustGone: []string{"new-api"},
			mustKeep: []string{"channel error", "model not found"},
		},
		{
			name:        "纯参数报错原样放行",
			input:       "max_tokens参数非法：限制数值范围[1,131072]",
			wantExactly: "max_tokens参数非法：限制数值范围[1,131072]",
		},
		{
			name:        "英文参数报错原样放行",
			input:       "Invalid content type: input_video. Supported types for user role are: 'input_text', 'input_image', 'input_file'.",
			wantExactly: "Invalid content type: input_video. Supported types for user role are: 'input_text', 'input_image', 'input_file'.",
		},
		{
			name:        "图片尺寸报错原样放行",
			input:       "The image length and width do not meet the model restrictions. [height:1 or width:1 must be larger than 10]",
			wantExactly: "The image length and width do not meet the model restrictions. [height:1 or width:1 must be larger than 10]",
		},
		{
			// 我方会在上游文案前加 "HTTP 400: "，中继前缀因此不在行首
			name:     "中继前缀不在行首也要剥",
			input:    "HTTP 400: Error from provider (Console Go): Upstream request failed: [invalid_request_error] GLM-5.3 is a thinking-only model",
			mustGone: []string{"Console Go", "Error from provider", "Upstream request failed"},
			mustKeep: []string{"HTTP 400", "[invalid_request_error]", "GLM-5.3 is a thinking-only model"},
		},
		{
			name:     "删中继名不留残肢",
			input:    "HTTP 400: new_api_error: 模型 claude-sonnet-4-5 的价格尚未配置",
			mustGone: []string{"new_api", "_error:"},
			mustKeep: []string{"模型 claude-sonnet-4-5 的价格尚未配置"},
		},
		{
			// 生产实测：中继 429/502 时把 nginx/openresty 错误页塞进文案
			name:     "上游 HTML 错误页整段截断",
			input:    "Asset provider returned non-JSON: <html> <head><title>429 Too Many Requests</title></head> <body> <center>openresty/1.15.8.3</center> </body> </html>",
			mustGone: []string{"<html", "openresty", "<title>"},
			mustKeep: []string{"Asset provider returned non-JSON"},
		},
		{
			name:     "DOCTYPE 页也要截断",
			input:    `HTTP 404: <!DOCTYPE html> <html lang="en"><body><pre>Cannot POST /proxy/v1/messages</pre></body></html>`,
			mustGone: []string{"DOCTYPE", "<html", "/proxy/v1/messages"},
			mustKeep: []string{"HTTP 404"},
		},
		{
			// 生产实测：上游把自己的数据库连接错误当 4xx 文案回给我们
			name:      "上游内网/基础设施错误整条判废",
			input:     "failed to connect to `user=postgres database=new-api`: 10.0.1.10:5432 (): server error: FATAL: no pg_hba.conf entry for host \"10.0.25.41\"",
			wantEmpty: true,
		},
		{
			name:      "只剩供应商标识时判空",
			input:     "api.aijws.com",
			wantEmpty: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := scrubber.scrubText(tt.input)
			if tt.wantEmpty {
				if got != "" {
					t.Fatalf("scrubText = %q, want 空（应回落我方兜底文案）", got)
				}
				return
			}
			if tt.wantExactly != "" {
				if got != tt.wantExactly {
					t.Fatalf("正常报错被改动:\n got=%q\nwant=%q", got, tt.wantExactly)
				}
				return
			}
			for _, token := range tt.mustGone {
				if strings.Contains(strings.ToLower(got), strings.ToLower(token)) {
					t.Errorf("供应商标识残留 %q: %q", token, got)
				}
			}
			for _, keep := range tt.mustKeep {
				if !strings.Contains(got, keep) {
					t.Errorf("客户需要的报错语义丢失 %q: %q", keep, got)
				}
			}
		})
	}
}

// TestScrubErrorBody_KeepsStructure 错误体结构与客户端要用的字段必须保留。
func TestScrubErrorBody_KeepsStructure(t *testing.T) {
	scrubber := testScrubber()
	body := []byte(`{"error":{"message":"Error from provider (Console Go): 'max_tokens' is not supported on /v1/responses (Request-ID: USA-2043)","type":"invalid_request_error","code":"unsupported_parameter","param":"max_tokens"},"request_id":"USA-2043","status":400}`)

	cleaned, ok := scrubber.scrubErrorBody(body)
	if !ok {
		t.Fatalf("scrubErrorBody 应可清洗")
	}

	var payload struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
			Code    string `json:"code"`
			Param   string `json:"param"`
		} `json:"error"`
		RequestID *string `json:"request_id"`
		Status    int     `json:"status"`
	}
	if err := json.Unmarshal(cleaned, &payload); err != nil {
		t.Fatalf("清洗后不是合法 JSON: %v (%s)", err, cleaned)
	}
	if payload.Error.Type != "invalid_request_error" || payload.Error.Code != "unsupported_parameter" || payload.Error.Param != "max_tokens" {
		t.Fatalf("错误体结构字段被改动: %+v", payload.Error)
	}
	if payload.Status != 400 {
		t.Fatalf("数值字段被改动: %d", payload.Status)
	}
	if payload.RequestID != nil {
		t.Fatalf("供应商工单号键未删除: %v", *payload.RequestID)
	}
	if strings.Contains(payload.Error.Message, "Console Go") || strings.Contains(payload.Error.Message, "USA-2043") {
		t.Fatalf("供应商标识残留: %q", payload.Error.Message)
	}
	if !strings.Contains(payload.Error.Message, "'max_tokens' is not supported") {
		t.Fatalf("报错语义丢失: %q", payload.Error.Message)
	}
}

// TestScrubErrorBody_NonJSONFallsBack 非 JSON / 无信息量的体应回落我方兜底，而不是硬塞给客户。
func TestScrubErrorBody_NonJSONFallsBack(t *testing.T) {
	scrubber := testScrubber()
	if _, ok := scrubber.scrubErrorBody([]byte("<html>502 Bad Gateway from api.aijws.com</html>")); ok {
		t.Fatalf("非 JSON 体不应被判为可清洗")
	}
	if _, ok := scrubber.scrubErrorBody([]byte(`{"error":{"message":"api.aijws.com"}}`)); ok {
		t.Fatalf("清洗后无信息量的体不应放行")
	}
	if _, ok := scrubber.scrubErrorBody(nil); ok {
		t.Fatalf("空体不应被判为可清洗")
	}
}

// TestWriteClientErrorResponse_EndToEnd 报错全链路：4xx 体过清洗 + 响应头过闸门。
func TestWriteClientErrorResponse_EndToEnd(t *testing.T) {
	c, recorder := newGatedContext(t, nil)

	headers := leakyUpstreamHeaders()
	writeClientErrorResponse(c, sdk.ForwardOutcome{
		Kind: sdk.OutcomeClientError,
		Upstream: sdk.UpstreamResponse{
			StatusCode: http.StatusBadRequest,
			Headers:    headers,
			Body:       []byte(`{"error":{"message":"Error from provider (Console Go): model gpt-9 does not exist (Request-ID: USA-2043)","type":"invalid_request_error","code":"model_not_found"}}`),
		},
		Reason: "HTTP 400",
	}, testScrubber())

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400（错误状态码必须如实透传）", recorder.Code)
	}
	assertHeaderPolicy(t, recorder, "ours-7f3c")

	body := recorder.Body.String()
	for _, leak := range []string{"Console Go", "USA-2043", "aijws", "贾克斯"} {
		if strings.Contains(body, leak) {
			t.Errorf("错误体泄漏供应商标识 %q: %s", leak, body)
		}
	}
	for _, keep := range []string{"model gpt-9 does not exist", "invalid_request_error", "model_not_found"} {
		if !strings.Contains(body, keep) {
			t.Errorf("错误体丢失客户端需要的信息 %q: %s", keep, body)
		}
	}
}

// TestWriteClientErrorResponse_FallsBackToOwnMessage 上游体不可清洗时回落我方文案，
// 且状态码与协议结构仍然正确。
func TestWriteClientErrorResponse_FallsBackToOwnMessage(t *testing.T) {
	c, recorder := newGatedContext(t, nil)

	writeClientErrorResponse(c, sdk.ForwardOutcome{
		Kind: sdk.OutcomeClientError,
		Upstream: sdk.UpstreamResponse{
			StatusCode: http.StatusBadRequest,
			Headers:    leakyUpstreamHeaders(),
			Body:       []byte("upstream 400 from api.aijws.com"),
		},
		Reason: "api.aijws.com rejected",
	}, testScrubber())

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
	body := recorder.Body.String()
	if strings.Contains(body, "aijws") {
		t.Fatalf("回落路径仍泄漏供应商域名: %s", body)
	}
	if !strings.Contains(body, defaultClientErrorMessage) && !strings.Contains(body, "rejected") {
		t.Fatalf("回落文案缺失: %s", body)
	}
}

// TestHostForwardPayload_ScrubsOnlyErrors 插件侧（工作坊 / AI Chat）拿到的载荷：
// 4xx 体清洗，成功体原样——插件要从成功体里解析任务 ID 与素材 URL。
func TestHostForwardPayload_ScrubsOnlyErrors(t *testing.T) {
	scrubber := testScrubber()

	successBody := `{"task_id":"vid-99","url":"https://cdn.hop-base.com/v/9.mp4","model":"hailuo-3"}`
	success := hostForwardPayload(sdk.ForwardOutcome{
		Kind:     sdk.OutcomeSuccess,
		Upstream: sdk.UpstreamResponse{StatusCode: http.StatusOK, Body: []byte(successBody)},
	}, scrubber)
	if got := success["body"].(string); got != successBody {
		t.Fatalf("成功体被清洗改动:\n got=%s\nwant=%s", got, successBody)
	}

	errBody := `{"error":{"message":"Error from provider (Console Go): invalid image size","type":"invalid_request_error"}}`
	failure := hostForwardPayload(sdk.ForwardOutcome{
		Kind:     sdk.OutcomeClientError,
		Upstream: sdk.UpstreamResponse{StatusCode: http.StatusBadRequest, Body: []byte(errBody)},
	}, scrubber)
	got := failure["body"].(string)
	if strings.Contains(got, "Console Go") {
		t.Fatalf("插件侧 4xx 体泄漏供应商标识: %s", got)
	}
	if !strings.Contains(got, "invalid image size") {
		t.Fatalf("插件侧 4xx 体丢失报错语义: %s", got)
	}
}

// TestScrubText_KeepsModelNameWhenAccountNamedAfterModel 账号常按模型命名
// （生产上有 seedance-inference-1 / 腾讯tokenhub-GLM5.3-7折 这类）。
// 账号名恰好等于模型名时不得抹掉——那是客户自己传的模型，谈不上泄漏，
// 抹了反而把「哪个模型不支持什么」这条最有用的信息删掉。
func TestScrubText_KeepsModelNameWhenAccountNamedAfterModel(t *testing.T) {
	scrubber := newIdentityScrubber(&ent.Account{
		Name:        "kimi-k3",
		Credentials: map[string]string{"base_url": "https://api.relay-vendor.com"},
	}, "kimi-k3")

	got := scrubber.scrubText("n must be 1 for kimi-k3")
	if got != "n must be 1 for kimi-k3" {
		t.Fatalf("模型名被误删: %q", got)
	}

	// 同一个 scrubber 仍要剥上游域名
	if out := scrubber.scrubText("rejected by api.relay-vendor.com"); strings.Contains(out, "relay-vendor") {
		t.Fatalf("上游域名未剥: %q", out)
	}
}

// TestEgressWriter_ConcurrentKeepAliveAndBody SSE 保活心跳来自独立 goroutine
// （openai 插件生图同步保活即如此），闸门必须在并发写下只裁剪一次且不炸。
// 本用例的价值在 -race 下体现。
func TestEgressWriter_ConcurrentKeepAliveAndBody(t *testing.T) {
	c, recorder := newGatedContext(t, nil)
	for key, values := range leakyUpstreamHeaders() {
		for _, v := range values {
			c.Writer.Header().Set(key, v)
		}
	}

	writer := &synchronizedTestWriter{ResponseWriter: c.Writer}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { // 保活心跳
		defer wg.Done()
		for i := 0; i < 20; i++ {
			_, _ = writer.Write([]byte(": ping\n\n"))
		}
	}()
	go func() { // 正文
		defer wg.Done()
		for i := 0; i < 20; i++ {
			_, _ = writer.Write([]byte("data: {\"delta\":\"x\"}\n\n"))
		}
	}()
	wg.Wait()

	for _, name := range mustStripHeaders {
		if got := recorder.Header().Get(name); got != "" {
			t.Errorf("并发写下身份头未剥离: %s = %q", name, got)
		}
	}
	if got := recorder.Header().Get("X-Request-Id"); got != "ours-7f3c" {
		t.Errorf("并发写下我方头丢失: %q", got)
	}
}

// synchronizedTestWriter 模拟插件侧的写串行化（openai 插件的 synchronizedResponseWriter）。
type synchronizedTestWriter struct {
	gin.ResponseWriter
	mu sync.Mutex
}

func (w *synchronizedTestWriter) Write(b []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.ResponseWriter.Write(b)
}

// TestEgressWriter_RateLimitResponseKeepsClientSignals 429 场景：
// 客户端自适应退避要用的信号必须全部保留，上游身份仍要剥净。
func TestEgressWriter_RateLimitResponseKeepsClientSignals(t *testing.T) {
	c, recorder := newGatedContext(t, nil)

	headers := leakyUpstreamHeaders()
	headers.Set("Retry-After", "30")
	headers.Set("X-Ratelimit-Remaining-Requests", "0")
	writeUpstream(c, sdk.UpstreamResponse{
		StatusCode: http.StatusTooManyRequests,
		Headers:    headers,
		Body:       []byte(`{"error":{"message":"rate limit reached","type":"rate_limit_error"}}`),
	})

	if recorder.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", recorder.Code)
	}
	if got := recorder.Header().Get("Retry-After"); got != "30" {
		t.Fatalf("Retry-After 被误剥: %q", got)
	}
	if got := recorder.Header().Get("X-Ratelimit-Remaining-Requests"); got != "0" {
		t.Fatalf("限流剩余量被误剥: %q", got)
	}
	for _, name := range mustStripHeaders {
		if got := recorder.Header().Get(name); got != "" {
			t.Errorf("429 响应仍带身份头 %s = %q", name, got)
		}
	}
}

// TestEgressWriter_MultiValueHeaders 多值头：放行的保留全部取值，剥离的一个不留。
func TestEgressWriter_MultiValueHeaders(t *testing.T) {
	c, recorder := newGatedContext(t, nil)

	c.Writer.Header().Add("Vary", "Accept-Encoding")
	c.Writer.Header().Add("Vary", "Origin")
	c.Writer.Header().Add("Set-Cookie", "session=abc")
	c.Writer.Header().Add("Set-Cookie", "trace=upstream-9931")
	c.Writer.WriteHeader(http.StatusOK)

	if got := recorder.Header().Values("Vary"); len(got) != 2 {
		t.Fatalf("放行的多值头丢值: %v", got)
	}
	if got := recorder.Header().Values("Set-Cookie"); len(got) != 0 {
		t.Fatalf("上游 Set-Cookie 未剥净: %v", got)
	}
}

// TestEgressWriter_IdempotentAcrossWrites 多次写只裁剪一次，且不会把后写入的我方头吃掉。
func TestEgressWriter_IdempotentAcrossWrites(t *testing.T) {
	c, recorder := newGatedContext(t, nil)
	c.Writer.Header().Set("Cf-Ray", "leak-1")
	c.Writer.Header().Set("Content-Type", "text/event-stream")

	for i := 0; i < 3; i++ {
		if _, err := c.Writer.Write([]byte(": ping\n\n")); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	if got := recorder.Header().Get("Cf-Ray"); got != "" {
		t.Fatalf("Cf-Ray 未剥离: %q", got)
	}
	if got := recorder.Header().Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("Content-Type = %q", got)
	}
	if got := recorder.Header().Get("X-Request-Id"); got != "ours-7f3c" {
		t.Fatalf("X-Request-Id = %q", got)
	}
}

// TestEgressWriter_KeepsOwnRetryAfterMs 我方在闸门之后生成的 Retry-After-Ms 必须出网。
//
// protocolRateLimitError 在 installEgressWriter 之后才 Set 这个头，它不在 owned 快照里，
// 只能靠白名单放行。漏掉会让 Anthropic SDK 这类优先读 retry-after-ms 的客户端
// 退化成整秒退避——这是闸门最容易误伤自己人的地方。
func TestEgressWriter_KeepsOwnRetryAfterMs(t *testing.T) {
	c, recorder := newGatedContext(t, nil)

	protocolRateLimitError(c, http.StatusTooManyRequests, "openai", "rate limited", 1500*time.Millisecond)

	if got := recorder.Header().Get("Retry-After-Ms"); got != "1500" {
		t.Fatalf("Retry-After-Ms = %q, want \"1500\"（被闸门剥掉即为回归）", got)
	}
	if got := recorder.Header().Get("Retry-After"); got != "2" {
		t.Fatalf("Retry-After = %q, want \"2\"", got)
	}
}

// TestReplaceFold_ByteLengthChangingRunes ToLower 改变字节长度的字符不得让下标错位。
//
// İ Ⱥ Ⱦ ẞ Ω K Å 这七个字符 ToLower 后字节长度会变。旧实现拿 ToLower(text) 的下标
// 去切原串，土耳其语大写文本会被删错位置（İÇERİK acme → İÇERİ），
// 字符再多一点直接 slice 越界 panic。
func TestReplaceFold_ByteLengthChangingRunes(t *testing.T) {
	cases := []struct {
		name string
		text string
		want string
	}{
		{"土耳其语大写", "İÇERİK acme reddedildi", "İÇERİK  reddedildi"},
		{"土耳其语长句", strings.Repeat("İ", 40) + " acme", strings.Repeat("İ", 40) + " "},
		{"开尔文符号", "Temperature 300K acme rejected", "Temperature 300K  rejected"},
		{"德语大写eszett", "GROẞE ANFRAGE acme abgelehnt", "GROẞE ANFRAGE  abgelehnt"},
		{"埃符号", "Wavelength 5000Å acme rejected", "Wavelength 5000Å  rejected"},
		{"大小写混合命中", "ACME and AcMe and acme", " and  and "},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := replaceFold(tc.text, "acme", "", boundaryWord)
			if got != tc.want {
				t.Fatalf("replaceFold = %q, want %q", got, tc.want)
			}
			if !utf8.ValidString(got) {
				t.Fatalf("输出不是合法 UTF-8: %q", got)
			}
		})
	}
}

// TestScrubText_TurkishTextSurvives 上游回显的土耳其语 prompt 不得被清洗改坏。
func TestScrubText_TurkishTextSurvives(t *testing.T) {
	scrubber := newIdentityScrubber(&ent.Account{Name: "acme-upstream"}, "gpt-5.6")
	in := "Your prompt was rejected: İSTANBUL İÇİN İYİ BİR İŞ İMKANI İSTİYORUM"
	got := scrubber.scrubText(in)
	if got != in {
		t.Fatalf("与供应商无关的土耳其语正文被改写:\n in = %q\nout = %q", in, got)
	}
}
