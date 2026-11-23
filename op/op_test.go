package op_test

import (
	"testing"

	"github.com/xbmlz/mist/op"
	"github.com/xbmlz/mist/pg"
	"github.com/xbmlz/mist/sql"
)

func TestComparisons(t *testing.T) {
	col := pg.Col[struct{}, int64]{Table: "users", Name: "id"}
	tests := []struct {
		name string
		expr *sql.SQL
		want string
	}{
		{"eq", op.Eq(col, 1).GetSQL(), `"users"."id" = $1`},
		{"ne", op.Ne(col, 1).GetSQL(), `"users"."id" <> $1`},
		{"gt", op.Gt(col, 1).GetSQL(), `"users"."id" > $1`},
		{"gte", op.Gte(col, 1).GetSQL(), `"users"."id" >= $1`},
		{"lt", op.Lt(col, 1).GetSQL(), `"users"."id" < $1`},
		{"lte", op.Lte(col, 1).GetSQL(), `"users"."id" <= $1`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := (pg.Dialect{}).SQLToQuery(tt.expr)
			if err != nil {
				t.Fatal(err)
			}
			if got.SQL != tt.want || len(got.Params) != 1 || got.Params[0] != 1 {
				t.Fatalf("got %#v", got)
			}
		})
	}
}

func TestConditions(t *testing.T) {
	id := pg.Col[struct{}, int64]{Table: "users", Name: "id"}
	name := pg.Col[struct{}, string]{Table: "users", Name: "name"}
	email := pg.Col[struct{}, *string]{Table: "users", Name: "email"}
	ph := sql.NewPlaceholder("id")

	tests := []struct {
		name   string
		expr   *sql.SQL
		want   string
		params []any
	}{
		{"in", op.InArray(id, []int64{1, 7}), `"users"."id" in ($1, $2)`, []any{int64(1), int64(7)}},
		{"in empty", op.InArray(id, []int64{}), `1 = 0`, nil},
		{"not in", op.NotInArray(name, []string{"bob"}), `"users"."name" not in ($1)`, []any{"bob"}},
		{"not in empty", op.NotInArray(id, []int64(nil)), `1 = 1`, nil},
		{"in placeholder", op.InArray(id, []any{ph, int64(2)}), `"users"."id" in ($1, $2)`, []any{ph, int64(2)}},
		{"between", op.Between(id, 2, 8), `"users"."id" between $1 and $2`, []any{2, 8}},
		{"not between", op.NotBetween(id, 2, 8), `"users"."id" not between $1 and $2`, []any{2, 8}},
		{"like", op.Like(name, "a%"), `"users"."name" like $1`, []any{"a%"}},
		{"not like", op.NotLike(name, "a%"), `"users"."name" not like $1`, []any{"a%"}},
		{"ilike", op.ILike(name, "ALICE"), `"users"."name" ilike $1`, []any{"ALICE"}},
		{"not ilike", op.NotILike(name, "%A%"), `"users"."name" not ilike $1`, []any{"%A%"}},
		{"is null", op.IsNull(email), `("users"."email" is null)`, nil},
		{"is not null", op.IsNotNull(email), `("users"."email" is not null)`, nil},
		{"exists", op.Exists(sql.Raw("select 1")), `exists (select 1)`, nil},
		{"not exists", op.NotExists(sql.Raw("select 1")), `not exists (select 1)`, nil},
		{"in query", op.In(id, sql.Raw("select 1")), `"users"."id" in (select 1)`, nil},
		{"not in query", op.NotIn(id, sql.Raw("select 1")), `"users"."id" not in (select 1)`, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := (pg.Dialect{}).SQLToQuery(tt.expr)
			if err != nil {
				t.Fatal(err)
			}
			if got.SQL != tt.want || !sameParams(got.Params, tt.params) {
				t.Fatalf("got %#v", got)
			}
		})
	}
}

func TestArithmetic(t *testing.T) {
	id := pg.Col[struct{}, int64]{Table: "users", Name: "id"}
	got, err := (pg.Dialect{}).SQLToQuery(op.Add(id, int64(1)))
	if err != nil {
		t.Fatal(err)
	}
	if got.SQL != `("users"."id" + $1)` || len(got.Params) != 1 || got.Params[0] != int64(1) {
		t.Fatalf("add = %#v", got)
	}
	nested, err := (pg.Dialect{}).SQLToQuery(op.Mul(op.Add(id, int64(1)), int64(2)))
	if err != nil {
		t.Fatal(err)
	}
	if nested.SQL != `(("users"."id" + $1) * $2)` {
		t.Fatalf("nested = %s", nested.SQL)
	}
}

func sameParams(got, want []any) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
