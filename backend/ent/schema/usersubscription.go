package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// UserSubscription 用户订阅
type UserSubscription struct {
	ent.Schema
}

func (UserSubscription) Fields() []ent.Field {
	return []ent.Field{
		field.Time("effective_at"),
		field.Time("expires_at"),
		field.JSON("usage", map[string]interface{}{}).Optional(), // 历史遗留：未再写入，保留读兼容
		field.Enum("status").Values("active", "expired", "suspended").Default("active"),
		// ---- 点数账本（订阅制分组按月额度计量，替代余额扣费）----
		// 当前计量期 [period_start, period_end)：按 effective_at 锚定逐月推进；
		// 零值表示尚未初始化，首次访问时由服务层惰性对齐（历史行升级也走这条路）。
		field.Time("period_start").Optional(),
		field.Time("period_end").Optional(),
		// 套餐权益在开通时快照，之后修改 Group.quotas 不会篡改已售出权益。
		field.JSON("plan_snapshot", map[string]interface{}{}).Optional(),
		// included_group_ids 是同一份订阅可使用的模型分组快照。主 group 仅作为套餐展示归属。
		field.JSON("included_group_ids", []int{}).Optional(),
		// 点数全程使用整数，避免并发扣费中的浮点误差。
		field.Int64("credits_limit").Default(0),
		field.Int64("credits_used").Default(0),
		field.Int64("credits_reserved").Default(0),
		// extra_credits 加购包累计点数，不随月重置；期满时先用本期超额抵扣再结转。
		field.Int64("extra_credits").Default(0),
		// images_used 本期已生成图片张数（体验档限 20 张之类的按张上限）；期满归零。
		field.Int("images_used").Default(0),
		field.Int("images_reserved").Default(0),
		field.Int("image_limit").Default(0),
		field.Int64("ledger_version").Default(0),
		// billing_cycle 购买周期：monthly / annual；只影响 expires_at 的推进步长，
		// 点数额度一律按月重置。
		field.Enum("billing_cycle").Values("monthly", "annual").Default("monthly"),
		field.String("source_provider").Default("admin"),
		field.String("source_execution_key").Optional().Nillable(),
		field.String("source_payment_key").Optional().Nillable(),
		field.Int64("payment_amount_minor").Default(0),
		field.String("payment_currency").Default(""),
		field.Time("created_at").Default(timeNow).Immutable(),
		field.Time("updated_at").Default(timeNow).UpdateDefault(timeNow),
	}
}

func (UserSubscription) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("status"),
		index.Fields("source_provider", "source_execution_key").Unique(),
		index.Fields("source_provider", "source_payment_key").Unique(),
	}
}

func (UserSubscription) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("user", User.Type).Ref("subscriptions").Unique().Required(),
		edge.From("group", Group.Type).Ref("subscriptions").Unique().Required(),
		edge.To("reservations", SubscriptionReservation.Type),
	}
}
