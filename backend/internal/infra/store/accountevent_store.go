package store

import (
	"context"

	"github.com/DouDOU-start/airgate-core/ent"
	entaccount "github.com/DouDOU-start/airgate-core/ent/account"
	entaccountevent "github.com/DouDOU-start/airgate-core/ent/accountevent"
	entgroup "github.com/DouDOU-start/airgate-core/ent/group"
	appaccountevent "github.com/DouDOU-start/airgate-core/internal/app/accountevent"
)

// AccountEventStore 账号异常事件的 ent 实现。
type AccountEventStore struct {
	db *ent.Client
}

// NewAccountEventStore 创建 AccountEventStore。
func NewAccountEventStore(db *ent.Client) *AccountEventStore {
	return &AccountEventStore{db: db}
}

// List 按时间倒序分页查询事件，账号信息随边加载。
func (s *AccountEventStore) List(ctx context.Context, filter appaccountevent.ListFilter) ([]appaccountevent.Event, int64, error) {
	query := s.db.AccountEvent.Query()

	if filter.AccountID != nil {
		query = query.Where(entaccountevent.HasAccountWith(entaccount.IDEQ(*filter.AccountID)))
	}
	if filter.GroupID != nil {
		query = query.Where(entaccountevent.HasAccountWith(entaccount.HasGroupsWith(entgroup.IDEQ(*filter.GroupID))))
	}
	if filter.EventType != "" {
		query = query.Where(entaccountevent.EventTypeEQ(entaccountevent.EventType(filter.EventType)))
	}
	if filter.Platform != "" {
		query = query.Where(entaccountevent.HasAccountWith(entaccount.PlatformEQ(filter.Platform)))
	}

	total, err := query.Count(ctx)
	if err != nil {
		return nil, 0, err
	}

	rows, err := query.
		WithAccount().
		Order(ent.Desc(entaccountevent.FieldCreatedAt), ent.Desc(entaccountevent.FieldID)).
		Offset((filter.Page - 1) * filter.PageSize).
		Limit(filter.PageSize).
		All(ctx)
	if err != nil {
		return nil, 0, err
	}

	list := make([]appaccountevent.Event, 0, len(rows))
	for _, row := range rows {
		item := appaccountevent.Event{
			ID:             row.ID,
			EventType:      row.EventType.String(),
			Reason:         row.Reason,
			Family:         row.Family,
			Source:         row.Source,
			UpstreamStatus: row.UpstreamStatus,
			StateUntil:     row.StateUntil,
			CreatedAt:      row.CreatedAt,
		}
		if acc := row.Edges.Account; acc != nil {
			item.AccountID = acc.ID
			item.AccountName = acc.Name
			item.Platform = acc.Platform
		}
		list = append(list, item)
	}
	return list, int64(total), nil
}
