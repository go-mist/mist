package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

// User is the bench table: id, name, age.
type User struct {
	ent.Schema
}

// Fields of a user row. Ent adds the integer id itself.
func (User) Fields() []ent.Field {
	return []ent.Field{
		field.String("name").NotEmpty(),
		field.Int("age"),
	}
}
