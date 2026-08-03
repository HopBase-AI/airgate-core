package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	corauth "github.com/DouDOU-start/airgate-core/internal/auth"
)

func TestRegisterRejectsDuplicateEmail(t *testing.T) {
	service := NewService(authStubRepository{
		emailExists: func() (bool, error) { return true, nil },
	}, corauth.NewJWTManager("secret", 24))

	_, err := service.Register(t.Context(), RegisterInput{
		Email:    "u@test.com",
		Password: "password123",
		Username: "u",
	})
	if !errors.Is(err, ErrEmailAlreadyExists) {
		t.Fatalf("Register() error = %v, want %v", err, ErrEmailAlreadyExists)
	}
}

func TestRegisterRejectsWhenDisabled(t *testing.T) {
	service := NewService(authStubRepository{}, corauth.NewJWTManager("secret", 24))
	// 注入返回"注册已关闭"的设置
	service.SetSettingsLister(&stubSettingsLister{
		data: map[string][]Setting{
			"registration": {{Key: "registration_enabled", Value: "false"}},
		},
	})

	_, err := service.Register(t.Context(), RegisterInput{
		Email:    "u@test.com",
		Password: "password123",
	})
	if !errors.Is(err, ErrRegistrationDisabled) {
		t.Fatalf("Register() error = %v, want %v", err, ErrRegistrationDisabled)
	}
}

// 邮箱注册携带邀请码：合法码绑定邀请人，非法/不存在的码静默忽略不阻断注册。
func TestRegisterBindsInviter(t *testing.T) {
	var created *CreateUserInput
	service := NewService(authStubRepository{
		create: func(input CreateUserInput) (User, error) {
			created = &input
			return User{ID: 1, Email: input.Email, Role: "user", Status: "active"}, nil
		},
		findUserIDByInviteCode: func(code string) (int, error) {
			if code == "abcd2345" {
				return 99, nil
			}
			return 0, ErrUserNotFound
		},
	}, corauth.NewJWTManager("secret", 24))

	// 大写输入应归一化后命中
	if _, err := service.Register(t.Context(), RegisterInput{
		Email: "u@test.com", Password: "password123", InviteCode: "ABCD2345",
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if created == nil || created.InviterID == nil || *created.InviterID != 99 {
		t.Fatalf("应绑定邀请人 99, got %+v", created)
	}
}

func TestRegisterIgnoresInvalidInviteCode(t *testing.T) {
	cases := []struct {
		name string
		code string
	}{
		{"非法格式", "!!bad code!!"},
		{"不存在的码", "zzzz9999"},
		{"空码", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var created *CreateUserInput
			service := NewService(authStubRepository{
				create: func(input CreateUserInput) (User, error) {
					created = &input
					return User{ID: 1, Email: input.Email, Role: "user", Status: "active"}, nil
				},
			}, corauth.NewJWTManager("secret", 24))
			if _, err := service.Register(t.Context(), RegisterInput{
				Email: "u@test.com", Password: "password123", InviteCode: tc.code,
			}); err != nil {
				t.Fatalf("邀请码问题不应阻断注册: %v", err)
			}
			if created == nil || created.InviterID != nil {
				t.Fatalf("不应绑定邀请人: %+v", created)
			}
		})
	}
}

// 邀请码查库遇基础设施错误：注册照常成功，只丢邀请绑定（归因绝不阻断注册主流程）。
func TestRegisterInviteLookupErrorDoesNotBlock(t *testing.T) {
	var created *CreateUserInput
	service := NewService(authStubRepository{
		create: func(input CreateUserInput) (User, error) {
			created = &input
			return User{ID: 1, Email: input.Email, Role: "user", Status: "active"}, nil
		},
		findUserIDByInviteCode: func(string) (int, error) {
			return 0, errors.New("db down")
		},
	}, corauth.NewJWTManager("secret", 24))

	if _, err := service.Register(t.Context(), RegisterInput{
		Email: "u@test.com", Password: "password123", InviteCode: "abcd2345",
	}); err != nil {
		t.Fatalf("查库失败不应阻断注册: %v", err)
	}
	if created == nil || created.InviterID != nil {
		t.Fatalf("查库失败应放弃绑定: %+v", created)
	}
}

func TestSanitizeInviteCode(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"ABCD2345", "abcd2345"},
		{"  abcd2345  ", "abcd2345"},
		{"abc", ""},               // 太短
		{"a2345678901234567", ""}, // 超 16 位
		{"abcd 2345", ""},         // 含空格
		{"abcd-2345", ""},         // 含符号
		{"", ""},
	}
	for _, tc := range cases {
		if got := sanitizeInviteCode(tc.in); got != tc.want {
			t.Errorf("sanitizeInviteCode(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// stubSettingsLister 设置桩实现。
type stubSettingsLister struct {
	data map[string][]Setting
}

func (s *stubSettingsLister) List(_ context.Context, group string) ([]Setting, error) {
	return s.data[group], nil
}

func TestLoginIssuesTokenForActiveUser(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("生成密码 hash 失败: %v", err)
	}
	jwtMgr := corauth.NewJWTManager("secret", 24)
	service := NewService(authStubRepository{
		findByEmail: func() (User, error) {
			return User{
				ID:           7,
				Email:        "u@test.com",
				PasswordHash: string(hash),
				Role:         "user",
				Status:       "active",
			}, nil
		},
	}, jwtMgr)

	result, err := service.Login(t.Context(), LoginInput{
		Email:    "u@test.com",
		Password: "password123",
	})
	if err != nil {
		t.Fatalf("登录失败: %v", err)
	}
	claims, err := jwtMgr.ParseToken(result.Token)
	if err != nil {
		t.Fatalf("解析登录 token 失败: %v", err)
	}
	if claims.UserID != 7 || result.User.Email != "u@test.com" {
		t.Fatalf("登录结果异常: user=%+v claims=%+v", result.User, claims)
	}
	if result.IsNewUser {
		t.Fatal("普通登录不应标记为新用户")
	}
}

func TestLoginRejectsDisabledUserAndWrongPassword(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("生成密码 hash 失败: %v", err)
	}
	tests := []struct {
		name     string
		user     User
		password string
		wantErr  error
	}{
		{
			name:     "disabled",
			user:     User{ID: 1, Email: "u@test.com", PasswordHash: string(hash), Role: "user", Status: "disabled"},
			password: "password123",
			wantErr:  ErrUserDisabled,
		},
		{
			name:     "wrong_password",
			user:     User{ID: 1, Email: "u@test.com", PasswordHash: string(hash), Role: "user", Status: "active"},
			password: "wrong",
			wantErr:  ErrInvalidCredentials,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := NewService(authStubRepository{
				findByEmail: func() (User, error) { return tt.user, nil },
			}, corauth.NewJWTManager("secret", 24))

			_, err := service.Login(t.Context(), LoginInput{Email: "u@test.com", Password: tt.password})
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("登录错误 = %v，期望 %v", err, tt.wantErr)
			}
		})
	}
}

func TestRegisterCreatesActiveUserAndToken(t *testing.T) {
	jwtMgr := corauth.NewJWTManager("secret", 24)
	var captured CreateUserInput
	service := NewService(authStubRepository{
		emailExists: func() (bool, error) { return false, nil },
		create: func(input CreateUserInput) (User, error) {
			captured = input
			return User{
				ID:           9,
				Email:        input.Email,
				Username:     input.Username,
				PasswordHash: input.PasswordHash,
				Role:         input.Role,
				Status:       input.Status,
			}, nil
		},
	}, jwtMgr)

	result, err := service.Register(t.Context(), RegisterInput{
		Email:    "new@test.com",
		Password: "password123",
		Username: "新用户",
	})
	if err != nil {
		t.Fatalf("注册失败: %v", err)
	}
	if captured.Role != "user" || captured.Status != "active" || captured.PasswordHash == "password123" {
		t.Fatalf("创建用户输入异常: %+v", captured)
	}
	// 默认并发数为 5（无设置时）
	if captured.MaxConcurrency != 5 {
		t.Fatalf("默认并发数异常: got %d, want 5", captured.MaxConcurrency)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(captured.PasswordHash), []byte("password123")); err != nil {
		t.Fatalf("密码 hash 无法校验: %v", err)
	}
	claims, err := jwtMgr.ParseToken(result.Token)
	if err != nil {
		t.Fatalf("解析注册 token 失败: %v", err)
	}
	if claims.UserID != 9 || result.User.Username != "新用户" {
		t.Fatalf("注册结果异常: user=%+v claims=%+v", result.User, claims)
	}
	if !result.IsNewUser {
		t.Fatal("注册结果应标记 IsNewUser=true")
	}
}

func TestRefreshTokenPreservesAPIKeyIdentity(t *testing.T) {
	jwtMgr := corauth.NewJWTManager("secret", 24)
	service := NewService(authStubRepository{}, jwtMgr)

	token, err := service.RefreshToken(t.Context(), AuthIdentity{
		UserID:   5,
		Role:     "admin",
		Email:    "u@test.com",
		APIKeyID: 13,
	})
	if err != nil {
		t.Fatalf("刷新 token 失败: %v", err)
	}
	claims, err := jwtMgr.ParseToken(token)
	if err != nil {
		t.Fatalf("解析刷新 token 失败: %v", err)
	}
	if claims.UserID != 0 || claims.Email != "" || claims.APIKeyID != 13 || claims.Role != corauth.APIKeySessionRole {
		t.Fatalf("刷新 claims 异常: %+v", claims)
	}
}

func TestRefreshTokenRejectsInvalidAPIKeySession(t *testing.T) {
	jwtMgr := corauth.NewJWTManager("secret", 24)
	service := NewService(authStubRepository{
		validateAPIKeySession: func(_ int) (User, error) {
			return User{}, ErrInvalidAPIKeySession
		},
	}, jwtMgr)

	_, err := service.RefreshToken(t.Context(), AuthIdentity{
		UserID:   5,
		Role:     "user",
		Email:    "u@test.com",
		APIKeyID: 13,
	})
	if !errors.Is(err, ErrInvalidAPIKeySession) {
		t.Fatalf("刷新错误 = %v，期望 %v", err, ErrInvalidAPIKeySession)
	}
}

func TestFindAndEmailDelegatesToRepository(t *testing.T) {
	service := NewService(authStubRepository{
		emailExists: func() (bool, error) { return true, nil },
		findByID: func() (User, error) {
			return User{ID: 3, Email: "u@test.com"}, nil
		},
	}, corauth.NewJWTManager("secret", 24))

	exists, err := service.EmailExists(t.Context(), "u@test.com")
	if err != nil || !exists {
		t.Fatalf("EmailExists = %v, %v，期望 true, nil", exists, err)
	}
	user, err := service.FindByID(t.Context(), 3)
	if err != nil || user.ID != 3 {
		t.Fatalf("FindByID = %+v, %v，期望用户 3", user, err)
	}
	if !IsUserMissing(ErrUserNotFound) {
		t.Fatal("ErrUserNotFound 应被识别为用户不存在")
	}
}

type authStubRepository struct {
	findByEmail            func() (User, error)
	emailExists            func() (bool, error)
	create                 func(CreateUserInput) (User, error)
	findByID               func() (User, error)
	validateAPIKeySession  func(keyID int) (User, error)
	validateAPIKeyForLogin func(key string) (APIKeyLoginInfo, error)
	getAPIKeyBrief         func(keyID int) (APIKeyBrief, error)
	findUserByIdentity     func(provider, providerUserID string) (User, error)
	linkIdentity           func(userID int, identity IdentityInput) error
	findUserIDByInviteCode func(code string) (int, error)
}

func (s authStubRepository) FindUserIDByInviteCode(_ context.Context, code string) (int, error) {
	if s.findUserIDByInviteCode == nil {
		return 0, ErrUserNotFound
	}
	return s.findUserIDByInviteCode(code)
}

func (s authStubRepository) FindUserByIdentity(_ context.Context, provider, providerUserID string) (User, error) {
	if s.findUserByIdentity == nil {
		return User{}, ErrUserNotFound
	}
	return s.findUserByIdentity(provider, providerUserID)
}

func (s authStubRepository) LinkIdentity(_ context.Context, userID int, identity IdentityInput) error {
	if s.linkIdentity == nil {
		return nil
	}
	return s.linkIdentity(userID, identity)
}

func (s authStubRepository) FindByEmail(_ context.Context, _ string) (User, error) {
	if s.findByEmail == nil {
		return User{}, ErrUserNotFound
	}
	return s.findByEmail()
}

func (s authStubRepository) EmailExists(_ context.Context, _ string) (bool, error) {
	if s.emailExists == nil {
		return false, nil
	}
	return s.emailExists()
}

func (s authStubRepository) Create(_ context.Context, input CreateUserInput) (User, error) {
	if s.create == nil {
		return User{
			ID:           1,
			Email:        input.Email,
			Username:     input.Username,
			PasswordHash: input.PasswordHash,
			Role:         input.Role,
			Status:       input.Status,
			CreatedAt:    time.Now(),
			UpdatedAt:    time.Now(),
		}, nil
	}
	return s.create(input)
}

func (s authStubRepository) FindByID(_ context.Context, _ int, _ bool) (User, error) {
	if s.findByID == nil {
		return User{}, ErrUserNotFound
	}
	return s.findByID()
}

func (s authStubRepository) ValidateAPIKeySession(_ context.Context, keyID int) (User, error) {
	if s.validateAPIKeySession != nil {
		return s.validateAPIKeySession(keyID)
	}
	if keyID <= 0 {
		return User{}, ErrInvalidAPIKeySession
	}
	return User{ID: 7, Email: "u@test.com", Role: "admin", Status: "active"}, nil
}

func (s authStubRepository) ValidateAPIKeyForLogin(_ context.Context, key string) (APIKeyLoginInfo, error) {
	if s.validateAPIKeyForLogin != nil {
		return s.validateAPIKeyForLogin(key)
	}
	return APIKeyLoginInfo{}, ErrInvalidAPIKey
}

func (s authStubRepository) GetAPIKeyBrief(_ context.Context, keyID int) (APIKeyBrief, error) {
	if s.getAPIKeyBrief != nil {
		return s.getAPIKeyBrief(keyID)
	}
	return APIKeyBrief{}, nil
}
