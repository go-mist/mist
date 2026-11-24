package pg_test

import (
	"testing"

	"github.com/go-mist/mist/dialect/pg"
	"github.com/go-mist/mist/sql"
)

func TestColAndTableRender(t *testing.T) {
	col := pg.Col[struct{}, int64]{Table: "users", Name: "id"}
	base := pg.TableBase{SQLName: "users"}
	got, err := (pg.Dialect{}).SQLToQuery(sql.S(
		sql.Raw("select "), col, sql.Raw(" from "), base,
	))
	if err != nil {
		t.Fatal(err)
	}
	if want := `select "users"."id" from "users"`; got.SQL != want || got.Params != nil {
		t.Fatalf("got %#v want %s", got, want)
	}

	bare := pg.Col[struct{}, string]{Name: "id"}
	got, err = (pg.Dialect{}).SQLToQuery(sql.S(bare))
	if err != nil {
		t.Fatal(err)
	}
	if want := `"id"`; got.SQL != want {
		t.Fatalf("got %s want %s", got.SQL, want)
	}
}
