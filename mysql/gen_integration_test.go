//go:build integration

package mysql_test

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/xbmlz/mist/internal/mysqlfixture"
	"github.com/xbmlz/mist/mysql"
	"github.com/xbmlz/mist/op"
)

func TestGeneratedNote(t *testing.T) {
	ctx := context.Background()
	db := openMySQL(t, ctx)
	if _, err := db.Exec(ctx, `drop table if exists m8_notes`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `create table m8_notes (
		id bigint primary key,
		title varchar(64) not null,
		body json not null,
		amount decimal(12,2) null,
		at datetime(6) not null,
		payload blob not null,
		flag tinyint not null
	)`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(context.Background(), `drop table if exists m8_notes`)
	})

	n := mysqlfixture.Notes
	at := time.Date(2026, 4, 5, 6, 7, 8, 0, time.UTC)
	_, err := mysql.Insert(n).
		Columns(n.ID, n.Title, n.Body, n.Amount, n.At, n.Payload, n.Flag).
		Values(int64(1), "hello", map[string]int{"a": 1}, "12.50", at, []byte("hi"), int8(1)).
		Exec(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	row, err := mysql.Select[mysqlfixture.Note]().From(n).Where(op.Eq(n.ID, int64(1))).One(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if row.Title != "hello" || row.Flag != 1 {
		t.Fatalf("%+v", row)
	}
	if row.Amount == nil || *row.Amount != "12.50" {
		t.Fatalf("amount %#v", row.Amount)
	}
	var body map[string]int
	if err := json.Unmarshal(row.Body, &body); err != nil || body["a"] != 1 {
		t.Fatal(err, string(row.Body))
	}
	if !bytes.Equal(row.Payload, []byte("hi")) {
		t.Fatalf("%q", row.Payload)
	}
	if row.At.Format("2006-01-02 15:04:05") != "2026-04-05 06:07:08" {
		t.Fatal(row.At)
	}
}
