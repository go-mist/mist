package sql_test

import (
	"path/filepath"
	"testing"

	"github.com/xbmlz/drizzle-go/internal/testutil"
	"github.com/xbmlz/drizzle-go/pg"
	"github.com/xbmlz/drizzle-go/sql"
)

// golden 用例与 drizzle 对拍：fixture 只描述期望 (sql, params)，
// 由 drizzle 与 drizzle-go 两侧分别产出后逐字节比较。
// 当前 fixture 的期望值来自 drizzle-orm v1.0.0-rc.5 的行为。

func TestGoldenSubquery(t *testing.T) {
	fixture := testutil.LoadFixture(t, filepath.Join("testdata", "subquery.json"))

	query := sql.S(
		sql.Raw("select "), sql.Identifier("id"), sql.Raw(" from "),
		sql.NewSubquery(sql.Raw("select 1"), "sq"),
	)

	got, err := (pg.Dialect{}).SQLToQuery(query)
	if err != nil {
		t.Fatalf("SQLToQuery() error: %v", err)
	}
	testutil.AssertQuery(t, got, fixture)
}

func TestGoldenValueRows(t *testing.T) {
	fixture := testutil.LoadFixture(t, filepath.Join("testdata", "value_rows.json"))

	rows := &sql.ValueRows{
		Cells:    []any{1, "a", 2, "b"},
		Encoders: []sql.Encoder{sql.NoopEncoder{}, sql.NoopEncoder{}},
	}
	got, err := (pg.Dialect{}).SQLToQuery(sql.S(sql.Raw("values "), rows))
	if err != nil {
		t.Fatalf("SQLToQuery() error: %v", err)
	}
	testutil.AssertQuery(t, got, fixture)
}
