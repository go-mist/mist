package op_test

import (
	"testing"

	"github.com/go-mist/mist/op"
	"github.com/go-mist/mist/pg"
	"github.com/go-mist/mist/sql"
)

func TestAggregates(t *testing.T) {
	id := pg.Col[struct{}, int64]{Table: "users", Name: "id"}
	tests := []struct {
		name string
		expr *sql.SQL
		want string
	}{
		{"count star", op.Count(), `count(*)`},
		{"count nil", op.Count(nil), `count(*)`},
		{"count", op.Count(id), `count("users"."id")`},
		{"count distinct", op.CountDistinct(id), `count(distinct "users"."id")`},
		{"sum", op.Sum(id), `sum("users"."id")`},
		{"sum distinct", op.SumDistinct(id), `sum(distinct "users"."id")`},
		{"avg", op.Avg(id), `avg("users"."id")`},
		{"avg distinct", op.AvgDistinct(id), `avg(distinct "users"."id")`},
		{"min", op.Min(id), `min("users"."id")`},
		{"max", op.Max(id), `max("users"."id")`},
		{"order", op.Desc(op.Count(id)).GetSQL(), `count("users"."id") desc`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := (pg.Dialect{}).SQLToQuery(tt.expr)
			if err != nil {
				t.Fatal(err)
			}
			if got.SQL != tt.want || len(got.Params) != 0 {
				t.Fatalf("got %#v", got)
			}
		})
	}
}
