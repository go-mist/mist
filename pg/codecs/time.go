package codecs

import "time"

// Time types. time.Time is sent by pgx over the binary protocol.
// date and time get a type suffix so they are not treated as timestamptz.
// timestamptz and timestamp get no suffix, and the placeholder stays $1.
var (
	Date        = timeCodec{name: "date", cast: true}
	Time        = timeCodec{name: "time", cast: true}
	Timestamp   = timeCodec{name: "timestamp"}
	Timestamptz = timeCodec{name: "timestamptz"}
)

type timeCodec struct {
	Base
	name string
	cast bool
}

func (c timeCodec) Bind(column string) Bound {
	return Bind(column, c.name, 0, c)
}

func (c timeCodec) CastParam(placeholder string, dims int) string {
	if !c.cast {
		return ""
	}
	return castType(placeholder, c.name, dims)
}

func (timeCodec) NormalizeParam(value any, _ int) (any, error) {
	if _, ok := value.(time.Time); ok {
		return value, nil
	}
	return nil, typeOf(value)
}
