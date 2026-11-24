package codecs

// Integer range checks. Placeholders get no suffix; pgx already sends them as integers.
// Generated bigint / integer / smallint columns do not attach these codecs by default, so the scan hot path stays a passthrough.
// Bind one to a column when a write must reject an out-of-range value.
var (
	Int2 = intCodec{name: "smallint", bits: 16}
	Int4 = intCodec{name: "integer", bits: 32}
	Int8 = intCodec{name: "bigint", bits: 64}
)

type intCodec struct {
	Base
	name string
	bits int
}

func (c intCodec) Bind(column string) Bound {
	return Bind(column, c.name, 0, c)
}

func (c intCodec) NormalizeParam(value any, _ int) (any, error) {
	n, ok := asInt64(value)
	if !ok {
		return nil, typeOf(value)
	}
	if c.bits < 64 {
		min := int64(-1) << (c.bits - 1)
		max := int64(1)<<(c.bits-1) - 1
		if n < min || n > max {
			return nil, errf("value %d is out of range", n)
		}
	}
	return value, nil
}

func asInt64(value any) (int64, bool) {
	switch v := value.(type) {
	case int:
		return int64(v), true
	case int8:
		return int64(v), true
	case int16:
		return int64(v), true
	case int32:
		return int64(v), true
	case int64:
		return v, true
	default:
		return 0, false
	}
}
