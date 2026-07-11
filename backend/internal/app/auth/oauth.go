package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"

	sdk "github.com/DouDOU-start/airgate-sdk/sdkgo"
)

// 支持的第三方登录平台。
const (
	OAuthProviderGoogle = "google"
	OAuthProviderGitHub = "github"
)

// oauthStateTTL state 参数有效期（授权页停留超时按失效处理）。
const oauthStateTTL = 10 * time.Minute

// oauthEndpoints 单个平台的协议端点（测试中用 httptest 覆盖）。
type oauthEndpoints struct {
	AuthURL     string
	TokenURL    string
	UserInfoURL string
	EmailsURL   string // GitHub 邮箱列表端点；其他平台留空
	Scopes      string
}

var defaultOAuthEndpoints = map[string]oauthEndpoints{
	OAuthProviderGoogle: {
		AuthURL:     "https://accounts.google.com/o/oauth2/v2/auth",
		TokenURL:    "https://oauth2.googleapis.com/token",
		UserInfoURL: "https://openidconnect.googleapis.com/v1/userinfo",
		Scopes:      "openid email profile",
	},
	OAuthProviderGitHub: {
		AuthURL:     "https://github.com/login/oauth/authorize",
		TokenURL:    "https://github.com/login/oauth/access_token",
		UserInfoURL: "https://api.github.com/user",
		EmailsURL:   "https://api.github.com/user/emails",
		Scopes:      "read:user user:email",
	},
}

// oauthProviderConfig 从系统设置读取的平台配置。
type oauthProviderConfig struct {
	Enabled      bool
	ClientID     string
	ClientSecret string
}

// oauthUserInfo 第三方平台返回的用户信息（归一化后）。
type oauthUserInfo struct {
	ProviderUserID string
	Email          string // 已验证邮箱；未验证视为空
	Name           string
}

var (
	oauthStateKeyOnce sync.Once
	oauthStateKey     []byte
)

// oauthSigningKey 进程级随机 state 签名密钥。
// authorize 与 callback 由同一进程处理（蓝绿切换瞬间跨进程的极端情况用户重试即可）。
func oauthSigningKey() []byte {
	oauthStateKeyOnce.Do(func() {
		key := make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			// 随机源不可用属于系统级故障；退化为固定串会破坏 CSRF 防护，直接 panic 暴露
			panic(fmt.Sprintf("oauth state key init failed: %v", err))
		}
		oauthStateKey = key
	})
	return oauthStateKey
}

// oauthHTTPClient 与第三方交换 token / 拉取用户信息的客户端。
var oauthHTTPClient = &http.Client{Timeout: 10 * time.Second}

// oauthEndpointsFor 取平台端点（测试注入优先）。
func (s *Service) oauthEndpointsFor(provider string) (oauthEndpoints, bool) {
	if s.oauthEndpointOverride != nil {
		if ep, ok := s.oauthEndpointOverride[provider]; ok {
			return ep, true
		}
	}
	ep, ok := defaultOAuthEndpoints[provider]
	return ep, ok
}

// loadOAuthConfig 从设置分组 oauth 读取平台配置。
func (s *Service) loadOAuthConfig(ctx context.Context, provider string) oauthProviderConfig {
	var cfg oauthProviderConfig
	if s.settings == nil {
		return cfg
	}
	items, err := s.settings.List(ctx, "oauth")
	if err != nil {
		return cfg
	}
	prefix := "oauth_" + provider + "_"
	for _, item := range items {
		switch item.Key {
		case prefix + "enabled":
			cfg.Enabled = strings.EqualFold(strings.TrimSpace(item.Value), "true")
		case prefix + "client_id":
			cfg.ClientID = strings.TrimSpace(item.Value)
		case prefix + "client_secret":
			cfg.ClientSecret = strings.TrimSpace(item.Value)
		}
	}
	return cfg
}

// oauthRedirectURI 回调地址：以 site.api_base_url 为准（需与平台侧登记完全一致）。
func (s *Service) oauthRedirectURI(ctx context.Context, provider string) (string, error) {
	if s.settings == nil {
		return "", ErrOAuthNotConfigured
	}
	items, err := s.settings.List(ctx, "site")
	if err != nil {
		return "", err
	}
	base := ""
	for _, item := range items {
		if item.Key == "api_base_url" {
			base = strings.TrimSpace(item.Value)
		}
	}
	if base == "" {
		return "", ErrOAuthNotConfigured
	}
	return strings.TrimRight(base, "/") + "/api/v1/auth/oauth/" + provider + "/callback", nil
}

// newOAuthState 生成带时间戳的 HMAC 签名 state（防 CSRF）。
func newOAuthState() (string, error) {
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	payload := hex.EncodeToString(nonce) + "." + strconv.FormatInt(time.Now().Unix(), 10)
	mac := hmac.New(sha256.New, oauthSigningKey())
	mac.Write([]byte(payload))
	return payload + "." + hex.EncodeToString(mac.Sum(nil)), nil
}

// verifyOAuthState 校验 state 的签名与时效。
func verifyOAuthState(state string) bool {
	parts := strings.Split(state, ".")
	if len(parts) != 3 {
		return false
	}
	payload := parts[0] + "." + parts[1]
	mac := hmac.New(sha256.New, oauthSigningKey())
	mac.Write([]byte(payload))
	expected := hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(expected), []byte(parts[2])) {
		return false
	}
	ts, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return false
	}
	issued := time.Unix(ts, 0)
	now := time.Now()
	return !issued.After(now.Add(time.Minute)) && now.Sub(issued) <= oauthStateTTL
}

// OAuthAuthorizeURL 构造第三方授权页跳转地址。
func (s *Service) OAuthAuthorizeURL(ctx context.Context, provider string) (string, error) {
	ep, ok := s.oauthEndpointsFor(provider)
	if !ok {
		return "", ErrOAuthProviderUnknown
	}
	cfg := s.loadOAuthConfig(ctx, provider)
	if !cfg.Enabled || cfg.ClientID == "" || cfg.ClientSecret == "" {
		return "", ErrOAuthProviderDisabled
	}
	redirectURI, err := s.oauthRedirectURI(ctx, provider)
	if err != nil {
		return "", err
	}
	state, err := newOAuthState()
	if err != nil {
		return "", err
	}

	query := url.Values{}
	query.Set("client_id", cfg.ClientID)
	query.Set("redirect_uri", redirectURI)
	query.Set("state", state)
	query.Set("scope", ep.Scopes)
	if provider == OAuthProviderGoogle {
		query.Set("response_type", "code")
	}
	return ep.AuthURL + "?" + query.Encode(), nil
}

// OAuthLogin 处理第三方回调：换取 token → 拉取用户信息 → 绑定/建号 → 签发 JWT。
func (s *Service) OAuthLogin(ctx context.Context, provider, code, state string) (LoginResult, error) {
	logger := sdk.LoggerFromContext(ctx)

	ep, ok := s.oauthEndpointsFor(provider)
	if !ok {
		return LoginResult{}, ErrOAuthProviderUnknown
	}
	cfg := s.loadOAuthConfig(ctx, provider)
	if !cfg.Enabled || cfg.ClientID == "" || cfg.ClientSecret == "" {
		return LoginResult{}, ErrOAuthProviderDisabled
	}
	if code == "" || !verifyOAuthState(state) {
		return LoginResult{}, ErrOAuthStateInvalid
	}
	redirectURI, err := s.oauthRedirectURI(ctx, provider)
	if err != nil {
		return LoginResult{}, err
	}

	accessToken, err := s.oauthExchangeCode(ctx, ep, cfg, code, redirectURI)
	if err != nil {
		logger.Warn("oauth_code_exchange_failed", "provider", provider, sdk.LogFieldError, err)
		return LoginResult{}, ErrOAuthExchangeFailed
	}
	info, err := s.oauthFetchUserInfo(ctx, provider, ep, accessToken)
	if err != nil {
		logger.Warn("oauth_userinfo_failed", "provider", provider, sdk.LogFieldError, err)
		return LoginResult{}, ErrOAuthExchangeFailed
	}
	if info.ProviderUserID == "" {
		return LoginResult{}, ErrOAuthExchangeFailed
	}

	user, err := s.resolveOAuthUser(ctx, provider, info)
	if err != nil {
		return LoginResult{}, err
	}
	if user.Status != "active" {
		logger.Warn("oauth_login_rejected", sdk.LogFieldReason, "user_disabled", sdk.LogFieldUserID, user.ID)
		return LoginResult{}, ErrUserDisabled
	}

	token, err := s.jwtMgr.GenerateToken(user.ID, user.Role, user.Email)
	if err != nil {
		logger.Error("jwt_issue_failed", sdk.LogFieldUserID, user.ID, sdk.LogFieldError, err)
		return LoginResult{}, err
	}
	logger.Info("oauth_login_succeeded", "provider", provider, sdk.LogFieldUserID, user.ID)
	return LoginResult{Token: token, User: user}, nil
}

// resolveOAuthUser 三段式匹配：已绑定身份 → 已验证同邮箱老用户自动绑定 → 新建用户。
func (s *Service) resolveOAuthUser(ctx context.Context, provider string, info oauthUserInfo) (User, error) {
	logger := sdk.LoggerFromContext(ctx)

	user, err := s.repo.FindUserByIdentity(ctx, provider, info.ProviderUserID)
	if err == nil {
		return user, nil
	}
	if !IsUserMissing(err) {
		return User{}, err
	}

	// 第三方必须给出已验证邮箱，否则无法安全归属账号
	if info.Email == "" {
		return User{}, ErrOAuthEmailRequired
	}

	identity := IdentityInput{Provider: provider, ProviderUserID: info.ProviderUserID, Email: info.Email}

	existing, err := s.repo.FindByEmail(ctx, info.Email)
	if err == nil {
		if linkErr := s.repo.LinkIdentity(ctx, existing.ID, identity); linkErr != nil {
			logger.Error("oauth_identity_link_failed", sdk.LogFieldUserID, existing.ID, sdk.LogFieldError, linkErr)
			return User{}, linkErr
		}
		logger.Info("oauth_identity_linked", "provider", provider, sdk.LogFieldUserID, existing.ID)
		return existing, nil
	}
	if !IsUserMissing(err) {
		return User{}, err
	}

	// 新用户：遵循注册开关；密码置为随机值（仅第三方登录，可后续重置）
	if !s.isRegistrationEnabled(ctx) {
		return User{}, ErrRegistrationDisabled
	}
	randomPassword := make([]byte, 32)
	if _, err := rand.Read(randomPassword); err != nil {
		return User{}, err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(hex.EncodeToString(randomPassword)), bcrypt.DefaultCost)
	if err != nil {
		return User{}, err
	}
	defaultBalance, defaultConcurrency := s.getNewUserDefaults(ctx)
	created, err := s.repo.Create(ctx, CreateUserInput{
		Email:          info.Email,
		PasswordHash:   string(hash),
		Username:       info.Name,
		Role:           "user",
		Status:         "active",
		Balance:        defaultBalance,
		MaxConcurrency: defaultConcurrency,
	})
	if err != nil {
		logger.Error("oauth_user_create_failed", "provider", provider, sdk.LogFieldError, err)
		return User{}, err
	}
	if err := s.repo.LinkIdentity(ctx, created.ID, identity); err != nil {
		logger.Error("oauth_identity_link_failed", sdk.LogFieldUserID, created.ID, sdk.LogFieldError, err)
		return User{}, err
	}
	logger.Info("oauth_user_registered", "provider", provider, sdk.LogFieldUserID, created.ID)
	return created, nil
}

// oauthExchangeCode 授权码换 access token。
func (s *Service) oauthExchangeCode(ctx context.Context, ep oauthEndpoints, cfg oauthProviderConfig, code, redirectURI string) (string, error) {
	form := url.Values{}
	form.Set("client_id", cfg.ClientID)
	form.Set("client_secret", cfg.ClientSecret)
	form.Set("code", code)
	form.Set("redirect_uri", redirectURI)
	form.Set("grant_type", "authorization_code")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ep.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json") // GitHub 默认回 form 编码，声明 JSON

	body, err := doOAuthRequest(req)
	if err != nil {
		return "", err
	}
	var payload struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
		ErrorDesc   string `json:"error_description"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", fmt.Errorf("解析 token 响应失败: %w", err)
	}
	if payload.AccessToken == "" {
		return "", fmt.Errorf("token 交换失败: %s %s", payload.Error, payload.ErrorDesc)
	}
	return payload.AccessToken, nil
}

// oauthFetchUserInfo 拉取用户信息并归一化（含 GitHub 邮箱列表回退）。
func (s *Service) oauthFetchUserInfo(ctx context.Context, provider string, ep oauthEndpoints, accessToken string) (oauthUserInfo, error) {
	body, err := oauthGetJSON(ctx, ep.UserInfoURL, accessToken)
	if err != nil {
		return oauthUserInfo{}, err
	}

	switch provider {
	case OAuthProviderGoogle:
		var payload struct {
			Sub           string `json:"sub"`
			Email         string `json:"email"`
			EmailVerified bool   `json:"email_verified"`
			Name          string `json:"name"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			return oauthUserInfo{}, err
		}
		info := oauthUserInfo{ProviderUserID: payload.Sub, Name: payload.Name}
		if payload.EmailVerified {
			info.Email = strings.ToLower(strings.TrimSpace(payload.Email))
		}
		return info, nil

	case OAuthProviderGitHub:
		var payload struct {
			ID    int64  `json:"id"`
			Login string `json:"login"`
			Name  string `json:"name"`
			Email string `json:"email"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			return oauthUserInfo{}, err
		}
		info := oauthUserInfo{Name: payload.Name}
		if payload.ID > 0 {
			info.ProviderUserID = strconv.FormatInt(payload.ID, 10)
		}
		if info.Name == "" {
			info.Name = payload.Login
		}
		// 公开邮箱可能为空，从 emails 端点取已验证的 primary 邮箱
		if ep.EmailsURL != "" {
			if email := fetchGitHubVerifiedEmail(ctx, ep.EmailsURL, accessToken); email != "" {
				info.Email = email
			}
		}
		return info, nil
	}
	return oauthUserInfo{}, ErrOAuthProviderUnknown
}

// fetchGitHubVerifiedEmail 取 GitHub 已验证邮箱（primary 优先）。
func fetchGitHubVerifiedEmail(ctx context.Context, emailsURL, accessToken string) string {
	body, err := oauthGetJSON(ctx, emailsURL, accessToken)
	if err != nil {
		return ""
	}
	var emails []struct {
		Email    string `json:"email"`
		Primary  bool   `json:"primary"`
		Verified bool   `json:"verified"`
	}
	if err := json.Unmarshal(body, &emails); err != nil {
		return ""
	}
	fallback := ""
	for _, item := range emails {
		if !item.Verified || item.Email == "" {
			continue
		}
		normalized := strings.ToLower(strings.TrimSpace(item.Email))
		if item.Primary {
			return normalized
		}
		if fallback == "" {
			fallback = normalized
		}
	}
	return fallback
}

func oauthGetJSON(ctx context.Context, rawURL, accessToken string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")
	return doOAuthRequest(req)
}

func doOAuthRequest(req *http.Request) ([]byte, error) {
	resp, err := oauthHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("第三方接口返回 %d: %.200s", resp.StatusCode, string(body))
	}
	return body, nil
}
