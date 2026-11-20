package sql_test

import (
	"testing"

	"github.com/xbmlz/drizzle-go/sql"
)

// 本文件对应 drizzle-orm v1.0.0-rc.5 的
// drizzle-orm/tests/sql-builder.test.ts。
//
// drizzle 的用例里直接用 pgTable 构造列；M1 之前用 testColumn 代替，
// 它渲染出与 drizzle 完全相同的 `"users"."id"` 文本。
// 每个用例名保持与上游一致，便于逐条对账。

// eqExpr 手工构造 drizzle 里 eq(users.id, <v>) 的等价片段。
// M2 的 Eq 会返回同样的 chunk 流。
func eqExpr(value any) *sql.SQL {
	return sql.S(testColumn(`"users"."id"`), sql.Fragment(" = "), value)
}

func TestSqlBuilderORConditionsWrappedInParentheses(t *testing.T) {
	query := sql.Or(
		eqExpr(1),
		sql.S(sql.Fragment("never")).If(false),
		sql.Raw("users.id = post.user_id AND posts.is_deleted = false"),
	)
	query.InlineParams()

	got := mustRender(t, query, sql.RenderConfig{})
	const want = `(("users"."id" = 1) or (users.id = post.user_id AND posts.is_deleted = false))`
	if got.SQL != want {
		t.Fatalf("got %s want %s", got.SQL, want)
	}
}

func TestSqlBuilderANDConditionsWrappedInParentheses(t *testing.T) {
	query := sql.And(
		eqExpr(1),
		sql.S(sql.Fragment("never")).If(false),
		sql.Raw("users.id = post.user_id AND posts.is_deleted = false"),
	)
	query.InlineParams()

	got := mustRender(t, query, sql.RenderConfig{})
	const want = `(("users"."id" = 1) and (users.id = post.user_id AND posts.is_deleted = false))`
	if got.SQL != want {
		t.Fatalf("got %s want %s", got.SQL, want)
	}
}

func TestSqlBuilderNOTConditionsWrappedInParenthesesForSQL(t *testing.T) {
	query := sql.Not(sql.Raw("users.id = post.user_id AND posts.is_deleted = false"))
	query.InlineParams()

	got := mustRender(t, query, sql.RenderConfig{})
	const want = `not (users.id = post.user_id AND posts.is_deleted = false)`
	if got.SQL != want {
		t.Fatalf("got %s want %s", got.SQL, want)
	}
}

func TestSqlBuilderNOTConditionsNotWrappedForNonSQL(t *testing.T) {
	query := sql.Not(testColumn(`"users"."id"`))

	got := mustRender(t, query, sql.RenderConfig{})
	const want = `not "users"."id"`
	if got.SQL != want {
		t.Fatalf("got %s want %s", got.SQL, want)
	}
}

func TestSqlBuilderEmptyConditions(t *testing.T) {
	// drizzle 的 and/or 没有条件时返回 undefined；Go 返回 nil。
	if got := sql.And(); got != nil {
		t.Fatalf("And() = %#v, want nil", got)
	}
	if got := sql.Or(); got != nil {
		t.Fatalf("Or() = %#v, want nil", got)
	}
	if got := sql.Not(nil); got != nil {
		t.Fatalf("Not(nil) = %#v, want nil", got)
	}
	// 只剩 nil 条件时同样为空。
	if got := sql.And(nil, sql.S(sql.Raw("x")).If(false)); got != nil {
		t.Fatalf("And(all nil) = %#v, want nil", got)
	}
}

func TestSqlBuilderSingleCondition(t *testing.T) {
	// 单个条件原样包一层，不添加任何括号。
	query := sql.And(sql.Raw("a = 1"))
	got := mustRender(t, query, sql.RenderConfig{})
	if want := "a = 1"; got.SQL != want {
		t.Fatalf("got %s want %s", got.SQL, want)
	}
}

func TestSqlBuilderNotSQLWrapping(t *testing.T) {
	// 非 SQL 的 wrapper 走自己的渲染分支；没有省略括号契约时交给 wrapper 加括号。
	got := mustRender(t, sql.Not(quotedWrapper{inner: sql.Raw("x")}), sql.RenderConfig{})
	if want := "not (x)"; got.SQL != want {
		t.Fatalf("got %s want %s", got.SQL, want)
	}
}
