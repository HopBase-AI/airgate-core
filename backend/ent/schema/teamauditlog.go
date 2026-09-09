package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// TeamAuditLog 企业团队操作审计：组织调整、成员变更、权限配置、额度调整、API Key 操作与账期改动
// 全程可记录、可查询、可追溯。按企业主（owner_id）归档，企业主查本企业范围，管理员可查全部。
//
// 刻意不做外键：成员/部门/密钥删除后审计行必须保留（否则"删除"这个动作本身就查不到了），
// 目标只记 ID + 名称快照；before / after 记变更前后的关键字段，供逐条对照。
type TeamAuditLog struct {
	ent.Schema
}

func (TeamAuditLog) Fields() []ent.Field {
	return []ent.Field{
		field.Int("owner_id").
			Comment("所属企业主 user id 快照。"),
		field.Int("actor_user_id").Default(0).
			Comment("操作者 user id：企业主本人，或成员账号（自建/改自己的密钥）。"),
		field.String("actor_email").Default("").MaxLen(255),
		field.String("action").NotEmpty().MaxLen(64).
			Comment("动作：department.create / member.update / apikey.delete / team.billing_period …"),
		field.String("target_type").NotEmpty().MaxLen(32).
			Comment("对象类型：department / member / apikey / team"),
		field.Int("target_id").Default(0),
		field.String("target_name").Default("").MaxLen(255).
			Comment("对象名称快照，对象删除后仍可读。"),
		field.JSON("before", map[string]any{}).Optional().
			Comment("变更前的关键字段快照；创建类动作为空。"),
		field.JSON("after", map[string]any{}).Optional().
			Comment("变更后的关键字段快照；删除类动作为空。"),
		field.String("ip").Default("").MaxLen(64),
		field.String("request_id").Default("").MaxLen(64),
		field.Time("created_at").Default(timeNow).Immutable(),
	}
}

func (TeamAuditLog) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("owner_id", "created_at").StorageKey("team_audit_owner_created_at"),
		index.Fields("owner_id", "target_type", "target_id").StorageKey("team_audit_owner_target"),
	}
}
