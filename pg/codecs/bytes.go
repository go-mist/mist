package codecs

import (
	"encoding/hex"
	"strings"
)

// Bytea is bytea. []byte is handed to the driver as-is; a string starting with \x is decoded as hexadecimal.
var Bytea byteaCodec

type byteaCodec struct{ Base }

func (byteaCodec) Bind(column string) Bound {
	return Bind(column, "bytea", 0, Bytea)
}

func (byteaCodec) CastParam(placeholder string, dims int) string {
	return castType(placeholder, "bytea", dims)
}

func (byteaCodec) NormalizeParam(value any, _ int) (any, error) {
	switch v := value.(type) {
	case []byte:
		return v, nil
	case string:
		if !strings.HasPrefix(v, `\x`) {
			return nil, errf("string must be \\x hex")
		}
		raw, err := hex.DecodeString(v[2:])
		if err != nil {
			return nil, errf("invalid hex")
		}
		return raw, nil
	default:
		return nil, typeOf(value)
	}
}
