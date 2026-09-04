package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// Member 团队成员：企业主账号（owner）名下的子身份。
//
// 2026-09-04 起成员是**真实账号**：企业主在团队页直接创建成员的邮箱+密码，成员用自己的
// 账号正常登录、使用全部控制台功能（密钥/用量/模型广场/工作台/AI Chat）。与普通用户
// 唯一的差别是**消耗与归属**：
//   - 成员名下的 key（以及成员账号发起的 Host 转发）付费身份统一解析到 owner——扣
//     owner 的 users.balance、按 owner 的 group_rates/pricing_mode 计价、走 owner 的
//     并发预算；usage_logs.user 记 owner、member_id 记该成员，owner 看全员用量，
//     成员只看自己；
//   - 额度：一次性总额或按月惰性换期（无定时任务，鉴权读到跨期才推进 period_start
//     并把 used_quota 快照进 period_used_base，本期已用 = used_quota − period_used_base）；
//   - 分组权限：allowed_group_ids 非空时成员只能用其中的分组（建 key / 工作台选组 /
//     转发时三处同口径），为空则继承 owner 全部可见分组。
//
// 兼容：account 为空的老成员（2026-09-03 的「密钥归属」模型）仍按 owner 自己名下
// 带 member 边的 key 计量，行为不变。
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
		field.JSON("allowed_group_ids", []int64{}).Optional().
			Comment("企业主授予成员可用的分组 ID 白名单；空/NULL 表示继承 owner 全部可见分组。" +
				"建 key、工作台选组、转发鉴权三处同口径校验。"),
		field.Time("created_at").Default(timeNow).Immutable(),
		field.Time("updated_at").Default(timeNow).UpdateDefault(timeNow),
	}
}

func (Member) Edges() []ent.Edge {
	return []ent.Edge{
		// 归属的主账号；成员随主账号删除而删除。
		edge.From("owner", User.Type).Ref("members").Unique().Required(),
		edge.To("api_keys", APIKey.Type),
		// 成员自己的登录账号（users 行，role=user）。为空表示 2026-09-03 的老模型成员
		// （仅作为 owner 密钥的归属单元）。O2O：外键 member_account 落在 users 表并唯一，
		// 一个账号至多是一个成员；从用户侧 WithMembership() 一跳即可判定是否成员账号。
		edge.To("account", User.Type).Unique(),
	}
}

func (Member) Indexes() []ent.Index {
	return []ent.Index{
		index.Edges("owner"),
	}
}
