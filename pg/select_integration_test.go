//go:build integration

package pg_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/go-mist/mist/internal/schemafixture"
	"github.com/go-mist/mist/internal/testdb"
	"github.com/go-mist/mist/op"
	"github.com/go-mist/mist/pg"
	"github.com/go-mist/mist/sql"
)

func TestSelectAgainstPostgres(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	dsn := testdb.PostgresDSN(t)
	conn, err := waitConn(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)

	_, err = conn.Exec(ctx, `
		drop table if exists posts;
		drop table if exists users;
		create table users (
			id bigint primary key,
			name text not null,
			email text,
			created_at timestamptz not null
		);
		insert into users (id, name, email, created_at) values
			(1, 'bob', 'b@example.com', '2026-01-01T00:00:00Z'),
			(7, 'alice', 'a@example.com', '2026-02-01T00:00:00Z');
	`)
	if err != nil {
		t.Fatal(err)
	}

	built, err := pg.Select(schemafixture.Users.ID, schemafixture.Users.Name).
		From(schemafixture.Users).
		Where(op.And(
			op.Eq(schemafixture.Users.Name, "alice"),
			op.Gt(schemafixture.Users.ID, 0),
		)).
		OrderBy(op.Desc(schemafixture.Users.CreatedAt)).
		Limit(10).
		SQL(pg.Dialect{})
	if err != nil {
		t.Fatal(err)
	}

	rows, err := conn.Query(ctx, built.SQL, built.Params...)
	if err != nil {
		t.Fatalf("query %s params %#v: %v", built.SQL, built.Params, err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatal(rows.Err())
	}
	var id int64
	var name string
	if err := rows.Scan(&id, &name); err != nil {
		t.Fatal(err)
	}
	if id != 7 || name != "alice" {
		t.Fatalf("got id=%d name=%s", id, name)
	}
	if rows.Next() {
		t.Fatal("expected one row")
	}

	none, err := pg.Select(schemafixture.Users.ID).From(schemafixture.Users).Limit(0).SQL(pg.Dialect{})
	if err != nil {
		t.Fatal(err)
	}
	zero, err := conn.Query(ctx, none.SQL, none.Params...)
	if err != nil {
		t.Fatal(err)
	}
	defer zero.Close()
	if zero.Next() {
		t.Fatal("LIMIT 0 returned a row")
	}
}

func TestJoinAgainstPostgres(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	dsn := testdb.PostgresDSN(t)
	conn, err := waitConn(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)

	_, err = conn.Exec(ctx, `
		drop table if exists posts;
		drop table if exists users;
		create table users (
			id bigint primary key,
			name text not null,
			email text,
			created_at timestamptz not null
		);
		create table posts (
			id bigint primary key,
			author_id bigint not null,
			title text not null
		);
		insert into users (id, name, email, created_at) values
			(1, 'bob', 'b@example.com', '2026-01-01T00:00:00Z'),
			(7, 'alice', 'a@example.com', '2026-02-01T00:00:00Z');
		insert into posts (id, author_id, title) values
			(10, 7, 'hello');
	`)
	if err != nil {
		t.Fatal(err)
	}

	on := op.Eq(schemafixture.Posts.AuthorID, schemafixture.Users.ID)
	inner, err := pg.Select(schemafixture.Users.Name).
		From(schemafixture.Users).
		InnerJoin(schemafixture.Posts, on).
		SQL(pg.Dialect{})
	if err != nil {
		t.Fatal(err)
	}
	var name string
	err = conn.QueryRow(ctx, inner.SQL, inner.Params...).Scan(&name)
	if err != nil {
		t.Fatalf("query %s: %v", inner.SQL, err)
	}
	if name != "alice" {
		t.Fatalf("inner = %s", name)
	}

	left, err := pg.Select(schemafixture.Users.Name).
		From(schemafixture.Users).
		LeftJoin(schemafixture.Posts, on).
		OrderBy(op.Asc(schemafixture.Users.ID)).
		SQL(pg.Dialect{})
	if err != nil {
		t.Fatal(err)
	}
	rows, err := conn.Query(ctx, left.SQL, left.Params...)
	if err != nil {
		t.Fatalf("query %s: %v", left.SQL, err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var userName string
		if err := rows.Scan(&userName); err != nil {
			t.Fatal(err)
		}
		got = append(got, userName)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "bob" || got[1] != "alice" {
		t.Fatalf("left = %#v", got)
	}
}

func TestConditionsAgainstPostgres(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t, ctx)
	resetUsers(t, ctx, db)

	alice := "a@example.com"
	bob := "b@example.com"
	_, err := pg.Insert(schemafixture.Users).
		Columns(schemafixture.Users.ID, schemafixture.Users.Name, schemafixture.Users.Email).
		Values(int64(1), "bob", &bob).
		Values(int64(7), "alice", &alice).
		Values(int64(8), "cara", nil).
		Exec(ctx, db)
	if err != nil {
		t.Fatal(err)
	}

	names := func(cond sql.Chunk) string {
		t.Helper()
		rows, err := pg.Select(schemafixture.Users.Name).
			From(schemafixture.Users).
			Where(cond).
			OrderBy(op.Asc(schemafixture.Users.ID)).
			All(ctx, db)
		if err != nil {
			t.Fatal(err)
		}
		out := make([]byte, 0, len(rows)*8)
		for i, row := range rows {
			if i > 0 {
				out = append(out, ',')
			}
			out = append(out, row.Name...)
		}
		return string(out)
	}

	if got := names(op.InArray(schemafixture.Users.ID, []int64{1, 8})); got != "bob,cara" {
		t.Fatalf("in = %s", got)
	}
	if got := names(op.InArray(schemafixture.Users.ID, []int64{})); got != "" {
		t.Fatalf("empty in = %s", got)
	}
	if got := names(op.NotInArray(schemafixture.Users.ID, []int64{})); got != "bob,alice,cara" {
		t.Fatalf("empty not in = %s", got)
	}
	if got := names(op.NotInArray(schemafixture.Users.Name, []string{"bob", "cara"})); got != "alice" {
		t.Fatalf("not in = %s", got)
	}
	if got := names(op.Between(schemafixture.Users.ID, 2, 8)); got != "alice,cara" {
		t.Fatalf("between = %s", got)
	}
	if got := names(op.NotBetween(schemafixture.Users.ID, 2, 8)); got != "bob" {
		t.Fatalf("not between = %s", got)
	}
	if got := names(op.Like(schemafixture.Users.Name, "a%")); got != "alice" {
		t.Fatalf("like = %s", got)
	}
	if got := names(op.NotLike(schemafixture.Users.Name, "a%")); got != "bob,cara" {
		t.Fatalf("not like = %s", got)
	}
	if got := names(op.ILike(schemafixture.Users.Name, "ALICE")); got != "alice" {
		t.Fatalf("ilike = %s", got)
	}
	if got := names(op.NotILike(schemafixture.Users.Name, "%a%")); got != "bob" {
		t.Fatalf("not ilike = %s", got)
	}
	if got := names(op.IsNull(schemafixture.Users.Email)); got != "cara" {
		t.Fatalf("is null = %s", got)
	}
	if got := names(op.IsNotNull(schemafixture.Users.Email)); got != "bob,alice" {
		t.Fatalf("is not null = %s", got)
	}
	existsAlice := sql.Raw(`select 1 from users u where u.id = "users"."id" and u.name = 'alice'`)
	if got := names(op.Exists(existsAlice)); got != "alice" {
		t.Fatalf("exists = %s", got)
	}
	if got := names(op.NotExists(existsAlice)); got != "bob,cara" {
		t.Fatalf("not exists = %s", got)
	}
}

func TestGroupByAgainstPostgres(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t, ctx)
	resetUsers(t, ctx, db)

	_, err := pg.Insert(schemafixture.Users).
		Columns(schemafixture.Users.ID, schemafixture.Users.Name).
		Values(int64(1), "bob").
		Values(int64(2), "alice").
		Values(int64(3), "alice").
		Exec(ctx, db)
	if err != nil {
		t.Fatal(err)
	}

	rows, err := pg.Select(schemafixture.Users.Name).
		From(schemafixture.Users).
		GroupBy(schemafixture.Users.Name).
		Having(sql.Raw("count(*) > 1")).
		OrderBy(op.Asc(schemafixture.Users.Name)).
		All(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Name != "alice" {
		t.Fatalf("grouped = %#v", rows)
	}
}

func TestAggregateAgainstPostgres(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t, ctx)
	resetUsers(t, ctx, db)

	_, err := pg.Insert(schemafixture.Users).
		Columns(schemafixture.Users.ID, schemafixture.Users.Name).
		Values(int64(1), "bob").
		Values(int64(2), "alice").
		Values(int64(3), "alice").
		Exec(ctx, db)
	if err != nil {
		t.Fatal(err)
	}

	rows, err := pg.Select(schemafixture.Users.Name).
		From(schemafixture.Users).
		GroupBy(schemafixture.Users.Name).
		Having(op.Gt(op.Count(schemafixture.Users.ID), 1)).
		OrderBy(op.Desc(op.CountDistinct(schemafixture.Users.ID))).
		All(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Name != "alice" {
		t.Fatalf("aggregated = %#v", rows)
	}
}

func TestAggregateValueAgainstPostgres(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t, ctx)
	resetUsers(t, ctx, db)

	_, err := pg.Insert(schemafixture.Users).
		Columns(schemafixture.Users.ID, schemafixture.Users.Name).
		Values(int64(1), "bob").
		Values(int64(2), "alice").
		Values(int64(3), "alice").
		Exec(ctx, db)
	if err != nil {
		t.Fatal(err)
	}

	n, err := pg.Value[int64](op.Count()).From(schemafixture.Users).One(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Fatalf("count = %d", n)
	}

	type nameCount struct {
		Name string
		N    int64
	}
	rows, err := pg.Scan(func(row sql.Row, dst *nameCount) error {
		return row.Scan(&dst.Name, &dst.N)
	}, schemafixture.Users.Name, op.Count(schemafixture.Users.ID)).
		From(schemafixture.Users).
		GroupBy(schemafixture.Users.Name).
		OrderBy(op.Asc(schemafixture.Users.Name)).
		All(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].Name != "alice" || rows[0].N != 2 || rows[1].Name != "bob" || rows[1].N != 1 {
		t.Fatalf("grouped = %#v", rows)
	}
}

func TestUnionAgainstPostgres(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t, ctx)
	resetUsers(t, ctx, db)

	_, err := pg.Insert(schemafixture.Users).
		Columns(schemafixture.Users.ID, schemafixture.Users.Name).
		Values(int64(1), "bob").
		Values(int64(2), "alice").
		Values(int64(3), "alice").
		Exec(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	left := pg.Select(schemafixture.Users.Name).From(schemafixture.Users).Where(op.Lte(schemafixture.Users.ID, int64(2)))
	right := pg.Select(schemafixture.Users.Name).From(schemafixture.Users).Where(op.Gte(schemafixture.Users.ID, int64(2)))

	names := func(rows []schemafixture.User) string {
		out := make([]string, len(rows))
		for i, row := range rows {
			out[i] = row.Name
		}
		return strings.Join(out, ",")
	}

	unionRows, err := left.Union(right).OrderBy(op.Asc(schemafixture.Users.Name)).All(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if names(unionRows) != "alice,bob" {
		t.Fatalf("union = %s", names(unionRows))
	}
	allRows, err := left.UnionAll(right).OrderBy(op.Asc(schemafixture.Users.Name)).All(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if names(allRows) != "alice,alice,alice,bob" {
		t.Fatalf("union all = %s", names(allRows))
	}
	exceptRows, err := left.Except(right).All(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if names(exceptRows) != "bob" {
		t.Fatalf("except = %s", names(exceptRows))
	}
}

func TestSelectExtrasAgainstPostgres(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t, ctx)
	resetUsers(t, ctx, db)
	_, err := pg.Insert(schemafixture.Users).
		Columns(schemafixture.Users.ID, schemafixture.Users.Name).
		Values(int64(1), "alice").
		Values(int64(2), "alice").
		Values(int64(3), "bob").
		Exec(ctx, db)
	if err != nil {
		t.Fatal(err)
	}

	names, err := pg.Select(schemafixture.Users.Name).
		From(schemafixture.Users).
		Distinct().
		OrderBy(op.Asc(schemafixture.Users.Name)).
		All(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 2 || names[0].Name != "alice" || names[1].Name != "bob" {
		t.Fatalf("distinct = %#v", names)
	}

	locked, err := pg.Select(schemafixture.Users.Name).
		From(schemafixture.Users).
		Where(op.Eq(schemafixture.Users.ID, op.Add(schemafixture.Users.ID, int64(0)))).
		ForUpdate().
		OrderBy(op.Asc(schemafixture.Users.ID)).
		All(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if len(locked) != 3 {
		t.Fatalf("locked = %#v", locked)
	}

	step := schemafixture.Users.ID.WithTable("walk")
	base := pg.Select(schemafixture.Users.ID, schemafixture.Users.Name).
		From(schemafixture.Users).
		Where(op.Eq(schemafixture.Users.ID, int64(1)))
	next := pg.Select(schemafixture.Users.ID, schemafixture.Users.Name).
		From(schemafixture.Users).
		InnerJoin(sql.Identifier("walk"), op.Eq(schemafixture.Users.ID, op.Add(step, int64(1))))
	walk := pg.Recursive("walk", base.UnionAll(next))
	walked, err := pg.Select(walk.Col(schemafixture.Users.Name)).
		From(walk).
		With(walk).
		OrderBy(op.Asc(walk.Col(schemafixture.Users.ID))).
		All(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if len(walked) != 3 || walked[0].Name != "alice" || walked[1].Name != "alice" || walked[2].Name != "bob" {
		t.Fatalf("walk = %#v", walked)
	}
}

func TestSubqueryAgainstPostgres(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t, ctx)
	resetUsers(t, ctx, db)
	if _, err := db.Exec(ctx, `
		create table posts (
			id bigint primary key,
			author_id bigint not null,
			title text not null
		)`); err != nil {
		t.Fatal(err)
	}
	_, err := pg.Insert(schemafixture.Users).
		Columns(schemafixture.Users.ID, schemafixture.Users.Name).
		Values(int64(1), "bob").
		Values(int64(2), "alice").
		Exec(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	_, err = pg.Insert(schemafixture.Posts).
		Columns(schemafixture.Posts.ID, schemafixture.Posts.AuthorID, schemafixture.Posts.Title).
		Values(int64(10), int64(2), "hello").
		Exec(ctx, db)
	if err != nil {
		t.Fatal(err)
	}

	inner := pg.Select(schemafixture.Users.ID).
		From(schemafixture.Users).
		Where(op.Eq(schemafixture.Users.Name, "alice"))
	scalar, err := pg.Select(schemafixture.Users.Name).
		From(schemafixture.Users).
		Where(op.Eq(schemafixture.Users.ID, inner)).
		One(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if scalar.Name != "alice" {
		t.Fatalf("scalar = %#v", scalar)
	}

	authors, err := pg.Select(schemafixture.Users.Name).
		From(schemafixture.Users).
		Where(op.In(schemafixture.Users.ID, pg.Select(schemafixture.Posts.AuthorID).From(schemafixture.Posts))).
		All(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if len(authors) != 1 || authors[0].Name != "alice" {
		t.Fatalf("in = %#v", authors)
	}

	withPosts, err := pg.Select(schemafixture.Users.Name).
		From(schemafixture.Users).
		Where(op.Exists(pg.Select(schemafixture.Posts.ID).
			From(schemafixture.Posts).
			Where(op.Eq(schemafixture.Posts.AuthorID, schemafixture.Users.ID)))).
		All(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if len(withPosts) != 1 || withPosts[0].Name != "alice" {
		t.Fatalf("exists = %#v", withPosts)
	}

	sq := pg.Select(schemafixture.Users.ID, schemafixture.Users.Name).
		From(schemafixture.Users).
		As("sq")
	derived, err := pg.Select(sq.Col(schemafixture.Users.Name)).
		From(sq).
		OrderBy(op.Asc(sq.Col(schemafixture.Users.ID))).
		All(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if len(derived) != 2 || derived[0].Name != "bob" || derived[1].Name != "alice" {
		t.Fatalf("derived = %#v", derived)
	}
}

func TestWithAgainstPostgres(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t, ctx)
	resetUsers(t, ctx, db)

	_, err := pg.Insert(schemafixture.Users).
		Columns(schemafixture.Users.ID, schemafixture.Users.Name).
		Values(int64(1), "bob").
		Values(int64(2), "alice").
		Exec(ctx, db)
	if err != nil {
		t.Fatal(err)
	}

	active := pg.CTE("active", pg.Select(schemafixture.Users.ID, schemafixture.Users.Name).
		From(schemafixture.Users).
		Where(op.Eq(schemafixture.Users.Name, "alice")))
	rows, err := pg.Select(active.Col(schemafixture.Users.Name)).
		From(active).
		With(active).
		All(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Name != "alice" {
		t.Fatalf("cte = %#v", rows)
	}

	walk := pg.Recursive("walk", pg.Select(schemafixture.Users.ID, schemafixture.Users.Name).
		From(schemafixture.Users).
		Where(op.Lte(schemafixture.Users.ID, int64(2))))
	walked, err := pg.Select(walk.Col(schemafixture.Users.ID), walk.Col(schemafixture.Users.Name)).
		From(walk).
		With(walk).
		OrderBy(op.Asc(walk.Col(schemafixture.Users.ID))).
		All(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if len(walked) != 2 || walked[0].Name != "bob" || walked[1].Name != "alice" {
		t.Fatalf("recursive = %#v", walked)
	}
}

func waitConn(ctx context.Context, dsn string) (*pgx.Conn, error) {
	var last error
	for {
		conn, err := pgx.Connect(ctx, dsn)
		if err == nil {
			return conn, nil
		}
		last = err
		select {
		case <-ctx.Done():
			return nil, last
		case <-time.After(300 * time.Millisecond):
		}
	}
}
