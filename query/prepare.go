package query

import (
	"context"
	"errors"

	"github.com/go-mist/mist/sql"
)

// ErrClosed means the statement has already been closed and cannot be executed again.
var ErrClosed = errors.New("query: statement is closed")

// statement stores the parameter list from one render and the driver statement.
// Later executions only fill placeholders and do not assemble the SQL again.
type statement struct {
	stmt   Stmt
	params []any
}

func prepareQuery(ctx context.Context, db Executor, build func() (*sql.SQL, error)) (statement, error) {
	rendered, err := renderSQL(db, build)
	if err != nil {
		return statement{}, err
	}
	stmt, err := db.Prepare(ctx, rendered.SQL)
	if err != nil {
		return statement{}, err
	}
	return statement{stmt: stmt, params: rendered.Params}, nil
}

func (s *statement) fill(values map[string]any) ([]any, error) {
	if s == nil || s.stmt == nil {
		return nil, ErrClosed
	}
	return sql.FillPlaceholders(s.params, values)
}

func (s *statement) query(ctx context.Context, values map[string]any) (Rows, error) {
	args, err := s.fill(values)
	if err != nil {
		return nil, err
	}
	return s.stmt.Query(ctx, args...)
}

func (s *statement) exec(ctx context.Context, values map[string]any) (int64, error) {
	args, err := s.fill(values)
	if err != nil {
		return 0, err
	}
	return s.stmt.Exec(ctx, args...)
}

func (s *statement) Close() error {
	if s == nil || s.stmt == nil {
		return nil
	}
	err := s.stmt.Close()
	s.stmt = nil
	return err
}

// PreparedSelect is a select rendered once. All and One execute it again with new parameters.
type PreparedSelect[T any] struct {
	statement
	picked []Column[T]
	table  Table[T]
}

// Prepare renders this select and registers the statement with the driver.
// Placeholders are written as sql.NewPlaceholder. All and One fill values under the same keys.
func (s Select[T]) Prepare(ctx context.Context, db Executor) (*PreparedSelect[T], error) {
	st, err := prepareQuery(ctx, db, func() (*sql.SQL, error) {
		return db.BuildSelectQuery(s.Spec())
	})
	if err != nil {
		return nil, err
	}
	return &PreparedSelect[T]{
		statement: st,
		picked:    s.copyPicked(),
		table:     s.table,
	}, nil
}

// All fills placeholders from values and returns every row. It returns nil when there are no rows.
func (p *PreparedSelect[T]) All(ctx context.Context, values map[string]any) ([]T, error) {
	rows, err := p.query(ctx, values)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	sel := Select[T]{picked: p.picked, table: p.table}
	dest := make([]any, len(p.picked))
	var out []T
	for rows.Next() {
		item, err := sel.scanInto(rows, dest)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

// One fills placeholders from values and requires exactly one row.
func (p *PreparedSelect[T]) One(ctx context.Context, values map[string]any) (T, error) {
	rows, err := p.query(ctx, values)
	var zero T
	if err != nil {
		return zero, err
	}
	defer rows.Close()
	sel := Select[T]{picked: p.picked, table: p.table}
	return oneRow(rows, func(row sql.Row) (T, error) {
		return sel.scanInto(row, make([]any, len(p.picked)))
	})
}

// PreparedInsert is an insert rendered once.
type PreparedInsert[T any] struct {
	statement
	returning []Column[T]
	output    []Column[T]
	outputSet bool
}

// Prepare renders this insert and registers the statement with the driver.
func (ins Insert[T]) Prepare(ctx context.Context, db Executor) (*PreparedInsert[T], error) {
	st, err := prepareQuery(ctx, db, func() (*sql.SQL, error) {
		return db.BuildInsertQuery(ins.Spec())
	})
	if err != nil {
		return nil, err
	}
	return &PreparedInsert[T]{
		statement: st,
		returning: append([]Column[T](nil), ins.returning...),
		output:    append([]Column[T](nil), ins.output...),
		outputSet: ins.outputSet,
	}, nil
}

// Exec fills placeholders from values and executes. When Returning or Output is set, use All or One instead.
func (p *PreparedInsert[T]) Exec(ctx context.Context, values map[string]any) (int64, error) {
	if p.outputSet {
		return 0, errors.New("query: use All or One when output is set")
	}
	return execPrepared(ctx, &p.statement, p.returning, values)
}

// All executes an insert with RETURNING or OUTPUT.
func (p *PreparedInsert[T]) All(ctx context.Context, values map[string]any) ([]T, error) {
	if p.outputSet {
		if len(p.output) == 0 {
			return nil, errors.New("query: output is missing columns")
		}
		return allPrepared(ctx, &p.statement, p.output, values)
	}
	if len(p.returning) == 0 {
		return nil, errors.New("query: insert has no returning")
	}
	return allPrepared(ctx, &p.statement, p.returning, values)
}

// One executes an insert with RETURNING or OUTPUT and requires exactly one row.
func (p *PreparedInsert[T]) One(ctx context.Context, values map[string]any) (T, error) {
	var zero T
	if p.outputSet {
		if len(p.output) == 0 {
			return zero, errors.New("query: output is missing columns")
		}
		return onePrepared(ctx, &p.statement, p.output, values)
	}
	if len(p.returning) == 0 {
		return zero, errors.New("query: insert has no returning")
	}
	return onePrepared(ctx, &p.statement, p.returning, values)
}

// PreparedUpdate is an update rendered once.
type PreparedUpdate[T any] struct {
	statement
	returning []Column[T]
	output    []Column[T]
	outputSet bool
}

// Prepare renders this update and registers the statement with the driver.
func (u Update[T]) Prepare(ctx context.Context, db Executor) (*PreparedUpdate[T], error) {
	st, err := prepareQuery(ctx, db, func() (*sql.SQL, error) {
		return db.BuildUpdateQuery(u.Spec())
	})
	if err != nil {
		return nil, err
	}
	out := append([]Column[T](nil), u.outputInserted...)
	out = append(out, u.outputDeleted...)
	return &PreparedUpdate[T]{
		statement: st,
		returning: append([]Column[T](nil), u.returning...),
		output:    out,
		outputSet: u.outputInsertedSet || u.outputDeletedSet,
	}, nil
}

// Exec fills placeholders from values and executes. When Returning or Output is set, use All or One instead.
func (p *PreparedUpdate[T]) Exec(ctx context.Context, values map[string]any) (int64, error) {
	if p.outputSet {
		return 0, errors.New("query: use All or One when output is set")
	}
	return execPrepared(ctx, &p.statement, p.returning, values)
}

// All executes an update with RETURNING or OUTPUT.
func (p *PreparedUpdate[T]) All(ctx context.Context, values map[string]any) ([]T, error) {
	if p.outputSet {
		if len(p.output) == 0 {
			return nil, errors.New("query: output is missing columns")
		}
		return allPrepared(ctx, &p.statement, p.output, values)
	}
	if len(p.returning) == 0 {
		return nil, errors.New("query: update has no returning")
	}
	return allPrepared(ctx, &p.statement, p.returning, values)
}

// One executes an update with RETURNING or OUTPUT and requires exactly one row.
func (p *PreparedUpdate[T]) One(ctx context.Context, values map[string]any) (T, error) {
	var zero T
	if p.outputSet {
		if len(p.output) == 0 {
			return zero, errors.New("query: output is missing columns")
		}
		return onePrepared(ctx, &p.statement, p.output, values)
	}
	if len(p.returning) == 0 {
		return zero, errors.New("query: update has no returning")
	}
	return onePrepared(ctx, &p.statement, p.returning, values)
}

// PreparedDelete is a delete rendered once.
type PreparedDelete[T any] struct {
	statement
	returning []Column[T]
	output    []Column[T]
	outputSet bool
}

// Prepare renders this delete and registers the statement with the driver.
func (d Delete[T]) Prepare(ctx context.Context, db Executor) (*PreparedDelete[T], error) {
	st, err := prepareQuery(ctx, db, func() (*sql.SQL, error) {
		return db.BuildDeleteQuery(d.Spec())
	})
	if err != nil {
		return nil, err
	}
	return &PreparedDelete[T]{
		statement: st,
		returning: append([]Column[T](nil), d.returning...),
		output:    append([]Column[T](nil), d.output...),
		outputSet: d.outputSet,
	}, nil
}

// Exec fills placeholders from values and executes. When Returning or Output is set, use All or One instead.
func (p *PreparedDelete[T]) Exec(ctx context.Context, values map[string]any) (int64, error) {
	if p.outputSet {
		return 0, errors.New("query: use All or One when output is set")
	}
	return execPrepared(ctx, &p.statement, p.returning, values)
}

// All executes a delete with RETURNING or OUTPUT.
func (p *PreparedDelete[T]) All(ctx context.Context, values map[string]any) ([]T, error) {
	if p.outputSet {
		if len(p.output) == 0 {
			return nil, errors.New("query: output is missing columns")
		}
		return allPrepared(ctx, &p.statement, p.output, values)
	}
	if len(p.returning) == 0 {
		return nil, errors.New("query: delete has no returning")
	}
	return allPrepared(ctx, &p.statement, p.returning, values)
}

// One executes a delete with RETURNING or OUTPUT and requires exactly one row.
func (p *PreparedDelete[T]) One(ctx context.Context, values map[string]any) (T, error) {
	var zero T
	if p.outputSet {
		if len(p.output) == 0 {
			return zero, errors.New("query: output is missing columns")
		}
		return onePrepared(ctx, &p.statement, p.output, values)
	}
	if len(p.returning) == 0 {
		return zero, errors.New("query: delete has no returning")
	}
	return onePrepared(ctx, &p.statement, p.returning, values)
}

func execPrepared[T any](ctx context.Context, st *statement, cols []Column[T], values map[string]any) (int64, error) {
	if len(cols) > 0 {
		return 0, errors.New("query: use All or One when returning is set")
	}
	return st.exec(ctx, values)
}

func allPrepared[T any](ctx context.Context, st *statement, cols []Column[T], values map[string]any) ([]T, error) {
	rows, err := st.query(ctx, values)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	dest := make([]any, len(cols))
	var out []T
	for rows.Next() {
		item, err := scanCols(rows, cols, dest)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func onePrepared[T any](ctx context.Context, st *statement, cols []Column[T], values map[string]any) (T, error) {
	rows, err := st.query(ctx, values)
	var zero T
	if err != nil {
		return zero, err
	}
	defer rows.Close()
	dest := make([]any, len(cols))
	return oneRow(rows, func(row sql.Row) (T, error) {
		return scanCols(row, cols, dest)
	})
}
