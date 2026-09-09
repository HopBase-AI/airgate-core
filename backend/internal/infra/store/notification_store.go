package store

import (
	"context"
	"time"

	"github.com/DouDOU-start/airgate-core/ent"
	entnotification "github.com/DouDOU-start/airgate-core/ent/usernotification"
	appnotification "github.com/DouDOU-start/airgate-core/internal/app/notification"
)

// NotificationStore 使用 Ent 实现站内通知仓储。
type NotificationStore struct {
	db *ent.Client
}

// NewNotificationStore 创建站内通知仓储。
func NewNotificationStore(db *ent.Client) *NotificationStore {
	return &NotificationStore{db: db}
}

// Create 写入一条通知；dedupe_key 撞唯一约束返回 appnotification.ErrDuplicate。
func (s *NotificationStore) Create(ctx context.Context, in appnotification.CreateInput) error {
	builder := s.db.UserNotification.Create().
		SetUserID(in.UserID).
		SetKind(in.Kind).
		SetLevel(entnotification.Level(in.Level)).
		SetTitle(in.Title).
		SetContent(in.Content).
		SetLink(in.Link)
	if in.DedupeKey != "" {
		builder.SetDedupeKey(in.DedupeKey)
	}
	if err := builder.Exec(ctx); err != nil {
		if in.DedupeKey != "" && ent.IsConstraintError(err) {
			return appnotification.ErrDuplicate
		}
		return err
	}
	return nil
}

// List 该用户的通知（时间倒序）。
func (s *NotificationStore) List(ctx context.Context, userID int, filter appnotification.ListFilter) ([]appnotification.Notification, int64, error) {
	query := s.db.UserNotification.Query().Where(entnotification.UserIDEQ(userID))
	if filter.UnreadOnly {
		query = query.Where(entnotification.ReadAtIsNil())
	}
	total, err := query.Count(ctx)
	if err != nil {
		return nil, 0, err
	}
	rows, err := query.
		Order(ent.Desc(entnotification.FieldCreatedAt), ent.Desc(entnotification.FieldID)).
		Offset((filter.Page - 1) * filter.PageSize).
		Limit(filter.PageSize).
		All(ctx)
	if err != nil {
		return nil, 0, err
	}
	out := make([]appnotification.Notification, 0, len(rows))
	for _, row := range rows {
		out = append(out, toNotification(row))
	}
	return out, int64(total), nil
}

// CountUnread 未读数。
func (s *NotificationStore) CountUnread(ctx context.Context, userID int) (int64, error) {
	n, err := s.db.UserNotification.Query().
		Where(entnotification.UserIDEQ(userID), entnotification.ReadAtIsNil()).
		Count(ctx)
	return int64(n), err
}

// MarkRead 只更新该用户自己名下、未读的指定 id。
func (s *NotificationStore) MarkRead(ctx context.Context, userID int, ids []int, at time.Time) (int, error) {
	return s.db.UserNotification.Update().
		Where(
			entnotification.UserIDEQ(userID),
			entnotification.IDIn(ids...),
			entnotification.ReadAtIsNil(),
		).
		SetReadAt(at).
		Save(ctx)
}

// MarkAllRead 该用户全部未读置为已读。
func (s *NotificationStore) MarkAllRead(ctx context.Context, userID int, at time.Time) (int, error) {
	return s.db.UserNotification.Update().
		Where(entnotification.UserIDEQ(userID), entnotification.ReadAtIsNil()).
		SetReadAt(at).
		Save(ctx)
}

func toNotification(row *ent.UserNotification) appnotification.Notification {
	return appnotification.Notification{
		ID:        row.ID,
		UserID:    row.UserID,
		Kind:      row.Kind,
		Level:     string(row.Level),
		Title:     row.Title,
		Content:   row.Content,
		Link:      row.Link,
		DedupeKey: row.DedupeKey,
		ReadAt:    row.ReadAt,
		CreatedAt: row.CreatedAt,
	}
}

var _ appnotification.Repository = (*NotificationStore)(nil)
