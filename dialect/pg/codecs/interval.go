package codecs

import "time"

// Interval is interval. time.Duration is handed to the driver; a string is left as-is and gets ::interval.
var Interval intervalCodec

type intervalCodec struct{ Base }

func (intervalCodec) Bind(column string) Bound {
	return Bind(column, "interval", 0, Interval)
}

func (intervalCodec) CastParam(placeholder string, dims int) string {
	return castType(placeholder, "interval", dims)
}

func (intervalCodec) NormalizeParam(value any, _ int) (any, error) {
	switch value.(type) {
	case time.Duration, string:
		return value, nil
	default:
		return nil, typeOf(value)
	}
}
