package audit

import (
	"context"
	"time"

	"github.com/DouDOU-start/airgate-core/internal/pkg/pagination"
	sdk "github.com/DouDOU-start/airgate-sdk/sdkgo"
)

// Service 审计应用服务：写入（Recorder）与查询。
type Service struct {
	repo Repository
	now  func() time.Time
}

// NewService 创建审计服务。
func NewService(repo Repository) *Service {
	return &Service{repo: repo, now: time.Now}
}

// Record 写一条审计；actor / ip / request_id 从 context 补齐（业务层只填 owner / action / target / before / after）。
// 写失败只记日志：审计不能反过来让业务操作失败。
func (s *Service) Record(ctx context.Context, entry Entry) {
	if s == nil || s.repo == nil {
		return
	}
	actor := ActorFromContext(ctx)
	if entry.ActorUserID == 0 {
		entry.ActorUserID = actor.UserID
	}
	if entry.ActorEmail == "" {
		entry.ActorEmail = actor.Email
	}
	if entry.IP == "" {
		entry.IP = actor.IP
	}
	if entry.RequestID == "" {
		entry.RequestID = actor.RequestID
	}
	if entry.RequestID == "" {
		entry.RequestID = sdk.RequestIDFromContext(ctx)
	}
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = s.now()
	}
	// 请求已取消/超时也要把审计写完：用不带取消的 context。
	if err := s.repo.Create(context.WithoutCancel(ctx), entry); err != nil {
		sdk.LoggerFromContext(ctx).Warn("team_audit_write_failed",
			"action", entry.Action, "owner_id", entry.OwnerID, sdk.LogFieldError, err)
	}
}

// List 分页查询。
func (s *Service) List(ctx context.Context, filter ListFilter) (ListResult, error) {
	page, pageSize := pagination.Normalize(filter.Page, filter.PageSize)
	filter.Page, filter.PageSize = page, pageSize
	list, total, err := s.repo.List(ctx, filter)
	if err != nil {
		sdk.LoggerFromContext(ctx).Error("team_audit_query_failed", "owner_id", filter.OwnerID, sdk.LogFieldError, err)
		return ListResult{}, err
	}
	return ListResult{List: list, Total: total, Page: page, PageSize: pageSize}, nil
}

// Noop 空实现：测试或未装配审计时使用。
type Noop struct{}

// Record 什么都不做。
func (Noop) Record(context.Context, Entry) {}

var _ Recorder = (*Service)(nil)
var _ Recorder = Noop{}
