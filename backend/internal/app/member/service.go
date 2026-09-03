package member

import (
	"context"
	"strings"
	"time"

	"github.com/DouDOU-start/airgate-core/internal/auth"
	"github.com/DouDOU-start/airgate-core/internal/pkg/pagination"
	"github.com/DouDOU-start/airgate-core/internal/pkg/period"
	"github.com/DouDOU-start/airgate-core/internal/pkg/timezone"
	sdk "github.com/DouDOU-start/airgate-sdk/sdkgo"
)

// Service 团队成员应用服务：主账号侧的成员管理。
//
// 成员改额度 / 停用 / 删除后要立刻反映到转发闸门，而鉴权结果有 5s 缓存，
// 因此写操作完成后会按成员名下 key 的 hash 逐个失效缓存；失效失败不影响写入结果
// （最多 apiKeyCacheTTL 后自然生效）。
type Service struct {
	repo Repository
	now  func() time.Time
}

// NewService 创建成员服务。
func NewService(repo Repository) *Service {
	return &Service{repo: repo, now: time.Now}
}

// List 查询当前用户名下的成员，并附带本期已用 / 密钥数 / 今日与近 30 天成本。
// tz 决定"今日"的起点；为空时回退到服务器本地时区。
func (s *Service) List(ctx context.Context, ownerID int, filter ListFilter, tz string) (ListResult, error) {
	logger := sdk.LoggerFromContext(ctx)
	page, pageSize := pagination.Normalize(filter.Page, filter.PageSize)
	filter.Page = page
	filter.PageSize = pageSize

	list, total, err := s.repo.ListByOwner(ctx, ownerID, filter)
	if err != nil {
		logger.Error("member_lookup_failed", sdk.LogFieldUserID, ownerID, sdk.LogFieldReason, "list", sdk.LogFieldError, err)
		return ListResult{}, err
	}

	ids := make([]int, 0, len(list))
	for _, item := range list {
		ids = append(ids, item.ID)
	}
	keyCounts, err := s.repo.KeyCounts(ctx, ids)
	if err != nil {
		logger.Error("member_lookup_failed", sdk.LogFieldUserID, ownerID, sdk.LogFieldReason, "key_counts", sdk.LogFieldError, err)
		return ListResult{}, err
	}
	loc := timezone.Resolve(tz)
	todayStart := timezone.StartOfDay(s.now().In(loc))
	todayMap, thirtyDayMap, err := s.repo.MemberUsage(ctx, ids, todayStart)
	if err != nil {
		logger.Error("member_lookup_failed", sdk.LogFieldUserID, ownerID, sdk.LogFieldReason, "usage", sdk.LogFieldError, err)
		return ListResult{}, err
	}

	now := s.now()
	for i := range list {
		Decorate(&list[i], now)
		list[i].KeyCount = keyCounts[list[i].ID]
		list[i].TodayCost = todayMap[list[i].ID]
		list[i].ThirtyDayCost = thirtyDayMap[list[i].ID]
	}
	return ListResult{List: list, Total: total, Page: page, PageSize: pageSize}, nil
}

// Get 查询当前用户名下的单个成员（含派生字段，不含成本聚合）。
func (s *Service) Get(ctx context.Context, ownerID, id int) (Member, error) {
	item, err := s.repo.FindOwned(ctx, ownerID, id)
	if err != nil {
		return Member{}, err
	}
	Decorate(&item, s.now())
	return item, nil
}

// Create 创建成员。额度周期默认 monthly，锚点取创建时刻。
func (s *Service) Create(ctx context.Context, ownerID int, input CreateInput) (Member, error) {
	logger := sdk.LoggerFromContext(ctx)
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return Member{}, ErrNameRequired
	}
	if input.QuotaUSD < 0 {
		return Member{}, ErrInvalidQuota
	}
	quotaPeriod := input.QuotaPeriod
	if quotaPeriod == "" {
		quotaPeriod = QuotaPeriodMonthly
	}
	if !validQuotaPeriod(quotaPeriod) {
		return Member{}, ErrInvalidQuotaPeriod
	}
	now := s.now()
	email := strings.TrimSpace(input.Email)
	note := strings.TrimSpace(input.Note)
	item, err := s.repo.Create(ctx, Mutation{
		OwnerID:      &ownerID,
		Name:         &name,
		Email:        &email,
		Note:         &note,
		QuotaUSD:     &input.QuotaUSD,
		QuotaPeriod:  &quotaPeriod,
		PeriodAnchor: &now,
		PeriodStart:  &now,
	})
	if err != nil {
		logger.Error("member_create_failed", sdk.LogFieldUserID, ownerID, sdk.LogFieldError, err)
		return Member{}, err
	}
	logger.Info("member_created", sdk.LogFieldUserID, ownerID, "member_id", item.ID)
	Decorate(&item, now)
	return item, nil
}

// Update 更新成员资料 / 额度 / 周期 / 状态。改周期不动锚点：从 none 切回 monthly 时
// 仍按原创建日对齐换期，避免每改一次周期就漂一次账期。
func (s *Service) Update(ctx context.Context, ownerID, id int, input UpdateInput) (Member, error) {
	logger := sdk.LoggerFromContext(ctx)
	mutation := Mutation{QuotaUSD: input.QuotaUSD}
	if input.Name != nil {
		name := strings.TrimSpace(*input.Name)
		if name == "" {
			return Member{}, ErrNameRequired
		}
		mutation.Name = &name
	}
	if input.Email != nil {
		email := strings.TrimSpace(*input.Email)
		mutation.Email = &email
	}
	if input.Note != nil {
		note := strings.TrimSpace(*input.Note)
		mutation.Note = &note
	}
	if input.QuotaUSD != nil && *input.QuotaUSD < 0 {
		return Member{}, ErrInvalidQuota
	}
	if input.QuotaPeriod != nil {
		if !validQuotaPeriod(*input.QuotaPeriod) {
			return Member{}, ErrInvalidQuotaPeriod
		}
		mutation.QuotaPeriod = input.QuotaPeriod
	}
	if input.Status != nil {
		if *input.Status != StatusActive && *input.Status != StatusDisabled {
			return Member{}, ErrInvalidStatus
		}
		mutation.Status = input.Status
	}

	updated, err := s.repo.UpdateOwned(ctx, ownerID, id, mutation)
	if err != nil {
		logger.Error("member_update_failed", sdk.LogFieldUserID, ownerID, "member_id", id, sdk.LogFieldError, err)
		return Member{}, err
	}
	if mutation.Status != nil {
		logger.Info("member_status_changed", sdk.LogFieldUserID, ownerID, "member_id", id, sdk.LogFieldStatus, *mutation.Status)
	}
	if mutation.QuotaUSD != nil || mutation.QuotaPeriod != nil {
		logger.Info("member_quota_updated", sdk.LogFieldUserID, ownerID, "member_id", id)
	}
	s.invalidateKeyCaches(ctx, id)
	Decorate(&updated, s.now())
	return updated, nil
}

// Delete 删除成员及其名下全部 API Key。
func (s *Service) Delete(ctx context.Context, ownerID, id int) error {
	logger := sdk.LoggerFromContext(ctx)
	// 先取 hash 再删：删完就查不到 key 了，而缓存里仍可能放行至多 5s。
	hashes, err := s.repo.KeyHashesByMember(ctx, id)
	if err != nil {
		logger.Warn("member_key_hash_lookup_failed", "member_id", id, sdk.LogFieldError, err)
	}
	if err := s.repo.DeleteOwned(ctx, ownerID, id); err != nil {
		logger.Error("member_delete_failed", sdk.LogFieldUserID, ownerID, "member_id", id, sdk.LogFieldError, err)
		return err
	}
	for _, hash := range hashes {
		auth.InvalidateAPIKeyCacheByHash(hash)
	}
	logger.Info("member_deleted", sdk.LogFieldUserID, ownerID, "member_id", id, "keys", len(hashes))
	return nil
}

// ResetPeriod 手动把成员本期已用清零（不改锚点与周期）。
func (s *Service) ResetPeriod(ctx context.Context, ownerID, id int) (Member, error) {
	logger := sdk.LoggerFromContext(ctx)
	now := s.now()
	updated, err := s.repo.ResetPeriodOwned(ctx, ownerID, id, now)
	if err != nil {
		logger.Error("member_period_reset_failed", sdk.LogFieldUserID, ownerID, "member_id", id, sdk.LogFieldError, err)
		return Member{}, err
	}
	logger.Info("member_period_reset", sdk.LogFieldUserID, ownerID, "member_id", id)
	s.invalidateKeyCaches(ctx, id)
	Decorate(&updated, now)
	return updated, nil
}

func (s *Service) invalidateKeyCaches(ctx context.Context, memberID int) {
	hashes, err := s.repo.KeyHashesByMember(ctx, memberID)
	if err != nil {
		sdk.LoggerFromContext(ctx).Warn("member_key_hash_lookup_failed", "member_id", memberID, sdk.LogFieldError, err)
		return
	}
	for _, hash := range hashes {
		auth.InvalidateAPIKeyCacheByHash(hash)
	}
}

// Decorate 按 now 填充派生字段 PeriodUsed / PeriodEnd。
//
// monthly 成员若已跨期但鉴权尚未推进 period_start（期内没有请求进来），
// 展示口径同样按新期从 0 起算，与转发闸门（auth.evaluateMember）一致。
func Decorate(m *Member, now time.Time) {
	base := m.PeriodUsedBase
	m.PeriodEnd = nil
	if m.QuotaPeriod == QuotaPeriodMonthly {
		_, end, rolled := period.Window(m.PeriodAnchor, m.PeriodStart, now)
		if rolled {
			base = m.UsedQuota
		}
		m.PeriodEnd = &end
	}
	used := m.UsedQuota - base
	if used < 0 {
		used = 0
	}
	m.PeriodUsed = used
}

func validQuotaPeriod(value string) bool {
	return value == QuotaPeriodNone || value == QuotaPeriodMonthly
}
