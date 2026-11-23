// Package codecs converts parameters between MySQL types and Go values.
//
// Placeholders stay ?. No ::type suffix is appended; MySQL has no such syntax.
// Scanning stays with database/sql. decimal scans into string, json into json.RawMessage or []byte,
// datetime into time.Time when the connection string has parseTime=true, and blob into []byte.
package codecs

import (
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"time"

	"github.com/xbmlz/mist/sql"
)

// Codec rewrites parameter values. CastParam stays empty on MySQL.
type Codec interface {
	CastParam(placeholder string, dimensions int) string
	NormalizeParam(value any, dimensions int) (any, error)
}

// Bound binds a codec to one column. Error messages include the column name and the expected type.
type Bound struct {
	Column  string
	SQLType string
	Inner   Codec
}

// Bind binds a codec to a column name.
func Bind(column, sqlType string, inner Codec) Bound {
	return Bound{Column: column, SQLType: sqlType, Inner: inner}
}

// IsNoop implements sql.Encoder.
func (b Bound) IsNoop() bool { return b.Inner == nil }

// MapToDriverValue implements sql.Encoder. On conversion failure the original value is kept, and the render path uses NormalizeParam and returns the error.
func (b Bound) MapToDriverValue(value any) any {
	if b.Inner == nil || isNull(value) {
		return value
	}
	got, err := b.NormalizeParam(value, 0)
	if err != nil {
		return value
	}
	return got
}

// Dimensions implements sql.ParamCodec. This version of MySQL has no arrays.
func (Bound) Dimensions() int { return 0 }

// NormalizeParam implements sql.ParamCodec.
func (b Bound) NormalizeParam(value any, _ int) (any, error) {
	if isNull(value) || b.Inner == nil {
		return nil, nil
	}
	value = deref(value)
	got, err := b.Inner.NormalizeParam(value, 0)
	if err != nil {
		return nil, fmt.Errorf("mysql: column %s expected %s, %w", b.Column, b.SQLType, err)
	}
	return got, nil
}

// CastParam implements sql.ParamCodec. MySQL adds no type suffix.
func (Bound) CastParam(string, int) string { return "" }

var (
	_ sql.Encoder    = Bound{}
	_ sql.ParamCodec = Bound{}
)

// Decimal is decimal. The parameter is a decimal string, and the placeholder stays ?.
var Decimal decimalCodec

type decimalCodec struct{}

var decimalPattern = regexp.MustCompile(`^[+-]?(?:\d+(?:\.\d*)?|\.\d+)$`)

func (decimalCodec) Bind(column string) Bound { return Bind(column, "decimal", Decimal) }

func (decimalCodec) CastParam(string, int) string { return "" }

func (decimalCodec) NormalizeParam(value any, _ int) (any, error) {
	s, ok := value.(string)
	if !ok {
		return nil, typeOf(value)
	}
	if !decimalPattern.MatchString(s) {
		return nil, fmt.Errorf("value %q is not a number", s)
	}
	return s, nil
}

// JSON is json. Structs are encoded to text first. Placeholders get no suffix.
var JSON jsonCodec

type jsonCodec struct{}

func (jsonCodec) Bind(column string) Bound { return Bind(column, "json", JSON) }

func (jsonCodec) CastParam(string, int) string { return "" }

func (jsonCodec) NormalizeParam(value any, _ int) (any, error) {
	switch v := value.(type) {
	case json.RawMessage:
		if !json.Valid(v) {
			return nil, fmt.Errorf("value is not valid JSON")
		}
		return string(v), nil
	case []byte:
		if !json.Valid(v) {
			return nil, fmt.Errorf("value is not valid JSON")
		}
		return string(v), nil
	case string:
		if !json.Valid([]byte(v)) {
			return nil, fmt.Errorf("value is not valid JSON")
		}
		return v, nil
	default:
		buf, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		return string(buf), nil
	}
}

// DateTime is datetime. Only time.Time is accepted.
var DateTime dateTimeCodec

type dateTimeCodec struct{}

func (dateTimeCodec) Bind(column string) Bound { return Bind(column, "datetime", DateTime) }

func (dateTimeCodec) CastParam(string, int) string { return "" }

func (dateTimeCodec) NormalizeParam(value any, _ int) (any, error) {
	if _, ok := value.(time.Time); ok {
		return value, nil
	}
	return nil, typeOf(value)
}

// Blob is blob. Only []byte is accepted.
var Blob blobCodec

type blobCodec struct{}

func (blobCodec) Bind(column string) Bound { return Bind(column, "blob", Blob) }

func (blobCodec) CastParam(string, int) string { return "" }

func (blobCodec) NormalizeParam(value any, _ int) (any, error) {
	b, ok := value.([]byte)
	if !ok {
		return nil, typeOf(value)
	}
	return b, nil
}

func typeOf(value any) error {
	return fmt.Errorf("got %T", value)
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
