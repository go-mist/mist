// Package op holds the operators used at query call sites.
//
// Conditions, ordering, and aggregates are written from here. The implementation is SQL fragments, and there is no separate expression tree.
// sql.And, sql.Or, and sql.Not remain fragment combinators; op.And calls them.
package op

import "github.com/go-mist/mist/sql"

// Eq renders `left = right`. right is bound as a parameter.
func Eq(left sql.Chunk, right any) sql.Chunk { return cmp(left, " = ", right) }

// Ne renders `left <> right`.
func Ne(left sql.Chunk, right any) sql.Chunk { return cmp(left, " <> ", right) }

// Gt renders `left > right`.
func Gt(left sql.Chunk, right any) sql.Chunk { return cmp(left, " > ", right) }

// Gte renders `left >= right`.
func Gte(left sql.Chunk, right any) sql.Chunk { return cmp(left, " >= ", right) }

// Lt renders `left < right`.
func Lt(left sql.Chunk, right any) sql.Chunk { return cmp(left, " < ", right) }

// Lte renders `left <= right`.
func Lte(left sql.Chunk, right any) sql.Chunk { return cmp(left, " <= ", right) }

// And joins conditions with and and skips nil. The meaning matches sql.And.
func And(conditions ...sql.Chunk) *sql.SQL { return sql.And(conditions...) }

// Or joins conditions with or and skips nil. The meaning matches sql.Or.
func Or(conditions ...sql.Chunk) *sql.SQL { return sql.Or(conditions...) }

// Not negates one condition. The meaning matches sql.Not.
func Not(condition sql.Chunk) *sql.SQL { return sql.Not(condition) }

// Asc renders `expr asc`.
func Asc(expr sql.Chunk) sql.Chunk { return &sortExpr{expr: expr, dir: " asc"} }

// Desc renders `expr desc`.
func Desc(expr sql.Chunk) sql.Chunk { return &sortExpr{expr: expr, dir: " desc"} }

// pred is one comparison. The chunk list is built only when a dialect renders the slow path.
type pred struct {
	left  sql.Chunk
	op    string
	right any
}

// GetSQL implements sql.Chunk.
func (p *pred) GetSQL() *sql.SQL {
	if p == nil {
		return nil
	}
	return sql.S(p.left, sql.Fragment(p.op), bindEncoded(p.left, p.right))
}

// ShouldOmitSQLParens keeps the comparison from being wrapped in parentheses.
func (*pred) ShouldOmitSQLParens() bool { return true }

// Compare exposes the comparison to a dialect fast path.
func (p *pred) Compare() (sql.Chunk, string, any) {
	if p == nil {
		return nil, "", nil
	}
	return p.left, p.op, p.right
}

// sortExpr is one ordering. The chunk list is built only when a dialect renders the slow path.
type sortExpr struct {
	expr sql.Chunk
	dir  string
}

// GetSQL implements sql.Chunk.
func (s *sortExpr) GetSQL() *sql.SQL {
	if s == nil {
		return nil
	}
	return sql.S(s.expr, sql.Fragment(s.dir))
}

// ShouldOmitSQLParens keeps the ordering from being wrapped in parentheses.
func (*sortExpr) ShouldOmitSQLParens() bool { return true }

// Direction exposes the ordering to a dialect fast path.
func (s *sortExpr) Direction() (sql.Chunk, string) {
	if s == nil {
		return nil, ""
	}
	return s.expr, s.dir
}

// In renders `left in (query)`. query is a select, or SQL that is already written.
// A list uses InArray. Go cannot take both argument kinds in one function.
func In(left, query sql.Chunk) *sql.SQL {
	return inQuery(left, " in ", query)
}

// NotIn renders `left not in (query)`.
func NotIn(left, query sql.Chunk) *sql.SQL {
	return inQuery(left, " not in ", query)
}

func inQuery(left sql.Chunk, keyword string, query sql.Chunk) *sql.SQL {
	if _, already := query.(*sql.SQL); already {
		return sql.S(left, sql.Fragment(keyword+"("), query, sql.Fragment(")"))
	}
	return sql.S(left, sql.Fragment(keyword), query)
}

// InArray renders `left in ($1, $2, ...)`.
// An empty list renders as `1 = 0`. PostgreSQL does not accept `in ()`.
func InArray[T any](left sql.Chunk, values []T) *sql.SQL {
	if len(values) == 0 {
		return sql.Raw("1 = 0")
	}
	enc := encoderOf(left)
	if enc == nil {
		return sql.S(left, sql.Fragment(" in "), sql.ListOf(values))
	}
	parts := make(sql.List, len(values))
	for i, v := range values {
		parts[i] = sql.NewParam(v, enc)
	}
	return sql.S(left, sql.Fragment(" in "), parts)
}

// NotInArray renders `left not in ($1, $2, ...)`.
// An empty list renders as `1 = 1`.
func NotInArray[T any](left sql.Chunk, values []T) *sql.SQL {
	if len(values) == 0 {
		return sql.Raw("1 = 1")
	}
	enc := encoderOf(left)
	if enc == nil {
		return sql.S(left, sql.Fragment(" not in "), sql.ListOf(values))
	}
	parts := make(sql.List, len(values))
	for i, v := range values {
		parts[i] = sql.NewParam(v, enc)
	}
	return sql.S(left, sql.Fragment(" not in "), parts)
}

// Between renders `left between min and max`, inclusive on both ends.
func Between(left sql.Chunk, min, max any) *sql.SQL {
	return sql.S(left, sql.Fragment(" between "), bindEncoded(left, min), sql.Fragment(" and "), bindEncoded(left, max))
}

// NotBetween renders `left not between min and max`.
func NotBetween(left sql.Chunk, min, max any) *sql.SQL {
	return sql.S(left, sql.Fragment(" not between "), bindEncoded(left, min), sql.Fragment(" and "), bindEncoded(left, max))
}

// Like renders `left like pattern`. `%` matches any length, and `_` matches one character.
func Like(left sql.Chunk, pattern any) *sql.SQL {
	return sql.S(left, sql.Fragment(" like "), bindEncoded(left, pattern))
}

// NotLike renders `left not like pattern`.
func NotLike(left sql.Chunk, pattern any) *sql.SQL {
	return sql.S(left, sql.Fragment(" not like "), bindEncoded(left, pattern))
}

// ILike renders `left ilike pattern`. The comparison is case-insensitive.
func ILike(left sql.Chunk, pattern any) *sql.SQL {
	return sql.S(left, sql.Fragment(" ilike "), bindEncoded(left, pattern))
}

// NotILike renders `left not ilike pattern`.
func NotILike(left sql.Chunk, pattern any) *sql.SQL {
	return sql.S(left, sql.Fragment(" not ilike "), bindEncoded(left, pattern))
}

// IsNull renders `(expr is null)`. In SQL, `= NULL` yields no truth value.
func IsNull(expr sql.Chunk) *sql.SQL {
	return sql.S(sql.Fragment("("), expr, sql.Fragment(" is null)"))
}

// IsNotNull renders `(expr is not null)`.
func IsNotNull(expr sql.Chunk) *sql.SQL {
	return sql.S(sql.Fragment("("), expr, sql.Fragment(" is not null)"))
}

// Exists renders `exists (query)`.
// query may be a select, or SQL that is already written.
func Exists(query sql.Chunk) *sql.SQL {
	return existsQuery("exists ", query)
}

// NotExists renders `not exists (query)`.
func NotExists(query sql.Chunk) *sql.SQL {
	return existsQuery("not exists ", query)
}

type driverEncoded interface {
	DriverEncoder() sql.Encoder
}

func encoderOf(ch sql.Chunk) sql.Encoder {
	e, ok := ch.(driverEncoded)
	if !ok || e == nil {
		return nil
	}
	enc := e.DriverEncoder()
	if enc == nil || enc.IsNoop() {
		return nil
	}
	return enc
}

func bindEncoded(left sql.Chunk, right any) any {
	enc := encoderOf(left)
	if enc == nil || right == nil {
		return right
	}
	if ph, ok := right.(*sql.Placeholder); ok {
		return sql.NewParam(ph, enc)
	}
	if _, ok := right.(sql.Chunk); ok {
		return right
	}
	return sql.NewParam(right, enc)
}

func existsQuery(keyword string, query sql.Chunk) *sql.SQL {
	if _, already := query.(*sql.SQL); already || query == nil {
		return sql.S(sql.Fragment(keyword+"("), query, sql.Fragment(")"))
	}
	return sql.S(sql.Fragment(keyword), query)
}

func cmp(left sql.Chunk, operator string, right any) sql.Chunk {
	return &pred{left: left, op: operator, right: right}
}
