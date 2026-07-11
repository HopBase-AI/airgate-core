package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// AccountEvent 账号异常/状态事件流（只追加，短期保留）。
//
// 与 Account.error_msg 的区别：error_msg 只存"当前状态的最新原因"，
// 本表记录每一次判决产生的事件（含未改状态的上游抖动），供"异常监控"页追溯：
// 哪个账号、什么时候、因为什么原因（限流/凭证失效/上游不稳定/手动操作）出了问题。
//
// 事件语义：
//
//	rate_limited     被上游限流（family 非空时为家族级冷却，账号其它模型仍可调度）
//	degraded         池账号软降级
//	disabled         凭证失效等硬禁用（自动）
//	recovered        从 rate_limited/degraded 自动恢复 active
//	upstream_error   上游侧瞬时故障（5xx/断流/池上游透传错误），不改账号状态
//	manual_disabled  管理员手动关闭调度
//	manual_recovered 管理员手动恢复调度
type AccountEvent struct {
	ent.Schema
}

func (AccountEvent) Fields() []ent.Field {
	return []ent.Field{
		field.Enum("event_type").
			Values("rate_limited", "degraded", "disabled", "recovered",
				"upstream_error", "manual_disabled", "manual_recovered"),
		field.String("reason").Default("").
			Comment("事件原因（forwarder 判决 Reason / 管理操作说明），写入前截断"),
		field.String("family").Default("").
			Comment("家族级冷却时的模型家族键（如 gpt-image），账号级事件为空"),
		field.String("source").Default("").
			Comment("事件来源：forward（转发判决）/ probe（配额巡检）/ manual（管理员操作）"),
		field.Int("upstream_status").Default(0).
			Comment("上游 HTTP 状态码，无则为 0"),
		field.Time("state_until").Optional().Nillable().
			Comment("rate_limited / degraded 的冷却到期时间"),
		field.Time("created_at").Default(timeNow).Immutable(),
	}
}

func (AccountEvent) Indexes() []ent.Index {
	return []ent.Index{
		// 列表按时间倒序分页 + 保留期清理都走 created_at。
		index.Fields("created_at"),
		// 按账号（及经账号关联分组）筛选走 FK 列；不建索引大表下是全表扫。
		index.Edges("account"),
	}
}

func (AccountEvent) Edges() []ent.Edge {
	return []ent.Edge{
		// 级联删除注解在 Account.edges.events（edge.To 侧）上声明。
		edge.From("account", Account.Type).Ref("events").Unique().Required(),
	}
}
