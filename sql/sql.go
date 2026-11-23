package sql

// SQL is an SQL fragment waiting to be rendered, made of a sequence of Chunks.
//
// It corresponds to drizzle's SQL class. drizzle's `sql` tagged template has no equivalent in Go
// (Go has no template strings), so S(...) takes on the same job: literals, fragments, and
// parameters are assembled in order into a sequence of chunks.
//
// Important difference: Go's S(...) treats a bare string as a **bound parameter**, not a literal —
// a safe default that matches the drizzle tag function's "interpolation is binding" guarantee.
// Literals must be written explicitly with Raw("...") or Fragment("...").
type SQL struct {
	QueryChunks []Chunk

	shouldInlineParams bool
}

// GetSQL implements Chunk. SQL is its own wrapper.
func (s *SQL) GetSQL() *SQL { return s }

// Append appends another SQL's chunks to the end and returns the receiver.
// Matches drizzle's append: mutation is in place.
func (s *SQL) Append(q *SQL) *SQL {
	if q == nil {
		return s
	}
	s.QueryChunks = append(s.QueryChunks, q.QueryChunks...)
	return s
}

// InlineParams turns on inline-parameter mode: parameters are written into the SQL text instead of being bound.
func (s *SQL) InlineParams() *SQL {
	s.shouldInlineParams = true
	return s
}

// ParamsInlined reports whether parameters are written into the SQL text.
func (s *SQL) ParamsInlined() bool { return s != nil && s.shouldInlineParams }

// If returns the receiver when cond is true, otherwise (*SQL)(nil).
// Matches drizzle's if; callers usually feed the result straight into And/Or/S.
//
// Note: the return value is a nil pointer, not a nil interface, so the interface itself is not nil once stored in a Chunk.
// The renderer and And/Or recognize this with isNilChunk, equivalent to drizzle's undefined.
func (s *SQL) If(cond bool) *SQL {
	if !cond {
		return nil
	}
	return s
}

// As gives this SQL a field alias, matching drizzle's sql`...`.as("alias").
func (s *SQL) As(alias string) *Alias {
	return &Alias{SQL: s, FieldAlias: alias}
}

// S assembles an SQL fragment in order.
//
// Argument rules:
//   - A Chunk (*SQL, Fragment, Name, Param, *Placeholder, *Alias, *Subquery,
//     *ValueRows, List, and tables and columns from M1 onward) enters the fragment stream as-is;
//   - nil is treated as missing and skipped — matching the `param !== undefined`
//     filter in drizzle's tag;
//   - []any, []Chunk, and List expand into a parenthesized list. Other slices (including []int) are bound
//     as a single parameter; use ListOf to expand element by element. []byte is always one parameter;
//   - every other value (including a bare string) is wrapped as a *Param and bound. To bind NULL, pass
//     NewParam(nil, nil) explicitly, because a bare nil is skipped.
func S(parts ...any) *SQL {
	chunks := make([]Chunk, 0, len(parts))
	for _, p := range parts {
		if ch, ok := toChunk(p); ok {
			chunks = append(chunks, ch)
		}
	}
	return &SQL{QueryChunks: chunks}
}

// Raw builds a literal SQL fragment: no escaping, no quoting, and no parameter binding.
// Matches drizzle's sql.raw(str).
func Raw(str string) *SQL {
	return &SQL{QueryChunks: []Chunk{Fragment(str)}}
}

// Empty returns an empty fragment, matching drizzle's sql.empty().
func Empty() *SQL { return &SQL{} }

// Join joins a group of fragments with separator.
// Matches drizzle's sql.join(): when separator is nil or the list is empty, it only shallow-copies.
func Join(chunks []Chunk, separator Chunk) *SQL {
	if separator == nil || len(chunks) == 0 {
		return &SQL{QueryChunks: append([]Chunk(nil), chunks...)}
	}
	result := make([]Chunk, 0, len(chunks)*2-1)
	for i, ch := range chunks {
		if i > 0 {
			result = append(result, separator)
		}
		result = append(result, ch)
	}
	return &SQL{QueryChunks: result}
}

// Identifier builds an identifier to be escaped, matching drizzle's sql.identifier(value).
func Identifier(value string) Name { return Name(value) }

// NewParam builds a bound parameter. A nil encoder uses noop semantics.
// Matches drizzle's sql.param(value, encoder).
func NewParam(value any, encoder Encoder) *Param {
	return &Param{Value: value, Encoder: encoder}
}

// NewPlaceholder builds a named placeholder, matching drizzle's sql.placeholder(name).
func NewPlaceholder(name string) *Placeholder {
	return &Placeholder{Name: name}
}

// NewSubquery builds a derived table, rendered as `(...) "alias"`.
func NewSubquery(query *SQL, aliasName string) *Subquery {
	return &Subquery{SQL: query, AliasName: aliasName}
}

// ListOf wraps a group of values as a List, for the expansion semantics of array interpolation in an SQL tag:
// ListOf([]int{1, 2, 3}) renders as `($1, $2, $3)`.
//
// Why a generic version is needed: Go's ...any parameter cannot accept a concrete slice such as []int,
// while drizzle's array expansion works for any element type.
func ListOf[T any](values []T) List {
	out := make(List, len(values))
	for i, v := range values {
		out[i] = toListElement(v)
	}
	return out
}

// toChunk normalizes an arbitrary value into a chunk. The second return value says whether the value is kept
// (false only for nil, matching drizzle's filtering of undefined).
func toChunk(v any) (Chunk, bool) {
	switch x := v.(type) {
	case nil:
		return nil, false
	case Chunk:
		if isNilChunk(x) {
			return nil, false
		}
		return x, true
	case []byte:
		// []byte is a slice in Go, but semantically a single value (bytea), so it is not expanded.
		return &Param{Value: x}, true
	}
	if list, ok := sliceToList(v); ok {
		return list, true
	}
	return &Param{Value: v}, true
}

// toListElement normalizes one list element: a nil in a list is bound as a NULL parameter,
// matching drizzle's array branch where null goes through param.
func toListElement(v any) Chunk {
	if v == nil {
		return &Param{Value: nil}
	}
	ch, _ := toChunk(v)
	return ch
}

// sliceToList expands only values that are already chunk lists.
// Concrete slices such as []int are not expanded here: reflection on the hot path is avoided, and callers use ListOf.
func sliceToList(v any) (List, bool) {
	switch x := v.(type) {
	case []Chunk:
		return List(x), true
	case List:
		return x, true
	case []any:
		return anyList(x), true
	default:
		return nil, false
	}
}

func anyList(values []any) List {
	out := make(List, len(values))
	for i, v := range values {
		out[i] = toListElement(v)
	}
	return out
}
