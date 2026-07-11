package store

import (
	"context"
	"testing"

	_ "github.com/mattn/go-sqlite3"

	entaccountevent "github.com/DouDOU-start/airgate-core/ent/accountevent"
	appaccountevent "github.com/DouDOU-start/airgate-core/internal/app/accountevent"
)

func TestAccountEventStoreListFilters(t *testing.T) {
	db := enttestOpen(t)
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	}()

	ctx := context.Background()
	group, err := db.Group.Create().SetName("测试分组").SetPlatform("claude").Save(ctx)
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	accInGroup, err := db.Account.Create().
		SetName("组内账号").SetPlatform("claude").SetType("oauth").
		SetCredentials(map[string]string{}).AddGroups(group).Save(ctx)
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	accOther, err := db.Account.Create().
		SetName("组外账号").SetPlatform("openai").SetType("oauth").
		SetCredentials(map[string]string{}).Save(ctx)
	if err != nil {
		t.Fatalf("create account: %v", err)
	}

	events := []struct {
		accountID int
		eventType entaccountevent.EventType
		reason    string
	}{
		{accInGroup.ID, entaccountevent.EventTypeDisabled, "401 invalid token"},
		{accInGroup.ID, entaccountevent.EventTypeRateLimited, "429"},
		{accOther.ID, entaccountevent.EventTypeUpstreamError, "502"},
	}
	for _, item := range events {
		if err := db.AccountEvent.Create().
			SetAccountID(item.accountID).
			SetEventType(item.eventType).
			SetReason(item.reason).
			Exec(ctx); err != nil {
			t.Fatalf("create event: %v", err)
		}
	}

	s := NewAccountEventStore(db)

	// 无筛选：全部 3 条，倒序（最后插入的在前）。
	list, total, err := s.List(ctx, appaccountevent.ListFilter{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if total != 3 || len(list) != 3 {
		t.Fatalf("total = %d len = %d, want 3", total, len(list))
	}
	if list[0].AccountName != "组外账号" || list[0].Platform != "openai" {
		t.Fatalf("first event = %+v, want 组外账号/openai（时间倒序）", list[0])
	}

	// 分组筛选：只剩组内账号的 2 条。
	list, total, err = s.List(ctx, appaccountevent.ListFilter{Page: 1, PageSize: 20, GroupID: &group.ID})
	if err != nil {
		t.Fatalf("List by group: %v", err)
	}
	if total != 2 || len(list) != 2 {
		t.Fatalf("group filter total = %d len = %d, want 2", total, len(list))
	}

	// 事件类型 + 分组组合筛选。
	list, total, err = s.List(ctx, appaccountevent.ListFilter{Page: 1, PageSize: 20, GroupID: &group.ID, EventType: "disabled"})
	if err != nil {
		t.Fatalf("List by group+type: %v", err)
	}
	if total != 1 || len(list) != 1 || list[0].Reason != "401 invalid token" {
		t.Fatalf("group+type filter = %+v total = %d, want 1 条 disabled", list, total)
	}

	// 账号筛选。
	list, total, err = s.List(ctx, appaccountevent.ListFilter{Page: 1, PageSize: 20, AccountID: &accOther.ID})
	if err != nil {
		t.Fatalf("List by account: %v", err)
	}
	if total != 1 || len(list) != 1 || list[0].EventType != "upstream_error" {
		t.Fatalf("account filter = %+v total = %d, want 1 条 upstream_error", list, total)
	}
}
