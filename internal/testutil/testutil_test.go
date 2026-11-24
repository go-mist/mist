package testutil_test

import (
	"testing"

	"github.com/go-mist/mist/dialect/pg"
	"github.com/go-mist/mist/internal/testutil"
	"github.com/go-mist/mist/sql"
)

func TestGoldenFixtures(t *testing.T) {
	d := pg.Dialect{}
	testutil.Run(t, "testdata", []testutil.Case{
		{
			Name: "select_users",
			Build: func() (sql.Query, error) {
				query := sql.S(
					sql.Raw("select "), sql.Identifier("id"),
					sql.Raw(", "), sql.Identifier("name"),
					sql.Raw(" from "), sql.Identifier("users"),
					sql.Raw(" where "), sql.Identifier("id"), sql.Raw(" = "), 1,
					sql.Raw(" and "), sql.Identifier("name"), sql.Raw(" = "), "alice",
				)
				return d.SQLToQuery(query)
			},
		},
		{
			Name: "mixed_params",
			Build: func() (sql.Query, error) {
				return d.SQLToQuery(sql.S(sql.Raw("values "), sql.ListOf([]any{nil, 1.5, "x"})))
			},
		},
	})
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
