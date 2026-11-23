package sql_test

import (
	"testing"

	"github.com/xbmlz/mist/sql"
)

// This file corresponds to drizzle-orm v1.0.0-rc.5's
// drizzle-orm/tests/sql-builder.test.ts.
//
// drizzle's cases build columns directly with pgTable; before M1, testColumn stands in,
// and it renders the exact same `"users"."id"` text as drizzle.
// Each case name matches upstream so they can be checked one by one.

// eqExpr hand-builds the fragment equivalent to drizzle's eq(users.id, <v>).
// M2's Eq returns the same chunk stream.
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
	// drizzle's and/or return undefined when there are no conditions; Go returns nil.
	if got := sql.And(); got != nil {
		t.Fatalf("And() = %#v, want nil", got)
	}
	if got := sql.Or(); got != nil {
		t.Fatalf("Or() = %#v, want nil", got)
	}
	if got := sql.Not(nil); got != nil {
		t.Fatalf("Not(nil) = %#v, want nil", got)
	}
	// When only nil conditions remain, the result is empty as well.
	if got := sql.And(nil, sql.S(sql.Raw("x")).If(false)); got != nil {
		t.Fatalf("And(all nil) = %#v, want nil", got)
	}
}

func TestSqlBuilderSingleCondition(t *testing.T) {
	// A single condition is wrapped as-is, with no parentheses added.
	query := sql.And(sql.Raw("a = 1"))
	got := mustRender(t, query, sql.RenderConfig{})
	if want := "a = 1"; got.SQL != want {
		t.Fatalf("got %s want %s", got.SQL, want)
	}
}

func TestSqlBuilderNotSQLWrapping(t *testing.T) {
	// A non-SQL wrapper takes its own render branch; without an omit-parentheses contract, the wrapper adds the parentheses.
	got := mustRender(t, sql.Not(quotedWrapper{inner: sql.Raw("x")}), sql.RenderConfig{})
	if want := "not (x)"; got.SQL != want {
		t.Fatalf("got %s want %s", got.SQL, want)
	}
}
