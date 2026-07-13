package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// ReferralCommission 分销返利流水。
//
// 每笔充值入账事件（users.notify_topup）按邀请关系产生 0~2 行：
//   - kind=rebate：按比例返给推广官（inviter 为受益人）；
//   - kind=first_bonus：被邀请人首充加赠（invitee 为受益人）。
//
// 双方 id/email 均为快照，刻意不做外键——用户硬删除后保留对账凭据；
// 余额实际变动经 user.AdjustBalance 幂等入账，本表是分销视角的业务流水。
type ReferralCommission struct {
	ent.Schema
}

func (ReferralCommission) Fields() []ent.Field {
	return []ent.Field{
		field.Int("inviter_id").
			Comment("推广官 user id 快照（kind=rebate 的受益人）。"),
		field.String("inviter_email").Default("").
			Comment("推广官邮箱快照。"),
		field.Int("invitee_id").
			Comment("被邀请人 user id 快照（kind=first_bonus 的受益人）。"),
		field.String("invitee_email").Default("").
			Comment("被邀请人邮箱快照。"),
		field.String("out_trade_no").NotEmpty().
			Comment("关联充值订单号（支付插件侧）。"),
		field.Enum("kind").Values("rebate", "first_bonus").
			Comment("rebate=推广官返利；first_bonus=被邀请人首充加赠。"),
		field.Float("paid_amount").
			SchemaType(map[string]string{dialect.Postgres: "decimal(20,8)"}).
			Comment("订单实付金额（计算基数，不含套餐赠送）。"),
		field.Float("rate").
			Comment("入账时生效的比例快照，防事后调比例对不上账。"),
		field.Float("amount").
			SchemaType(map[string]string{dialect.Postgres: "decimal(20,8)"}).
			Comment("实际入账金额 = paid_amount × rate。"),
		field.Enum("status").Values("settled", "reversed").Default("settled").
			Comment("settled=已入账；reversed=已回冲（退款/作弊人工处理）。预留结算态扩展。"),
		field.Time("created_at").Default(timeNow).Immutable(),
		field.Time("reversed_at").Optional().Nillable().
			Comment("回冲时间；NULL 表示未回冲。"),
	}
}

func (ReferralCommission) Indexes() []ent.Index {
	return []ent.Index{
		// 与 balance_logs 幂等键双保险：同一订单同一种返利只入账一次
		index.Fields("out_trade_no", "kind").Unique(),
		// 推广官流水/汇总查询
		index.Fields("inviter_id", "created_at"),
		// 首充加赠防重（该被邀请人是否已发过 first_bonus）
		index.Fields("invitee_id", "kind"),
	}
}
