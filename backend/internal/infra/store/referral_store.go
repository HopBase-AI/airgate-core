package store

import (
	"context"
	"time"

	"github.com/DouDOU-start/airgate-core/ent"
	entbalancelog "github.com/DouDOU-start/airgate-core/ent/balancelog"
	entreferral "github.com/DouDOU-start/airgate-core/ent/referralcommission"
	entuser "github.com/DouDOU-start/airgate-core/ent/user"
	appreferral "github.com/DouDOU-start/airgate-core/internal/app/referral"
)

// ReferralStore 使用 Ent 实现分销域仓储。
type ReferralStore struct {
	db *ent.Client
}

// NewReferralStore 创建分销仓储。
func NewReferralStore(db *ent.Client) *ReferralStore {
	return &ReferralStore{db: db}
}

// GetUserBrief 查询分销视角的用户概要。
func (s *ReferralStore) GetUserBrief(ctx context.Context, id int) (appreferral.UserBrief, error) {
	item, err := s.db.User.Query().Where(entuser.IDEQ(id)).Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return appreferral.UserBrief{}, appreferral.ErrUserNotFound
		}
		return appreferral.UserBrief{}, err
	}
	return mapUserBrief(item), nil
}

// ClaimInviteCode 仅当用户尚无邀请码时设置（WHERE invite_code IS NULL 防并发覆盖）。
func (s *ReferralStore) ClaimInviteCode(ctx context.Context, userID int, code string) (string, error) {
	n, err := s.db.User.Update().
		Where(entuser.IDEQ(userID), entuser.InviteCodeIsNil()).
		SetInviteCode(code).
		Save(ctx)
	if err != nil {
		if ent.IsConstraintError(err) {
			return "", appreferral.ErrInviteCodeTaken
		}
		return "", err
	}
	if n == 0 {
		// 本人已有码（先前生成或并发生成），读回复用
		brief, err := s.GetUserBrief(ctx, userID)
		if err != nil {
			return "", err
		}
		if brief.InviteCode == "" {
			return "", appreferral.ErrInviteCodeTaken
		}
		return brief.InviteCode, nil
	}
	return code, nil
}

// CountInvitees 统计邀请注册人数。
func (s *ReferralStore) CountInvitees(ctx context.Context, inviterID int) (int, error) {
	return s.db.User.Query().Where(entuser.InviterIDEQ(inviterID)).Count(ctx)
}

// CreateCommission 落返利流水；(out_trade_no, kind) 唯一冲突（回调重试重放）静默幂等。
func (s *ReferralStore) CreateCommission(ctx context.Context, input appreferral.CommissionCreate) error {
	err := s.db.ReferralCommission.Create().
		SetInviterID(input.InviterID).
		SetInviterEmail(input.InviterEmail).
		SetInviteeID(input.InviteeID).
		SetInviteeEmail(input.InviteeEmail).
		SetOutTradeNo(input.OutTradeNo).
		SetKind(entreferral.Kind(input.Kind)).
		SetPaidAmount(input.PaidAmount).
		SetRate(input.Rate).
		SetAmount(input.Amount).
		Exec(ctx)
	if err != nil && ent.IsConstraintError(err) {
		return nil
	}
	return err
}

// HasCommission 该被邀请人是否已存在指定类型的流水（首充加赠防重）。
func (s *ReferralStore) HasCommission(ctx context.Context, inviteeID int, kind string) (bool, error) {
	return s.db.ReferralCommission.Query().
		Where(
			entreferral.InviteeIDEQ(inviteeID),
			entreferral.KindEQ(entreferral.Kind(kind)),
		).
		Exist(ctx)
}

// SumsByInviter 单个推广官的返利合计（settled / reversed 分桶）。
func (s *ReferralStore) SumsByInviter(ctx context.Context, inviterID int) (appreferral.InviterSums, error) {
	var rows []struct {
		Status string  `json:"status"`
		Sum    float64 `json:"sum"`
	}
	err := s.db.ReferralCommission.Query().
		Where(
			entreferral.InviterIDEQ(inviterID),
			entreferral.KindEQ(entreferral.KindRebate),
		).
		GroupBy(entreferral.FieldStatus).
		Aggregate(ent.As(ent.Sum(entreferral.FieldAmount), "sum")).
		Scan(ctx, &rows)
	if err != nil {
		return appreferral.InviterSums{}, err
	}
	sums := appreferral.InviterSums{}
	for _, r := range rows {
		switch r.Status {
		case appreferral.StatusSettled:
			sums.TotalRebate = r.Sum
		case appreferral.StatusReversed:
			sums.TotalReversed = r.Sum
		}
	}
	return sums, nil
}

// ListCommissions 流水分页查询。
func (s *ReferralStore) ListCommissions(ctx context.Context, filter appreferral.CommissionFilter) ([]appreferral.Commission, int64, error) {
	query := s.db.ReferralCommission.Query()
	if filter.InviterID > 0 {
		query = query.Where(entreferral.InviterIDEQ(filter.InviterID))
	}
	if filter.InviteeID > 0 {
		query = query.Where(entreferral.InviteeIDEQ(filter.InviteeID))
	}
	if filter.Kind != "" {
		query = query.Where(entreferral.KindEQ(entreferral.Kind(filter.Kind)))
	}
	if filter.Status != "" {
		query = query.Where(entreferral.StatusEQ(entreferral.Status(filter.Status)))
	}
	total, err := query.Clone().Count(ctx)
	if err != nil {
		return nil, 0, err
	}
	items, err := query.
		Order(ent.Desc(entreferral.FieldCreatedAt)).
		Offset((filter.Page - 1) * filter.PageSize).
		Limit(filter.PageSize).
		All(ctx)
	if err != nil {
		return nil, 0, err
	}
	result := make([]appreferral.Commission, 0, len(items))
	for _, item := range items {
		result = append(result, mapCommission(item))
	}
	return result, int64(total), nil
}

// GetCommission 按 ID 查询流水。
func (s *ReferralStore) GetCommission(ctx context.Context, id int) (appreferral.Commission, error) {
	item, err := s.db.ReferralCommission.Query().Where(entreferral.IDEQ(id)).Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return appreferral.Commission{}, appreferral.ErrCommissionNotFound
		}
		return appreferral.Commission{}, err
	}
	return mapCommission(item), nil
}

// MarkReversed 标记回冲（WHERE status='settled' 防并发重复回冲）。
func (s *ReferralStore) MarkReversed(ctx context.Context, id int) error {
	n, err := s.db.ReferralCommission.Update().
		Where(
			entreferral.IDEQ(id),
			entreferral.StatusEQ(entreferral.StatusSettled),
		).
		SetStatus(entreferral.StatusReversed).
		SetReversedAt(time.Now()).
		Save(ctx)
	if err != nil {
		return err
	}
	if n == 0 {
		return appreferral.ErrCommissionAlreadyReversed
	}
	return nil
}

// PromoterSummaries 推广官汇总：以「有被邀请人的用户」为驱动集，聚合返利合计。
// 有注册但尚无充值的推广官也会出现在报表里（返利为 0）。
func (s *ReferralStore) PromoterSummaries(ctx context.Context) ([]appreferral.PromoterSummary, error) {
	var inviteeRows []struct {
		InviterID int `json:"inviter_id"`
		Count     int `json:"count"`
	}
	if err := s.db.User.Query().
		Where(entuser.InviterIDNotNil()).
		GroupBy(entuser.FieldInviterID).
		Aggregate(ent.Count()).
		Scan(ctx, &inviteeRows); err != nil {
		return nil, err
	}
	if len(inviteeRows) == 0 {
		return []appreferral.PromoterSummary{}, nil
	}

	ids := make([]int, 0, len(inviteeRows))
	counts := make(map[int]int, len(inviteeRows))
	for _, r := range inviteeRows {
		ids = append(ids, r.InviterID)
		counts[r.InviterID] = r.Count
	}

	var sumRows []struct {
		InviterID int     `json:"inviter_id"`
		Kind      string  `json:"kind"`
		Status    string  `json:"status"`
		Sum       float64 `json:"sum"`
	}
	if err := s.db.ReferralCommission.Query().
		Where(entreferral.InviterIDIn(ids...)).
		GroupBy(entreferral.FieldInviterID, entreferral.FieldKind, entreferral.FieldStatus).
		Aggregate(ent.As(ent.Sum(entreferral.FieldAmount), "sum")).
		Scan(ctx, &sumRows); err != nil {
		return nil, err
	}

	users, err := s.db.User.Query().Where(entuser.IDIn(ids...)).All(ctx)
	if err != nil {
		return nil, err
	}
	userByID := make(map[int]*ent.User, len(users))
	for _, u := range users {
		userByID[u.ID] = u
	}

	result := make([]appreferral.PromoterSummary, 0, len(ids))
	byID := make(map[int]*appreferral.PromoterSummary, len(ids))
	for _, id := range ids {
		summary := appreferral.PromoterSummary{
			UserID:       id,
			InviteeCount: counts[id],
			Email:        "(已删除)",
		}
		if u, ok := userByID[id]; ok {
			summary.Email = u.Email
			summary.Username = u.Username
			summary.ReferralRate = u.ReferralRate
			summary.Tier = string(u.ReferralTier)
			summary.DisplayName = u.ReferralDisplayName
			if u.InviteCode != nil {
				summary.InviteCode = *u.InviteCode
			}
		}
		result = append(result, summary)
		byID[id] = &result[len(result)-1]
	}
	for _, r := range sumRows {
		summary, ok := byID[r.InviterID]
		if !ok {
			continue
		}
		switch {
		case r.Kind == appreferral.KindRebate && r.Status == appreferral.StatusSettled:
			summary.TotalRebate = r.Sum
		case r.Kind == appreferral.KindRebate && r.Status == appreferral.StatusReversed:
			summary.TotalReversed = r.Sum
		case r.Kind == appreferral.KindFirstBonus && r.Status == appreferral.StatusSettled:
			summary.FirstBonusTotal = r.Sum
		}
	}
	return result, nil
}

// BalanceChangeApplied 指定幂等键的余额变更是否已入账。
func (s *ReferralStore) BalanceChangeApplied(ctx context.Context, idempotencyKey string) (bool, error) {
	return s.db.BalanceLog.Query().
		Where(entbalancelog.IdempotencyKeyEQ(idempotencyKey)).
		Exist(ctx)
}

// SetUserReferralRate 设置/清除用户级返利比例覆盖。
func (s *ReferralStore) SetUserReferralRate(ctx context.Context, userID int, rate *float64) error {
	update := s.db.User.UpdateOneID(userID)
	if rate == nil {
		update = update.ClearReferralRate()
	} else {
		update = update.SetReferralRate(*rate)
	}
	if err := update.Exec(ctx); err != nil {
		if ent.IsNotFound(err) {
			return appreferral.ErrUserNotFound
		}
		return err
	}
	return nil
}

// GetPromoterByCode 按邀请码查一个有效（active）推广人概要；无匹配返回 ErrUserNotFound。
func (s *ReferralStore) GetPromoterByCode(ctx context.Context, code string) (appreferral.UserBrief, error) {
	item, err := s.db.User.Query().
		Where(
			entuser.InviteCodeEQ(code),
			entuser.StatusEQ(entuser.StatusActive),
		).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return appreferral.UserBrief{}, appreferral.ErrUserNotFound
		}
		return appreferral.UserBrief{}, err
	}
	return mapUserBrief(item), nil
}

// AdminSetInviteCode 管理端覆盖设置邀请码（官方 vanity 码），即便已有码也改写。
func (s *ReferralStore) AdminSetInviteCode(ctx context.Context, userID int, code string) error {
	err := s.db.User.UpdateOneID(userID).SetInviteCode(code).Exec(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return appreferral.ErrUserNotFound
		}
		if ent.IsConstraintError(err) {
			return appreferral.ErrInviteCodeTaken
		}
		return err
	}
	return nil
}

// SetPromoterIdentity 设置推广身份层级与官方署名（不动邀请码/比例/返佣）。
func (s *ReferralStore) SetPromoterIdentity(ctx context.Context, userID int, tier, displayName string) error {
	err := s.db.User.UpdateOneID(userID).
		SetReferralTier(entuser.ReferralTier(tier)).
		SetReferralDisplayName(displayName).
		Exec(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return appreferral.ErrUserNotFound
		}
		return err
	}
	return nil
}

func mapUserBrief(item *ent.User) appreferral.UserBrief {
	brief := appreferral.UserBrief{
		ID:           item.ID,
		Email:        item.Email,
		Username:     item.Username,
		Status:       string(item.Status),
		InviterID:    item.InviterID,
		ReferralRate: item.ReferralRate,
		Tier:         string(item.ReferralTier),
		DisplayName:  item.ReferralDisplayName,
	}
	if item.InviteCode != nil {
		brief.InviteCode = *item.InviteCode
	}
	return brief
}

func mapCommission(item *ent.ReferralCommission) appreferral.Commission {
	return appreferral.Commission{
		ID:           item.ID,
		InviterID:    item.InviterID,
		InviterEmail: item.InviterEmail,
		InviteeID:    item.InviteeID,
		InviteeEmail: item.InviteeEmail,
		OutTradeNo:   item.OutTradeNo,
		Kind:         string(item.Kind),
		PaidAmount:   item.PaidAmount,
		Rate:         item.Rate,
		Amount:       item.Amount,
		Status:       string(item.Status),
		CreatedAt:    item.CreatedAt,
		ReversedAt:   item.ReversedAt,
	}
}
