package testutil_test

import (
	"path/filepath"
	"testing"

	"github.com/xbmlz/drizzle-go/internal/testutil"
	"github.com/xbmlz/drizzle-go/pg"
	"github.com/xbmlz/drizzle-go/sql"
)

func TestAssertQueryWithDrizzleFixture(t *testing.T) {
	fixture := testutil.LoadFixture(t, filepath.Join("testdata", "select_users.json"))

	query := sql.S(
		sql.Raw("select "), sql.Identifier("id"),
		sql.Raw(", "), sql.Identifier("name"),
		sql.Raw(" from "), sql.Identifier("users"),
		sql.Raw(" where "), sql.Identifier("id"), sql.Raw(" = "), 1,
		sql.Raw(" and "), sql.Identifier("name"), sql.Raw(" = "), "alice",
	)

	got, err := (pg.Dialect{}).SQLToQuery(query)
	if err != nil {
		t.Fatalf("SQLToQuery() error: %v", err)
	}
	testutil.AssertQuery(t, got, fixture)
}

func TestAssertQueryMixedParams(t *testing.T) {
	fixture := testutil.LoadFixture(t, filepath.Join("testdata", "mixed_params.json"))

	got, err := (pg.Dialect{}).SQLToQuery(sql.S(sql.Raw("values "), sql.ListOf([]any{nil, 1.5, "x"})))
	if err != nil {
		t.Fatalf("SQLToQuery() error: %v", err)
	}
	testutil.AssertQuery(t, got, fixture)
}

func TestAssertQueryNoParams(t *testing.T) {
	got, err := (pg.Dialect{}).SQLToQuery(sql.Raw(`select "id" from "users"`))
	if err != nil {
		t.Fatalf("SQLToQuery() error: %v", err)
	}
	testutil.AssertQuery(t, got, testutil.Fixture{SQL: `select "id" from "users"`})
}

func TestJSONEqual(t *testing.T) {
	tests := []struct {
		name string
		a, b string
		want bool
	}{
		{"whitespace", `{"a": 1}`, `{"a":1}`, true},
		{"key order", `{"a":1,"b":2}`, `{"b":2,"a":1}`, true},
		{"number literal", `[1]`, `[1.0]`, false},
		{"different values", `[1]`, `[2]`, false},
		{"nested", `[{"a":1}]`, `[{"a":1.0}]`, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := testutil.JSONEqual(t, []byte(tt.a), []byte(tt.b)); got != tt.want {
				t.Fatalf("JSONEqual(%s, %s) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}
