//go:build integration

package mysql_test

import (
	"context"
	"testing"

	"github.com/go-mist/mist/internal/schemafixture"
	"github.com/go-mist/mist/internal/testdb"
	"github.com/go-mist/mist/mysql"
	"github.com/go-mist/mist/op"
)

func TestCRUD(t *testing.T) {
	ctx := context.Background()
	db := openMySQL(t, ctx)
	if _, err := db.Exec(ctx, `drop table if exists users`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `create table users (
		id bigint primary key,
		name varchar(64) not null,
		email varchar(64) null,
		created_at datetime(6) null
	)`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(context.Background(), `drop table if exists users`)
	})

	u := schemafixture.Users
	if _, err := mysql.Insert(u).Columns(u.ID, u.Name).Values(int64(1), "ann").Values(int64(2), "bob").Exec(ctx, db); err != nil {
		t.Fatal(err)
	}
	rows, err := mysql.Select(u.ID, u.Name).From(u).OrderBy(op.Asc(u.ID)).All(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].Name != "ann" || rows[1].Name != "bob" {
		t.Fatalf("%+v", rows)
	}
	if _, err := mysql.Update(u).Set(u.Name, "ada").Where(op.Eq(u.ID, int64(1))).Exec(ctx, db); err != nil {
		t.Fatal(err)
	}
	one, err := mysql.Select(u.ID, u.Name).From(u).Where(op.Eq(u.ID, int64(1))).One(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if one.Name != "ada" {
		t.Fatal(one.Name)
	}
	if _, err := mysql.Delete(u).Where(op.Eq(u.ID, int64(2))).Exec(ctx, db); err != nil {
		t.Fatal(err)
	}
	left, err := mysql.Select(u.ID).From(u).All(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 1 || left[0].ID != 1 {
		t.Fatalf("%+v", left)
	}
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
