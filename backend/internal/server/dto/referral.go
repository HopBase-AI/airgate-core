package dto

import "time"

// MyReferralResp 用户侧「我的邀请」概览。
type MyReferralResp struct {
	InviteCode string `json:"invite_code"`
	// LinkBaseURL 邀请链接前缀（后台可配）；空 = 前端用当前控制台域名拼接。
	LinkBaseURL string `json:"link_base_url"`
	Enabled     bool   `json:"enabled"`
	// ReferralRate 当前用户有效返利比例（0~1）：用户级覆盖 else 全局默认。
	ReferralRate  float64 `json:"referral_rate"`
	InviteeCount  int     `json:"invitee_count"`
	TotalRebate   float64 `json:"total_rebate"`
	TotalReversed float64 `json:"total_reversed"`
}

// MyReferralCommissionResp 用户侧返利流水（被邀请人邮箱已脱敏，不含订单号）。
type MyReferralCommissionResp struct {
	ID           int        `json:"id"`
	InviteeEmail string     `json:"invitee_email"`
	PaidAmount   float64    `json:"paid_amount"`
	Rate         float64    `json:"rate"`
	Amount       float64    `json:"amount"`
	Status       string     `json:"status"`
	CreatedAt    time.Time  `json:"created_at"`
	ReversedAt   *time.Time `json:"reversed_at,omitempty"`
}

// ReferralCommissionResp 管理端返利流水（完整字段）。
type ReferralCommissionResp struct {
	ID           int        `json:"id"`
	InviterID    int        `json:"inviter_id"`
	InviterEmail string     `json:"inviter_email"`
	InviteeID    int        `json:"invitee_id"`
	InviteeEmail string     `json:"invitee_email"`
	OutTradeNo   string     `json:"out_trade_no"`
	Kind         string     `json:"kind"`
	PaidAmount   float64    `json:"paid_amount"`
	Rate         float64    `json:"rate"`
	Amount       float64    `json:"amount"`
	Status       string     `json:"status"`
	CreatedAt    time.Time  `json:"created_at"`
	ReversedAt   *time.Time `json:"reversed_at,omitempty"`
}

// ReferralPromoterResp 推广官汇总行（管理端对账报表）。
type ReferralPromoterResp struct {
	UserID          int      `json:"user_id"`
	Email           string   `json:"email"`
	Username        string   `json:"username"`
	ReferralRate    *float64 `json:"referral_rate"`
	InviteeCount    int      `json:"invitee_count"`
	TotalRebate     float64  `json:"total_rebate"`
	TotalReversed   float64  `json:"total_reversed"`
	FirstBonusTotal float64  `json:"first_bonus_total"`
}

// ReferralCommissionListReq 管理端流水筛选。
type ReferralCommissionListReq struct {
	PageReq
	InviterID int    `form:"inviter_id" binding:"omitempty,min=1"`
	InviteeID int    `form:"invitee_id" binding:"omitempty,min=1"`
	Kind      string `form:"kind" binding:"omitempty,oneof=rebate first_bonus"`
	Status    string `form:"status" binding:"omitempty,oneof=settled reversed"`
}

// SetReferralRateReq 设置用户级返利比例覆盖；rate 传 null 表示清除覆盖（回落全局默认）。
type SetReferralRateReq struct {
	Rate *float64 `json:"rate" binding:"omitempty,gte=0,lte=1"`
}
