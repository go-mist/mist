package sql

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"github.com/xbmlz/mist/entity"
	"github.com/xbmlz/mist/sql/dialect"
)

// Chunk is one element of an SQL fragment stream.
//
// It corresponds to SQLChunk in drizzle-orm v1.0.0-rc.5. drizzle expresses it as a type union; Go uses an interface:
// any value that can produce SQL is a Chunk — the same contract as drizzle's SQLWrapper
// (isSQLWrapper checks typeof value.getSQL === 'function').
//
// One deliberate difference from drizzle: drizzle allows bare scalars in the chunk stream (wrapped as
// Param only at runtime), while Go's Chunk interface requires GetSQL, so S(...) wraps scalars as Param at construction time.
// The result is the same thing, but type problems surface earlier.
type Chunk interface {
	GetSQL() *SQL
}

// Wrapper is an alias of Chunk; drizzle calls this role SQLWrapper.
type Wrapper = Chunk

// Fragment is literal SQL text: emitted as-is, with no escaping, no quoting, and no parameter binding.
//
// It corresponds to drizzle's StringChunk. Most of the time you do not construct it directly; use Raw("...").
type Fragment string

// GetSQL implements Chunk.
func (f Fragment) GetSQL() *SQL { return &SQL{QueryChunks: []Chunk{f}} }

// Name is an identifier that must be escaped per dialect (table name, column name, alias, index name).
//
// It corresponds to drizzle's Name. The only difference from Fragment is:
// Name goes through Dialect.EscapeName; Fragment does not.
type Name string

// GetSQL implements Chunk.
func (n Name) GetSQL() *SQL { return &SQL{QueryChunks: []Chunk{n}} }

// Param is a parameter value waiting to be bound.
//
// A nil Encoder is equivalent to drizzle's noopEncoder: the value enters the parameter list as-is.
// Note that Param.Value may be *SQL (the whole fragment is expanded instead of bound),
// or *Placeholder (the placeholder itself enters the parameter list, to be filled later by FillPlaceholders).
type Param struct {
	Value   any
	Encoder Encoder
}

// GetSQL implements Chunk.
func (p *Param) GetSQL() *SQL { return &SQL{QueryChunks: []Chunk{p}} }

// Placeholder is a named placeholder, replaced with a real value by FillPlaceholders before execution.
//
// It corresponds to drizzle's Placeholder.
type Placeholder struct {
	Name string
}

// GetSQL implements Chunk.
func (p *Placeholder) GetSQL() *SQL { return &SQL{QueryChunks: []Chunk{p}} }

// Alias gives a piece of SQL a field name (`expr as "alias"` in a select list).
//
// It corresponds to drizzle's SQL.Aliased. Origin corresponds to drizzle's origin: when a table alias is present,
// it renders as origin.alias rather than a bare alias.
type Alias struct {
	SQL        *SQL
	FieldAlias string
	Origin     string
}

// GetSQL implements Chunk. Note that drizzle returns the aliased SQL itself here.
func (a *Alias) GetSQL() *SQL { return a.SQL }

// Subquery is a derived table or CTE.
//
// It corresponds to drizzle's Subquery. When IsWith is true only the alias is rendered (the CTE was
// already defined in the with clause); otherwise it renders as `(select ...) "alias"`.
type Subquery struct {
	SQL       *SQL
	AliasName string
	IsWith    bool
}

// GetSQL implements Chunk.
func (s *Subquery) GetSQL() *SQL { return s.SQL }

// ValueRows is the multi-row values of one insert: rendered as `(...), (...), (...)`.
//
// It corresponds to drizzle's ValueRows. Cells is a flat row-major list of values, and Encoders holds
// one encoder per column; the width is len(Encoders) and decides where to wrap.
//
// Difference from drizzle: drizzle's Cells is unknown[], and each element is dispatched by chunk
// type at render time; here Cells may hold Fragment / *SQL / *Placeholder directly.
// Placeholders stay in the parameter list and are filled by FillPlaceholders before execution.
type ValueRows struct {
	Cells    []any
	Encoders []Encoder
}

// GetSQL implements Chunk. Keeps drizzle's same-named compatibility shim: a builder that does not know ValueRows
// can still obtain equivalent SQL through it.
func (v *ValueRows) GetSQL() *SQL {
	chunks := make([]Chunk, 0, len(v.Cells)+2)
	for i, cell := range v.Cells {
		column := i % len(v.Encoders)
		switch column {
		case 0:
			if i == 0 {
				chunks = append(chunks, Fragment("("))
			} else {
				chunks = append(chunks, Fragment("), ("))
			}
		default:
			chunks = append(chunks, Fragment(", "))
		}
		chunks = append(chunks, valueRowsCell(cell, v.Encoders[column]))
	}
	chunks = append(chunks, Fragment(")"))
	return &SQL{QueryChunks: chunks}
}

// ShouldOmitSQLParens implements the optional omit-parentheses contract: ValueRows already includes parentheses.
func (v *ValueRows) ShouldOmitSQLParens() bool { return true }

// List is a group of fragments wrapped in parentheses and separated by commas: `(a, b, c)`.
//
// It corresponds to the Array.isArray(chunk) branch in the renderer. drizzle does not define a type for it;
// arrays take an inline branch. Go expresses it with a named type so reflection does not scatter through the renderer.
type List []Chunk

// GetSQL implements Chunk.
func (l List) GetSQL() *SQL { return &SQL{QueryChunks: l} }

// Encoder maps a Go value to a driver parameter.
//
// It corresponds to drizzle's DriverValueEncoder. drizzle decides whether conversion is unnecessary via the mapToDriverValue.isNoop
// property; Go expresses that with the IsNoop method. The semantics are the same:
// when IsNoop is true the renderer skips the mapping and saves a meaningless function call.
// A nil encoder is a pass-through as well.
type Encoder interface {
	MapToDriverValue(value any) any
	IsNoop() bool
}

// ParamCodec is a typed encoder. The renderer calls it when IsNoop is false:
// NormalizeParam rewrites the parameter value, and CastParam appends a suffix to the placeholder (for example $1::numeric).
// An empty string from CastParam means no suffix. dimensions is the array dimensionality; a scalar is 0.
type ParamCodec interface {
	NormalizeParam(value any, dimensions int) (any, error)
	CastParam(placeholder string, dimensions int) string
	Dimensions() int
}

// NoopEncoder passes values through unchanged, matching drizzle's noopEncoder.
type NoopEncoder struct{}

// MapToDriverValue returns the value unchanged.
func (NoopEncoder) MapToDriverValue(value any) any { return value }

// IsNoop is always true.
func (NoopEncoder) IsNoop() bool { return true }

// KindOf returns a value's role in the fragment stream, matching drizzle's chunkKindOf.
//
// Difference from drizzle: drizzle caches a numeric kind on the prototype; Go type-asserts directly —
// no string comparison, and no cache.
func KindOf(ch Chunk) entity.Kind {
	switch ch.(type) {
	case Fragment:
		return entity.String
	case *SQL:
		return entity.SQL
	case Name:
		return entity.Name
	case *Param:
		return entity.Param
	case *Placeholder:
		return entity.Placeholder
	case *Alias:
		return entity.Aliased
	case *Subquery:
		return entity.Subquery
	case *ValueRows:
		return entity.ValueRows
	default:
		return entity.Other
	}
}

// isNilChunk recognizes a nil pointer stored in an interface.
//
// A Go-specific pitfall: If(false) returns (*SQL)(nil), and once that is stored in a Chunk interface the interface itself is not nil.
// The renderer and And/Or must treat it as missing, equivalent to drizzle's undefined.
func isNilChunk(ch Chunk) bool {
	if ch == nil {
		return true
	}
	v := reflect.ValueOf(ch)
	return v.Kind() == reflect.Pointer && v.IsNil()
}

func valueRowsCell(cell any, enc Encoder) Chunk {
	switch x := cell.(type) {
	case Fragment:
		return x
	case *SQL:
		return x
	case *Placeholder:
		if enc != nil && !enc.IsNoop() {
			return &Param{Value: x, Encoder: enc}
		}
		return x
	default:
		return &Param{Value: cell, Encoder: enc}
	}
}

// FillPlaceholders fills placeholders in params with values and returns a new parameter list.
//
// Matches drizzle's fillPlaceholders: a direct placeholder is replaced wholesale; a placeholder wrapped in Param
// goes through the encoder before replacement. A missing placeholder is an error, not a silent nil.
func FillPlaceholders(params []any, values map[string]any) ([]any, error) {
	filled := make([]any, len(params))
	for i, p := range params {
		switch v := p.(type) {
		case *Placeholder:
			value, ok := values[v.Name]
			if !ok {
				return nil, fmt.Errorf("sql: no value for placeholder %q", v.Name)
			}
			filled[i] = value
		case *Param:
			ph, ok := v.Value.(*Placeholder)
			if !ok {
				filled[i] = p
				continue
			}
			value, ok := values[ph.Name]
			if !ok {
				return nil, fmt.Errorf("sql: no value for placeholder %q", ph.Name)
			}
			if value == nil {
				filled[i] = nil
				continue
			}
			if v.Encoder != nil && !v.Encoder.IsNoop() {
				if pc, ok := v.Encoder.(ParamCodec); ok {
					var err error
					value, err = pc.NormalizeParam(value, pc.Dimensions())
					if err != nil {
						return nil, err
					}
				} else {
					value = v.Encoder.MapToDriverValue(value)
				}
			}
			filled[i] = value
		default:
			filled[i] = p
		}
	}
	return filled, nil
}

// renderer is Render's internal state. Its fields correspond to the mutable accumulators in drizzle's collectSQL.
type renderer struct {
	d      dialect.Dialect
	cfg    RenderConfig
	buf    strings.Builder
	params []any
	next   int
	err    error
}

// Query is a render result: the final SQL text and the bound parameters.
type Query struct {
	SQL    string
	Params []any
}

// RenderConfig corresponds to drizzle's BuildQueryConfig, keeping only the fields Go uses.
//
// Two fields deliberately not ported (recorded in the ROADMAP difference log):
//   - tagged: drizzle uses it to produce a _sql fragment array for Bun.SQL; Go has no equivalent need;
//   - invokeSource: a dispatch switch specific to mssql / index metadata; M0 is pg only.
//
// A column with a nil encoder is a pass-through. A column that implements ParamCodec and is not a noop converts the parameter and appends a type suffix.
type RenderConfig struct {
	// ParamStart is the index of the first bound parameter (starting at 0). pg renders it as $1, $2, ...
	ParamStart int
	// InlineParams inlines parameters directly into the SQL text, for debug output and DDL.
	InlineParams bool
}

// ErrNilDialect means no dialect was provided at render time.
var ErrNilDialect = errors.New("sql: nil dialect")

// Render renders a piece of SQL into (SQL text, parameter list).
//
// It corresponds to drizzle's buildQueryFromSourceParams. The execution order matches drizzle exactly:
// recurse into nested SQL, dispatch by chunk type, and bind or inline parameters when needed.
//
// It returns an error rather than panicking: a value that cannot be formatted in inline mode (drizzle would throw)
// should be a propagatable error in Go.
func Render(s *SQL, d dialect.Dialect, cfg RenderConfig) (Query, error) {
	if d == nil {
		return Query{}, ErrNilDialect
	}
	if s == nil {
		return Query{}, nil
	}
	r := &renderer{d: d, cfg: cfg, next: cfg.ParamStart}
	r.collect(s.QueryChunks, cfg.InlineParams || s.shouldInlineParams)
	if r.err != nil {
		return Query{}, r.err
	}
	return Query{SQL: r.buf.String(), Params: r.params}, nil
}

// collect corresponds to drizzle's collectSQL.
func (r *renderer) collect(chunks []Chunk, inline bool) {
	for _, ch := range chunks {
		if r.err != nil {
			return
		}
		if isNilChunk(ch) {
			continue
		}
		switch v := ch.(type) {
		case Fragment:
			r.buf.WriteString(string(v))
		case *SQL:
			r.collect(v.QueryChunks, inline || v.shouldInlineParams)
		case Name:
			r.buf.WriteString(r.d.EscapeName(string(v)))
		case *Param:
			r.collectParam(v, inline)
		case *Placeholder:
			r.writePlaceholder(v)
		case *Alias:
			r.collectAlias(v, inline)
		case *Subquery:
			r.collectSubquery(v, inline)
		case *ValueRows:
			r.collectValueRows(v, inline)
		case List:
			r.collectList(v, inline)
		default:
			r.collectWrapper(ch, inline)
		}
	}
}

// collectParam corresponds to the chunkKinds.param branch.
func (r *renderer) collectParam(p *Param, inline bool) {
	// When the parameter value itself is SQL, expand it and do not bind (drizzle's valueKind === sql branch).
	if nested, ok := p.Value.(*SQL); ok && nested != nil {
		r.collect(nested.QueryChunks, inline || nested.shouldInlineParams)
		return
	}
	// When the parameter value is a placeholder, the placeholder itself enters the parameter list and is filled only before execution.
	// When the column has a codec, the type suffix is written into the SQL first and the value is converted at fill time.
	if ph, ok := p.Value.(*Placeholder); ok && ph != nil {
		escaped := r.d.EscapeParam(r.next, ph)
		if p.Encoder != nil && !p.Encoder.IsNoop() {
			if pc, ok := p.Encoder.(ParamCodec); ok {
				if cast := pc.CastParam(escaped, pc.Dimensions()); cast != "" {
					escaped = cast
				}
			}
			r.buf.WriteString(escaped)
			r.next++
			r.params = append(r.params, p)
			return
		}
		r.buf.WriteString(escaped)
		r.next++
		r.params = append(r.params, ph)
		return
	}
	r.collectBoundValue(p.Value, p.Encoder, inline)
}

func (r *renderer) writePlaceholder(ph *Placeholder) {
	r.buf.WriteString(r.d.EscapeParam(r.next, ph))
	r.next++
	r.params = append(r.params, ph)
}

// collectBoundValue corresponds to drizzle's collectBoundValue.
func (r *renderer) collectBoundValue(value any, enc Encoder, inline bool) {
	mapped := value
	var pc ParamCodec
	useCodec := value != nil && enc != nil && !enc.IsNoop()
	if useCodec {
		if cast, ok := enc.(ParamCodec); ok {
			pc = cast
			got, err := pc.NormalizeParam(value, pc.Dimensions())
			if err != nil {
				r.err = err
				return
			}
			mapped = got
		} else {
			mapped = enc.MapToDriverValue(value)
		}
		if nested, ok := mapped.(*SQL); ok && nested != nil {
			r.collect(nested.QueryChunks, inline || nested.shouldInlineParams)
			return
		}
	}
	if inline {
		r.writeInline(mapped)
		return
	}
	escaped := r.d.EscapeParam(r.next, mapped)
	if pc != nil {
		if cast := pc.CastParam(escaped, pc.Dimensions()); cast != "" {
			escaped = cast
		}
	}
	r.buf.WriteString(escaped)
	r.next++
	r.params = append(r.params, mapped)
}

func (r *renderer) collectAlias(a *Alias, inline bool) {
	if a.FieldAlias == "" {
		// drizzle falls back to the SQLWrapper branch when fieldAlias is undefined.
		r.collectWrapper(a, inline)
		return
	}
	if a.Origin != "" {
		r.buf.WriteString(r.d.EscapeName(a.Origin))
		r.buf.WriteByte('.')
	}
	r.buf.WriteString(r.d.EscapeName(a.FieldAlias))
}

func (r *renderer) collectSubquery(s *Subquery, inline bool) {
	if s.IsWith {
		r.buf.WriteString(r.d.EscapeName(s.AliasName))
		return
	}
	r.buf.WriteByte('(')
	if s.SQL != nil {
		r.collect(s.SQL.QueryChunks, inline || s.SQL.shouldInlineParams)
	}
	r.buf.WriteString(") ")
	r.buf.WriteString(r.d.EscapeName(s.AliasName))
}

func (r *renderer) collectValueRows(v *ValueRows, inline bool) {
	width := len(v.Encoders)
	if width == 0 {
		r.err = errors.New("sql: ValueRows with no encoders")
		return
	}
	r.buf.WriteByte('(')
	column := 0
	for i, cell := range v.Cells {
		if column == width {
			column = 0
			r.buf.WriteString("), (")
		} else if column > 0 {
			r.buf.WriteString(", ")
		}
		switch x := cell.(type) {
		case Fragment:
			r.buf.WriteString(string(x))
		case *SQL:
			r.collect(x.QueryChunks, inline || x.shouldInlineParams)
		case *Placeholder:
			r.writePlaceholder(x)
		default:
			r.collectBoundValue(cell, v.Encoders[column], inline)
		}
		column++
		_ = i
	}
	r.buf.WriteByte(')')
}

func (r *renderer) collectList(l List, inline bool) {
	r.buf.WriteByte('(')
	for i, ch := range l {
		if i > 0 {
			r.buf.WriteString(", ")
		}
		r.collect([]Chunk{ch}, inline)
	}
	r.buf.WriteByte(')')
}

// collectWrapper corresponds to drizzle's isSQLWrapper branch.
func (r *renderer) collectWrapper(ch Chunk, inline bool) {
	nested := ch.GetSQL()
	if nested == nil {
		return
	}
	innerInline := inline || nested.shouldInlineParams
	if o, ok := ch.(interface{ ShouldOmitSQLParens() bool }); ok && o.ShouldOmitSQLParens() {
		r.collect(nested.QueryChunks, innerInline)
		return
	}
	r.buf.WriteByte('(')
	r.collect(nested.QueryChunks, innerInline)
	r.buf.WriteByte(')')
}

// writeInline corresponds to drizzle's mapInlineParam.
//
// Difference from drizzle: JS toString() on a plain object goes through [object Object] and then JSON,
// while Go uses fmt.Stringer to detect a custom text representation and sends the rest through encoding/json.
func (r *renderer) writeInline(v any) {
	switch x := v.(type) {
	case nil:
		r.buf.WriteString("null")
	case bool:
		r.buf.WriteString(strconv.FormatBool(x))
	case int:
		r.buf.WriteString(strconv.FormatInt(int64(x), 10))
	case int8:
		r.buf.WriteString(strconv.FormatInt(int64(x), 10))
	case int16:
		r.buf.WriteString(strconv.FormatInt(int64(x), 10))
	case int32:
		r.buf.WriteString(strconv.FormatInt(int64(x), 10))
	case int64:
		r.buf.WriteString(strconv.FormatInt(x, 10))
	case uint:
		r.buf.WriteString(strconv.FormatUint(uint64(x), 10))
	case uint8:
		r.buf.WriteString(strconv.FormatUint(uint64(x), 10))
	case uint16:
		r.buf.WriteString(strconv.FormatUint(uint64(x), 10))
	case uint32:
		r.buf.WriteString(strconv.FormatUint(uint64(x), 10))
	case uint64:
		r.buf.WriteString(strconv.FormatUint(x, 10))
	case float32:
		r.buf.WriteString(strconv.FormatFloat(float64(x), 'g', -1, 32))
	case float64:
		r.buf.WriteString(strconv.FormatFloat(x, 'g', -1, 64))
	case string:
		r.buf.WriteString(r.d.EscapeString(x))
	default:
		if s, ok := v.(fmt.Stringer); ok {
			r.buf.WriteString(r.d.EscapeString(s.String()))
			return
		}
		encoded, err := json.Marshal(v)
		if err != nil {
			r.err = fmt.Errorf("sql: cannot inline param of type %T: %w", v, err)
			return
		}
		r.buf.WriteString(r.d.EscapeString(string(encoded)))
	}
}
