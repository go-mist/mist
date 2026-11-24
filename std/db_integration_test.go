//go:build integration

package std_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/go-mist/mist"
	"github.com/go-mist/mist/internal/schemafixture"
	"github.com/go-mist/mist/internal/testdb"
	"github.com/go-mist/mist/op"
	"github.com/go-mist/mist/pg"
	"github.com/go-mist/mist/query"
	"github.com/go-mist/mist/sql"
	"github.com/go-mist/mist/std"
)

func TestStdCRUD(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t, ctx)
	resetUsers(t, ctx, db)

	n, err := pg.Insert(schemafixture.Users).
		Columns(schemafixture.Users.ID, schemafixture.Users.Name).
		Values(int64(1), "ann").
		Values(int64(2), "bob").
		Exec(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("inserted %d", n)
	}

	rows, err := pg.Select(schemafixture.Users.ID, schemafixture.Users.Name).
		From(schemafixture.Users).
		OrderBy(op.Asc(schemafixture.Users.ID)).
		All(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].Name != "ann" || rows[1].Name != "bob" {
		t.Fatalf("rows = %#v", rows)
	}

	err = db.Tx(ctx, func(tx mist.Tx) error {
		if _, err := pg.Insert(schemafixture.Users).
			Columns(schemafixture.Users.ID, schemafixture.Users.Name).
			Values(int64(3), "cam").
			Exec(ctx, tx); err != nil {
			return err
		}
		err := tx.Tx(ctx, func(inner mist.Tx) error {
			_, err := pg.Insert(schemafixture.Users).
				Columns(schemafixture.Users.ID, schemafixture.Users.Name).
				Values(int64(4), "dee").
				Exec(ctx, inner)
			if err != nil {
				return err
			}
			return errors.New("rollback inner")
		})
		if err == nil || err.Error() != "rollback inner" {
			t.Fatalf("inner err = %v", err)
		}
		_, err = pg.Select(schemafixture.Users.ID).From(schemafixture.Users).Where(op.Eq(schemafixture.Users.ID, 4)).One(ctx, tx)
		if !errors.Is(err, query.ErrNoRows) {
			t.Fatalf("savepoint row visible: %v", err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	kept, err := pg.Select(schemafixture.Users.Name).From(schemafixture.Users).Where(op.Eq(schemafixture.Users.ID, 3)).One(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if kept.Name != "cam" {
		t.Fatalf("kept = %#v", kept)
	}
}

func TestStdCancel(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t, ctx)

	short, stop := context.WithTimeout(ctx, 200*time.Millisecond)
	defer stop()
	rows, err := db.Query(short, "select pg_sleep(5)")
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
	}
	if rows.Err() == nil {
		t.Fatal("pg_sleep was not cancelled")
	}
}

func TestStdPreparedReuse(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t, ctx)
	resetUsers(t, ctx, db)

	_, err := pg.Insert(schemafixture.Users).
		Columns(schemafixture.Users.ID, schemafixture.Users.Name).
		Values(int64(1), "alice").
		Values(int64(2), "bob").
		Exec(ctx, db)
	if err != nil {
		t.Fatal(err)
	}

	stmt, err := pg.Select(schemafixture.Users.Name).
		From(schemafixture.Users).
		Where(op.Eq(schemafixture.Users.Name, sql.NewPlaceholder("name"))).
		Prepare(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = stmt.Close() })
	alice, err := stmt.One(ctx, map[string]any{"name": "alice"})
	if err != nil {
		t.Fatal(err)
	}
	bob, err := stmt.One(ctx, map[string]any{"name": "bob"})
	if err != nil {
		t.Fatal(err)
	}
	if alice.Name != "alice" || bob.Name != "bob" {
		t.Fatalf("alice %#v bob %#v", alice, bob)
	}

	err = db.Tx(ctx, func(tx mist.Tx) error {
		st, err := pg.Select(schemafixture.Users.Name).
			From(schemafixture.Users).
			Where(op.And(
				op.Eq(schemafixture.Users.Name, sql.NewPlaceholder("name")),
				sql.Raw("/*drizzle_prepare_probe*/ true"),
			)).
			Prepare(ctx, tx)
		if err != nil {
			return err
		}
		defer st.Close()
		one, err := st.One(ctx, map[string]any{"name": "bob"})
		if err != nil {
			return err
		}
		if one.Name != "bob" {
			return fmt.Errorf("name = %s", one.Name)
		}
		rows, err := tx.Query(ctx, `select count(*) from pg_prepared_statements where statement like $1`, "%drizzle_prepare_probe%")
		if err != nil {
			return err
		}
		defer rows.Close()
		if !rows.Next() {
			return errors.New("no prepared-statement count")
		}
		var count int64
		if err := rows.Scan(&count); err != nil {
			return err
		}
		if count < 1 {
			return fmt.Errorf("pg_prepared_statements = %d", count)
		}
		return rows.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
}

func openTestDB(t *testing.T, ctx context.Context) *std.DB {
	t.Helper()
	db, err := std.Open(ctx, "pgx", testdb.PostgresDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	db.SQLDB().SetMaxOpenConns(4)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func resetUsers(t *testing.T, ctx context.Context, db *std.DB) {
	t.Helper()
	if _, err := db.Exec(ctx, "drop table if exists posts"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, "drop table if exists users"); err != nil {
		t.Fatal(err)
	}
	_, err := db.Exec(ctx, `
		create table users (
			id bigint primary key,
			name text not null,
			email text,
			created_at timestamptz not null default now()
		)`)
	if err != nil {
		t.Fatal(err)
	}
}
