package generationtask

import (
	"context"
	"testing"
	"time"
)

type stubRepository struct {
	listFilter      ListFilter
	summarySnapshot SummarySnapshot
	recentSince     time.Time
	backlogBefore   time.Time
	staleBefore     time.Time
}

func (s *stubRepository) List(_ context.Context, filter ListFilter) ([]Task, int64, error) {
	s.listFilter = filter
	return []Task{{ID: 1}}, 1, nil
}

func (s *stubRepository) Summary(_ context.Context, recentSince, backlogBefore, staleBefore time.Time) (SummarySnapshot, error) {
	s.recentSince = recentSince
	s.backlogBefore = backlogBefore
	s.staleBefore = staleBefore
	return s.summarySnapshot, nil
}

func TestListNormalizesPaginationAndFilters(t *testing.T) {
	repo := &stubRepository{}
	service := NewService(repo)

	result, err := service.List(t.Context(), ListFilter{
		Page: -1, PageSize: 500, Status: " FAILED ", Kind: " VIDEO. ", PluginID: " airgate-seedance ", TaskType: " VIDEO.GENERATE ",
	})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if result.Page != 1 || result.PageSize != 20 || result.Total != 1 {
		t.Fatalf("List() result = %+v", result)
	}
	if repo.listFilter.Status != "failed" || repo.listFilter.Kind != "video" || repo.listFilter.PluginID != "airgate-seedance" || repo.listFilter.TaskType != "video.generate" {
		t.Fatalf("normalized filter = %+v", repo.listFilter)
	}
}

func TestSummaryComputesQueueHealth(t *testing.T) {
	now := time.Date(2026, 8, 1, 8, 0, 0, 0, time.UTC)
	repo := &stubRepository{summarySnapshot: SummarySnapshot{
		Pending: 3, Processing: 2, Retrying: 1, Cancelling: 1,
		CompletedRecent: 18, FailedRecent: 2,
	}}
	service := NewService(repo)
	service.now = func() time.Time { return now }

	result, err := service.Summary(t.Context())
	if err != nil {
		t.Fatalf("Summary() error = %v", err)
	}
	if result.Queued != 4 || result.Active != 7 {
		t.Fatalf("queue summary = %+v", result)
	}
	if result.FailureRateRecent != 0.1 {
		t.Fatalf("FailureRateRecent = %v, want 0.1", result.FailureRateRecent)
	}
	if !repo.recentSince.Equal(now.Add(-24*time.Hour)) || !repo.backlogBefore.Equal(now.Add(-5*time.Minute)) || !repo.staleBefore.Equal(now.Add(-15*time.Minute)) {
		t.Fatalf("summary thresholds = recent %v backlog %v stale %v", repo.recentSince, repo.backlogBefore, repo.staleBefore)
	}
}
