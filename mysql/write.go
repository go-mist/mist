package mysql

import (
	"errors"
	"fmt"

	"github.com/xbmlz/mist/query"
	"github.com/xbmlz/mist/sql"
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
// on conflict and returning are not generated. on duplicate key update accepts only Set, with no target columns and no where.
func (d Dialect) BuildInsertQuery(spec query.InsertSpec) (*sql.SQL, error) {
	if spec.Table == nil {
		return nil, errors.New("mysql: insert is missing a table")
	}
	if len(spec.ColumnNames) == 0 {
		return nil, errors.New("mysql: insert is missing columns")
	}
	if spec.Conflict != nil && spec.DuplicateKey {
		return nil, errors.New("mysql: on duplicate key update cannot be combined with on conflict")
	}
	if spec.Conflict != nil {
		return nil, errors.New("mysql: on conflict is not generated. on duplicate key update is not the same thing")
	}
	if spec.DuplicateKey && len(spec.Duplicate) == 0 {
		return nil, errors.New("mysql: on duplicate key update is missing set")
	}
	if len(spec.Returning) > 0 {
		return nil, errors.New("mysql: returning is not supported")
	}
	if spec.Select != nil && len(spec.Rows) > 0 {
		return nil, errors.New("mysql: insert cannot use values and select together")
	}
	if spec.Select == nil && len(spec.Rows) == 0 {
		return nil, errors.New("mysql: insert is missing values or select")
	}
	if spec.Select != nil && len(spec.Select.Columns) != len(spec.ColumnNames) {
		return nil, fmt.Errorf("mysql: insert select has %d columns, column list has %d", len(spec.Select.Columns), len(spec.ColumnNames))
	}
	for i, name := range spec.ColumnNames {
		if name == "" {
			return nil, fmt.Errorf("mysql: insert column %d has an empty name", i+1)
		}
	}
	for i, row := range spec.Rows {
		if len(row) != len(spec.ColumnNames) {
			return nil, fmt.Errorf("mysql: insert row %d has %d values, columns has %d", i+1, len(row), len(spec.ColumnNames))
		}
	}
	if len(spec.With) > 0 && spec.Select == nil {
		return nil, errors.New("mysql: insert values cannot use with; use insert select")
	}
	verb := "insert into "
	if spec.Ignore {
		verb = "insert ignore into "
	}
	parts := []any{sql.Raw(verb), spec.Table, sql.Raw(" (")}
	for i, name := range spec.ColumnNames {
		if i > 0 {
			parts = append(parts, sql.Raw(", "))
		}
		parts = append(parts, sql.Identifier(name))
	}
	parts = append(parts, sql.Raw(")"))
	if spec.Select != nil {
		body, err := d.duplicateSelect(spec)
		if err != nil {
			return nil, err
		}
		prefix, err := d.withPrefix(spec.With)
		if err != nil {
			return nil, err
		}
		parts = append(parts, sql.Raw(" "))
		parts = append(parts, prefix...)
		parts = append(parts, body)
	} else {
		parts = append(parts, sql.Raw(" values "), insertRows(spec))
	}
	if spec.DuplicateKey {
		if spec.Select == nil {
			parts = append(parts, sql.Raw(" as "), sql.Identifier("newrow"))
		}
		parts = append(parts, sql.Raw(" on duplicate key update "))
		for i, set := range spec.Duplicate {
			if set.Column == "" {
				return nil, fmt.Errorf("mysql: on duplicate key update assignment %d has an empty column name", i+1)
			}
			if i > 0 {
				parts = append(parts, sql.Raw(", "))
			}
			parts = append(parts, sql.Identifier(set.Column), sql.Raw(" = "), bindValue(set.Value, set.Encoder))
		}
	}
	return d.expandSQL(sql.S(parts...))
}

// duplicateSelect builds the select that follows insert.
// With on duplicate key update, MySQL cannot place a row alias after select, so the select is wrapped and column names are aligned to the insert columns.
func (d Dialect) duplicateSelect(spec query.InsertSpec) (*sql.SQL, error) {
	bodySpec := *spec.Select
	if !spec.DuplicateKey {
		return d.BuildSelectQuery(bodySpec)
	}
	cols := make([]sql.Chunk, len(bodySpec.Columns))
	for i, col := range bodySpec.Columns {
		cols[i] = sql.S(col, sql.Raw(" as "), sql.Identifier(spec.ColumnNames[i]))
	}
	bodySpec.Columns = cols
	inner, err := d.BuildSelectQuery(bodySpec)
	if err != nil {
		return nil, err
	}
	parts := []any{sql.Raw("select ")}
	for i, name := range spec.ColumnNames {
		if i > 0 {
			parts = append(parts, sql.Raw(", "))
		}
		parts = append(parts, sql.Identifier("newrow"), sql.Raw("."), sql.Identifier(name))
	}
	parts = append(parts, sql.Raw(" from ("), inner, sql.Raw(") as "), sql.Identifier("newrow"))
	return sql.S(parts...), nil
}

// NewRow refers to the row this insert intends to write. It renders as `newrow`.`column`.
func NewRow(col interface{ ColName() string }) *sql.SQL {
	if col == nil {
		return sql.S(sql.Identifier("newrow"), sql.Raw("."))
	}
	return sql.S(sql.Identifier("newrow"), sql.Raw("."), sql.Identifier(col.ColName()))
}

// BuildUpdateQuery compiles an update spec into an SQL fragment.
func (Dialect) BuildUpdateQuery(spec query.UpdateSpec) (*sql.SQL, error) {
	if spec.Table == nil {
		return nil, errors.New("mysql: update is missing a table")
	}
	if len(spec.Sets) == 0 {
		return nil, errors.New("mysql: update is missing set")
	}
	if len(spec.Returning) > 0 {
		return nil, errors.New("mysql: returning is not supported")
	}
	if err := singleTableOrder("update", spec.From != nil || len(spec.Joins) > 0, spec.OrderBy, spec.LimitSet); err != nil {
		return nil, err
	}
	if len(spec.Joins) > 0 && spec.From == nil {
		return nil, errors.New("mysql: update join requires from")
	}
	head, err := mysqlJoined([]any{sql.Raw("update "), spec.Table}, spec.From, spec.Joins)
	if err != nil {
		return nil, err
	}
	parts, err := Dialect{}.prependWith(spec.With, head...)
	if err != nil {
		return nil, err
	}
	parts = append(parts, sql.Raw(" set "))
	for i, set := range spec.Sets {
		if set.Column == "" {
			return nil, fmt.Errorf("mysql: update assignment %d has an empty column name", i+1)
		}
		if i > 0 {
			parts = append(parts, sql.Raw(", "))
		}
		parts = append(parts, sql.Identifier(set.Column), sql.Raw(" = "), bindValue(set.Value, set.Encoder))
	}
	if spec.Where != nil {
		parts = append(parts, sql.Raw(" where "), spec.Where)
	}
	parts = writeOrderLimit(parts, spec.OrderBy, spec.Limit, spec.LimitSet)
	return Dialect{}.expandSQL(sql.S(parts...))
}

// BuildDeleteQuery compiles a delete spec into an SQL fragment.
func (Dialect) BuildDeleteQuery(spec query.DeleteSpec) (*sql.SQL, error) {
	if spec.Table == nil {
		return nil, errors.New("mysql: delete is missing a table")
	}
	if len(spec.Returning) > 0 {
		return nil, errors.New("mysql: returning is not supported")
	}
	if err := singleTableOrder("delete", spec.Using != nil || len(spec.Joins) > 0, spec.OrderBy, spec.LimitSet); err != nil {
		return nil, err
	}
	if len(spec.Joins) > 0 && spec.Using == nil {
		return nil, errors.New("mysql: delete join requires using")
	}
	var head []any
	if spec.Using == nil && len(spec.Joins) == 0 {
		head = []any{sql.Raw("delete from "), spec.Table}
	} else {
		var err error
		head, err = mysqlJoined([]any{sql.Raw("delete "), spec.Table, sql.Raw(" from "), spec.Table}, spec.Using, spec.Joins)
		if err != nil {
			return nil, err
		}
	}
	parts, err := Dialect{}.prependWith(spec.With, head...)
	if err != nil {
		return nil, err
	}
	if spec.Where != nil {
		parts = append(parts, sql.Raw(" where "), spec.Where)
	}
	parts = writeOrderLimit(parts, spec.OrderBy, spec.Limit, spec.LimitSet)
	return Dialect{}.expandSQL(sql.S(parts...))
}

func singleTableOrder(kind string, multi bool, order []sql.Chunk, limitSet bool) error {
	if multi && (len(order) > 0 || limitSet) {
		return fmt.Errorf("mysql: multi-table %s cannot use order by or limit", kind)
	}
	return nil
}

func writeOrderLimit(parts []any, order []sql.Chunk, limit int, limitSet bool) []any {
	if len(order) > 0 {
		parts = append(parts, sql.Raw(" order by "))
		for i, expr := range order {
			if i > 0 {
				parts = append(parts, sql.Raw(", "))
			}
			parts = append(parts, expr)
		}
	}
	if limitSet {
		parts = append(parts, sql.Raw(" limit "), limit)
	}
	return parts
}

// mysqlJoined writes extra tables as comma sources, then appends joins.
// Multi-table update / delete on MySQL has no PostgreSQL placement for the from / using keywords.
func mysqlJoined(parts []any, extra sql.Chunk, joins []query.Join) ([]any, error) {
	if extra == nil && len(joins) == 0 {
		return parts, nil
	}
	if err := unaliasedSelect(extra); err != nil {
		return nil, err
	}
	if extra != nil {
		parts = append(parts, sql.Raw(", "), extra)
	}
	for _, j := range joins {
		if j.Table == nil {
			return nil, errors.New("mysql: join is missing a table")
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
			return nil, fmt.Errorf("mysql: %s is missing on", keyword)
		}
		parts = append(parts, sql.Raw(" on "), j.On)
	}
	return parts, nil
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
