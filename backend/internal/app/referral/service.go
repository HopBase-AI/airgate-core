package referral

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	appuser "github.com/DouDOU-start/airgate-core/internal/app/user"
	"github.com/DouDOU-start/airgate-core/internal/pkg/pagination"

	sdk "github.com/DouDOU-start/airgate-sdk/sdkgo"
)

// Service 分销域应用服务。
type Service struct {
	repo     Repository
	balance  BalanceAdjuster
	settings SettingsReader
}

// NewService 创建分销服务。
func NewService(repo Repository, balance BalanceAdjuster, settings SettingsReader) *Service {
	return &Service{repo: repo, balance: balance, settings: settings}
}

// LoadConfig 读取分销配置（settings 表 referral 分组）；缺省/读失败按全零处理（功能关闭）。
func (s *Service) LoadConfig(ctx context.Context) Config {
	cfg := Config{}
	if s.settings == nil {
		return cfg
	}
	items, err := s.settings.List(ctx, "referral")
	if err != nil {
		return cfg
	}
	for _, item := range items {
		switch item.Key {
		case "referral_enabled":
			cfg.Enabled = item.Value == "true"
		case "referral_default_rate":
			cfg.DefaultRate = parseRate(item.Value)
		case "referral_first_bonus_rate":
			cfg.FirstBonusRate = parseRate(item.Value)
		case "referral_link_base_url":
			cfg.LinkBaseURL = strings.TrimSpace(item.Value)
		}
	}
	return cfg
}

// HandleTopup 处理一笔充值入账事件：给推广官按比例返利，被邀请人首充加赠。
//
// 幂等契约：余额入账靠 balance_logs 幂等键（"referral:"/"refbonus:" + 订单号），
// 流水靠 (out_trade_no, kind) 唯一索引——支付回调重试重放本方法安全。
//
// 失败语义：业务上不适用（功能关闭/无邀请人/比例为 0）一律返回 nil，绝不因分销
// 阻塞支付链路；只有基础设施错误才返回 error，让支付插件回调重试补账。
func (s *Service) HandleTopup(ctx context.Context, ev TopupEvent) error {
	logger := sdk.LoggerFromContext(ctx)
	if ev.UserID <= 0 || ev.OutTradeNo == "" || ev.PaidAmount <= 0 {
		return nil
	}
	cfg := s.LoadConfig(ctx)
	if !cfg.Enabled {
		return nil
	}

	invitee, err := s.repo.GetUserBrief(ctx, ev.UserID)
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			return nil
		}
		return err
	}
	if invitee.InviterID == nil {
		return nil
	}

	inviter, err := s.repo.GetUserBrief(ctx, *invitee.InviterID)
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			logger.Info("referral_rebate_skipped",
				sdk.LogFieldReason, "inviter_missing",
				sdk.LogFieldUserID, ev.UserID,
			)
			return nil
		}
		return err
	}

	// 推广官返利
	if inviter.Status != "active" {
		logger.Info("referral_rebate_skipped",
			sdk.LogFieldReason, "inviter_disabled",
			"inviter_id", inviter.ID,
			"out_trade_no", ev.OutTradeNo,
		)
	} else {
		rate := cfg.DefaultRate
		if inviter.ReferralRate != nil {
			rate = clampRate(*inviter.ReferralRate)
		}
		amount := round8(ev.PaidAmount * rate)
		if amount > 0 {
			if _, err := s.balance.AdjustBalance(ctx, inviter.ID, appuser.BalanceChange{
				Action:         "add",
				Amount:         amount,
				Remark:         fmt.Sprintf("分销返利（%s 充值 %.2f）", maskEmail(invitee.Email), ev.PaidAmount),
				IdempotencyKey: "referral:" + ev.OutTradeNo,
			}); err != nil {
				return fmt.Errorf("推广官返利入账失败: %w", err)
			}
			if err := s.repo.CreateCommission(ctx, CommissionCreate{
				InviterID:    inviter.ID,
				InviterEmail: inviter.Email,
				InviteeID:    invitee.ID,
				InviteeEmail: invitee.Email,
				OutTradeNo:   ev.OutTradeNo,
				Kind:         KindRebate,
				PaidAmount:   ev.PaidAmount,
				Rate:         rate,
				Amount:       amount,
			}); err != nil {
				return fmt.Errorf("返利流水落库失败: %w", err)
			}
			logger.Info("referral_rebate_settled",
				"inviter_id", inviter.ID,
				"invitee_id", invitee.ID,
				"out_trade_no", ev.OutTradeNo,
				"amount", amount,
				"rate", rate,
			)
		}
	}

	// 被邀请人首充加赠（与推广官状态无关，两者独立）
	if ev.FirstTopup && cfg.FirstBonusRate > 0 && invitee.Status == "active" {
		granted, err := s.repo.HasCommission(ctx, invitee.ID, KindFirstBonus)
		if err != nil {
			return err
		}
		if !granted {
			amount := round8(ev.PaidAmount * cfg.FirstBonusRate)
			if amount > 0 {
				if _, err := s.balance.AdjustBalance(ctx, invitee.ID, appuser.BalanceChange{
					Action:         "add",
					Amount:         amount,
					Remark:         "邀请首充加赠",
					IdempotencyKey: "refbonus:" + ev.OutTradeNo,
				}); err != nil {
					return fmt.Errorf("首充加赠入账失败: %w", err)
				}
				if err := s.repo.CreateCommission(ctx, CommissionCreate{
					InviterID:    inviter.ID,
					InviterEmail: inviter.Email,
					InviteeID:    invitee.ID,
					InviteeEmail: invitee.Email,
					OutTradeNo:   ev.OutTradeNo,
					Kind:         KindFirstBonus,
					PaidAmount:   ev.PaidAmount,
					Rate:         cfg.FirstBonusRate,
					Amount:       amount,
				}); err != nil {
					return fmt.Errorf("首充加赠流水落库失败: %w", err)
				}
				logger.Info("referral_first_bonus_settled",
					"invitee_id", invitee.ID,
					"out_trade_no", ev.OutTradeNo,
					"amount", amount,
				)
			}
		}
	}

	return nil
}

// MyReferral 用户侧「我的邀请」概览；邀请码惰性生成。
func (s *Service) MyReferral(ctx context.Context, userID int) (MyReferral, error) {
	cfg := s.LoadConfig(ctx)
	brief, err := s.repo.GetUserBrief(ctx, userID)
	if err != nil {
		return MyReferral{}, err
	}
	code := brief.InviteCode
	if code == "" {
		code, err = s.ensureInviteCode(ctx, userID)
		if err != nil {
			return MyReferral{}, err
		}
	}
	count, err := s.repo.CountInvitees(ctx, userID)
	if err != nil {
		return MyReferral{}, err
	}
	sums, err := s.repo.SumsByInviter(ctx, userID)
	if err != nil {
		return MyReferral{}, err
	}
	return MyReferral{
		InviteCode:    code,
		LinkBaseURL:   cfg.LinkBaseURL,
		Enabled:       cfg.Enabled,
		InviteeCount:  count,
		TotalRebate:   sums.TotalRebate,
		TotalReversed: sums.TotalReversed,
	}, nil
}

// MyCommissions 用户侧返利流水（仅本人作为推广官的记录）。
func (s *Service) MyCommissions(ctx context.Context, userID, page, pageSize int) (CommissionList, error) {
	page, pageSize = pagination.Normalize(page, pageSize)
	list, total, err := s.repo.ListCommissions(ctx, CommissionFilter{
		Page: page, PageSize: pageSize, InviterID: userID, Kind: KindRebate,
	})
	if err != nil {
		return CommissionList{}, err
	}
	return CommissionList{List: list, Total: total, Page: page, PageSize: pageSize}, nil
}

// AdminSummary 推广官汇总报表，按累计返利倒序（线下结算对账依据）。
func (s *Service) AdminSummary(ctx context.Context) ([]PromoterSummary, error) {
	items, err := s.repo.PromoterSummaries(ctx)
	if err != nil {
		return nil, err
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].TotalRebate != items[j].TotalRebate {
			return items[i].TotalRebate > items[j].TotalRebate
		}
		return items[i].InviteeCount > items[j].InviteeCount
	})
	return items, nil
}

// AdminCommissions 管理端全量流水。
func (s *Service) AdminCommissions(ctx context.Context, filter CommissionFilter) (CommissionList, error) {
	filter.Page, filter.PageSize = pagination.Normalize(filter.Page, filter.PageSize)
	list, total, err := s.repo.ListCommissions(ctx, filter)
	if err != nil {
		return CommissionList{}, err
	}
	return CommissionList{List: list, Total: total, Page: filter.Page, PageSize: filter.PageSize}, nil
}

// Reverse 回冲一条返利（退款/作弊场景人工处理）：受益人余额扣回 + 流水标记 reversed。
//
// 扣款幂等键 "referral_reverse:<id>"——扣款成功但标记失败的窗口内重试安全。
func (s *Service) Reverse(ctx context.Context, id int) (Commission, error) {
	logger := sdk.LoggerFromContext(ctx)
	item, err := s.repo.GetCommission(ctx, id)
	if err != nil {
		return Commission{}, err
	}
	if item.Status != StatusSettled {
		return Commission{}, ErrCommissionAlreadyReversed
	}
	beneficiary := item.InviterID
	if item.Kind == KindFirstBonus {
		beneficiary = item.InviteeID
	}
	if _, err := s.balance.AdjustBalance(ctx, beneficiary, appuser.BalanceChange{
		Action:         "subtract",
		Amount:         item.Amount,
		Remark:         fmt.Sprintf("分销返利回冲（订单 %s）", item.OutTradeNo),
		IdempotencyKey: fmt.Sprintf("referral_reverse:%d", item.ID),
	}); err != nil {
		return Commission{}, err
	}
	if err := s.repo.MarkReversed(ctx, id); err != nil {
		return Commission{}, err
	}
	logger.Info("referral_commission_reversed",
		"commission_id", id,
		"beneficiary_id", beneficiary,
		"amount", item.Amount,
	)
	return s.repo.GetCommission(ctx, id)
}

// SetUserReferralRate 设置/清除用户级返利比例覆盖（nil = 清除，回落全局默认）。
func (s *Service) SetUserReferralRate(ctx context.Context, userID int, rate *float64) error {
	if rate != nil && (*rate < 0 || *rate > 1) {
		return ErrInvalidRate
	}
	if err := s.repo.SetUserReferralRate(ctx, userID, rate); err != nil {
		return err
	}
	sdk.LoggerFromContext(ctx).Info("referral_rate_updated",
		sdk.LogFieldUserID, userID,
		"rate", rate,
	)
	return nil
}

// ensureInviteCode 惰性生成邀请码；唯一冲突重试，并发下他人先设置则复用已有码。
func (s *Service) ensureInviteCode(ctx context.Context, userID int) (string, error) {
	for i := 0; i < 5; i++ {
		code, err := generateInviteCode()
		if err != nil {
			return "", err
		}
		got, err := s.repo.ClaimInviteCode(ctx, userID, code)
		if err == nil {
			return got, nil
		}
		if !errors.Is(err, ErrInviteCodeTaken) {
			return "", err
		}
	}
	return "", errors.New("邀请码生成冲突重试耗尽")
}

// inviteCodeAlphabet 去除易混字符（0/o/1/l/i）的字母表。
const inviteCodeAlphabet = "abcdefghjkmnpqrstuvwxyz23456789"
const inviteCodeLength = 8

func generateInviteCode() (string, error) {
	buf := make([]byte, inviteCodeLength)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	out := make([]byte, inviteCodeLength)
	for i, b := range buf {
		out[i] = inviteCodeAlphabet[int(b)%len(inviteCodeAlphabet)]
	}
	return string(out), nil
}

// parseRate 解析比例配置；非法或越界（<0 / >1）一律按 0 处理——配置错误宁可不返，不能超发。
func parseRate(raw string) float64 {
	v, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil || math.IsNaN(v) {
		return 0
	}
	return clampRate(v)
}

// clampRate 越界比例（<0 / >1）按 0 处理。
func clampRate(v float64) float64 {
	if v < 0 || v > 1 {
		return 0
	}
	return v
}

// round8 金额保留 8 位小数，与余额 decimal(20,8) 精度对齐。
func round8(v float64) float64 {
	return math.Round(v*1e8) / 1e8
}

// maskEmail 邮箱脱敏：保留首字符与域名（写进推广官余额流水备注，不暴露被邀请人完整邮箱）。
func maskEmail(email string) string {
	at := strings.Index(email, "@")
	if at <= 0 {
		return "***"
	}
	return email[:1] + "***" + email[at:]
}
