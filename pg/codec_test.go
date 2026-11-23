package pg_test

import (
	"testing"

	"github.com/go-mist/mist/op"
	"github.com/go-mist/mist/pg"
	"github.com/go-mist/mist/pg/codecs"
	"github.com/go-mist/mist/sql"
)

type money struct {
	Amount string
}

type moneyTable struct{ pg.TableBase }

func (moneyTable) Row() *money { return nil }

func (moneyTable) Columns() []sql.Chunk { return []sql.Chunk{moneyAmount} }

func (moneyTable) Scan(row pg.Row, dst *money) error {
	return row.Scan(&dst.Amount)
}

var moneyRows = moneyTable{TableBase: pg.TableBase{SQLName: "money"}}

var moneyAmount = pg.Col[money, string]{
	Table: "money",
	Name:  "amount",
	Ptr:   func(m *money) any { return &m.Amount },
	Codec: codecs.Numeric.Bind("amount"),
}

func TestNumericParamCast(t *testing.T) {
	got, err := pg.Insert(moneyRows).Columns(moneyAmount).Values("1.230000").SQL(pg.Dialect{})
	if err != nil {
		t.Fatal(err)
	}
	const want = `insert into "money" ("amount") values ($1::numeric)`
	if got.SQL != want || len(got.Params) != 1 || got.Params[0] != "1.230000" {
		t.Fatalf("got %#v", got)
	}
	_, err = pg.Insert(moneyRows).Columns(moneyAmount).Values("nope").SQL(pg.Dialect{})
	if err == nil {
		t.Fatal("invalid numeric should fail")
	}

	where, err := pg.Select(moneyAmount).From(moneyRows).Where(op.Eq(moneyAmount, "2.5")).SQL(pg.Dialect{})
	if err != nil {
		t.Fatal(err)
	}
	if where.SQL != `select "money"."amount" from "money" where "money"."amount" = $1::numeric` {
		t.Fatalf("where = %s", where.SQL)
	}
}
