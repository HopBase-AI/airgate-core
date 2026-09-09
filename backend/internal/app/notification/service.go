package notification

import (
	"context"
	"errors"
	"time"

	"github.com/DouDOU-start/airgate-core/internal/pkg/pagination"
)

// Service 站内通知应用服务。
type Service struct {
	repo Repository
	now  func() time.Time
}

// NewService 创建通知服务。
func NewService(repo Repository) *Service {
	return &Service{repo: repo, now: time.Now}
}

// Create 投递一条通知。DedupeKey 非空且已存在时返回 created=false 且不报错，
// 调用方据此跳过随附动作（如邮件）。
func (s *Service) Create(ctx context.Context, in CreateInput) (created bool, err error) {
	if in.UserID <= 0 || in.Kind == "" || in.Title == "" {
		return false, ErrInvalidInput
	}
	switch in.Level {
	case LevelInfo, LevelWarning, LevelDanger:
	case "":
		in.Level = LevelInfo
	default:
		return false, ErrInvalidInput
	}
	if err := s.repo.Create(ctx, in); err != nil {
		if errors.Is(err, ErrDuplicate) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// ListMine 当前用户的通知列表（时间倒序），单页上限 MaxPageSize。
func (s *Service) ListMine(ctx context.Context, userID int, filter ListFilter) (ListResult, error) {
	page, pageSize := pagination.Normalize(filter.Page, filter.PageSize)
	if pageSize > MaxPageSize {
		pageSize = MaxPageSize
	}
	filter.Page, filter.PageSize = page, pageSize
	list, total, err := s.repo.List(ctx, userID, filter)
	if err != nil {
		return ListResult{}, err
	}
	if list == nil {
		list = []Notification{}
	}
	return ListResult{List: list, Total: total, Page: page, PageSize: pageSize}, nil
}

// UnreadCount 未读数。
func (s *Service) UnreadCount(ctx context.Context, userID int) (int64, error) {
	return s.repo.CountUnread(ctx, userID)
}

// MarkRead 把指定 id 标记为已读；只作用于该用户自己的通知，返回实际更新行数。
func (s *Service) MarkRead(ctx context.Context, userID int, ids []int) (int, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	return s.repo.MarkRead(ctx, userID, ids, s.now())
}

// MarkAllRead 把该用户全部未读标记为已读，返回实际更新行数。
func (s *Service) MarkAllRead(ctx context.Context, userID int) (int, error) {
	return s.repo.MarkAllRead(ctx, userID, s.now())
}
