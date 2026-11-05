package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	entschema "entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// Post holds the schema for the post entity (岗位, ported from RuoYi 6.0
// sys_post — posts are department-scoped, ADR 0015). RuoYi 6.0's
// post_category is deliberately not ported.
type Post struct {
	ent.Schema
}

// Mixin contributes the audit columns (ADR 0002).
func (Post) Mixin() []ent.Mixin {
	return []ent.Mixin{AuditMixin{}}
}

// Fields of the Post. dept_id references sys_dept as a plain column (like
// menu.parent_id — no ent edge, no FK); uniqueness (post_name per
// department, post_code globally) is a soft domain check, repo convention.
func (Post) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id").Unique().Immutable(),
		field.Int64("dept_id"),
		field.String("post_code").MaxLen(64),
		field.String("post_name").MaxLen(50),
		field.Int("post_sort").Default(0).SchemaType(map[string]string{
			dialect.Postgres: "integer",
		}),
		// smallint in PG (RuoYi's char(1) status made numeric); 0正常 1停用.
		field.Int8("status").Default(0).SchemaType(map[string]string{
			dialect.Postgres: "smallint",
		}),
		field.String("remark").Default("").MaxLen(500),
	}
}

// Annotations pins the table to RuoYi's sys_post.
func (Post) Annotations() []entschema.Annotation {
	return []entschema.Annotation{
		entsql.Annotation{Table: "sys_post"},
	}
}

// Indexes backs the department's posts lookup (assignment candidates and the
// dept delete guard).
func (Post) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("dept_id"),
	}
}

// Edges of the Post: the by-post side of the Post Assignment lives as
// explicit UserPost link rows in sys_user_post, with audit columns and soft
// delete (ADR 0002).
func (Post) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("user_links", UserPost.Type),
	}
}
