package handler

import (
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"

	appgenerationtask "github.com/DouDOU-start/airgate-core/internal/app/generationtask"
	"github.com/DouDOU-start/airgate-core/internal/server/dto"
	"github.com/DouDOU-start/airgate-core/internal/server/response"
)

// GenerationTaskHandler 是管理员生成任务监控 Handler。
type GenerationTaskHandler struct {
	service *appgenerationtask.Service
}

// NewGenerationTaskHandler 创建 GenerationTaskHandler。
func NewGenerationTaskHandler(service *appgenerationtask.Service) *GenerationTaskHandler {
	return &GenerationTaskHandler{service: service}
}

// ListGenerationTasks 分页查询生成任务。
func (h *GenerationTaskHandler) ListGenerationTasks(c *gin.Context) {
	var page dto.PageReq
	if err := c.ShouldBindQuery(&page); err != nil {
		response.BindError(c, err)
		return
	}

	result, err := h.service.List(c.Request.Context(), appgenerationtask.ListFilter{
		Page:     page.Page,
		PageSize: page.PageSize,
		Status:   c.Query("status"),
		Kind:     c.Query("kind"),
		PluginID: c.Query("plugin_id"),
		TaskType: c.Query("task_type"),
		UserID:   parseOptionalInt(c.Query("user_id")),
	})
	if err != nil {
		slog.Error("查询生成任务失败", "error", err)
		response.InternalError(c, "查询生成任务失败")
		return
	}

	list := make([]dto.GenerationTaskResp, 0, len(result.List))
	for _, item := range result.List {
		list = append(list, toGenerationTaskResp(item))
	}
	response.Success(c, response.PagedData(list, result.Total, result.Page, result.PageSize))
}

// GenerationTaskSummary 查询生成队列健康摘要。
func (h *GenerationTaskHandler) GenerationTaskSummary(c *gin.Context) {
	result, err := h.service.Summary(c.Request.Context())
	if err != nil {
		slog.Error("查询生成任务摘要失败", "error", err)
		response.InternalError(c, "查询生成任务摘要失败")
		return
	}
	response.Success(c, toGenerationTaskSummaryResp(result))
}

func toGenerationTaskResp(item appgenerationtask.Task) dto.GenerationTaskResp {
	resp := dto.GenerationTaskResp{
		ID:           item.ID,
		PublicTaskID: item.PublicTaskID,
		PluginID:     item.PluginID,
		TaskType:     item.TaskType,
		Kind:         item.Kind,
		Model:        item.Model,
		Status:       item.Status,
		Stage:        item.Stage,
		UserID:       item.UserID,
		UserEmail:    item.UserEmail,
		Progress:     item.Progress,
		Attempts:     item.Attempts,
		MaxAttempts:  item.MaxAttempts,
		ErrorType:    item.ErrorType,
		ErrorCode:    item.ErrorCode,
		ErrorMessage: item.ErrorMessage,
		CreatedAt:    item.CreatedAt.UTC().Format(time.RFC3339Nano),
		UpdatedAt:    item.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
	if item.StartedAt != nil {
		resp.StartedAt = item.StartedAt.UTC().Format(time.RFC3339Nano)
	}
	if item.CompletedAt != nil {
		resp.CompletedAt = item.CompletedAt.UTC().Format(time.RFC3339Nano)
	}
	return resp
}

func toGenerationTaskSummaryResp(item appgenerationtask.Summary) dto.GenerationTaskSummaryResp {
	resp := dto.GenerationTaskSummaryResp{
		Pending:                 item.Pending,
		Processing:              item.Processing,
		Retrying:                item.Retrying,
		Cancelling:              item.Cancelling,
		Queued:                  item.Queued,
		Active:                  item.Active,
		CompletedRecent:         item.CompletedRecent,
		FailedRecent:            item.FailedRecent,
		CancelledRecent:         item.CancelledRecent,
		FailureRateRecent:       item.FailureRateRecent,
		Backlog:                 item.Backlog,
		StaleProcessing:         item.StaleProcessing,
		RecentWindowSeconds:     item.RecentWindowSeconds,
		BacklogThresholdSeconds: item.BacklogThresholdSeconds,
		StaleThresholdSeconds:   item.StaleThresholdSeconds,
		Plugins:                 item.Plugins,
		TaskTypes:               item.TaskTypes,
	}
	if item.OldestQueuedAt != nil {
		resp.OldestQueuedAt = item.OldestQueuedAt.UTC().Format(time.RFC3339Nano)
	}
	return resp
}
