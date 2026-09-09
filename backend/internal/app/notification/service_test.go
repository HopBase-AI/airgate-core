package notification

import (
	"context"
	"errors"
	"testing"
	"time"
)

// stubRepo 记录调用参数，按 dupKeys 模拟唯一约束冲突。
type stubRepo struct {
	created    []CreateInput
	dupKeys    map[string]bool
	listFilter ListFilter
	listUser   int
	readUser   int
	readIDs    []int
	allUser    int
	err        error
}

func (s *stubRepo) Create(_ context.Context, in CreateInput) error {
	if s.err != nil {
		return s.err
	}
	if in.DedupeKey != "" && s.dupKeys[in.DedupeKey] {
		return ErrDuplicate
	}
	if s.dupKeys == nil {
		s.dupKeys = map[string]bool{}
	}
	if in.DedupeKey != "" {
		s.dupKeys[in.DedupeKey] = true
	}
	s.created = append(s.created, in)
	return nil
}
func (s *stubRepo) List(_ context.Context, userID int, f ListFilter) ([]Notification, int64, error) {
	s.listUser, s.listFilter = userID, f
	return nil, 0, s.err
}
func (s *stubRepo) CountUnread(_ context.Context, userID int) (int64, error) { return 3, s.err }
func (s *stubRepo) MarkRead(_ context.Context, userID int, ids []int, _ time.Time) (int, error) {
	s.readUser, s.readIDs = userID, ids
	return len(ids), s.err
}
func (s *stubRepo) MarkAllRead(_ context.Context, userID int, _ time.Time) (int, error) {
	s.allUser = userID
	return 5, s.err
}

func TestServiceCreate(t *testing.T) {
	cases := []struct {
		name        string
		in          CreateInput
		prepared    map[string]bool
		wantCreated bool
		wantErr     error
		wantLevel   string
	}{
		{"正常投递，level 缺省 info", CreateInput{UserID: 1, Kind: KindSystem, Title: "t"}, nil, true, nil, LevelInfo},
		{"dedupe 命中 → created=false 且不报错", CreateInput{UserID: 1, Kind: KindQuotaAlert, Title: "t", DedupeKey: "k1"}, map[string]bool{"k1": true}, false, nil, ""},
		{"缺 user → ErrInvalidInput", CreateInput{Kind: KindSystem, Title: "t"}, nil, false, ErrInvalidInput, ""},
		{"缺 title → ErrInvalidInput", CreateInput{UserID: 1, Kind: KindSystem}, nil, false, ErrInvalidInput, ""},
		{"非法 level → ErrInvalidInput", CreateInput{UserID: 1, Kind: KindSystem, Title: "t", Level: "fatal"}, nil, false, ErrInvalidInput, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &stubRepo{dupKeys: tc.prepared}
			created, err := NewService(repo).Create(context.Background(), tc.in)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if created != tc.wantCreated {
				t.Fatalf("created = %v, want %v", created, tc.wantCreated)
			}
			if tc.wantLevel != "" && repo.created[0].Level != tc.wantLevel {
				t.Fatalf("level = %q, want %q", repo.created[0].Level, tc.wantLevel)
			}
		})
	}
}

// 同一 dedupe_key 第二次投递被拦下：只有首次 created=true。
func TestServiceCreateDedupeSecondCall(t *testing.T) {
	repo := &stubRepo{}
	svc := NewService(repo)
	in := CreateInput{UserID: 7, Kind: KindQuotaAlert, Title: "t", DedupeKey: "member:1:100:warning:7"}
	first, err := svc.Create(context.Background(), in)
	if err != nil || !first {
		t.Fatalf("first = (%v, %v), want (true, nil)", first, err)
	}
	second, err := svc.Create(context.Background(), in)
	if err != nil || second {
		t.Fatalf("second = (%v, %v), want (false, nil)", second, err)
	}
	if len(repo.created) != 1 {
		t.Fatalf("created rows = %d, want 1", len(repo.created))
	}
}

func TestServiceListMineNormalizesAndCapsPageSize(t *testing.T) {
	cases := []struct {
		name         string
		filter       ListFilter
		wantPage     int
		wantPageSize int
	}{
		{"零值取默认", ListFilter{}, 1, 20},
		{"超过上限夹到 100", ListFilter{Page: 2, PageSize: 500}, 2, MaxPageSize},
		{"正常透传", ListFilter{Page: 3, PageSize: 50, UnreadOnly: true}, 3, 50},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &stubRepo{}
			result, err := NewService(repo).ListMine(context.Background(), 9, tc.filter)
			if err != nil {
				t.Fatalf("ListMine: %v", err)
			}
			if repo.listUser != 9 {
				t.Fatalf("list user = %d, want 9", repo.listUser)
			}
			if result.Page != tc.wantPage || result.PageSize != tc.wantPageSize {
				t.Fatalf("page = (%d, %d), want (%d, %d)", result.Page, result.PageSize, tc.wantPage, tc.wantPageSize)
			}
			if repo.listFilter.UnreadOnly != tc.filter.UnreadOnly {
				t.Fatalf("unread_only 未透传")
			}
			if result.List == nil {
				t.Fatalf("空列表须为 [] 而非 nil")
			}
		})
	}
}

// MarkRead / MarkAllRead 以会话用户为范围传给仓储；空 ids 不触达仓储。
func TestServiceMarkReadScopesToUser(t *testing.T) {
	repo := &stubRepo{}
	svc := NewService(repo)
	ctx := context.Background()
	if n, err := svc.MarkRead(ctx, 4, nil); err != nil || n != 0 || repo.readUser != 0 {
		t.Fatalf("empty ids: n=%d err=%v repoUser=%d", n, err, repo.readUser)
	}
	n, err := svc.MarkRead(ctx, 4, []int{10, 11})
	if err != nil || n != 2 {
		t.Fatalf("MarkRead = (%d, %v), want (2, nil)", n, err)
	}
	if repo.readUser != 4 || len(repo.readIDs) != 2 {
		t.Fatalf("repo got user=%d ids=%v", repo.readUser, repo.readIDs)
	}
	if n, err := svc.MarkAllRead(ctx, 4); err != nil || n != 5 || repo.allUser != 4 {
		t.Fatalf("MarkAllRead = (%d, %v) user=%d", n, err, repo.allUser)
	}
	if c, err := svc.UnreadCount(ctx, 4); err != nil || c != 3 {
		t.Fatalf("UnreadCount = (%d, %v)", c, err)
	}
}
