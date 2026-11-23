package codecs_test

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/go-mist/mist/pg/codecs"
)

func TestNumericCastAndReject(t *testing.T) {
	enc := codecs.Numeric.Bind("amount")
	got, err := enc.NormalizeParam("1.230000", 0)
	if err != nil || got != "1.230000" {
		t.Fatalf("normalize = %v, %v", got, err)
	}
	if cast := enc.CastParam("$1", 0); cast != "$1::numeric" {
		t.Fatalf("cast = %s", cast)
	}
	_, err = enc.NormalizeParam("12abc", 0)
	if err == nil || !strings.Contains(err.Error(), "amount") || !strings.Contains(err.Error(), "numeric") {
		t.Fatalf("err = %v", err)
	}
	_, err = enc.NormalizeParam(1, 0)
	if err == nil || !strings.Contains(err.Error(), "column amount") {
		t.Fatalf("err = %v", err)
	}
}

func TestJSONNestedRoundTrip(t *testing.T) {
	enc := codecs.JSONB.Bind("body")
	in := map[string]any{"a": []any{float64(1), map[string]any{"b": []any{true, nil}}}}
	got, err := enc.NormalizeParam(in, 0)
	if err != nil {
		t.Fatal(err)
	}
	if enc.CastParam("$1", 0) != "$1::jsonb" {
		t.Fatalf("cast = %s", enc.CastParam("$1", 0))
	}
	raw, err := codecs.JSONB.Normalize(got)
	if err != nil {
		t.Fatal(err)
	}
	var back map[string]any
	if err := json.Unmarshal(raw.(json.RawMessage), &back); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(back, in) {
		t.Fatalf("back = %#v", back)
	}
}

func TestArrayAndUUID(t *testing.T) {
	tags := codecs.Array{BaseType: "text", Dimensions: 1}.Bind("tags")
	if tags.CastParam("$1", tags.Dimensions()) != "$1::text[]" {
		t.Fatalf("text array = %s", tags.CastParam("$1", tags.Dimensions()))
	}
	grid := codecs.Array{BaseType: "int4", Dimensions: 2}.Bind("grid")
	if grid.CastParam("$2", grid.Dimensions()) != "$2::int4[][]" {
		t.Fatalf("int grid = %s", grid.CastParam("$2", grid.Dimensions()))
	}
	_, err := tags.NormalizeParam("nope", 1)
	if err == nil || !strings.Contains(err.Error(), "tags") {
		t.Fatalf("err = %v", err)
	}
	uid := codecs.UUID.Bind("uid")
	if uid.CastParam("$1", 0) != "$1::uuid" {
		t.Fatalf("uuid cast = %s", uid.CastParam("$1", 0))
	}
	if _, err := uid.NormalizeParam("not-a-uuid", 0); err == nil {
		t.Fatal("invalid uuid was accepted")
	}
}

func TestIntRangeAndRegistry(t *testing.T) {
	enc := codecs.Int2.Bind("n")
	if _, err := enc.NormalizeParam(40000, 0); err == nil || !strings.Contains(err.Error(), "n") {
		t.Fatalf("err = %v", err)
	}
	if _, err := enc.NormalizeParam(int16(3), 0); err != nil {
		t.Fatal(err)
	}
	if codecs.Default.Get("numeric") == nil || codecs.Default.GetOID(1700) == nil {
		t.Fatal("numeric codec was not registered")
	}
	if codecs.Default.Get("geometry") != nil {
		t.Fatal("unknown type should be missing")
	}
	if _, err := codecs.Default.Bind("geometry", "g", 0); err == nil || !strings.Contains(err.Error(), "no codec registered") {
		t.Fatalf("err = %v", err)
	}
}

func TestCustomCodec(t *testing.T) {
	reg := codecs.NewCollection()
	reg.Register("citext", upperCodec{})
	enc, err := reg.Bind("citext", "name", 0)
	if err != nil {
		t.Fatal(err)
	}
	got, err := enc.NormalizeParam("Ab", 0)
	if err != nil || got != "AB" {
		t.Fatalf("got %v %v", got, err)
	}
	if enc.CastParam("$1", 0) != "$1::citext" {
		t.Fatalf("cast = %s", enc.CastParam("$1", 0))
	}
}

func TestByteaHexAndInterval(t *testing.T) {
	enc := codecs.Bytea.Bind("payload")
	got, err := enc.NormalizeParam(`\x61ff`, 0)
	if err != nil {
		t.Fatal(err)
	}
	raw, ok := got.([]byte)
	if !ok || len(raw) != 2 || raw[0] != 0x61 || raw[1] != 0xff {
		t.Fatalf("got %#v", got)
	}
	if enc.CastParam("$1", 0) != "$1::bytea" {
		t.Fatalf("cast = %s", enc.CastParam("$1", 0))
	}
	span := codecs.Interval.Bind("span")
	if _, err := span.NormalizeParam(time.Minute, 0); err != nil {
		t.Fatal(err)
	}
	if span.CastParam("$1", 0) != "$1::interval" {
		t.Fatalf("cast = %s", span.CastParam("$1", 0))
	}
}

type upperCodec struct{ codecs.Base }

func (upperCodec) NormalizeParam(value any, _ int) (any, error) {
	s, ok := value.(string)
	if !ok {
		return nil, fmt.Errorf("got %T", value)
	}
	return strings.ToUpper(s), nil
}

func (upperCodec) CastParam(placeholder string, _ int) string {
	return placeholder + "::citext"
}
