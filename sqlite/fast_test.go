package sqlite_test

import (
	"context"
	"errors"
	"testing"

	"github.com/xbmlz/mist/op"
	"github.com/xbmlz/mist/query"
	"github.com/xbmlz/mist/sql"
	"github.com/xbmlz/mist/sqlite"
)

type fastUser struct {
	ID   int64
	Name string
}

type fastTable struct {
	sqlite.TableBase
	ID   sqlite.Col[fastUser, int64]
	Name sqlite.Col[fastUser, string]
}

func (fastTable) Scan(row sqlite.Row, dst *fastUser) error {
	return row.Scan(&dst.ID, &dst.Name)
}

func (fastTable) Row() *fastUser { return nil }

func (t fastTable) Columns() []sql.Chunk { return []sql.Chunk{t.ID, t.Name} }

func newFastTable() fastTable {
	return fastTable{
		TableBase: sqlite.TableBase{SQLName: "users"},
		ID: sqlite.Col[fastUser, int64]{
			Table: "users", Name: "id",
			Ptr: func(v *fastUser) any { return &v.ID },
		},
		Name: sqlite.Col[fastUser, string]{
			Table: "users", Name: "name",
			Ptr: func(v *fastUser) any { return &v.Name },
		},
	}
}

func TestFastSelectMatchesSlow(t *testing.T) {
	u := newFastTable()
	q := sqlite.Select(u.ID, u.Name).From(u).Where(op.Eq(u.Name, "ada")).OrderBy(op.Asc(u.ID)).Limit(2)
	fast, ok, err := sqlite.Dialect{}.RenderSelect(q.Spec())
	if err != nil || !ok {
		t.Fatalf("fast path declined: %v", err)
	}
	built, err := sqlite.Dialect{}.BuildSelectQuery(q.Spec())
	if err != nil {
		t.Fatal(err)
	}
	slow, err := sql.Render(built, sqlite.Dialect{}, sql.RenderConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if fast.SQL != slow.SQL {
		t.Fatalf("sql\n got %s\nwant %s", fast.SQL, slow.SQL)
	}
	if len(fast.Params) != len(slow.Params) {
		t.Fatalf("params %#v vs %#v", fast.Params, slow.Params)
	}
	for i := range fast.Params {
		if fast.Params[i] != slow.Params[i] {
			t.Fatalf("param %d %#v vs %#v", i, fast.Params[i], slow.Params[i])
		}
	}
	want := `select "users"."id", "users"."name" from "users" where "users"."name" = ? order by "users"."id" asc limit ?`
	if fast.SQL != want {
		t.Fatalf("sql\n got %s\nwant %s", fast.SQL, want)
	}
}

func TestFastInsertMatchesSlow(t *testing.T) {
	u := newFastTable()
	q := sqlite.Insert(u).Columns(u.ID, u.Name).Values(int64(1), "ada")
	fast, ok, err := sqlite.Dialect{}.RenderInsert(q.Spec())
	if err != nil || !ok {
		t.Fatalf("fast path declined: %v", err)
	}
	built, err := sqlite.Dialect{}.BuildInsertQuery(q.Spec())
	if err != nil {
		t.Fatal(err)
	}
	slow, err := sql.Render(built, sqlite.Dialect{}, sql.RenderConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if fast.SQL != slow.SQL {
		t.Fatalf("sql\n got %s\nwant %s", fast.SQL, slow.SQL)
	}
	want := `insert into "users" ("id", "name") values (?, ?)`
	if fast.SQL != want {
		t.Fatalf("sql\n got %s\nwant %s", fast.SQL, want)
	}
}

func TestFastUpdateMatchesSlow(t *testing.T) {
	u := newFastTable()
	q := sqlite.Update(u).Set(u.Name, "bea").Where(op.Eq(u.ID, int64(1)))
	fast, ok, err := sqlite.Dialect{}.RenderUpdate(q.Spec())
	if err != nil || !ok {
		t.Fatalf("fast path declined: %v", err)
	}
	built, err := sqlite.Dialect{}.BuildUpdateQuery(q.Spec())
	if err != nil {
		t.Fatal(err)
	}
	slow, err := sql.Render(built, sqlite.Dialect{}, sql.RenderConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if fast.SQL != slow.SQL {
		t.Fatalf("sql\n got %s\nwant %s", fast.SQL, slow.SQL)
	}
	want := `update "users" set "name" = ? where "users"."id" = ?`
	if fast.SQL != want {
		t.Fatalf("sql\n got %s\nwant %s", fast.SQL, want)
	}
}

type ptrUser struct {
	ID   int64
	Name string
}

type ptrTable struct {
	sqlite.TableBase
	ID   *sqlite.Col[ptrUser, int64]
	Name *sqlite.Col[ptrUser, string]
}

func (ptrTable) Scan(row sqlite.Row, dst *ptrUser) error {
	return row.Scan(&dst.ID, &dst.Name)
}

func (ptrTable) Row() *ptrUser { return nil }

func (t ptrTable) Columns() []sql.Chunk { return []sql.Chunk{t.ID, t.Name} }

func newPtrTable() *ptrTable {
	return &ptrTable{
		TableBase: sqlite.TableBase{SQLName: "users"},
		ID: &sqlite.Col[ptrUser, int64]{
			Table: "users", Name: "id",
			Ptr: func(v *ptrUser) any { return &v.ID },
		},
		Name: &sqlite.Col[ptrUser, string]{
			Table: "users", Name: "name",
			Ptr: func(v *ptrUser) any { return &v.Name },
		},
	}
}

// slowDB fails if execution falls back to the general builder.
type slowDB struct {
	sqlite.Dialect
	sql  string
	args []any
	rows [][]any
}

func (slowDB) RenderSelect(query.SelectSpec) (sql.Query, bool, error) {
	return sql.Query{}, false, nil
}

func (slowDB) RenderInsert(query.InsertSpec) (sql.Query, bool, error) {
	return sql.Query{}, false, nil
}

func (slowDB) RenderUpdate(query.UpdateSpec) (sql.Query, bool, error) {
	return sql.Query{}, false, nil
}

func (slowDB) BuildSelectQuery(query.SelectSpec) (*sql.SQL, error) {
	return nil, errors.New("slow select")
}

func (slowDB) BuildInsertQuery(query.InsertSpec) (*sql.SQL, error) {
	return nil, errors.New("slow insert")
}

func (slowDB) BuildUpdateQuery(query.UpdateSpec) (*sql.SQL, error) {
	return nil, errors.New("slow update")
}

func (slowDB) BuildDeleteQuery(query.DeleteSpec) (*sql.SQL, error) {
	return nil, errors.New("slow delete")
}

func (s *slowDB) Exec(_ context.Context, query string, args ...any) (int64, error) {
	s.sql = query
	s.args = append([]any(nil), args...)
	return 1, nil
}

func (s *slowDB) Query(_ context.Context, query string, args ...any) (query.Rows, error) {
	s.sql = query
	s.args = append([]any(nil), args...)
	return &slowRows{data: s.rows}, nil
}

func (slowDB) Prepare(context.Context, string) (query.Stmt, error) {
	return nil, errors.New("prepare")
}

type slowRows struct {
	data [][]any
	pos  int
}

func (r *slowRows) Next() bool {
	if r.pos >= len(r.data) {
		return false
	}
	r.pos++
	return true
}

func (r *slowRows) Scan(dest ...any) error {
	row := r.data[r.pos-1]
	for i := range dest {
		switch d := dest[i].(type) {
		case *int64:
			*d = row[i].(int64)
		case *string:
			*d = row[i].(string)
		default:
			return errors.New("scan")
		}
	}
	return nil
}

func (r *slowRows) Err() error { return nil }
func (r *slowRows) Close()     {}

func TestFastExecMatchesSQL(t *testing.T) {
	ctx := context.Background()
	u := newPtrTable()

	ins := sqlite.Insert(u).Columns(u.Name).Values("ada")
	want, err := ins.SQL(sqlite.Dialect{})
	if err != nil {
		t.Fatal(err)
	}
	db := &slowDB{}
	if _, err := ins.Exec(ctx, db); err != nil {
		t.Fatal(err)
	}
	if db.sql != want.SQL || len(db.args) != 1 || db.args[0] != "ada" {
		t.Fatalf("insert %s %#v", db.sql, db.args)
	}

	upd := sqlite.Update(u).Set(u.Name, "bea").Where(op.Eq(u.ID, int64(1)))
	want, err = upd.SQL(sqlite.Dialect{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := upd.Exec(ctx, db); err != nil {
		t.Fatal(err)
	}
	if db.sql != want.SQL || len(db.args) != 2 || db.args[0] != "bea" || db.args[1] != int64(1) {
		t.Fatalf("update %s %#v", db.sql, db.args)
	}

	db.rows = [][]any{{int64(1), "bea"}}
	sel := sqlite.Select(u.ID, u.Name).From(u).Where(op.Eq(u.ID, int64(1))).Limit(1)
	want, err = sel.SQL(sqlite.Dialect{})
	if err != nil {
		t.Fatal(err)
	}
	got, err := sel.Take(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if db.sql != want.SQL || len(db.args) != 2 || db.args[0] != int64(1) || db.args[1] != 1 {
		t.Fatalf("select %s %#v", db.sql, db.args)
	}
	if got.ID != 1 || got.Name != "bea" {
		t.Fatalf("row %#v", got)
	}
}

