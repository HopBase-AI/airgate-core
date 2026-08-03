package auth

import (
	"context"
	"time"
)

// User 认证域用户对象。
type User struct {
	ID              int
	Email           string
	Username        string
	DisplayBadge    string
	PasswordHash    string
	Balance         float64
	Role            string
	MaxConcurrency  int
	GroupRates      map[int64]float64
	AllowedGroupIDs []int64
	Status          string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// LoginInput 登录输入。
type LoginInput struct {
	Email    string
	Password string
}

// LoginByAPIKeyInput API Key 登录输入。
type LoginByAPIKeyInput struct {
	Key string
}

// LoginByAPIKeyResult API Key 登录结果。
type LoginByAPIKeyResult struct {
	Token      string
	APIKeyID   int
	APIKeyName string
	// API Key 维度字段（额度/已用/到期/倍率）
	QuotaUSD  float64
	UsedQuota float64
	Rate      float64
	ExpiresAt *time.Time
	Platform  string
}

// RegisterInput 注册输入。
type RegisterInput struct {
	Email      string
	Password   string
	Username   string
	VerifyCode string
	// SourceSite 注册来源站点 ID（ToC 落地页 ?site= 归因），可为空。
	SourceSite string
	// InviteCode 分销邀请码（?inv= 归因），可为空；非法/不存在的码静默忽略，不阻断注册。
	InviteCode string
}

// OAuthAttribution OAuth 登录发起时携带的注册归因；经 state 签名往返穿透
// （第三方授权页跳转会丢 query 参数，归因只能藏在 state 里）。
type OAuthAttribution struct {
	SourceSite string
	InviteCode string
	// ReturnOrigin 发起登录的前端源（scheme://host）。回调固定落在 api_base_url 域，
	// 当控制台与 api 不同域时（如 ToC：console.essevin.com 发起、api.essevin.com 回调），
	// 据此把用户跳回原域，否则登录态会落在回调域、回到原域显示未登录。经白名单校验后签进 state。
	ReturnOrigin string
}

// SendVerifyCodeInput 发送验证码输入。
type SendVerifyCodeInput struct {
	Email string
}

// AuthIdentity 表示当前登录身份。
type AuthIdentity struct {
	UserID   int
	Role     string
	Email    string
	APIKeyID int // >0 表示 API Key 登录
}

// LoginResult 登录/注册结果。
type LoginResult struct {
	Token     string
	User      User
	IsNewUser bool
}

// APIKeyLoginInfo API Key 登录验证后的基本信息。
type APIKeyLoginInfo struct {
	KeyID   int
	KeyName string
	UserID  int
}

// APIKeyBrief API Key 概要（额度/已用/到期/倍率）。
type APIKeyBrief struct {
	QuotaUSD  float64
	UsedQuota float64
	ExpiresAt *time.Time
	SellRate  float64
	GroupRate float64
	Platform  string
}

// CreateUserInput 创建用户输入。
type CreateUserInput struct {
	Email          string
	PasswordHash   string
	Username       string
	Role           string
	Status         string
	Balance        float64
	MaxConcurrency int
	// SignupSource 注册来源站点 ID，已经过 sanitizeSiteID 归一化。
	SignupSource string
	// InviterID 邀请人 user id（分销归因），nil 表示无邀请人；落库后终身不变。
	InviterID *int
}

// Setting 设置键值对（从设置服务透传）。
type Setting struct {
	Key   string
	Value string
}

// IdentityInput 第三方身份绑定输入。
type IdentityInput struct {
	Provider       string
	ProviderUserID string
	Email          string
}

// Repository 认证域仓储接口。
type Repository interface {
	FindByEmail(context.Context, string) (User, error)
	EmailExists(context.Context, string) (bool, error)
	Create(context.Context, CreateUserInput) (User, error)
	FindByID(context.Context, int, bool) (User, error)
	ValidateAPIKeySession(context.Context, int) (User, error)
	// ValidateAPIKeyForLogin 验证 API Key 用于 Web 登录（不要求绑定分组）。
	ValidateAPIKeyForLogin(ctx context.Context, key string) (APIKeyLoginInfo, error)
	// GetAPIKeyBrief 获取 API Key 概要信息（额度/已用/到期/倍率）。
	GetAPIKeyBrief(ctx context.Context, keyID int) (APIKeyBrief, error)
	// FindUserByIdentity 按第三方身份查用户；未绑定返回 ErrUserNotFound。
	FindUserByIdentity(ctx context.Context, provider, providerUserID string) (User, error)
	// LinkIdentity 绑定第三方身份到用户（同一身份重复绑定同一用户应幂等）。
	LinkIdentity(ctx context.Context, userID int, identity IdentityInput) error
	// FindUserIDByInviteCode 按邀请码查邀请人 ID（仅 active 用户）；未命中返回 ErrUserNotFound。
	FindUserIDByInviteCode(ctx context.Context, code string) (int, error)
}
