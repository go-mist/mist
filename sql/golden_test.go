package sql_test

import (
	"testing"

	"github.com/go-mist/mist/internal/testutil"
	"github.com/go-mist/mist/pg"
	"github.com/go-mist/mist/sql"
)

// Golden cases are compared against drizzle: a fixture only describes the expected (sql, params).
// Add a Case here and run `go test ./sql/ -update-golden` to write testdata/<name>.json.

func TestGolden(t *testing.T) {
	d := pg.Dialect{}
	testutil.Run(t, "testdata", []testutil.Case{
		{
			Name: "subquery",
			Build: func() (sql.Query, error) {
				query := sql.S(
					sql.Raw("select "), sql.Identifier("id"), sql.Raw(" from "),
					sql.NewSubquery(sql.Raw("select 1"), "sq"),
				)
				return d.SQLToQuery(query)
			},
		},
		{
			Name: "value_rows",
			Build: func() (sql.Query, error) {
				rows := &sql.ValueRows{
					Cells:    []any{1, "a", 2, "b"},
					Encoders: []sql.Encoder{sql.NoopEncoder{}, sql.NoopEncoder{}},
				}
				return d.SQLToQuery(sql.S(sql.Raw("values "), rows))
			},
		},
	})
}
