// Package mysql is the MySQL dialect.
//
// Query chains still use query. This package only handles identifiers, placeholders, and the statement shape of select / insert / update / delete.
// Syntax MySQL does not have is reported as an error, and is not rewritten into a form that only looks equivalent.
package mysql

import (
	"strings"

	"github.com/xbmlz/mist/sql"
)

// Dialect implements the three escapes the renderer needs.
//
//	EscapeName   -> `name`, with internal backticks doubled
//	EscapeParam  -> ?
//	EscapeString -> 'text', with internal single quotes doubled
type Dialect struct{}

// EscapeName wraps an identifier in backticks.
func (Dialect) EscapeName(name string) string {
	return "`" + strings.ReplaceAll(name, "`", "``") + "`"
}

// EscapeParam produces MySQL's question-mark placeholder. The index is unused; every parameter is ?.
func (Dialect) EscapeParam(int, any) string { return "?" }

// EscapeString produces a single-quoted string literal, with internal single quotes doubled.
func (Dialect) EscapeString(str string) string {
	return "'" + strings.ReplaceAll(str, "'", "''") + "'"
}

// SQLToQuery renders a piece of SQL with this dialect into (SQL text, parameters).
func (Dialect) SQLToQuery(query *sql.SQL) (sql.Query, error) {
	return sql.Render(query, Dialect{}, sql.RenderConfig{})
}

// SQLToQueryInline inlines parameters into the SQL for debug output.
func (Dialect) SQLToQueryInline(query *sql.SQL) (sql.Query, error) {
	return sql.Render(query, Dialect{}, sql.RenderConfig{InlineParams: true})
}
