package query_test

import (
	"strings"
	"sync"
	"testing"

	"github.com/go-mist/mist/internal/schemafixture"
	"github.com/go-mist/mist/op"
	"github.com/go-mist/mist/pg"
	"github.com/go-mist/mist/sql"
)

func TestInsertBatchAndConflict(t *testing.T) {
	got, err := pg.Insert(schemafixture.Users).
		Columns(schemafixture.Users.Name, schemafixture.Users.Email).
		Values("alice", "a@example.com").
		Values("", nil).
		OnConflictDoNothing().
		Returning(schemafixture.Users.ID, schemafixture.Users.CreatedAt).
		SQL(pg.Dialect{})
	if err != nil {
		t.Fatal(err)
	}
	const want = `insert into "users" ("name", "email") values ($1, $2), ($3, $4) on conflict do nothing returning "users"."id", "users"."created_at"`
	if got.SQL != want {
		t.Fatalf("sql = %s", got.SQL)
	}
	if len(got.Params) != 4 || got.Params[0] != "alice" || got.Params[1] != "a@example.com" || got.Params[2] != "" || got.Params[3] != nil {
		t.Fatalf("params = %#v", got.Params)
	}
}

func TestInsertPlaceholderStaysUnbound(t *testing.T) {
	ph := sql.NewPlaceholder("name")
	got, err := pg.Insert(schemafixture.Users).
		Columns(schemafixture.Users.Name).
		Values(ph).
		SQL(pg.Dialect{})
	if err != nil {
		t.Fatal(err)
	}
	const want = `insert into "users" ("name") values ($1)`
	if got.SQL != want {
		t.Fatalf("sql = %s", got.SQL)
	}
	if len(got.Params) != 1 || got.Params[0] != ph {
		t.Fatalf("params = %#v", got.Params)
	}
}

func TestInsertOnConflictDoUpdate(t *testing.T) {
	got, err := pg.Insert(schemafixture.Users).
		Columns(schemafixture.Users.ID, schemafixture.Users.Name).
		Values(int64(1), "anne").
		OnConflictDoUpdate(schemafixture.Users.ID).
		OnConflictWhere(op.IsNotNull(schemafixture.Users.Email)).
		Set(schemafixture.Users.Name, pg.Excluded(schemafixture.Users.Name)).
		Set(schemafixture.Users.Email, nil).
		UpdateWhere(op.Ne(schemafixture.Users.Name, "anne")).
		SQL(pg.Dialect{})
	if err != nil {
		t.Fatal(err)
	}
	const want = `insert into "users" ("id", "name") values ($1, $2) on conflict ("id") where ("users"."email" is not null) do update set "name" = excluded."name", "email" = $3 where "users"."name" <> $4`
	if got.SQL != want || len(got.Params) != 4 || got.Params[0] != int64(1) || got.Params[1] != "anne" || got.Params[2] != nil || got.Params[3] != "anne" {
		t.Fatalf("got %#v", got)
	}

	plain, err := pg.Insert(schemafixture.Users).
		Columns(schemafixture.Users.ID).
		Values(int64(1)).
		OnConflictDoNothing(schemafixture.Users.ID).
		SQL(pg.Dialect{})
	if err != nil {
		t.Fatal(err)
	}
	if plain.SQL != `insert into "users" ("id") values ($1) on conflict ("id") do nothing` {
		t.Fatalf("nothing = %s", plain.SQL)
	}

	if _, err := pg.Insert(schemafixture.Users).Columns(schemafixture.Users.ID).Values(int64(1)).Set(schemafixture.Users.Name, "x").SQL(pg.Dialect{}); err == nil {
		t.Fatal("set without target should fail")
	}
}

func TestInsertForkKeepsRowsApart(t *testing.T) {
	base := pg.Insert(schemafixture.Users).Columns(schemafixture.Users.Name)
	alice, err := base.Values("alice").SQL(pg.Dialect{})
	if err != nil {
		t.Fatal(err)
	}
	bob, err := base.Values("bob").SQL(pg.Dialect{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := base.SQL(pg.Dialect{}); err == nil {
		t.Fatal("base insert rendered without values")
	}
	if alice.Params[0] == bob.Params[0] {
		t.Fatalf("forks collided: %#v %#v", alice.Params, bob.Params)
	}
}

func TestInsertRejectsShortRow(t *testing.T) {
	_, err := pg.Insert(schemafixture.Users).
		Columns(schemafixture.Users.Name, schemafixture.Users.Email).
		Values("alice").
		SQL(pg.Dialect{})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestUpdateSetSubquery(t *testing.T) {
	sub := pg.Select(schemafixture.Users.Name).
		From(schemafixture.Users).
		Where(op.Eq(schemafixture.Users.ID, int64(1)))
	got, err := pg.Update(schemafixture.Users).
		Set(schemafixture.Users.Name, sub).
		Where(op.Eq(schemafixture.Users.ID, int64(2))).
		SQL(pg.Dialect{})
	if err != nil {
		t.Fatal(err)
	}
	const want = `update "users" set "name" = (select "users"."name" from "users" where "users"."id" = $1) where "users"."id" = $2`
	if got.SQL != want || len(got.Params) != 2 || got.Params[0] != int64(1) || got.Params[1] != int64(2) {
		t.Fatalf("got %#v", got)
	}
}

func TestInsertSelect(t *testing.T) {
	got, err := pg.Insert(schemafixture.Users).
		Columns(schemafixture.Users.ID, schemafixture.Users.Name).
		Values(int64(3), pg.Select(schemafixture.Users.Name).
			From(schemafixture.Users).
			Where(op.Eq(schemafixture.Users.ID, int64(1)))).
		SQL(pg.Dialect{})
	if err != nil {
		t.Fatal(err)
	}
	const wantValue = `insert into "users" ("id", "name") values ($1, (select "users"."name" from "users" where "users"."id" = $2))`
	if got.SQL != wantValue || len(got.Params) != 2 || got.Params[0] != int64(3) || got.Params[1] != int64(1) {
		t.Fatalf("value = %#v", got)
	}

	selected, err := pg.Insert(schemafixture.Users).
		Columns(schemafixture.Users.ID, schemafixture.Users.Name).
		Select(pg.Select(schemafixture.Users.ID, schemafixture.Users.Name).
			From(schemafixture.Users).
			Where(op.Eq(schemafixture.Users.Name, "alice"))).
		SQL(pg.Dialect{})
	if err != nil {
		t.Fatal(err)
	}
	const wantSelect = `insert into "users" ("id", "name") select "users"."id", "users"."name" from "users" where "users"."name" = $1`
	if selected.SQL != wantSelect || len(selected.Params) != 1 || selected.Params[0] != "alice" {
		t.Fatalf("select = %#v", selected)
	}

	if _, err := pg.Insert(schemafixture.Users).Columns(schemafixture.Users.ID).Values(int64(1)).Select(pg.Select(schemafixture.Users.ID).From(schemafixture.Users)).SQL(pg.Dialect{}); err == nil {
		t.Fatal("values and select together should fail")
	}
	if _, err := pg.Insert(schemafixture.Users).Columns(schemafixture.Users.ID).Select(pg.Select(schemafixture.Users.ID, schemafixture.Users.Name).From(schemafixture.Users)).SQL(pg.Dialect{}); err == nil {
		t.Fatal("column count mismatch should fail")
	}
	if _, err := pg.Insert(schemafixture.Users).Columns(schemafixture.Users.Name).Values(pg.Select(schemafixture.Users.Name).From(schemafixture.Users).As("sq")).SQL(pg.Dialect{}); err == nil {
		t.Fatal("derived table in values should fail")
	}
}

func TestWriteWith(t *testing.T) {
	gone := pg.CTE("gone", pg.Select(schemafixture.Users.ID).
		From(schemafixture.Users).
		Where(op.Eq(schemafixture.Users.Name, "alice")))
	got, err := pg.Update(schemafixture.Users).
		With(gone).
		Set(schemafixture.Users.Name, "ann").
		Where(op.In(schemafixture.Users.ID, pg.Select(gone.Col(schemafixture.Users.ID)).From(gone))).
		SQL(pg.Dialect{})
	if err != nil {
		t.Fatal(err)
	}
	const want = `with "gone" as (select "users"."id" from "users" where "users"."name" = $1) update "users" set "name" = $2 where "users"."id" in (select "gone"."id" from "gone")`
	if got.SQL != want || len(got.Params) != 2 || got.Params[0] != "alice" || got.Params[1] != "ann" {
		t.Fatalf("update = %#v", got)
	}

	deleted, err := pg.Delete(schemafixture.Users).
		With(pg.Recursive("gone", pg.Select(schemafixture.Users.ID).From(schemafixture.Users))).
		Where(op.Eq(schemafixture.Users.ID, int64(1))).
		SQL(pg.Dialect{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(deleted.SQL, `with recursive "gone" as (`) {
		t.Fatalf("delete = %s", deleted.SQL)
	}

	inserted, err := pg.Insert(schemafixture.Users).
		With(gone).
		Columns(schemafixture.Users.ID, schemafixture.Users.Name).
		Values(int64(3), "cara").
		SQL(pg.Dialect{})
	if err != nil {
		t.Fatal(err)
	}
	const wantInsert = `with "gone" as (select "users"."id" from "users" where "users"."name" = $1) insert into "users" ("id", "name") values ($2, $3)`
	if inserted.SQL != wantInsert || len(inserted.Params) != 3 || inserted.Params[1] != int64(3) || inserted.Params[2] != "cara" {
		t.Fatalf("insert = %#v", inserted)
	}

	if _, err := pg.Update(schemafixture.Users).With(pg.CTE("", pg.Select(schemafixture.Users.ID).From(schemafixture.Users))).Set(schemafixture.Users.Name, "ann").SQL(pg.Dialect{}); err == nil {
		t.Fatal("empty cte name should fail")
	}
}

func TestDeleteUsing(t *testing.T) {
	got, err := pg.Delete(schemafixture.Users).
		Using(schemafixture.Posts).
		Where(op.Eq(schemafixture.Users.ID, schemafixture.Posts.AuthorID)).
		SQL(pg.Dialect{})
	if err != nil {
		t.Fatal(err)
	}
	const want = `delete from "users" using "posts" where "users"."id" = "posts"."author_id"`
	if got.SQL != want {
		t.Fatalf("sql = %s", got.SQL)
	}
	if _, err := pg.Delete(schemafixture.Users).LeftJoin(schemafixture.Posts, op.Eq(schemafixture.Users.ID, schemafixture.Posts.AuthorID)).SQL(pg.Dialect{}); err == nil {
		t.Fatal("join without using should fail")
	}
}

func TestUpdateFrom(t *testing.T) {
	got, err := pg.Update(schemafixture.Users).
		Set(schemafixture.Users.Name, schemafixture.Posts.Title).
		From(schemafixture.Posts).
		LeftJoin(schemafixture.Users, op.Eq(schemafixture.Users.ID, schemafixture.Posts.AuthorID)).
		Where(op.Eq(schemafixture.Users.ID, schemafixture.Posts.AuthorID)).
		SQL(pg.Dialect{})
	if err != nil {
		t.Fatal(err)
	}
	const want = `update "users" set "name" = "posts"."title" from "posts" left join "users" on "users"."id" = "posts"."author_id" where "users"."id" = "posts"."author_id"`
	if got.SQL != want {
		t.Fatalf("sql = %s", got.SQL)
	}

	sq := pg.Select(schemafixture.Posts.AuthorID, schemafixture.Posts.Title).From(schemafixture.Posts).As("p")
	derived, err := pg.Update(schemafixture.Users).
		Set(schemafixture.Users.Name, sq.Col(schemafixture.Posts.Title)).
		From(sq).
		Where(op.Eq(schemafixture.Users.ID, sq.Col(schemafixture.Posts.AuthorID))).
		SQL(pg.Dialect{})
	if err != nil {
		t.Fatal(err)
	}
	const wantDerived = `update "users" set "name" = "p"."title" from (select "posts"."author_id", "posts"."title" from "posts") "p" where "users"."id" = "p"."author_id"`
	if derived.SQL != wantDerived {
		t.Fatalf("derived = %s", derived.SQL)
	}

	base := pg.Update(schemafixture.Users).Set(schemafixture.Users.Name, "ann").From(schemafixture.Posts)
	cleared, err := base.From(nil).SQL(pg.Dialect{})
	if err != nil {
		t.Fatal(err)
	}
	if cleared.SQL != `update "users" set "name" = $1` {
		t.Fatalf("cleared = %s", cleared.SQL)
	}

	if _, err := pg.Update(schemafixture.Users).Set(schemafixture.Users.Name, "ann").LeftJoin(schemafixture.Posts, op.Eq(schemafixture.Users.ID, schemafixture.Posts.AuthorID)).SQL(pg.Dialect{}); err == nil {
		t.Fatal("join without from should fail")
	}
	if _, err := pg.Update(schemafixture.Users).Set(schemafixture.Users.Name, "ann").From(schemafixture.Posts).InnerJoin(nil, nil).SQL(pg.Dialect{}); err == nil {
		t.Fatal("join without table should fail")
	}
}

func TestUpdateZeroAndExpression(t *testing.T) {
	got, err := pg.Update(schemafixture.Users).
		Set(schemafixture.Users.Name, "").
		Set(schemafixture.Users.ID, 0).
		Set(schemafixture.Users.Email, sql.Raw("lower(\"email\")")).
		Where(op.Eq(schemafixture.Users.ID, 7)).
		Returning(schemafixture.Users.Name).
		SQL(pg.Dialect{})
	if err != nil {
		t.Fatal(err)
	}
	const want = `update "users" set "name" = $1, "id" = $2, "email" = lower("email") where "users"."id" = $3 returning "users"."name"`
	if got.SQL != want {
		t.Fatalf("sql = %s", got.SQL)
	}
	if len(got.Params) != 3 || got.Params[0] != "" || got.Params[1] != 0 || got.Params[2] != 7 {
		t.Fatalf("params = %#v", got.Params)
	}
}

func TestUpdateNilBindsNull(t *testing.T) {
	var email *string
	got, err := pg.Update(schemafixture.Users).
		Set(schemafixture.Users.Email, email).
		SQL(pg.Dialect{})
	if err != nil {
		t.Fatal(err)
	}
	const want = `update "users" set "email" = $1`
	if got.SQL != want || len(got.Params) != 1 || got.Params[0] != nil {
		t.Fatalf("got %#v", got)
	}
}

func TestUpdateFork(t *testing.T) {
	base := pg.Update(schemafixture.Users).Where(op.Eq(schemafixture.Users.ID, 1))
	alice, err := base.Set(schemafixture.Users.Name, "alice").SQL(pg.Dialect{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := base.SQL(pg.Dialect{}); err == nil {
		t.Fatal("base update rendered without set")
	}
	if alice.SQL != `update "users" set "name" = $1 where "users"."id" = $2` {
		t.Fatalf("sql = %s", alice.SQL)
	}
}

func TestDeleteWhereAndReturning(t *testing.T) {
	got, err := pg.Delete(schemafixture.Users).
		Where(op.Eq(schemafixture.Users.ID, 7)).
		Returning(schemafixture.Users.ID).
		SQL(pg.Dialect{})
	if err != nil {
		t.Fatal(err)
	}
	const want = `delete from "users" where "users"."id" = $1 returning "users"."id"`
	if got.SQL != want || len(got.Params) != 1 || got.Params[0] != 7 {
		t.Fatalf("got %#v", got)
	}
}

func TestDeleteWithoutWhere(t *testing.T) {
	got, err := pg.Delete(schemafixture.Users).SQL(pg.Dialect{})
	if err != nil {
		t.Fatal(err)
	}
	if got.SQL != `delete from "users"` || got.Params != nil {
		t.Fatalf("got %#v", got)
	}
}

func TestWriteRenderRace(t *testing.T) {
	q := pg.Insert(schemafixture.Users).
		Columns(schemafixture.Users.Name).
		Values("alice")
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
