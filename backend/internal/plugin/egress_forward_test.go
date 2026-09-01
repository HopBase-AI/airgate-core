package plugin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"entgo.io/ent/dialect/sql/schema"
	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"

	"github.com/DouDOU-start/airgate-core/ent"
	"github.com/DouDOU-start/airgate-core/ent/enttest"
	"github.com/DouDOU-start/airgate-core/internal/auth"
	"github.com/DouDOU-start/airgate-core/internal/billing"
	"github.com/DouDOU-start/airgate-core/internal/scheduler"
	sdk "github.com/DouDOU-start/airgate-sdk/sdkgo"
)

// egress_forward_test.go —— 出网闸门的全链路验证：
// 走真实的 writeResult 派发（成功 / 上游 4xx / 流中断三种判决 + 计费落库），
// 上下文按 Forwarder.Forward 的真实顺序装 egressWriter → ttftWriter。

type egressFixture struct {
	forwarder *Forwarder
	state     *forwardState
	ctx       *gin.Context
	recorder  *httptest.ResponseRecorder
	db        *ent.Client
}

// newEgressFixture 造一套「用户 → API Key → 分组 → 中继账号」的真实链路。
// 账号刻意用生产上那家中继的形态：名字带供应商名，凭证里是中继域名。
func newEgressFixture(t *testing.T, dsn string, stream bool) *egressFixture {
	t.Helper()
	db := enttest.Open(t, "sqlite3", dsn, enttest.WithMigrateOptions(schema.WithGlobalUniqueID(false)))
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	})

	ctx := context.Background()
	user := db.User.Create().
		SetEmail("egress@example.com").
		SetPasswordHash("secret").
		SaveX(ctx)
	group := db.Group.Create().
		SetName("Codex Plus").
		SetPlatform("openai").
		SaveX(ctx)
	account := db.Account.Create().
		SetName("贾克斯-pro-0.15").
		SetPlatform("openai").
		SetCredentials(map[string]string{"base_url": "https://api.aijws.com", "api_key": "sk-secret"}).
		SaveX(ctx)
	key := db.APIKey.Create().
		SetName("loyal-Codexplus").
		SetKeyHash("hash-" + dsn).
		SetUserID(user.ID).
		SetGroupID(group.ID).
		SaveX(ctx)

	usageRecorder := billing.NewRecorder(db, 0)
	usageRecorder.Start()
	t.Cleanup(usageRecorder.Stop)

	forwarder := &Forwarder{
		scheduler:  scheduler.NewScheduler(db, nil),
		calculator: billing.NewCalculator(),
		recorder:   usageRecorder,
		db:         db,
	}

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	// 中间件已设的我方头
	c.Writer.Header().Set("X-Request-Id", "ours-e2e")
	c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
	// Forward 的真实安装顺序
	installEgressWriter(c, nil)
	installTTFTWriter(c)

	return &egressFixture{
		forwarder: forwarder,
		ctx:       c,
		recorder:  recorder,
		db:        db,
		state: &forwardState{
			stream:      stream,
			requestPath: "/v1/chat/completions",
			model:       "gpt-5.6",
			plugin:      &PluginInstance{Name: "openai", Platform: "openai"},
			account:     account,
			keyInfo: &auth.APIKeyInfo{
				KeyID:               key.ID,
				UserID:              user.ID,
				UserEmail:           user.Email,
				GroupID:             group.ID,
				GroupRateMultiplier: 1,
				UserBalance:         100,
			},
		},
	}
}

// TestForwardEgress_SuccessKeepsPayloadStripsIdentity 正常响应：
// 客户端拿到完整的成功响应体与限流头，拿不到任何上游身份。
func TestForwardEgress_SuccessKeepsPayloadStripsIdentity(t *testing.T) {
	fx := newEgressFixture(t, "file:egress_success?mode=memory&cache=shared&_fk=1", false)

	successBody := `{"id":"chatcmpl-abc","object":"chat.completion","model":"gpt-5.6","choices":[{"index":0,"message":{"role":"assistant","content":"你好"}}],"usage":{"prompt_tokens":9,"completion_tokens":3}}`
	fx.forwarder.writeResult(fx.ctx, fx.state, forwardExecution{
		outcome: sdk.ForwardOutcome{
			Kind: sdk.OutcomeSuccess,
			Upstream: sdk.UpstreamResponse{
				StatusCode: http.StatusOK,
				Headers:    leakyUpstreamHeaders(),
				Body:       []byte(successBody),
			},
			Usage: &sdk.Usage{Model: "gpt-5.6", Currency: "USD"},
		},
	})

	if fx.recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", fx.recorder.Code)
	}
	if got := fx.recorder.Body.String(); got != successBody {
		t.Fatalf("成功响应体被改动:\n got=%s\nwant=%s", got, successBody)
	}
	assertHeaderPolicy(t, fx.recorder, "ours-e2e")
}

// TestForwardEgress_ClientErrorScrubsIdentityKeepsSemantics 上游 4xx：
// 供应商标识剥净、客户端要用的报错语义与状态码原样保留。
func TestForwardEgress_ClientErrorScrubsIdentityKeepsSemantics(t *testing.T) {
	fx := newEgressFixture(t, "file:egress_client_error?mode=memory&cache=shared&_fk=1", false)

	upstreamBody := `{"error":{"message":"Error from provider (Console Go): 'max_tokens' is not supported on /v1/responses — use 'max_output_tokens' (Request-ID: USA-20434252906100)","type":"invalid_request_error","code":"unsupported_parameter","param":"max_tokens"},"request_id":"USA-20434252906100"}`
	fx.forwarder.writeResult(fx.ctx, fx.state, forwardExecution{
		outcome: sdk.ForwardOutcome{
			Kind: sdk.OutcomeClientError,
			Upstream: sdk.UpstreamResponse{
				StatusCode: http.StatusBadRequest,
				Headers:    leakyUpstreamHeaders(),
				Body:       []byte(upstreamBody),
			},
			Reason: "HTTP 400: 'max_tokens' is not supported",
		},
	})

	if fx.recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400（错误状态码必须如实透传）", fx.recorder.Code)
	}
	assertHeaderPolicy(t, fx.recorder, "ours-e2e")

	body := fx.recorder.Body.String()
	for _, leak := range []string{"Console Go", "USA-20434252906100", "aijws", "贾克斯", "request_id"} {
		if strings.Contains(body, leak) {
			t.Errorf("4xx 响应泄漏供应商标识 %q: %s", leak, body)
		}
	}
	for _, keep := range []string{"'max_tokens' is not supported", "max_output_tokens", "invalid_request_error", "unsupported_parameter"} {
		if !strings.Contains(body, keep) {
			t.Errorf("4xx 响应丢失客户端需要的信息 %q: %s", keep, body)
		}
	}
}

// TestForwardEgress_UpstreamFailureStaysGeneric 上游故障（非客户端错误）：
// 对外只给分类文案，既不给上游原文也不给上游身份；错误仍如实落库供管理员排障。
func TestForwardEgress_UpstreamFailureStaysGeneric(t *testing.T) {
	fx := newEgressFixture(t, "file:egress_upstream_failure?mode=memory&cache=shared&_fk=1", false)

	fx.forwarder.writeResult(fx.ctx, fx.state, forwardExecution{
		outcome: sdk.ForwardOutcome{
			Kind: sdk.OutcomeUpstreamTransient,
			Upstream: sdk.UpstreamResponse{
				StatusCode: http.StatusBadGateway,
				Headers:    leakyUpstreamHeaders(),
			},
			Reason: "[ObjectParam] [tool_choice.name] api.aijws.com 转换失败",
		},
	})

	if fx.recorder.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", fx.recorder.Code)
	}
	body := fx.recorder.Body.String()
	for _, leak := range []string{"aijws", "贾克斯", "tool_choice.name", "ObjectParam"} {
		if strings.Contains(body, leak) {
			t.Errorf("上游故障响应泄漏内部细节 %q: %s", leak, body)
		}
	}
	if !strings.Contains(body, "上游服务暂不可用") {
		t.Errorf("缺少对外分类文案: %s", body)
	}
	for _, name := range mustStripHeaders {
		if got := fx.recorder.Header().Get(name); got != "" {
			t.Errorf("上游故障响应仍带身份头 %s = %q", name, got)
		}
	}
}

// TestForwardEgress_StreamKeepsProtocolHeaders 流式：闸门不能破坏 SSE 协议头，
// 客户端仍拿得到 Content-Type / X-Accel-Buffering / 限流头。
func TestForwardEgress_StreamKeepsProtocolHeaders(t *testing.T) {
	fx := newEgressFixture(t, "file:egress_stream?mode=memory&cache=shared&_fk=1", true)

	// 插件流式直写：先搬上游头，再写 SSE 事件
	for key, values := range leakyUpstreamHeaders() {
		for _, v := range values {
			fx.ctx.Writer.Header().Set(key, v)
		}
	}
	fx.ctx.Writer.Header().Set("Content-Type", "text/event-stream")
	if _, err := fx.ctx.Writer.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"你\"}}]}\n\n")); err != nil {
		t.Fatalf("写 SSE: %v", err)
	}
	fx.ctx.Writer.Flush()

	fx.forwarder.writeResult(fx.ctx, fx.state, forwardExecution{
		outcome: sdk.ForwardOutcome{
			Kind:  sdk.OutcomeSuccess,
			Usage: &sdk.Usage{Model: "gpt-5.6", Currency: "USD"},
		},
	})

	if got := fx.recorder.Header().Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("Content-Type = %q, want text/event-stream", got)
	}
	if got := fx.recorder.Header().Get("X-Accel-Buffering"); got != "no" {
		t.Fatalf("X-Accel-Buffering 被误剥，SSE 会被反代缓冲")
	}
	if got := fx.recorder.Header().Get("X-Codex-Primary-Used-Percent"); got != "37.2" {
		t.Fatalf("Codex 限流头被误剥: %q", got)
	}
	for _, name := range mustStripHeaders {
		if got := fx.recorder.Header().Get(name); got != "" {
			t.Errorf("流式响应仍带身份头 %s = %q", name, got)
		}
	}
	if !strings.Contains(fx.recorder.Body.String(), "delta") {
		t.Fatalf("SSE 体被改动: %q", fx.recorder.Body.String())
	}
}
