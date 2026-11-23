package query_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/xbmlz/mist/internal/schemafixture"
	"github.com/xbmlz/mist/op"
	"github.com/xbmlz/mist/pg"
	"github.com/xbmlz/mist/query"
	"github.com/xbmlz/mist/sql"
)

func TestPreparedSelectFillsPlaceholders(t *testing.T) {
	ctx := context.Background()
	db := &fakeDB{rows: [][]any{{int64(1), "alice"}}}
	stmt, err := pg.Select(schemafixture.Users.ID, schemafixture.Users.Name).
		From(schemafixture.Users).
		Where(op.Eq(schemafixture.Users.Name, sql.NewPlaceholder("name"))).
		Prepare(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if len(db.prepared) != 1 || !strings.Contains(db.prepared[0], `$1`) {
		t.Fatalf("prepared = %#v", db.prepared)
	}

	got, err := stmt.One(ctx, map[string]any{"name": "alice"})
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != 1 || got.Name != "alice" || len(db.lastArgs) != 1 || db.lastArgs[0] != "alice" {
		t.Fatalf("got %#v args %#v", got, db.lastArgs)
	}

	db.rows = [][]any{{int64(2), "bob"}}
	got, err = stmt.One(ctx, map[string]any{"name": "bob"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "bob" || db.lastArgs[0] != "bob" || len(db.prepared) != 1 {
		t.Fatalf("second got %#v prepared %d args %#v", got, len(db.prepared), db.lastArgs)
	}
	if _, err := stmt.One(ctx, nil); err == nil {
		t.Fatal("missing placeholder should fail")
	}
	if err := stmt.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := stmt.One(ctx, map[string]any{"name": "alice"}); !errors.Is(err, query.ErrClosed) {
		t.Fatalf("closed err = %v", err)
	}
}

func TestPreparedWholeRow(t *testing.T) {
	email := "a@b.c"
	created := time.Unix(10, 0).UTC()
	db := &fakeDB{rows: [][]any{{int64(7), "alice", &email, created}}}
	stmt, err := pg.Select[schemafixture.User]().
		From(schemafixture.Users).
		Where(op.Eq(schemafixture.Users.ID, sql.NewPlaceholder("id"))).
		Prepare(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	got, err := stmt.One(context.Background(), map[string]any{"id": int64(7)})
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != 7 || got.Name != "alice" || got.Email == nil || *got.Email != email || !got.CreatedAt.Equal(created) {
		t.Fatalf("whole = %#v", got)
	}
	if db.lastArgs[0] != int64(7) {
		t.Fatalf("args = %#v", db.lastArgs)
	}
}

func TestPreparedWrite(t *testing.T) {
	ctx := context.Background()
	db := &fakeDB{}
	ins, err := pg.Insert(schemafixture.Users).
		Columns(schemafixture.Users.Name).
		Values(sql.NewPlaceholder("name")).
		Prepare(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	n, err := ins.Exec(ctx, map[string]any{"name": "cara"})
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 || len(db.lastArgs) != 1 || db.lastArgs[0] != "cara" {
		t.Fatalf("insert n=%d args %#v", n, db.lastArgs)
	}

	upd, err := pg.Update(schemafixture.Users).
		Set(schemafixture.Users.Name, sql.NewPlaceholder("name")).
		Where(op.Eq(schemafixture.Users.ID, sql.NewPlaceholder("id"))).
		Prepare(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := upd.Exec(ctx, map[string]any{"name": "ann", "id": int64(1)}); err != nil {
		t.Fatal(err)
	}
	if len(db.lastArgs) != 2 || db.lastArgs[0] != "ann" || db.lastArgs[1] != int64(1) {
		t.Fatalf("update args %#v", db.lastArgs)
	}

	del, err := pg.Delete(schemafixture.Users).
		Where(op.Eq(schemafixture.Users.ID, sql.NewPlaceholder("id"))).
		Prepare(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := del.Exec(ctx, map[string]any{"id": int64(2)}); err != nil {
		t.Fatal(err)
	}
	if len(db.lastArgs) != 1 || db.lastArgs[0] != int64(2) {
		t.Fatalf("delete args %#v", db.lastArgs)
	}
	if len(db.prepared) != 3 {
		t.Fatalf("prepared %d times", len(db.prepared))
	}
}

func TestPreparedReturning(t *testing.T) {
	ctx := context.Background()
	db := &fakeDB{rows: [][]any{{int64(9), "cara"}}}
	stmt, err := pg.Insert(schemafixture.Users).
		Columns(schemafixture.Users.ID, schemafixture.Users.Name).
		Values(sql.NewPlaceholder("id"), sql.NewPlaceholder("name")).
		Returning(schemafixture.Users.ID, schemafixture.Users.Name).
		Prepare(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := stmt.Exec(ctx, map[string]any{"id": int64(9), "name": "cara"}); err == nil {
		t.Fatal("returning exec should fail")
	}
	got, err := stmt.One(ctx, map[string]any{"id": int64(9), "name": "cara"})
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != 9 || got.Name != "cara" {
		t.Fatalf("got %#v", got)
	}
	if len(db.lastArgs) != 2 || db.lastArgs[0] != int64(9) || db.lastArgs[1] != "cara" {
		t.Fatalf("args %#v", db.lastArgs)
	}
}
