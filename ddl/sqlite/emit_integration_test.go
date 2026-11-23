//go:build integration

package sqlite_test

import (
	"context"
	"strings"
	"testing"

	sqliteddl "github.com/xbmlz/mist/ddl/sqlite"
	"github.com/xbmlz/mist/diff"
	"github.com/xbmlz/mist/internal/testdb"
	"github.com/xbmlz/mist/schema"
	"github.com/xbmlz/mist/sqlite"
)

func TestAlterColumnRuns(t *testing.T) {
	ctx := context.Background()
	db, err := sqlite.Open(ctx, testdb.SQLiteDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(ctx, `create table "users" ("id" integer primary key, "name" text not null)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `insert into "users" ("id", "name") values (1, 'ada')`); err != nil {
		t.Fatal(err)
	}
	have := schema.Schema{Tables: []schema.Table{{
		Name: "users",
		Columns: []schema.Column{
			{Name: "id", Type: schema.Type{Name: "integer"}, NotNull: true},
			{Name: "name", Type: schema.Type{Name: "text"}, NotNull: true},
		},
		PrimaryKey: []string{"id"},
	}}}
	want := schema.Schema{Tables: []schema.Table{{
		Name: "users",
		Columns: []schema.Column{
			{Name: "id", Type: schema.Type{Name: "integer"}, NotNull: true},
			{Name: "name", Type: schema.Type{Name: "varchar", Length: 40}, NotNull: true},
		},
		PrimaryKey: []string{"id"},
	}}}
	script, err := sqliteddl.Render(diff.Diff(want, have), want)
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range strings.Split(script, "--> statement-breakpoint") {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}
		if _, err := db.Exec(ctx, stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	var name, typ string
	rows, err := db.Query(ctx, `select "name", typeof("name") from "users" where "id" = 1`)
	if err != nil {
		t.Fatal(err)
	}
	if !rows.Next() {
		t.Fatal(rows.Err())
	}
	if err := rows.Scan(&name, &typ); err != nil {
		t.Fatal(err)
	}
	rows.Close()
	if name != "ada" {
		t.Fatal(name)
	}
	var declared string
	info, err := db.Query(ctx, `select type from pragma_table_info('users') where name = 'name'`)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Next() {
		t.Fatal(info.Err())
	}
	if err := info.Scan(&declared); err != nil {
		t.Fatal(err)
	}
	info.Close()
	if declared != "varchar(40)" {
		t.Fatalf("declared %s typeof %s", declared, typ)
	}
}
