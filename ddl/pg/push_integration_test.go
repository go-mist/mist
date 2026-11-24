//go:build integration

package pg

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/go-mist/mist"
	"github.com/go-mist/mist/diff"
	"github.com/go-mist/mist/internal/testdb"
	"github.com/go-mist/mist/introspect"
	"github.com/go-mist/mist/migrate"
	"github.com/go-mist/mist/pg"
	"github.com/go-mist/mist/schema"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPushDropWaitsForForce(t *testing.T) {
	ctx := context.Background()
	const name = "m7push"
	db := openSchemaDB(t, ctx, name)
	resetSchema(t, ctx, db, name)
	if err := applyInSchema(ctx, db, name, mustSQL(t, diff.Diff(usersV1(), schema.Schema{}))); err != nil {
		t.Fatal(err)
	}
	got, err := introspect.Read(ctx, db, name)
	if err != nil {
		t.Fatal(err)
	}
	if changes := diff.Diff(usersV1(), got); len(changes) != 0 {
		t.Fatal(formatPush(changes))
	}
	drop := diff.Diff(usersIDOnly(), got)
	if !HasDestructive(drop) {
		t.Fatal("drop column was not marked destructive")
	}
	sqlText := mustSQL(t, drop)
	if !strings.Contains(sqlText, `DROP COLUMN "name"`) {
		t.Fatal(sqlText)
	}
	again, err := introspect.Read(ctx, db, name)
	if err != nil {
		t.Fatal(err)
	}
	if changes := diff.Diff(usersV1(), again); len(changes) != 0 {
		t.Fatal("not applied yet, but the column is already gone")
	}
	if err := applyInSchema(ctx, db, name, sqlText); err != nil {
		t.Fatal(err)
	}
	final, err := introspect.Read(ctx, db, name)
	if err != nil {
		t.Fatal(err)
	}
	if changes := diff.Diff(usersIDOnly(), final); len(changes) != 0 {
		t.Fatal(formatPush(changes))
	}
}

func TestMediumSchemaRoundTrip(t *testing.T) {
	folder := t.TempDir()
	want := mediumSchema()
	if _, err := WriteMigration(folder, "init", time.Date(2026, 11, 23, 6, 0, 0, 0, time.UTC), want); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	const name = "m7med"
	db := openSchemaDB(t, ctx, name)
	resetSchema(t, ctx, db, name)
	mig := "m7medmig"
	t.Cleanup(func() {
		_, _ = db.Exec(context.Background(), "drop schema if exists "+pg.Dialect{}.EscapeName(mig)+" cascade")
	})
	applied, err := migrate.Run(ctx, pg.Migrator(db), migrate.Config{Folder: folder, Schema: mig})
	if err != nil {
		t.Fatal(err)
	}
	if applied != 1 {
		t.Fatalf("applied %d", applied)
	}
	got, err := introspect.Read(ctx, db, name)
	if err != nil {
		t.Fatal(err)
	}
	if changes := diff.Diff(want, got); len(changes) != 0 {
		t.Fatal(formatPush(changes))
	}
}

func usersIDOnly() schema.Schema {
	return schema.Schema{Tables: []schema.Table{{
		Name:       "users",
		PrimaryKey: []string{"id"},
		Columns:    []schema.Column{{Name: "id", Type: schema.Type{Name: "bigserial"}, NotNull: true}},
	}}}
}

func mediumSchema() schema.Schema {
	tables := []schema.Table{
		{
			Name:       "m7_org",
			PrimaryKey: []string{"tenant", "region"},
			Columns: []schema.Column{
				{Name: "tenant", Type: schema.Type{Name: "integer"}, NotNull: true},
				{Name: "region", Type: schema.Type{Name: "integer"}, NotNull: true},
				{Name: "name", Type: schema.Type{Name: "text"}, NotNull: true, Default: "'acme'"},
			},
			Indexes: []schema.Index{{Name: "m7_org_name_idx", Columns: []string{"name"}}},
		},
		{
			Name:       "m7_user",
			PrimaryKey: []string{"id"},
			Columns: []schema.Column{
				{Name: "id", Type: schema.Type{Name: "bigserial"}, NotNull: true},
				{Name: "tenant", Type: schema.Type{Name: "integer"}, NotNull: true},
				{Name: "region", Type: schema.Type{Name: "integer"}, NotNull: true},
				{Name: "email", Type: schema.Type{Name: "text"}},
				{Name: "code", Type: schema.Type{Name: "varchar", Length: 8}},
				{Name: "status", Type: schema.Type{Name: "m7_status", Enum: true}},
				{Name: "created_at", Type: schema.Type{Name: "timestamptz"}, NotNull: true, Default: "now()"},
			},
			Uniques: []schema.Unique{{Name: "m7_user_email_key", Columns: []string{"email"}}},
			ForeignKeys: []schema.ForeignKey{{
				Name: "m7_user_org_fkey", Columns: []string{"tenant", "region"},
				RefTable: "m7_org", RefColumns: []string{"tenant", "region"}, OnDelete: "cascade",
			}},
		},
	}
	for i := 3; i <= 20; i++ {
		table := fmt.Sprintf("m7_t%02d", i)
		tables = append(tables, schema.Table{
			Name:       table,
			PrimaryKey: []string{"id"},
			Columns: []schema.Column{
				{Name: "id", Type: schema.Type{Name: "bigserial"}, NotNull: true},
				{Name: "user_id", Type: schema.Type{Name: "bigint"}, NotNull: true},
				{Name: "note", Type: schema.Type{Name: "text"}, NotNull: true, Default: "''"},
			},
			Indexes: []schema.Index{{Name: table + "_user_idx", Columns: []string{"user_id"}}},
			ForeignKeys: []schema.ForeignKey{{
				Name: table + "_user_id_fkey", Columns: []string{"user_id"},
				RefTable: "m7_user", RefColumns: []string{"id"}, OnDelete: "cascade",
			}},
		})
	}
	return schema.Schema{
		Enums:  []schema.Enum{{Name: "m7_status", Values: []string{"on", "off"}}},
		Tables: tables,
	}
}

func mustSQL(t *testing.T, changes []diff.Change) string {
	t.Helper()
	sqlText, err := Render(changes)
	if err != nil {
		t.Fatal(err)
	}
	return sqlText
}

func applyInSchema(ctx context.Context, db *pg.DB, name, sqlText string) error {
	return db.Tx(ctx, func(tx mist.Tx) error {
		if _, err := tx.Exec(ctx, "set local search_path to "+pg.Dialect{}.EscapeName(name)); err != nil {
			return err
		}
		return Execute(ctx, tx, sqlText)
	})
}

func resetSchema(t *testing.T, ctx context.Context, db *pg.DB, name string) {
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

func openSchemaDB(t *testing.T, ctx context.Context, name string) *pg.DB {
	t.Helper()
	dsn := testdb.PostgresDSN(t)
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.MaxConns = 2
	cfg.ConnConfig.RuntimeParams["search_path"] = name
	db, err := pg.OpenConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func formatPush(changes []diff.Change) string {
	var b strings.Builder
	for _, change := range changes {
		fmt.Fprintf(&b, "%s %s\n", change.Kind, change.Summary)
		if change.Column != nil || change.ColumnFrom != nil {
			fmt.Fprintf(&b, "  from %+v\n  to %+v\n", change.ColumnFrom, change.Column)
		}
	}
	return b.String()
}
