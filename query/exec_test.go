package query_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/go-mist/mist/dialect/pg"
	"github.com/go-mist/mist/internal/schemafixture"
	"github.com/go-mist/mist/query"
)

func TestSelectAllAndOne(t *testing.T) {
	db := &fakeDB{rows: [][]any{
		{int64(7), "alice"},
		{int64(8), "bob"},
	}}
	got, err := pg.Select(schemafixture.Users.ID, schemafixture.Users.Name).
		From(schemafixture.Users).
		All(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != 7 || got[0].Name != "alice" || got[1].Name != "bob" {
		t.Fatalf("all = %#v", got)
	}
	if got[0].Email != nil || !got[0].CreatedAt.IsZero() {
		t.Fatalf("unselected fields were filled: %#v", got[0])
	}

	db.rows = [][]any{{int64(7), "alice"}}
	one, err := pg.Select(schemafixture.Users.ID, schemafixture.Users.Name).
		From(schemafixture.Users).
		One(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	if one.ID != 7 || one.Name != "alice" {
		t.Fatalf("one = %#v", one)
	}
}

func TestSelectOneRowCounts(t *testing.T) {
	db := &fakeDB{}
	_, err := pg.Select(schemafixture.Users.ID).From(schemafixture.Users).One(context.Background(), db)
	if !errors.Is(err, query.ErrNoRows) {
		t.Fatalf("err = %v", err)
	}

	db.rows = [][]any{{int64(1)}, {int64(2)}}
	_, err = pg.Select(schemafixture.Users.ID).From(schemafixture.Users).One(context.Background(), db)
	if !errors.Is(err, query.ErrManyRows) {
		t.Fatalf("err = %v", err)
	}
}

func TestSelectWholeRowScan(t *testing.T) {
	email := "a@b.c"
	created := time.Unix(10, 0).UTC()
	db := &fakeDB{rows: [][]any{{int64(7), "alice", &email, created}}}
	got, err := pg.Select[schemafixture.User]().From(schemafixture.Users).One(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != 7 || got.Name != "alice" || got.Email == nil || *got.Email != email || !got.CreatedAt.Equal(created) {
		t.Fatalf("whole = %#v", got)
	}
}

func TestUpdateAndDeleteRequireWhere(t *testing.T) {
	ctx := context.Background()
	db := &fakeDB{}
	_, err := pg.Update(schemafixture.Users).Set(schemafixture.Users.Name, "bea").Exec(ctx, db)
	if err == nil || err.Error() != "query: update has no where" {
		t.Fatal(err)
	}
	_, err = pg.Delete(schemafixture.Users).Exec(ctx, db)
	if err == nil || err.Error() != "query: delete has no where" {
		t.Fatal(err)
	}
	if _, err := pg.Update(schemafixture.Users).Set(schemafixture.Users.Name, "bea").AllRows().Exec(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, err := pg.Delete(schemafixture.Users).AllRows().Exec(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, err := pg.Update(schemafixture.Users).Set(schemafixture.Users.Name, "bea").Prepare(ctx, db); err == nil {
		t.Fatal("prepare ran an update with no where")
	}
}

func TestVersionConflict(t *testing.T) {
	ctx := context.Background()
	db := &fakeDB{countSet: true}
	_, err := pg.Update(schemafixture.Users).
		Set(schemafixture.Users.Name, "bea").
		Version(schemafixture.Users.ID, int64(3)).
		Exec(ctx, db)
	if !errors.Is(err, query.ErrVersionConflict) {
		t.Fatal(err)
	}
	db.countSet = false
	n, err := pg.Update(schemafixture.Users).
		Set(schemafixture.Users.Name, "bea").
		Version(schemafixture.Users.ID, int64(3)).
		Exec(ctx, db)
	if err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
}

func TestReturningOne(t *testing.T) {
	db := &fakeDB{rows: [][]any{{int64(4), "cea"}}}
	got, err := pg.Insert(schemafixture.Users).
		Columns(schemafixture.Users.ID, schemafixture.Users.Name).
		Values(int64(4), "cea").
		Returning(schemafixture.Users.ID, schemafixture.Users.Name).
		One(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != 4 || got.Name != "cea" {
		t.Fatalf("got %#v", got)
	}
	if _, err := pg.Insert(schemafixture.Users).Columns(schemafixture.Users.Name).Values("x").All(context.Background(), db); err == nil {
		t.Fatal("insert without returning should fail")
	}
}

type fakeDB struct {
	pg.Dialect
	rows     [][]any
	prepared []string
	lastArgs []any
	affected int64
	countSet bool
}

func (f *fakeDB) Query(context.Context, string, ...any) (query.Rows, error) {
	return &fakeRows{data: f.rows}, nil
}

func (f *fakeDB) Exec(context.Context, string, ...any) (int64, error) {
	if f.countSet {
		return f.affected, nil
	}
	return 1, nil
}

func (f *fakeDB) Prepare(_ context.Context, sql string) (query.Stmt, error) {
	f.prepared = append(f.prepared, sql)
	return &fakeStmt{db: f}, nil
}

type fakeStmt struct {
	db *fakeDB
}

func (s *fakeStmt) Query(_ context.Context, args ...any) (query.Rows, error) {
	s.db.lastArgs = args
	return &fakeRows{data: s.db.rows}, nil
}

func (s *fakeStmt) Exec(_ context.Context, args ...any) (int64, error) {
	s.db.lastArgs = args
	if s.db.countSet {
		return s.db.affected, nil
	}
	return 1, nil
}

func (s *fakeStmt) Close() error { return nil }

type fakeRows struct {
	data [][]any
	pos  int
}

func (f *fakeRows) Next() bool {
	if f.pos >= len(f.data) {
		return false
	}
	f.pos++
	return true
}

func (f *fakeRows) Scan(dest ...any) error {
	row := f.data[f.pos-1]
	for i := range dest {
		target := reflect.ValueOf(dest[i]).Elem()
		if row[i] == nil {
			target.SetZero()
			continue
		}
		target.Set(reflect.ValueOf(row[i]))
	}
	return nil
}

func (f *fakeRows) Err() error { return nil }

func (f *fakeRows) Close() {}
