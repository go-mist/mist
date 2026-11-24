// Package oracle is the Oracle dialect.
//
// Query chains still use query. This package quotes identifiers, numbers placeholders, and builds select / insert / update / delete.
// A statement Oracle does not accept in this form returns an error. It is not rewritten into something that only looks equivalent.
package oracle

import (
	"strings"

	"github.com/go-mist/mist/sql"
)

// Dialect implements the three escapes the renderer needs.
//
//	EscapeName   -> "name", with internal quotes doubled
//	EscapeParam  -> :1, :2, ... The index passed in starts at 0.
//	EscapeString -> 'text', with internal single quotes doubled
type Dialect struct{}

// EscapeName wraps an identifier in double quotes so Oracle does not fold it to upper case.
func (Dialect) EscapeName(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// EscapeParam produces a :n placeholder. num starts at 0, and the marker starts at :1.
func (Dialect) EscapeParam(num int, _ any) string {
	return ":" + itoa(num+1)
}

// EscapeString produces a single-quoted string literal, with internal single quotes doubled.
func (Dialect) EscapeString(str string) string {
	return "'" + strings.ReplaceAll(str, "'", "''") + "'"
}

// SQLToQuery renders a piece of SQL with this dialect into (SQL text, parameters).
func (Dialect) SQLToQuery(query *sql.SQL) (sql.Query, error) {
	return sql.Render(query, Dialect{}, sql.RenderConfig{})
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
