package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// UserIdentity 第三方登录身份绑定（OAuth：google / github 等）。
// 一个用户可绑定多个第三方身份；同一第三方身份全局唯一。
type UserIdentity struct {
	ent.Schema
}

func (UserIdentity) Fields() []ent.Field {
	return []ent.Field{
		field.String("provider").NotEmpty().
			Comment("第三方平台标识：google / github"),
		field.String("provider_user_id").NotEmpty().
			Comment("第三方平台的用户唯一 ID（Google sub / GitHub id）"),
		field.String("email").Default("").
			Comment("绑定时第三方返回的邮箱，仅作展示与排查，不参与登录匹配"),
		field.Time("created_at").Default(timeNow).Immutable(),
	}
}

func (UserIdentity) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("user", User.Type).Ref("identities").Unique().Required(),
	}
}

func (UserIdentity) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("provider", "provider_user_id").Unique(),
	}
}
