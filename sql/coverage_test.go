package sql_test

import (
	"testing"

	"github.com/xbmlz/mist/sql"
)

// This file covers interface methods on chunk types: most are called from inside the renderer,
// but Go coverage counts them only when they are invoked directly.

func TestChunkGetSQL(t *testing.T) {
	tests := []struct {
		name string
		ch   sql.Chunk
	}{
		{"fragment", sql.Fragment("x")},
		{"sql", sql.Raw("x")},
		{"name", sql.Identifier("x")},
		{"param", sql.NewParam(1, nil)},
		{"placeholder", sql.NewPlaceholder("p")},
		{"alias", sql.Raw("x").As("a")},
		{"subquery", sql.NewSubquery(sql.Raw("x"), "s")},
		{"list", sql.ListOf([]int{1})},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.ch.GetSQL(); got == nil {
				t.Fatalf("%s.GetSQL() returned nil", tt.name)
			}
		})
	}
}

func TestValueRowsInterface(t *testing.T) {
	rows := &sql.ValueRows{Cells: []any{1}, Encoders: []sql.Encoder{sql.NoopEncoder{}}}
	if !rows.ShouldOmitSQLParens() {
		t.Fatal("ValueRows must omit parentheses")
	}
	if got := rows.GetSQL(); got == nil {
		t.Fatal("ValueRows.GetSQL() returned nil")
	}
}

func TestNoopEncoder(t *testing.T) {
	if !(sql.NoopEncoder{}).IsNoop() {
		t.Fatal("NoopEncoder must report noop")
	}
	if got := (sql.NoopEncoder{}).MapToDriverValue("x"); got != "x" {
		t.Fatalf("NoopEncoder mapped value to %v", got)
	}
}

func TestInlineUnsignedAndNonFinite(t *testing.T) {
	tests := []struct {
		value any
		want  string
	}{
		{uint(7), "7"},
		{uint8(7), "7"},
		{uint16(7), "7"},
		{uint32(7), "7"},
		{uint64(7), "7"},
	}
	for _, tt := range tests {
		got := mustRender(t, sql.S(sql.NewParam(tt.value, nil)), sql.RenderConfig{InlineParams: true})
		if got.SQL != tt.want {
			t.Fatalf("inline %T(%v) = %s, want %s", tt.value, tt.value, got.SQL, tt.want)
		}
	}
}

// sqlEncoder makes the encoder return a piece of SQL, covering the "still SQL after encoding, so expand" branch.
type sqlEncoder struct{}

func (sqlEncoder) MapToDriverValue(any) any { return sql.Raw("now()") }
func (sqlEncoder) IsNoop() bool             { return false }

func TestEncoderReturningSQLExpands(t *testing.T) {
	got := mustRender(t, sql.S(sql.NewParam("ignored", sqlEncoder{})), sql.RenderConfig{})
	if want := "now()"; got.SQL != want {
		t.Fatalf("got %s want %s", got.SQL, want)
	}
	if len(got.Params) != 0 {
		t.Fatalf("expanded value must not bind params: %#v", got.Params)
	}
}

// nilWrapper's GetSQL returns nil, covering the nil guard on the wrapper branch.
type nilWrapper struct{}

func (nilWrapper) GetSQL() *sql.SQL { return nil }

func TestWrapperReturningNil(t *testing.T) {
	got := mustRender(t, sql.S(sql.Raw("a"), nilWrapper{}, sql.Raw("b")), sql.RenderConfig{})
	if want := "ab"; got.SQL != want {
		t.Fatalf("got %q want %q", got.SQL, want)
	}
}

func TestListVariants(t *testing.T) {
	// []Chunk forms a List directly, without going through reflection.
	chunks := []sql.Chunk{sql.Raw("a"), sql.NewParam(1, nil)}
	got := mustRender(t, sql.S(chunks), sql.RenderConfig{})
	if want := "(a, $1)"; got.SQL != want {
		t.Fatalf("got %s want %s", got.SQL, want)
	}

	// When a valueRows cell is inline SQL, the SQL branch is taken.
	rows := &sql.ValueRows{
		Cells:    []any{sql.NewParam(1, nil), sql.S(sql.Raw("x"), 2).InlineParams()},
		Encoders: []sql.Encoder{sql.NoopEncoder{}, sql.NoopEncoder{}},
	}
	got = mustRender(t, sql.S(rows), sql.RenderConfig{})
	if want := "($1, x2)"; got.SQL != want {
		t.Fatalf("got %s want %s", got.SQL, want)
	}
}
