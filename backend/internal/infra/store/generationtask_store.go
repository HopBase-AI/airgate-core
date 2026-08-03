package store

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/DouDOU-start/airgate-core/ent"
	"github.com/DouDOU-start/airgate-core/ent/predicate"
	enttask "github.com/DouDOU-start/airgate-core/ent/task"
	entuser "github.com/DouDOU-start/airgate-core/ent/user"
	appgenerationtask "github.com/DouDOU-start/airgate-core/internal/app/generationtask"
)

// GenerationTaskStore 是管理员生成任务监控的 ent 实现。
type GenerationTaskStore struct {
	db *ent.Client
}

// NewGenerationTaskStore 创建 GenerationTaskStore。
func NewGenerationTaskStore(db *ent.Client) *GenerationTaskStore {
	return &GenerationTaskStore{db: db}
}

func generationTaskPredicate() predicate.Task {
	// *.generate 自动覆盖后续新增的音频、文档等生成任务；image.edit 和
	// video.api 是当前两个不以 .generate 结尾的生成链路。
	return enttask.Or(
		enttask.TaskTypeHasSuffix(".generate"),
		enttask.TaskTypeEQ("image.edit"),
		enttask.TaskTypeEQ("video.api"),
	)
}

func generationKind(taskType string) string {
	kind, _, _ := strings.Cut(taskType, ".")
	return kind
}

// List 按创建时间倒序分页查询生成任务。
func (s *GenerationTaskStore) List(ctx context.Context, filter appgenerationtask.ListFilter) ([]appgenerationtask.Task, int64, error) {
	query := s.db.Task.Query().Where(generationTaskPredicate())
	if filter.Status != "" {
		query = query.Where(enttask.StatusEQ(enttask.Status(filter.Status)))
	}
	if filter.Kind != "" {
		query = query.Where(enttask.TaskTypeHasPrefix(filter.Kind + "."))
	}
	if filter.PluginID != "" {
		query = query.Where(enttask.PluginIDEQ(filter.PluginID))
	}
	if filter.TaskType != "" {
		query = query.Where(enttask.TaskTypeEQ(filter.TaskType))
	}
	if filter.UserID != nil {
		query = query.Where(enttask.UserIDEQ(*filter.UserID))
	}

	total, err := query.Clone().Count(ctx)
	if err != nil {
		return nil, 0, err
	}
	rows, err := query.
		Order(ent.Desc(enttask.FieldCreatedAt), ent.Desc(enttask.FieldID)).
		Offset((filter.Page - 1) * filter.PageSize).
		Limit(filter.PageSize).
		All(ctx)
	if err != nil {
		return nil, 0, err
	}

	list := make([]appgenerationtask.Task, 0, len(rows))
	for _, row := range rows {
		item := appgenerationtask.Task{
			ID:                  row.ID,
			PluginID:            row.PluginID,
			TaskType:            row.TaskType,
			Kind:                generationKind(row.TaskType),
			Model:               taskModel(row.Input),
			Status:              row.Status.String(),
			Stage:               row.Stage,
			UserID:              row.UserID,
			Progress:            row.Progress,
			Attempts:            row.Attempts,
			MaxAttempts:         row.MaxAttempts,
			ErrorType:           row.ErrorType,
			ErrorCode:           row.ErrorCode,
			ErrorMessage:        row.ErrorMessage,
			CreatedAt:           row.CreatedAt,
			UpdatedAt:           row.UpdatedAt,
			StartedAt:           row.StartedAt,
			CompletedAt:         row.CompletedAt,
			UpstreamCreatedAt:   taskExecutionTime(row.Execution, "upstream_created_at"),
			UpstreamCompletedAt: taskExecutionTime(row.Execution, "upstream_completed_at"),
		}
		if row.PublicTaskID != nil {
			item.PublicTaskID = *row.PublicTaskID
		}
		list = append(list, item)
	}
	s.fillUserEmails(ctx, list)
	return list, int64(total), nil
}

func taskExecutionTime(execution map[string]interface{}, key string) *time.Time {
	raw, _ := execution[key].(string)
	parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(raw))
	if err != nil {
		return nil
	}
	return &parsed
}

func taskModel(input map[string]interface{}) string {
	model, _ := input["model"].(string)
	return strings.TrimSpace(model)
}

func (s *GenerationTaskStore) fillUserEmails(ctx context.Context, list []appgenerationtask.Task) {
	ids := make([]int, 0, len(list))
	seen := make(map[int]bool, len(list))
	for _, item := range list {
		if item.UserID > 0 && !seen[item.UserID] {
			seen[item.UserID] = true
			ids = append(ids, item.UserID)
		}
	}
	if len(ids) == 0 {
		return
	}
	users, err := s.db.User.Query().Where(entuser.IDIn(ids...)).All(ctx)
	if err != nil {
		return
	}
	emailByID := make(map[int]string, len(users))
	for _, user := range users {
		emailByID[user.ID] = user.Email
	}
	for i := range list {
		list[i].UserEmail = emailByID[list[i].UserID]
	}
}

// Summary 聚合当前队列健康、最近终态和筛选项。
func (s *GenerationTaskStore) Summary(ctx context.Context, recentSince, backlogBefore, staleBefore time.Time) (appgenerationtask.SummarySnapshot, error) {
	base := s.db.Task.Query().Where(generationTaskPredicate())
	result := appgenerationtask.SummarySnapshot{}

	var currentRows []struct {
		Status string `json:"status"`
		Count  int    `json:"count"`
	}
	if err := base.Clone().
		Where(enttask.StatusIn(enttask.StatusPending, enttask.StatusProcessing, enttask.StatusRetrying, enttask.StatusCancelling)).
		GroupBy(enttask.FieldStatus).
		Aggregate(ent.Count()).
		Scan(ctx, &currentRows); err != nil {
		return result, err
	}
	for _, row := range currentRows {
		switch enttask.Status(row.Status) {
		case enttask.StatusPending:
			result.Pending = int64(row.Count)
		case enttask.StatusProcessing:
			result.Processing = int64(row.Count)
		case enttask.StatusRetrying:
			result.Retrying = int64(row.Count)
		case enttask.StatusCancelling:
			result.Cancelling = int64(row.Count)
		}
	}

	var recentRows []struct {
		Status string `json:"status"`
		Count  int    `json:"count"`
	}
	if err := base.Clone().
		Where(
			enttask.CompletedAtNotNil(),
			enttask.CompletedAtGTE(recentSince),
			enttask.StatusIn(enttask.StatusCompleted, enttask.StatusFailed, enttask.StatusCancelled),
		).
		GroupBy(enttask.FieldStatus).
		Aggregate(ent.Count()).
		Scan(ctx, &recentRows); err != nil {
		return result, err
	}
	for _, row := range recentRows {
		switch enttask.Status(row.Status) {
		case enttask.StatusCompleted:
			result.CompletedRecent = int64(row.Count)
		case enttask.StatusFailed:
			result.FailedRecent = int64(row.Count)
		case enttask.StatusCancelled:
			result.CancelledRecent = int64(row.Count)
		}
	}

	backlog, err := base.Clone().Where(
		enttask.StatusIn(enttask.StatusPending, enttask.StatusRetrying),
		enttask.CreatedAtLT(backlogBefore),
	).Count(ctx)
	if err != nil {
		return result, err
	}
	result.Backlog = int64(backlog)

	stale, err := base.Clone().Where(
		enttask.StatusEQ(enttask.StatusProcessing),
		enttask.UpdatedAtLT(staleBefore),
	).Count(ctx)
	if err != nil {
		return result, err
	}
	result.StaleProcessing = int64(stale)

	oldest, err := base.Clone().
		Where(enttask.StatusIn(enttask.StatusPending, enttask.StatusRetrying)).
		Order(ent.Asc(enttask.FieldCreatedAt), ent.Asc(enttask.FieldID)).
		First(ctx)
	if err == nil {
		oldestAt := oldest.CreatedAt
		result.OldestQueuedAt = &oldestAt
	} else if !ent.IsNotFound(err) {
		return result, err
	}

	if err := base.Clone().Unique(true).Select(enttask.FieldPluginID).Scan(ctx, &result.Plugins); err != nil {
		return result, err
	}
	if err := base.Clone().Unique(true).Select(enttask.FieldTaskType).Scan(ctx, &result.TaskTypes); err != nil {
		return result, err
	}
	sort.Strings(result.Plugins)
	sort.Strings(result.TaskTypes)
	return result, nil
}
