// Package sqlite is the SQLite dialect.
//
// The default driver is github.com/glebarez/go-sqlite, and the engine is modernc.org/sqlite.
// SQLite is compiled into the binary. CGO_ENABLED=0 works. It does not load the system libsqlite3.
package sqlite

import (
	"strings"

	"github.com/go-mist/mist/sql"
)

// Dialect implements dialect.Dialect.
//
//	escapeName   -> `"name"`, internal double quotes are doubled
//	escapeParam  -> `?`
//	escapeString -> `'text'`, internal single quotes are doubled
type Dialect struct{}

// EscapeName wraps an identifier in double quotes and escapes an internal `"` as `""`.
func (Dialect) EscapeName(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// EscapeParam returns the SQLite placeholder. The index is not written into the text; it is always `?`.
func (Dialect) EscapeParam(int, any) string { return "?" }

// EscapeString returns a single-quoted string literal, doubling internal single quotes.
func (Dialect) EscapeString(str string) string {
	return "'" + strings.ReplaceAll(str, "'", "''") + "'"
}

// SQLToQuery renders a piece of SQL with this dialect into (SQL text, parameters).
func (Dialect) SQLToQuery(query *sql.SQL) (sql.Query, error) {
	return sql.Render(query, Dialect{}, sql.RenderConfig{})
}

// SQLToQueryInline is the inline-parameter mode, for debug output and DDL.
func (Dialect) SQLToQueryInline(query *sql.SQL) (sql.Query, error) {
	return sql.Render(query, Dialect{}, sql.RenderConfig{InlineParams: true})
}
