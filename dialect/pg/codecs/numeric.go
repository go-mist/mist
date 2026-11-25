package codecs

import "regexp"

// Numeric is numeric / decimal. The parameter is a decimal string, and the placeholder is written as $1::numeric.
// A string is used so decimal places are kept, without going through float64.
var Numeric numericCodec

type numericCodec struct{ Base }

var numericPattern = regexp.MustCompile(`^[+-]?(?:\d+(?:\.\d*)?|\.\d+)$`)

func (numericCodec) Bind(column string) Bound {
	return Bind(column, "numeric", 0, Numeric)
}

func (numericCodec) CastParam(placeholder string, dims int) string {
	return castType(placeholder, "numeric", dims)
}

func (numericCodec) NormalizeParam(value any, _ int) (any, error) {
	s, ok := value.(string)
	if !ok {
		return nil, typeOf(value)
	}
	if !numericPattern.MatchString(s) {
		return nil, errf("value %q is not a number", s)
	}
	return s, nil
}
