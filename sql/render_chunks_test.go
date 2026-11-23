package sql_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/go-mist/mist/pg"
	"github.com/go-mist/mist/sql"
)

func TestRenderValueRows(t *testing.T) {
	rows := &sql.ValueRows{
		Cells:    []any{1, "a", 2, "b"},
		Encoders: []sql.Encoder{sql.NoopEncoder{}, sql.NoopEncoder{}},
	}

	got := mustRender(t, sql.S(sql.Raw("values "), rows), sql.RenderConfig{})
	if want := `values ($1, $2), ($3, $4)`; got.SQL != want {
		t.Fatalf("got %s want %s", got.SQL, want)
	}
	if want := []any{1, "a", 2, "b"}; !reflect.DeepEqual(got.Params, want) {
		t.Fatalf("params mismatch: got %#v want %#v", got.Params, want)
	}
}

func TestRenderValueRowsRagged(t *testing.T) {
	// When the last row is shorter than the width, wrap by width; the rendered result is still valid.
	rows := &sql.ValueRows{Cells: []any{1, 2, 3}, Encoders: []sql.Encoder{sql.NoopEncoder{}, sql.NoopEncoder{}}}
	got := mustRender(t, sql.S(rows), sql.RenderConfig{})
	if want := `($1, $2), ($3)`; got.SQL != want {
		t.Fatalf("got %s want %s", got.SQL, want)
	}
}

func TestRenderValueRowsExpressions(t *testing.T) {
	// Cells may hold SQL fragments directly: this covers insert default / expressions.
	rows := &sql.ValueRows{
		Cells:    []any{1, sql.Raw("default")},
		Encoders: []sql.Encoder{sql.NoopEncoder{}, sql.NoopEncoder{}},
	}
	got := mustRender(t, sql.S(rows), sql.RenderConfig{})
	if want := `($1, default)`; got.SQL != want {
		t.Fatalf("got %s want %s", got.SQL, want)
	}
}

func TestRenderValueRowsPlaceholder(t *testing.T) {
	ph := sql.NewPlaceholder("name")
	rows := &sql.ValueRows{
		Cells:    []any{ph},
		Encoders: []sql.Encoder{sql.NoopEncoder{}},
	}
	got := mustRender(t, sql.S(rows), sql.RenderConfig{})
	if want := `($1)`; got.SQL != want {
		t.Fatalf("got %s want %s", got.SQL, want)
	}
	if len(got.Params) != 1 || got.Params[0] != ph {
		t.Fatalf("params = %#v", got.Params)
	}
}

func TestRenderValueRowsNoEncoders(t *testing.T) {
	_, err := sql.Render(sql.S(&sql.ValueRows{Cells: []any{1}}), pg.Dialect{}, sql.RenderConfig{})
	if err == nil || !strings.Contains(err.Error(), "no encoders") {
		t.Fatalf("expected encoder error, got %v", err)
	}
}

func TestRenderValueRowsGetSQL(t *testing.T) {
	// drizzle compatibility shim: a path that does not know ValueRows still gets equivalent SQL.
	rows := &sql.ValueRows{Cells: []any{1, 2, 3}, Encoders: []sql.Encoder{sql.NoopEncoder{}, sql.NoopEncoder{}}}
	got := mustRender(t, rows.GetSQL(), sql.RenderConfig{})
	if want := `($1, $2), ($3)`; got.SQL != want {
		t.Fatalf("got %s want %s", got.SQL, want)
	}
}

func TestRenderList(t *testing.T) {
	t.Run("uniform list", func(t *testing.T) {
		got := mustRender(t, sql.S(sql.ListOf([]int{1, 2, 3})), sql.RenderConfig{})
		if want := `($1, $2, $3)`; got.SQL != want {
			t.Fatalf("got %s want %s", got.SQL, want)
		}
	})

	t.Run("empty list", func(t *testing.T) {
		got := mustRender(t, sql.S(sql.List{}), sql.RenderConfig{})
		if want := `()`; got.SQL != want {
			t.Fatalf("got %s want %s", got.SQL, want)
		}
	})

	t.Run("nil element keeps separators", func(t *testing.T) {
		got := mustRender(t, sql.S(sql.List{sql.NewParam(1, nil), nil, sql.NewParam(3, nil)}), sql.RenderConfig{})
		if want := `($1, , $2)`; got.SQL != want {
			t.Fatalf("got %s want %s", got.SQL, want)
		}
	})

	t.Run("nested list", func(t *testing.T) {
		got := mustRender(t, sql.S(sql.List{sql.ListOf([]int{1, 2})}), sql.RenderConfig{})
		if want := `(($1, $2))`; got.SQL != want {
			t.Fatalf("got %s want %s", got.SQL, want)
		}
	})
}

func TestRenderByteSliceIsScalar(t *testing.T) {
	// []byte is a slice but semantically one value, and must not be expanded into a list.
	got := mustRender(t, sql.S([]byte{1, 2, 3}), sql.RenderConfig{})
	if want := `$1`; got.SQL != want {
		t.Fatalf("got %s want %s", got.SQL, want)
	}
	if len(got.Params) != 1 {
		t.Fatalf("expected 1 param, got %d", len(got.Params))
	}
}

func TestRenderLargeParamList(t *testing.T) {
	// Corresponds to drizzle Issue #5994: a very large parameter list must not be truncated.
	const count = 150000
	values := make([]int, count)
	for i := range values {
		values[i] = i
	}

	got := mustRender(t, sql.S(sql.ListOf(values)), sql.RenderConfig{})
	if len(got.Params) != count {
		t.Fatalf("expected %d params, got %d", count, len(got.Params))
	}
	for i, p := range got.Params {
		if p != i {
			t.Fatalf("param %d = %v, want %d", i, p, i)
		}
	}
	if !strings.HasPrefix(got.SQL, "($1, $2,") || !strings.HasSuffix(got.SQL, "$150000)") {
		t.Fatalf("unexpected SQL shape: %.40s ... %.10s", got.SQL, got.SQL)
	}
}
