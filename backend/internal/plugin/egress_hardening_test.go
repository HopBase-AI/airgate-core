package plugin

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"strings"
	"testing"

	"github.com/DouDOU-start/airgate-core/ent"
	sdk "github.com/DouDOU-start/airgate-sdk/sdkgo"
)

// egress_hardening_test.go —— 2026-09-02 对 #87 review 遗留项的回归守卫。
// 每条对应一种「客户可见文案被静默改坏」或「清洗失效」的形状。

// TestScrubText_ShortOrGenericAccountNameLeavesTextAlone 短账号名 / 通用词账号名
// 不能当 token：max 不能把 max_tokens 删成 tokens，stream 不能把 stream_options 删成 options。
func TestScrubText_ShortOrGenericAccountNameLeavesTextAlone(t *testing.T) {
	cases := []struct{ account, text string }{
		{"max", "max_tokens must be less than or equal to 8192"},
		{"pro", "The prompt was blocked by the provider content filter"},
		{"api", "Unsupported api version, please use v1"},
		{"new", "renew your subscription; new requests are rejected"},
		{"tool", "tool_choice is required when tools are provided"},
		{"luna", "luna is not a valid value for reasoning_effort"},
		{"stream", "stream_options.include_usage must be a boolean"},
		{"tokens", "tokens exceeded: max 8192 tokens per request"},
		{"OpenAI", "Unsupported OpenAI parameter: logprobs"},
	}
	for _, tc := range cases {
		t.Run(tc.account, func(t *testing.T) {
			scrubber := newIdentityScrubber(&ent.Account{Name: tc.account}, "gpt-5.6")
			if got := scrubber.scrubText(tc.text); got != tc.text {
				t.Fatalf("账号 %q 把客户正文改坏:\n in = %q\nout = %q", tc.account, tc.text, got)
			}
		})
	}
}

// TestScrubText_AccountTokenRespectsWordBoundary 账号名只在词边界上命中：
// 是别的词的一部分（前后紧邻字母 / 数字 / 下划线）时不动；CJK 无词边界，紧邻中文照常命中。
func TestScrubText_AccountTokenRespectsWordBoundary(t *testing.T) {
	scrubber := newIdentityScrubber(&ent.Account{Name: "acmecloud"}, "gpt-5.6")
	cases := []struct{ in, want string }{
		{"acmecloud rejected the request", "rejected the request"},
		{"request rejected by ACMECLOUD (pool-3)", "request rejected by (pool-3)"},
		{"myacmecloud_flag must be set", "myacmecloud_flag must be set"},
		{"acmecloud_error: bad request", "acmecloud_error: bad request"},
		{"acmecloud2 is not a model", "acmecloud2 is not a model"},
	}
	for _, tc := range cases {
		if got := scrubber.scrubText(tc.in); got != tc.want {
			t.Fatalf("scrubText(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}

	cjk := newIdentityScrubber(&ent.Account{Name: "贾克斯-pro-0.15"}, "gpt-5.6")
	if got := cjk.scrubText("账号贾克斯-pro-0.15拒绝了请求"); strings.Contains(got, "贾克斯") {
		t.Fatalf("紧邻中文的账号名未剥: %q", got)
	}
}

// TestScrubText_ProductTokenIgnoresUnderscoreBoundary 产品名固定表用宽松边界：
// 中继的 new_api_error 这种自称必须剥掉，且不留 "_error" 残渣。
func TestScrubText_ProductTokenIgnoresUnderscoreBoundary(t *testing.T) {
	var scrubber *identityScrubber
	got := scrubber.scrubText("HTTP 400: new_api_error: 模型 claude-sonnet-4-5 的价格尚未配置")
	if strings.Contains(strings.ToLower(got), "new_api") || strings.Contains(got, "_error") {
		t.Fatalf("产品名未剥净: %q", got)
	}
	if !strings.Contains(got, "模型 claude-sonnet-4-5 的价格尚未配置") {
		t.Fatalf("报错语义丢失: %q", got)
	}
}

// TestScrubText_AccountNameContainingModelStillScrubbed 「供应商名 + 模型名」是常见命名
// （腾讯tokenhub-GLM5.3-7折）。早先双向 Contains 会让这类账号名整个退出清洗；
// 现在只有 token 是模型名的一段时才跳过。
func TestScrubText_AccountNameContainingModelStillScrubbed(t *testing.T) {
	scrubber := newIdentityScrubber(&ent.Account{Name: "腾讯tokenhub-GLM5.3-7折"}, "GLM5.3")
	got := scrubber.scrubText("腾讯tokenhub-GLM5.3-7折 rejected: bad param")
	if strings.Contains(got, "tokenhub") || strings.Contains(got, "腾讯") {
		t.Fatalf("含模型名的账号名未剥: %q", got)
	}
	if !strings.Contains(got, "rejected: bad param") {
		t.Fatalf("报错语义丢失: %q", got)
	}

	// 账号名是模型名的一段：客户传的模型名不能被削
	sub := newIdentityScrubber(&ent.Account{Name: "gpt-5.6"}, "gpt-5.6-terra")
	in := "model gpt-5.6-terra is not available in this region"
	if got := sub.scrubText(in); got != in {
		t.Fatalf("模型名被误删: %q", got)
	}
}

// TestScrubText_BareDomainKeepsParamNamesAndLinkContext 裸域名规则不能吃掉
// JSON 路径尾段（parameters.top / data.io），删 URL 要留占位别留断句。
func TestScrubText_BareDomainKeepsParamNamesAndLinkContext(t *testing.T) {
	scrubber := testScrubber()
	keep := []string{
		"parameters.top must be an integer",
		"the field data.io must be a string",
		"version 2.0 of the schema is no longer supported",
	}
	for _, in := range keep {
		if got := scrubber.scrubText(in); got != in {
			t.Fatalf("参数名被当域名吃掉:\n in = %q\nout = %q", in, got)
		}
	}

	if got := scrubber.scrubText("rejected by api.minimax.io"); strings.Contains(got, "minimax.io") {
		t.Fatalf("两级 .io 域名未剥: %q", got)
	}
	if got := scrubber.scrubText("quota exhausted, see relay-vendor.com for details"); strings.Contains(got, "relay-vendor.com") {
		t.Fatalf("一级 .com 域名未剥: %q", got)
	}

	// 生产实测：腾讯 402 引导语被删成结尾光秃秃的 See
	in := "Please go to Console > Online Inference Service to enable postpaid billing. See: https://console.cloud.tencent.com/hunyuan/start"
	got := scrubber.scrubText(in)
	if strings.Contains(got, "tencent") || strings.Contains(got, "https://") {
		t.Fatalf("URL 未剥: %q", got)
	}
	if !strings.HasSuffix(got, "See: "+urlPlaceholder) {
		t.Fatalf("删 URL 后留下断句: %q", got)
	}
}

// TestScrubText_InfraSignalsNeedCorroboration 软信号（mysql / connection refused）
// 单独出现不判废——那可能是上游回显的用户 prompt；强信号或软信号 + 内网地址才整条作废。
func TestScrubText_InfraSignalsNeedCorroboration(t *testing.T) {
	scrubber := testScrubber()
	keep := []string{
		"upstream connection refused by policy engine, retry with fewer tools",
		"prompt mentions mysql injection which is fine",
		"your code prints panic: index out of range, please fix before retry",
	}
	for _, in := range keep {
		if got := scrubber.scrubText(in); got != in {
			t.Fatalf("正常报错被整条判废:\n in = %q\nout = %q", in, got)
		}
	}
	discard := []string{
		"failed to connect to `user=postgres database=new-api`: 10.0.1.10:5432 … no pg_hba.conf entry for host \"10.0.25.41\"",
		"dial tcp 10.0.1.10:5432: connect: connection refused",
		"mysql: connection refused host=db.internal:3306",
		"goroutine 1 [running]:\nmain.main()\n\t/app/main.go:12",
	}
	for _, in := range discard {
		if got := scrubber.scrubText(in); got != "" {
			t.Fatalf("基础设施内部错误未判废:\n in = %q\nout = %q", in, got)
		}
	}
}

// TestScrubErrorBody_PreservesNumbersAndNumericOnlyBody 数字必须按字面保留
// （task_id 大整数不能被 float64 改值）；没有字符串的合法错误体不能被当成空体回落。
func TestScrubErrorBody_PreservesNumbersAndNumericOnlyBody(t *testing.T) {
	scrubber := testScrubber()

	in := `{"error":{"message":"rejected at api.aijws.com","type":"invalid_request_error"},"task_id":9007199254740993,"cost":0.000000123}`
	out, ok := scrubber.scrubErrorBody([]byte(in))
	if !ok {
		t.Fatal("错误体被判为不可清洗")
	}
	got := string(out)
	if strings.Contains(got, "aijws") {
		t.Fatalf("域名未剥: %s", got)
	}
	for _, literal := range []string{"9007199254740993", "0.000000123"} {
		if !strings.Contains(got, literal) {
			t.Fatalf("数字字面量 %s 被改写: %s", literal, got)
		}
	}

	numeric := `{"code":400,"success":false}`
	out, ok = scrubber.scrubErrorBody([]byte(numeric))
	if !ok || string(out) != numeric {
		t.Fatalf("纯数字错误体应原样放行: ok=%v body=%s", ok, out)
	}

	if _, ok := scrubber.scrubErrorBody([]byte(`{"a":1} trailing`)); ok {
		t.Fatal("正文后带垃圾的体不应被当作 JSON")
	}
}

// TestHostForwardPayload_DropsStaleContentLength 清洗改动了 body，上游的 Content-Length
// 就过期了；照抄给插件会让新插件重演 2026-08-29 的 CF 520。body 没变则原样保留。
func TestHostForwardPayload_DropsStaleContentLength(t *testing.T) {
	scrubber := testScrubber()
	headers := http.Header{
		"Content-Length": []string{"98"},
		"Content-Type":   []string{"application/json"},
	}

	dirty := hostForwardPayload(sdk.ForwardOutcome{
		Kind: sdk.OutcomeClientError,
		Upstream: sdk.UpstreamResponse{
			StatusCode: http.StatusBadRequest,
			Headers:    headers,
			Body:       []byte(`{"error":{"message":"Error from provider (Console Go): invalid image size"}}`),
		},
	}, scrubber)
	if _, present := dirty["headers"].(map[string]interface{})["Content-Length"]; present {
		t.Fatalf("清洗后仍带过期 Content-Length: %v", dirty["headers"])
	}
	if _, present := dirty["headers"].(map[string]interface{})["Content-Type"]; !present {
		t.Fatalf("无关头被误删: %v", dirty["headers"])
	}
	if got := headers.Get("Content-Length"); got != "98" {
		t.Fatalf("调用方的 header 被原地修改: %q", got)
	}

	clean := hostForwardPayload(sdk.ForwardOutcome{
		Kind: sdk.OutcomeClientError,
		Upstream: sdk.UpstreamResponse{
			StatusCode: http.StatusBadRequest,
			Headers:    headers,
			Body:       []byte(`{"error":{"message":"invalid image size"}}`),
		},
	}, scrubber)
	if _, present := clean["headers"].(map[string]interface{})["Content-Length"]; !present {
		t.Fatalf("body 未变却删了 Content-Length: %v", clean["headers"])
	}
}

// TestEgressWriter_InstallAfterTTFTWriterDoesNotDoubleWrap ttftWriter 装在外层后
// 再装闸门必须复用链上已有的那个，否则最外层多包一层，*ttftWriter 断言静默失败。
func TestEgressWriter_InstallAfterTTFTWriterDoesNotDoubleWrap(t *testing.T) {
	c, _ := newGatedContext(t, nil) // 内部顺序：installEgressWriter → installTTFTWriter
	outer, ok := c.Writer.(*ttftWriter)
	if !ok {
		t.Fatalf("最外层应是 *ttftWriter，实际 %T", c.Writer)
	}
	inner, ok := outer.ResponseWriter.(*egressWriter)
	if !ok {
		t.Fatalf("ttftWriter 内层应是 *egressWriter，实际 %T", outer.ResponseWriter)
	}

	again := installEgressWriter(c, nil)
	if again != inner {
		t.Fatal("二次安装没有复用链上已有的闸门")
	}
	if c.Writer != gin.ResponseWriter(outer) {
		t.Fatalf("二次安装改变了最外层 writer: %T", c.Writer)
	}
}
