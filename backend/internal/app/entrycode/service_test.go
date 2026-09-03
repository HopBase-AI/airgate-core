package entrycode

import (
	"context"
	"testing"

	appsettings "github.com/DouDOU-start/airgate-core/internal/app/settings"
)

// memRepo 内存 settings 仓储:在 map 里持久化单键,复现真实读改写循环。
type memRepo struct{ store map[string]string }

func (m *memRepo) List(_ context.Context, group string) ([]appsettings.Setting, error) {
	out := []appsettings.Setting{}
	for k, v := range m.store {
		out = append(out, appsettings.Setting{Key: k, Value: v, Group: group})
	}
	return out, nil
}
func (m *memRepo) UpsertMany(_ context.Context, items []appsettings.ItemInput) error {
	for _, it := range items {
		m.store[it.Key] = it.Value
	}
	return nil
}

func newTestService() *Service {
	repo := &memRepo{store: map[string]string{}}
	// users=nil:仅测未绑定路径;绑定路径单独在 resolveUser 里以 userID<=0 短路验证。
	return NewService(appsettings.NewService(repo, ""), nil)
}

func TestCreateListUpdateDelete(t *testing.T) {
	s := newTestService()
	ctx := context.Background()

	a, err := s.Create(ctx, CreateInput{Note: "客户甲"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if len(a.Code) != codeLength || !a.Enabled {
		t.Fatalf("生成的码不合规: %+v", a)
	}
	b, _ := s.Create(ctx, CreateInput{Note: "客户乙"})
	if a.Code == b.Code {
		t.Fatal("两个码不应相同")
	}

	list, err := s.List(ctx)
	if err != nil || len(list) != 2 {
		t.Fatalf("list = %d,%v", len(list), err)
	}

	note := "改名"
	off := false
	upd, err := s.Update(ctx, a.Code, UpdateInput{Note: &note, Enabled: &off})
	if err != nil || upd.Note != "改名" || upd.Enabled {
		t.Fatalf("update 未生效: %+v %v", upd, err)
	}

	if err := s.Delete(ctx, a.Code); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := s.Update(ctx, a.Code, UpdateInput{}); err != ErrNotFound {
		t.Fatalf("已删除的码应 ErrNotFound,got %v", err)
	}
	list, _ = s.List(ctx)
	if len(list) != 1 || list[0].Code != b.Code {
		t.Fatalf("删除后应只剩 b: %+v", list)
	}
}

func TestBindUserZeroIsUnbound(t *testing.T) {
	s := newTestService()
	ec, err := s.Create(context.Background(), CreateInput{UserID: 0})
	if err != nil || ec.UserID != 0 || ec.UserEmail != "" {
		t.Fatalf("userID=0 应视为未绑定: %+v %v", ec, err)
	}
}

func TestGenerateCodeAvoidsCollision(t *testing.T) {
	existing := []EntryCode{{Code: "aaaaaaaaaaaa"}}
	if c := generateCode(existing); c == "aaaaaaaaaaaa" || len(c) != codeLength {
		t.Fatalf("码冲突或长度错: %q", c)
	}
}
