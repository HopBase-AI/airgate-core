package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// UserNotification 站内通知：按用户投递的一条消息（额度预警 / 余额预警 / 系统通知）。
//
// 刻意不做外键：与 TeamAuditLog 同款，user_id 只是普通列，投递对象删除后通知行可随查询自然消失，
// 无需级联。dedupe_key 唯一索引承担"同一事件只投递一次"的去重：写入撞唯一约束即视为已投递，
// 调用方据此决定要不要再发邮件（邮件与站内信共用同一把去重钥匙）。
type UserNotification struct {
	ent.Schema
}

func (UserNotification) Fields() []ent.Field {
	return []ent.Field{
		field.Int("user_id").
			Comment("接收者 user id（普通列，无外键）。"),
		field.String("kind").NotEmpty().MaxLen(32).
			Comment("通知类型：quota_alert / balance_alert / system；前端按 kind 选图标与多语言。"),
		field.Enum("level").Values("info", "warning", "danger").Default("info").
			Comment("严重程度：info 普通、warning 预警（≥80%）、danger 已用尽 / 需立即处理。"),
		field.String("title").NotEmpty().MaxLen(255),
		field.String("content").Default(""),
		field.String("link").Default("").MaxLen(255).
			Comment("控制台内跳转路径，如 /team、/usage、/profile；空表示无跳转。"),
		field.String("dedupe_key").Optional().Unique().MaxLen(128).
			Comment("去重钥匙（唯一）：同一事件只投递一次；为空表示不去重。"),
		field.Time("read_at").Optional().Nillable().
			Comment("已读时间；NULL 表示未读。"),
		field.Time("created_at").Default(timeNow).Immutable(),
	}
}

func (UserNotification) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id", "created_at").StorageKey("user_notification_user_created_at"),
		index.Fields("user_id", "read_at").StorageKey("user_notification_user_read_at"),
	}
}
