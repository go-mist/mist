package oracle

import (
	"errors"
	"fmt"

	"github.com/go-mist/mist/query"
	"github.com/go-mist/mist/sql"
)

// Select starts a query. Omitting columns means the whole row; the column list is taken from the table in From.
func Select[T any](cols ...query.Column[T]) query.Select[T] {
	return query.NewSelect(cols...)
}

// Insert starts an insert.
func Insert[T any](table query.Table[T]) query.Insert[T] {
	return query.NewInsert(table)
}

// Update starts an update.
func Update[T any](table query.Table[T]) query.Update[T] {
	return query.NewUpdate(table)
}

// Delete starts a delete.
func Delete[T any](table query.Table[T]) query.Delete[T] {
	return query.NewDelete(table)
}

// BuildSelectQuery compiles a select spec into an SQL fragment.
// OFFSET / FETCH is written only after ORDER BY. Oracle rejects it otherwise.
func (Dialect) BuildSelectQuery(spec query.SelectSpec) (*sql.SQL, error) {
	if spec.SetErr != "" {
		return nil, errors.New(spec.SetErr)
	}
	if len(spec.With) > 0 || len(spec.Sets) > 0 {
		return nil, errors.New("oracle: with and set operations are not supported")
	}
	if len(spec.Joins) > 0 || len(spec.GroupBy) > 0 || spec.Having != nil {
		return nil, errors.New("oracle: join, group by, and having are not supported")
	}
	if spec.Distinct || len(spec.DistinctOn) > 0 {
		return nil, errors.New("oracle: distinct and distinct on are not supported")
	}
	if spec.Lock != nil {
		return nil, errors.New("oracle: for update is not supported")
	}
	if spec.From == nil {
		return nil, errors.New("oracle: select is missing from")
	}
	if len(spec.Columns) == 0 {
		return nil, errors.New("oracle: select is missing columns")
	}
	if (spec.LimitSet || spec.OffsetSet) && len(spec.OrderBy) == 0 {
		return nil, errors.New("oracle: offset and fetch require order by")
	}
	parts := []any{sql.Raw("select ")}
	for i, col := range spec.Columns {
		if i > 0 {
			parts = append(parts, sql.Raw(", "))
		}
		parts = append(parts, col)
	}
	parts = append(parts, sql.Raw(" from "), spec.From)
	if spec.Where != nil {
		parts = append(parts, sql.Raw(" where "), spec.Where)
	}
	if len(spec.OrderBy) > 0 {
		parts = append(parts, sql.Raw(" order by "))
		for i, expr := range spec.OrderBy {
			if i > 0 {
				parts = append(parts, sql.Raw(", "))
			}
			parts = append(parts, expr)
		}
	}
	if spec.OffsetSet {
		parts = append(parts, sql.Raw(" offset "), spec.Offset, sql.Raw(" rows"))
	}
	if spec.LimitSet {
		parts = append(parts, sql.Raw(" fetch next "), spec.Limit, sql.Raw(" rows only"))
	}
	return sql.S(parts...), nil
}

// BuildInsertQuery compiles an insert spec into an SQL fragment.
func (Dialect) BuildInsertQuery(spec query.InsertSpec) (*sql.SQL, error) {
	if spec.DuplicateKey || spec.Ignore || spec.Conflict != nil {
		return nil, errors.New("oracle: on conflict, insert ignore, and on duplicate key update are not supported")
	}
	if len(spec.Returning) > 0 || len(spec.With) > 0 || spec.Select != nil {
		return nil, errors.New("oracle: insert only supports values")
	}
	if spec.Table == nil {
		return nil, errors.New("oracle: insert is missing a table")
	}
	if len(spec.ColumnNames) == 0 {
		return nil, errors.New("oracle: insert is missing columns")
	}
	if len(spec.Rows) == 0 {
		return nil, errors.New("oracle: insert is missing values")
	}
	for i, name := range spec.ColumnNames {
		if name == "" {
			return nil, fmt.Errorf("oracle: insert column %d has an empty name", i+1)
		}
	}
	for i, row := range spec.Rows {
		if len(row) != len(spec.ColumnNames) {
			return nil, fmt.Errorf("oracle: insert row %d has %d values, columns has %d", i+1, len(row), len(spec.ColumnNames))
		}
	}
	parts := []any{sql.Raw("insert into "), spec.Table, sql.Raw(" (")}
	for i, name := range spec.ColumnNames {
		if i > 0 {
			parts = append(parts, sql.Raw(", "))
		}
		parts = append(parts, sql.Identifier(name))
	}
	parts = append(parts, sql.Raw(") values "), insertRows(spec))
	return sql.S(parts...), nil
}

// BuildUpdateQuery compiles an update spec into an SQL fragment.
func (Dialect) BuildUpdateQuery(spec query.UpdateSpec) (*sql.SQL, error) {
	if len(spec.With) > 0 || len(spec.Returning) > 0 || spec.From != nil || len(spec.Joins) > 0 || len(spec.OrderBy) > 0 || spec.LimitSet {
		return nil, errors.New("oracle: update only supports set and where")
	}
	if spec.Table == nil {
		return nil, errors.New("oracle: update is missing a table")
	}
	if len(spec.Sets) == 0 {
		return nil, errors.New("oracle: update is missing set")
	}
	parts := []any{sql.Raw("update "), spec.Table, sql.Raw(" set ")}
	for i, set := range spec.Sets {
		if set.Column == "" {
			return nil, fmt.Errorf("oracle: update assignment %d has an empty column name", i+1)
		}
		if i > 0 {
			parts = append(parts, sql.Raw(", "))
		}
		parts = append(parts, sql.Identifier(set.Column), sql.Raw(" = "), bindValue(set.Value, set.Encoder))
	}
	if spec.Where != nil {
		parts = append(parts, sql.Raw(" where "), spec.Where)
	}
	return sql.S(parts...), nil
}

// BuildDeleteQuery compiles a delete spec into an SQL fragment.
// With no where, the statement deletes the whole table.
func (Dialect) BuildDeleteQuery(spec query.DeleteSpec) (*sql.SQL, error) {
	if len(spec.With) > 0 || len(spec.Returning) > 0 || spec.Using != nil || len(spec.Joins) > 0 || len(spec.OrderBy) > 0 || spec.LimitSet {
		return nil, errors.New("oracle: delete only supports where")
	}
	if spec.Table == nil {
		return nil, errors.New("oracle: delete is missing a table")
	}
	parts := []any{sql.Raw("delete from "), spec.Table}
	if spec.Where != nil {
		parts = append(parts, sql.Raw(" where "), spec.Where)
	}
	return sql.S(parts...), nil
}

func insertRows(spec query.InsertSpec) *sql.ValueRows {
	cells := make([]any, 0, len(spec.Rows)*len(spec.ColumnNames))
	for _, row := range spec.Rows {
		cells = append(cells, row...)
	}
	encoders := spec.Encoders
	if len(encoders) != len(spec.ColumnNames) {
		encoders = make([]sql.Encoder, len(spec.ColumnNames))
	}
	return &sql.ValueRows{Cells: cells, Encoders: encoders}
}

// bindValue is used inside sql.S. A bare nil is skipped by S, so NULL must be a Param.
func bindValue(v any, enc sql.Encoder) any {
	if _, ok := v.(sql.Chunk); ok {
		if ph, isPH := v.(*sql.Placeholder); isPH && enc != nil && !enc.IsNoop() {
			return sql.NewParam(ph, enc)
		}
		return v
	}
	if v == nil {
		return sql.NewParam(nil, nil)
	}
	if enc == nil || enc.IsNoop() {
		return v
	}
	return sql.NewParam(v, enc)
}
