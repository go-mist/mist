package page_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/go-mist/mist/dialect/pg"
	"github.com/go-mist/mist/internal/schemafixture"
	"github.com/go-mist/mist/op"
	"github.com/go-mist/mist/page"
	"github.com/go-mist/mist/query"
)

func TestFetchPage(t *testing.T) {
	db := &fakeDB{total: 5, rows: [][]any{{int64(3)}, {int64(4)}}}
	got, err := page.Fetch(context.Background(), db,
		pg.Select(schemafixture.Users.ID).
			From(schemafixture.Users).
			Where(op.Gt(schemafixture.Users.ID, int64(0))).
			OrderBy(op.Asc(schemafixture.Users.ID)).
			Limit(100),
		page.Request{Current: 2, Size: 2})
	if err != nil {
		t.Fatal(err)
	}
	if got.Total != 5 || got.Current != 2 || got.Size != 2 || got.Pages != 3 || len(got.Records) != 2 || got.Records[0].ID != 3 || got.Records[1].ID != 4 {
		t.Fatalf("%#v", got)
	}
	if len(db.sql) != 2 {
		t.Fatalf("sql %#v", db.sql)
	}
	if db.sql[0] != `select count(*) from "users" where "users"."id" > $1` {
		t.Fatalf("count = %s", db.sql[0])
	}
	const want = `select "users"."id" from "users" where "users"."id" > $1 order by "users"."id" asc limit $2 offset $3`
	if db.sql[1] != want {
		t.Fatalf("page = %s", db.sql[1])
	}
	if len(db.args[1]) != 3 || db.args[1][0] != int64(0) || db.args[1][1] != 2 || db.args[1][2] != 2 {
		t.Fatalf("args %#v", db.args[1])
	}
}

func TestFetchSkipsEmptyWindow(t *testing.T) {
	db := &fakeDB{total: 3}
	got, err := page.Fetch(context.Background(), db,
		pg.Select(schemafixture.Users.ID).From(schemafixture.Users).OrderBy(op.Asc(schemafixture.Users.ID)),
		page.Request{Current: 3, Size: 2})
	if err != nil {
		t.Fatal(err)
	}
	if got.Total != 3 || got.Pages != 2 || len(got.Records) != 0 || len(db.sql) != 1 {
		t.Fatalf("%#v sql %#v", got, db.sql)
	}
}

func TestFetchRejects(t *testing.T) {
	db := &fakeDB{}
	q := pg.Select(schemafixture.Users.ID).From(schemafixture.Users)
	if _, err := page.Fetch(context.Background(), db, q, page.Request{Current: -1, Size: 10}); err == nil {
		t.Fatal("negative current was accepted")
	}
	if _, err := page.Fetch(context.Background(), db, q, page.Request{Current: 1, Size: -1}); err == nil {
		t.Fatal("negative size was accepted")
	}
	got, err := page.Fetch(context.Background(), db, q, page.Request{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Current != 1 || got.Size != 10 || got.Pages != 0 || len(got.Records) != 0 {
		t.Fatalf("%#v", got)
	}
}

func TestFetchNoCount(t *testing.T) {
	db := &fakeDB{rows: [][]any{{int64(1)}, {int64(2)}}}
	got, err := page.Fetch(context.Background(), db,
		pg.Select(schemafixture.Users.ID).From(schemafixture.Users).OrderBy(op.Asc(schemafixture.Users.ID)),
		page.Request{Current: 1, Size: 2, NoCount: true})
	if err != nil {
		t.Fatal(err)
	}
	if got.Total != 0 || got.Pages != 0 || got.Current != 1 || got.Size != 2 || len(got.Records) != 2 {
		t.Fatalf("%#v", got)
	}
	if len(db.sql) != 1 || strings.Contains(db.sql[0], "count(*)") {
		t.Fatalf("sql %#v", db.sql)
	}
	const want = `select "users"."id" from "users" order by "users"."id" asc limit $1 offset $2`
	if db.sql[0] != want {
		t.Fatalf("page = %s", db.sql[0])
	}
}

func TestFetchAfter(t *testing.T) {
	db := &fakeDB{rows: [][]any{{int64(4)}, {int64(5)}}}
	got, err := page.FetchAfter(context.Background(), db,
		pg.Select(schemafixture.Users.ID).From(schemafixture.Users).Where(op.Gt(schemafixture.Users.ID, int64(0))),
		schemafixture.Users.ID,
		page.Cursor{Value: int64(3), Size: 2})
	if err != nil {
		t.Fatal(err)
	}
	if got.Total != 0 || got.Current != 0 || got.Pages != 0 || got.Size != 2 || len(got.Records) != 2 || got.Records[0].ID != 4 {
		t.Fatalf("%#v", got)
	}
	const want = `select "users"."id" from "users" where (("users"."id" > $1) and ("users"."id" > $2)) order by "users"."id" asc limit $3`
	if len(db.sql) != 1 || db.sql[0] != want {
		t.Fatalf("sql %#v", db.sql)
	}
	if len(db.args[0]) != 3 || db.args[0][0] != int64(0) || db.args[0][1] != int64(3) || db.args[0][2] != 2 {
		t.Fatalf("args %#v", db.args[0])
	}

	db.sql = nil
	first, err := page.FetchAfter(context.Background(), db,
		pg.Select(schemafixture.Users.ID).From(schemafixture.Users),
		schemafixture.Users.ID,
		page.Cursor{Size: 2, Desc: true})
	if err != nil {
		t.Fatal(err)
	}
	if first.Size != 2 || len(db.sql) != 1 || strings.Contains(db.sql[0], "where") || !strings.Contains(db.sql[0], `order by "users"."id" desc limit $1`) {
		t.Fatalf("%#v sql %#v", first, db.sql)
	}
	if _, err := page.FetchAfter(context.Background(), db, pg.Select(schemafixture.Users.ID).From(schemafixture.Users), nil, page.Cursor{}); err == nil {
		t.Fatal("missing column was accepted")
	}
	if _, err := page.FetchAfter(context.Background(), db, pg.Select(schemafixture.Users.ID).From(schemafixture.Users).Offset(1), schemafixture.Users.ID, page.Cursor{}); err == nil {
		t.Fatal("offset was accepted")
	}
	if _, err := page.FetchAfter(context.Background(), db, pg.Select(schemafixture.Users.ID).From(schemafixture.Users), schemafixture.Users.ID, page.Cursor{Size: -1}); err == nil {
		t.Fatal("negative size was accepted")
	}
	db.sql, db.args = nil, nil
	if _, err := page.FetchAfter(context.Background(), db,
		pg.Select(schemafixture.Users.ID).From(schemafixture.Users),
		schemafixture.Users.ID,
		page.Cursor{Value: int64(9), Size: 1, Desc: true}); err != nil {
		t.Fatal(err)
	}
	const desc = `select "users"."id" from "users" where "users"."id" < $1 order by "users"."id" desc limit $2`
	if db.sql[0] != desc || db.args[0][0] != int64(9) || db.args[0][1] != 1 {
		t.Fatalf("sql %s args %#v", db.sql[0], db.args[0])
	}
}

func TestRequestAndResultJSON(t *testing.T) {
	var req page.Request
	if err := json.Unmarshal([]byte(`{"current":2,"size":20}`), &req); err != nil {
		t.Fatal(err)
	}
	if req.Current != 2 || req.Size != 20 || req.NoCount {
		t.Fatalf("%#v", req)
	}
	if err := json.Unmarshal([]byte(`{"current":1,"size":10,"noCount":true}`), &req); err != nil {
		t.Fatal(err)
	}
	if !req.NoCount {
		t.Fatal("noCount was not bound")
	}
	body, err := json.Marshal(page.Result[int]{
		Records: []int{1},
		Total:   21,
		Size:    20,
		Current: 2,
		Pages:   2,
	})
	if err != nil {
		t.Fatal(err)
	}
	const want = `{"records":[1],"total":21,"size":20,"current":2,"pages":2}`
	if string(body) != want {
		t.Fatalf("json = %s", body)
	}
}

func TestRowCountGrouped(t *testing.T) {
	got, err := pg.Select(schemafixture.Users.Name).
		From(schemafixture.Users).
		Where(op.Eq(schemafixture.Users.Name, "ada")).
		GroupBy(schemafixture.Users.Name).
		OrderBy(op.Asc(schemafixture.Users.Name)).
		Limit(10).
		RowCount().
		SQL(pg.Dialect{})
	if err != nil {
		t.Fatal(err)
	}
	const want = `select count(*) from (select "users"."name" from "users" where "users"."name" = $1 group by "users"."name") "mist_count"`
	if got.SQL != want {
		t.Fatalf("sql = %s", got.SQL)
	}
	if len(got.Params) != 1 || got.Params[0] != "ada" {
		t.Fatalf("params %#v", got.Params)
	}
}

func TestRowCountKeepsLimitInsideUnion(t *testing.T) {
	left := pg.Select(schemafixture.Users.ID).From(schemafixture.Users).Limit(2)
	got, err := left.Union(pg.Select(schemafixture.Users.ID).From(schemafixture.Users).Where(op.Eq(schemafixture.Users.ID, int64(9)))).
		OrderBy(op.Asc(schemafixture.Users.ID)).
		Limit(5).
		RowCount().
		SQL(pg.Dialect{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.SQL, "limit $1") || strings.Count(got.SQL, "limit") != 1 {
		t.Fatalf("sql = %s", got.SQL)
	}
	if strings.Contains(got.SQL, "order by") {
		t.Fatalf("sql = %s", got.SQL)
	}
}

type fakeDB struct {
	pg.Dialect
	total int64
	rows  [][]any
	sql   []string
	args  [][]any
}

func (f *fakeDB) Query(_ context.Context, query string, args ...any) (query.Rows, error) {
	f.sql = append(f.sql, query)
	f.args = append(f.args, append([]any(nil), args...))
	if strings.Contains(query, "count(*)") {
		return &fakeRows{data: [][]any{{f.total}}}, nil
	}
	return &fakeRows{data: f.rows}, nil
}

func (f *fakeDB) Exec(context.Context, string, ...any) (int64, error) {
	return 0, errors.New("page test: exec")
}

func (f *fakeDB) Prepare(context.Context, string) (query.Stmt, error) {
	return nil, errors.New("page test: prepare")
}

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
		reflect.ValueOf(dest[i]).Elem().Set(reflect.ValueOf(row[i]))
	}
	return nil
}

func (f *fakeRows) Err() error { return nil }

func (f *fakeRows) Close() {}
