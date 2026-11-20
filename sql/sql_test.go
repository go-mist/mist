package sql_test

import (
	"testing"

	"github.com/xbmlz/drizzle-go/sql"
)

func TestSChunkNormalization(t *testing.T) {
	got := mustRender(t, sql.S(
		sql.Raw("a"),                  // 片段原样
		"literal",                     // 裸字符串按参数绑定（安全默认值）
		nil,                           // 缺失，跳过
		sql.S(sql.Raw("b")).If(false), // nil 指针，跳过
		[]int{1, 2},                   // 具体切片整体绑定，不展开
		[]byte{7, 8},                  // []byte 不展开
	), sql.RenderConfig{})

	const want = `a$1$2$3`
	if got.SQL != want {
		t.Fatalf("got %s want %s", got.SQL, want)
	}
	if len(got.Params) != 3 {
		t.Fatalf("expected 3 params, got %d: %#v", len(got.Params), got.Params)
	}
	nums, ok := got.Params[1].([]int)
	if !ok || len(nums) != 2 || nums[0] != 1 || nums[1] != 2 {
		t.Fatalf("[]int must stay one param, got %#v", got.Params[1])
	}
}

func TestSDoesNotTreatStringsAsLiterals(t *testing.T) {
	// 这是本库最重要的安全默认值：插值即绑定。
	// 要把字符串写进 SQL 文本，必须显式 Raw/Fragment。
	got := mustRender(t, sql.S("select * from users"), sql.RenderConfig{})
	if want := "$1"; got.SQL != want {
		t.Fatalf("got %s want %s", got.SQL, want)
	}
	if len(got.Params) != 1 || got.Params[0] != "select * from users" {
		t.Fatalf("expected the whole string bound as one param, got %#v", got.Params)
	}
}

func TestRawAndEmpty(t *testing.T) {
	got := mustRender(t, sql.Raw("select 1"), sql.RenderConfig{})
	if want := "select 1"; got.SQL != want || got.Params != nil {
		t.Fatalf("got %#v want SQL=%q params=nil", got, want)
	}

	// 空片段只贡献空字符串，不绑定参数。
	got = mustRender(t, sql.Raw(""), sql.RenderConfig{})
	if got.SQL != "" || got.Params != nil {
		t.Fatalf("got %#v want empty", got)
	}
}

func TestJoin(t *testing.T) {
	parts := []sql.Chunk{sql.Raw("a"), sql.Raw("b"), sql.Raw("c")}

	got := mustRender(t, sql.Join(parts, sql.Raw(", ")), sql.RenderConfig{})
	if want := "a, b, c"; got.SQL != want {
		t.Fatalf("got %s want %s", got.SQL, want)
	}

	got = mustRender(t, sql.Join(parts, nil), sql.RenderConfig{})
	if want := "abc"; got.SQL != want {
		t.Fatalf("got %s want %s", got.SQL, want)
	}

	got = mustRender(t, sql.Join(nil, sql.Raw(", ")), sql.RenderConfig{})
	if want := ""; got.SQL != want {
		t.Fatalf("got %q want empty", got.SQL)
	}
}

func TestJoinDoesNotAliasInput(t *testing.T) {
	parts := []sql.Chunk{sql.Raw("a"), sql.Raw("b")}
	query := sql.Join(parts, sql.Raw("|"))

	got := mustRender(t, query, sql.RenderConfig{})
	if want := "a|b"; got.SQL != want {
		t.Fatalf("got %s want %s", got.SQL, want)
	}

	// 修改原切片不应影响已构造的片段。
	parts[0] = sql.Raw("changed")
	got = mustRender(t, query, sql.RenderConfig{})
	if want := "a|b"; got.SQL != want {
		t.Fatalf("Join must copy its input, got %s want %s", got.SQL, want)
	}
}

func TestAppend(t *testing.T) {
	query := sql.Raw("select ")
	query.Append(sql.S(sql.Identifier("id"), sql.Raw(" from "))).Append(sql.S(sql.Identifier("users")))

	got := mustRender(t, query, sql.RenderConfig{})
	if want := `select "id" from "users"`; got.SQL != want {
		t.Fatalf("got %s want %s", got.SQL, want)
	}

	// Append(nil) 是空操作。
	query.Append(nil)
	got = mustRender(t, query, sql.RenderConfig{})
	if want := `select "id" from "users"`; got.SQL != want {
		t.Fatalf("got %s want %s", got.SQL, want)
	}
}

func TestIfAndAs(t *testing.T) {
	query := sql.S(sql.Raw("limit "), 1)

	if got := query.If(false); got != nil {
		t.Fatalf("If(false) = %#v, want nil", got)
	}
	if got := query.If(true); got != query {
		t.Fatalf("If(true) must return the receiver")
	}

	alias := query.As("limit_clause")
	if alias.FieldAlias != "limit_clause" || alias.SQL != query {
		t.Fatalf("As() wired incorrectly: %#v", alias)
	}

	got := mustRender(t, sql.S(alias), sql.RenderConfig{})
	if want := `"limit_clause"`; got.SQL != want {
		t.Fatalf("got %s want %s", got.SQL, want)
	}
}

func TestListOf(t *testing.T) {
	strings := sql.ListOf([]string{"a", "b"})
	got := mustRender(t, sql.S(strings), sql.RenderConfig{})
	if want := "($1, $2)"; got.SQL != want {
		t.Fatalf("got %s want %s", got.SQL, want)
	}
	if len(got.Params) != 2 || got.Params[0] != "a" || got.Params[1] != "b" {
		t.Fatalf("params mismatch: %#v", got.Params)
	}

	// 元素本身是 chunk 时保持 chunk 语义。
	chunks := sql.ListOf([]any{sql.Raw("default"), 1})
	got = mustRender(t, sql.S(chunks), sql.RenderConfig{})
	if want := `(default, $1)`; got.SQL != want {
		t.Fatalf("got %s want %s", got.SQL, want)
	}
}
