package relation

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ReadTextInt writes a JSON string or number into *int64. A nested bigint is a string after ::text.
func ReadTextInt(dst any, raw []byte) error {
	p, ok := dst.(*int64)
	if !ok {
		return fmt.Errorf("relation: ReadTextInt requires *int64")
	}
	if err := json.Unmarshal(raw, p); err == nil {
		return nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return err
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return err
	}
	*p = n
	return nil
}

// ReadTextTime writes a time string from JSON into *time.Time.
// It accepts RFC3339, the ISO text of PostgreSQL `timestamptz::text`, and a zoneless `YYYY-MM-DD HH:MM:SS`. A zoneless value is parsed as UTC.
func ReadTextTime(dst any, raw []byte) error {
	p, ok := dst.(*time.Time)
	if !ok {
		return fmt.Errorf("relation: ReadTextTime requires *time.Time")
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return err
	}
	for _, layout := range []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05.999999999-07",
		"2006-01-02 15:04:05.999999999-07:00",
		"2006-01-02 15:04:05.999999999Z07:00",
		"2006-01-02 15:04:05-07",
		"2006-01-02 15:04:05-07:00",
		"2006-01-02 15:04:05Z07:00",
		"2006-01-02T15:04:05.999999999-07",
		"2006-01-02T15:04:05-07",
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05.999999999",
		"2006-01-02T15:04:05",
	} {
		got, err := time.Parse(layout, s)
		if err == nil {
			*p = got
			return nil
		}
	}
	return fmt.Errorf("relation: cannot parse time %q", s)
}

// ReadHexBytes writes `\x` hexadecimal from JSON into *[]byte. bytea comes through row_to_json as this text.
func ReadHexBytes(dst any, raw []byte) error {
	p, ok := dst.(*[]byte)
	if !ok {
		return fmt.Errorf("relation: ReadHexBytes requires *[]byte")
	}
	if string(raw) == "null" {
		*p = nil
		return nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return err
	}
	s = strings.TrimPrefix(s, `\x`)
	s = strings.TrimPrefix(s, "\\x")
	buf, err := hex.DecodeString(s)
	if err != nil {
		return err
	}
	*p = buf
	return nil
}
