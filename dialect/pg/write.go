package pg

import (
	"errors"
	"fmt"

	"github.com/go-mist/mist/query"
	"github.com/go-mist/mist/sql"
)

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

// BuildInsertQuery compiles an insert spec into an SQL fragment.
func (Dialect) BuildInsertQuery(spec query.InsertSpec) (*sql.SQL, error) {
	if spec.DuplicateKey {
		return nil, errors.New("pg: on duplicate key update is not supported")
	}
	if spec.Ignore {
		return nil, errors.New("pg: insert ignore is not supported")
	}
	if spec.HasOutput || len(spec.Output) > 0 {
		return nil, errors.New("pg: output is not supported")
	}
	if spec.Table == nil {
		return nil, errors.New("pg: insert is missing a table")
	}
	if len(spec.ColumnNames) == 0 {
		return nil, errors.New("pg: insert is missing columns")
	}
	if spec.Select != nil && len(spec.Rows) > 0 {
		return nil, errors.New("pg: insert cannot use values and select together")
	}
	if spec.Select == nil && len(spec.Rows) == 0 {
		return nil, errors.New("pg: insert is missing values or select")
	}
	if spec.Select != nil && len(spec.Select.Columns) != len(spec.ColumnNames) {
		return nil, fmt.Errorf("pg: insert select has %d columns, column list has %d", len(spec.Select.Columns), len(spec.ColumnNames))
	}
	for i, name := range spec.ColumnNames {
		if name == "" {
			return nil, fmt.Errorf("pg: insert column %d has an empty name", i+1)
		}
	}
	for i, row := range spec.Rows {
		if len(row) != len(spec.ColumnNames) {
			return nil, fmt.Errorf("pg: insert row %d has %d values, columns has %d", i+1, len(row), len(spec.ColumnNames))
		}
	}

	parts, err := Dialect{}.prependWith(spec.With, sql.Raw("insert into "), spec.Table, sql.Raw(" ("))
	if err != nil {
		return nil, err
	}
	for i, name := range spec.ColumnNames {
		if i > 0 {
			parts = append(parts, sql.Raw(", "))
		}
		parts = append(parts, sql.Identifier(name))
	}
	parts = append(parts, sql.Raw(")"))
	if spec.Select != nil {
		body, err := Dialect{}.BuildSelectQuery(*spec.Select)
		if err != nil {
			return nil, err
		}
		parts = append(parts, sql.Raw(" "), body)
	} else {
		parts = append(parts, sql.Raw(" values "), insertRows(spec))
	}
	conflict, err := conflictClause(spec.Conflict)
	if err != nil {
		return nil, err
	}
	parts = append(parts, conflict...)
	parts = appendReturning(parts, spec.Returning)
	return Dialect{}.expandSQL(sql.S(parts...))
}

// BuildUpdateQuery compiles an update spec into an SQL fragment.
// The left-hand side of set is only the column name. PostgreSQL does not accept a table-name prefix.
func (Dialect) BuildUpdateQuery(spec query.UpdateSpec) (*sql.SQL, error) {
	if spec.Table == nil {
		return nil, errors.New("pg: update is missing a table")
	}
	if len(spec.OrderBy) > 0 || spec.LimitSet {
		return nil, errors.New("pg: update has no order by or limit")
	}
	if spec.HasOutputInserted || spec.HasOutputDeleted || len(spec.OutputInserted) > 0 || len(spec.OutputDeleted) > 0 {
		return nil, errors.New("pg: output is not supported")
	}
	if len(spec.Sets) == 0 {
		return nil, errors.New("pg: update is missing set")
	}
	parts, err := Dialect{}.prependWith(spec.With, sql.Raw("update "), spec.Table, sql.Raw(" set "))
	if err != nil {
		return nil, err
	}
	for i, set := range spec.Sets {
		if set.Column == "" {
			return nil, fmt.Errorf("pg: update assignment %d has an empty column name", i+1)
		}
		if i > 0 {
			parts = append(parts, sql.Raw(", "))
		}
		parts = append(parts, sql.Identifier(set.Column), sql.Raw(" = "), bindValue(set.Value, set.Encoder))
	}
	if len(spec.Joins) > 0 && spec.From == nil {
		return nil, errors.New("pg: update join requires from")
	}
	if err := unaliasedSelect(spec.From); err != nil {
		return nil, err
	}
	if spec.From != nil {
		parts = append(parts, sql.Raw(" from "), spec.From)
	}
	for _, j := range spec.Joins {
		if j.Table == nil {
			return nil, errors.New("pg: join is missing a table")
		}
		if err := unaliasedSelect(j.Table); err != nil {
			return nil, err
		}
		keyword, needsOn, err := joinKeyword(j.Kind)
		if err != nil {
			return nil, err
		}
		parts = append(parts, sql.Raw(" "+keyword+" "), j.Table)
		if !needsOn {
			continue
		}
		if j.On == nil {
			return nil, fmt.Errorf("pg: %s is missing on", keyword)
		}
		parts = append(parts, sql.Raw(" on "), j.On)
	}
	if spec.Where != nil {
		parts = append(parts, sql.Raw(" where "), spec.Where)
	}
	parts = appendReturning(parts, spec.Returning)
	return Dialect{}.expandSQL(sql.S(parts...))
}

// BuildDeleteQuery compiles a delete spec into an SQL fragment.
// Without a where the statement deletes the whole table; omitting Where means that.
func (Dialect) BuildDeleteQuery(spec query.DeleteSpec) (*sql.SQL, error) {
	if spec.Table == nil {
		return nil, errors.New("pg: delete is missing a table")
	}
	if len(spec.OrderBy) > 0 || spec.LimitSet {
		return nil, errors.New("pg: delete has no order by or limit")
	}
	if spec.HasOutput || len(spec.Output) > 0 {
		return nil, errors.New("pg: output is not supported")
	}
	parts, err := Dialect{}.prependWith(spec.With, sql.Raw("delete from "), spec.Table)
	if err != nil {
		return nil, err
	}
	if len(spec.Joins) > 0 && spec.Using == nil {
		return nil, errors.New("pg: delete join requires using")
	}
	if err := unaliasedSelect(spec.Using); err != nil {
		return nil, err
	}
	if spec.Using != nil {
		parts = append(parts, sql.Raw(" using "), spec.Using)
	}
	for _, j := range spec.Joins {
		if j.Table == nil {
			return nil, errors.New("pg: join is missing a table")
		}
		if err := unaliasedSelect(j.Table); err != nil {
			return nil, err
		}
		keyword, needsOn, err := joinKeyword(j.Kind)
		if err != nil {
			return nil, err
		}
		parts = append(parts, sql.Raw(" "+keyword+" "), j.Table)
		if !needsOn {
			continue
		}
		if j.On == nil {
			return nil, fmt.Errorf("pg: %s is missing on", keyword)
		}
		parts = append(parts, sql.Raw(" on "), j.On)
	}
	if spec.Where != nil {
		parts = append(parts, sql.Raw(" where "), spec.Where)
	}
	parts = appendReturning(parts, spec.Returning)
	return Dialect{}.expandSQL(sql.S(parts...))
}

func (d Dialect) prependWith(list []query.WithSpec, head ...any) ([]any, error) {
	prefix, err := d.withPrefix(list)
	if err != nil {
		return nil, err
	}
	if len(prefix) == 0 {
		return head, nil
	}
	return append(prefix, head...), nil
}

// Excluded builds excluded."column", the value this insert proposes to write.
func Excluded(col interface{ ColName() string }) *sql.SQL {
	if col == nil {
		return sql.Raw("excluded.")
	}
	return sql.S(sql.Raw("excluded."), sql.Identifier(col.ColName()))
}

func conflictClause(c *query.InsertConflict) ([]any, error) {
	if c == nil {
		return nil, nil
	}
	if c.Nothing {
		if len(c.Targets) == 0 && c.TargetWhere == nil {
			return []any{sql.Raw(" on conflict do nothing")}, nil
		}
		parts := []any{sql.Raw(" on conflict (")}
		target, err := conflictTargets(c.Targets)
		if err != nil {
			return nil, err
		}
		parts = append(parts, target...)
		parts = append(parts, sql.Raw(")"))
		if c.TargetWhere != nil {
			parts = append(parts, sql.Raw(" where "), c.TargetWhere)
		}
		parts = append(parts, sql.Raw(" do nothing"))
		return parts, nil
	}
	if len(c.Targets) == 0 {
		return nil, errors.New("pg: on conflict do update is missing a target column")
	}
	if len(c.Sets) == 0 {
		return nil, errors.New("pg: on conflict do update is missing set")
	}
	parts := []any{sql.Raw(" on conflict (")}
	target, err := conflictTargets(c.Targets)
	if err != nil {
		return nil, err
	}
	parts = append(parts, target...)
	parts = append(parts, sql.Raw(")"))
	if c.TargetWhere != nil {
		parts = append(parts, sql.Raw(" where "), c.TargetWhere)
	}
	parts = append(parts, sql.Raw(" do update set "))
	for i, set := range c.Sets {
		if set.Column == "" {
			return nil, fmt.Errorf("pg: on conflict assignment %d has an empty column name", i+1)
		}
		if i > 0 {
			parts = append(parts, sql.Raw(", "))
		}
		parts = append(parts, sql.Identifier(set.Column), sql.Raw(" = "), bindValue(set.Value, set.Encoder))
	}
	if c.SetWhere != nil {
		parts = append(parts, sql.Raw(" where "), c.SetWhere)
	}
	return parts, nil
}

func conflictTargets(names []string) ([]any, error) {
	if len(names) == 0 {
		return nil, errors.New("pg: on conflict is missing a target column")
	}
	parts := make([]any, 0, len(names)*2)
	for i, name := range names {
		if name == "" {
			return nil, fmt.Errorf("pg: on conflict target column %d has an empty name", i+1)
		}
		if i > 0 {
			parts = append(parts, sql.Raw(", "))
		}
		parts = append(parts, sql.Identifier(name))
	}
	return parts, nil
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
	return &sql.ValueRows{
		Cells:    cells,
		Encoders: encoders,
	}
}

func appendReturning(parts []any, cols []sql.Chunk) []any {
	if len(cols) == 0 {
		return parts
	}
	parts = append(parts, sql.Raw(" returning "))
	for i, col := range cols {
		if i > 0 {
			parts = append(parts, sql.Raw(", "))
		}
		parts = append(parts, col)
	}
	return parts
}

// bindValue is used inside sql.S. A bare nil is skipped by S, so NULL must be a Param.
// When the column has an encoder, the parameter carries it so rendering adds the type suffix.
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
