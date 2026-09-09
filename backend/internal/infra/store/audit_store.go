package store

import (
	"context"
	"time"

	"github.com/DouDOU-start/airgate-core/ent"
	entaudit "github.com/DouDOU-start/airgate-core/ent/teamauditlog"
	appaudit "github.com/DouDOU-start/airgate-core/internal/app/audit"
	"github.com/DouDOU-start/airgate-core/internal/pkg/timezone"
)

// TeamAuditStore 使用 Ent 实现团队审计仓储。
type TeamAuditStore struct {
	db *ent.Client
}

// NewTeamAuditStore 创建团队审计仓储。
func NewTeamAuditStore(db *ent.Client) *TeamAuditStore {
	return &TeamAuditStore{db: db}
}

// Create 写一条审计。
func (s *TeamAuditStore) Create(ctx context.Context, entry appaudit.Entry) error {
	builder := s.db.TeamAuditLog.Create().
		SetOwnerID(entry.OwnerID).
		SetActorUserID(entry.ActorUserID).
		SetActorEmail(entry.ActorEmail).
		SetAction(entry.Action).
		SetTargetType(entry.TargetType).
		SetTargetID(entry.TargetID).
		SetTargetName(entry.TargetName).
		SetIP(entry.IP).
		SetRequestID(entry.RequestID)
	if entry.Before != nil {
		builder.SetBefore(entry.Before)
	}
	if entry.After != nil {
		builder.SetAfter(entry.After)
	}
	if !entry.CreatedAt.IsZero() {
		builder.SetCreatedAt(entry.CreatedAt)
	}
	return builder.Exec(ctx)
}

// List 分页查询（按时间倒序）。
func (s *TeamAuditStore) List(ctx context.Context, filter appaudit.ListFilter) ([]appaudit.Entry, int64, error) {
	query := s.db.TeamAuditLog.Query()
	if filter.OwnerID > 0 {
		query = query.Where(entaudit.OwnerIDEQ(filter.OwnerID))
	}
	if filter.TargetType != "" {
		query = query.Where(entaudit.TargetTypeEQ(filter.TargetType))
	}
	if filter.TargetID > 0 {
		query = query.Where(entaudit.TargetIDEQ(filter.TargetID))
	}
	if filter.Action != "" {
		query = query.Where(entaudit.ActionEQ(filter.Action))
	}
	loc := timezone.Resolve(filter.TZ)
	if filter.StartDate != "" {
		if parsed, _, err := timezone.ParseDateOrDateTime(filter.StartDate, loc); err == nil {
			query = query.Where(entaudit.CreatedAtGTE(parsed))
		}
	}
	if filter.EndDate != "" {
		if parsed, withTime, err := timezone.ParseDateOrDateTime(filter.EndDate, loc); err == nil {
			if withTime {
				query = query.Where(entaudit.CreatedAtLT(parsed.Add(time.Second)))
			} else {
				query = query.Where(entaudit.CreatedAtLT(parsed.AddDate(0, 0, 1)))
			}
		}
	}
	total, err := query.Count(ctx)
	if err != nil {
		return nil, 0, err
	}
	rows, err := query.
		Order(ent.Desc(entaudit.FieldCreatedAt), ent.Desc(entaudit.FieldID)).
		Offset((filter.Page - 1) * filter.PageSize).
		Limit(filter.PageSize).
		All(ctx)
	if err != nil {
		return nil, 0, err
	}
	out := make([]appaudit.Entry, 0, len(rows))
	for _, row := range rows {
		out = append(out, appaudit.Entry{
			ID:          row.ID,
			OwnerID:     row.OwnerID,
			ActorUserID: row.ActorUserID,
			ActorEmail:  row.ActorEmail,
			Action:      row.Action,
			TargetType:  row.TargetType,
			TargetID:    row.TargetID,
			TargetName:  row.TargetName,
			Before:      row.Before,
			After:       row.After,
			IP:          row.IP,
			RequestID:   row.RequestID,
			CreatedAt:   row.CreatedAt,
		})
	}
	return out, int64(total), nil
}

var _ appaudit.Repository = (*TeamAuditStore)(nil)
