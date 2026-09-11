package handler

import (
	"errors"
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"

	appaudit "github.com/DouDOU-start/airgate-core/internal/app/audit"
	appdepartment "github.com/DouDOU-start/airgate-core/internal/app/department"
	"github.com/DouDOU-start/airgate-core/internal/server/dto"
	"github.com/DouDOU-start/airgate-core/internal/server/middleware"
)

// DepartmentHandler 企业组织（部门）管理 + 企业总览 / 账期 + 团队操作审计：企业主侧。
type DepartmentHandler struct {
	service *appdepartment.Service
	audit   *appaudit.Service
}

// NewDepartmentHandler 创建 DepartmentHandler。
func NewDepartmentHandler(service *appdepartment.Service, audit *appaudit.Service) *DepartmentHandler {
	return &DepartmentHandler{service: service, audit: audit}
}

var parseDepartmentID = ParseID

func (h *DepartmentHandler) handleError(logMessage, publicMessage string, err error) (int, string) {
	switch {
	case errors.Is(err, appdepartment.ErrDepartmentNotFound):
		return 404, err.Error()
	case errors.Is(err, appdepartment.ErrOutOfScope):
		return 403, err.Error()
	case errors.Is(err, appdepartment.ErrNameTaken):
		return 409, err.Error()
	case errors.Is(err, appdepartment.ErrNameRequired),
		errors.Is(err, appdepartment.ErrInvalidQuota),
		errors.Is(err, appdepartment.ErrInvalidQuotaPeriod),
		errors.Is(err, appdepartment.ErrManagerNotInDepartment),
		errors.Is(err, appdepartment.ErrInvalidBillingDay):
		return 400, err.Error()
	default:
		slog.Error(logMessage, "error", err)
		return 500, publicMessage
	}
}

// auditContext 把操作者（会话用户 / 邮箱 / IP / request_id）放进 context，供审计写入补齐。
func auditContext(c *gin.Context) *gin.Context {
	userID, _ := currentUserID(c)
	email := ""
	if v, ok := c.Get(middleware.CtxKeyEmail); ok {
		email, _ = v.(string)
	}
	c.Request = c.Request.WithContext(appaudit.WithActor(c.Request.Context(), appaudit.Actor{
		UserID:    userID,
		Email:     email,
		IP:        c.ClientIP(),
		RequestID: middleware.RequestIDFromGinContext(c),
	}))
	return c
}

func toDepartmentResp(item appdepartment.Department) dto.DepartmentResp {
	resp := dto.DepartmentResp{
		ID:               int64(item.ID),
		Name:             item.Name,
		Note:             item.Note,
		Sort:             item.Sort,
		QuotaUSD:         item.QuotaUSD,
		QuotaPeriod:      item.QuotaPeriod,
		PeriodUsed:       item.PeriodUsed,
		PeriodStart:      item.PeriodStart.Format(time.RFC3339),
		UsedQuota:        item.UsedQuota,
		UsedQuotaActual:  item.UsedQuotaActual,
		MemberCount:      item.MemberCount,
		KeyCount:         item.KeyCount,
		MemberQuotaTotal: item.MemberQuotaTotal,
		TodayCost:        item.TodayCost,
		ThirtyDayCost:    item.ThirtyDayCost,
		ManagerMemberID:  int64(item.ManagerMemberID),
		ManagerName:      item.ManagerName,
		TimeMixin: dto.TimeMixin{
			CreatedAt: item.CreatedAt,
			UpdatedAt: item.UpdatedAt,
		},
	}
	if item.PeriodEnd != nil {
		end := item.PeriodEnd.Format(time.RFC3339)
		resp.PeriodEnd = &end
	}
	return resp
}

func toTeamOverviewResp(o appdepartment.Overview) dto.TeamOverviewResp {
	return dto.TeamOverviewResp{
		Balance:               o.Balance,
		DepartmentCount:       o.DepartmentCount,
		MemberCount:           o.MemberCount,
		DepartmentQuotaTotal:  o.DepartmentQuotaTotal,
		MemberQuotaTotal:      o.MemberQuotaTotal,
		UnassignedMemberQuota: o.UnassignedMemberQuota,
		BillingDay:            o.BillingDay,
		PeriodAnchor:          o.PeriodAnchor.Format(time.RFC3339),
		PeriodStart:           o.PeriodStart.Format(time.RFC3339),
		PeriodEnd:             o.PeriodEnd.Format(time.RFC3339),
		PeriodUsedActual:      o.PeriodUsedActual,
		PeriodUsedBilled:      o.PeriodUsedBilled,
	}
}

func toTeamAuditLogResp(e appaudit.Entry) dto.TeamAuditLogResp {
	return dto.TeamAuditLogResp{
		ID:          int64(e.ID),
		ActorUserID: int64(e.ActorUserID),
		ActorEmail:  e.ActorEmail,
		Action:      e.Action,
		TargetType:  e.TargetType,
		TargetID:    int64(e.TargetID),
		TargetName:  e.TargetName,
		Before:      e.Before,
		After:       e.After,
		IP:          e.IP,
		RequestID:   e.RequestID,
		CreatedAt:   e.CreatedAt.Format(time.RFC3339),
	}
}
