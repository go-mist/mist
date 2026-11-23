package relation

import "github.com/xbmlz/mist/sql"

// Kinds of filter nodes. Constructors fill them, and the compiler reads them.
const (
	FilterNone = iota
	FilterField
	FilterAnd
	FilterOr
	FilterNot
	FilterRaw
	FilterRel
)

// Filter is a data-shaped where. The zero value means no filter.
type Filter struct {
	Kind     int
	Column   string
	Op       string
	Value    any
	Value2   any
	Relation string
	Exists   bool
	Kids     []Filter
	Raw      sql.Chunk
	Nested   *Filter
}

func field(col, op string, v, v2 any) Filter {
	return Filter{Kind: FilterField, Column: col, Op: op, Value: v, Value2: v2}
}

// Eq is a column equal to a parameter.
func Eq(col string, v any) Filter { return field(col, "eq", v, nil) }

// Ne is a column not equal to a parameter.
func Ne(col string, v any) Filter { return field(col, "ne", v, nil) }

// Gt is a column greater than a parameter.
func Gt(col string, v any) Filter { return field(col, "gt", v, nil) }

// Gte is a column greater than or equal to a parameter.
func Gte(col string, v any) Filter { return field(col, "gte", v, nil) }

// Lt is a column less than a parameter.
func Lt(col string, v any) Filter { return field(col, "lt", v, nil) }

// Lte is a column less than or equal to a parameter.
func Lte(col string, v any) Filter { return field(col, "lte", v, nil) }

// Like is `like`.
func Like(col string, v any) Filter { return field(col, "like", v, nil) }

// ILike is `ilike`.
func ILike(col string, v any) Filter { return field(col, "ilike", v, nil) }

// NotLike is `not like`.
func NotLike(col string, v any) Filter { return field(col, "notLike", v, nil) }

// NotILike is `not ilike`.
func NotILike(col string, v any) Filter { return field(col, "notIlike", v, nil) }

// IsNull is `(column is null)`.
func IsNull(col string) Filter { return field(col, "isNull", nil, nil) }

// IsNotNull is `(column is not null)`.
func IsNotNull(col string) Filter { return field(col, "isNotNull", nil, nil) }

// In is `in`. Values are []any. An empty list uses the same SQL as op.InArray.
func In(col string, vals []any) Filter { return field(col, "in", vals, nil) }

// NotIn is `not in`.
func NotIn(col string, vals []any) Filter { return field(col, "notIn", vals, nil) }

// Between is `between`.
func Between(col string, min, max any) Filter { return field(col, "between", min, max) }

// NotBetween is `not between`.
func NotBetween(col string, min, max any) Filter { return field(col, "notBetween", min, max) }

// And joins filters with and. An empty list returns an error at compile time.
func And(filters ...Filter) Filter {
	return Filter{Kind: FilterAnd, Kids: append([]Filter(nil), filters...)}
}

// Or joins filters with or.
func Or(filters ...Filter) Filter {
	return Filter{Kind: FilterOr, Kids: append([]Filter(nil), filters...)}
}

// Not negates a filter. Wrapped around a relation condition, it is `not (exists ...)`.
func Not(f Filter) Filter {
	return Filter{Kind: FilterNot, Kids: []Filter{f}}
}

// RawSQL is the only SQL escape hatch. It counts as a relation condition, so optimized may wrap.
func RawSQL(chunk sql.Chunk) Filter {
	return Filter{Kind: FilterRaw, Raw: chunk}
}

// Exists means a related row exists and renders `exists`.
func Exists(name string) Filter {
	return Filter{Kind: FilterRel, Relation: name, Exists: true}
}

// Absent means no related row exists and renders `not exists`, with no further `not` outside.
func Absent(name string) Filter {
	return Filter{Kind: FilterRel, Relation: name, Exists: false}
}

// WhereRel is exists with a nested condition, for example a post whose title is x.
func WhereRel(name string, nested Filter) Filter {
	n := nested
	return Filter{Kind: FilterRel, Relation: name, Exists: true, Nested: &n}
}
