//go:build integration

package migrate_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/go-mist/mist/internal/testdb"
	"github.com/go-mist/mist/migrate"
	"github.com/go-mist/mist/pg"
)

func TestMigrate(t *testing.T) {
	ctx := context.Background()
	db := openMigDB(t, ctx)
	dropPublic := func(tables ...string) {
		t.Helper()
		for _, table := range tables {
			if _, err := db.Exec(ctx, "drop table if exists "+table); err != nil {
				t.Fatal(err)
			}
		}
	}

	t.Run("twenty files and concurrent", func(t *testing.T) {
		schema := "m6twenty"
		resetSchema(t, ctx, db, schema)
		var tables []string
		dir := t.TempDir()
		for i := 1; i <= 20; i++ {
			table := fmt.Sprintf("m6_step_%02d", i)
			tables = append(tables, table)
			body := fmt.Sprintf("create table %s (id int primary key);\n--> statement-breakpoint\ninsert into %s (id) values (1);\n", table, table)
			writeMig(t, dir, fmt.Sprintf("202401%02d120000_step", i), body)
		}
		t.Cleanup(func() { dropPublic(tables...) })
		cfg := migrate.Config{Folder: dir, Schema: schema}
		other := openMigDB(t, ctx)
		var wg sync.WaitGroup
		errs := make(chan error, 2)
		counts := make(chan int, 2)
		for _, conn := range []*pg.DB{db, other} {
			wg.Add(1)
			go func(conn *pg.DB) {
				defer wg.Done()
				n, err := migrate.Run(ctx, pg.Migrator(conn), cfg)
				errs <- err
				counts <- n
			}(conn)
		}
		wg.Wait()
		close(errs)
		close(counts)
		var applied int
		for err := range errs {
			if err != nil {
				t.Fatal(err)
			}
		}
		for n := range counts {
			applied += n
		}
		if applied != 20 {
			t.Fatalf("applied = %d", applied)
		}
		for _, table := range tables {
			var n int
			if err := queryOne(ctx, db, &n, "select count(*) from "+table); err != nil {
				t.Fatal(err)
			}
			if n != 1 {
				t.Fatalf("%s rows = %d", table, n)
			}
		}
		again, err := migrate.Run(ctx, pg.Migrator(db), cfg)
		if err != nil {
			t.Fatal(err)
		}
		if again != 0 {
			t.Fatalf("second applied = %d", again)
		}
		var created int64
		if err := queryOne(ctx, db, &created, `select created_at from "m6twenty"."__drizzle_migrations" where name = $1`, "20240101120000_step"); err != nil {
			t.Fatal(err)
		}
		want, err := migrate.FolderMillis("20240101120000_step")
		if err != nil {
			t.Fatal(err)
		}
		if created != want {
			t.Fatalf("created_at = %d, want %d", created, want)
		}
	})

	t.Run("gap", func(t *testing.T) {
		schema := "m6gap"
		resetSchema(t, ctx, db, schema)
		dropPublic("m6_gap_a", "m6_gap_b", "m6_gap_c")
		dir := t.TempDir()
		writeMig(t, dir, "20240201000000_a", "create table m6_gap_a (id int);\n--> statement-breakpoint\ninsert into m6_gap_a (id) values (1);")
		writeMig(t, dir, "20240203000000_c", "create table m6_gap_c (id int);\n--> statement-breakpoint\ninsert into m6_gap_c (id) values (1);")
		cfg := migrate.Config{Folder: dir, Schema: schema}
		if _, err := migrate.Run(ctx, pg.Migrator(db), cfg); err != nil {
			t.Fatal(err)
		}
		writeMig(t, dir, "20240202000000_b", "create table m6_gap_b (id int);\n--> statement-breakpoint\ninsert into m6_gap_b (id) values (1);")
		n, err := migrate.Run(ctx, pg.Migrator(db), cfg)
		if err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Fatalf("applied = %d", n)
		}
		for _, table := range []string{"m6_gap_a", "m6_gap_b", "m6_gap_c"} {
			var count int
			if err := queryOne(ctx, db, &count, "select count(*) from "+table); err != nil {
				t.Fatal(err)
			}
			if count != 1 {
				t.Fatalf("%s = %d", table, count)
			}
		}
	})

	t.Run("hash", func(t *testing.T) {
		schema := "m6hash"
		resetSchema(t, ctx, db, schema)
		dropPublic("m6_hash_t")
		dir := t.TempDir()
		name := "20240301000000_hash"
		writeMig(t, dir, name, "create table m6_hash_t (id int);")
		cfg := migrate.Config{Folder: dir, Schema: schema}
		if _, err := migrate.Run(ctx, pg.Migrator(db), cfg); err != nil {
			t.Fatal(err)
		}
		writeMig(t, dir, name, "create table m6_hash_t (id int, name text);")
		_, err := migrate.Run(ctx, pg.Migrator(db), cfg)
		hashErr, ok := err.(*migrate.HashError)
		if !ok || hashErr.Name != name {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("rollback", func(t *testing.T) {
		schema := "m6roll"
		resetSchema(t, ctx, db, schema)
		dropPublic("m6_rb_ok", "m6_rb_bad")
		dir := t.TempDir()
		writeMig(t, dir, "20240401000000_ok", "create table m6_rb_ok (id int);")
		writeMig(t, dir, "20240402000000_bad", "create table m6_rb_bad (id int);\n--> statement-breakpoint\nselect missing_m6_rb;\n")
		_, err := migrate.Run(ctx, pg.Migrator(db), migrate.Config{Folder: dir, Schema: schema})
		if err == nil {
			t.Fatal("expected statement error")
		}
		for _, table := range []string{"m6_rb_ok", "m6_rb_bad"} {
			var name *string
			if err := queryOne(ctx, db, &name, "select to_regclass($1)", "public."+table); err != nil {
				t.Fatal(err)
			}
			if name != nil {
				t.Fatalf("%s still exists", table)
			}
		}
		var n int
		if err := queryOne(ctx, db, &n, `select count(*) from "m6roll"."__drizzle_migrations"`); err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Fatalf("migration rows = %d", n)
		}
	})

	t.Run("old table", func(t *testing.T) {
		schema := "m6old"
		resetSchema(t, ctx, db, schema)
		if _, err := db.Exec(ctx, `create schema m6old`); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(ctx, `create table m6old.__drizzle_migrations (
			id serial primary key,
			hash text not null,
			created_at bigint
		)`); err != nil {
			t.Fatal(err)
		}
		dir := t.TempDir()
		writeMig(t, dir, "20240501000000_old", "select 1;")
		_, err := migrate.Run(ctx, pg.Migrator(db), migrate.Config{Folder: dir, Schema: schema})
		old, ok := err.(*migrate.OldTableError)
		if !ok || old.Table != "__drizzle_migrations" {
			t.Fatalf("error = %v", err)
		}
	})
}

func writeMig(t *testing.T, root, name, body string) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "migration.sql"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func resetSchema(t *testing.T, ctx context.Context, db *pg.DB, schema string) {
	t.Helper()
	if _, err := db.Exec(ctx, "drop schema if exists "+schema+" cascade"); err != nil {
		t.Fatal(err)
	}
}

func queryOne(ctx context.Context, db *pg.DB, dest any, sql string, args ...any) error {
	rows, err := db.Query(ctx, sql, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return err
		}
		return fmt.Errorf("no row")
	}
	if err := rows.Scan(dest); err != nil {
		return err
	}
	return rows.Err()
}

func openMigDB(t *testing.T, ctx context.Context) *pg.DB {
	t.Helper()
	dsn := testdb.PostgresDSN(t)
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.MaxConns = 4
	db, err := pg.OpenConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}
