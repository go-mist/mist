package mssql

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
// TOP is written as select top (n). OFFSET / FETCH is written only after ORDER BY, and cannot share a select with TOP.
// FETCH always follows OFFSET, using 0 when only a limit was set.
// DISTINCT ON, FOR UPDATE, FOR SHARE, and the RECURSIVE keyword are not written. A recursive CTE is still WITH.
func (d Dialect) BuildSelectQuery(spec query.SelectSpec) (*sql.SQL, error) {
	built, err := d.assembleSelect(spec)
	if err != nil {
		return nil, err
	}
	return d.expandSQL(built)
}

func (d Dialect) assembleSelect(spec query.SelectSpec) (*sql.SQL, error) {
	if spec.SetErr != "" {
		return nil, errors.New(spec.SetErr)
	}
	prefix, err := d.withPrefix(spec.With)
	if err != nil {
		return nil, err
	}
	left, err := d.buildOneSelect(spec)
	if err != nil {
		return nil, err
	}
	if len(spec.Sets) == 0 {
		if len(prefix) == 0 {
			return left, nil
		}
		return sql.S(append(prefix, left)...), nil
	}
	query := left
	for _, set := range spec.Sets {
		if err := sameColumns(spec.Columns, set.Right.Columns); err != nil {
			return nil, err
		}
		keyword, err := setKeyword(set.Op)
		if err != nil {
			return nil, err
		}
		right, err := d.assembleSelect(set.Right)
		if err != nil {
			return nil, err
		}
		parts := []any{sql.Raw("("), query, sql.Raw(") " + keyword + " ("), right, sql.Raw(")")}
		if (set.LimitSet || set.OffsetSet) && len(set.OrderBy) == 0 {
			return nil, errors.New("mssql: offset and fetch require order by")
		}
		if len(set.OrderBy) > 0 {
			parts = append(parts, sql.Raw(" order by "))
			for i, expr := range set.OrderBy {
				if i > 0 {
					parts = append(parts, sql.Raw(", "))
				}
				parts = append(parts, unqualifyCols(expr))
			}
		}
		parts = appendFetch(parts, set.Offset, set.OffsetSet, set.Limit, set.LimitSet)
		query = sql.S(parts...)
	}
	if len(prefix) == 0 {
		return query, nil
	}
	return sql.S(append(prefix, query)...), nil
}

func (d Dialect) withPrefix(list []query.WithSpec) ([]any, error) {
	if len(list) == 0 {
		return nil, nil
	}
	parts := []any{sql.Raw("with ")}
	for i, item := range list {
		if item.Name == "" {
			return nil, errors.New("mssql: cte is missing a name")
		}
		body, err := d.assembleSelect(item.Query)
		if err != nil {
			return nil, err
		}
		if i > 0 {
			parts = append(parts, sql.Raw(", "))
		}
		parts = append(parts, sql.Identifier(item.Name), sql.Raw(" as ("), body, sql.Raw(")"))
	}
	parts = append(parts, sql.Raw(" "))
	return parts, nil
}

func (d Dialect) buildOneSelect(spec query.SelectSpec) (*sql.SQL, error) {
	if spec.From == nil {
		return nil, errors.New("mssql: select is missing from")
	}
	if err := unaliasedSelect(spec.From); err != nil {
		return nil, err
	}
	if len(spec.Columns) == 0 {
		return nil, errors.New("mssql: select is missing columns")
	}
	if len(spec.DistinctOn) > 0 {
		return nil, errors.New("mssql: distinct on is not supported")
	}
	if spec.Lock != nil {
		return nil, errors.New("mssql: for update and for share are not supported")
	}
	if spec.TopSet && spec.Top < 0 {
		return nil, errors.New("mssql: top must be zero or greater")
	}
	if spec.TopSet && (spec.LimitSet || spec.OffsetSet) {
		return nil, errors.New("mssql: top cannot be combined with offset or fetch")
	}
	if (spec.LimitSet || spec.OffsetSet) && len(spec.OrderBy) == 0 {
		return nil, errors.New("mssql: offset and fetch require order by")
	}
	parts := make([]any, 0, 8+len(spec.Columns)*2+len(spec.Joins)*4)
	head := "select"
	if spec.Distinct {
		head += " distinct"
	}
	if spec.TopSet {
		head += " top (" + itoa(spec.Top) + ")"
	}
	parts = append(parts, sql.Raw(head+" "))
	for i, col := range spec.Columns {
		if i > 0 {
			parts = append(parts, sql.Raw(", "))
		}
		parts = append(parts, col)
	}
	parts = append(parts, sql.Raw(" from "), spec.From)
	for _, j := range spec.Joins {
		if j.Table == nil {
			return nil, errors.New("mssql: join is missing a table")
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
			return nil, fmt.Errorf("mssql: %s is missing on", keyword)
		}
		parts = append(parts, sql.Raw(" on "), j.On)
	}
	if spec.Where != nil {
		parts = append(parts, sql.Raw(" where "), spec.Where)
	}
	if len(spec.GroupBy) > 0 {
		parts = append(parts, sql.Raw(" group by "))
		for i, expr := range spec.GroupBy {
			if i > 0 {
				parts = append(parts, sql.Raw(", "))
			}
			parts = append(parts, expr)
		}
	}
	if spec.Having != nil {
		parts = append(parts, sql.Raw(" having "), spec.Having)
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
	parts = appendFetch(parts, spec.Offset, spec.OffsetSet, spec.Limit, spec.LimitSet)
	return sql.S(parts...), nil
}

func appendFetch(parts []any, offset int, offsetSet bool, limit int, limitSet bool) []any {
	if !offsetSet && !limitSet {
		return parts
	}
	if offsetSet {
		parts = append(parts, sql.Raw(" offset "), offset, sql.Raw(" rows"))
	} else {
		parts = append(parts, sql.Raw(" offset 0 rows"))
	}
	if limitSet {
		parts = append(parts, sql.Raw(" fetch next "), limit, sql.Raw(" rows only"))
	}
	return parts
}

func setKeyword(op string) (string, error) {
	switch op {
	case "union", "union all", "intersect", "intersect all", "except", "except all":
		return op, nil
	default:
		return "", fmt.Errorf("mssql: unknown set operation %s", op)
	}
}

func sameColumns(left, right []sql.Chunk) error {
	if len(left) != len(right) {
		return fmt.Errorf("mssql: set operation column counts differ, left %d, right %d", len(left), len(right))
	}
	for i := range left {
		ln, lok := colNameOf(left[i])
		rn, rok := colNameOf(right[i])
		if lok && rok && ln != rn {
			return fmt.Errorf("mssql: set operation column %d is %s and %s", i+1, ln, rn)
		}
	}
	return nil
}

func unqualifyCols(expr sql.Chunk) sql.Chunk {
	if d, ok := expr.(interface{ Direction() (sql.Chunk, string) }); ok {
		inner, dir := d.Direction()
		return sql.S(unqualifyCols(inner), sql.Fragment(dir))
	}
	if name, ok := colNameOf(expr); ok {
		return sql.Identifier(name)
	}
	q, ok := expr.(*sql.SQL)
	if !ok || q == nil {
		return expr
	}
	parts := make([]any, len(q.QueryChunks))
	for i, ch := range q.QueryChunks {
		if name, ok := colNameOf(ch); ok {
			parts[i] = sql.Identifier(name)
			continue
		}
		if _, ok := ch.(*sql.SQL); ok {
			parts[i] = unqualifyCols(ch)
			continue
		}
		parts[i] = ch
	}
	return sql.S(parts...)
}

func colNameOf(ch sql.Chunk) (string, bool) {
	named, ok := ch.(interface{ ColName() string })
	if !ok || named == nil {
		return "", false
	}
	return named.ColName(), true
}

func joinKeyword(kind query.JoinKind) (string, bool, error) {
	switch kind {
	case query.InnerJoin:
		return "inner join", true, nil
	case query.LeftJoin:
		return "left join", true, nil
	case query.RightJoin:
		return "right join", true, nil
	case query.FullJoin:
		return "full join", true, nil
	case query.CrossJoin:
		return "cross join", false, nil
	default:
		return "", false, fmt.Errorf("mssql: unknown join %d", kind)
	}
}

// BuildInsertQuery compiles an insert spec into an SQL fragment.
// RETURNING, ON CONFLICT, INSERT IGNORE, and ON DUPLICATE KEY UPDATE are rejected.
// A WITH on an insert select is written in front of INSERT. Output writes OUTPUT INSERTED and is not a stand-in for RETURNING.
func (d Dialect) BuildInsertQuery(spec query.InsertSpec) (*sql.SQL, error) {
	if spec.DuplicateKey || spec.Ignore || spec.Conflict != nil {
		return nil, errors.New("mssql: on conflict, insert ignore, and on duplicate key update are not supported")
	}
	if len(spec.Returning) > 0 {
		return nil, errors.New("mssql: returning is not supported")
	}
	if spec.HasOutput && len(spec.Output) == 0 {
		return nil, errors.New("mssql: output is missing columns")
	}
	if spec.Table == nil {
		return nil, errors.New("mssql: insert is missing a table")
	}
	if len(spec.ColumnNames) == 0 {
		return nil, errors.New("mssql: insert is missing columns")
	}
	if spec.Select != nil && len(spec.Rows) > 0 {
		return nil, errors.New("mssql: insert cannot use values and select together")
	}
	if spec.Select == nil && len(spec.Rows) == 0 {
		return nil, errors.New("mssql: insert is missing values or select")
	}
	if len(spec.With) > 0 && spec.Select == nil {
		return nil, errors.New("mssql: insert values cannot use with; use insert select")
	}
	if spec.Select != nil && len(spec.Select.Columns) != len(spec.ColumnNames) {
		return nil, fmt.Errorf("mssql: insert select has %d columns, column list has %d", len(spec.Select.Columns), len(spec.ColumnNames))
	}
	for i, name := range spec.ColumnNames {
		if name == "" {
			return nil, fmt.Errorf("mssql: insert column %d has an empty name", i+1)
		}
	}
	for i, row := range spec.Rows {
		if len(row) != len(spec.ColumnNames) {
			return nil, fmt.Errorf("mssql: insert row %d has %d values, columns has %d", i+1, len(row), len(spec.ColumnNames))
		}
	}
	head := []any{sql.Raw("insert into "), spec.Table, sql.Raw(" (")}
	for i, name := range spec.ColumnNames {
		if i > 0 {
			head = append(head, sql.Raw(", "))
		}
		head = append(head, sql.Identifier(name))
	}
	head = append(head, sql.Raw(")"))
	head, err := appendOutput(head, "inserted", spec.Output)
	if err != nil {
		return nil, err
	}
	if spec.Select != nil {
		sel := *spec.Select
		with := append(append([]query.WithSpec{}, spec.With...), sel.With...)
		sel.With = nil
		body, err := d.assembleSelect(sel)
		if err != nil {
			return nil, err
		}
		prefix, err := d.withPrefix(with)
		if err != nil {
			return nil, err
		}
		parts := append(prefix, head...)
		parts = append(parts, sql.Raw(" "), body)
		return d.expandSQL(sql.S(parts...))
	}
	head = append(head, sql.Raw(" values "), insertRows(spec))
	return d.expandSQL(sql.S(head...))
}

// BuildUpdateQuery compiles an update spec into an SQL fragment.
// FROM and joins use SQL Server's UPDATE ... FROM form. ORDER BY, LIMIT, and RETURNING are rejected.
func (d Dialect) BuildUpdateQuery(spec query.UpdateSpec) (*sql.SQL, error) {
	if len(spec.Returning) > 0 {
		return nil, errors.New("mssql: returning is not supported")
	}
	if (spec.HasOutputInserted || spec.HasOutputDeleted) && len(spec.OutputInserted)+len(spec.OutputDeleted) == 0 {
		return nil, errors.New("mssql: output is missing columns")
	}
	if len(spec.OrderBy) > 0 || spec.LimitSet {
		return nil, errors.New("mssql: update has no order by or limit")
	}
	if spec.Table == nil {
		return nil, errors.New("mssql: update is missing a table")
	}
	if len(spec.Sets) == 0 {
		return nil, errors.New("mssql: update is missing set")
	}
	prefix, err := d.withPrefix(spec.With)
	if err != nil {
		return nil, err
	}
	parts := append(prefix, sql.Raw("update "), spec.Table, sql.Raw(" set "))
	for i, set := range spec.Sets {
		if set.Column == "" {
			return nil, fmt.Errorf("mssql: update assignment %d has an empty column name", i+1)
		}
		if i > 0 {
			parts = append(parts, sql.Raw(", "))
		}
		parts = append(parts, sql.Identifier(set.Column), sql.Raw(" = "), bindValue(set.Value, set.Encoder))
	}
	parts, err = appendUpdateOutput(parts, spec.OutputInserted, spec.OutputDeleted)
	if err != nil {
		return nil, err
	}
	if err := unaliasedSelect(spec.From); err != nil {
		return nil, err
	}
	if len(spec.Joins) > 0 {
		parts = append(parts, sql.Raw(" from "), spec.Table)
		if err := appendJoins(&parts, spec.Joins); err != nil {
			return nil, err
		}
		if spec.From != nil {
			parts = append(parts, sql.Raw(" cross join "), spec.From)
		}
	} else if spec.From != nil {
		parts = append(parts, sql.Raw(" from "), spec.From)
	}
	if spec.Where != nil {
		parts = append(parts, sql.Raw(" where "), spec.Where)
	}
	return d.expandSQL(sql.S(parts...))
}

// BuildDeleteQuery compiles a delete spec into an SQL fragment.
// With no where, the statement deletes the whole table. USING and joins use DELETE ... FROM.
func (d Dialect) BuildDeleteQuery(spec query.DeleteSpec) (*sql.SQL, error) {
	if len(spec.Returning) > 0 {
		return nil, errors.New("mssql: returning is not supported")
	}
	if spec.HasOutput && len(spec.Output) == 0 {
		return nil, errors.New("mssql: output is missing columns")
	}
	if len(spec.OrderBy) > 0 || spec.LimitSet {
		return nil, errors.New("mssql: delete has no order by or limit")
	}
	if spec.Table == nil {
		return nil, errors.New("mssql: delete is missing a table")
	}
	prefix, err := d.withPrefix(spec.With)
	if err != nil {
		return nil, err
	}
	parts := append(prefix, sql.Raw("delete from "), spec.Table)
	parts, err = appendOutput(parts, "deleted", spec.Output)
	if err != nil {
		return nil, err
	}
	if err := unaliasedSelect(spec.Using); err != nil {
		return nil, err
	}
	if len(spec.Joins) > 0 {
		parts = append(parts, sql.Raw(" from "), spec.Table)
		if err := appendJoins(&parts, spec.Joins); err != nil {
			return nil, err
		}
		if spec.Using != nil {
			parts = append(parts, sql.Raw(" cross join "), spec.Using)
		}
	} else if spec.Using != nil {
		parts = append(parts, sql.Raw(" from "), spec.Using)
	}
	if spec.Where != nil {
		parts = append(parts, sql.Raw(" where "), spec.Where)
	}
	return d.expandSQL(sql.S(parts...))
}

func appendOutput(parts []any, kind string, cols []sql.Chunk) ([]any, error) {
	if len(cols) == 0 {
		return parts, nil
	}
	parts = append(parts, sql.Raw(" output "))
	for i, col := range cols {
		name, ok := colNameOf(col)
		if !ok || name == "" {
			return nil, errors.New("mssql: output column has no name")
		}
		if i > 0 {
			parts = append(parts, sql.Raw(", "))
		}
		parts = append(parts, sql.Raw(kind+"."), sql.Identifier(name))
	}
	return parts, nil
}

func appendUpdateOutput(parts []any, inserted, deleted []sql.Chunk) ([]any, error) {
	if len(inserted) == 0 && len(deleted) == 0 {
		return parts, nil
	}
	parts = append(parts, sql.Raw(" output "))
	n := 0
	write := func(kind string, cols []sql.Chunk) error {
		for _, col := range cols {
			name, ok := colNameOf(col)
			if !ok || name == "" {
				return errors.New("mssql: output column has no name")
			}
			if n > 0 {
				parts = append(parts, sql.Raw(", "))
			}
			n++
			parts = append(parts, sql.Raw(kind+"."), sql.Identifier(name))
		}
		return nil
	}
	if err := write("inserted", inserted); err != nil {
		return nil, err
	}
	if err := write("deleted", deleted); err != nil {
		return nil, err
	}
	return parts, nil
}

func appendJoins(parts *[]any, joins []query.Join) error {
	for _, j := range joins {
		if j.Table == nil {
			return errors.New("mssql: join is missing a table")
		}
		if err := unaliasedSelect(j.Table); err != nil {
			return err
		}
		keyword, needsOn, err := joinKeyword(j.Kind)
		if err != nil {
			return err
		}
		*parts = append(*parts, sql.Raw(" "+keyword+" "), j.Table)
		if !needsOn {
			continue
		}
		if j.On == nil {
			return fmt.Errorf("mssql: %s is missing on", keyword)
		}
		*parts = append(*parts, sql.Raw(" on "), j.On)
	}
	return nil
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
