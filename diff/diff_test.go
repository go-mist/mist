package diff

import (
	"testing"

	"github.com/go-mist/mist/schema"
)

func TestRenameIsDropAndAdd(t *testing.T) {
	actual := schema.Schema{Tables: []schema.Table{{
		Name: "users",
		Columns: []schema.Column{
			{Name: "email", Type: schema.Type{Name: "text"}, Loc: schema.Loc{File: "a.go", Line: 4}},
		},
	}}}
	desired := schema.Schema{Tables: []schema.Table{{
		Name: "users",
		Columns: []schema.Column{
			{Name: "mail", Type: schema.Type{Name: "text"}, Loc: schema.Loc{File: "b.go", Line: 8}},
		},
	}}}
	changes := Diff(desired, actual)
	if len(changes) != 2 {
		t.Fatalf("got %d changes, want add and drop", len(changes))
	}
	if changes[0].Kind != AddColumn || changes[0].Column.Name != "mail" || changes[0].Loc.Line != 8 {
		t.Fatalf("add = %+v", changes[0])
	}
	if changes[1].Kind != DropColumn || !changes[1].Destructive || changes[1].Name != "email" || changes[1].Loc.Line != 4 {
		t.Fatalf("drop = %+v", changes[1])
	}
	for _, change := range changes {
		if change.Kind == "rename" {
			t.Fatal("inferred a rename")
		}
	}
}

func TestTypeChangeIsDestructive(t *testing.T) {
	actual := userTable(schema.Column{Name: "name", Type: schema.Type{Name: "text"}, NotNull: true})
	desired := userTable(schema.Column{Name: "name", Type: schema.Type{Name: "varchar", Length: 8}, NotNull: true})
	changes := Diff(desired, actual)
	if len(changes) != 1 || changes[0].Kind != AlterColumn || !changes[0].Destructive {
		t.Fatalf("%+v", changes)
	}
}

func TestSameSchemaIsEmpty(t *testing.T) {
	s := userTable(schema.Column{Name: "name", Type: schema.Type{Name: "text"}, NotNull: true})
	if changes := Diff(s, s); len(changes) != 0 {
		t.Fatalf("%+v", changes)
	}
}

func TestNullAndDefaultAreNotDestructive(t *testing.T) {
	actual := userTable(schema.Column{Name: "name", Type: schema.Type{Name: "text"}})
	desired := userTable(schema.Column{Name: "name", Type: schema.Type{Name: "text"}, NotNull: true, Default: "''"})
	changes := Diff(desired, actual)
	if len(changes) != 1 || changes[0].Kind != AlterColumn || changes[0].Destructive {
		t.Fatalf("%+v", changes)
	}
}

func TestForeignKeyNameDoesNotMatter(t *testing.T) {
	fk := schema.ForeignKey{Name: "posts_author_id_fkey", Columns: []string{"author_id"}, RefTable: "users", RefColumns: []string{"id"}, OnDelete: "cascade"}
	actual := schema.Schema{Tables: []schema.Table{{Name: "posts", ForeignKeys: []schema.ForeignKey{fk}}}}
	fk.Name = "other_name"
	desired := schema.Schema{Tables: []schema.Table{{Name: "posts", ForeignKeys: []schema.ForeignKey{fk}}}}
	if changes := Diff(desired, actual); len(changes) != 0 {
		t.Fatalf("%+v", changes)
	}
}

func TestUnknownEnumValuesMatch(t *testing.T) {
	desired := schema.Schema{Enums: []schema.Enum{{Name: "status"}}}
	actual := schema.Schema{Enums: []schema.Enum{{Name: "status", Values: []string{"on", "off"}}}}
	if changes := Diff(desired, actual); len(changes) != 0 {
		t.Fatalf("%+v", changes)
	}
}

func userTable(cols ...schema.Column) schema.Schema {
	return schema.Schema{Tables: []schema.Table{{Name: "users", Columns: cols}}}
}
