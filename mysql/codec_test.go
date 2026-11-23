package mysql

import (
	"strings"
	"testing"

	"github.com/xbmlz/mist/mysql/codecs"
	"github.com/xbmlz/mist/sql"
)

type codecRow struct {
	Body string
}

type codecProbe struct{ TableBase }

func (codecProbe) Row() *codecRow { return nil }

func (codecProbe) Columns() []sql.Chunk { return []sql.Chunk{codecBodyCol} }

func (codecProbe) Scan(row sql.Row, dst *codecRow) error {
	return row.Scan(&dst.Body)
}

var codecBodyCol = Col[codecRow, string]{
	Table: "samples", Name: "body",
	Codec: codecs.JSON.Bind("body"),
	Ptr:   func(r *codecRow) any { return &r.Body },
}

func TestJSONParamStaysPlain(t *testing.T) {
	got, err := Insert(codecProbe{TableBase: TableBase{SQLName: "samples"}}).
		Columns(codecBodyCol).
		Values(map[string]int{"a": 1}).
		SQL(Dialect{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got.SQL, "::") || !strings.Contains(got.SQL, "?") {
		t.Fatal(got.SQL)
	}
	if len(got.Params) != 1 || got.Params[0] != `{"a":1}` {
		t.Fatalf("%#v", got.Params)
	}
}
