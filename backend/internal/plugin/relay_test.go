package plugin

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	sdk "github.com/DouDOU-start/airgate-sdk/sdkgo"
)

const relayTestSecret = "6a8f3d2e1b9c4f7a0e5d2c8b3a1f6e9d4c7b2a5e8f1d3c6b9a2e5f8d1c4b7a0e"

func newTestRelay(t *testing.T) *RelayService {
	t.Helper()
	rs, err := NewRelayService(nil, relayTestSecret)
	if err != nil {
		t.Fatalf("NewRelayService: %v", err)
	}
	return rs
}

func TestRelaySignPathRoundtrip(t *testing.T) {
	rs := newTestRelay(t)
	path, expiresAt, err := rs.SignPath("gateway-seedance", "vt1xmvt-a/tok/0", "video.mp4", time.Hour)
	if err != nil {
		t.Fatalf("SignPath: %v", err)
	}
	if !strings.HasPrefix(path, RelayPublicPrefix+"/") {
		t.Fatalf("path 前缀错误: %s", path)
	}
	if got, want := expiresAt, time.Now().Add(time.Hour).Unix(); got < want-5 || got > want+5 {
		t.Fatalf("expires_at=%d 偏离预期 %d", got, want)
	}
	token := strings.TrimPrefix(path, RelayPublicPrefix+"/")
	payload, err := rs.parseToken(token)
	if err != nil {
		t.Fatalf("parseToken: %v", err)
	}
	if payload.Plugin != "gateway-seedance" || payload.Ref != "vt1xmvt-a/tok/0" || payload.Filename != "video.mp4" {
		t.Fatalf("payload 不符: %+v", payload)
	}
}

func TestRelaySignPathValidation(t *testing.T) {
	rs := newTestRelay(t)
	if _, _, err := rs.SignPath("", "ref", "", time.Hour); err == nil {
		t.Fatal("空 plugin 应报错")
	}
	if _, _, err := rs.SignPath("p", "", "", time.Hour); err == nil {
		t.Fatal("空 ref 应报错")
	}
}

func TestRelayParseTokenRejects(t *testing.T) {
	rs := newTestRelay(t)
	path, _, err := rs.SignPath("gateway-seedance", "ref", "", time.Hour)
	if err != nil {
		t.Fatalf("SignPath: %v", err)
	}
	token := strings.TrimPrefix(path, RelayPublicPrefix+"/")

	cases := map[string]string{
		"非法结构":  "abc",
		"版本不符":  "v0." + strings.SplitN(token, ".", 2)[1],
		"签名被篡改": token[:len(token)-2] + "xx",
	}
	for name, raw := range cases {
		if _, err := rs.parseToken(raw); err == nil {
			t.Fatalf("%s 应被拒绝", name)
		}
	}

	// 换密钥后旧 token 失效
	other, err := NewRelayService(nil, strings.Repeat("ab", 32))
	if err != nil {
		t.Fatalf("NewRelayService: %v", err)
	}
	if _, err := other.parseToken(token); err == nil {
		t.Fatal("跨密钥 token 应被拒绝")
	}
}

func TestRelayParseTokenExpired(t *testing.T) {
	rs := newTestRelay(t)
	// 过期精度是秒级，手工构造一个已过期 payload。
	raw, _ := json.Marshal(relayTokenPayload{
		Plugin:  "gateway-seedance",
		Ref:     "ref",
		Expires: time.Now().Add(-time.Minute).Unix(),
	})
	encoded := base64.RawURLEncoding.EncodeToString(raw)
	token := relayTokenVersion + "." + encoded + "." + rs.sign(encoded)
	if _, err := rs.parseToken(token); !errors.Is(err, errRelayTokenExpired) {
		t.Fatalf("过期 token 应返回 errRelayTokenExpired, got %v", err)
	}
}

func TestRelayTargetUsesCoreAccountAuthorization(t *testing.T) {
	rs := newTestRelay(t)
	var gotAccountID int64
	var gotPlatform string
	rs.loadAccountBearerAuth = func(_ context.Context, accountID int64, platform string) (string, error) {
		gotAccountID = accountID
		gotPlatform = platform
		return "Bearer relay-test-key", nil
	}

	target, err := rs.targetFromResolvedURL(context.Background(), "https://upstream.example/v1/video/files/a/last-frame", 42, "seedance")
	if err != nil {
		t.Fatalf("targetFromResolvedURL: %v", err)
	}
	if target.url != "https://upstream.example/v1/video/files/a/last-frame" {
		t.Fatalf("url=%q", target.url)
	}
	if target.authorization != "Bearer relay-test-key" {
		t.Fatalf("authorization=%q", target.authorization)
	}
	if gotAccountID != 42 || gotPlatform != "seedance" {
		t.Fatalf("credential lookup=(%d,%q)", gotAccountID, gotPlatform)
	}

	// The internal target must not be serializable into an HTTP response.
	body, err := json.Marshal(target)
	if err != nil {
		t.Fatalf("marshal target: %v", err)
	}
	if strings.Contains(string(body), "relay-test-key") {
		t.Fatalf("serialized target leaked authorization: %s", body)
	}
}

func TestRelayTargetWithoutAccountDoesNotLoadCredentials(t *testing.T) {
	rs := newTestRelay(t)
	rs.loadAccountBearerAuth = func(context.Context, int64, string) (string, error) {
		t.Fatal("public upstream URLs must not load account credentials")
		return "", nil
	}
	target, err := rs.targetFromResolvedURL(context.Background(), "https://storage.example/video.mp4", 0, "seedance")
	if err != nil {
		t.Fatalf("targetFromResolvedURL: %v", err)
	}
	if target.authorization != "" {
		t.Fatalf("unexpected authorization: %q", target.authorization)
	}
}

func TestRelayServeHTTPForwardsAuthorizationOnlyUpstream(t *testing.T) {
	var gotAuthorization string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuthorization = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte("frame-bytes"))
	}))
	defer upstream.Close()

	rs := newTestRelay(t)
	rs.resolveTarget = func(context.Context, relayTokenPayload) (relayUpstreamTarget, error) {
		return relayUpstreamTarget{url: upstream.URL + "/last-frame", authorization: "Bearer relay-test-key"}, nil
	}
	path, _, err := rs.SignPath("gateway-seedance", "vt1/token/last-frame", "last-frame.jpg", time.Hour)
	if err != nil {
		t.Fatalf("SignPath: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "http://relay.example"+path, nil)
	recorder := httptest.NewRecorder()
	rs.ServeHTTP(recorder, req, strings.TrimPrefix(path, RelayPublicPrefix+"/"))

	if gotAuthorization != "Bearer relay-test-key" {
		t.Fatalf("upstream authorization=%q", gotAuthorization)
	}
	if recorder.Code != http.StatusOK || recorder.Body.String() != "frame-bytes" {
		t.Fatalf("relay response status=%d body=%q", recorder.Code, recorder.Body.String())
	}
	if recorder.Header().Get("Authorization") != "" || strings.Contains(recorder.Body.String(), "relay-test-key") {
		t.Fatalf("relay response leaked authorization: headers=%v body=%q", recorder.Header(), recorder.Body.String())
	}
}

func TestIsMetadataOnlyRoutePrefix(t *testing.T) {
	m := NewManager("", "info", "", nil)
	m.mu.Lock()
	m.routeCache["gateway-seedance"] = []sdk.RouteDefinition{
		{Method: "GET", Path: "/v1/models", Metadata: map[string]string{"metadata_only": "true"}},
		{Method: "GET", Path: "/v1/video/tasks", Metadata: map[string]string{"metadata_only": "prefix"}},
	}
	m.rebuildMetadataOnlyPathsLocked()
	m.mu.Unlock()

	cases := []struct {
		path string
		want bool
	}{
		{"/v1/models", true},
		{"/v1/models/x", false},           // "true" 声明只精确匹配
		{"/v1/video/tasks", true},         // prefix 声明本身命中
		{"/v1/video/tasks/mvt-123", true}, // prefix 声明覆盖子路径
		{"/v1/video/tasksfoo", false},     // 非路径边界不命中
		{"/v1/video/generate", false},
	}
	for _, c := range cases {
		if got := m.IsMetadataOnlyRoute(c.path); got != c.want {
			t.Fatalf("IsMetadataOnlyRoute(%s)=%v want %v", c.path, got, c.want)
		}
	}
}
