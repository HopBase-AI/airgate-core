package generationtask

import (
	"context"
	"strings"
	"time"
)

// Service 提供管理员生成任务监控查询。
type Service struct {
	repo Repository
	now  func() time.Time
}

// NewService 创建生成任务监控服务。
func NewService(repo Repository) *Service {
	return &Service{repo: repo, now: time.Now}
}

// List 分页查询生成任务，按创建时间倒序。
func (s *Service) List(ctx context.Context, filter ListFilter) (ListResult, error) {
	if filter.Page < 1 {
		filter.Page = 1
	}
	if filter.PageSize < 1 || filter.PageSize > 100 {
		filter.PageSize = 20
	}
	filter.Status = strings.TrimSpace(strings.ToLower(filter.Status))
	filter.Kind = strings.TrimSuffix(strings.TrimSpace(strings.ToLower(filter.Kind)), ".")
	filter.PluginID = strings.TrimSpace(filter.PluginID)
	filter.TaskType = strings.TrimSpace(strings.ToLower(filter.TaskType))

	list, total, err := s.repo.List(ctx, filter)
	if err != nil {
		return ListResult{}, err
	}
	return ListResult{List: list, Total: total, Page: filter.Page, PageSize: filter.PageSize}, nil
}

// Summary 返回当前队列健康和最近 24 小时终态统计。
func (s *Service) Summary(ctx context.Context) (Summary, error) {
	now := s.now().UTC()
	snapshot, err := s.repo.Summary(
		ctx,
		now.Add(-RecentWindow),
		now.Add(-BacklogThreshold),
		now.Add(-StaleProcessingThreshold),
	)
	if err != nil {
		return Summary{}, err
	}

	terminal := snapshot.CompletedRecent + snapshot.FailedRecent
	failureRate := 0.0
	if terminal > 0 {
		failureRate = float64(snapshot.FailedRecent) / float64(terminal)
	}
	return Summary{
		SummarySnapshot:         snapshot,
		Queued:                  snapshot.Pending + snapshot.Retrying,
		Active:                  snapshot.Pending + snapshot.Processing + snapshot.Retrying + snapshot.Cancelling,
		FailureRateRecent:       failureRate,
		RecentWindowSeconds:     int64(RecentWindow.Seconds()),
		BacklogThresholdSeconds: int64(BacklogThreshold.Seconds()),
		StaleThresholdSeconds:   int64(StaleProcessingThreshold.Seconds()),
	}, nil
}
