package sqlite_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/go-mist/mist"
	"github.com/go-mist/mist/logger"
	"github.com/go-mist/mist/sqlite"
)

type recording struct {
	lines []string
}

func (r *recording) LogQuery(query string, params []any) {
	logger.Default{Writer: writerFunc(func(message string) {
		r.lines = append(r.lines, message)
	})}.LogQuery(query, params)
}

type writerFunc func(string)

func (f writerFunc) Write(message string) { f(message) }

func TestQueryLog(t *testing.T) {
	ctx := context.Background()
	log := &recording{}
	db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "log.db"), sqlite.WithLogger(log))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(ctx, `create table notes (id integer primary key, name text)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `insert into notes (id, name) values (?, ?)`, int64(1), "ada"); err != nil {
		t.Fatal(err)
	}
	rows, err := db.Query(ctx, `select name from notes where id = ?`, int64(1))
	if err != nil {
		t.Fatal(err)
	}
	rows.Close()
	stmt, err := db.Prepare(ctx, `select name from notes where id = ?`)
	if err != nil {
		t.Fatal(err)
	}
	got, err := stmt.Query(ctx, int64(1))
	if err != nil {
		t.Fatal(err)
	}
	got.Close()
	if err := stmt.Close(); err != nil {
		t.Fatal(err)
	}
	err = db.Tx(ctx, func(tx mist.Tx) error {
		_, err := tx.Exec(ctx, `insert into notes (id, name) values (?, ?)`, int64(2), "bea")
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"Query: create table notes (id integer primary key, name text)",
		`Query: insert into notes (id, name) values (?, ?) -- params: [1, "ada"]`,
		`Query: select name from notes where id = ? -- params: [1]`,
		`Query: select name from notes where id = ? -- params: [1]`,
		`Query: insert into notes (id, name) values (?, ?) -- params: [2, "bea"]`,
	}
	if len(log.lines) != len(want) {
		t.Fatalf("%v", log.lines)
	}
	for i := range want {
		if log.lines[i] != want[i] {
			t.Fatalf("%d\n got %s\nwant %s", i, log.lines[i], want[i])
		}
	}
}
