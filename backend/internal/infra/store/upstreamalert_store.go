package store

import (
	"context"

	"github.com/DouDOU-start/airgate-core/ent"
	entaccount "github.com/DouDOU-start/airgate-core/ent/account"
	entuser "github.com/DouDOU-start/airgate-core/ent/user"
	appupstreamalert "github.com/DouDOU-start/airgate-core/internal/app/upstreamalert"
)

// UpstreamAlertStore 上游欠费预警的读仓储。
type UpstreamAlertStore struct {
	db *ent.Client
}

// NewUpstreamAlertStore 创建上游欠费预警读仓储。
func NewUpstreamAlertStore(db *ent.Client) *UpstreamAlertStore {
	return &UpstreamAlertStore{db: db}
}

// Account 取账号展示信息。
func (s *UpstreamAlertStore) Account(ctx context.Context, id int) (appupstreamalert.Account, bool, error) {
	row, err := s.db.Account.Query().
		Where(entaccount.IDEQ(id)).
		Select(entaccount.FieldID, entaccount.FieldName, entaccount.FieldPlatform).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return appupstreamalert.Account{}, false, nil
		}
		return appupstreamalert.Account{}, false, err
	}
	return appupstreamalert.Account{ID: row.ID, Name: row.Name, Platform: row.Platform}, true, nil
}

// AdminUserIDs 取全部启用中的管理员 id，按 id 升序。
func (s *UpstreamAlertStore) AdminUserIDs(ctx context.Context) ([]int, error) {
	return s.db.User.Query().
		Where(entuser.RoleEQ(entuser.RoleAdmin), entuser.StatusEQ(entuser.StatusActive)).
		Order(ent.Asc(entuser.FieldID)).
		IDs(ctx)
}
