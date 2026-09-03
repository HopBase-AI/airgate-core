package auth

import "errors"

var (
	// ErrInvalidCredentials 用户名或密码错误。
	ErrInvalidCredentials = errors.New("邮箱或密码错误")
	// ErrUserDisabled 用户已禁用。
	ErrUserDisabled = errors.New("账户已禁用")
	// ErrEmailAlreadyExists 注册邮箱已存在。
	ErrEmailAlreadyExists = errors.New("邮箱已注册")
	// ErrUserNotFound 用户不存在。
	ErrUserNotFound = errors.New("用户不存在")
	// ErrInvalidAPIKeySession API Key 登录会话已失效。
	ErrInvalidAPIKeySession = errors.New("API Key 登录会话已失效")
	// ErrInvalidAPIKeyFormat API Key 格式无效。
	ErrInvalidAPIKeyFormat = errors.New("无效的 API Key 格式")
	// ErrInvalidAPIKey API Key 无效。
	ErrInvalidAPIKey = errors.New("无效的 API Key")
	// ErrAPIKeyExpired API Key 已过期。
	ErrAPIKeyExpired = errors.New("API Key 已过期")
	// ErrMemberDisabled API Key 所属团队成员已被主账号停用。
	ErrMemberDisabled = errors.New("所属团队成员已被停用")
	// ErrRegistrationDisabled 注册功能已关闭。
	ErrRegistrationDisabled = errors.New("注册功能已关闭")
	// ErrVerifyCodeRequired 需要验证码。
	ErrVerifyCodeRequired = errors.New("请输入验证码")
	// ErrVerifyCodeInvalid 验证码无效或已过期。
	ErrVerifyCodeInvalid = errors.New("验证码无效或已过期")
	// ErrMailerNotConfigured 邮件服务未配置。
	ErrMailerNotConfigured = errors.New("邮件服务未配置")
	// ErrSendMailFailed 发送邮件失败。
	ErrSendMailFailed = errors.New("发送邮件失败")
	// ErrOAuthProviderUnknown 不支持的第三方登录平台。
	ErrOAuthProviderUnknown = errors.New("不支持的第三方登录平台")
	// ErrOAuthProviderDisabled 第三方登录未启用或未配置。
	ErrOAuthProviderDisabled = errors.New("该第三方登录方式未启用")
	// ErrOAuthNotConfigured 站点 api_base_url 未配置，无法构造回调地址。
	ErrOAuthNotConfigured = errors.New("第三方登录未完成配置")
	// ErrOAuthStateInvalid state 校验失败（可能为 CSRF 或授权页停留超时）。
	ErrOAuthStateInvalid = errors.New("登录状态已失效，请重新发起登录")
	// ErrOAuthExchangeFailed 与第三方交换凭证失败。
	ErrOAuthExchangeFailed = errors.New("第三方登录验证失败，请稍后重试")
	// ErrOAuthEmailRequired 第三方账号缺少已验证邮箱。
	ErrOAuthEmailRequired = errors.New("第三方账号未提供已验证邮箱，无法登录")
)
