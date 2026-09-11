package department

import (
	"context"
	"strings"
	"time"

	"github.com/DouDOU-start/airgate-core/internal/app/audit"
	"github.com/DouDOU-start/airgate-core/internal/app/teamscope"
	"github.com/DouDOU-start/airgate-core/internal/auth"
	"github.com/DouDOU-start/airgate-core/internal/pkg/pagination"
	"github.com/DouDOU-start/airgate-core/internal/pkg/period"
	"github.com/DouDOU-start/airgate-core/internal/pkg/timezone"
	sdk "github.com/DouDOU-start/airgate-sdk/sdkgo"
)

// Service 企业组织（部门）应用服务：企业主侧的部门管理、企业总览与账期设置。
//
// 每个方法都收 teamscope.Scope：组织结构写操作（建 / 改 / 删部门、重置部门本期）与企业账期
// **只允许全企业范围**，部门负责人一律 ErrOutOfScope——路由已经拦过一层，这里是第二层，
// 保证将来任何新调用方都不可能绕过去。只读路径按范围收敛：负责人只看得到自己那个部门，
// 总览也换成部门口径（企业余额不对负责人暴露）。
//
// 部门改额度 / 删除后要立刻反映到转发闸门（鉴权结果有 5s 缓存），因此写操作完成后按
// 有效部门为该部门的 key 逐个失效缓存，并清空成员账号的团队归属缓存；失效失败不影响写入。
type Service struct {
	repo  Repository
	audit audit.Recorder
	now   func() time.Time
}

// NewService 创建部门服务；recorder 为 nil 时不写审计。
func NewService(repo Repository, recorder audit.Recorder) *Service {
	if recorder == nil {
		recorder = audit.Noop{}
	}
	return &Service{repo: repo, audit: recorder, now: time.Now}
}

// List 查询企业主名下的部门，并附带本期已用 / 成员数 / 密钥数 / 已分配 / 今日与近 30 天成本。
func (s *Service) List(ctx context.Context, scope teamscope.Scope, filter ListFilter, tz string) (ListResult, error) {
	logger := sdk.LoggerFromContext(ctx)
	ownerID := scope.OwnerID
	page, pageSize := pagination.Normalize(filter.Page, filter.PageSize)
	filter.Page, filter.PageSize = page, pageSize

	// 部门负责人：列表就是他那一个部门，直接按 id 取，不给分页/关键词留出扫描别人部门的口子。
	if scope.IsDepartmentManager() {
		item, err := s.scopedDepartment(ctx, scope, tz)
		if err != nil {
			return ListResult{}, err
		}
		return ListResult{List: []Department{item}, Total: 1, Page: page, PageSize: pageSize}, nil
	}

	list, total, err := s.repo.ListByOwner(ctx, ownerID, filter)
	if err != nil {
		logger.Error("department_lookup_failed", sdk.LogFieldUserID, ownerID, sdk.LogFieldReason, "list", sdk.LogFieldError, err)
		return ListResult{}, err
	}
	if err := s.decorateAll(ctx, list, tz); err != nil {
		logger.Error("department_lookup_failed", sdk.LogFieldUserID, ownerID, sdk.LogFieldReason, "decorate", sdk.LogFieldError, err)
		return ListResult{}, err
	}
	return ListResult{List: list, Total: total, Page: page, PageSize: pageSize}, nil
}

// All 范围内的全部部门（下拉/筛选用，含派生字段）；部门负责人只有自己那一个。
func (s *Service) All(ctx context.Context, scope teamscope.Scope, tz string) ([]Department, error) {
	if scope.IsDepartmentManager() {
		item, err := s.scopedDepartment(ctx, scope, tz)
		if err != nil {
			return nil, err
		}
		return []Department{item}, nil
	}
	list, err := s.repo.AllByOwner(ctx, scope.OwnerID)
	if err != nil {
		return nil, err
	}
	if err := s.decorateAll(ctx, list, tz); err != nil {
		return nil, err
	}
	return list, nil
}

// decorateOne 单个部门补齐派生字段（成员数 / 密钥数 / 已分配 / 成本），供创建 / 更新 / 重置的响应使用：
// 否则前端拿到的 member_count 是 0，与列表对不上。
func (s *Service) decorateOne(ctx context.Context, item *Department) {
	list := []Department{*item}
	if err := s.decorateAll(ctx, list, ""); err != nil {
		sdk.LoggerFromContext(ctx).Warn("department_decorate_failed", "department_id", item.ID, sdk.LogFieldError, err)
		Decorate(item, s.now())
		return
	}
	*item = list[0]
}

func (s *Service) decorateAll(ctx context.Context, list []Department, tz string) error {
	ids := make([]int, 0, len(list))
	for _, item := range list {
		ids = append(ids, item.ID)
	}
	members, keys, memberQuota, err := s.repo.Counts(ctx, ids)
	if err != nil {
		return err
	}
	loc := timezone.Resolve(tz)
	todayStart := timezone.StartOfDay(s.now().In(loc))
	todayMap, thirtyDayMap, err := s.repo.Usage(ctx, ids, todayStart)
	if err != nil {
		return err
	}
	now := s.now()
	for i := range list {
		Decorate(&list[i], now)
		list[i].MemberCount = members[list[i].ID]
		list[i].KeyCount = keys[list[i].ID]
		list[i].MemberQuotaTotal = memberQuota[list[i].ID]
		list[i].TodayCost = todayMap[list[i].ID]
		list[i].ThirtyDayCost = thirtyDayMap[list[i].ID]
	}
	return nil
}

// Get 查询范围内的单个部门（含派生字段，不含成本聚合）。
// 范围外的部门按"不存在"返回，不泄露该企业还有哪些部门。
func (s *Service) Get(ctx context.Context, scope teamscope.Scope, id int) (Department, error) {
	if !scope.AllowsDepartment(id) {
		return Department{}, ErrDepartmentNotFound
	}
	item, err := s.repo.FindOwned(ctx, scope.OwnerID, id)
	if err != nil {
		return Department{}, err
	}
	Decorate(&item, s.now())
	return item, nil
}

// scopedDepartment 取部门负责人自己那个部门（含派生字段）。
func (s *Service) scopedDepartment(ctx context.Context, scope teamscope.Scope, tz string) (Department, error) {
	item, err := s.repo.FindOwned(ctx, scope.OwnerID, scope.ScopedDepartmentID())
	if err != nil {
		return Department{}, err
	}
	list := []Department{item}
	if err := s.decorateAll(ctx, list, tz); err != nil {
		return Department{}, err
	}
	return list[0], nil
}

// Create 创建部门。额度周期默认 monthly；锚点继承企业账期锚点，本期起点按锚点逐月对齐
// ——三层「本期」严格同窗，否则会出现部门本期已用小于成员本期已用的父子矛盾。
func (s *Service) Create(ctx context.Context, scope teamscope.Scope, input CreateInput) (Department, error) {
	logger := sdk.LoggerFromContext(ctx)
	if scope.IsDepartmentManager() {
		return Department{}, ErrOutOfScope
	}
	ownerID := scope.OwnerID
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return Department{}, ErrNameRequired
	}
	if input.QuotaUSD < 0 {
		return Department{}, ErrInvalidQuota
	}
	quotaPeriod := input.QuotaPeriod
	if quotaPeriod == "" {
		quotaPeriod = QuotaPeriodMonthly
	}
	if !validQuotaPeriod(quotaPeriod) {
		return Department{}, ErrInvalidQuotaPeriod
	}
	taken, err := s.repo.NameTaken(ctx, ownerID, name, 0)
	if err != nil {
		return Department{}, err
	}
	if taken {
		return Department{}, ErrNameTaken
	}
	anchor, err := s.repo.OwnerBillingAnchor(ctx, ownerID)
	if err != nil {
		return Department{}, err
	}
	now := s.now()
	start, _, _ := period.Window(anchor, now, now)
	note := strings.TrimSpace(input.Note)
	item, err := s.repo.Create(ctx, Mutation{
		OwnerID:      &ownerID,
		Name:         &name,
		Note:         &note,
		Sort:         &input.Sort,
		QuotaUSD:     &input.QuotaUSD,
		QuotaPeriod:  &quotaPeriod,
		PeriodAnchor: &anchor,
		PeriodStart:  &start,
	})
	if err != nil {
		logger.Error("department_create_failed", sdk.LogFieldUserID, ownerID, sdk.LogFieldError, err)
		return Department{}, err
	}
	logger.Info("department_created", sdk.LogFieldUserID, ownerID, "department_id", item.ID)
	s.audit.Record(ctx, audit.Entry{
		OwnerID: ownerID, Action: audit.ActionDepartmentCreate, TargetType: audit.TargetDepartment,
		TargetID: item.ID, TargetName: item.Name, After: snapshot(item),
	})
	s.decorateOne(ctx, &item)
	return item, nil
}

// Update 更新部门资料 / 额度 / 周期 / 负责人。改周期不动锚点；负责人必须是当前在本部门的成员。
func (s *Service) Update(ctx context.Context, scope teamscope.Scope, id int, input UpdateInput) (Department, error) {
	logger := sdk.LoggerFromContext(ctx)
	// 部门额度天花板是企业主分给该部门的钱，负责人改不得；负责人换人更不能自己改。
	if scope.IsDepartmentManager() {
		return Department{}, ErrOutOfScope
	}
	ownerID := scope.OwnerID
	current, err := s.repo.FindOwned(ctx, ownerID, id)
	if err != nil {
		return Department{}, err
	}
	mutation := Mutation{QuotaUSD: input.QuotaUSD, Sort: input.Sort}
	if input.Name != nil {
		name := strings.TrimSpace(*input.Name)
		if name == "" {
			return Department{}, ErrNameRequired
		}
		if name != current.Name {
			taken, err := s.repo.NameTaken(ctx, ownerID, name, id)
			if err != nil {
				return Department{}, err
			}
			if taken {
				return Department{}, ErrNameTaken
			}
		}
		mutation.Name = &name
	}
	if input.Note != nil {
		note := strings.TrimSpace(*input.Note)
		mutation.Note = &note
	}
	if input.QuotaUSD != nil && *input.QuotaUSD < 0 {
		return Department{}, ErrInvalidQuota
	}
	if input.QuotaPeriod != nil {
		if !validQuotaPeriod(*input.QuotaPeriod) {
			return Department{}, ErrInvalidQuotaPeriod
		}
		mutation.QuotaPeriod = input.QuotaPeriod
	}
	if input.ManagerMemberID != nil {
		mutation.HasManagerMemberID = true
		if memberID := int(*input.ManagerMemberID); memberID > 0 {
			in, err := s.repo.MemberInDepartment(ctx, ownerID, id, memberID)
			if err != nil {
				return Department{}, err
			}
			if !in {
				return Department{}, ErrManagerNotInDepartment
			}
			mutation.ManagerMemberID = &memberID
		}
	}
	updated, err := s.repo.UpdateOwned(ctx, ownerID, id, mutation)
	if err != nil {
		logger.Error("department_update_failed", sdk.LogFieldUserID, ownerID, "department_id", id, sdk.LogFieldError, err)
		return Department{}, err
	}
	if mutation.QuotaUSD != nil || mutation.QuotaPeriod != nil {
		logger.Info("department_quota_updated", sdk.LogFieldUserID, ownerID, "department_id", id)
	}
	if mutation.HasManagerMemberID {
		logger.Info("department_manager_changed", sdk.LogFieldUserID, ownerID, "department_id", id, "manager_member_id", updated.ManagerMemberID)
	}
	s.invalidateCaches(ctx, id)
	s.audit.Record(ctx, audit.Entry{
		OwnerID: ownerID, Action: audit.ActionDepartmentUpdate, TargetType: audit.TargetDepartment,
		TargetID: id, TargetName: updated.Name, Before: snapshot(current), After: snapshot(updated),
	})
	s.decorateOne(ctx, &updated)
	return updated, nil
}

// Delete 删除部门：成员与密钥回落「未分配」，历史用量按快照留在原部门。
func (s *Service) Delete(ctx context.Context, scope teamscope.Scope, id int) error {
	logger := sdk.LoggerFromContext(ctx)
	if scope.IsDepartmentManager() {
		return ErrOutOfScope
	}
	ownerID := scope.OwnerID
	current, err := s.repo.FindOwned(ctx, ownerID, id)
	if err != nil {
		return err
	}
	// 先取 hash / 账号再删：删完就查不到了，而缓存里仍可能按旧额度放行至多 5s。
	hashes, _ := s.repo.KeyHashesByDepartment(ctx, id)
	accounts, _ := s.repo.MemberAccountIDs(ctx, id)
	if err := s.repo.DeleteOwned(ctx, ownerID, id); err != nil {
		logger.Error("department_delete_failed", sdk.LogFieldUserID, ownerID, "department_id", id, sdk.LogFieldError, err)
		return err
	}
	for _, hash := range hashes {
		auth.InvalidateAPIKeyCacheByHash(hash)
	}
	for _, uid := range accounts {
		auth.InvalidateTeamIdentity(uid)
	}
	logger.Info("department_deleted", sdk.LogFieldUserID, ownerID, "department_id", id, "keys", len(hashes))
	s.audit.Record(ctx, audit.Entry{
		OwnerID: ownerID, Action: audit.ActionDepartmentDelete, TargetType: audit.TargetDepartment,
		TargetID: id, TargetName: current.Name, Before: snapshot(current),
	})
	return nil
}

// ResetPeriod 手动把部门本期已用清零（不改锚点与周期）。
func (s *Service) ResetPeriod(ctx context.Context, scope teamscope.Scope, id int) (Department, error) {
	logger := sdk.LoggerFromContext(ctx)
	// 清零部门本期已用 = 给本部门续一整期额度，只能是企业主。
	if scope.IsDepartmentManager() {
		return Department{}, ErrOutOfScope
	}
	ownerID := scope.OwnerID
	now := s.now()
	updated, err := s.repo.ResetPeriodOwned(ctx, ownerID, id, now)
	if err != nil {
		logger.Error("department_period_reset_failed", sdk.LogFieldUserID, ownerID, "department_id", id, sdk.LogFieldError, err)
		return Department{}, err
	}
	logger.Info("department_period_reset", sdk.LogFieldUserID, ownerID, "department_id", id)
	s.invalidateCaches(ctx, id)
	s.audit.Record(ctx, audit.Entry{
		OwnerID: ownerID, Action: audit.ActionDepartmentResetPeriod, TargetType: audit.TargetDepartment,
		TargetID: id, TargetName: updated.Name,
	})
	s.decorateOne(ctx, &updated)
	return updated, nil
}

// Overview 企业层总览：余额、部门/成员数、已分配额度（限额之和）、账期与本期消耗。
// 部门负责人拿到的是**部门口径**的同形投影（见 departmentOverview）。
func (s *Service) Overview(ctx context.Context, scope teamscope.Scope, tz string) (Overview, error) {
	logger := sdk.LoggerFromContext(ctx)
	if scope.IsDepartmentManager() {
		return s.departmentOverview(ctx, scope, tz)
	}
	ownerID := scope.OwnerID
	departments, err := s.repo.AllByOwner(ctx, ownerID)
	if err != nil {
		logger.Error("team_overview_failed", sdk.LogFieldUserID, ownerID, sdk.LogFieldReason, "departments", sdk.LogFieldError, err)
		return Overview{}, err
	}
	balance, memberCount, memberQuotaTotal, unassigned, err := s.repo.OwnerOverview(ctx, ownerID)
	if err != nil {
		logger.Error("team_overview_failed", sdk.LogFieldUserID, ownerID, sdk.LogFieldReason, "owner", sdk.LogFieldError, err)
		return Overview{}, err
	}
	anchor, err := s.repo.OwnerBillingAnchor(ctx, ownerID)
	if err != nil {
		return Overview{}, err
	}
	// 过渡态（改了账期日、新锚点未到）：本期延续到新锚点，起点按新锚点往前推一个月近似。
	start, end := s.periodWindow(anchor)
	actual, billed, err := s.repo.OwnerPeriodUsage(ctx, ownerID, start)
	if err != nil {
		logger.Error("team_overview_failed", sdk.LogFieldUserID, ownerID, sdk.LogFieldReason, "usage", sdk.LogFieldError, err)
		return Overview{}, err
	}
	// 账期日按企业主的展示时区取（锚点是该时区零点；服务端 UTC 取 Day 会差一天）
	loc := timezone.Resolve(tz)
	overview := Overview{
		Balance:               balance,
		DepartmentCount:       len(departments),
		MemberCount:           memberCount,
		MemberQuotaTotal:      memberQuotaTotal,
		UnassignedMemberQuota: unassigned,
		PeriodAnchor:          anchor,
		PeriodStart:           start,
		PeriodEnd:             end,
		BillingDay:            anchor.In(loc).Day(),
		PeriodUsedActual:      actual,
		PeriodUsedBilled:      billed,
	}
	for _, d := range departments {
		overview.DepartmentQuotaTotal += d.QuotaUSD
	}
	return overview, nil
}

// departmentOverview 部门负责人视角的总览：只算自己那个部门，且**不含企业余额**
// （余额是企业主的钱，负责人无权知道）。账期沿用企业口径——三层同窗，部门没有自己的账期。
func (s *Service) departmentOverview(ctx context.Context, scope teamscope.Scope, tz string) (Overview, error) {
	logger := sdk.LoggerFromContext(ctx)
	departmentID := scope.ScopedDepartmentID()
	item, err := s.repo.FindOwned(ctx, scope.OwnerID, departmentID)
	if err != nil {
		return Overview{}, err
	}
	members, _, memberQuota, err := s.repo.Counts(ctx, []int{departmentID})
	if err != nil {
		logger.Error("team_overview_failed", sdk.LogFieldUserID, scope.OwnerID, sdk.LogFieldReason, "counts", sdk.LogFieldError, err)
		return Overview{}, err
	}
	anchor, err := s.repo.OwnerBillingAnchor(ctx, scope.OwnerID)
	if err != nil {
		return Overview{}, err
	}
	start, end := s.periodWindow(anchor)
	actual, billed, err := s.repo.DepartmentPeriodUsage(ctx, scope.OwnerID, departmentID, start)
	if err != nil {
		logger.Error("team_overview_failed", sdk.LogFieldUserID, scope.OwnerID, sdk.LogFieldReason, "department_usage", sdk.LogFieldError, err)
		return Overview{}, err
	}
	return Overview{
		DepartmentCount:      1,
		MemberCount:          members[departmentID],
		DepartmentQuotaTotal: item.QuotaUSD,
		MemberQuotaTotal:     memberQuota[departmentID],
		PeriodAnchor:         anchor,
		PeriodStart:          start,
		PeriodEnd:            end,
		BillingDay:           anchor.In(timezone.Resolve(tz)).Day(),
		PeriodUsedActual:     actual,
		PeriodUsedBilled:     billed,
	}, nil
}

// periodWindow 企业本期窗口；过渡态（改了账期日、新锚点未到）与 Overview 同口径。
func (s *Service) periodWindow(anchor time.Time) (time.Time, time.Time) {
	now := s.now()
	start, end := period.Containing(anchor, now)
	if now.Before(anchor) {
		return period.AddMonths(anchor, -1), anchor
	}
	return start, end
}

// SetBillingDay 把企业账期日改成每月固定日（1~28）：新锚点取严格晚于现在的下一个该日在企业主时区的零点，
// 当前期延续到新锚点，之后按新账期日按月推进；名下全部部门与成员的锚点同步改动（闲置行先结转旧期）。
func (s *Service) SetBillingDay(ctx context.Context, scope teamscope.Scope, day int, tz string) (Overview, error) {
	logger := sdk.LoggerFromContext(ctx)
	// 账期是全企业口径（改一次全部部门/成员的"本期"都跟着动），只能是企业主。
	if scope.IsDepartmentManager() {
		return Overview{}, ErrOutOfScope
	}
	ownerID := scope.OwnerID
	if day < 1 || day > 28 {
		return Overview{}, ErrInvalidBillingDay
	}
	before, err := s.repo.OwnerBillingAnchor(ctx, ownerID)
	if err != nil {
		return Overview{}, err
	}
	anchor := period.AnchorForDay(s.now().In(timezone.Resolve(tz)), day)
	if err := s.repo.SetOwnerBillingAnchor(ctx, ownerID, anchor); err != nil {
		logger.Error("team_billing_anchor_update_failed", sdk.LogFieldUserID, ownerID, sdk.LogFieldError, err)
		return Overview{}, err
	}
	// 锚点变了，所有成员/部门的本期口径都变：清空归属缓存，密钥缓存 5s 自然过期即可。
	auth.InvalidateAllTeamIdentities()
	logger.Info("team_billing_anchor_updated", sdk.LogFieldUserID, ownerID, "day", day, "anchor", anchor)
	s.audit.Record(ctx, audit.Entry{
		OwnerID: ownerID, Action: audit.ActionTeamBillingPeriod, TargetType: audit.TargetTeam, TargetID: ownerID,
		Before: map[string]any{"billing_day": before.Day(), "period_anchor": before},
		After:  map[string]any{"billing_day": day, "period_anchor": anchor},
	})
	return s.Overview(ctx, scope, tz)
}

func (s *Service) invalidateCaches(ctx context.Context, departmentID int) {
	hashes, err := s.repo.KeyHashesByDepartment(ctx, departmentID)
	if err != nil {
		sdk.LoggerFromContext(ctx).Warn("department_key_hash_lookup_failed", "department_id", departmentID, sdk.LogFieldError, err)
	}
	for _, hash := range hashes {
		auth.InvalidateAPIKeyCacheByHash(hash)
	}
	accounts, err := s.repo.MemberAccountIDs(ctx, departmentID)
	if err != nil {
		sdk.LoggerFromContext(ctx).Warn("department_member_lookup_failed", "department_id", departmentID, sdk.LogFieldError, err)
	}
	for _, uid := range accounts {
		auth.InvalidateTeamIdentity(uid)
	}
}

// Decorate 按 now 填充派生字段 PeriodUsed / PeriodEnd（口径与 auth.evaluateDepartment 一致）。
func Decorate(d *Department, now time.Time) {
	base := d.PeriodUsedBase
	d.PeriodEnd = nil
	if d.QuotaPeriod == QuotaPeriodMonthly {
		_, end, rolled := period.Window(d.PeriodAnchor, d.PeriodStart, now)
		if rolled {
			base = d.UsedQuota
		}
		d.PeriodEnd = &end
	}
	used := d.UsedQuota - base
	if used < 0 {
		used = 0
	}
	d.PeriodUsed = used
}

// snapshot 审计用的关键字段快照。
func snapshot(d Department) map[string]any {
	return map[string]any{
		"name":              d.Name,
		"note":              d.Note,
		"quota_usd":         d.QuotaUSD,
		"quota_period":      d.QuotaPeriod,
		"manager_member_id": d.ManagerMemberID,
	}
}

func validQuotaPeriod(value string) bool {
	return value == QuotaPeriodNone || value == QuotaPeriodMonthly
}
