//go:build integration

package oracle_test

import (
	"context"
	"os"
	"testing"

	"github.com/go-mist/mist"
	"github.com/go-mist/mist/dialect/oracle"
	"github.com/go-mist/mist/op"
	"github.com/go-mist/mist/sql"
)

func TestOracleCRUD(t *testing.T) {
	dsn := os.Getenv("ORACLE_DSN")
	if dsn == "" {
		t.Skip("ORACLE_DSN is not set")
	}
	ctx := context.Background()
	db, err := oracle.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if _, err := db.Exec(ctx, `BEGIN EXECUTE IMMEDIATE 'DROP TABLE "mist_oracle_tasks"'; EXCEPTION WHEN OTHERS THEN NULL; END;`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `CREATE TABLE "mist_oracle_tasks" ("id" NUMBER(19) PRIMARY KEY, "title" VARCHAR2(200))`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(context.Background(), `DROP TABLE "mist_oracle_tasks"`)
	})

	tasks := oracleTasks()
	if _, err := oracle.Insert(tasks).Columns(tasks.ID, tasks.Title).Values(int64(1), "buy milk").Exec(ctx, db); err != nil {
		t.Fatal(err)
	}
	row, err := oracle.Select(tasks.ID, tasks.Title).From(tasks).Where(op.Eq(tasks.ID, int64(1))).One(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if row.Title != "buy milk" {
		t.Fatalf("%+v", row)
	}
	err = db.Tx(ctx, func(tx mist.Tx) error {
		_, err := oracle.Update(tasks).Set(tasks.Title, "bought").Where(op.Eq(tasks.ID, int64(1))).Exec(ctx, tx)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := oracle.Delete(tasks).Where(op.Eq(tasks.ID, int64(1))).Exec(ctx, db); err != nil {
		t.Fatal(err)
	}
	_, err = oracle.Select(tasks.ID).From(tasks).Where(op.Eq(tasks.ID, int64(1))).One(ctx, db)
	if err == nil {
		t.Fatal("row still present")
	}
}

type oracleTask struct {
	ID    int64
	Title string
}

type oracleTaskTable struct {
	mist.TableBase
	ID    mist.Col[oracleTask, int64]
	Title mist.Col[oracleTask, string]
}

func (oracleTaskTable) Row() *oracleTask { return nil }

func (t oracleTaskTable) Columns() []sql.Chunk {
	return []sql.Chunk{t.ID, t.Title}
}

func (t oracleTaskTable) Scan(row mist.Row, dst *oracleTask) error {
	return row.Scan(&dst.ID, &dst.Title)
}

func oracleTasks() oracleTaskTable {
	return oracleTaskTable{
		TableBase: mist.TableBase{SQLName: "mist_oracle_tasks"},
		ID: mist.Col[oracleTask, int64]{
			Table: "mist_oracle_tasks",
			Name:  "id",
			Ptr:   func(v *oracleTask) any { return &v.ID },
		},
		Title: mist.Col[oracleTask, string]{
			Table: "mist_oracle_tasks",
			Name:  "title",
			Ptr:   func(v *oracleTask) any { return &v.Title },
		},
	}
}
