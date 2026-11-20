package sql_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/xbmlz/drizzle-go/pg"
	"github.com/xbmlz/drizzle-go/sql"
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
	// 最后一行不足宽度时按宽度换行，渲染结果仍然合法。
	rows := &sql.ValueRows{Cells: []any{1, 2, 3}, Encoders: []sql.Encoder{sql.NoopEncoder{}, sql.NoopEncoder{}}}
	got := mustRender(t, sql.S(rows), sql.RenderConfig{})
	if want := `($1, $2), ($3)`; got.SQL != want {
		t.Fatalf("got %s want %s", got.SQL, want)
	}
}

func TestRenderValueRowsExpressions(t *testing.T) {
	// cells 里允许直接放 SQL 片段：对应插入 default / 表达式。
	rows := &sql.ValueRows{
		Cells:    []any{1, sql.Raw("default")},
		Encoders: []sql.Encoder{sql.NoopEncoder{}, sql.NoopEncoder{}},
	}
	got := mustRender(t, sql.S(rows), sql.RenderConfig{})
	if want := `($1, default)`; got.SQL != want {
		t.Fatalf("got %s want %s", got.SQL, want)
	}
}

func TestRenderValueRowsNoEncoders(t *testing.T) {
	_, err := sql.Render(sql.S(&sql.ValueRows{Cells: []any{1}}), pg.Dialect{}, sql.RenderConfig{})
	if err == nil || !strings.Contains(err.Error(), "no encoders") {
		t.Fatalf("expected encoder error, got %v", err)
	}
}

func TestRenderValueRowsGetSQL(t *testing.T) {
	// drizzle 的兼容壳：不认 ValueRows 的路径也能拿到等价 SQL。
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
	// []byte 是切片但语义上是一个值，不能被展开成列表。
	got := mustRender(t, sql.S([]byte{1, 2, 3}), sql.RenderConfig{})
	if want := `$1`; got.SQL != want {
		t.Fatalf("got %s want %s", got.SQL, want)
	}
	if len(got.Params) != 1 {
		t.Fatalf("expected 1 param, got %d", len(got.Params))
	}
}

func TestRenderLargeParamList(t *testing.T) {
	// 对应 drizzle Issue #5994：超大参数列表不能被截断。
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
