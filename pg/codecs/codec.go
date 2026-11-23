// Package codecs converts between PostgreSQL types and Go values.
//
// A nil encoder means passthrough: driver values pass in and out as-is, and placeholders get no type suffix.
// Types that need a suffix (numeric, jsonb, arrays) implement Codec, then Bind to a column name.
// Array dimensions are written on the column, not into the codec itself.
package codecs

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/go-mist/mist/sql"
)

// Codec is a six-slot conversion for one PostgreSQL type.
// Cast rewrites a select expression, CastInJson rewrites the expression placed into json,
// and CastParam rewrites a placeholder. The three Normalize methods turn a driver value or a parameter value into a Go value.
// Slots that are not needed embed Base, which returns the value as-is.
type Codec interface {
	Cast(expr string) string
	CastInJson(expr string) string
	CastParam(placeholder string, dimensions int) string
	Normalize(value any) (any, error)
	NormalizeInJson(value any) (any, error)
	NormalizeParam(value any, dimensions int) (any, error)
}

// Base makes all six slots passthrough. A concrete type overrides only the methods it needs to change.
type Base struct{}

func (Base) Cast(expr string) string          { return expr }
func (Base) CastInJson(expr string) string    { return expr }
func (Base) CastParam(string, int) string     { return "" }
func (Base) Normalize(value any) (any, error) { return value, nil }
func (Base) NormalizeInJson(value any) (any, error) {
	return value, nil
}
func (Base) NormalizeParam(value any, _ int) (any, error) { return value, nil }

// Bound binds a codec to one column. Error text includes the column name and the expected type.
type Bound struct {
	Column  string
	SQLType string
	Dims    int
	Inner   Codec
}

// Bind binds a codec to a column name. A dimensions value of 0 means a scalar.
func Bind(column, sqlType string, dimensions int, inner Codec) Bound {
	return Bound{Column: column, SQLType: sqlType, Dims: dimensions, Inner: inner}
}

// IsNoop implements sql.Encoder. It is a passthrough when there is no inner codec.
func (b Bound) IsNoop() bool { return b.Inner == nil }

// MapToDriverValue implements sql.Encoder. On conversion failure it keeps the original value; the render path then calls NormalizeParam and returns the error.
func (b Bound) MapToDriverValue(value any) any {
	if b.Inner == nil || isNull(value) {
		return value
	}
	got, err := b.NormalizeParam(value, b.Dims)
	if err != nil {
		return value
	}
	return got
}

// Dimensions implements sql.ParamCodec.
func (b Bound) Dimensions() int { return b.Dims }

// NormalizeParam implements sql.ParamCodec.
func (b Bound) NormalizeParam(value any, dims int) (any, error) {
	if isNull(value) || b.Inner == nil {
		return nil, nil
	}
	value = deref(value)
	got, err := b.Inner.NormalizeParam(value, dims)
	if err != nil {
		return nil, fmt.Errorf("pg: column %s expected %s, %w", b.Column, b.SQLType, err)
	}
	return got, nil
}

// CastParam implements sql.ParamCodec.
func (b Bound) CastParam(placeholder string, dims int) string {
	if b.Inner == nil {
		return ""
	}
	return b.Inner.CastParam(placeholder, dims)
}

// Ensure Bound satisfies the two interfaces the renderer uses.
var (
	_ sql.Encoder    = Bound{}
	_ sql.ParamCodec = Bound{}
)

func castType(placeholder, base string, dims int) string {
	if base == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(placeholder) + 2 + len(base) + dims*2)
	b.WriteString(placeholder)
	b.WriteString("::")
	b.WriteString(base)
	for i := 0; i < dims; i++ {
		b.WriteString("[]")
	}
	return b.String()
}

func deref(value any) any {
	rv := reflect.ValueOf(value)
	for rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return nil
		}
		rv = rv.Elem()
	}
	return rv.Interface()
}

func isNull(value any) bool {
	if value == nil {
		return true
	}
	rv := reflect.ValueOf(value)
	switch rv.Kind() {
	case reflect.Pointer, reflect.Slice, reflect.Map, reflect.Interface:
		return rv.IsNil()
	default:
		return false
	}
}
