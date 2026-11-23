//go:build integration

package mysql_test

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/xbmlz/mist/mysql"
	"github.com/xbmlz/mist/mysql/codecs"
	"github.com/xbmlz/mist/op"
	"github.com/xbmlz/mist/sql"
)

type codecSample struct {
	ID      int64
	Amount  string
	Body    json.RawMessage
	Payload []byte
	At      time.Time
}

type codecTable struct{ mysql.TableBase }

func (codecTable) Row() *codecSample { return nil }

func (codecTable) Columns() []sql.Chunk {
	return []sql.Chunk{codecID, codecAmount, codecBody, codecPayload, codecAt}
}

func (codecTable) Scan(row sql.Row, dst *codecSample) error {
	return row.Scan(&dst.ID, &dst.Amount, &dst.Body, &dst.Payload, &dst.At)
}

var codecSamples = codecTable{TableBase: mysql.TableBase{SQLName: "m8_codec"}}

var (
	codecID = mysql.Col[codecSample, int64]{
		Table: "m8_codec", Name: "id",
		Ptr: func(r *codecSample) any { return &r.ID },
	}
	codecAmount = mysql.Col[codecSample, string]{
		Table: "m8_codec", Name: "amount",
		Ptr:   func(r *codecSample) any { return &r.Amount },
		Codec: codecs.Decimal.Bind("amount"),
	}
	codecBody = mysql.Col[codecSample, json.RawMessage]{
		Table: "m8_codec", Name: "body",
		Ptr:   func(r *codecSample) any { return &r.Body },
		Codec: codecs.JSON.Bind("body"),
	}
	codecPayload = mysql.Col[codecSample, []byte]{
		Table: "m8_codec", Name: "payload",
		Ptr:   func(r *codecSample) any { return &r.Payload },
		Codec: codecs.Blob.Bind("payload"),
	}
	codecAt = mysql.Col[codecSample, time.Time]{
		Table: "m8_codec", Name: "at",
		Ptr:   func(r *codecSample) any { return &r.At },
		Codec: codecs.DateTime.Bind("at"),
	}
)

func TestCodecRoundTrip(t *testing.T) {
	ctx := context.Background()
	db := openMySQL(t, ctx)
	if _, err := db.Exec(ctx, `drop table if exists m8_codec`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `create table m8_codec (
		id bigint primary key,
		amount decimal(12,2) not null,
		body json not null,
		payload blob null,
		at datetime(6) not null
	)`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(context.Background(), `drop table if exists m8_codec`)
	})
	at := time.Date(2026, 3, 2, 4, 5, 6, 0, time.UTC)
	_, err := mysql.Insert(codecSamples).
		Columns(codecID, codecAmount, codecBody, codecPayload, codecAt).
		Values(int64(1), "12.50", map[string]int{"a": 1}, []byte("hi"), at).
		Exec(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	row, err := mysql.Select(codecID, codecAmount, codecBody, codecPayload, codecAt).
		From(codecSamples).Where(op.Eq(codecID, int64(1))).One(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if row.Amount != "12.50" {
		t.Fatal(row.Amount)
	}
	var body map[string]int
	if err := json.Unmarshal(row.Body, &body); err != nil {
		t.Fatal(err)
	}
	if body["a"] != 1 {
		t.Fatal(string(row.Body))
	}
	if !bytes.Equal(row.Payload, []byte("hi")) {
		t.Fatalf("%q", row.Payload)
	}
	if row.At.Format("2006-01-02 15:04:05") != "2026-03-02 04:05:06" {
		t.Fatal(row.At)
	}
}
