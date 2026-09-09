package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// Department 企业组织（部门 / 项目 / 业务线）：企业主（owner）名下、成员与密钥之上的一层额度单元。
//
// 三层额度：成员 → 部门 → 企业余额，一次请求的可用额度取三者最小。部门额度是**限额（Cap）**
// 而非预扣：只是上限计数器，不从企业余额里划走，各部门额度之和允许超过企业余额（先花先得），
// 真钱永远只从 users.balance 扣——保住单一扣费点这个不变量。
//
// 额度六件套（quota_usd / quota_period / period_anchor / period_start / period_used_base /
// used_quota / used_quota_actual）与 Member 同款：monthly 惰性换期，鉴权读到跨期才推进；
// period_anchor 继承企业账期锚点（users.billing_period_anchor，缺省为企业主创建时刻），
// 部门、成员、企业三层的「本期」严格同窗，否则会出现「部门本期已用 < 成员本期已用」的父子矛盾。
//
// 归属：成员（members.department）与密钥（api_keys.department）都可直挂部门；密钥的有效部门
// = key.department ?? key.member.department。刻意不设 status：只允许删除（成员与密钥回落
// 「未分配」，用量快照保留），不引入「停用部门要不要连带停用成员」这个无解问题。
type Department struct {
	ent.Schema
}

func (Department) Fields() []ent.Field {
	return []ent.Field{
		field.String("name").NotEmpty().MaxLen(64),
		field.String("note").Default("").MaxLen(255),
		field.Int("sort").Default(0),
		field.Float("quota_usd").Default(0).Min(0).
			Comment("部门额度（USD 账面口径，与 members.quota_usd 同源）。0 表示不限。"),
		field.Enum("quota_period").Values("none", "monthly").Default("monthly").
			Comment("额度周期：none=一次性总额；monthly=按月重置（以 period_anchor 逐月对齐、月末夹紧）。"),
		field.Time("period_anchor").Default(timeNow).
			Comment("月度换期锚点，继承企业账期锚点；换期日与它同日对齐。"),
		field.Time("period_start").Default(timeNow).
			Comment("当前计量期起点。monthly 时惰性推进；none 时仅记录最近一次手动重置。"),
		field.Float("period_used_base").Default(0).
			Comment("本期起点时 used_quota 的快照。本期已用 = used_quota − period_used_base。"),
		field.Float("used_quota").Default(0).
			Comment("累计账面已用：累加 billed_cost（与 members.used_quota 同口径）。"),
		field.Float("used_quota_actual").Default(0).
			Comment("累计真实成本：累加 actual_cost，即企业主为该部门实际付出的余额。"),
		field.Time("created_at").Default(timeNow).Immutable(),
		field.Time("updated_at").Default(timeNow).UpdateDefault(timeNow),
	}
}

func (Department) Edges() []ent.Edge {
	return []ent.Edge{
		// 归属的企业主；部门随企业主删除而删除。
		edge.From("owner", User.Type).Ref("departments").Unique().Required(),
		// 部门下的成员与直挂部门的密钥（删除部门时置空，回落「未分配」）。
		edge.To("members", Member.Type),
		edge.To("api_keys", APIKey.Type),
	}
}

func (Department) Indexes() []ent.Index {
	return []ent.Index{
		index.Edges("owner"),
		// 同一企业主名下部门名唯一。
		index.Fields("name").Edges("owner").Unique(),
	}
}
