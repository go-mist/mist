package query_test

import (
	"context"
	"testing"

	"github.com/xbmlz/mist/internal/schemafixture"
	"github.com/xbmlz/mist/op"
	"github.com/xbmlz/mist/pg"
	"github.com/xbmlz/mist/sql"
)

func TestValueCountSQL(t *testing.T) {
	got, err := pg.Value[int64](op.Count()).
		From(schemafixture.Users).
		Where(op.Eq(schemafixture.Users.Name, "alice")).
		SQL(pg.Dialect{})
	if err != nil {
		t.Fatal(err)
	}
	const want = `select count(*) from "users" where "users"."name" = $1`
	if got.SQL != want || len(got.Params) != 1 || got.Params[0] != "alice" {
		t.Fatalf("got %#v", got)
	}
}

func TestValueOne(t *testing.T) {
	db := &fakeDB{rows: [][]any{{int64(2)}}}
	got, err := pg.Value[int64](op.Count(schemafixture.Users.ID)).
		From(schemafixture.Users).
		One(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	if got != 2 {
		t.Fatalf("count = %d", got)
	}
}

func TestScanGroup(t *testing.T) {
	db := &fakeDB{rows: [][]any{{"alice", int64(2)}, {"bob", int64(1)}}}
	type nameCount struct {
		Name string
		N    int64
	}
	got, err := pg.Scan(func(row sql.Row, dst *nameCount) error {
		return row.Scan(&dst.Name, &dst.N)
	}, schemafixture.Users.Name, op.Count(schemafixture.Users.ID)).
		From(schemafixture.Users).
		GroupBy(schemafixture.Users.Name).
		OrderBy(op.Asc(schemafixture.Users.Name)).
		All(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Name != "alice" || got[0].N != 2 || got[1].Name != "bob" || got[1].N != 1 {
		t.Fatalf("got %#v", got)
	}
}

func TestPreparedValue(t *testing.T) {
	db := &fakeDB{rows: [][]any{{int64(1)}}}
	stmt, err := pg.Value[int64](op.Count()).
		From(schemafixture.Users).
		Where(op.Eq(schemafixture.Users.Name, sql.NewPlaceholder("name"))).
		Prepare(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	got, err := stmt.One(context.Background(), map[string]any{"name": "alice"})
	if err != nil {
		t.Fatal(err)
	}
	if got != 1 || len(db.prepared) != 1 || len(db.lastArgs) != 1 || db.lastArgs[0] != "alice" {
		t.Fatalf("got %d prepared %#v args %#v", got, db.prepared, db.lastArgs)
	}
}
