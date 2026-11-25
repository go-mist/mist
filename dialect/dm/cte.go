package dm

import (
	"errors"

	"github.com/go-mist/mist/query"
	"github.com/go-mist/mist/sql"
)

// WithQuery is a named query in a with. From uses it as a table, and Col qualifies columns to this name.
type WithQuery[T any] struct {
	name      string
	query     query.Select[T]
	recursive bool
}

// CTE defines a with query.
func CTE[T any](name string, q query.Select[T]) WithQuery[T] {
	return WithQuery[T]{name: name, query: q}
}

// Recursive defines a recursive CTE. Dameng writes it as WITH, without the RECURSIVE keyword.
func Recursive[T any](name string, q query.Select[T]) WithQuery[T] {
	return WithQuery[T]{name: name, query: q, recursive: true}
}

// WithSpec implements query.WithSource.
func (c WithQuery[T]) WithSpec() query.WithSpec {
	return query.WithSpec{Name: c.name, Recursive: c.recursive, Query: c.query.Spec()}
}

// GetSQL renders the CTE as a table name.
func (c WithQuery[T]) GetSQL() *sql.SQL {
	return sql.S(sql.Identifier(c.name))
}

// ShouldOmitSQLParens keeps the table name from being wrapped in parentheses.
func (WithQuery[T]) ShouldOmitSQLParens() bool { return true }

// Row satisfies query.Table. The row type is still T.
func (WithQuery[T]) Row() *T { return nil }

// Columns are the columns this CTE projects, with the table name replaced by the CTE name.
func (c WithQuery[T]) Columns() []sql.Chunk {
	cols := c.query.Projected()
	out := make([]sql.Chunk, len(cols))
	for i, col := range cols {
		out[i] = c.Col(col)
	}
	return out
}

// Col copies a column and replaces the table name with this CTE. Scanning still uses the original field address.
func (c WithQuery[T]) Col(col query.Column[T]) query.Column[T] {
	type relabel interface {
		WithTable(string) query.Column[T]
	}
	if col == nil {
		return nil
	}
	named, ok := col.(relabel)
	if !ok {
		return col
	}
	return named.WithTable(c.name)
}

// Scan scans one row into T following the CTE projection. Unselected fields stay at the zero value.
func (c WithQuery[T]) Scan(row sql.Row, dst *T) error {
	cols := c.query.Projected()
	if len(cols) == 0 {
		return errors.New("dm: cte has no columns")
	}
	dest := make([]any, len(cols))
	for i, col := range cols {
		ptr := col.FieldPtr(dst)
		if ptr == nil {
			return query.MissingField(col)
		}
		dest[i] = ptr
	}
	return row.Scan(dest...)
}
