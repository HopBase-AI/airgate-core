package group

import (
	"context"
	"testing"
	"time"
)

func TestListNormalizesPagination(t *testing.T) {
	var captured ListFilter

	service := NewService(groupStubRepository{
		list: func(_ context.Context, filter ListFilter) ([]Group, int64, error) {
			captured = filter
			return nil, 0, nil
		},
	}, stubConcurrencyReader{})

	result, err := service.List(t.Context(), ListFilter{})
	if err != nil {
		t.Fatalf("List() returned error: %v", err)
	}
	if captured.Page != 1 || captured.PageSize != 20 {
		t.Fatalf("List() normalized filter = %+v, want page=1 pageSize=20", captured)
	}
	if result.Page != 1 || result.PageSize != 20 {
		t.Fatalf("List() result pagination = %+v, want page=1 pageSize=20", result)
	}
}

func TestCreateClonesMutableFields(t *testing.T) {
	var captured CreateInput

	service := NewService(groupStubRepository{
		create: func(_ context.Context, input CreateInput) (Group, error) {
			captured = input
			return Group{ID: 1}, nil
		},
	}, stubConcurrencyReader{})

	quotas := map[string]any{"day": float64(100)}
	routing := map[string][]int64{"gpt-*": {1, 2}}

	_, err := service.Create(t.Context(), CreateInput{
		Name:             "默认分组",
		Platform:         "openai",
		SubscriptionType: "standard",
		Quotas:           quotas,
		ModelRouting:     routing,
	})
	if err != nil {
		t.Fatalf("Create() returned error: %v", err)
	}

	quotas["day"] = float64(200)
	routing["gpt-*"][0] = 99

	if captured.Quotas["day"] != float64(100) {
		t.Fatalf("captured quotas mutated to %v, want 100", captured.Quotas["day"])
	}
	if captured.ModelRouting["gpt-*"][0] != 1 {
		t.Fatalf("captured model routing mutated to %v, want 1", captured.ModelRouting["gpt-*"][0])
	}
}

func TestCreateSanitizesI18nMaps(t *testing.T) {
	var captured CreateInput

	service := NewService(groupStubRepository{
		create: func(_ context.Context, input CreateInput) (Group, error) {
			captured = input
			return Group{ID: 1}, nil
		},
	}, stubConcurrencyReader{})

	source := map[string]string{"en": " Default Group ", "zh-HK": "   ", "ja": ""}
	_, err := service.Create(t.Context(), CreateInput{
		Name:             "默认分组",
		Platform:         "openai",
		SubscriptionType: "standard",
		NameI18n:         source,
	})
	if err != nil {
		t.Fatalf("Create() returned error: %v", err)
	}

	// 空白 value 剔除、保留值去首尾空白，且与入参 map 解耦。
	if len(captured.NameI18n) != 1 || captured.NameI18n["en"] != "Default Group" {
		t.Fatalf("captured name_i18n = %+v, want 仅 en=Default Group", captured.NameI18n)
	}
	source["en"] = "mutated"
	if captured.NameI18n["en"] != "Default Group" {
		t.Fatalf("captured name_i18n 被入参突变污染: %+v", captured.NameI18n)
	}
	if captured.NoteI18n != nil {
		t.Fatalf("未提交的 note_i18n 应保持 nil, got %+v", captured.NoteI18n)
	}
}

func TestUpdateSanitizesI18nMapsKeepsNilSemantics(t *testing.T) {
	var captured UpdateInput

	service := NewService(groupStubRepository{
		update: func(_ context.Context, _ int, input UpdateInput) (Group, error) {
			captured = input
			return Group{ID: 1}, nil
		},
	}, stubConcurrencyReader{})

	// name_i18n 未提交（nil=不修改）；note_i18n 提交但全空白（清理后应为非 nil 空 map=清空）。
	_, err := service.Update(t.Context(), 1, UpdateInput{
		NoteI18n: map[string]string{"en": "  ", "ja": ""},
	})
	if err != nil {
		t.Fatalf("Update() returned error: %v", err)
	}
	if captured.NameI18n != nil {
		t.Fatalf("未提交的 name_i18n 应保持 nil, got %+v", captured.NameI18n)
	}
	if captured.NoteI18n == nil || len(captured.NoteI18n) != 0 {
		t.Fatalf("全空白 note_i18n 应清理为非 nil 空 map, got %+v", captured.NoteI18n)
	}
}

type stubConcurrencyReader struct{}

func (stubConcurrencyReader) GetCurrentCounts(_ context.Context, _ []int) map[int]int {
	return nil
}

type groupStubRepository struct {
	list           func(context.Context, ListFilter) ([]Group, int64, error)
	listAvailable  func(context.Context, AvailableFilter) ([]Group, int64, error)
	findByID       func(context.Context, int) (Group, error)
	create         func(context.Context, CreateInput) (Group, error)
	update         func(context.Context, int, UpdateInput) (Group, error)
	delete         func(context.Context, int) error
	statsForGroups func(context.Context, []int) (map[int]GroupStats, map[int][]AccountCapacity, error)
}

func (s groupStubRepository) List(ctx context.Context, filter ListFilter) ([]Group, int64, error) {
	if s.list == nil {
		return nil, 0, nil
	}
	return s.list(ctx, filter)
}

func (s groupStubRepository) ListAvailable(ctx context.Context, filter AvailableFilter) ([]Group, int64, error) {
	if s.listAvailable == nil {
		return nil, 0, nil
	}
	return s.listAvailable(ctx, filter)
}

func (s groupStubRepository) FindByID(ctx context.Context, id int) (Group, error) {
	if s.findByID == nil {
		return Group{}, nil
	}
	return s.findByID(ctx, id)
}

func (s groupStubRepository) Create(ctx context.Context, input CreateInput) (Group, error) {
	if s.create == nil {
		return Group{}, nil
	}
	return s.create(ctx, input)
}

func (s groupStubRepository) Update(ctx context.Context, id int, input UpdateInput) (Group, error) {
	if s.update == nil {
		return Group{}, nil
	}
	return s.update(ctx, id, input)
}

func (s groupStubRepository) Delete(ctx context.Context, id int) error {
	if s.delete == nil {
		return nil
	}
	return s.delete(ctx, id)
}

func (s groupStubRepository) StatsForGroups(ctx context.Context, groupIDs []int, _ time.Time) (map[int]GroupStats, map[int][]AccountCapacity, error) {
	if s.statsForGroups == nil {
		return nil, nil, nil
	}
	return s.statsForGroups(ctx, groupIDs)
}
