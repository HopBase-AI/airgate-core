package dto

// LoginReq 登录请求
type LoginReq struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required,min=6"`
}

// APIKeyLoginReq API Key 登录请求
type APIKeyLoginReq struct {
	Key string `json:"key" binding:"required"`
}

// LoginResp 登录响应
type LoginResp struct {
	Token      string   `json:"token"`
	User       UserResp `json:"user"`
	APIKeyID   int64    `json:"api_key_id,omitempty"`
	APIKeyName string   `json:"api_key_name,omitempty"`
}

// RegisterReq 注册请求
type RegisterReq struct {
	Email      string `json:"email" binding:"required,email"`
	Password   string `json:"password" binding:"required,min=6"`
	Username   string `json:"username"`
	VerifyCode string `json:"verify_code"`
	// SourceSite 注册来源站点 ID（ToC 落地页 ?site= 归因），可选。
	SourceSite string `json:"source_site" binding:"omitempty,max=64"`
	// InviteCode 分销邀请码（?inv= 归因），可选；非法码静默忽略。
	InviteCode string `json:"invite_code" binding:"omitempty,max=16"`
}

// SendVerifyCodeReq 发送验证码请求
type SendVerifyCodeReq struct {
	Email string `json:"email" binding:"required,email"`
}

type VerifyCodeReq struct {
	Email string `json:"email" binding:"required,email"`
	Code  string `json:"code" binding:"required"`
}

// RefreshResp Token 刷新响应
type RefreshResp struct {
	Token string `json:"token"`
}
