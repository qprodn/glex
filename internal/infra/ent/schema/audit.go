package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/mixin"
)

// AuditMixin contributes the repo-wide audit columns (ADR 0002): create_by /
// update_by (0 = the system itself acted) and created_at / updated_at /
// deleted_at. Every table mixes it in, and no table hard-deletes rows —
// withdrawal sets deleted_at. The Application layer stamps create_by /
// update_by from the authenticated Principal; the columns default to 0.
type AuditMixin struct {
	mixin.Schema
}

// Fields of the audit columns.
func (AuditMixin) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("create_by").Optional().Default(0),
		field.Int64("update_by").Optional().Default(0),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
		// Soft delete. NULL = not deleted.
		field.Time("deleted_at").Optional().Nillable(),
	}
}
