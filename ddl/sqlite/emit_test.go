package sqlite_test

import (
	"strings"
	"testing"

	sqliteddl "github.com/xbmlz/mist/ddl/sqlite"
	"github.com/xbmlz/mist/diff"
	"github.com/xbmlz/mist/schema"
)

func TestAlterColumnRebuildsTable(t *testing.T) {
	have := schema.Schema{Tables: []schema.Table{{
		Name: "users",
		Columns: []schema.Column{
			{Name: "id", Type: schema.Type{Name: "integer"}, NotNull: true},
			{Name: "name", Type: schema.Type{Name: "text"}, NotNull: true},
		},
		PrimaryKey: []string{"id"},
		Indexes:    []schema.Index{{Name: "users_name", Columns: []string{"name"}}},
	}}}
	want := schema.Schema{Tables: []schema.Table{{
		Name: "users",
		Columns: []schema.Column{
			{Name: "id", Type: schema.Type{Name: "integer"}, NotNull: true},
			{Name: "name", Type: schema.Type{Name: "varchar", Length: 64}, NotNull: true},
		},
		PrimaryKey: []string{"id"},
		Indexes:    []schema.Index{{Name: "users_name", Columns: []string{"name"}}},
	}}}
	changes := diff.Diff(want, have)
	got, err := sqliteddl.Render(changes, want)
	if err != nil {
		t.Fatal(err)
	}
	for _, piece := range []string{
		`CREATE TABLE "users__mist_new"`,
		`"name" varchar(64) NOT NULL`,
		`INSERT INTO "users__mist_new" ("id", "name") SELECT "id", "name" FROM "users";`,
		`DROP TABLE "users";`,
		`ALTER TABLE "users__mist_new" RENAME TO "users";`,
		`CREATE INDEX "users_name" ON "users" ("name");`,
	} {
		if !strings.Contains(got, piece) {
			t.Fatalf("missing %s\n%s", piece, got)
		}
	}
}

func TestAddNotNullColumnNeedsDefault(t *testing.T) {
	have := schema.Schema{Tables: []schema.Table{{
		Name: "users",
		Columns: []schema.Column{
			{Name: "id", Type: schema.Type{Name: "integer"}, NotNull: true},
		},
		PrimaryKey: []string{"id"},
	}}}
	want := schema.Schema{Tables: []schema.Table{{
		Name: "users",
		Columns: []schema.Column{
			{Name: "id", Type: schema.Type{Name: "bigint"}, NotNull: true},
			{Name: "name", Type: schema.Type{Name: "text"}, NotNull: true},
		},
		PrimaryKey: []string{"id"},
	}}}
	_, err := sqliteddl.Render(diff.Diff(want, have), want)
	if err == nil || !strings.Contains(err.Error(), "has no default") {
		t.Fatal(err)
	}
}
