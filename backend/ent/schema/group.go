package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
)

// Group 分组
type Group struct {
	ent.Schema
}

func (Group) Fields() []ent.Field {
	return []ent.Field{
		field.String("name").NotEmpty(),
		// name_i18n / note_i18n 分组展示文案的多语言覆盖（键=语言码 en / zh-HK / ja；
		// zh 基准即 name / note 本身）。缺键或空值时展示端回退基准文案，旧数据行为不变。
		// 语言码有意对齐控制台 SPA i18next 的口径（zh-HK），与博客 SSR sites_branding
		// 侧的 zh-Hant 不同源；新增第三处 i18n 覆盖时先对齐这两套之一，勿再发明新码。
		field.JSON("name_i18n", map[string]string{}).Optional(),
		field.String("platform").NotEmpty(),
		field.Float("rate_multiplier").Default(1.0),
		field.Bool("is_exclusive").Default(false),
		// status_visible 控制此分组是否在公开「服务状态」页展示。
		// 默认 true 保持旧行为兼容；管理员可在「分组管理」中关掉以对外隐藏
		// （比如仅限熟客的专属分组、调试中的分组等）。
		// 隐藏仅影响公开状态页 (/status)，不影响 admin 视图和 API 鉴权逻辑。
		field.Bool("status_visible").Default(true),
		field.Enum("subscription_type").Values("standard", "subscription").Default("standard"),
		field.JSON("quotas", map[string]interface{}{}).Optional(),
		field.JSON("model_routing", map[string][]int64{}).Optional(),
		// plugin_settings 按插件命名空间存放细粒度开关，形如
		//   {"claude": {"claude_code_only": "true"}}
		// Core 在 buildForwardHeaders 时按约定映射成 X-Airgate-* 头下发给网关插件。
		// 保持 string→string 嵌套是为了不侵入 SDK（零 SDK bump）。
		field.JSON("plugin_settings", map[string]map[string]string{}).Optional(),
		field.String("service_tier").Default(""),
		field.String("force_instructions").Default(""),
		field.String("note").Default(""),
		// note 的多语言覆盖，语义同 name_i18n。
		field.JSON("note_i18n", map[string]string{}).Optional(),
		field.Int("sort_weight").Default(0),
		field.Time("created_at").Default(timeNow).Immutable(),
		field.Time("updated_at").Default(timeNow).UpdateDefault(timeNow),
	}
}

func (Group) Edges() []ent.Edge {
	return []ent.Edge{
		// 分组关联的账号（多对多反向）
		edge.From("accounts", Account.Type).Ref("groups"),
		// 允许访问此专属分组的用户（多对多反向）
		edge.From("allowed_users", User.Type).Ref("allowed_groups"),
		edge.To("api_keys", APIKey.Type),
		edge.To("subscriptions", UserSubscription.Type),
		edge.To("usage_logs", UsageLog.Type),
	}
}
