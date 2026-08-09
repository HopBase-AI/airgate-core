package plugin

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	sdk "github.com/DouDOU-start/airgate-sdk/sdkgo"
)

// RelayService 通用签名媒体中继。
//
// 场景：上游平台返回的产物是短时效签名直链（如对象存储 24h 签名 URL），我们既不想
// 把上游地址透传给终端用户，也不想下载落盘承担存储成本。中继把它变成
// 「我们自己域名 + 长时效签名 token」的稳定地址：
//
//	GET /relay/v1/<token>  →  验签 → 请对应 gateway 插件解析出当前上游直链 → 流式回源
//
// core 不感知任何平台语义：token 只包含 (plugin, ref, exp, filename)，ref 的含义由
// 插件自己定义（如任务 ID + 防猜随机串）。回源为原生 HTTP 流式拷贝并透传 Range 等
// 条件头，不经过 gRPC 大消息、不缓冲整个文件。
//
// 插件侧契约：
//   - 经 host method relay.sign_url 签发中继路径；
//   - Forward 需处理内部解析请求：X-Airgate-Internal=relay-resolve、
//     X-Forwarded-Path=relayResolvePath、body {"ref":"..."}，成功时返回
//     JSON {"url":"<当前上游直链>"}；无法解析（过期/不存在）返回非 2xx 与
//     {"error":{"message":"..."}}，中继会以 410 转达给客户端。
type RelayService struct {
	manager *Manager
	hmacKey []byte
	client  *http.Client

	// loadAccountBearerAuth keeps credential lookup inside Core. Plugins may
	// identify the account that owns a protected relay resource, but never
	// receive or return the credential through the relay contract.
	loadAccountBearerAuth func(context.Context, int64, string) (string, error)
	resolveTarget         func(context.Context, relayTokenPayload) (relayUpstreamTarget, error)
}

const (
	// RelayPublicPrefix 公开路由前缀，router 注册 GET/HEAD RelayPublicPrefix/:token。
	RelayPublicPrefix = "/relay/v1"

	// RelayResolvePath 内部解析请求的 X-Forwarded-Path，gateway 插件按此路径分流。
	RelayResolvePath = "/internal/relay/resolve"

	// RelayInternalHeaderValue 内部解析请求的 X-Airgate-Internal 标记值。
	RelayInternalHeaderValue = "relay-resolve"

	relayTokenVersion   = "v1"
	relayDefaultTTL     = 7 * 24 * time.Hour
	relayMaxTTL         = 30 * 24 * time.Hour
	relayResolveTimeout = 30 * time.Second
)

var (
	errRelayTokenInvalid = errors.New("中继 token 无效")
	errRelayTokenExpired = errors.New("中继 token 已过期")
)

// relayTokenPayload 是 token 内明文携带（HMAC 保护完整性）的最小信息。
type relayTokenPayload struct {
	Plugin   string `json:"p"`
	Ref      string `json:"r"`
	Expires  int64  `json:"e"`
	Filename string `json:"f,omitempty"`
}

// NewRelayService 以 hex 编码的 secret（复用 security.api_key_secret）派生 HMAC 密钥。
func NewRelayService(manager *Manager, secretHex string) (*RelayService, error) {
	raw, err := hex.DecodeString(secretHex)
	if err != nil {
		return nil, fmt.Errorf("relay secret 不是有效的 hex 字符串: %w", err)
	}
	if len(raw) < 32 {
		return nil, fmt.Errorf("relay secret 长度不足 32 字节")
	}
	mac := hmac.New(sha256.New, raw[:32])
	mac.Write([]byte("airgate-relay-v1"))
	key := mac.Sum(nil)
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = 60 * time.Second
	service := &RelayService{
		manager: manager,
		hmacKey: key,
		// 不设整体 Timeout：大文件流式拷贝时长不可预估，取消交由请求 context。
		client: &http.Client{Transport: transport},
	}
	service.loadAccountBearerAuth = service.defaultAccountBearerAuth
	service.resolveTarget = service.resolveUpstreamURL
	return service, nil
}

// SignPath 为 (plugin, ref) 签发中继路径（不含 host），返回路径与过期时间（unix 秒）。
func (s *RelayService) SignPath(pluginName, ref, filename string, ttl time.Duration) (string, int64, error) {
	pluginName = strings.TrimSpace(pluginName)
	ref = strings.TrimSpace(ref)
	if pluginName == "" || ref == "" {
		return "", 0, fmt.Errorf("plugin 与 ref 不能为空")
	}
	if ttl <= 0 {
		ttl = relayDefaultTTL
	}
	if ttl > relayMaxTTL {
		ttl = relayMaxTTL
	}
	payload := relayTokenPayload{
		Plugin:   pluginName,
		Ref:      ref,
		Expires:  time.Now().Add(ttl).Unix(),
		Filename: strings.TrimSpace(filename),
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", 0, err
	}
	encoded := base64.RawURLEncoding.EncodeToString(body)
	token := relayTokenVersion + "." + encoded + "." + s.sign(encoded)
	return RelayPublicPrefix + "/" + token, payload.Expires, nil
}

func (s *RelayService) sign(encodedPayload string) string {
	mac := hmac.New(sha256.New, s.hmacKey)
	mac.Write([]byte(relayTokenVersion))
	mac.Write([]byte("."))
	mac.Write([]byte(encodedPayload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// parseToken 验签并解出 payload。
func (s *RelayService) parseToken(token string) (relayTokenPayload, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 || parts[0] != relayTokenVersion {
		return relayTokenPayload{}, errRelayTokenInvalid
	}
	if !hmac.Equal([]byte(s.sign(parts[1])), []byte(parts[2])) {
		return relayTokenPayload{}, errRelayTokenInvalid
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return relayTokenPayload{}, errRelayTokenInvalid
	}
	var payload relayTokenPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return relayTokenPayload{}, errRelayTokenInvalid
	}
	if payload.Plugin == "" || payload.Ref == "" {
		return relayTokenPayload{}, errRelayTokenInvalid
	}
	if payload.Expires > 0 && time.Now().Unix() > payload.Expires {
		return relayTokenPayload{}, errRelayTokenExpired
	}
	return payload, nil
}

type relayUpstreamTarget struct {
	url           string
	authorization string
}

// resolveUpstreamURL 请插件把 ref 解析成当前可用的上游直链。受保护资源可以
// 返回所属 account_id；Core 从本地账户读取 api_key，并只在回源请求中附加 Bearer
// 授权。凭证不会经过插件响应、任务记录或公开 HTTP 响应。
func (s *RelayService) resolveUpstreamURL(ctx context.Context, payload relayTokenPayload) (relayUpstreamTarget, error) {
	inst := s.manager.GetGatewayInstance(payload.Plugin)
	if inst == nil {
		return relayUpstreamTarget{}, fmt.Errorf("插件 %s 未运行", payload.Plugin)
	}
	body, _ := json.Marshal(map[string]string{"ref": payload.Ref})
	req := &sdk.ForwardRequest{
		Account: &sdk.Account{Platform: inst.Platform},
		Body:    body,
		Headers: http.Header{
			"Content-Type":       {"application/json"},
			"X-Airgate-Internal": {RelayInternalHeaderValue},
			"X-Forwarded-Path":   {RelayResolvePath},
			"X-Forwarded-Method": {http.MethodPost},
		},
		Stream: false,
	}
	resolveCtx, cancel := context.WithTimeout(ctx, relayResolveTimeout)
	defer cancel()
	outcome, err := inst.Gateway.Forward(resolveCtx, req)
	if err != nil {
		return relayUpstreamTarget{}, fmt.Errorf("插件解析失败: %w", err)
	}
	if outcome.Upstream.StatusCode < 200 || outcome.Upstream.StatusCode >= 300 {
		return relayUpstreamTarget{}, fmt.Errorf("插件拒绝解析(status=%d): %s",
			outcome.Upstream.StatusCode, extractErrorMessage(outcome.Upstream.Body))
	}
	var resolved struct {
		URL       string `json:"url"`
		AccountID int64  `json:"account_id,omitempty"`
	}
	if err := json.Unmarshal(outcome.Upstream.Body, &resolved); err != nil || strings.TrimSpace(resolved.URL) == "" {
		return relayUpstreamTarget{}, fmt.Errorf("插件解析响应缺少 url")
	}
	return s.targetFromResolvedURL(ctx, resolved.URL, resolved.AccountID, inst.Platform)
}

func (s *RelayService) targetFromResolvedURL(ctx context.Context, rawURL string, accountID int64, platform string) (relayUpstreamTarget, error) {
	target := relayUpstreamTarget{url: strings.TrimSpace(rawURL)}
	if target.url == "" {
		return relayUpstreamTarget{}, fmt.Errorf("插件解析响应缺少 url")
	}
	if accountID == 0 {
		return target, nil
	}
	if accountID < 0 {
		return relayUpstreamTarget{}, fmt.Errorf("插件返回的 account_id 非法")
	}
	if s.loadAccountBearerAuth == nil {
		return relayUpstreamTarget{}, fmt.Errorf("中继账户凭证解析不可用")
	}
	authorization, err := s.loadAccountBearerAuth(ctx, accountID, platform)
	if err != nil {
		return relayUpstreamTarget{}, fmt.Errorf("中继账户凭证解析失败: %w", err)
	}
	target.authorization = authorization
	return target, nil
}

func (s *RelayService) defaultAccountBearerAuth(ctx context.Context, accountID int64, platform string) (string, error) {
	if s.manager == nil || s.manager.db == nil {
		return "", fmt.Errorf("账户存储不可用")
	}
	if accountID > int64(^uint(0)>>1) {
		return "", fmt.Errorf("account_id 超出范围")
	}
	item, err := s.manager.db.Account.Get(ctx, int(accountID))
	if err != nil {
		return "", err
	}
	if !strings.EqualFold(strings.TrimSpace(item.Platform), strings.TrimSpace(platform)) {
		return "", fmt.Errorf("账户平台不匹配")
	}
	apiKey := strings.TrimSpace(item.Credentials["api_key"])
	if apiKey == "" {
		return "", fmt.Errorf("账户缺少 api_key")
	}
	return "Bearer " + apiKey, nil
}

// relayPassthroughRequestHeaders 回源时透传的条件/范围请求头。
var relayPassthroughRequestHeaders = []string{
	"Range", "If-Range", "If-None-Match", "If-Modified-Since", "Accept-Encoding",
}

// relayPassthroughResponseHeaders 写回客户端的响应头白名单。
var relayPassthroughResponseHeaders = []string{
	"Content-Type", "Content-Length", "Content-Range", "Accept-Ranges",
	"ETag", "Last-Modified", "Content-Encoding",
}

// ServeHTTP 处理公开中继请求（GET/HEAD）。token 为路径中的最后一段。
func (s *RelayService) ServeHTTP(w http.ResponseWriter, r *http.Request, token string) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		relayError(w, http.StatusMethodNotAllowed, "仅支持 GET/HEAD")
		return
	}
	payload, err := s.parseToken(strings.TrimSpace(token))
	if err != nil {
		status := http.StatusForbidden
		message := "链接无效"
		if errors.Is(err, errRelayTokenExpired) {
			status = http.StatusGone
			message = "链接已过期"
		}
		relayError(w, status, message)
		return
	}

	resolveTarget := s.resolveTarget
	if resolveTarget == nil {
		resolveTarget = s.resolveUpstreamURL
	}
	target, err := resolveTarget(r.Context(), payload)
	if err != nil {
		slog.Warn("relay_resolve_failed",
			sdk.LogFieldPluginID, payload.Plugin, "ref", payload.Ref, sdk.LogFieldError, err)
		relayError(w, http.StatusGone, "文件已不可用（可能超出上游保留时限）")
		return
	}

	upstreamReq, err := http.NewRequestWithContext(r.Context(), r.Method, target.url, nil)
	if err != nil {
		relayError(w, http.StatusBadGateway, "回源地址无效")
		return
	}
	for _, key := range relayPassthroughRequestHeaders {
		if v := r.Header.Get(key); v != "" {
			upstreamReq.Header.Set(key, v)
		}
	}
	if target.authorization != "" {
		upstreamReq.Header.Set("Authorization", target.authorization)
	}

	resp, err := s.client.Do(upstreamReq)
	if err != nil {
		slog.Warn("relay_fetch_failed",
			sdk.LogFieldPluginID, payload.Plugin, "ref", payload.Ref, sdk.LogFieldError, err)
		relayError(w, http.StatusBadGateway, "回源失败，请稍后重试")
		return
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		slog.Warn("relay_upstream_status",
			sdk.LogFieldPluginID, payload.Plugin, "ref", payload.Ref, sdk.LogFieldStatus, resp.StatusCode)
		// 上游签名过期等场景统一 410：对客户端而言就是文件不可用。
		relayError(w, http.StatusGone, "文件已不可用（可能超出上游保留时限）")
		return
	}

	header := w.Header()
	for _, key := range relayPassthroughResponseHeaders {
		if v := resp.Header.Get(key); v != "" {
			header.Set(key, v)
		}
	}
	if payload.Filename != "" {
		header.Set("Content-Disposition", fmt.Sprintf("inline; filename=%q", payload.Filename))
	}
	w.WriteHeader(resp.StatusCode)
	if r.Method == http.MethodHead {
		return
	}
	if _, err := io.Copy(w, resp.Body); err != nil {
		// 客户端中断或上游断流：连接层错误，仅记录。
		slog.Debug("relay_copy_interrupted",
			sdk.LogFieldPluginID, payload.Plugin, "ref", payload.Ref, sdk.LogFieldError, err)
	}
}

func relayError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]any{"message": message, "type": "relay_error"},
	})
}
