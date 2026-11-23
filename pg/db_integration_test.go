//go:build integration

package pg_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/xbmlz/mist/internal/schemafixture"
	"github.com/xbmlz/mist/internal/testdb"
	"github.com/xbmlz/mist/op"
	"github.com/xbmlz/mist/pg"
	"github.com/xbmlz/mist/query"
	"github.com/xbmlz/mist/sql"
)

func TestDBCRUD(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t, ctx)
	resetUsers(t, ctx, db)

	_, err := pg.Insert(schemafixture.Users).
		Columns(schemafixture.Users.ID, schemafixture.Users.Name).
		Values(int64(1), "ann").
		Values(int64(2), "bob").
		Exec(ctx, db)
	if err != nil {
		t.Fatal(err)
	}

	rows, err := pg.Select(schemafixture.Users.ID, schemafixture.Users.Name).
		From(schemafixture.Users).
		OrderBy(op.Asc(schemafixture.Users.ID)).
		All(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].Name != "ann" || rows[1].Name != "bob" || rows[0].Email != nil {
		t.Fatalf("rows = %#v", rows)
	}

	whole, err := pg.Select[schemafixture.User]().
		From(schemafixture.Users).
		Where(op.Eq(schemafixture.Users.ID, 1)).
		One(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if whole.Name != "ann" || whole.Email != nil || whole.CreatedAt.IsZero() {
		t.Fatalf("whole = %#v", whole)
	}

	updated, err := pg.Update(schemafixture.Users).
		Set(schemafixture.Users.Name, "anne").
		Where(op.Eq(schemafixture.Users.ID, 1)).
		Returning(schemafixture.Users.ID, schemafixture.Users.Name).
		One(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if updated.ID != 1 || updated.Name != "anne" {
		t.Fatalf("updated = %#v", updated)
	}

	deleted, err := pg.Delete(schemafixture.Users).
		Where(op.Eq(schemafixture.Users.ID, 2)).
		Returning(schemafixture.Users.Name).
		One(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if deleted.Name != "bob" {
		t.Fatalf("deleted = %#v", deleted)
	}
	_, err = pg.Select(schemafixture.Users.ID).From(schemafixture.Users).Where(op.Eq(schemafixture.Users.ID, 2)).One(ctx, db)
	if !errors.Is(err, query.ErrNoRows) {
		t.Fatalf("err = %v", err)
	}
}

func TestDBTransaction(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t, ctx)
	resetUsers(t, ctx, db)

	err := db.Tx(ctx, func(tx *pg.Tx) error {
		if _, err := pg.Insert(schemafixture.Users).
			Columns(schemafixture.Users.ID, schemafixture.Users.Name).
			Values(int64(1), "ann").
			Exec(ctx, tx); err != nil {
			return err
		}
		err := tx.Tx(ctx, func(inner *pg.Tx) error {
			_, err := pg.Insert(schemafixture.Users).
				Columns(schemafixture.Users.ID, schemafixture.Users.Name).
				Values(int64(2), "bob").
				Exec(ctx, inner)
			if err != nil {
				return err
			}
			return errors.New("rollback inner")
		})
		if err == nil || err.Error() != "rollback inner" {
			t.Fatalf("inner err = %v", err)
		}
		_, err = pg.Select(schemafixture.Users.ID).From(schemafixture.Users).Where(op.Eq(schemafixture.Users.ID, 2)).One(ctx, tx)
		if !errors.Is(err, query.ErrNoRows) {
			t.Fatalf("inner row visible: %v", err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	kept, err := pg.Select(schemafixture.Users.Name).From(schemafixture.Users).Where(op.Eq(schemafixture.Users.ID, 1)).One(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if kept.Name != "ann" {
		t.Fatalf("kept = %#v", kept)
	}

	err = db.Tx(ctx, func(tx *pg.Tx) error {
		_, err := pg.Insert(schemafixture.Users).
			Columns(schemafixture.Users.ID, schemafixture.Users.Name).
			Values(int64(3), "nope").
			Exec(ctx, tx)
		if err != nil {
			return err
		}
		return errors.New("rollback outer")
	})
	if err == nil {
		t.Fatal("expected rollback")
	}
	_, err = pg.Select(schemafixture.Users.ID).From(schemafixture.Users).Where(op.Eq(schemafixture.Users.ID, 3)).One(ctx, db)
	if !errors.Is(err, query.ErrNoRows) {
		t.Fatalf("rolled back row visible: %v", err)
	}
}

func TestDBPanicRollsBack(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t, ctx)
	resetUsers(t, ctx, db)

	defer func() {
		if recover() == nil {
			t.Fatal("expected panic")
		}
		_, err := pg.Select(schemafixture.Users.ID).From(schemafixture.Users).Where(op.Eq(schemafixture.Users.ID, 9)).One(ctx, db)
		if !errors.Is(err, query.ErrNoRows) {
			t.Fatalf("panic did not roll back: %v", err)
		}
	}()
	_ = db.Tx(ctx, func(tx *pg.Tx) error {
		_, err := pg.Insert(schemafixture.Users).
			Columns(schemafixture.Users.ID, schemafixture.Users.Name).
			Values(int64(9), "boom").
			Exec(ctx, tx)
		if err != nil {
			return err
		}
		panic("boom")
	})
}

func TestDBCancel(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t, ctx)
	resetUsers(t, ctx, db)

	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	_, err := pg.Select[schemafixture.User]().From(schemafixture.Users).All(cancelled, db)
	if err == nil {
		t.Fatal("cancelled context executed")
	}

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

func TestDBConcurrentRead(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t, ctx)
	resetUsers(t, ctx, db)
	if _, err := pg.Insert(schemafixture.Users).
		Columns(schemafixture.Users.ID, schemafixture.Users.Name).
		Values(int64(1), "ann").
		Exec(ctx, db); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	wg.Add(100)
	for range 100 {
		go func() {
			defer wg.Done()
			got, err := pg.Select(schemafixture.Users.Name).From(schemafixture.Users).One(ctx, db)
			if err != nil {
				t.Error(err)
				return
			}
			if got.Name != "ann" {
				t.Errorf("name = %s", got.Name)
			}
		}()
	}
	wg.Wait()
}

func TestPreparedReuse(t *testing.T) {
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

	stmt, err := pg.Select(schemafixture.Users.ID, schemafixture.Users.Name).
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
	if alice.ID != 1 || alice.Name != "alice" || bob.ID != 2 || bob.Name != "bob" {
		t.Fatalf("alice %#v bob %#v", alice, bob)
	}

	ins, err := pg.Insert(schemafixture.Users).
		Columns(schemafixture.Users.ID, schemafixture.Users.Name).
		Values(sql.NewPlaceholder("id"), sql.NewPlaceholder("name")).
		Prepare(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ins.Close() })
	n, err := ins.Exec(ctx, map[string]any{"id": int64(3), "name": "cara"})
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("inserted %d", n)
	}
	cara, err := pg.Select(schemafixture.Users.Name).
		From(schemafixture.Users).
		Where(op.Eq(schemafixture.Users.ID, int64(3))).
		One(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if cara.Name != "cara" {
		t.Fatalf("name = %s", cara.Name)
	}

	err = db.Tx(ctx, func(tx *pg.Tx) error {
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
		one, err := st.One(ctx, map[string]any{"name": "alice"})
		if err != nil {
			return err
		}
		if one.Name != "alice" {
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

func openTestDB(t *testing.T, ctx context.Context) *pg.DB {
	t.Helper()
	dsn := testdb.PostgresDSN(t)
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.MaxConns = 8
	db, err := pg.OpenConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	return db
}

func resetUsers(t *testing.T, ctx context.Context, db *pg.DB) {
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
