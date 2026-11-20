package pg_test

import (
	"reflect"
	"testing"

	"github.com/xbmlz/drizzle-go/pg"
	"github.com/xbmlz/drizzle-go/sql"
)

func TestEscapeName(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"users", `"users"`},
		{"user name", `"user name"`},
		{`we"ird`, `"we""ird"`},
		{"", `""`},
	}
	for _, tt := range tests {
		if got := (pg.Dialect{}).EscapeName(tt.in); got != tt.want {
			t.Fatalf("EscapeName(%q) = %s, want %s", tt.in, got, tt.want)
		}
	}
}

func TestEscapeParam(t *testing.T) {
	d := pg.Dialect{}
	tests := []struct {
		num  int
		want string
	}{
		{0, "$1"},
		{1, "$2"},
		{99, "$100"},
	}
	for _, tt := range tests {
		// value 参数在 pg 上被忽略，传 nil 与真实值结果相同。
		if got := d.EscapeParam(tt.num, nil); got != tt.want {
			t.Fatalf("EscapeParam(%d) = %s, want %s", tt.num, got, tt.want)
		}
		if got := d.EscapeParam(tt.num, "ignored"); got != tt.want {
			t.Fatalf("EscapeParam(%d) with value = %s, want %s", tt.num, got, tt.want)
		}
	}
}

func TestEscapeString(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"alice", "'alice'"},
		{"O'Reilly", "'O''Reilly'"},
		{"", "''"},
		{"a b", "'a b'"},
	}
	for _, tt := range tests {
		if got := (pg.Dialect{}).EscapeString(tt.in); got != tt.want {
			t.Fatalf("EscapeString(%q) = %s, want %s", tt.in, got, tt.want)
		}
	}
}

func TestSQLToQuery(t *testing.T) {
	query := sql.S(
		sql.Raw("select "), sql.Identifier("name"),
		sql.Raw(" from "), sql.Identifier("users"),
		sql.Raw(" where "), sql.Identifier("id"), sql.Raw(" = "), 1,
	)

	got, err := (pg.Dialect{}).SQLToQuery(query)
	if err != nil {
		t.Fatalf("SQLToQuery() error: %v", err)
	}

	wantSQL := `select "name" from "users" where "id" = $1`
	if got.SQL != wantSQL {
		t.Fatalf("got %s want %s", got.SQL, wantSQL)
	}
	if want := []any{1}; !reflect.DeepEqual(got.Params, want) {
		t.Fatalf("params mismatch: got %#v want %#v", got.Params, want)
	}
}

func TestSQLToQueryInline(t *testing.T) {
	query := sql.S(sql.Raw("where "), sql.Identifier("name"), sql.Raw(" = "), "O'Reilly")

	got, err := (pg.Dialect{}).SQLToQueryInline(query)
	if err != nil {
		t.Fatalf("SQLToQueryInline() error: %v", err)
	}

	wantSQL := `where "name" = 'O''Reilly'`
	if got.SQL != wantSQL {
		t.Fatalf("got %s want %s", got.SQL, wantSQL)
	}
	if len(got.Params) != 0 {
		t.Fatalf("inline query must not carry params: %#v", got.Params)
	}
}
