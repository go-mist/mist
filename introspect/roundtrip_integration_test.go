//go:build integration

package introspect_test

import (
	"context"
	"testing"

	"github.com/go-mist/mist/ddl"
	mysqlddl "github.com/go-mist/mist/ddl/mysql"
	sqliteddl "github.com/go-mist/mist/ddl/sqlite"
	"github.com/go-mist/mist/dialect/mysql"
	"github.com/go-mist/mist/dialect/sqlite"
	"github.com/go-mist/mist/diff"
	"github.com/go-mist/mist/internal/testdb"
	"github.com/go-mist/mist/introspect"
	"github.com/go-mist/mist/schema"
)

func TestSQLiteRoundTrip(t *testing.T) {
	ctx := context.Background()
	db, err := sqlite.Open(ctx, testdb.SQLiteDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	want := schema.Schema{Tables: []schema.Table{{
		Name:       "round_items",
		PrimaryKey: []string{"id"},
		Columns: []schema.Column{
			{Name: "id", Type: schema.Type{Name: "integer"}, NotNull: true},
			{Name: "title", Type: schema.Type{Name: "text"}, NotNull: true},
		},
		Indexes: []schema.Index{{Name: "round_items_title", Columns: []string{"title"}}},
	}}}
	sqlText, err := sqliteddl.Render(diff.Diff(want, schema.Schema{}), want)
	if err != nil {
		t.Fatal(err)
	}
	if err := ddl.Execute(ctx, db, sqlText); err != nil {
		t.Fatal(err)
	}
	actual, err := introspect.ReadSQLite(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if changes := diff.Diff(want, actual); len(changes) != 0 {
		for _, change := range changes {
			t.Log(change.Summary)
		}
		t.Fatalf("%d changes", len(changes))
	}
}

func TestMySQLRoundTrip(t *testing.T) {
	ctx := context.Background()
	db, err := mysql.Open(ctx, testdb.MySQLDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	want := schema.Schema{Tables: []schema.Table{{
		Name:       "mist_my_round",
		PrimaryKey: []string{"id"},
		Columns: []schema.Column{
			{Name: "id", Type: schema.Type{Name: "bigint"}, NotNull: true, Identity: true},
			{Name: "title", Type: schema.Type{Name: "varchar", Length: 40}, NotNull: true},
		},
		Indexes: []schema.Index{{Name: "mist_my_round_title", Columns: []string{"title"}}},
	}}}
	sqlText, err := mysqlddl.Render(diff.Diff(want, schema.Schema{}))
	if err != nil {
		t.Fatal(err)
	}
	_, _ = db.Exec(ctx, "drop table if exists mist_my_round")
	if err := ddl.Execute(ctx, db, sqlText); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = db.Exec(context.Background(), "drop table if exists mist_my_round") })
	actual, err := introspect.ReadMySQL(ctx, db, "mist")
	if err != nil {
		t.Fatal(err)
	}
	var got schema.Schema
	for _, table := range actual.Tables {
		if table.Name == "mist_my_round" {
			got.Tables = append(got.Tables, table)
		}
	}
	if changes := diff.Diff(want, got); len(changes) != 0 {
		for _, change := range changes {
			t.Log(change.Kind, change.Summary)
		}
		t.Fatalf("%d changes", len(changes))
	}
}
