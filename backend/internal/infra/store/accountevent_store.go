package store

import (
	"context"

	"github.com/DouDOU-start/airgate-core/ent"
	entaccount "github.com/DouDOU-start/airgate-core/ent/account"
	entaccountevent "github.com/DouDOU-start/airgate-core/ent/accountevent"
	entapikey "github.com/DouDOU-start/airgate-core/ent/apikey"
	entgroup "github.com/DouDOU-start/airgate-core/ent/group"
	entuser "github.com/DouDOU-start/airgate-core/ent/user"
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
			UserID:         row.UserID,
			APIKeyID:       row.APIKeyID,
		}
		if acc := row.Edges.Account; acc != nil {
			item.AccountID = acc.ID
			item.AccountName = acc.Name
			item.Platform = acc.Platform
		}
		list = append(list, item)
	}
	s.fillActorNames(ctx, list)
	return list, int64(total), nil
}

// fillActorNames 批量联查触发者的邮箱与密钥名。事件表只存 ID（14 天保留的观测
// 数据不建外键边），展示时按页内去重 ID 各查一次；用户/密钥已删除时留空。
// 查询失败只降级为不展示名字，不影响事件列表本身。
func (s *AccountEventStore) fillActorNames(ctx context.Context, list []appaccountevent.Event) {
	userIDs := make([]int, 0, len(list))
	keyIDs := make([]int, 0, len(list))
	seenUser := map[int]bool{}
	seenKey := map[int]bool{}
	for _, item := range list {
		if item.UserID > 0 && !seenUser[item.UserID] {
			seenUser[item.UserID] = true
			userIDs = append(userIDs, item.UserID)
		}
		if item.APIKeyID > 0 && !seenKey[item.APIKeyID] {
			seenKey[item.APIKeyID] = true
			keyIDs = append(keyIDs, item.APIKeyID)
		}
	}

	emailByID := map[int]string{}
	if len(userIDs) > 0 {
		users, err := s.db.User.Query().Where(entuser.IDIn(userIDs...)).All(ctx)
		if err == nil {
			for _, u := range users {
				emailByID[u.ID] = u.Email
			}
		}
	}
	keyNameByID := map[int]string{}
	if len(keyIDs) > 0 {
		keys, err := s.db.APIKey.Query().Where(entapikey.IDIn(keyIDs...)).All(ctx)
		if err == nil {
			for _, k := range keys {
				keyNameByID[k.ID] = k.Name
			}
		}
	}

	for i := range list {
		list[i].UserEmail = emailByID[list[i].UserID]
		list[i].APIKeyName = keyNameByID[list[i].APIKeyID]
	}
}
