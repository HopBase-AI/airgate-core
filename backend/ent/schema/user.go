package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// User 用户表
type User struct {
	ent.Schema
}

func (User) Fields() []ent.Field {
	return []ent.Field{
		field.String("email").Unique().NotEmpty(),
		field.String("password_hash").NotEmpty().Sensitive(),
		field.String("username").Default(""),
		field.String("display_badge").Default("").MaxLen(128).
			Comment("用户侧展示标签，仅用于控制台身份文案展示，不参与权限判断。"),
		field.Float("balance").Default(0).
			SchemaType(map[string]string{dialect.Postgres: "decimal(20,8)"}),
		field.Enum("role").Values("admin", "user").Default("user"),
		field.Bool("can_author_blog").Default(false).
			Comment("是否可进入后台撰写/管理博客(授予非管理员的营销/运营用户);管理员天然可写。"),
		field.Int("max_concurrency").Default(0).Min(0).
			Comment("用户级并发上限：同一 user 所有 API Key 加起来同时在途的请求数。0 表示不限制（默认）。与 api_key.max_concurrency 是 AND 关系，两者都会检查。"),
		field.String("totp_secret").Optional().Nillable().Sensitive(),
		field.JSON("group_rates", map[int64]float64{}).Optional(),
		field.Enum("pricing_mode").Values("standard", "quote").Default("standard").
			Comment("定价展示模式：standard=标准牌价（现状）；quote=报价客户——控制台只展示 group_rates 报价单换算出的价格，登录态接口裁剪牌价对比与折扣锚点。仅影响展示口径，计费仍走 group_rates 覆盖链。"),
		field.JSON("group_plugin_settings", map[int64]map[string]map[string]string{}).Optional().
			Comment("用户级分组插件配置覆盖。用于 OpenAI 生图 1K/2K/4K 专属固定价等，不影响 group_rates 倍率。"),
		field.Float("balance_alert_threshold").Default(0), // 0 表示关闭预警
		field.Bool("balance_alert_notified").Default(false),
		field.Enum("status").Values("active", "disabled").Default("active"),
		field.String("signup_source").Default("").MaxLen(64).
			Comment("注册来源站点 ID（ToC 落地页 ?site= 归因）；空表示直接注册或来源未知。"),
		field.String("invite_code").Optional().Nillable().MaxLen(16).Unique().
			Comment("本人的推广邀请码；惰性生成（首次访问我的邀请页），NULL 表示尚未生成。"),
		field.Int("inviter_id").Optional().Nillable().Immutable().
			Comment("邀请人 user id；注册时经邀请码绑定，终身不变。刻意不做外键，邀请人删除后保留归因。"),
		field.Float("referral_rate").Optional().Nillable().
			Comment("作为推广官的返利比例覆盖（0~1）；NULL 表示使用全局默认 referral_default_rate。"),
		field.Enum("referral_tier").Values("user", "official").Default("user").
			Comment("推广身份层级：user=普通用户邀请，official=官方/团队推广官（后台授予）。仅驱动展示样式，不改返佣逻辑。"),
		field.String("referral_display_name").Default("").MaxLen(64).
			Comment("官方推广官对访客展示的团队/署名（仅 official 生效）；空则前端回退到通用「官方推广」徽章。"),
		field.Time("created_at").Default(timeNow).Immutable(),
		field.Time("updated_at").Default(timeNow).UpdateDefault(timeNow),
	}
}

func (User) Indexes() []ent.Index {
	return []ent.Index{
		// 邀请人数统计 / 我的被邀请人列表按邀请人查询
		index.Fields("inviter_id"),
	}
}

func (User) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("api_keys", APIKey.Type),
		edge.To("subscriptions", UserSubscription.Type),
		edge.To("usage_logs", UsageLog.Type),
		// 用户可访问的专属分组（多对多）
		edge.To("allowed_groups", Group.Type),
		edge.To("balance_logs", BalanceLog.Type),
		// 第三方登录身份绑定（OAuth）
		edge.To("identities", UserIdentity.Type),
	}
}
