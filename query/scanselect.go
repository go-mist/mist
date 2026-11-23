package query

import (
	"context"
	"errors"

	"github.com/go-mist/mist/sql"
)

// ScanFunc scans one row into T in select-list order.
type ScanFunc[T any] func(sql.Row, *T) error

// ScanQuery is a select whose row type does not come from a table.
// A single value uses NewValue. Multiple columns use NewScan and a scan function the caller writes.
type ScanQuery[T any] struct {
	cols []sql.Chunk
	scan ScanFunc[T]
	from sql.Chunk
	tail *clause
}

// NewValue starts a single-column query and scans the result directly into T. Callers write pg.Value.
func NewValue[T any](expr sql.Chunk) ScanQuery[T] {
	return ScanQuery[T]{
		cols: []sql.Chunk{expr},
		scan: func(row sql.Row, dst *T) error { return row.Scan(dst) },
	}
}

// NewScan starts a multi-column query. The fields of scan follow cols. Callers write pg.Scan.
func NewScan[T any](scan ScanFunc[T], cols ...sql.Chunk) ScanQuery[T] {
	return ScanQuery[T]{cols: append([]sql.Chunk(nil), cols...), scan: scan}
}

// From sets the source table. The table here only renders and does not determine T.
func (s ScanQuery[T]) From(table sql.Chunk) ScanQuery[T] {
	s.from = table
	return s
}

// Where sets the condition. A later call replaces the previous one.
func (s ScanQuery[T]) Where(cond sql.Chunk) ScanQuery[T] {
	s.tail = &clause{prev: s.tail, kind: clauseWhere, chunk: cond}
	return s
}

// GroupBy sets the grouping. A later call replaces the previous one.
func (s ScanQuery[T]) GroupBy(exprs ...sql.Chunk) ScanQuery[T] {
	s.tail = &clause{prev: s.tail, kind: clauseGroup, chunks: append([]sql.Chunk(nil), exprs...)}
	return s
}

// Having sets the condition after grouping. A later call replaces the previous one.
func (s ScanQuery[T]) Having(cond sql.Chunk) ScanQuery[T] {
	s.tail = &clause{prev: s.tail, kind: clauseHaving, chunk: cond}
	return s
}

// OrderBy sets the ordering. A later call replaces the previous one.
func (s ScanQuery[T]) OrderBy(exprs ...sql.Chunk) ScanQuery[T] {
	s.tail = &clause{prev: s.tail, kind: clauseOrder, chunks: append([]sql.Chunk(nil), exprs...)}
	return s
}

// Limit sets the limit. Limit(0) renders as LIMIT 0.
func (s ScanQuery[T]) Limit(n int) ScanQuery[T] {
	s.tail = &clause{prev: s.tail, kind: clauseLimit, n: n}
	return s
}

// Offset sets the offset. Offset(0) renders as OFFSET 0.
func (s ScanQuery[T]) Offset(n int) ScanQuery[T] {
	s.tail = &clause{prev: s.tail, kind: clauseOffset, n: n}
	return s
}

// InnerJoin appends an inner join.
func (s ScanQuery[T]) InnerJoin(table, on sql.Chunk) ScanQuery[T] {
	return s.addJoin(InnerJoin, table, on)
}

// LeftJoin appends a left join.
func (s ScanQuery[T]) LeftJoin(table, on sql.Chunk) ScanQuery[T] {
	return s.addJoin(LeftJoin, table, on)
}

// RightJoin appends a right join.
func (s ScanQuery[T]) RightJoin(table, on sql.Chunk) ScanQuery[T] {
	return s.addJoin(RightJoin, table, on)
}

// FullJoin appends a full join.
func (s ScanQuery[T]) FullJoin(table, on sql.Chunk) ScanQuery[T] {
	return s.addJoin(FullJoin, table, on)
}

// CrossJoin appends a cross join, with no on.
func (s ScanQuery[T]) CrossJoin(table sql.Chunk) ScanQuery[T] {
	return s.addJoin(CrossJoin, table, nil)
}

func (s ScanQuery[T]) addJoin(kind JoinKind, table, on sql.Chunk) ScanQuery[T] {
	s.tail = &clause{prev: s.tail, kind: clauseJoin, join: kind, chunk: table, on: on}
	return s
}

// Spec expands this chain. Clause override rules are the same as for Select.
func (s ScanQuery[T]) Spec() SelectSpec {
	return buildSpec(s.from, s.cols, s.tail)
}

// SQL asks the dialect to build the fragments, then renders them into text and parameters.
func (s ScanQuery[T]) SQL(d SelectDialect) (sql.Query, error) {
	built, err := d.BuildSelectQuery(s.Spec())
	if err != nil {
		return sql.Query{}, err
	}
	return sql.Render(built, d, sql.RenderConfig{})
}

// All executes the query and returns every row through the scan function. It returns nil when there are no rows.
func (s ScanQuery[T]) All(ctx context.Context, db Executor) ([]T, error) {
	if s.scan == nil {
		return nil, errors.New("query: missing scan function")
	}
	rows, err := queryRows(ctx, db, func() (*sql.SQL, error) {
		return db.BuildSelectQuery(s.Spec())
	})
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []T
	for rows.Next() {
		var item T
		if err := s.scan(rows, &item); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

// One executes the query and requires exactly one row.
func (s ScanQuery[T]) One(ctx context.Context, db Executor) (T, error) {
	var zero T
	if s.scan == nil {
		return zero, errors.New("query: missing scan function")
	}
	rows, err := queryRows(ctx, db, func() (*sql.SQL, error) {
		return db.BuildSelectQuery(s.Spec())
	})
	if err != nil {
		return zero, err
	}
	defer rows.Close()
	return oneRow(rows, func(row sql.Row) (T, error) {
		var item T
		err := s.scan(row, &item)
		return item, err
	})
}

// PreparedScan is a ScanQuery rendered once.
type PreparedScan[T any] struct {
	statement
	scan ScanFunc[T]
}

// Prepare renders this query and registers the statement with the driver.
func (s ScanQuery[T]) Prepare(ctx context.Context, db Executor) (*PreparedScan[T], error) {
	if s.scan == nil {
		return nil, errors.New("query: missing scan function")
	}
	st, err := prepareQuery(ctx, db, func() (*sql.SQL, error) {
		return db.BuildSelectQuery(s.Spec())
	})
	if err != nil {
		return nil, err
	}
	return &PreparedScan[T]{statement: st, scan: s.scan}, nil
}

// All fills placeholders from values and returns every row.
func (p *PreparedScan[T]) All(ctx context.Context, values map[string]any) ([]T, error) {
	rows, err := p.query(ctx, values)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []T
	for rows.Next() {
		var item T
		if err := p.scan(rows, &item); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

// One fills placeholders from values and requires exactly one row.
func (p *PreparedScan[T]) One(ctx context.Context, values map[string]any) (T, error) {
	rows, err := p.query(ctx, values)
	var zero T
	if err != nil {
		return zero, err
	}
	defer rows.Close()
	return oneRow(rows, func(row sql.Row) (T, error) {
		var item T
		err := p.scan(row, &item)
		return item, err
	})
}
