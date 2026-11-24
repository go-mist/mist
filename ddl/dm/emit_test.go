package dm

import (
	"strings"
	"testing"

	"github.com/go-mist/mist/diff"
	"github.com/go-mist/mist/schema"
)

func TestRenderIdentityAndVarchar(t *testing.T) {
	table := schema.Table{
		Name:       "users",
		PrimaryKey: []string{"id"},
		Columns: []schema.Column{
			{Name: "id", Type: schema.Type{Name: "bigint"}, Identity: true, NotNull: true},
			{Name: "name", Type: schema.Type{Name: "varchar", Length: 40}, NotNull: true},
			{Name: "bio", Type: schema.Type{Name: "text"}},
			{Name: "active", Type: schema.Type{Name: "boolean"}, NotNull: true},
		},
	}
	got, err := Render([]diff.Change{{
		Kind:     diff.CreateTable,
		Table:    "users",
		TableDef: &table,
	}})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`"id" BIGINT IDENTITY(1,1) NOT NULL PRIMARY KEY`,
		`"name" VARCHAR(40) NOT NULL`,
		`"bio" CLOB`,
		`"active" BIT NOT NULL`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %s\n%s", want, got)
		}
	}
}
