package codecs

import (
	"bytes"
	"encoding/json"
)

// JSON is json. JSONB is jsonb. A parameter becomes JSON text first, then gets ::json or ::jsonb.
var (
	JSON  = jsonCodec{name: "json"}
	JSONB = jsonCodec{name: "jsonb"}
)

type jsonCodec struct {
	Base
	name string
}

func (c jsonCodec) Bind(column string) Bound {
	return Bind(column, c.name, 0, c)
}

func (c jsonCodec) CastParam(placeholder string, dims int) string {
	return castType(placeholder, c.name, dims)
}

func (c jsonCodec) NormalizeParam(value any, _ int) (any, error) {
	switch v := value.(type) {
	case json.RawMessage:
		if !json.Valid(v) {
			return nil, errf("value is not valid JSON")
		}
		return string(v), nil
	case []byte:
		if !json.Valid(v) {
			return nil, errf("value is not valid JSON")
		}
		return string(v), nil
	case string:
		if !json.Valid([]byte(v)) {
			return nil, errf("value is not valid JSON")
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

func (c jsonCodec) Normalize(value any) (any, error) {
	raw, err := jsonBytes(value)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(bytes.Clone(raw)), nil
}

func (c jsonCodec) NormalizeInJson(value any) (any, error) {
	return c.Normalize(value)
}

func jsonBytes(value any) ([]byte, error) {
	switch v := value.(type) {
	case json.RawMessage:
		return v, nil
	case []byte:
		return v, nil
	case string:
		return []byte(v), nil
	default:
		return nil, typeOf(value)
	}
}
