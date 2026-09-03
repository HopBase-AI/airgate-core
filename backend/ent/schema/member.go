package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// Member 团队成员：企业主账号（owner）名下的子身份。
//
// 成员没有独立密码与余额，只是主账号密钥的归属单元与额度分配单元：
//   - 一个成员可挂多把 API Key（跨分组），额度记在成员头上而非单把 key；
//   - 请求扣费仍落主账号 users.balance（统一从主账号走消耗），成员只累加账面用量；
//   - 额度支持一次性总额与按月惰性换期：无定时任务，鉴权时读到跨期才把
//     period_start 推进并把 used_quota 快照进 period_used_base，本期已用 =
//     used_quota − period_used_base。
type Member struct {
	ent.Schema
}

func (Member) Fields() []ent.Field {
	return []ent.Field{
		field.String("name").NotEmpty().MaxLen(64),
		field.String("email").Default("").MaxLen(255).
			Comment("联系邮箱，仅供主账号辨识成员；不参与登录。"),
		field.String("note").Default("").MaxLen(255),
		field.Float("quota_usd").Default(0).Min(0).
			Comment("成员额度（USD 账面口径，与 api_keys.quota_usd 同源）。0 表示不限。"),
		field.Enum("quota_period").Values("none", "monthly").Default("monthly").
			Comment("额度周期：none=一次性总额；monthly=按月重置（以 period_anchor 逐月对齐、月末夹紧）。"),
		field.Time("period_anchor").Default(timeNow).
			Comment("月度换期锚点（创建时刻），换期日与它同日对齐。"),
		field.Time("period_start").Default(timeNow).
			Comment("当前计量期起点。monthly 时惰性推进；none 时仅记录最近一次手动重置。"),
		field.Float("period_used_base").Default(0).
			Comment("本期起点时 used_quota 的快照。本期已用 = used_quota − period_used_base。"),
		field.Float("used_quota").Default(0).
			Comment("累计账面已用：累加 billed_cost（与 api_keys.used_quota 同口径）。"),
		field.Float("used_quota_actual").Default(0).
			Comment("累计真实成本：累加 actual_cost，即主账号为该成员实际付出的余额。"),
		field.Enum("status").Values("active", "disabled").Default("active"),
		field.Time("created_at").Default(timeNow).Immutable(),
		field.Time("updated_at").Default(timeNow).UpdateDefault(timeNow),
	}
}

func (Member) Edges() []ent.Edge {
	return []ent.Edge{
		// 归属的主账号；成员随主账号删除而删除。
		edge.From("owner", User.Type).Ref("members").Unique().Required(),
		edge.To("api_keys", APIKey.Type),
	}
}

func (Member) Indexes() []ent.Index {
	return []ent.Index{
		index.Edges("owner"),
	}
}
