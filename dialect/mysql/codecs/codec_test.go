package codecs

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/go-mist/mist/sql"
)

func TestNoCast(t *testing.T) {
	encoders := []sql.ParamCodec{
		Decimal.Bind("amount"),
		JSON.Bind("body"),
		DateTime.Bind("at"),
		Blob.Bind("payload"),
	}
	for _, enc := range encoders {
		if got := enc.CastParam("?", 0); got != "" {
			t.Fatalf("cast %q", got)
		}
	}
}

func TestNormalize(t *testing.T) {
	amount, err := Decimal.Bind("amount").NormalizeParam("12.50", 0)
	if err != nil || amount != "12.50" {
		t.Fatal(err, amount)
	}
	if _, err := Decimal.Bind("amount").NormalizeParam("nope", 0); err == nil || !strings.Contains(err.Error(), "amount") {
		t.Fatal(err)
	}
	body, err := JSON.Bind("body").NormalizeParam(map[string]int{"a": 1}, 0)
	if err != nil || body != `{"a":1}` {
		t.Fatal(err, body)
	}
	if _, err := JSON.Bind("body").NormalizeParam("{", 0); err == nil {
		t.Fatal("invalid json accepted")
	}
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	got, err := DateTime.Bind("at").NormalizeParam(now, 0)
	if err != nil || got != now {
		t.Fatal(err, got)
	}
	if _, err := DateTime.Bind("at").NormalizeParam("2026-01-01", 0); err == nil || !strings.Contains(err.Error(), "datetime") {
		t.Fatal(err)
	}
	raw := []byte("hi")
	blob, err := Blob.Bind("payload").NormalizeParam(raw, 0)
	if err != nil || string(blob.([]byte)) != "hi" {
		t.Fatal(err, blob)
	}
	empty, err := JSON.Bind("body").NormalizeParam(json.RawMessage("null"), 0)
	if err != nil || empty != "null" {
		t.Fatal(err, empty)
	}
}
