package relation

import (
	"testing"
	"time"
)

func TestReadJSONHelpers(t *testing.T) {
	var n int64
	if err := ReadTextInt(&n, []byte(`"9007199254740993"`)); err != nil {
		t.Fatal(err)
	}
	if n != 9007199254740993 {
		t.Fatalf("int = %d", n)
	}
	var at time.Time
	if err := ReadTextTime(&at, []byte(`"2026-11-20 08:30:00+08"`)); err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, 11, 20, 8, 30, 0, 0, time.FixedZone("CST", 8*3600))
	if !at.Equal(want) {
		t.Fatalf("time = %s", at)
	}
	if err := ReadTextTime(&at, []byte(`"2026-11-20 08:30:00"`)); err != nil {
		t.Fatal(err)
	}
	if !at.Equal(time.Date(2026, 11, 20, 8, 30, 0, 0, time.UTC)) {
		t.Fatalf("time = %s", at)
	}
	var buf []byte
	if err := ReadHexBytes(&buf, []byte(`"\\x0102"`)); err != nil {
		t.Fatal(err)
	}
	if len(buf) != 2 || buf[0] != 1 || buf[1] != 2 {
		t.Fatalf("bytes = %v", buf)
	}
}
