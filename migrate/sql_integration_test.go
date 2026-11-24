//go:build integration

package migrate_test

import (
	"context"
	"embed"
	"fmt"
	"sync"
	"testing"

	"github.com/go-mist/mist/dialect/mysql"
	"github.com/go-mist/mist/dialect/sqlite"
	"github.com/go-mist/mist/internal/testdb"
	"github.com/go-mist/mist/migrate"
)

//go:embed all:testdata/sqlite
var sqliteMigrations embed.FS

func TestSQLiteEmbed(t *testing.T) {
	ctx := context.Background()
	db := openSQLite(t, ctx, testdb.SQLiteDSN(t))
	cfg := migrate.Config{FS: sqliteMigrations, Folder: "testdata/sqlite", Table: "m6lite_embed_migrations"}
	applied, err := migrate.Run(ctx, sqlite.Migrator(db), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if applied != 1 {
		t.Fatalf("applied = %d", applied)
	}
	var id int
	if err := querySQLite(ctx, db, &id, "select id from m6lite_embed"); err != nil {
		t.Fatal(err)
	}
	if id != 7 {
		t.Fatalf("id = %d", id)
	}
	var name string
	err = querySQLite(ctx, db, &name, "select name from sqlite_master where type = 'table' and name = ?", "m6lite_embed_down")
	if err == nil {
		t.Fatal("down.sql was applied")
	}
	again, err := migrate.Run(ctx, sqlite.Migrator(db), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if again != 0 {
		t.Fatalf("second applied = %d", again)
	}
}

func TestSQLiteMigrate(t *testing.T) {
	ctx := context.Background()
	dsn := testdb.SQLiteDSN(t)
	db := openSQLite(t, ctx, dsn)

	t.Run("apply and concurrent", func(t *testing.T) {
		dir := t.TempDir()
		const n = 8
		for i := 1; i <= n; i++ {
			table := fmt.Sprintf("m6lite_step_%02d", i)
			body := fmt.Sprintf("create table %s (id int primary key);\n--> statement-breakpoint\ninsert into %s (id) values (1);\n", table, table)
			writeMig(t, dir, fmt.Sprintf("202501%02d120000_step", i), body)
		}
		other := openSQLite(t, ctx, dsn)
		cfg := migrate.Config{Folder: dir}
		applied := runTwo(t, ctx, cfg, sqlite.Migrator(db), sqlite.Migrator(other))
		if applied != n {
			t.Fatalf("applied = %d", applied)
		}
		for i := 1; i <= n; i++ {
			var count int
			table := fmt.Sprintf("m6lite_step_%02d", i)
			if err := querySQLite(ctx, db, &count, "select count(*) from "+table); err != nil {
				t.Fatal(err)
			}
			if count != 1 {
				t.Fatalf("%s = %d", table, count)
			}
		}
		again, err := migrate.Run(ctx, sqlite.Migrator(db), cfg)
		if err != nil {
			t.Fatal(err)
		}
		if again != 0 {
			t.Fatalf("second applied = %d", again)
		}
	})

	t.Run("rollback", func(t *testing.T) {
		dir := t.TempDir()
		writeMig(t, dir, "20250401000000_ok", "create table m6lite_rb_ok (id int);")
		writeMig(t, dir, "20250402000000_bad", "create table m6lite_rb_bad (id int);\n--> statement-breakpoint\nselect missing_m6lite_rb;\n")
		_, err := migrate.Run(ctx, sqlite.Migrator(db), migrate.Config{Folder: dir, Table: "m6lite_rb_migrations"})
		if err == nil {
			t.Fatal("expected statement error")
		}
		for _, table := range []string{"m6lite_rb_ok", "m6lite_rb_bad"} {
			var name string
			err := querySQLite(ctx, db, &name, "select name from sqlite_master where type = 'table' and name = ?", table)
			if err == nil {
				t.Fatalf("%s still exists", table)
			}
		}
		var rows int
		if err := querySQLite(ctx, db, &rows, `select count(*) from "m6lite_rb_migrations"`); err != nil {
			t.Fatal(err)
		}
		if rows != 0 {
			t.Fatalf("migration rows = %d", rows)
		}
	})

	t.Run("old table", func(t *testing.T) {
		if _, err := db.Exec(ctx, `create table m6lite_old (id integer primary key, hash text not null, created_at integer)`); err != nil {
			t.Fatal(err)
		}
		dir := t.TempDir()
		writeMig(t, dir, "20250501000000_old", "select 1;")
		_, err := migrate.Run(ctx, sqlite.Migrator(db), migrate.Config{Folder: dir, Table: "m6lite_old"})
		old, ok := err.(*migrate.OldTableError)
		if !ok || old.Table != "m6lite_old" {
			t.Fatalf("error = %v", err)
		}
	})
}

func TestMySQLMigrate(t *testing.T) {
	ctx := context.Background()
	db := openMySQL(t, ctx)
	schema := "m6mysql"
	if _, err := db.Exec(ctx, "drop database if exists "+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx := context.Background()
		for i := 1; i <= 8; i++ {
			_, _ = db.Exec(ctx, fmt.Sprintf("drop table if exists m6my_step_%02d", i))
		}
		_, _ = db.Exec(ctx, "drop table if exists m6my_hash")
		_, _ = db.Exec(ctx, "drop database if exists "+schema)
	})

	t.Run("apply and concurrent", func(t *testing.T) {
		dir := t.TempDir()
		const n = 8
		for i := 1; i <= n; i++ {
			table := fmt.Sprintf("m6my_step_%02d", i)
			body := fmt.Sprintf("create table %s (id int primary key);\n--> statement-breakpoint\ninsert into %s (id) values (1);\n", table, table)
			writeMig(t, dir, fmt.Sprintf("202506%02d120000_step", i), body)
		}
		other := openMySQL(t, ctx)
		cfg := migrate.Config{Folder: dir, Schema: schema}
		applied := runTwo(t, ctx, cfg, mysql.Migrator(db), mysql.Migrator(other))
		if applied != n {
			t.Fatalf("applied = %d", applied)
		}
		for i := 1; i <= n; i++ {
			var count int
			table := fmt.Sprintf("m6my_step_%02d", i)
			if err := queryMySQL(ctx, db, &count, "select count(*) from "+table); err != nil {
				t.Fatal(err)
			}
			if count != 1 {
				t.Fatalf("%s = %d", table, count)
			}
		}
		again, err := migrate.Run(ctx, mysql.Migrator(db), cfg)
		if err != nil {
			t.Fatal(err)
		}
		if again != 0 {
			t.Fatalf("second applied = %d", again)
		}
	})

	t.Run("hash", func(t *testing.T) {
		dir := t.TempDir()
		name := "20250701000000_hash"
		writeMig(t, dir, name, "create table m6my_hash (id int);")
		cfg := migrate.Config{Folder: dir, Schema: schema, Table: "m6my_hash_migrations"}
		if _, err := migrate.Run(ctx, mysql.Migrator(db), cfg); err != nil {
			t.Fatal(err)
		}
		writeMig(t, dir, name, "create table m6my_hash (id int, name text);")
		_, err := migrate.Run(ctx, mysql.Migrator(db), cfg)
		hashErr, ok := err.(*migrate.HashError)
		if !ok || hashErr.Name != name {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("statement error records nothing", func(t *testing.T) {
		dir := t.TempDir()
		writeMig(t, dir, "20250801000000_bad", "select missing_m6my;")
		cfg := migrate.Config{Folder: dir, Schema: schema, Table: "m6my_bad_migrations"}
		if _, err := migrate.Run(ctx, mysql.Migrator(db), cfg); err == nil {
			t.Fatal("expected statement error")
		}
		var rows int
		if err := queryMySQL(ctx, db, &rows, "select count(*) from "+schema+".m6my_bad_migrations"); err != nil {
			t.Fatal(err)
		}
		if rows != 0 {
			t.Fatalf("migration rows = %d", rows)
		}
	})
}

func runTwo(t *testing.T, ctx context.Context, cfg migrate.Config, a, b migrate.Conn) int {
	t.Helper()
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	counts := make(chan int, 2)
	for _, conn := range []migrate.Conn{a, b} {
		wg.Add(1)
		go func(conn migrate.Conn) {
			defer wg.Done()
			n, err := migrate.Run(ctx, conn, cfg)
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
	return applied
}

func openSQLite(t *testing.T, ctx context.Context, dsn string) *sqlite.DB {
	t.Helper()
	db, err := sqlite.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	db.SQLDB().SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func openMySQL(t *testing.T, ctx context.Context) *mysql.DB {
	t.Helper()
	db, err := mysql.Open(ctx, testdb.MySQLDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func querySQLite(ctx context.Context, db *sqlite.DB, dest any, sql string, args ...any) error {
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

func queryMySQL(ctx context.Context, db *mysql.DB, dest any, sql string, args ...any) error {
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
