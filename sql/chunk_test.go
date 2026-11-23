package sql_test

import (
	"reflect"
	"testing"

	"github.com/go-mist/mist/entity"
	"github.com/go-mist/mist/pg"
	"github.com/go-mist/mist/sql"
)

func TestKindOf(t *testing.T) {
	tests := []struct {
		name string
		ch   sql.Chunk
		want entity.Kind
	}{
		{"fragment", sql.Fragment("x"), entity.String},
		{"sql", sql.Raw("x"), entity.SQL},
		{"name", sql.Identifier("x"), entity.Name},
		{"param", sql.NewParam(1, nil), entity.Param},
		{"placeholder", sql.NewPlaceholder("p"), entity.Placeholder},
		{"alias", sql.Raw("x").As("a"), entity.Aliased},
		{"subquery", sql.NewSubquery(sql.Raw("x"), "s"), entity.Subquery},
		{"value rows", &sql.ValueRows{}, entity.ValueRows},
		{"list", sql.List{}, entity.Other},
		{"custom wrapper", testColumn("x"), entity.Other},
		{"nil", nil, entity.Other},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sql.KindOf(tt.ch); got != tt.want {
				t.Fatalf("KindOf() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFillPlaceholders(t *testing.T) {
	t.Run("direct placeholder", func(t *testing.T) {
		ph := sql.NewPlaceholder("id")
		params := []any{ph, "keep"}
		filled, err := sql.FillPlaceholders(params, map[string]any{"id": 7})
		if err != nil {
			t.Fatalf("FillPlaceholders() error: %v", err)
		}
		if want := []any{7, "keep"}; !reflect.DeepEqual(filled, want) {
			t.Fatalf("got %#v want %#v", filled, want)
		}
	})

	t.Run("param wrapped placeholder uses encoder", func(t *testing.T) {
		ph := sql.NewPlaceholder("name")
		params := []any{sql.NewParam(ph, upperEncoder{})}
		filled, err := sql.FillPlaceholders(params, map[string]any{"name": "alice"})
		if err != nil {
			t.Fatalf("FillPlaceholders() error: %v", err)
		}
		if want := []any{"ALICE"}; !reflect.DeepEqual(filled, want) {
			t.Fatalf("got %#v want %#v", filled, want)
		}
	})

	t.Run("nil value skips encoder", func(t *testing.T) {
		params := []any{sql.NewParam(sql.NewPlaceholder("name"), upperEncoder{})}
		filled, err := sql.FillPlaceholders(params, map[string]any{"name": nil})
		if err != nil {
			t.Fatalf("FillPlaceholders() error: %v", err)
		}
		if want := []any{nil}; !reflect.DeepEqual(filled, want) {
			t.Fatalf("got %#v want %#v", filled, want)
		}
	})

	t.Run("noop encoder passes through", func(t *testing.T) {
		params := []any{sql.NewParam(sql.NewPlaceholder("name"), sql.NoopEncoder{})}
		filled, err := sql.FillPlaceholders(params, map[string]any{"name": "alice"})
		if err != nil {
			t.Fatalf("FillPlaceholders() error: %v", err)
		}
		if want := []any{"alice"}; !reflect.DeepEqual(filled, want) {
			t.Fatalf("got %#v want %#v", filled, want)
		}
	})

	t.Run("plain param untouched", func(t *testing.T) {
		params := []any{sql.NewParam("x", nil), 3}
		filled, err := sql.FillPlaceholders(params, nil)
		if err != nil {
			t.Fatalf("FillPlaceholders() error: %v", err)
		}
		if want := params; !reflect.DeepEqual(filled, want) {
			t.Fatalf("got %#v want %#v", filled, want)
		}
	})

	t.Run("missing value is an error", func(t *testing.T) {
		_, err := sql.FillPlaceholders([]any{sql.NewPlaceholder("x")}, map[string]any{})
		if err == nil {
			t.Fatal("expected error for missing placeholder")
		}

		_, err = sql.FillPlaceholders([]any{sql.NewParam(sql.NewPlaceholder("y"), nil)}, nil)
		if err == nil {
			t.Fatal("expected error for missing param placeholder")
		}
	})
}

func TestFillPlaceholdersThenRender(t *testing.T) {
	// Full path: render yields placeholders, then fill them, then obtain executable parameters.
	query := sql.S(sql.Raw("select * from users where id = "), sql.NewPlaceholder("id"))
	rendered, err := sql.Render(query, pg.Dialect{}, sql.RenderConfig{})
	if err != nil {
		t.Fatalf("Render() error: %v", err)
	}
	if want := "select * from users where id = $1"; rendered.SQL != want {
		t.Fatalf("got %s want %s", rendered.SQL, want)
	}

	params, err := sql.FillPlaceholders(rendered.Params, map[string]any{"id": 42})
	if err != nil {
		t.Fatalf("FillPlaceholders() error: %v", err)
	}
	if want := []any{42}; !reflect.DeepEqual(params, want) {
		t.Fatalf("got %#v want %#v", params, want)
	}
}
