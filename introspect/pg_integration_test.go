//go:build integration

package introspect

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/xbmlz/mist/codegen"
	ddlpg "github.com/xbmlz/mist/ddl/pg"
	"github.com/xbmlz/mist/diff"
	"github.com/xbmlz/mist/internal/testdb"
	"github.com/xbmlz/mist/pg"
	"github.com/xbmlz/mist/schema"
)

func TestRoundTrip(t *testing.T) {
	ctx := context.Background()
	db := openDB(t, ctx)

	t.Run("fixture", func(t *testing.T) {
		want, err := codegen.LoadSchema(filepath.Join("..", "internal", "schemafixture"))
		if err != nil {
			t.Fatal(err)
		}
		roundTrip(t, ctx, db, "m7fix", want)
	})

	t.Run("rich", func(t *testing.T) {
		roundTrip(t, ctx, db, "m7rich", richSchema())
	})

	t.Run("empty", func(t *testing.T) {
		reset(t, ctx, db, "m7empty")
		got, err := Read(ctx, db, "m7empty")
		if err != nil {
			t.Fatal(err)
		}
		if len(got.Tables) != 0 || len(got.Enums) != 0 {
			t.Fatalf("%+v", got)
		}
	})

	t.Run("missing schema", func(t *testing.T) {
		if _, err := Read(ctx, db, "m7missing"); err == nil || !strings.Contains(err.Error(), "does not exist") {
			t.Fatal(err)
		}
	})

	t.Run("expression index", func(t *testing.T) {
		reset(t, ctx, db, "m7expr")
		if _, err := db.Exec(ctx, `create table m7expr.t (id int, name text)`); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(ctx, `create index t_name on m7expr.t ((lower(name)))`); err != nil {
			t.Fatal(err)
		}
		_, err := Read(ctx, db, "m7expr")
		if err == nil || !strings.Contains(err.Error(), "not supported yet") {
			t.Fatal(err)
		}
	})

	t.Run("partition", func(t *testing.T) {
		reset(t, ctx, db, "m7part")
		if _, err := db.Exec(ctx, `create table m7part.p (id int) partition by range (id)`); err != nil {
			t.Fatal(err)
		}
		_, err := Read(ctx, db, "m7part")
		if err == nil || !strings.Contains(err.Error(), "partitioned table") {
			t.Fatal(err)
		}
	})
}

func roundTrip(t *testing.T, ctx context.Context, db *pg.DB, schemaName string, want schema.Schema) {
	t.Helper()
	sqlText, err := ddlpg.Render(diff.Diff(want, schema.Schema{}))
	if err != nil {
		t.Fatal(err)
	}
	reset(t, ctx, db, schemaName)
	err = db.Tx(ctx, func(tx *pg.Tx) error {
		if _, err := tx.Exec(ctx, "set local search_path to "+pg.Dialect{}.EscapeName(schemaName)); err != nil {
			return err
		}
		for _, stmt := range splitSQL(sqlText) {
			if _, err := tx.Exec(ctx, stmt); err != nil {
				return fmt.Errorf("%s: %w", stmt, err)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := Read(ctx, db, schemaName)
	if err != nil {
		t.Fatal(err)
	}
	changes := diff.Diff(want, got)
	if len(changes) != 0 {
		t.Fatalf("%s\nactual:\n%s", formatChanges(changes), formatSchema(got))
	}
}

func richSchema() schema.Schema {
	return schema.Schema{
		Enums: []schema.Enum{{Name: "status", Values: []string{"on", "off"}}},
		Tables: []schema.Table{
			{
				Name: "users",
				Columns: []schema.Column{
					{Name: "id", Type: schema.Type{Name: "bigserial"}, NotNull: true},
					{Name: "name", Type: schema.Type{Name: "text"}, NotNull: true},
					{Name: "email", Type: schema.Type{Name: "text"}},
					{Name: "code", Type: schema.Type{Name: "varchar", Length: 8}},
					{Name: "amount", Type: schema.Type{Name: "numeric", Precision: 10, Scale: 2}},
					{Name: "tags", Type: schema.Type{Name: "text", ArrayDims: 1}},
					{Name: "created_at", Type: schema.Type{Name: "timestamptz"}, NotNull: true, Default: "now()"},
					{Name: "status", Type: schema.Type{Name: "status", Enum: true}},
					{Name: "active", Type: schema.Type{Name: "boolean"}, NotNull: true, Default: "true"},
					{Name: "label", Type: schema.Type{Name: "text"}, Default: "'x'"},
					{Name: "n", Type: schema.Type{Name: "integer"}, NotNull: true, Generated: "1"},
				},
				PrimaryKey: []string{"id"},
				Uniques:    []schema.Unique{{Name: "users_email_key", Columns: []string{"email"}}},
				Indexes:    []schema.Index{{Name: "users_name_idx", Columns: []string{"name"}, Unique: true}},
				Checks:     []schema.Check{{Name: "users_name_len", Expr: "length(name) > 0"}},
			},
			{
				Name: "posts",
				Columns: []schema.Column{
					{Name: "id", Type: schema.Type{Name: "bigint"}, NotNull: true, Identity: true},
					{Name: "author_id", Type: schema.Type{Name: "bigint"}, NotNull: true},
					{Name: "title", Type: schema.Type{Name: "text"}, NotNull: true},
				},
				PrimaryKey: []string{"id"},
				ForeignKeys: []schema.ForeignKey{{
					Name: "posts_author_id_fkey", Columns: []string{"author_id"},
					RefTable: "users", RefColumns: []string{"id"}, OnDelete: "cascade",
				}},
			},
			{
				Name: "pair",
				Columns: []schema.Column{
					{Name: "a", Type: schema.Type{Name: "integer"}, NotNull: true},
					{Name: "b", Type: schema.Type{Name: "integer"}, NotNull: true},
				},
				PrimaryKey: []string{"a", "b"},
			},
		},
	}
}

func splitSQL(sqlText string) []string {
	var out []string
	for _, part := range strings.Split(sqlText, "--> statement-breakpoint") {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func reset(t *testing.T, ctx context.Context, db *pg.DB, name string) {
	t.Helper()
	quoted := pg.Dialect{}.EscapeName(name)
	if _, err := db.Exec(ctx, "drop schema if exists "+quoted+" cascade"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, "create schema "+quoted); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(context.Background(), "drop schema if exists "+quoted+" cascade")
	})
}

func formatChanges(changes []diff.Change) string {
	var b strings.Builder
	for _, change := range changes {
		fmt.Fprintf(&b, "%s %s\n", change.Kind, change.Summary)
		if change.Column != nil || change.ColumnFrom != nil {
			fmt.Fprintf(&b, "  from %+v\n  to %+v\n", change.ColumnFrom, change.Column)
		}
	}
	return b.String()
}

func formatSchema(s schema.Schema) string {
	var b strings.Builder
	for _, enum := range s.Enums {
		fmt.Fprintf(&b, "enum %s %v\n", enum.Name, enum.Values)
	}
	for _, table := range s.Tables {
		fmt.Fprintf(&b, "table %s pk=%v %s\n", table.Name, table.PrimaryKey, table.PrimaryKeyName)
		for _, col := range table.Columns {
			fmt.Fprintf(&b, "  %s %+v null=%v def=%q gen=%q identity=%v\n", col.Name, col.Type, col.NotNull, col.Default, col.Generated, col.Identity)
		}
		for _, unique := range table.Uniques {
			fmt.Fprintf(&b, "  unique %s %v\n", unique.Name, unique.Columns)
		}
		for _, idx := range table.Indexes {
			fmt.Fprintf(&b, "  index %s %v unique=%v\n", idx.Name, idx.Columns, idx.Unique)
		}
		for _, check := range table.Checks {
			fmt.Fprintf(&b, "  check %s %s\n", check.Name, check.Expr)
		}
		for _, fk := range table.ForeignKeys {
			fmt.Fprintf(&b, "  fk %s %v -> %s %v %s %s\n", fk.Name, fk.Columns, fk.RefTable, fk.RefColumns, fk.OnDelete, fk.OnUpdate)
		}
	}
	return b.String()
}

func openDB(t *testing.T, ctx context.Context) *pg.DB {
	t.Helper()
	dsn := testdb.PostgresDSN(t)
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.MaxConns = 4
	db, err := pg.OpenConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	return db
}
