package pg

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xbmlz/mist/codegen"
	"github.com/xbmlz/mist/diff"
	"github.com/xbmlz/mist/migrate"
	"github.com/xbmlz/mist/schema"
)

func TestFixtureAgainstEmpty(t *testing.T) {
	got, err := Render(diff.Diff(loadFixture(t), schema.Schema{}))
	if err != nil {
		t.Fatal(err)
	}
	want := `CREATE TABLE "users" (
	"id" bigserial PRIMARY KEY,
	"name" text NOT NULL,
	"email" text,
	"created_at" timestamptz NOT NULL DEFAULT now(),
	CONSTRAINT "users_email_key" UNIQUE ("email")
);
--> statement-breakpoint
CREATE TABLE "posts" (
	"id" bigserial PRIMARY KEY,
	"author_id" bigint NOT NULL,
	"title" text NOT NULL,
	CONSTRAINT "posts_author_id_fkey" FOREIGN KEY ("author_id") REFERENCES "users" ("id") ON DELETE cascade
);
`
	if got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
}

func TestFixtureSplitsAsMigration(t *testing.T) {
	sqlText, err := Render(diff.Diff(loadFixture(t), schema.Schema{}))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	sub := filepath.Join(dir, "20261123000000_init")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "migration.sql"), []byte(sqlText), 0o644); err != nil {
		t.Fatal(err)
	}
	migs, err := migrate.Read(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(migs) != 1 || len(migs[0].Statements) != 2 {
		t.Fatalf("%+v", migs)
	}
	if !strings.Contains(migs[0].Statements[0], `CREATE TABLE "users"`) {
		t.Fatal(migs[0].Statements[0])
	}
}

func TestCreateOrdersByForeignKey(t *testing.T) {
	posts := schema.Table{
		Name: "posts",
		Columns: []schema.Column{
			{Name: "author_id", Type: schema.Type{Name: "bigint"}, NotNull: true},
		},
		ForeignKeys: []schema.ForeignKey{{
			Name: "posts_author_id_fkey", Columns: []string{"author_id"}, RefTable: "users", RefColumns: []string{"id"},
		}},
	}
	users := schema.Table{
		Name:       "users",
		Columns:    []schema.Column{{Name: "id", Type: schema.Type{Name: "bigint"}, NotNull: true}},
		PrimaryKey: []string{"id"},
	}
	got, err := Render(diff.Diff(schema.Schema{Tables: []schema.Table{posts, users}}, schema.Schema{}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Index(got, `CREATE TABLE "users"`) > strings.Index(got, `CREATE TABLE "posts"`) {
		t.Fatal(got)
	}
}

func TestCycleKeepsForeignKeysOutsideCreate(t *testing.T) {
	a := schema.Table{
		Name:        "a",
		Columns:     []schema.Column{{Name: "b_id", Type: schema.Type{Name: "bigint"}}},
		ForeignKeys: []schema.ForeignKey{{Name: "a_b_id_fkey", Columns: []string{"b_id"}, RefTable: "b", RefColumns: []string{"id"}}},
	}
	b := schema.Table{
		Name:        "b",
		Columns:     []schema.Column{{Name: "id", Type: schema.Type{Name: "bigint"}}},
		ForeignKeys: []schema.ForeignKey{{Name: "b_id_fkey", Columns: []string{"id"}, RefTable: "a", RefColumns: []string{"b_id"}}},
	}
	got, err := Render(diff.Diff(schema.Schema{Tables: []schema.Table{a, b}}, schema.Schema{}))
	if err != nil {
		t.Fatal(err)
	}
	createA := got[:strings.Index(got, "ALTER TABLE")]
	if strings.Contains(createA, "FOREIGN KEY") {
		t.Fatal(got)
	}
	if strings.Count(got, "ADD CONSTRAINT") != 2 {
		t.Fatal(got)
	}
}

func TestDropTableIsReverseDependency(t *testing.T) {
	s := schema.Schema{Tables: []schema.Table{
		{Name: "users", Columns: []schema.Column{{Name: "id", Type: schema.Type{Name: "bigint"}}}},
		{
			Name:        "posts",
			Columns:     []schema.Column{{Name: "author_id", Type: schema.Type{Name: "bigint"}}},
			ForeignKeys: []schema.ForeignKey{{Name: "posts_author_id_fkey", Columns: []string{"author_id"}, RefTable: "users", RefColumns: []string{"id"}}},
		},
	}}
	got, err := Render(diff.Diff(schema.Schema{}, s))
	if err != nil {
		t.Fatal(err)
	}
	want := "DROP TABLE \"posts\";\n--> statement-breakpoint\nDROP TABLE \"users\";\n"
	if got != want {
		t.Fatalf("got\n%s", got)
	}
}

func TestEnumWithoutValuesErrors(t *testing.T) {
	_, err := Render(diff.Diff(schema.Schema{Enums: []schema.Enum{{Name: "status"}}}, schema.Schema{}))
	if err == nil || !strings.Contains(err.Error(), "has no values") {
		t.Fatal(err)
	}
}

func TestGeneratedChangeErrors(t *testing.T) {
	actual := schema.Schema{Tables: []schema.Table{{
		Name:    "users",
		Columns: []schema.Column{{Name: "n", Type: schema.Type{Name: "integer"}, Generated: "1"}},
	}}}
	desired := schema.Schema{Tables: []schema.Table{{
		Name:    "users",
		Columns: []schema.Column{{Name: "n", Type: schema.Type{Name: "integer"}, Generated: "2"}},
	}}}
	_, err := Render(diff.Diff(desired, actual))
	if err == nil || !strings.Contains(err.Error(), "generated") {
		t.Fatal(err)
	}
}

func loadFixture(t *testing.T) schema.Schema {
	t.Helper()
	s, err := codegen.LoadSchema(filepath.Join("..", "..", "internal", "schemafixture"))
	if err != nil {
		t.Fatal(err)
	}
	return s
}
