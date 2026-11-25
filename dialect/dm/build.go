package dm

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
// LIMIT and OFFSET are written in that order. ORDER BY is not required.
// DISTINCT ON, FOR SHARE, and TOP are rejected. A recursive CTE is still WITH, without the RECURSIVE keyword.
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
		right, err := d.BuildSelectQuery(set.Right)
		if err != nil {
			return nil, err
		}
		parts := []any{sql.Raw("("), query, sql.Raw(") " + keyword + " ("), right, sql.Raw(")")}
		if len(set.OrderBy) > 0 {
			parts = append(parts, sql.Raw(" order by "))
			for i, expr := range set.OrderBy {
				if i > 0 {
					parts = append(parts, sql.Raw(", "))
				}
				parts = append(parts, unqualifyCols(expr))
			}
		}
		parts = appendLimit(parts, set.Offset, set.OffsetSet, set.Limit, set.LimitSet)
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
			return nil, errors.New("dm: cte is missing a name")
		}
		body, err := d.BuildSelectQuery(item.Query)
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
		return nil, errors.New("dm: select is missing from")
	}
	if err := unaliasedSelect(spec.From); err != nil {
		return nil, err
	}
	if len(spec.Columns) == 0 {
		return nil, errors.New("dm: select is missing columns")
	}
	if spec.TopSet {
		return nil, errors.New("dm: top is not supported")
	}
	if len(spec.DistinctOn) > 0 {
		return nil, errors.New("dm: distinct on is not supported")
	}
	if spec.Lock != nil && spec.Lock.Share {
		return nil, errors.New("dm: for share is not supported")
	}
	parts := make([]any, 0, 8+len(spec.Columns)*2+len(spec.Joins)*4)
	if spec.Distinct {
		parts = append(parts, sql.Raw("select distinct "))
	} else {
		parts = append(parts, sql.Raw("select "))
	}
	for i, col := range spec.Columns {
		if i > 0 {
			parts = append(parts, sql.Raw(", "))
		}
		parts = append(parts, col)
	}
	parts = append(parts, sql.Raw(" from "), spec.From)
	for _, j := range spec.Joins {
		if j.Table == nil {
			return nil, errors.New("dm: join is missing a table")
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
			return nil, fmt.Errorf("dm: %s is missing on", keyword)
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
	parts = appendLimit(parts, spec.Offset, spec.OffsetSet, spec.Limit, spec.LimitSet)
	if spec.Lock != nil {
		if spec.Lock.NoWait && spec.Lock.SkipLocked {
			return nil, errors.New("dm: for update cannot set both nowait and skip locked")
		}
		parts = append(parts, sql.Raw(" for update"))
		if len(spec.Lock.Of) > 0 {
			parts = append(parts, sql.Raw(" of "))
			for i, table := range spec.Lock.Of {
				if i > 0 {
					parts = append(parts, sql.Raw(", "))
				}
				parts = append(parts, table)
			}
		}
		if spec.Lock.NoWait {
			parts = append(parts, sql.Raw(" nowait"))
		}
		if spec.Lock.SkipLocked {
			parts = append(parts, sql.Raw(" skip locked"))
		}
	}
	return sql.S(parts...), nil
}

func appendLimit(parts []any, offset int, offsetSet bool, limit int, limitSet bool) []any {
	if !limitSet && !offsetSet {
		return parts
	}
	if limitSet {
		parts = append(parts, sql.Raw(" limit "), limit)
	} else {
		parts = append(parts, sql.Raw(" limit 2147483647"))
	}
	if offsetSet {
		parts = append(parts, sql.Raw(" offset "), offset)
	}
	return parts
}

func setKeyword(op string) (string, error) {
	switch op {
	case "union", "union all", "intersect", "intersect all", "except", "except all":
		return op, nil
	default:
		return "", fmt.Errorf("dm: unknown set operation %s", op)
	}
}

func sameColumns(left, right []sql.Chunk) error {
	if len(left) != len(right) {
		return fmt.Errorf("dm: set operation column counts differ, left %d, right %d", len(left), len(right))
	}
	for i := range left {
		ln, lok := colNameOf(left[i])
		rn, rok := colNameOf(right[i])
		if lok && rok && ln != rn {
			return fmt.Errorf("dm: set operation column %d is %s and %s", i+1, ln, rn)
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
		return "", false, fmt.Errorf("dm: unknown join %d", kind)
	}
}

// BuildInsertQuery compiles an insert spec into an SQL fragment.
// RETURNING, ON CONFLICT, INSERT IGNORE, ON DUPLICATE KEY UPDATE, and OUTPUT are rejected.
// Several VALUES rows are written as one list. A WITH on an insert select is written in front of INSERT.
func (d Dialect) BuildInsertQuery(spec query.InsertSpec) (*sql.SQL, error) {
	if spec.DuplicateKey || spec.Ignore || spec.Conflict != nil {
		return nil, errors.New("dm: on conflict, insert ignore, and on duplicate key update are not supported")
	}
	if len(spec.Returning) > 0 {
		return nil, errors.New("dm: returning is not supported")
	}
	if spec.HasOutput || len(spec.Output) > 0 {
		return nil, errors.New("dm: output is not supported")
	}
	if spec.Table == nil {
		return nil, errors.New("dm: insert is missing a table")
	}
	if len(spec.ColumnNames) == 0 {
		return nil, errors.New("dm: insert is missing columns")
	}
	if spec.Select != nil && len(spec.Rows) > 0 {
		return nil, errors.New("dm: insert cannot use values and select together")
	}
	if spec.Select == nil && len(spec.Rows) == 0 {
		return nil, errors.New("dm: insert is missing values or select")
	}
	if len(spec.With) > 0 && spec.Select == nil {
		return nil, errors.New("dm: insert values cannot use with; use insert select")
	}
	if spec.Select != nil && len(spec.Select.Columns) != len(spec.ColumnNames) {
		return nil, fmt.Errorf("dm: insert select has %d columns, column list has %d", len(spec.Select.Columns), len(spec.ColumnNames))
	}
	for i, name := range spec.ColumnNames {
		if name == "" {
			return nil, fmt.Errorf("dm: insert column %d has an empty name", i+1)
		}
	}
	for i, row := range spec.Rows {
		if len(row) != len(spec.ColumnNames) {
			return nil, fmt.Errorf("dm: insert row %d has %d values, columns has %d", i+1, len(row), len(spec.ColumnNames))
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
func (Dialect) BuildUpdateQuery(spec query.UpdateSpec) (*sql.SQL, error) {
	if spec.HasOutputInserted || spec.HasOutputDeleted || len(spec.OutputInserted) > 0 || len(spec.OutputDeleted) > 0 {
		return nil, errors.New("dm: output is not supported")
	}
	if len(spec.With) > 0 || len(spec.Returning) > 0 || spec.From != nil || len(spec.Joins) > 0 || len(spec.OrderBy) > 0 || spec.LimitSet {
		return nil, errors.New("dm: update only supports set and where")
	}
	if spec.Table == nil {
		return nil, errors.New("dm: update is missing a table")
	}
	if len(spec.Sets) == 0 {
		return nil, errors.New("dm: update is missing set")
	}
	parts := []any{sql.Raw("update "), spec.Table, sql.Raw(" set ")}
	for i, set := range spec.Sets {
		if set.Column == "" {
			return nil, fmt.Errorf("dm: update assignment %d has an empty column name", i+1)
		}
		if i > 0 {
			parts = append(parts, sql.Raw(", "))
		}
		parts = append(parts, sql.Identifier(set.Column), sql.Raw(" = "), bindValue(set.Value, set.Encoder))
	}
	if spec.Where != nil {
		parts = append(parts, sql.Raw(" where "), spec.Where)
	}
	return Dialect{}.expandSQL(sql.S(parts...))
}

// BuildDeleteQuery compiles a delete spec into an SQL fragment.
// With no where, the statement deletes the whole table.
func (Dialect) BuildDeleteQuery(spec query.DeleteSpec) (*sql.SQL, error) {
	if spec.HasOutput || len(spec.Output) > 0 {
		return nil, errors.New("dm: output is not supported")
	}
	if len(spec.With) > 0 || len(spec.Returning) > 0 || spec.Using != nil || len(spec.Joins) > 0 || len(spec.OrderBy) > 0 || spec.LimitSet {
		return nil, errors.New("dm: delete only supports where")
	}
	if spec.Table == nil {
		return nil, errors.New("dm: delete is missing a table")
	}
	parts := []any{sql.Raw("delete from "), spec.Table}
	if spec.Where != nil {
		parts = append(parts, sql.Raw(" where "), spec.Where)
	}
	return Dialect{}.expandSQL(sql.S(parts...))
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
