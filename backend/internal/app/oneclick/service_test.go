package oneclick

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	appapikey "github.com/DouDOU-start/airgate-core/internal/app/apikey"
)

// fakeRevealer 测试用 KeyRevealer。
type fakeRevealer struct {
	key appapikey.Key
	err error
}

func (f *fakeRevealer) RevealOwned(_ context.Context, _, _ int) (appapikey.Key, error) {
	return f.key, f.err
}

func newTestService(t *testing.T, revealer KeyRevealer) (*Service, *redis.Client) {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return NewService(client, revealer, nil), client
}

func TestIssueAndExchange(t *testing.T) {
	svc, rdb := newTestService(t, &fakeRevealer{key: appapikey.Key{PlainKey: "sk-test-123", Name: "工作机"}})
	ctx := context.Background()

	token, err := svc.IssueToken(ctx, 7, 42)
	if err != nil {
		t.Fatalf("IssueToken: %v", err)
	}
	if token == "" {
		t.Fatal("IssueToken 返回空 token")
	}
	if got := svc.Status(ctx, 7, token); got != StatusPending {
		t.Fatalf("签发后状态 = %q, want pending", got)
	}

	result, err := svc.Exchange(ctx, token)
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}
	if result.APIKey != "sk-test-123" || result.KeyName != "工作机" {
		t.Fatalf("Exchange 结果不符: %+v", result)
	}
	if got := svc.Status(ctx, 7, token); got != StatusExchanged {
		t.Fatalf("兑换后状态 = %q, want exchanged", got)
	}

	// 二次兑换：单次语义,记录被摘除后不再可用
	if _, err := svc.Exchange(ctx, token); !errors.Is(err, ErrTokenState) {
		t.Fatalf("二次 Exchange err = %v, want ErrTokenState", err)
	}

	// Redis 里不应存 key 明文
	keys, _ := rdb.Keys(ctx, setupTokenKeyPrefix+"*").Result()
	for _, k := range keys {
		v, _ := rdb.Get(ctx, k).Result()
		if strings.Contains(v, "sk-test-123") {
			t.Fatalf("Redis 记录泄露 key 明文: %s", v)
		}
	}
}

func TestExchangeErrors(t *testing.T) {
	tests := []struct {
		name     string
		revealer KeyRevealer
		token    func(svc *Service, ctx context.Context) string
		wantErr  error
	}{
		{
			name:     "token 不存在",
			revealer: &fakeRevealer{},
			token:    func(*Service, context.Context) string { return "no-such-token" },
			wantErr:  ErrTokenNotFound,
		},
		{
			name:     "空 token",
			revealer: &fakeRevealer{},
			token:    func(*Service, context.Context) string { return "  " },
			wantErr:  ErrTokenNotFound,
		},
		{
			name:     "reveal 失败向上冒泡",
			revealer: &fakeRevealer{err: appapikey.ErrKeyDecryptFailed},
			token: func(svc *Service, ctx context.Context) string {
				// IssueToken 也会先 reveal 校验,这里绕过它直接种一条 pending 记录
				raw := `{"user_id":1,"key_id":2,"status":"pending"}`
				svc.rdb.Set(ctx, setupTokenKey("seeded"), raw, setupTokenTTL)
				return "seeded"
			},
			wantErr: appapikey.ErrKeyDecryptFailed,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, _ := newTestService(t, tt.revealer)
			ctx := context.Background()
			_, err := svc.Exchange(ctx, tt.token(svc, ctx))
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestVerifyLifecycle(t *testing.T) {
	svc, _ := newTestService(t, &fakeRevealer{key: appapikey.Key{PlainKey: "sk-x"}})
	ctx := context.Background()

	token, err := svc.IssueToken(ctx, 1, 2)
	if err != nil {
		t.Fatalf("IssueToken: %v", err)
	}

	// 未兑换先回执 → 状态错误
	if err := svc.Verify(ctx, token); !errors.Is(err, ErrTokenState) {
		t.Fatalf("pending Verify err = %v, want ErrTokenState", err)
	}
	if _, err := svc.Exchange(ctx, token); err != nil {
		t.Fatalf("Exchange: %v", err)
	}
	if err := svc.Verify(ctx, token); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if got := svc.Status(ctx, 1, token); got != StatusVerified {
		t.Fatalf("回执后状态 = %q, want verified", got)
	}
	if err := svc.Verify(ctx, "missing"); !errors.Is(err, ErrTokenNotFound) {
		t.Fatalf("missing Verify err = %v, want ErrTokenNotFound", err)
	}
}

func TestStatusOwnership(t *testing.T) {
	svc, _ := newTestService(t, &fakeRevealer{key: appapikey.Key{PlainKey: "sk-x"}})
	ctx := context.Background()

	token, err := svc.IssueToken(ctx, 9, 1)
	if err != nil {
		t.Fatalf("IssueToken: %v", err)
	}
	if got := svc.Status(ctx, 8, token); got != StatusExpired {
		t.Fatalf("他人查询状态 = %q, want expired（不泄露存在性）", got)
	}
	if got := svc.Status(ctx, 9, "missing"); got != StatusExpired {
		t.Fatalf("不存在 token 状态 = %q, want expired", got)
	}
}

func TestRedisUnavailable(t *testing.T) {
	svc := NewService(nil, &fakeRevealer{}, nil)
	ctx := context.Background()
	if _, err := svc.IssueToken(ctx, 1, 1); !errors.Is(err, ErrRedisUnavailable) {
		t.Fatalf("IssueToken err = %v, want ErrRedisUnavailable", err)
	}
	if _, err := svc.Exchange(ctx, "t"); !errors.Is(err, ErrRedisUnavailable) {
		t.Fatalf("Exchange err = %v, want ErrRedisUnavailable", err)
	}
	if got := svc.Status(ctx, 1, "t"); got != StatusExpired {
		t.Fatalf("Status = %q, want expired", got)
	}
}

func TestRenderScripts(t *testing.T) {
	svc := NewService(nil, &fakeRevealer{}, nil)
	cfg := Config{BaseURL: "https://api.example.com", SiteName: "HopBase"}

	for _, tc := range []struct {
		name   string
		render func(Config) (string, error)
		needle string
	}{
		{"bash", svc.RenderSetupScript, `BASE_URL="https://api.example.com"`},
		{"powershell", svc.RenderSetupScriptPowerShell, `$BaseUrl  = 'https://api.example.com'`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := tc.render(cfg)
			if err != nil {
				t.Fatalf("render: %v", err)
			}
			if !strings.Contains(out, tc.needle) {
				t.Fatalf("渲染结果缺少 %q", tc.needle)
			}
			if strings.Contains(out, "{{") {
				t.Fatal("渲染结果残留未替换的模板占位符")
			}
		})
	}
}
