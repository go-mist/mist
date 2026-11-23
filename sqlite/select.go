package sqlite

import (
	"errors"
	"fmt"

	"github.com/xbmlz/mist/query"
	"github.com/xbmlz/mist/sql"
)

// Select starts a query. Passing no columns means the whole row; the column list is taken from the table at From.
func Select[T any](cols ...query.Column[T]) query.Select[T] {
	return query.NewSelect(cols...)
}

// Value starts a single-column query. The result is scanned directly into T.
// Read an aggregate with this: the row type does not come from a table.
func Value[T any](expr sql.Chunk) query.ScanQuery[T] {
	return query.NewValue[T](expr)
}

// Scan starts a multi-column query. scan scans one row into T in the order of cols.
func Scan[T any](scan func(sql.Row, *T) error, cols ...sql.Chunk) query.ScanQuery[T] {
	return query.NewScan(scan, cols...)
}

// BuildSelectQuery compiles a select spec into an SQL fragment.
//
// Render writes the final text of identifiers and parameters, escaped for this dialect. This only decides clause shape.
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
		// modernc SQLite rejects a statement that starts with '(', so neither side is parenthesized.
		// UNION / EXCEPT still associate left to right in written order. INTERSECT has higher precedence. That is SQLite's own rule.
		parts := []any{query, sql.Raw(" " + keyword + " "), right}
		if len(set.OrderBy) > 0 {
			parts = append(parts, sql.Raw(" order by "))
			for i, expr := range set.OrderBy {
				if i > 0 {
					parts = append(parts, sql.Raw(", "))
				}
				parts = append(parts, unqualifyCols(expr))
			}
		}
		if set.LimitSet {
			parts = append(parts, sql.Raw(" limit "), set.Limit)
		}
		if set.OffsetSet {
			parts = append(parts, sql.Raw(" offset "), set.Offset)
		}
		query = sql.S(parts...)
	}
	if len(prefix) == 0 {
		return query, nil
	}
	return sql.S(append(prefix, query)...), nil
}

func (Dialect) withPrefix(list []query.WithSpec) ([]any, error) {
	if len(list) == 0 {
		return nil, nil
	}
	recursive := false
	for _, item := range list {
		if item.Recursive {
			recursive = true
		}
	}
	keyword := "with "
	if recursive {
		keyword = "with recursive "
	}
	parts := []any{sql.Raw(keyword)}
	for i, item := range list {
		if item.Name == "" {
			return nil, errors.New("sqlite: cte is missing a name")
		}
		body, err := (Dialect{}).BuildSelectQuery(item.Query)
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

func (Dialect) buildOneSelect(spec query.SelectSpec) (*sql.SQL, error) {
	if spec.From == nil {
		return nil, errors.New("sqlite: select is missing from")
	}
	if err := unaliasedSelect(spec.From); err != nil {
		return nil, err
	}
	if len(spec.Columns) == 0 {
		return nil, errors.New("sqlite: select has no columns")
	}
	if len(spec.DistinctOn) > 0 {
		return nil, errors.New("sqlite: distinct on is not supported")
	}
	parts := make([]any, 0, 8+len(spec.Columns)*2+len(spec.Joins)*4+len(spec.OrderBy)*2)
	switch {
	case spec.Distinct:
		parts = append(parts, sql.Raw("select distinct "))
	default:
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
			return nil, errors.New("sqlite: join is missing a table")
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
			return nil, fmt.Errorf("sqlite: %s is missing on", keyword)
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
	if spec.LimitSet {
		parts = append(parts, sql.Raw(" limit "), spec.Limit)
	}
	if spec.OffsetSet {
		parts = append(parts, sql.Raw(" offset "), spec.Offset)
	}
	if spec.Lock != nil {
		return nil, errors.New("sqlite: for update and for share are not supported")
	}
	return sql.S(parts...), nil
}

func setKeyword(op string) (string, error) {
	switch op {
	case "union", "union all", "intersect", "intersect all", "except", "except all":
		return op, nil
	default:
		return "", fmt.Errorf("sqlite: unknown set operation %s", op)
	}
}

func sameColumns(left, right []sql.Chunk) error {
	if len(left) != len(right) {
		return fmt.Errorf("sqlite: set operation column counts differ, left %d, right %d", len(left), len(right))
	}
	for i := range left {
		ln, lok := colNameOf(left[i])
		rn, rok := colNameOf(right[i])
		if lok && rok && ln != rn {
			return fmt.Errorf("sqlite: set operation column %d is %s and %s", i+1, ln, rn)
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
		return "", false, fmt.Errorf("sqlite: unknown join %d", kind)
	}
}
