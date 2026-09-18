package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// SubscriptionReservation reserves a bounded amount from one immutable monthly window.
// The stable key makes reserve, settle, release and callback retries idempotent.
type SubscriptionReservation struct {
	ent.Schema
}

func (SubscriptionReservation) Fields() []ent.Field {
	return []ent.Field{
		field.String("reservation_key").NotEmpty().Immutable(),
		field.Int("user_id_snapshot").Immutable(),
		field.Int("group_id_snapshot").Immutable(),
		field.Int("task_id").Default(0).Immutable(),
		field.Int("account_id_snapshot").Default(0).Immutable(),
		field.Time("period_start").Immutable(),
		field.Time("period_end").Immutable(),
		field.Int64("credits_reserved").Default(0).Immutable(),
		field.Int("images_reserved").Default(0).Immutable(),
		field.Int64("credits_settled").Default(0),
		field.Int("images_settled").Default(0),
		field.Enum("status").Values("reserved", "settled", "released").Default("reserved"),
		field.Time("expires_at").Immutable(),
		field.Time("created_at").Default(timeNow).Immutable(),
		field.Time("updated_at").Default(timeNow).UpdateDefault(timeNow),
	}
}

func (SubscriptionReservation) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("subscription", UserSubscription.Type).Ref("reservations").Unique().Required(),
	}
}

func (SubscriptionReservation) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("reservation_key").Unique(),
		index.Fields("status", "expires_at"),
		index.Fields("user_id_snapshot", "period_start"),
	}
}
