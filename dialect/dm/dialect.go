// Package dm is the Dameng dialect.
//
// Query chains still use query. This package quotes identifiers, uses ? placeholders, and builds select / insert / update / delete.
// A statement Dameng does not accept in this form returns an error. It is not rewritten into something that only looks equivalent.
// The connection driver is the pure Go driver gitee.com/chunanyong/dm. That module redistributes Dameng's Go driver and does not declare a license.
package dm

import (
	"strings"

	"github.com/go-mist/mist/sql"
)

// Dialect implements the three escapes the renderer needs.
//
//	EscapeName   -> "name", with internal quotes doubled
//	EscapeParam  -> ? on every call. The index is unused; the driver binds by position.
//	EscapeString -> 'text', with internal single quotes doubled
type Dialect struct{}

// EscapeName wraps an identifier in double quotes so the written case is kept.
func (Dialect) EscapeName(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// EscapeParam produces a ? placeholder. Dameng's Go driver binds parameters in order.
func (Dialect) EscapeParam(int, any) string {
	return "?"
}

// EscapeString produces a single-quoted string literal, with internal single quotes doubled.
func (Dialect) EscapeString(str string) string {
	return "'" + strings.ReplaceAll(str, "'", "''") + "'"
}

// SQLToQuery renders a piece of SQL with this dialect into (SQL text, parameters).
func (Dialect) SQLToQuery(query *sql.SQL) (sql.Query, error) {
	return sql.Render(query, Dialect{}, sql.RenderConfig{})
}
