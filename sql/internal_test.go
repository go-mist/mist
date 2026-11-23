package sql

import (
	"testing"
)

// This file is white-box tests: it covers only the normalization logic external callers cannot see.
// Black-box behavior lives in sql_test / render_test / condition_test.

func TestToChunk(t *testing.T) {
	tests := []struct {
		name   string
		input  any
		want   bool // whether to keep
		assert func(t *testing.T, ch Chunk)
	}{
		{
			name:  "nil is dropped",
			input: nil,
			want:  false,
		},
		{
			name:  "chunk passes through",
			input: Fragment("x"),
			want:  true,
			assert: func(t *testing.T, ch Chunk) {
				if _, ok := ch.(Fragment); !ok {
					t.Fatalf("expected Fragment, got %T", ch)
				}
			},
		},
		{
			name:  "typed nil chunk is dropped",
			input: (*SQL)(nil),
			want:  false,
		},
		{
			name:  "scalar becomes param",
			input: 42,
			want:  true,
			assert: func(t *testing.T, ch Chunk) {
				p, ok := ch.(*Param)
				if !ok {
					t.Fatalf("expected *Param, got %T", ch)
				}
				if p.Value != 42 {
					t.Fatalf("param value = %v, want 42", p.Value)
				}
			},
		},
		{
			name:  "plain string becomes param",
			input: "select 1",
			want:  true,
			assert: func(t *testing.T, ch Chunk) {
				if _, ok := ch.(*Param); !ok {
					t.Fatalf("expected *Param, got %T", ch)
				}
			},
		},
		{
			name:  "typed slice stays one param",
			input: []string{"a", "b"},
			want:  true,
			assert: func(t *testing.T, ch Chunk) {
				p, ok := ch.(*Param)
				if !ok {
					t.Fatalf("expected *Param, got %T", ch)
				}
				vals, ok := p.Value.([]string)
				if !ok || len(vals) != 2 {
					t.Fatalf("expected []string param, got %#v", p.Value)
				}
			},
		},
		{
			name:  "byte slice stays scalar",
			input: []byte{1, 2},
			want:  true,
			assert: func(t *testing.T, ch Chunk) {
				if _, ok := ch.(*Param); !ok {
					t.Fatalf("expected *Param, got %T", ch)
				}
			},
		},
		{
			name:  "array stays one param",
			input: [2]int{1, 2},
			want:  true,
			assert: func(t *testing.T, ch Chunk) {
				if _, ok := ch.(*Param); !ok {
					t.Fatalf("expected *Param, got %T", ch)
				}
			},
		},
		{
			name:  "chunk list is preserved",
			input: List{Fragment("x")},
			want:  true,
			assert: func(t *testing.T, ch Chunk) {
				if l, ok := ch.(List); !ok || len(l) != 1 {
					t.Fatalf("expected List, got %T", ch)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ch, kept := toChunk(tt.input)
			if kept != tt.want {
				t.Fatalf("kept = %v, want %v", kept, tt.want)
			}
			if !kept {
				return
			}
			if tt.assert != nil {
				tt.assert(t, ch)
			}
		})
	}
}

func TestSliceToList(t *testing.T) {
	if _, ok := sliceToList(1); ok {
		t.Fatal("int is not a list")
	}
	if _, ok := sliceToList([]uint8{1}); ok {
		t.Fatal("[]uint8 must stay scalar")
	}

	if _, ok := sliceToList([]int{1, 2}); ok {
		t.Fatal("[]int must not expand; use ListOf")
	}

	// A nil element in a list is bound as a NULL parameter rather than discarded.
	l, ok := sliceToList([]any{1, nil})
	if !ok || len(l) != 2 {
		t.Fatalf("unexpected list shape: %#v", l)
	}
	p, ok := l[1].(*Param)
	if !ok || p.Value != nil {
		t.Fatalf("nil element must become a NULL param, got %#v", l[1])
	}
}

func TestIsNilChunk(t *testing.T) {
	if !isNilChunk(nil) {
		t.Fatal("nil interface must be nil")
	}
	if !isNilChunk((*SQL)(nil)) {
		t.Fatal("typed nil pointer must be nil")
	}
	if !isNilChunk((*Param)(nil)) {
		t.Fatal("typed nil param must be nil")
	}
	if isNilChunk(Fragment("x")) {
		t.Fatal("fragment is not nil")
	}
	if isNilChunk(List(nil)) {
		t.Fatal("nil List is a valid empty list")
	}
}

func TestValueRowsCell(t *testing.T) {
	enc := NoopEncoder{}
	if got := valueRowsCell(Fragment("default"), enc); got != Fragment("default") {
		t.Fatalf("fragment cell = %#v", got)
	}
	s := Raw("now()")
	if got := valueRowsCell(s, enc); got != s {
		t.Fatalf("sql cell = %#v", got)
	}
	p, ok := valueRowsCell(3, enc).(*Param)
	if !ok || p.Value != 3 || p.Encoder != enc {
		t.Fatalf("scalar cell = %#v", valueRowsCell(3, enc))
	}
}
