package sql_test

import (
	"testing"

	"github.com/go-mist/mist/pg"
	"github.com/go-mist/mist/sql"
)

// The benchmark measures only allocations and time for the build-to-render stretch.
// What matters is allocations per query, not absolute ns — the latter depends on the machine.

func benchQuery() *sql.SQL {
	return sql.S(
		sql.Raw("select "), sql.Identifier("id"), sql.Raw(", "), sql.Identifier("name"),
		sql.Raw(" from "), sql.Identifier("users"),
		sql.Raw(" where "), sql.Identifier("name"), sql.Raw(" = "), "alice",
		sql.Raw(" and "), sql.Identifier("id"), sql.Raw(" > "), 100,
		sql.Raw(" order by "), sql.Identifier("id"), sql.Raw(" desc limit "), 10,
	)
}

func BenchmarkRenderBind(b *testing.B) {
	query := benchQuery()
	d := pg.Dialect{}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := sql.Render(query, d, sql.RenderConfig{}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRenderInline(b *testing.B) {
	query := benchQuery()
	d := pg.Dialect{}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := sql.Render(query, d, sql.RenderConfig{InlineParams: true}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRenderNested(b *testing.B) {
	inner := sql.S(sql.Raw("select "), sql.Identifier("id"), sql.Raw(" from "), sql.Identifier("posts"))
	query := sql.S(
		sql.Raw("select * from "), sql.NewSubquery(inner, "p"),
		sql.Raw(" where exists "), sql.NewSubquery(sql.Raw("select 1"), "e"),
	)
	d := pg.Dialect{}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := sql.Render(query, d, sql.RenderConfig{}); err != nil {
			b.Fatal(err)
		}
	}
}
