//go:build integration

package pg_test

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/go-mist/mist/dialect/pg"
	"github.com/go-mist/mist/dialect/pg/codecs"
	"github.com/go-mist/mist/sql"
)

type codecRow struct {
	ID      int32
	Amount  *string
	Body    json.RawMessage
	Payload []byte
	Span    time.Duration
	Tags    []string
	Grid    [][]int32
	At      time.Time
	UID     string
	Mood    string
}

type codecTable struct{ pg.TableBase }

func (codecTable) Row() *codecRow { return nil }

func (codecTable) Columns() []sql.Chunk {
	return []sql.Chunk{codecID, codecAmount, codecBody, codecPayload, codecSpan, codecTags, codecGrid, codecAt, codecUID, codecMood}
}

func (codecTable) Scan(row pg.Row, dst *codecRow) error {
	return row.Scan(&dst.ID, &dst.Amount, &dst.Body, &dst.Payload, &dst.Span, &dst.Tags, &dst.Grid, &dst.At, &dst.UID, &dst.Mood)
}

var codecRows = codecTable{TableBase: pg.TableBase{SQLName: "codec_samples"}}

var (
	codecID     = pg.Col[codecRow, int32]{Table: "codec_samples", Name: "id", Ptr: func(r *codecRow) any { return &r.ID }}
	codecAmount = pg.Col[codecRow, *string]{
		Table: "codec_samples", Name: "amount",
		Ptr:   func(r *codecRow) any { return &r.Amount },
		Codec: codecs.Numeric.Bind("amount"),
	}
	codecBody = pg.Col[codecRow, json.RawMessage]{
		Table: "codec_samples", Name: "body",
		Ptr:   func(r *codecRow) any { return &r.Body },
		Codec: codecs.JSONB.Bind("body"),
	}
	codecPayload = pg.Col[codecRow, []byte]{
		Table: "codec_samples", Name: "payload",
		Ptr:   func(r *codecRow) any { return &r.Payload },
		Codec: codecs.Bytea.Bind("payload"),
	}
	codecSpan = pg.Col[codecRow, time.Duration]{
		Table: "codec_samples", Name: "span",
		Ptr:   func(r *codecRow) any { return &r.Span },
		Codec: codecs.Interval.Bind("span"),
	}
	codecTags = pg.Col[codecRow, []string]{
		Table: "codec_samples", Name: "tags",
		Ptr:   func(r *codecRow) any { return &r.Tags },
		Codec: codecs.Array{BaseType: "text", Dimensions: 1}.Bind("tags"),
	}
	codecGrid = pg.Col[codecRow, [][]int32]{
		Table: "codec_samples", Name: "grid",
		Ptr:   func(r *codecRow) any { return &r.Grid },
		Codec: codecs.Array{BaseType: "int4", Dimensions: 2}.Bind("grid"),
	}
	codecAt = pg.Col[codecRow, time.Time]{
		Table: "codec_samples", Name: "at",
		Ptr:   func(r *codecRow) any { return &r.At },
		Codec: codecs.Timestamptz.Bind("at"),
	}
	codecUID = pg.Col[codecRow, string]{
		Table: "codec_samples", Name: "uid",
		Ptr:   func(r *codecRow) any { return &r.UID },
		Codec: codecs.UUID.Bind("uid"),
	}
	codecMood = pg.Col[codecRow, string]{
		Table: "codec_samples", Name: "mood",
		Ptr:   func(r *codecRow) any { return &r.Mood },
		Codec: codecs.Enum{Name: "codec_mood"}.Bind("mood"),
	}
)

func TestCodecRoundTrip(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t, ctx)
	resetCodec(t, ctx, db)

	amount := "12345678901234.560000"
	body := json.RawMessage(`{"a":[1,{"b":[true,null]}]}`)
	payload := []byte{0x00, 0xff, 'A'}
	when := time.Date(2026, 11, 20, 8, 30, 0, 0, time.FixedZone("CST", 8*3600))
	uid := "6ba7b810-9dad-11d1-80b4-00c04fd430c8"
	_, err := pg.Insert(codecRows).
		Columns(codecID, codecAmount, codecBody, codecPayload, codecSpan, codecTags, codecGrid, codecAt, codecUID, codecMood).
		Values(int32(1), &amount, body, payload, 90*time.Minute, []string{"a", "b"}, [][]int32{{1, 2}, {3, 4}}, when, uid, "ok").
		Exec(ctx, db)
	if err != nil {
		t.Fatal(err)
	}

	got, err := pg.Select[codecRow]().From(codecRows).One(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if got.Amount == nil || *got.Amount != amount {
		t.Fatalf("amount = %#v", got.Amount)
	}
	var wantBody, gotBody any
	if err := json.Unmarshal(body, &wantBody); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(got.Body, &gotBody); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(gotBody, wantBody) {
		t.Fatalf("body = %s", got.Body)
	}
	if !reflect.DeepEqual(got.Payload, payload) {
		t.Fatalf("payload = %#v", got.Payload)
	}
	if got.Span != 90*time.Minute {
		t.Fatalf("span = %s", got.Span)
	}
	if !reflect.DeepEqual(got.Tags, []string{"a", "b"}) {
		t.Fatalf("tags = %#v", got.Tags)
	}
	if !reflect.DeepEqual(got.Grid, [][]int32{{1, 2}, {3, 4}}) {
		t.Fatalf("grid = %#v", got.Grid)
	}
	if !got.At.Equal(when) {
		t.Fatalf("at = %s", got.At)
	}
	if got.UID != uid || got.Mood != "ok" {
		t.Fatalf("uid %s mood %s", got.UID, got.Mood)
	}

	_, err = pg.Update(codecRows).Set(codecAmount, (*string)(nil)).AllRows().Exec(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	cleared, err := pg.Select[codecRow]().From(codecRows).One(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if cleared.Amount != nil {
		t.Fatalf("null amount = %#v", cleared.Amount)
	}
}

func resetCodec(t *testing.T, ctx context.Context, db *pg.DB) {
	t.Helper()
	if _, err := db.Exec(ctx, "drop table if exists codec_samples"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, "drop type if exists codec_mood"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, "create type codec_mood as enum ('ok', 'no')"); err != nil {
		t.Fatal(err)
	}
	_, err := db.Exec(ctx, `
		create table codec_samples (
			id int primary key,
			amount numeric(20,6),
			body jsonb,
			payload bytea,
			span interval,
			tags text[],
			grid int4[][],
			at timestamptz,
			uid uuid,
			mood codec_mood
		)`)
	if err != nil {
		t.Fatal(err)
	}
}
