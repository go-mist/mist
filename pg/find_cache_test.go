package pg

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/go-mist/mist/query"
	"github.com/go-mist/mist/relation"
)

type cacheUser struct {
	ID   int64
	Name string
}

type refuseQueryer struct {
	calls int
}

func (q *refuseQueryer) Query(context.Context, string, ...any) (query.Rows, error) {
	q.calls++
	return nil, errRefused
}

var errRefused = errString("unexpected query")

type errString string

func (e errString) Error() string { return string(e) }

func TestFindManyCache(t *testing.T) {
	ctx := context.Background()
	schema := cacheSchema(t)
	db := &refuseQueryer{}
	_, err := FindMany[cacheUser](ctx, db, schema, "users", relation.Config{WithCache: relation.Bool(true)})
	if err == nil || !strings.Contains(err.Error(), "needs Cache") {
		t.Fatalf("missing cache: %v", err)
	}
	if db.calls != 0 {
		t.Fatalf("queries = %d", db.calls)
	}

	cfg := relation.Config{Where: relation.Eq("id", int64(1)), WithCache: relation.Bool(true), Cache: &relation.MemoryCache{}}
	built, err := BuildRelationalQuery(schema, "users", cfg, relation.ModeMany)
	if err != nil {
		t.Fatal(err)
	}
	rendered, err := Dialect{}.SQLToQuery(built.Query)
	if err != nil {
		t.Fatal(err)
	}
	key, err := relationalCacheKey(rendered)
	if err != nil {
		t.Fatal(err)
	}
	blob, err := json.Marshal([]cacheUser{{ID: 1, Name: "alice"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.Cache.Set(ctx, key, blob); err != nil {
		t.Fatal(err)
	}
	got, err := FindMany[cacheUser](ctx, db, schema, "users", cfg)
	if err != nil {
		t.Fatal(err)
	}
	if db.calls != 0 || len(got) != 1 || got[0].Name != "alice" {
		t.Fatalf("got %#v calls %d", got, db.calls)
	}
}

func cacheSchema(t *testing.T) *relation.Schema {
	t.Helper()
	s, err := relation.Define([]relation.Table{{
		Name: "users",
		Columns: []relation.Column{
			{Name: "id", Ptr: func(row any) any { return &row.(*cacheUser).ID }},
			{Name: "name", Ptr: func(row any) any { return &row.(*cacheUser).Name }},
		},
		New: func() any { return &cacheUser{} },
	}})
	if err != nil {
		t.Fatal(err)
	}
	return s
}
