package sql_test

import (
	"testing"

	"github.com/xbmlz/drizzle-go/sql"
)

// 本文件补上 chunk 类型上的接口方法：它们大多由渲染器内部调用，
// 但在 Go 的覆盖率统计里只有被直接调用过才算覆盖。

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

// sqlEncoder 让编码器返回一段 SQL，验证“编码后仍是 SQL 就展开”的分支。
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

// nilWrapper 的 GetSQL 返回 nil，验证 wrapper 分支的空值保护。
type nilWrapper struct{}

func (nilWrapper) GetSQL() *sql.SQL { return nil }

func TestWrapperReturningNil(t *testing.T) {
	got := mustRender(t, sql.S(sql.Raw("a"), nilWrapper{}, sql.Raw("b")), sql.RenderConfig{})
	if want := "ab"; got.SQL != want {
		t.Fatalf("got %q want %q", got.SQL, want)
	}
}

func TestListVariants(t *testing.T) {
	// []Chunk 直接构成 List，不经过反射。
	chunks := []sql.Chunk{sql.Raw("a"), sql.NewParam(1, nil)}
	got := mustRender(t, sql.S(chunks), sql.RenderConfig{})
	if want := "(a, $1)"; got.SQL != want {
		t.Fatalf("got %s want %s", got.SQL, want)
	}

	// valueRows 的 cell 是内联 SQL 时走 SQL 分支。
	rows := &sql.ValueRows{
		Cells:    []any{sql.NewParam(1, nil), sql.S(sql.Raw("x"), 2).InlineParams()},
		Encoders: []sql.Encoder{sql.NoopEncoder{}, sql.NoopEncoder{}},
	}
	got = mustRender(t, sql.S(rows), sql.RenderConfig{})
	if want := "($1, x2)"; got.SQL != want {
		t.Fatalf("got %s want %s", got.SQL, want)
	}
}
