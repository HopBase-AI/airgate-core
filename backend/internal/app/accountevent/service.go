package accountevent

import "context"

// Service 账号异常事件查询服务。
type Service struct {
	repo Repository
}

// NewService 构造事件查询服务。
func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

// List 分页查询异常事件，按发生时间倒序。
func (s *Service) List(ctx context.Context, filter ListFilter) (ListResult, error) {
	if filter.Page < 1 {
		filter.Page = 1
	}
	if filter.PageSize < 1 || filter.PageSize > 100 {
		filter.PageSize = 20
	}
	list, total, err := s.repo.List(ctx, filter)
	if err != nil {
		return ListResult{}, err
	}
	return ListResult{List: list, Total: total, Page: filter.Page, PageSize: filter.PageSize}, nil
}
