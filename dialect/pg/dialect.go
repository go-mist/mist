// Package pg is the PostgreSQL dialect.
//
// The three escape functions the renderer needs, and the column descriptors and table base used by generated code, are here.
// Query compilation and type mapping are in M2 and M4.
package pg

import (
	"strconv"
	"strings"

	"github.com/go-mist/mist/sql"
)

// Dialect implements the minimum contract of dialect.Dialect.
//
// This matches the part of drizzle's PgDialect that lives in the renderer:
//
//	escapeName   -> `"name"`, internal double quotes doubled
//	escapeParam  -> `$1`, `$2`, ...
//	escapeString -> `'text'`, internal single quotes doubled
type Dialect struct{}

// EscapeName wraps an identifier in double quotes and escapes an internal `"` as `""`.
// It matches drizzle's escapePgName byte for byte.
func (Dialect) EscapeName(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// EscapeParam builds a pg positional parameter, starting at $1.
// drizzle's PgDialect ignores the second argument (value); Go keeps it for future dialects.
func (Dialect) EscapeParam(num int, _ any) string {
	return "$" + strconv.Itoa(num+1)
}

// EscapeString builds a single-quoted string literal, doubling internal single quotes.
func (Dialect) EscapeString(str string) string {
	return "'" + strings.ReplaceAll(str, "'", "''") + "'"
}

// SQLToQuery renders a piece of SQL with this dialect into (SQL text, parameters).
// It matches drizzle's PgDialect.sqlToQuery.
func (Dialect) SQLToQuery(query *sql.SQL) (sql.Query, error) {
	return sql.Render(query, Dialect{}, sql.RenderConfig{})
}

// SQLToQueryInline is inline-parameter mode, for debug output and DDL.
//
// drizzle has no counterpart: it switches to inline mode in place via `query.inlineParams()`.
// Go keeps both entry points, because the two renderings of one SQL statement are a common need in tests and the CLI.
func (Dialect) SQLToQueryInline(query *sql.SQL) (sql.Query, error) {
	return sql.Render(query, Dialect{}, sql.RenderConfig{InlineParams: true})
}
