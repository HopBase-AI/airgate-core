package group

import (
	"context"
	"strings"
	"time"
)

// Repository 定义分组域持久化接口。
type Repository interface {
	List(context.Context, ListFilter) ([]Group, int64, error)
	ListAvailable(context.Context, AvailableFilter) ([]Group, int64, error)
	FindByID(context.Context, int) (Group, error)
	Create(context.Context, CreateInput) (Group, error)
	Update(context.Context, int, UpdateInput) (Group, error)
	Delete(context.Context, int) error
	StatsForGroups(ctx context.Context, groupIDs []int, todayStart time.Time) (stats map[int]GroupStats, activeAccounts map[int][]AccountCapacity, err error)
}

// ConcurrencyReader 并发读接口。
type ConcurrencyReader interface {
	GetCurrentCounts(context.Context, []int) map[int]int
}

// GroupStats 描述分组统计信息。
type GroupStats struct {
	AccountActive   int
	AccountError    int
	AccountDisabled int
	AccountTotal    int
	CapacityUsed    int
	CapacityTotal   int
	TodayCost       float64
	TotalCost       float64
}

// AccountCapacity 描述每个分组中活跃账号的容量信息。
type AccountCapacity struct {
	AccountID      int
	MaxConcurrency int
}

// GroupAllowedUser 描述被授权访问某专属分组的用户摘要（仅含展示所需字段）。
type GroupAllowedUser struct {
	ID       int64
	Email    string
	Username string
}

// Group 描述分组领域对象。
type Group struct {
	ID   int
	Name string
	// NameI18n / NoteI18n 展示文案多语言覆盖（键=语言码 en / zh-HK / ja；zh 基准即 Name / Note）。
	NameI18n       map[string]string
	Platform       string
	RateMultiplier float64
	IsExclusive    bool
	StatusVisible  bool
	// AllowedUsers 仅在加载了 allowed_users 边时填充（管理员列表/详情）；
	// 用户可用分组列表不填充，避免泄漏其他用户。
	AllowedUsers      []GroupAllowedUser
	SubscriptionType  string
	Quotas            map[string]any
	ModelRouting      map[string][]int64
	PluginSettings    map[string]map[string]string
	ServiceTier       string
	ForceInstructions string
	Note              string
	NoteI18n          map[string]string
	SortWeight        int
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// ListFilter 描述管理员分组列表查询条件。
type ListFilter struct {
	Page        int
	PageSize    int
	Keyword     string
	Platform    string
	ServiceTier string
}

// AvailableFilter 描述用户可用分组查询条件。
type AvailableFilter struct {
	UserID   int
	Page     int
	PageSize int
	Keyword  string
	Platform string
}

// ListResult 描述分页结果。
type ListResult struct {
	List     []Group
	Total    int64
	Page     int
	PageSize int
}

// CreateInput 描述创建分组输入。
type CreateInput struct {
	Name string
	// NameI18n / NoteI18n 展示文案多语言覆盖；service 保存前会剔除 value 为空白的条目。
	NameI18n       map[string]string
	Platform       string
	RateMultiplier float64
	IsExclusive    bool
	StatusVisible  bool
	// AllowedUserIDs 专属分组的授权用户 ID 列表（仅 IsExclusive 时有意义；空=仅管理员可见）。
	AllowedUserIDs    []int64
	SubscriptionType  string
	Quotas            map[string]any
	ModelRouting      map[string][]int64
	PluginSettings    map[string]map[string]string
	ServiceTier       string
	ForceInstructions string
	Note              string
	NoteI18n          map[string]string
	SortWeight        int
	// CopyAccountsFromGroupIDs 指定在新分组创建后从这些分组复制账号绑定（同平台，自动去重）。
	CopyAccountsFromGroupIDs []int
}

// UpdateInput 描述更新分组输入。
type UpdateInput struct {
	Name *string
	// NameI18n / NoteI18n：nil=不修改；非 nil 时整体覆盖（清理空白 value 后为空 = 清空）。
	NameI18n       map[string]string
	RateMultiplier *float64
	IsExclusive    *bool
	StatusVisible  *bool
	// AllowedUserIDs / HasAllowedUserIDs：HasAllowedUserIDs=false 时不改动授权用户；
	// 为 true 时按 AllowedUserIDs 覆盖（空切片=清空，即仅管理员可见）。
	AllowedUserIDs    []int64
	HasAllowedUserIDs bool
	SubscriptionType  *string
	Quotas            map[string]any
	ModelRouting      map[string][]int64
	PluginSettings    map[string]map[string]string
	ServiceTier       *string
	ForceInstructions *string
	Note              *string
	NoteI18n          map[string]string
	SortWeight        *int
}

// sanitizeI18nMap 克隆并清理多语言文案 map：value 去首尾空白，空白条目剔除。
// 保持 nil / 非 nil 语义（nil=不修改，非 nil 空 map=清空），供 Update 部分更新使用。
func sanitizeI18nMap(input map[string]string) map[string]string {
	if input == nil {
		return nil
	}
	cleaned := make(map[string]string, len(input))
	for lang, text := range input {
		trimmed := strings.TrimSpace(text)
		if trimmed == "" {
			continue
		}
		cleaned[lang] = trimmed
	}
	return cleaned
}

func cloneQuotas(input map[string]any) map[string]any {
	if input == nil {
		return nil
	}
	cloned := make(map[string]any, len(input))
	for key, value := range input {
		cloned[key] = value
	}
	return cloned
}

func cloneModelRouting(input map[string][]int64) map[string][]int64 {
	if input == nil {
		return nil
	}
	cloned := make(map[string][]int64, len(input))
	for key, value := range input {
		cloned[key] = append([]int64(nil), value...)
	}
	return cloned
}

func clonePluginSettings(input map[string]map[string]string) map[string]map[string]string {
	if input == nil {
		return nil
	}
	cloned := make(map[string]map[string]string, len(input))
	for plugin, kv := range input {
		inner := make(map[string]string, len(kv))
		for k, v := range kv {
			inner[k] = v
		}
		cloned[plugin] = inner
	}
	return cloned
}
