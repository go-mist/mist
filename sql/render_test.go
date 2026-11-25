package sql_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/go-mist/mist/dialect/pg"
	"github.com/go-mist/mist/sql"
)

// testColumn simulates a column with a dedicated render branch and an omit-parentheses contract.
// M1's real columns will replace it; here it lets the renderer and condition combinators be tested on their own.
type testColumn string

// GetSQL implements sql.Chunk.
func (c testColumn) GetSQL() *sql.SQL { return &sql.SQL{QueryChunks: []sql.Chunk{sql.Fragment(c)}} }

// ShouldOmitSQLParens means this chunk already has its own boundary, so the wrapper branch must not add parentheses.
func (c testColumn) ShouldOmitSQLParens() bool { return true }

// quotedWrapper is an ordinary wrapper: it has no omit-parentheses contract, so rendering wraps it in parentheses.
type quotedWrapper struct{ inner *sql.SQL }

// GetSQL implements sql.Chunk.
func (w quotedWrapper) GetSQL() *sql.SQL { return w.inner }

// upperEncoder demonstrates a non-noop encoder.
type upperEncoder struct{}

// MapToDriverValue uppercases strings and leaves everything else unchanged.
func (upperEncoder) MapToDriverValue(v any) any {
	if s, ok := v.(string); ok {
		return strings.ToUpper(s)
	}
	return v
}

// IsNoop returns false, so the renderer calls MapToDriverValue.
func (upperEncoder) IsNoop() bool { return false }

type customStringer struct{}

func (customStringer) String() string { return "custom" }

func mustRender(t *testing.T, query *sql.SQL, cfg sql.RenderConfig) sql.Query {
	t.Helper()
	q, err := sql.Render(query, pg.Dialect{}, cfg)
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}
	return q
}

func TestRenderBindings(t *testing.T) {
	query := sql.S(
		sql.Raw("select "), sql.Identifier("name"),
		sql.Raw(" from "), sql.Identifier("users"),
		sql.Raw(" where "), sql.Identifier("age"), sql.Raw(" > "), 42,
		sql.Raw(" and "), sql.Identifier("name"), sql.Raw(" = "), "alice",
	)

	got := mustRender(t, query, sql.RenderConfig{})

	const want = `select "name" from "users" where "age" > $1 and "name" = $2`
	if got.SQL != want {
		t.Fatalf("SQL mismatch: got %s want %s", got.SQL, want)
	}
	if wantParams := []any{42, "alice"}; !reflect.DeepEqual(got.Params, wantParams) {
		t.Fatalf("params mismatch: got %#v want %#v", got.Params, wantParams)
	}
}

func TestRenderParamStart(t *testing.T) {
	query := sql.S(sql.Raw("select "), "a", sql.Raw(", "), "b")
	got := mustRender(t, query, sql.RenderConfig{ParamStart: 2})

	const want = `select $3, $4`
	if got.SQL != want {
		t.Fatalf("SQL mismatch: got %s want %s", got.SQL, want)
	}
	if len(got.Params) != 2 {
		t.Fatalf("expected 2 params, got %d", len(got.Params))
	}
}

func TestRenderInlineValues(t *testing.T) {
	tests := []struct {
		name  string
		value any
		want  string
	}{
		{"nil", nil, "null"},
		{"true", true, "true"},
		{"false", false, "false"},
		{"int", 42, "42"},
		{"negative", -7, "-7"},
		{"int64", int64(9007199254740993), "9007199254740993"},
		{"float", 1.5, "1.5"},
		{"float32", float32(1.25), "1.25"},
		{"string", "alice", "'alice'"},
		{"string with quote", "O'Reilly", "'O''Reilly'"},
		{"stringer", customStringer{}, "'custom'"},
		{"object", map[string]int{"a": 1}, `'{"a":1}'`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			query := sql.S(sql.NewParam(tt.value, nil))
			got := mustRender(t, query, sql.RenderConfig{InlineParams: true})
			if got.SQL != tt.want {
				t.Fatalf("got %q want %q", got.SQL, tt.want)
			}
			if len(got.Params) != 0 {
				t.Fatalf("inline mode must not collect params, got %#v", got.Params)
			}
		})
	}
}

func TestRenderInlineError(t *testing.T) {
	query := sql.S(sql.NewParam(func() {}, nil))
	_, err := sql.Render(query, pg.Dialect{}, sql.RenderConfig{InlineParams: true})
	if err == nil || !strings.Contains(err.Error(), "cannot inline") {
		t.Fatalf("expected inline error, got %v", err)
	}
}

func TestRenderInlinePropagation(t *testing.T) {
	// Inline mode is passed in from the outside: parameters inside nested SQL and List are inlined too.
	inner := sql.S(sql.Raw("("), "x", sql.Raw(")"))
	query := sql.S(sql.Raw("select "), inner, sql.Raw(", "), sql.ListOf([]int{1, 2}))

	got := mustRender(t, query, sql.RenderConfig{InlineParams: true})
	if want := `select ('x'), (1, 2)`; got.SQL != want {
		t.Fatalf("SQL mismatch: got %s want %s", got.SQL, want)
	}

	// SQL.InlineParams() turns inline mode on by itself; no config is required.
	inlineInner := sql.S(sql.Raw("x"), 1).InlineParams()
	outer := sql.S(sql.Raw("limit "), inlineInner)
	got = mustRender(t, outer, sql.RenderConfig{})
	if want := "limit x1"; got.SQL != want {
		t.Fatalf("SQL mismatch: got %s want %s", got.SQL, want)
	}
}

func TestRenderAlias(t *testing.T) {
	t.Run("plain alias", func(t *testing.T) {
		got := mustRender(t, sql.S(sql.Raw("count(*)").As("total")), sql.RenderConfig{})
		if want := `"total"`; got.SQL != want {
			t.Fatalf("got %s want %s", got.SQL, want)
		}
	})

	t.Run("alias with origin", func(t *testing.T) {
		alias := sql.Raw("count(*)").As("total")
		alias.Origin = "t"
		got := mustRender(t, sql.S(alias), sql.RenderConfig{})
		if want := `"t"."total"`; got.SQL != want {
			t.Fatalf("got %s want %s", got.SQL, want)
		}
	})

	t.Run("empty alias falls back to wrapper", func(t *testing.T) {
		got := mustRender(t, sql.S(&sql.Alias{SQL: sql.Raw("x")}), sql.RenderConfig{})
		if want := "(x)"; got.SQL != want {
			t.Fatalf("got %s want %s", got.SQL, want)
		}
	})
}

func TestRenderSubquery(t *testing.T) {
	got := mustRender(t, sql.S(sql.NewSubquery(sql.Raw("select 1"), "sq")), sql.RenderConfig{})
	if want := `(select 1) "sq"`; got.SQL != want {
		t.Fatalf("got %s want %s", got.SQL, want)
	}

	got = mustRender(t, sql.S(&sql.Subquery{SQL: sql.Raw("select 1"), AliasName: "sq", IsWith: true}), sql.RenderConfig{})
	if want := `"sq"`; got.SQL != want {
		t.Fatalf("got %s want %s", got.SQL, want)
	}

	got = mustRender(t, sql.S(&sql.Subquery{AliasName: "sq"}), sql.RenderConfig{})
	if want := `() "sq"`; got.SQL != want {
		t.Fatalf("got %s want %s", got.SQL, want)
	}
}

func TestRenderWrapperParens(t *testing.T) {
	got := mustRender(t, sql.S(quotedWrapper{inner: sql.Raw("x")}), sql.RenderConfig{})
	if want := "(x)"; got.SQL != want {
		t.Fatalf("got %s want %s", got.SQL, want)
	}

	got = mustRender(t, sql.S(testColumn(`"users"."id"`)), sql.RenderConfig{})
	if want := `"users"."id"`; got.SQL != want {
		t.Fatalf("got %s want %s", got.SQL, want)
	}
}

func TestRenderSkipsNils(t *testing.T) {
	// Both nil and a nil pointer stored in an interface must be skipped, equivalent to drizzle's undefined.
	got := mustRender(t, sql.S(sql.Raw("a"), nil, sql.S(sql.Raw("b")).If(false), sql.Raw("c")), sql.RenderConfig{})
	if want := "ac"; got.SQL != want {
		t.Fatalf("got %q want %q", got.SQL, want)
	}
}

func TestRenderEmpty(t *testing.T) {
	got := mustRender(t, nil, sql.RenderConfig{})
	if got.SQL != "" || got.Params != nil {
		t.Fatalf("got %#v, want zero value", got)
	}

	got = mustRender(t, sql.Empty(), sql.RenderConfig{})
	if got.SQL != "" || got.Params != nil {
		t.Fatalf("got %#v, want zero value", got)
	}
}

func TestRenderNilDialect(t *testing.T) {
	_, err := sql.Render(sql.Raw("x"), nil, sql.RenderConfig{})
	if !errors.Is(err, sql.ErrNilDialect) {
		t.Fatalf("expected ErrNilDialect, got %v", err)
	}
}

func TestRenderEncoder(t *testing.T) {
	got := mustRender(t, sql.S(sql.NewParam("alice", upperEncoder{})), sql.RenderConfig{})
	if want := []any{"ALICE"}; !reflect.DeepEqual(got.Params, want) {
		t.Fatalf("params mismatch: got %#v want %#v", got.Params, want)
	}

	got = mustRender(t, sql.S(sql.NewParam("alice", upperEncoder{})), sql.RenderConfig{InlineParams: true})
	if want := "'ALICE'"; got.SQL != want {
		t.Fatalf("got %s want %s", got.SQL, want)
	}
}

func TestRenderParamValueVariants(t *testing.T) {
	t.Run("sql value is expanded", func(t *testing.T) {
		got := mustRender(t, sql.S(sql.Raw("limit "), sql.NewParam(sql.Raw("1"), nil)), sql.RenderConfig{})
		if want := "limit 1"; got.SQL != want {
			t.Fatalf("got %s want %s", got.SQL, want)
		}
		if len(got.Params) != 0 {
			t.Fatalf("expanded SQL must not bind params, got %#v", got.Params)
		}
	})

	t.Run("placeholder value binds placeholder", func(t *testing.T) {
		ph := sql.NewPlaceholder("limit")
		got := mustRender(t, sql.S(sql.Raw("limit "), sql.NewParam(ph, nil)), sql.RenderConfig{})
		if want := "limit $1"; got.SQL != want {
			t.Fatalf("got %s want %s", got.SQL, want)
		}
		if len(got.Params) != 1 || got.Params[0] != ph {
			t.Fatalf("expected placeholder itself in params, got %#v", got.Params)
		}
	})
}
