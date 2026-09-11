package member

import (
	"context"
	"net/mail"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/DouDOU-start/airgate-core/internal/app/audit"
	"github.com/DouDOU-start/airgate-core/internal/app/teamscope"
	"github.com/DouDOU-start/airgate-core/internal/auth"
	"github.com/DouDOU-start/airgate-core/internal/pkg/pagination"
	"github.com/DouDOU-start/airgate-core/internal/pkg/period"
	"github.com/DouDOU-start/airgate-core/internal/pkg/timezone"
	sdk "github.com/DouDOU-start/airgate-sdk/sdkgo"
)

// Service 团队成员应用服务：主账号侧的成员管理。
//
// 每个方法的首个业务参数是 teamscope.Scope 而不是裸 ownerID：企业主 / 管理员传全企业范围，
// 部门负责人传单部门范围。**范围校验落在本层**（不是 handler，也不是路由），任何新调用方
// 都绕不过去；跨部门一律按"不存在"处理（404 语义），不泄露别的部门有没有这个成员。
//
// 成员改额度 / 停用 / 删除后要立刻反映到转发闸门，而鉴权结果有 5s 缓存，
// 因此写操作完成后会按成员名下 key 的 hash 逐个失效缓存；失效失败不影响写入结果
// （最多 apiKeyCacheTTL 后自然生效）。
type Service struct {
	repo  Repository
	audit audit.Recorder
	now   func() time.Time
}

// NewService 创建成员服务；recorder 为 nil 时不写审计。
func NewService(repo Repository, recorder audit.Recorder) *Service {
	if recorder == nil {
		recorder = audit.Noop{}
	}
	return &Service{repo: repo, audit: recorder, now: time.Now}
}

// List 查询当前用户名下的成员，并附带本期已用 / 密钥数 / 今日与近 30 天成本。
// tz 决定"今日"的起点；为空时回退到服务器本地时区。
func (s *Service) List(ctx context.Context, scope teamscope.Scope, filter ListFilter, tz string) (ListResult, error) {
	logger := sdk.LoggerFromContext(ctx)
	ownerID := scope.OwnerID
	page, pageSize := pagination.Normalize(filter.Page, filter.PageSize)
	filter.Page = page
	filter.PageSize = pageSize
	// 部门负责人：无视客户端传来的 department_id，一律钉死本部门。
	if scope.IsDepartmentManager() {
		departmentID := scope.ScopedDepartmentID()
		filter.DepartmentID = &departmentID
	}

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

// Get 查询范围内的单个成员（含派生字段，不含成本聚合）。
func (s *Service) Get(ctx context.Context, scope teamscope.Scope, id int) (Member, error) {
	item, err := s.repo.FindOwned(ctx, scope.OwnerID, id)
	if err != nil {
		return Member{}, err
	}
	if err := ensureMemberInScope(scope, item); err != nil {
		return Member{}, err
	}
	Decorate(&item, s.now())
	return item, nil
}

// ensureMemberInScope 读路径的范围校验：部门负责人只看得到本部门成员（含自己那条，
// 否则列表里有、单查却没有，前后不一致）。
//
// 跨部门按 ErrMemberNotFound 返回（404 语义）：负责人显式拿别的部门的成员 id 来试探时，
// 不该从"403 还是 404"里读出那个成员到底存不存在。
func ensureMemberInScope(scope teamscope.Scope, m Member) error {
	if !scope.IsDepartmentManager() {
		return nil
	}
	if !scope.AllowsDepartment(m.DepartmentID) {
		return ErrMemberNotFound
	}
	return nil
}

// ensureMemberWritableInScope 写路径的范围校验：在读的基础上再禁掉"改 / 删自己那条记录"
// ——自己抬自己的额度、或把自己停用锁死，都必须回到企业主手里。
func ensureMemberWritableInScope(scope teamscope.Scope, m Member) error {
	if err := ensureMemberInScope(scope, m); err != nil {
		return err
	}
	if scope.IsSelfMember(m.ID) {
		return ErrOutOfScope
	}
	return nil
}

// Create 创建成员。额度周期默认 monthly，锚点取创建时刻。
//
// 传了密码即同时创建成员的登录账号（邮箱必填且全站唯一，额度必填）：成员用邮箱+密码正常登录，
// 与普通用户唯一的差别是消耗与归属落在企业主名下。
func (s *Service) Create(ctx context.Context, scope teamscope.Scope, input CreateInput) (Member, error) {
	logger := sdk.LoggerFromContext(ctx)
	ownerID := scope.OwnerID
	// 部门负责人只能往自己部门加人：显式传了别的部门直接拒，没传则钉死本部门。
	if scope.IsDepartmentManager() {
		if input.DepartmentID != nil && int(*input.DepartmentID) != scope.ScopedDepartmentID() {
			return Member{}, ErrOutOfScope
		}
		departmentID := int64(scope.ScopedDepartmentID())
		input.DepartmentID = &departmentID
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return Member{}, ErrNameRequired
	}
	if input.QuotaUSD < 0 {
		return Member{}, ErrInvalidQuota
	}
	// 有登录账号的成员额度必填：成员登录后看到的"余额"就是本期剩余额度，0=不限会让他
	// 直接看到企业主余额；老模型（无账号）成员沿用 0=不限。
	if input.Password != "" && input.QuotaUSD <= 0 {
		return Member{}, ErrQuotaRequired
	}
	quotaPeriod := input.QuotaPeriod
	if quotaPeriod == "" {
		quotaPeriod = QuotaPeriodMonthly
	}
	if !validQuotaPeriod(quotaPeriod) {
		return Member{}, ErrInvalidQuotaPeriod
	}
	allowed, err := s.normalizeAllowedGroups(ctx, ownerID, input.AllowedGroupIDs)
	if err != nil {
		return Member{}, err
	}
	departmentID, hasDepartment, err := s.resolveDepartment(ctx, ownerID, input.DepartmentID)
	if err != nil {
		return Member{}, err
	}
	// 账期对齐：锚点继承企业账期锚点、本期起点按锚点逐月对齐——部门/成员/企业三层「本期」同窗，
	// 否则会出现「部门本期已用 < 成员本期已用」的父子矛盾。
	anchor, err := s.repo.OwnerBillingAnchor(ctx, ownerID)
	if err != nil {
		return Member{}, err
	}
	now := s.now()
	start, _, _ := period.Window(anchor, now, now)
	email := strings.TrimSpace(input.Email)
	note := strings.TrimSpace(input.Note)
	mutation := Mutation{
		OwnerID:            &ownerID,
		Name:               &name,
		Email:              &email,
		Note:               &note,
		QuotaUSD:           &input.QuotaUSD,
		QuotaPeriod:        &quotaPeriod,
		AllowedGroupIDs:    allowed,
		HasAllowedGroupIDs: true,
		PeriodAnchor:       &anchor,
		PeriodStart:        &start,
		DepartmentID:       departmentID,
		HasDepartmentID:    hasDepartment,
	}

	var item Member
	if password := input.Password; password != "" {
		email = strings.ToLower(email)
		mutation.Email = &email
		account, err := s.buildAccount(ctx, email, password, name)
		if err != nil {
			return Member{}, err
		}
		item, err = s.repo.CreateWithAccount(ctx, mutation, account)
		if err != nil {
			logger.Error("member_create_failed", sdk.LogFieldUserID, ownerID, sdk.LogFieldReason, "with_account", sdk.LogFieldError, err)
			return Member{}, err
		}
	} else {
		item, err = s.repo.Create(ctx, mutation)
		if err != nil {
			logger.Error("member_create_failed", sdk.LogFieldUserID, ownerID, sdk.LogFieldError, err)
			return Member{}, err
		}
	}
	logger.Info("member_created", sdk.LogFieldUserID, ownerID, "member_id", item.ID, "with_account", item.AccountUserID > 0)
	s.audit.Record(ctx, audit.Entry{
		OwnerID: ownerID, Action: audit.ActionMemberCreate, TargetType: audit.TargetMember,
		TargetID: item.ID, TargetName: item.Name, After: snapshot(item),
	})
	Decorate(&item, now)
	return item, nil
}

// resolveDepartment 校验部门归属：nil 不动；0 = 未分配；>0 必须是本企业主的部门。
func (s *Service) resolveDepartment(ctx context.Context, ownerID int, raw *int64) (departmentID *int, has bool, err error) {
	if raw == nil {
		return nil, false, nil
	}
	if *raw <= 0 {
		return nil, true, nil
	}
	id := int(*raw)
	owned, err := s.repo.DepartmentOwnedBy(ctx, ownerID, id)
	if err != nil {
		return nil, false, err
	}
	if !owned {
		return nil, false, ErrDepartmentNotFound
	}
	return &id, true, nil
}

// snapshot 审计用的关键字段快照（不含密码）。
func snapshot(m Member) map[string]any {
	return map[string]any{
		"name":              m.Name,
		"email":             m.Email,
		"quota_usd":         m.QuotaUSD,
		"quota_period":      m.QuotaPeriod,
		"status":            m.Status,
		"allowed_group_ids": append([]int64{}, m.AllowedGroupIDs...),
		"department_id":     m.DepartmentID,
	}
}

// buildAccount 校验邮箱/密码并生成账号写入；邮箱与全站用户唯一。
func (s *Service) buildAccount(ctx context.Context, email, password, username string) (AccountInput, error) {
	if email == "" {
		return AccountInput{}, ErrEmailRequired
	}
	if addr, err := mail.ParseAddress(email); err != nil || addr.Address != email {
		return AccountInput{}, ErrInvalidEmail
	}
	if len(password) < 6 {
		return AccountInput{}, ErrPasswordTooShort
	}
	exists, err := s.repo.AccountEmailExists(ctx, email)
	if err != nil {
		return AccountInput{}, err
	}
	if exists {
		return AccountInput{}, ErrEmailAlreadyExists
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return AccountInput{}, err
	}
	return AccountInput{Email: email, PasswordHash: string(hash), Username: username}, nil
}

// normalizeAllowedGroups 去重并校验白名单只能选企业主自己可见的分组；空即不限。
func (s *Service) normalizeAllowedGroups(ctx context.Context, ownerID int, ids []int64) ([]int64, error) {
	if len(ids) == 0 {
		return []int64{}, nil
	}
	visible, err := s.repo.OwnerVisibleGroupIDs(ctx, ownerID)
	if err != nil {
		return nil, err
	}
	visibleSet := make(map[int64]struct{}, len(visible))
	for _, id := range visible {
		visibleSet[id] = struct{}{}
	}
	out := make([]int64, 0, len(ids))
	seen := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		if _, ok := visibleSet[id]; !ok {
			return nil, ErrGroupNotAllowed
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out, nil
}

// Update 更新成员资料 / 额度 / 周期 / 状态。改周期不动锚点：从 none 切回 monthly 时
// 仍按原创建日对齐换期，避免每改一次周期就漂一次账期。
func (s *Service) Update(ctx context.Context, scope teamscope.Scope, id int, input UpdateInput) (Member, error) {
	logger := sdk.LoggerFromContext(ctx)
	ownerID := scope.OwnerID
	if scope.IsDepartmentManager() {
		// 调岗（跨部门搬人）是企业主的动作，负责人只能在本部门内改人。
		if input.DepartmentID != nil && int(*input.DepartmentID) != scope.ScopedDepartmentID() {
			return Member{}, ErrOutOfScope
		}
		// 重置他人登录密码 = 可直接冒用其账号，留给企业主。
		if input.Password != nil && *input.Password != "" {
			return Member{}, ErrOutOfScope
		}
		if scope.IsSelfMember(id) {
			return Member{}, ErrOutOfScope
		}
	}
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
	if input.AllowedGroupIDs != nil {
		allowed, err := s.normalizeAllowedGroups(ctx, ownerID, *input.AllowedGroupIDs)
		if err != nil {
			return Member{}, err
		}
		mutation.AllowedGroupIDs = allowed
		mutation.HasAllowedGroupIDs = true
	}
	if departmentID, has, err := s.resolveDepartment(ctx, ownerID, input.DepartmentID); err != nil {
		return Member{}, err
	} else if has {
		mutation.DepartmentID = departmentID
		mutation.HasDepartmentID = true
	}

	// 账号资料（邮箱/密码）先于成员资料写：有账号的成员邮箱是登录凭证，须全站唯一。
	current, err := s.repo.FindOwned(ctx, ownerID, id)
	if err != nil {
		return Member{}, err
	}
	if err := ensureMemberWritableInScope(scope, current); err != nil {
		return Member{}, err
	}
	// 登录邮箱是该成员的全站唯一凭证，改它等于换人；负责人可原样回传（表单整体提交），
	// 但不能真的改动。
	//
	// ⚠️ 比对基准只认 **AccountEmail（users.email，登录身份的唯一权威）**：members.email 只是
	// 展示/联系列，且与账号邮箱分两条非事务语句写入（UpdateAccountOwned 后 UpdateOwned），中途
	// 失败就会漂移；一旦漂移，拿 members.email 当放行依据就等于"传个旧值即可改掉别人的登录邮箱"
	// ——那是一条完整的账号接管链（改邮箱 → 走找回密码）。无账号的老模型成员没有登录身份，
	// 比对 members.email 即可。
	if scope.IsDepartmentManager() && input.Email != nil {
		next := strings.TrimSpace(*input.Email)
		authoritative := current.Email
		if current.AccountUserID > 0 {
			authoritative = current.AccountEmail
		}
		if !strings.EqualFold(next, authoritative) {
			return Member{}, ErrOutOfScope
		}
	}
	// 有账号的成员不允许把额度改回 0（不限），口径同 Create；老模型成员不受限。
	if current.AccountUserID > 0 && input.QuotaUSD != nil && *input.QuotaUSD <= 0 {
		return Member{}, ErrQuotaRequired
	}
	if current.AccountUserID > 0 {
		patch := AccountPatch{}
		if mutation.Email != nil && !strings.EqualFold(*mutation.Email, current.AccountEmail) {
			email := strings.ToLower(*mutation.Email)
			if email == "" {
				return Member{}, ErrEmailRequired
			}
			if addr, err := mail.ParseAddress(email); err != nil || addr.Address != email {
				return Member{}, ErrInvalidEmail
			}
			exists, err := s.repo.AccountEmailExists(ctx, email)
			if err != nil {
				return Member{}, err
			}
			if exists {
				return Member{}, ErrEmailAlreadyExists
			}
			mutation.Email = &email
			patch.Email = &email
		}
		if input.Password != nil && *input.Password != "" {
			if len(*input.Password) < 6 {
				return Member{}, ErrPasswordTooShort
			}
			hash, err := bcrypt.GenerateFromPassword([]byte(*input.Password), bcrypt.DefaultCost)
			if err != nil {
				return Member{}, err
			}
			hashed := string(hash)
			patch.PasswordHash = &hashed
		}
		if patch.Email != nil || patch.PasswordHash != nil {
			if err := s.repo.UpdateAccountOwned(ctx, ownerID, id, patch); err != nil {
				logger.Error("member_account_update_failed", sdk.LogFieldUserID, ownerID, "member_id", id, sdk.LogFieldError, err)
				return Member{}, err
			}
			if patch.PasswordHash != nil {
				logger.Info("member_password_reset", sdk.LogFieldUserID, ownerID, "member_id", id)
			}
		}
	} else if input.Password != nil && *input.Password != "" {
		return Member{}, ErrMemberNoAccount
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
	if mutation.HasAllowedGroupIDs {
		logger.Info("member_groups_updated", sdk.LogFieldUserID, ownerID, "member_id", id, "groups", len(mutation.AllowedGroupIDs))
	}
	if mutation.HasDepartmentID {
		logger.Info("member_department_changed", sdk.LogFieldUserID, ownerID, "member_id", id, "department_id", updated.DepartmentID)
	}
	s.invalidateKeyCaches(ctx, id)
	auth.InvalidateTeamIdentity(updated.AccountUserID)
	after := snapshot(updated)
	if input.Password != nil && *input.Password != "" {
		after["password_reset"] = true
	}
	s.audit.Record(ctx, audit.Entry{
		OwnerID: ownerID, Action: audit.ActionMemberUpdate, TargetType: audit.TargetMember,
		TargetID: id, TargetName: updated.Name, Before: snapshot(current), After: after,
	})
	Decorate(&updated, s.now())
	return updated, nil
}

// Delete 删除成员、其登录账号及名下全部 API Key（使用记录保留）。
func (s *Service) Delete(ctx context.Context, scope teamscope.Scope, id int) error {
	logger := sdk.LoggerFromContext(ctx)
	ownerID := scope.OwnerID
	if scope.IsSelfMember(id) {
		return ErrOutOfScope
	}
	accountUserID := 0
	current, findErr := s.repo.FindOwned(ctx, ownerID, id)
	if findErr == nil {
		accountUserID = current.AccountUserID
		if err := ensureMemberWritableInScope(scope, current); err != nil {
			return err
		}
	} else if scope.IsDepartmentManager() {
		// 负责人范围下取不到成员就别删：范围校验依赖这一行，查不到一律失败。
		return findErr
	}
	// 先取 hash / 账号再删：删完就查不到了，而缓存里仍可能放行至多 5s。
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
	auth.InvalidateTeamIdentity(accountUserID)
	logger.Info("member_deleted", sdk.LogFieldUserID, ownerID, "member_id", id, "keys", len(hashes), "account_user_id", accountUserID)
	if findErr == nil {
		s.audit.Record(ctx, audit.Entry{
			OwnerID: ownerID, Action: audit.ActionMemberDelete, TargetType: audit.TargetMember,
			TargetID: id, TargetName: current.Name, Before: snapshot(current),
		})
	}
	return nil
}

// ResetPeriod 手动把成员本期已用清零（不改锚点与周期）。
func (s *Service) ResetPeriod(ctx context.Context, scope teamscope.Scope, id int) (Member, error) {
	logger := sdk.LoggerFromContext(ctx)
	ownerID := scope.OwnerID
	// 清零本期已用等于给自己续额度，负责人不能对自己做；跨部门同样拦在这里。
	if scope.IsDepartmentManager() {
		current, err := s.repo.FindOwned(ctx, ownerID, id)
		if err != nil {
			return Member{}, err
		}
		if err := ensureMemberWritableInScope(scope, current); err != nil {
			return Member{}, err
		}
	}
	now := s.now()
	updated, err := s.repo.ResetPeriodOwned(ctx, ownerID, id, now)
	if err != nil {
		logger.Error("member_period_reset_failed", sdk.LogFieldUserID, ownerID, "member_id", id, sdk.LogFieldError, err)
		return Member{}, err
	}
	logger.Info("member_period_reset", sdk.LogFieldUserID, ownerID, "member_id", id)
	s.invalidateKeyCaches(ctx, id)
	s.audit.Record(ctx, audit.Entry{
		OwnerID: ownerID, Action: audit.ActionMemberResetPeriod, TargetType: audit.TargetMember,
		TargetID: id, TargetName: updated.Name,
	})
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
