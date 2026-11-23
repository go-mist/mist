//go:build integration

package pg_test

import (
	"context"
	"testing"
	"time"

	"github.com/go-mist/mist/internal/schemafixture"
	"github.com/go-mist/mist/internal/testdb"
	"github.com/go-mist/mist/op"
	"github.com/go-mist/mist/pg"
	"github.com/go-mist/mist/sql"
)

func TestInsertSelectAgainstPostgres(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t, ctx)
	resetUsers(t, ctx, db)
	_, err := pg.Insert(schemafixture.Users).
		Columns(schemafixture.Users.ID, schemafixture.Users.Name).
		Values(int64(1), "alice").
		Exec(ctx, db)
	if err != nil {
		t.Fatal(err)
	}

	n, err := pg.Insert(schemafixture.Users).
		Columns(schemafixture.Users.ID, schemafixture.Users.Name).
		Values(int64(2), pg.Select(schemafixture.Users.Name).
			From(schemafixture.Users).
			Where(op.Eq(schemafixture.Users.ID, int64(1)))).
		Exec(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("inserted %d", n)
	}

	n, err = pg.Insert(schemafixture.Users).
		Columns(schemafixture.Users.ID, schemafixture.Users.Name).
		Select(pg.Select(schemafixture.Users.ID, schemafixture.Users.Name).
			From(schemafixture.Users).
			Where(op.Eq(schemafixture.Users.ID, int64(1)))).
		OnConflictDoUpdate(schemafixture.Users.ID).
		Set(schemafixture.Users.Name, "ann").
		Exec(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("upserted %d", n)
	}

	rows, err := pg.Select(schemafixture.Users.ID, schemafixture.Users.Name).
		From(schemafixture.Users).
		OrderBy(op.Asc(schemafixture.Users.ID)).
		All(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].Name != "ann" || rows[1].Name != "alice" {
		t.Fatalf("rows = %#v", rows)
	}
}

func TestWriteWithAgainstPostgres(t *testing.T) {
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

	gone := pg.CTE("gone", pg.Select(schemafixture.Users.ID).
		From(schemafixture.Users).
		Where(op.Eq(schemafixture.Users.Name, "alice")))
	n, err := pg.Update(schemafixture.Users).
		With(gone).
		Set(schemafixture.Users.Name, "ann").
		Where(op.In(schemafixture.Users.ID, pg.Select(gone.Col(schemafixture.Users.ID)).From(gone))).
		Exec(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("updated %d", n)
	}

	bob := pg.CTE("gone", pg.Select(schemafixture.Users.ID).
		From(schemafixture.Users).
		Where(op.Eq(schemafixture.Users.Name, "bob")))
	n, err = pg.Delete(schemafixture.Users).
		With(bob).
		Where(op.In(schemafixture.Users.ID, pg.Select(bob.Col(schemafixture.Users.ID)).From(bob))).
		Exec(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("deleted %d", n)
	}

	rows, err := pg.Select(schemafixture.Users.Name).From(schemafixture.Users).All(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Name != "ann" {
		t.Fatalf("rows = %#v", rows)
	}
}

func TestDeleteUsingAgainstPostgres(t *testing.T) {
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
	n, err := pg.Delete(schemafixture.Users).
		Using(schemafixture.Posts).
		Where(op.Eq(schemafixture.Users.ID, schemafixture.Posts.AuthorID)).
		Exec(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("deleted %d", n)
	}
	rows, err := pg.Select(schemafixture.Users.Name).From(schemafixture.Users).All(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Name != "bob" {
		t.Fatalf("rows = %#v", rows)
	}
}

func TestUpdateFromAgainstPostgres(t *testing.T) {
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

	n, err := pg.Update(schemafixture.Users).
		Set(schemafixture.Users.Name, schemafixture.Posts.Title).
		From(schemafixture.Posts).
		Where(op.Eq(schemafixture.Users.ID, schemafixture.Posts.AuthorID)).
		Exec(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("updated %d", n)
	}
	rows, err := pg.Select(schemafixture.Users.ID, schemafixture.Users.Name).
		From(schemafixture.Users).
		OrderBy(op.Asc(schemafixture.Users.ID)).
		All(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].Name != "bob" || rows[1].Name != "hello" {
		t.Fatalf("rows = %#v", rows)
	}
}

func TestUpsertAgainstPostgres(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t, ctx)
	resetUsers(t, ctx, db)

	_, err := pg.Insert(schemafixture.Users).
		Columns(schemafixture.Users.ID, schemafixture.Users.Name).
		Values(int64(1), "ann").
		Exec(ctx, db)
	if err != nil {
		t.Fatal(err)
	}

	row, err := pg.Insert(schemafixture.Users).
		Columns(schemafixture.Users.ID, schemafixture.Users.Name).
		Values(int64(1), "anne").
		OnConflictDoUpdate(schemafixture.Users.ID).
		Set(schemafixture.Users.Name, pg.Excluded(schemafixture.Users.Name)).
		Returning(schemafixture.Users.Name).
		One(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if row.Name != "anne" {
		t.Fatalf("upsert name = %s", row.Name)
	}

	_, err = pg.Insert(schemafixture.Users).
		Columns(schemafixture.Users.ID, schemafixture.Users.Name).
		Values(int64(1), "nope").
		OnConflictDoUpdate(schemafixture.Users.ID).
		Set(schemafixture.Users.Name, "nope").
		UpdateWhere(sql.Raw("false")).
		Exec(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	kept, err := pg.Select(schemafixture.Users.Name).
		From(schemafixture.Users).
		Where(op.Eq(schemafixture.Users.ID, int64(1))).
		One(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if kept.Name != "anne" {
		t.Fatalf("where false updated name to %s", kept.Name)
	}
}

func TestWriteAgainstPostgres(t *testing.T) {
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
			created_at timestamptz not null default now()
		);
	`)
	if err != nil {
		t.Fatal(err)
	}

	batch, err := pg.Insert(schemafixture.Users).
		Columns(schemafixture.Users.ID, schemafixture.Users.Name).
		Values(int64(1), "ann").
		Values(int64(2), "").
		SQL(pg.Dialect{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, batch.SQL, batch.Params...); err != nil {
		t.Fatalf("insert %s params %#v: %v", batch.SQL, batch.Params, err)
	}

	var name string
	var email *string
	var created time.Time
	err = conn.QueryRow(ctx, `select "name", "email", "created_at" from "users" where "id" = $1`, 1).
		Scan(&name, &email, &created)
	if err != nil {
		t.Fatal(err)
	}
	if name != "ann" || email != nil || created.IsZero() {
		t.Fatalf("inserted = %q email %#v created %v", name, email, created)
	}
	err = conn.QueryRow(ctx, `select "name" from "users" where "id" = $1`, 2).Scan(&name)
	if err != nil {
		t.Fatal(err)
	}
	if name != "" {
		t.Fatalf("zero name = %q", name)
	}

	again, err := pg.Insert(schemafixture.Users).
		Columns(schemafixture.Users.ID, schemafixture.Users.Name).
		Values(int64(1), "other").
		OnConflictDoNothing().
		SQL(pg.Dialect{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, again.SQL, again.Params...); err != nil {
		t.Fatalf("conflict %s: %v", again.SQL, err)
	}
	err = conn.QueryRow(ctx, `select "name" from "users" where "id" = $1`, 1).Scan(&name)
	if err != nil {
		t.Fatal(err)
	}
	if name != "ann" {
		t.Fatalf("conflict overwrote name: %s", name)
	}

	upd, err := pg.Update(schemafixture.Users).
		Set(schemafixture.Users.Name, "anne").
		Set(schemafixture.Users.Email, "a@example.com").
		Where(op.Eq(schemafixture.Users.ID, 1)).
		SQL(pg.Dialect{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, upd.SQL, upd.Params...); err != nil {
		t.Fatalf("update %s: %v", upd.SQL, err)
	}

	expr, err := pg.Update(schemafixture.Users).
		Set(schemafixture.Users.Name, sql.Raw(`"name" || '!'`)).
		Where(op.Eq(schemafixture.Users.ID, 2)).
		SQL(pg.Dialect{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, expr.SQL, expr.Params...); err != nil {
		t.Fatalf("expr update %s: %v", expr.SQL, err)
	}

	del, err := pg.Delete(schemafixture.Users).
		Where(op.Eq(schemafixture.Users.ID, 2)).
		Returning(schemafixture.Users.Name).
		SQL(pg.Dialect{})
	if err != nil {
		t.Fatal(err)
	}
	var deleted string
	err = conn.QueryRow(ctx, del.SQL, del.Params...).Scan(&deleted)
	if err != nil {
		t.Fatalf("delete %s: %v", del.SQL, err)
	}
	if deleted != "!" {
		t.Fatalf("deleted name = %q", deleted)
	}

	err = conn.QueryRow(ctx, `select "name", "email" from "users" where "id" = $1`, 1).Scan(&name, &email)
	if err != nil {
		t.Fatal(err)
	}
	if name != "anne" || email == nil || *email != "a@example.com" {
		t.Fatalf("updated = %q %#v", name, email)
	}
}
