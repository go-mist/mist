package query

import (
	"fmt"

	"github.com/go-mist/mist/sql"
)

// Subquery is an aliased derived table. From and Join use it.
// Scalar, in, and exists take the original Select directly and do not go through here.
type Subquery[T any] struct {
	alias string
	spec  SelectSpec
	cols  []Column[T]
}

// DerivedQuery hands the alias and the spec to the dialect. It renders as `(select ...) "alias"`.
func (q Subquery[T]) DerivedQuery() (string, SelectSpec) { return q.alias, q.spec }

// GetSQL stands in as a placeholder until the dialect expands it. After expansion this spot is replaced by the real derived table.
func (Subquery[T]) GetSQL() *sql.SQL {
	return sql.Raw("/* query: subquery was not expanded */")
}

// ShouldOmitSQLParens keeps the unexpanded placeholder text from being wrapped in parentheses again.
func (Subquery[T]) ShouldOmitSQLParens() bool { return true }

// Row satisfies Table. The row type is still T.
func (Subquery[T]) Row() *T { return nil }

// Columns are the derived table's columns, with the table name replaced by the alias.
func (q Subquery[T]) Columns() []sql.Chunk {
	out := make([]sql.Chunk, len(q.cols))
	for i, col := range q.cols {
		out[i] = q.Col(col)
	}
	return out
}

// Col copies one column and replaces the table name with this derived table's alias. Scan still uses the original field address.
func (q Subquery[T]) Col(col Column[T]) Column[T] {
	type renamer interface {
		WithTable(string) Column[T]
	}
	if col == nil {
		return nil
	}
	named, ok := col.(renamer)
	if !ok {
		return col
	}
	return named.WithTable(q.alias)
}

// Scan scans one row into T by the derived table's projection. Fields that were not selected stay at the zero value.
func (q Subquery[T]) Scan(row sql.Row, dst *T) error {
	if len(q.cols) == 0 {
		return fmt.Errorf("query: subquery has no columns")
	}
	dest := make([]any, len(q.cols))
	for i, col := range q.cols {
		ptr := col.FieldPtr(dst)
		if ptr == nil {
			return MissingField(col)
		}
		dest[i] = ptr
	}
	return row.Scan(dest...)
}
