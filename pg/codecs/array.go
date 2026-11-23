package codecs

import (
	"fmt"
	"reflect"
	"strings"
)

// Array is an array of one element type. dimensions starts at 1.
// A one-dimensional text array renders as $1::text[], and a two-dimensional int4 array renders as $1::int4[][].
type Array struct {
	Base
	BaseType   string
	Dimensions int
}

func (a Array) sqlType() string {
	dims := a.Dimensions
	if dims < 1 {
		dims = 1
	}
	return a.BaseType + strings.Repeat("[]", dims)
}

// Bind binds this array type to a column.
func (a Array) Bind(column string) Bound {
	dims := a.Dimensions
	if dims < 1 {
		dims = 1
	}
	a.Dimensions = dims
	return Bind(column, a.sqlType(), dims, a)
}

func (a Array) CastParam(placeholder string, dims int) string {
	if dims < 1 {
		dims = a.Dimensions
	}
	if dims < 1 {
		dims = 1
	}
	return castType(placeholder, a.BaseType, dims)
}

func (a Array) NormalizeParam(value any, _ int) (any, error) {
	rv := reflect.ValueOf(value)
	switch rv.Kind() {
	case reflect.Slice, reflect.Array:
		return value, nil
	default:
		return nil, typeOf(value)
	}
}

// Enum is a PostgreSQL enum. The parameter is a string, and the suffix is :: plus the type name.
type Enum struct {
	Base
	Name string
}

func (e Enum) Bind(column string) Bound {
	return Bind(column, e.Name, 0, e)
}

func (e Enum) CastParam(placeholder string, _ int) string {
	return castType(placeholder, e.Name, 0)
}

func (Enum) NormalizeParam(value any, _ int) (any, error) {
	s, ok := value.(string)
	if !ok {
		return nil, typeOf(value)
	}
	if s == "" {
		return nil, errf("enum value cannot be empty")
	}
	return s, nil
}

// UUID is uuid. The value must be hexadecimal text in 8-4-4-4-12 form.
var UUID uuidCodec

type uuidCodec struct{ Base }

func (uuidCodec) Bind(column string) Bound {
	return Bind(column, "uuid", 0, UUID)
}

func (uuidCodec) CastParam(placeholder string, dims int) string {
	return castType(placeholder, "uuid", dims)
}

func (uuidCodec) NormalizeParam(value any, _ int) (any, error) {
	s, ok := value.(string)
	if !ok {
		return nil, typeOf(value)
	}
	if !uuidOK(s) {
		return nil, errf("value %q is not a uuid", s)
	}
	return s, nil
}

func uuidOK(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i := 0; i < len(s); i++ {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if s[i] != '-' {
				return false
			}
			continue
		}
		c := s[i]
		switch {
		case c >= '0' && c <= '9', c >= 'a' && c <= 'f', c >= 'A' && c <= 'F':
		default:
			return false
		}
	}
	return true
}

func typeOf(value any) error {
	return fmt.Errorf("got %T", value)
}

func errf(format string, args ...any) error {
	return fmt.Errorf(format, args...)
}
