// Package dialect defines the minimum contract a dialect owes the renderer.
//
// Design principle 4: a dialect is three escape functions plus a handful of build functions. Only the former live here.
// Build*Query lives in each dialect package (see ROADMAP M2) and does not enter this interface — otherwise every new
// dialect implementation would force every dialect to grow a pile of unused methods.
package dialect

// Dialect is everything a dialect must provide when rendering an SQL fragment stream.
//
// The drizzle-orm v1.0.0-rc.5 counterpart is the three callbacks
// escapeName / escapeParam / escapeString on BuildQueryConfig.
type Dialect interface {
	// EscapeName escapes an identifier (table name, column name, alias).
	EscapeName(name string) string
	// EscapeParam produces the placeholder for parameter num (starting at 0).
	// value is unused on pg, but mysql's ? and mssql's @p1 need it for type inference.
	EscapeParam(num int, value any) string
	// EscapeString escapes a string literal, for inline-parameter mode.
	EscapeString(str string) string
}
