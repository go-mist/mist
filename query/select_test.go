package query_test

import (
	"strings"
	"sync"
	"testing"

	"github.com/go-mist/mist/internal/schemafixture"
	"github.com/go-mist/mist/op"
	"github.com/go-mist/mist/pg"
	"github.com/go-mist/mist/query"
	"github.com/go-mist/mist/sql"
)

func TestSelectREADMEExample(t *testing.T) {
	got, err := pg.Select(schemafixture.Users.ID, schemafixture.Users.Name).
		From(schemafixture.Users).
		Where(op.And(
			op.Eq(schemafixture.Users.Name, "alice"),
			op.Gt(schemafixture.Users.ID, 100),
		)).
		OrderBy(op.Desc(schemafixture.Users.CreatedAt)).
		Limit(10).
		SQL(pg.Dialect{})
	if err != nil {
		t.Fatal(err)
	}
	const want = `select "users"."id", "users"."name" from "users" where (("users"."name" = $1) and ("users"."id" > $2)) order by "users"."created_at" desc limit $3`
	if got.SQL != want {
		t.Fatalf("sql = %s", got.SQL)
	}
	if len(got.Params) != 3 || got.Params[0] != "alice" || got.Params[1] != 100 || got.Params[2] != 10 {
		t.Fatalf("params = %#v", got.Params)
	}
}

func TestSelectWholeRow(t *testing.T) {
	got, err := pg.Select[schemafixture.User]().From(schemafixture.Users).SQL(pg.Dialect{})
	if err != nil {
		t.Fatal(err)
	}
	const want = `select "users"."id", "users"."name", "users"."email", "users"."created_at" from "users"`
	if got.SQL != want || got.Params != nil {
		t.Fatalf("got %#v", got)
	}
}

func TestSelectLimitZero(t *testing.T) {
	got, err := pg.Select(schemafixture.Users.ID).From(schemafixture.Users).Limit(0).SQL(pg.Dialect{})
	if err != nil {
		t.Fatal(err)
	}
	const want = `select "users"."id" from "users" limit $1`
	if got.SQL != want || len(got.Params) != 1 || got.Params[0] != 0 {
		t.Fatalf("got %#v", got)
	}
}

func TestSelectOffsetZero(t *testing.T) {
	got, err := pg.Select(schemafixture.Users.ID).From(schemafixture.Users).Offset(0).SQL(pg.Dialect{})
	if err != nil {
		t.Fatal(err)
	}
	const want = `select "users"."id" from "users" offset $1`
	if got.SQL != want || len(got.Params) != 1 || got.Params[0] != 0 {
		t.Fatalf("got %#v", got)
	}
}

func TestSelectForkSharesPrefix(t *testing.T) {
	base := pg.Select(schemafixture.Users.ID).From(schemafixture.Users)
	alice, err := base.Where(op.Eq(schemafixture.Users.Name, "alice")).SQL(pg.Dialect{})
	if err != nil {
		t.Fatal(err)
	}
	bob, err := base.Where(op.Eq(schemafixture.Users.Name, "bob")).SQL(pg.Dialect{})
	if err != nil {
		t.Fatal(err)
	}
	plain, err := base.SQL(pg.Dialect{})
	if err != nil {
		t.Fatal(err)
	}
	if alice.Params[0] == bob.Params[0] {
		t.Fatalf("forks collided: %#v %#v", alice, bob)
	}
	if alice.SQL != `select "users"."id" from "users" where "users"."name" = $1` {
		t.Fatalf("alice sql = %s", alice.SQL)
	}
	if plain.SQL != `select "users"."id" from "users"` {
		t.Fatalf("base changed: %s", plain.SQL)
	}
}

func TestSelectLaterClauseWins(t *testing.T) {
	got, err := pg.Select(schemafixture.Users.ID).
		From(schemafixture.Users).
		Where(op.Eq(schemafixture.Users.Name, "alice")).
		Where(nil).
		OrderBy(op.Asc(schemafixture.Users.ID)).
		OrderBy(op.Desc(schemafixture.Users.Name)).
		Limit(1).
		Limit(5).
		SQL(pg.Dialect{})
	if err != nil {
		t.Fatal(err)
	}
	const want = `select "users"."id" from "users" order by "users"."name" desc limit $1`
	if got.SQL != want || len(got.Params) != 1 || got.Params[0] != 5 {
		t.Fatalf("got %#v", got)
	}
}

func TestSelectGroupByHaving(t *testing.T) {
	got, err := pg.Select(schemafixture.Users.Name).
		From(schemafixture.Users).
		Where(op.Gt(schemafixture.Users.ID, 0)).
		GroupBy(schemafixture.Users.Name, schemafixture.Users.ID).
		GroupBy(schemafixture.Users.Name).
		Having(op.Eq(schemafixture.Users.Name, "alice")).
		Having(sql.Raw("count(*) > 1")).
		OrderBy(op.Asc(schemafixture.Users.Name)).
		SQL(pg.Dialect{})
	if err != nil {
		t.Fatal(err)
	}
	const want = `select "users"."name" from "users" where "users"."id" > $1 group by "users"."name" having count(*) > 1 order by "users"."name" asc`
	if got.SQL != want || len(got.Params) != 1 || got.Params[0] != 0 {
		t.Fatalf("got %#v", got)
	}

	base := pg.Select(schemafixture.Users.Name).From(schemafixture.Users)
	if _, err := base.GroupBy(schemafixture.Users.Name).SQL(pg.Dialect{}); err != nil {
		t.Fatal(err)
	}
	plain, err := base.SQL(pg.Dialect{})
	if err != nil {
		t.Fatal(err)
	}
	if plain.SQL != `select "users"."name" from "users"` {
		t.Fatalf("base changed: %s", plain.SQL)
	}
}

func TestSelectAggregateHaving(t *testing.T) {
	got, err := pg.Select(schemafixture.Users.Name).
		From(schemafixture.Users).
		GroupBy(schemafixture.Users.Name).
		Having(op.Gt(op.Count(schemafixture.Users.ID), 1)).
		OrderBy(op.Desc(op.Count(schemafixture.Users.ID))).
		SQL(pg.Dialect{})
	if err != nil {
		t.Fatal(err)
	}
	const want = `select "users"."name" from "users" group by "users"."name" having count("users"."id") > $1 order by count("users"."id") desc`
	if got.SQL != want || len(got.Params) != 1 || got.Params[0] != 1 {
		t.Fatalf("got %#v", got)
	}
}

func TestSelectSetOps(t *testing.T) {
	left := pg.Select(schemafixture.Users.ID, schemafixture.Users.Name).
		From(schemafixture.Users).
		Where(op.Eq(schemafixture.Users.Name, "alice"))
	right := pg.Select(schemafixture.Users.ID, schemafixture.Users.Name).
		From(schemafixture.Users).
		Where(op.Eq(schemafixture.Users.Name, "bob"))
	got, err := left.Union(right).OrderBy(op.Asc(schemafixture.Users.Name)).Limit(10).SQL(pg.Dialect{})
	if err != nil {
		t.Fatal(err)
	}
	const want = `(select "users"."id", "users"."name" from "users" where "users"."name" = $1) union (select "users"."id", "users"."name" from "users" where "users"."name" = $2) order by "name" asc limit $3`
	if got.SQL != want || len(got.Params) != 3 || got.Params[0] != "alice" || got.Params[1] != "bob" || got.Params[2] != 10 {
		t.Fatalf("got %#v", got)
	}

	plain, err := left.SQL(pg.Dialect{})
	if err != nil {
		t.Fatal(err)
	}
	if plain.SQL != `select "users"."id", "users"."name" from "users" where "users"."name" = $1` {
		t.Fatalf("base changed: %s", plain.SQL)
	}

	inside, err := pg.Select(schemafixture.Users.Name).
		From(schemafixture.Users).
		OrderBy(op.Asc(schemafixture.Users.ID)).
		UnionAll(pg.Select(schemafixture.Users.Name).From(schemafixture.Users)).
		SQL(pg.Dialect{})
	if err != nil {
		t.Fatal(err)
	}
	const insideWant = `(select "users"."name" from "users" order by "users"."id" asc) union all (select "users"."name" from "users")`
	if inside.SQL != insideWant {
		t.Fatalf("inside = %s", inside.SQL)
	}

	keywords := []struct {
		sql string
		run func() (string, error)
	}{
		{"intersect", func() (string, error) {
			q, err := left.Intersect(right).SQL(pg.Dialect{})
			return q.SQL, err
		}},
		{"intersect all", func() (string, error) {
			q, err := left.IntersectAll(right).SQL(pg.Dialect{})
			return q.SQL, err
		}},
		{"except", func() (string, error) {
			q, err := left.Except(right).SQL(pg.Dialect{})
			return q.SQL, err
		}},
		{"except all", func() (string, error) {
			q, err := left.ExceptAll(right).SQL(pg.Dialect{})
			return q.SQL, err
		}},
	}
	for _, kw := range keywords {
		sqlText, err := kw.run()
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(sqlText, ") "+kw.sql+" (") {
			t.Fatalf("%s sql = %s", kw.sql, sqlText)
		}
	}

	if _, err := pg.Select(schemafixture.Users.ID).From(schemafixture.Users).Union(pg.Select(schemafixture.Users.Name).From(schemafixture.Users)).SQL(pg.Dialect{}); err == nil {
		t.Fatal("different columns should fail")
	}
	if _, err := left.Union(right).Where(op.Eq(schemafixture.Users.ID, 1)).SQL(pg.Dialect{}); err == nil {
		t.Fatal("where after union should fail")
	}
}

func TestSelectSubquery(t *testing.T) {
	inner := pg.Select(schemafixture.Users.ID).
		From(schemafixture.Users).
		Where(op.Eq(schemafixture.Users.Name, "alice"))
	got, err := pg.Select(schemafixture.Users.Name).
		From(schemafixture.Users).
		Where(op.Eq(schemafixture.Users.ID, inner)).
		SQL(pg.Dialect{})
	if err != nil {
		t.Fatal(err)
	}
	const scalar = `select "users"."name" from "users" where "users"."id" = (select "users"."id" from "users" where "users"."name" = $1)`
	if got.SQL != scalar || len(got.Params) != 1 || got.Params[0] != "alice" {
		t.Fatalf("scalar = %#v", got)
	}

	inQuery, err := pg.Select(schemafixture.Users.Name).
		From(schemafixture.Users).
		Where(op.In(schemafixture.Users.ID, inner)).
		SQL(pg.Dialect{})
	if err != nil {
		t.Fatal(err)
	}
	const wantIn = `select "users"."name" from "users" where "users"."id" in (select "users"."id" from "users" where "users"."name" = $1)`
	if inQuery.SQL != wantIn {
		t.Fatalf("in = %s", inQuery.SQL)
	}

	exists, err := pg.Select(schemafixture.Users.Name).
		From(schemafixture.Users).
		Where(op.Exists(pg.Select(schemafixture.Posts.ID).
			From(schemafixture.Posts).
			Where(op.Eq(schemafixture.Posts.AuthorID, schemafixture.Users.ID)))).
		SQL(pg.Dialect{})
	if err != nil {
		t.Fatal(err)
	}
	const wantExists = `select "users"."name" from "users" where exists (select "posts"."id" from "posts" where "posts"."author_id" = "users"."id")`
	if exists.SQL != wantExists {
		t.Fatalf("exists = %s", exists.SQL)
	}

	sq := inner.As("sq")
	derived, err := pg.Select(sq.Col(schemafixture.Users.ID)).
		From(sq).
		Where(op.Eq(sq.Col(schemafixture.Users.ID), int64(7))).
		SQL(pg.Dialect{})
	if err != nil {
		t.Fatal(err)
	}
	const wantDerived = `select "sq"."id" from (select "users"."id" from "users" where "users"."name" = $1) "sq" where "sq"."id" = $2`
	if derived.SQL != wantDerived || len(derived.Params) != 2 || derived.Params[0] != "alice" || derived.Params[1] != int64(7) {
		t.Fatalf("derived = %#v", derived)
	}

	if _, err := pg.Select(schemafixture.Users.ID).From(schemafixture.Users).LeftJoin(inner, op.Eq(schemafixture.Users.ID, schemafixture.Users.ID)).SQL(pg.Dialect{}); err == nil {
		t.Fatal("join subquery without alias should fail")
	}
	if _, err := pg.Select(inner.As("").Col(schemafixture.Users.ID)).From(inner.As("")).SQL(pg.Dialect{}); err == nil {
		t.Fatal("empty alias should fail")
	}
}

func TestSelectDistinctAndLock(t *testing.T) {
	got, err := pg.Select(schemafixture.Users.Name).
		From(schemafixture.Users).
		Distinct().
		SQL(pg.Dialect{})
	if err != nil {
		t.Fatal(err)
	}
	if got.SQL != `select distinct "users"."name" from "users"` {
		t.Fatalf("distinct = %s", got.SQL)
	}

	on, err := pg.Select(schemafixture.Users.ID, schemafixture.Users.Name).
		From(schemafixture.Users).
		DistinctOn(schemafixture.Users.ID).
		OrderBy(op.Asc(schemafixture.Users.ID)).
		ForUpdate(query.RowLock{Of: []sql.Chunk{schemafixture.Users}, SkipLocked: true}).
		SQL(pg.Dialect{})
	if err != nil {
		t.Fatal(err)
	}
	const want = `select distinct on ("users"."id") "users"."id", "users"."name" from "users" order by "users"."id" asc for update of "users" skip locked`
	if on.SQL != want {
		t.Fatalf("on = %s", on.SQL)
	}

	if _, err := pg.Select(schemafixture.Users.ID).From(schemafixture.Users).DistinctOn().SQL(pg.Dialect{}); err == nil {
		t.Fatal("empty distinct on should fail")
	}
	if _, err := pg.Select(schemafixture.Users.ID).From(schemafixture.Users).ForUpdate(query.RowLock{NoWait: true, SkipLocked: true}).SQL(pg.Dialect{}); err == nil {
		t.Fatal("nowait and skip locked together should fail")
	}
}

func TestSelectWith(t *testing.T) {
	active := pg.CTE("active", pg.Select(schemafixture.Users.ID, schemafixture.Users.Name).
		From(schemafixture.Users).
		Where(op.Eq(schemafixture.Users.Name, "alice")))
	got, err := pg.Select(active.Col(schemafixture.Users.Name)).
		From(active).
		With(active).
		Where(op.Eq(active.Col(schemafixture.Users.ID), int64(7))).
		SQL(pg.Dialect{})
	if err != nil {
		t.Fatal(err)
	}
	const want = `with "active" as (select "users"."id", "users"."name" from "users" where "users"."name" = $1) select "active"."name" from "active" where "active"."id" = $2`
	if got.SQL != want || len(got.Params) != 2 || got.Params[0] != "alice" || got.Params[1] != int64(7) {
		t.Fatalf("got %#v", got)
	}

	recursive, err := pg.Select(active.Col(schemafixture.Users.Name)).
		From(active).
		With(pg.Recursive("active", pg.Select(schemafixture.Users.Name).From(schemafixture.Users))).
		SQL(pg.Dialect{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(recursive.SQL, `with recursive "active" as (`) {
		t.Fatalf("recursive = %s", recursive.SQL)
	}

	other := pg.CTE("other", pg.Select(schemafixture.Users.ID).From(schemafixture.Users))
	two, err := pg.Select(active.Col(schemafixture.Users.Name)).From(active).With(active, other).SQL(pg.Dialect{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(two.SQL, `with "active" as (`) || !strings.Contains(two.SQL, `, "other" as (`) {
		t.Fatalf("two = %s", two.SQL)
	}

	if _, err := pg.Select(schemafixture.Users.ID).From(schemafixture.Users).With(pg.CTE("", pg.Select(schemafixture.Users.ID).From(schemafixture.Users))).SQL(pg.Dialect{}); err == nil {
		t.Fatal("empty cte name should fail")
	}
}

func TestSelectJoinsKeepOrder(t *testing.T) {
	on := op.Eq(schemafixture.Posts.AuthorID, schemafixture.Users.ID)
	got, err := pg.Select(schemafixture.Users.Name).
		From(schemafixture.Users).
		InnerJoin(schemafixture.Posts, on).
		LeftJoin(schemafixture.Posts, on).
		Where(op.Eq(schemafixture.Users.Name, "alice")).
		SQL(pg.Dialect{})
	if err != nil {
		t.Fatal(err)
	}
	const want = `select "users"."name" from "users" inner join "posts" on "posts"."author_id" = "users"."id" left join "posts" on "posts"."author_id" = "users"."id" where "users"."name" = $1`
	if got.SQL != want || len(got.Params) != 1 || got.Params[0] != "alice" {
		t.Fatalf("got %#v", got)
	}
}

func TestSelectJoinKinds(t *testing.T) {
	on := op.Eq(schemafixture.Posts.AuthorID, schemafixture.Users.ID)
	base := pg.Select(schemafixture.Users.ID).From(schemafixture.Users)
	cases := []struct {
		name string
		sql  string
		run  func() (string, error)
	}{
		{"right", `select "users"."id" from "users" right join "posts" on "posts"."author_id" = "users"."id"`, func() (string, error) {
			got, err := base.RightJoin(schemafixture.Posts, on).SQL(pg.Dialect{})
			return got.SQL, err
		}},
		{"full", `select "users"."id" from "users" full join "posts" on "posts"."author_id" = "users"."id"`, func() (string, error) {
			got, err := base.FullJoin(schemafixture.Posts, on).SQL(pg.Dialect{})
			return got.SQL, err
		}},
		{"cross", `select "users"."id" from "users" cross join "posts"`, func() (string, error) {
			got, err := base.CrossJoin(schemafixture.Posts).SQL(pg.Dialect{})
			return got.SQL, err
		}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.run()
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.sql {
				t.Fatalf("sql = %s", got)
			}
		})
	}
}

func TestSelectJoinForkLeavesBase(t *testing.T) {
	base := pg.Select(schemafixture.Users.ID).From(schemafixture.Users)
	joined, err := base.InnerJoin(schemafixture.Posts, op.Eq(schemafixture.Posts.AuthorID, schemafixture.Users.ID)).SQL(pg.Dialect{})
	if err != nil {
		t.Fatal(err)
	}
	plain, err := base.SQL(pg.Dialect{})
	if err != nil {
		t.Fatal(err)
	}
	if joined.SQL != `select "users"."id" from "users" inner join "posts" on "posts"."author_id" = "users"."id"` {
		t.Fatalf("joined = %s", joined.SQL)
	}
	if plain.SQL != `select "users"."id" from "users"` {
		t.Fatalf("base changed: %s", plain.SQL)
	}
}

func TestSelectJoinRequiresOn(t *testing.T) {
	_, err := pg.Select(schemafixture.Users.ID).From(schemafixture.Users).LeftJoin(schemafixture.Posts, nil).SQL(pg.Dialect{})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestSelectMissingFrom(t *testing.T) {
	_, err := pg.Select(schemafixture.Users.ID).SQL(pg.Dialect{})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestSelectRenderRace(t *testing.T) {
	q := pg.Select(schemafixture.Users.ID, schemafixture.Users.Name).
		From(schemafixture.Users).
		Where(op.Eq(schemafixture.Users.Name, "alice")).
		Limit(10)
	var wg sync.WaitGroup
	wg.Add(2)
	for range 2 {
		go func() {
			defer wg.Done()
			got, err := q.SQL(pg.Dialect{})
			if err != nil {
				t.Error(err)
				return
			}
			if got.Params[0] != "alice" {
				t.Errorf("params = %#v", got.Params)
			}
		}()
	}
	wg.Wait()
}
