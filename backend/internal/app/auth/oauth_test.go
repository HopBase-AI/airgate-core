package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	corauth "github.com/DouDOU-start/airgate-core/internal/auth"
)

// newOAuthTestProvider 起一个伪第三方（token/userinfo/emails 三端点）。
func newOAuthTestProvider(t *testing.T, userinfo map[string]any, githubEmails []map[string]any) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method", http.StatusMethodNotAllowed)
			return
		}
		_ = r.ParseForm()
		if r.PostForm.Get("code") == "bad-code" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"bad code"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"test-access-token","token_type":"bearer"}`))
	})
	mux.HandleFunc("/userinfo", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-access-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(userinfo)
	})
	mux.HandleFunc("/emails", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(githubEmails)
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

// newOAuthTestService 组装带伪端点与配置的 Service。
func newOAuthTestService(repo authStubRepository, provider string, server *httptest.Server, settings map[string][]Setting) *Service {
	service := NewService(repo, corauth.NewJWTManager("secret", 24))
	if settings == nil {
		settings = map[string][]Setting{}
	}
	if _, ok := settings["oauth"]; !ok {
		settings["oauth"] = []Setting{
			{Key: "oauth_" + provider + "_enabled", Value: "true"},
			{Key: "oauth_" + provider + "_client_id", Value: "cid"},
			{Key: "oauth_" + provider + "_client_secret", Value: "secret"},
		}
	}
	if _, ok := settings["site"]; !ok {
		settings["site"] = []Setting{{Key: "api_base_url", Value: "https://api.example.com"}}
	}
	service.SetSettingsLister(&stubSettingsLister{data: settings})
	if server != nil {
		service.oauthEndpointOverride = map[string]oauthEndpoints{
			provider: {
				AuthURL:     server.URL + "/authorize",
				TokenURL:    server.URL + "/token",
				UserInfoURL: server.URL + "/userinfo",
				EmailsURL:   server.URL + "/emails",
				Scopes:      "test-scope",
			},
		}
	}
	return service
}

func validState(t *testing.T) string {
	t.Helper()
	state, err := newOAuthState(oauthStateAttrs{})
	if err != nil {
		t.Fatalf("newOAuthState() error = %v", err)
	}
	return state
}

func TestOAuthAuthorizeURL(t *testing.T) {
	cases := []struct {
		name     string
		provider string
		settings map[string][]Setting
		wantErr  error
		contains []string
	}{
		{
			name:     "google 正常生成授权链接",
			provider: OAuthProviderGoogle,
			contains: []string{"client_id=cid", "state=", "redirect_uri=", "response_type=code"},
		},
		{
			name:     "未启用平台被拒",
			provider: OAuthProviderGoogle,
			settings: map[string][]Setting{"oauth": {
				{Key: "oauth_google_enabled", Value: "false"},
				{Key: "oauth_google_client_id", Value: "cid"},
				{Key: "oauth_google_client_secret", Value: "secret"},
			}},
			wantErr: ErrOAuthProviderDisabled,
		},
		{
			name:     "缺 client_secret 视为未配置",
			provider: OAuthProviderGoogle,
			settings: map[string][]Setting{"oauth": {
				{Key: "oauth_google_enabled", Value: "true"},
				{Key: "oauth_google_client_id", Value: "cid"},
			}},
			wantErr: ErrOAuthProviderDisabled,
		},
		{
			name:     "未知平台被拒",
			provider: "wechat",
			wantErr:  ErrOAuthProviderUnknown,
		},
		{
			name:     "缺 api_base_url 报未配置",
			provider: OAuthProviderGoogle,
			settings: map[string][]Setting{"site": {}},
			wantErr:  ErrOAuthNotConfigured,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			service := newOAuthTestService(authStubRepository{}, OAuthProviderGoogle, nil, tc.settings)
			got, err := service.OAuthAuthorizeURL(t.Context(), tc.provider, OAuthAttribution{})
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("OAuthAuthorizeURL() error = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("OAuthAuthorizeURL() error = %v", err)
			}
			for _, fragment := range tc.contains {
				if !strings.Contains(got, fragment) {
					t.Fatalf("授权链接缺少 %q: %s", fragment, got)
				}
			}
		})
	}
}

func TestOAuthStateRoundTrip(t *testing.T) {
	state := validState(t)
	if _, ok := verifyOAuthState(state); !ok {
		t.Fatal("合法 state 应通过校验")
	}
	if _, ok := verifyOAuthState(state + "x"); ok {
		t.Fatal("被篡改的 state 应拒绝")
	}
	if _, ok := verifyOAuthState(""); ok {
		t.Fatal("空 state 应拒绝")
	}
	if _, ok := verifyOAuthState("a.b"); ok {
		t.Fatal("畸形 state 应拒绝")
	}
}

func TestOAuthStateCarriesAttribution(t *testing.T) {
	state, err := newOAuthState(oauthStateAttrs{SourceSite: "ink", InviteCode: "abcd2345"})
	if err != nil {
		t.Fatalf("newOAuthState() error = %v", err)
	}
	attrs, ok := verifyOAuthState(state)
	if !ok {
		t.Fatal("携带归因的 state 应通过校验")
	}
	if attrs.SourceSite != "ink" || attrs.InviteCode != "abcd2345" {
		t.Fatalf("归因载荷往返不一致: %+v", attrs)
	}
}

// 过期与未来时间戳的 state 一律拒绝（新旧格式同规则）。
func TestOAuthStateExpiryRejected(t *testing.T) {
	sign := func(payload string) string {
		mac := hmac.New(sha256.New, oauthSigningKey())
		mac.Write([]byte(payload))
		return payload + "." + hex.EncodeToString(mac.Sum(nil))
	}
	expired := sign("deadbeef." + strconv.FormatInt(time.Now().Add(-oauthStateTTL-time.Minute).Unix(), 10) + ".")
	if _, ok := verifyOAuthState(expired); ok {
		t.Fatal("超过 TTL 的 state 应拒绝")
	}
	future := sign("deadbeef." + strconv.FormatInt(time.Now().Add(2*time.Minute).Unix(), 10) + ".")
	if _, ok := verifyOAuthState(future); ok {
		t.Fatal("时间戳在未来的 state 应拒绝")
	}
}

// TestOAuthStateLegacyFormat 旧三段格式（nonce.ts.hmac）在发布过渡窗口内必须仍被接受。
func TestOAuthStateLegacyFormat(t *testing.T) {
	payload := "deadbeef." + strconv.FormatInt(time.Now().Unix(), 10)
	mac := hmac.New(sha256.New, oauthSigningKey())
	mac.Write([]byte(payload))
	legacy := payload + "." + hex.EncodeToString(mac.Sum(nil))

	attrs, ok := verifyOAuthState(legacy)
	if !ok {
		t.Fatal("旧格式 state 应通过校验（发布兼容窗口）")
	}
	if attrs != (oauthStateAttrs{}) {
		t.Fatalf("旧格式 state 不应带出归因: %+v", attrs)
	}
	if _, ok := verifyOAuthState(legacy + "x"); ok {
		t.Fatal("被篡改的旧格式 state 应拒绝")
	}
}

func TestOAuthLoginGoogleCreatesNewUser(t *testing.T) {
	var created *CreateUserInput
	var linked *IdentityInput
	repo := authStubRepository{
		create: func(input CreateUserInput) (User, error) {
			created = &input
			return User{ID: 7, Email: input.Email, Username: input.Username, Role: "user", Status: "active"}, nil
		},
		linkIdentity: func(userID int, identity IdentityInput) error {
			linked = &identity
			if userID != 7 {
				t.Fatalf("绑定到了错误的用户 %d", userID)
			}
			return nil
		},
	}
	server := newOAuthTestProvider(t, map[string]any{
		"sub": "g-123", "email": "New@Example.com", "email_verified": true, "name": "New User",
	}, nil)
	service := newOAuthTestService(repo, OAuthProviderGoogle, server, nil)

	result, err := service.OAuthLogin(t.Context(), OAuthProviderGoogle, "good-code", validState(t))
	if err != nil {
		t.Fatalf("OAuthLogin() error = %v", err)
	}
	if result.Token == "" {
		t.Fatal("应签发 JWT")
	}
	if created == nil || created.Email != "new@example.com" {
		t.Fatalf("新用户邮箱应小写归一化, got %+v", created)
	}
	if created.PasswordHash == "" {
		t.Fatal("新用户应设置随机密码哈希")
	}
	if linked == nil || linked.Provider != OAuthProviderGoogle || linked.ProviderUserID != "g-123" {
		t.Fatalf("身份绑定不正确: %+v", linked)
	}
}

// OAuth 建号必须落 state 携带的归因：来源站 → signup_source，邀请码 → inviter_id。
func TestOAuthLoginGoogleCreatesNewUserWithAttribution(t *testing.T) {
	var created *CreateUserInput
	repo := authStubRepository{
		create: func(input CreateUserInput) (User, error) {
			created = &input
			return User{ID: 7, Email: input.Email, Role: "user", Status: "active"}, nil
		},
		findUserIDByInviteCode: func(code string) (int, error) {
			if code != "abcd2345" {
				t.Fatalf("邀请码应归一化为小写后查询, got %q", code)
			}
			return 99, nil
		},
	}
	server := newOAuthTestProvider(t, map[string]any{
		"sub": "g-456", "email": "invited@example.com", "email_verified": true,
	}, nil)
	service := newOAuthTestService(repo, OAuthProviderGoogle, server, nil)

	state, err := newOAuthState(oauthStateAttrs{SourceSite: "ink", InviteCode: "ABCD2345"})
	if err != nil {
		t.Fatalf("newOAuthState() error = %v", err)
	}
	if _, err := service.OAuthLogin(t.Context(), OAuthProviderGoogle, "good-code", state); err != nil {
		t.Fatalf("OAuthLogin() error = %v", err)
	}
	if created == nil {
		t.Fatal("应创建新用户")
	}
	if created.SignupSource != "ink" {
		t.Fatalf("SignupSource = %q, want ink（OAuth 注册不能丢站点归因）", created.SignupSource)
	}
	if created.InviterID == nil || *created.InviterID != 99 {
		t.Fatalf("InviterID = %v, want 99（OAuth 注册不能丢邀请绑定）", created.InviterID)
	}
}

func TestOAuthLoginBindsExistingUserByEmail(t *testing.T) {
	var linkedUserID int
	repo := authStubRepository{
		findByEmail: func() (User, error) {
			return User{ID: 42, Email: "old@example.com", Role: "user", Status: "active"}, nil
		},
		linkIdentity: func(userID int, _ IdentityInput) error {
			linkedUserID = userID
			return nil
		},
		create: func(CreateUserInput) (User, error) {
			t.Fatal("同邮箱老用户不应新建账号")
			return User{}, nil
		},
	}
	server := newOAuthTestProvider(t, map[string]any{
		"sub": "g-456", "email": "old@example.com", "email_verified": true, "name": "Old",
	}, nil)
	service := newOAuthTestService(repo, OAuthProviderGoogle, server, nil)

	if _, err := service.OAuthLogin(t.Context(), OAuthProviderGoogle, "good-code", validState(t)); err != nil {
		t.Fatalf("OAuthLogin() error = %v", err)
	}
	if linkedUserID != 42 {
		t.Fatalf("应绑定到老用户 42, got %d", linkedUserID)
	}
}

func TestOAuthLoginExistingIdentitySkipsBinding(t *testing.T) {
	repo := authStubRepository{
		findUserByIdentity: func(provider, providerUserID string) (User, error) {
			if provider != OAuthProviderGitHub || providerUserID != "789" {
				t.Fatalf("身份查询参数错误: %s %s", provider, providerUserID)
			}
			return User{ID: 9, Email: "gh@example.com", Role: "user", Status: "active"}, nil
		},
		linkIdentity: func(int, IdentityInput) error {
			t.Fatal("已绑定身份不应重复绑定")
			return nil
		},
	}
	server := newOAuthTestProvider(t, map[string]any{
		"id": 789, "login": "octo", "name": "Octo",
	}, []map[string]any{{"email": "gh@example.com", "primary": true, "verified": true}})
	service := newOAuthTestService(repo, OAuthProviderGitHub, server, nil)

	result, err := service.OAuthLogin(t.Context(), OAuthProviderGitHub, "good-code", validState(t))
	if err != nil {
		t.Fatalf("OAuthLogin() error = %v", err)
	}
	if result.User.ID != 9 {
		t.Fatalf("应登录到绑定用户 9, got %d", result.User.ID)
	}
}

func TestOAuthLoginGitHubUsesVerifiedPrimaryEmail(t *testing.T) {
	var created *CreateUserInput
	repo := authStubRepository{
		create: func(input CreateUserInput) (User, error) {
			created = &input
			return User{ID: 3, Email: input.Email, Role: "user", Status: "active"}, nil
		},
	}
	server := newOAuthTestProvider(t, map[string]any{
		"id": 555, "login": "dev", "email": "",
	}, []map[string]any{
		{"email": "unverified@example.com", "primary": false, "verified": false},
		{"email": "backup@example.com", "primary": false, "verified": true},
		{"email": "Primary@Example.com", "primary": true, "verified": true},
	})
	service := newOAuthTestService(repo, OAuthProviderGitHub, server, nil)

	if _, err := service.OAuthLogin(t.Context(), OAuthProviderGitHub, "good-code", validState(t)); err != nil {
		t.Fatalf("OAuthLogin() error = %v", err)
	}
	if created == nil || created.Email != "primary@example.com" {
		t.Fatalf("应选用已验证的 primary 邮箱, got %+v", created)
	}
	if created.Username != "dev" {
		t.Fatalf("GitHub 无 name 时应回退 login, got %q", created.Username)
	}
}

func TestOAuthLoginRejections(t *testing.T) {
	activeGoogle := map[string]any{"sub": "g-1", "email": "x@example.com", "email_verified": true}

	cases := []struct {
		name     string
		repo     authStubRepository
		userinfo map[string]any
		settings map[string][]Setting
		code     string
		state    func(t *testing.T) string
		wantErr  error
	}{
		{
			name:     "state 非法",
			userinfo: activeGoogle,
			code:     "good-code",
			state:    func(t *testing.T) string { return "forged.state.value" },
			wantErr:  ErrOAuthStateInvalid,
		},
		{
			name:     "code 交换失败",
			userinfo: activeGoogle,
			code:     "bad-code",
			state:    validState,
			wantErr:  ErrOAuthExchangeFailed,
		},
		{
			name:     "邮箱未验证拒绝建号",
			userinfo: map[string]any{"sub": "g-2", "email": "x@example.com", "email_verified": false},
			code:     "good-code",
			state:    validState,
			wantErr:  ErrOAuthEmailRequired,
		},
		{
			name:     "注册关闭时新用户被拒",
			userinfo: activeGoogle,
			settings: map[string][]Setting{
				"registration": {{Key: "registration_enabled", Value: "false"}},
			},
			code:    "good-code",
			state:   validState,
			wantErr: ErrRegistrationDisabled,
		},
		{
			name: "禁用用户被拒",
			repo: authStubRepository{
				findUserByIdentity: func(string, string) (User, error) {
					return User{ID: 5, Status: "disabled"}, nil
				},
			},
			userinfo: activeGoogle,
			code:     "good-code",
			state:    validState,
			wantErr:  ErrUserDisabled,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := newOAuthTestProvider(t, tc.userinfo, nil)
			service := newOAuthTestService(tc.repo, OAuthProviderGoogle, server, tc.settings)
			_, err := service.OAuthLogin(t.Context(), OAuthProviderGoogle, tc.code, tc.state(t))
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("OAuthLogin() error = %v, want %v", err, tc.wantErr)
			}
		})
	}
}
