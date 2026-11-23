package query

import (
	"context"
	"errors"
	"fmt"

	"github.com/xbmlz/mist/sql"
	"github.com/xbmlz/mist/sql/dialect"
)

// ErrNoRows means One read no rows.
var ErrNoRows = errors.New("query: no rows")

// ErrManyRows means One read multiple rows.
var ErrManyRows = errors.New("query: multiple rows")

// Rows is the row cursor a query returns.
type Rows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
	Close()
}

// Stmt is one statement on the driver. Query and Exec accept only parameters that are already filled in.
type Stmt interface {
	Query(ctx context.Context, args ...any) (Rows, error)
	Exec(ctx context.Context, args ...any) (int64, error)
	Close() error
}

// Executor is a connection or a transaction. Dialect methods render. Query and Exec execute.
// Prepare registers one statement with the driver from the SQL text, for repeated execution of the same shape.
type Executor interface {
	dialect.Dialect
	BuildSelectQuery(SelectSpec) (*sql.SQL, error)
	BuildInsertQuery(InsertSpec) (*sql.SQL, error)
	BuildUpdateQuery(UpdateSpec) (*sql.SQL, error)
	BuildDeleteQuery(DeleteSpec) (*sql.SQL, error)
	Query(ctx context.Context, query string, args ...any) (Rows, error)
	Exec(ctx context.Context, query string, args ...any) (int64, error)
	Prepare(ctx context.Context, query string) (Stmt, error)
}

// All executes the select and returns every row. It returns nil when there are no rows.
// With no columns it uses the generated whole-row scan. With columns it fills only those fields and leaves the rest at the zero value.
func (s Select[T]) All(ctx context.Context, db Executor) ([]T, error) {
	rows, err := queryRows(ctx, db, func() (*sql.SQL, error) {
		return db.BuildSelectQuery(s.Spec())
	})
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var dest []any
	if len(s.picked) > 0 {
		dest = make([]any, len(s.picked))
	}
	var out []T
	for rows.Next() {
		item, err := s.scanInto(rows, dest)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

// One executes the select and requires exactly one row. No rows returns ErrNoRows. Multiple rows returns ErrManyRows.
func (s Select[T]) One(ctx context.Context, db Executor) (T, error) {
	rows, err := queryRows(ctx, db, func() (*sql.SQL, error) {
		return db.BuildSelectQuery(s.Spec())
	})
	var zero T
	if err != nil {
		return zero, err
	}
	defer rows.Close()
	return oneRow(rows, func(row sql.Row) (T, error) {
		return s.scanInto(row, make([]any, len(s.picked)))
	})
}

func (s Select[T]) scanInto(row sql.Row, dest []any) (T, error) {
	var item T
	if len(s.picked) == 0 {
		if s.table == nil {
			return item, errors.New("query: select is missing from")
		}
		err := s.table.Scan(row, &item)
		return item, err
	}
	item, err := scanCols(row, s.picked, dest)
	return item, err
}

// Exec executes the insert. When Returning is set, use All or One instead.
func (ins Insert[T]) Exec(ctx context.Context, db Executor) (int64, error) {
	if len(ins.returning) > 0 {
		return 0, errors.New("query: use All or One when returning is set")
	}
	return execSQL(ctx, db, func() (*sql.SQL, error) {
		return db.BuildInsertQuery(ins.Spec())
	})
}

// All executes an insert with RETURNING and fills T from the RETURNING columns.
func (ins Insert[T]) All(ctx context.Context, db Executor) ([]T, error) {
	if len(ins.returning) == 0 {
		return nil, errors.New("query: insert has no returning")
	}
	return allCols(ctx, db, ins.returning, func() (*sql.SQL, error) {
		return db.BuildInsertQuery(ins.Spec())
	})
}

// One executes an insert with RETURNING and requires exactly one row.
func (ins Insert[T]) One(ctx context.Context, db Executor) (T, error) {
	var zero T
	if len(ins.returning) == 0 {
		return zero, errors.New("query: insert has no returning")
	}
	return oneCols(ctx, db, ins.returning, func() (*sql.SQL, error) {
		return db.BuildInsertQuery(ins.Spec())
	})
}

// Exec executes the update. When Returning is set, use All or One instead.
func (u Update[T]) Exec(ctx context.Context, db Executor) (int64, error) {
	if len(u.returning) > 0 {
		return 0, errors.New("query: use All or One when returning is set")
	}
	return execSQL(ctx, db, func() (*sql.SQL, error) {
		return db.BuildUpdateQuery(u.Spec())
	})
}

// All executes an update with RETURNING.
func (u Update[T]) All(ctx context.Context, db Executor) ([]T, error) {
	if len(u.returning) == 0 {
		return nil, errors.New("query: update has no returning")
	}
	return allCols(ctx, db, u.returning, func() (*sql.SQL, error) {
		return db.BuildUpdateQuery(u.Spec())
	})
}

// One executes an update with RETURNING and requires exactly one row.
func (u Update[T]) One(ctx context.Context, db Executor) (T, error) {
	var zero T
	if len(u.returning) == 0 {
		return zero, errors.New("query: update has no returning")
	}
	return oneCols(ctx, db, u.returning, func() (*sql.SQL, error) {
		return db.BuildUpdateQuery(u.Spec())
	})
}

// Exec executes the delete. When Returning is set, use All or One instead.
func (d Delete[T]) Exec(ctx context.Context, db Executor) (int64, error) {
	if len(d.returning) > 0 {
		return 0, errors.New("query: use All or One when returning is set")
	}
	return execSQL(ctx, db, func() (*sql.SQL, error) {
		return db.BuildDeleteQuery(d.Spec())
	})
}

// All executes a delete with RETURNING.
func (d Delete[T]) All(ctx context.Context, db Executor) ([]T, error) {
	if len(d.returning) == 0 {
		return nil, errors.New("query: delete has no returning")
	}
	return allCols(ctx, db, d.returning, func() (*sql.SQL, error) {
		return db.BuildDeleteQuery(d.Spec())
	})
}

// One executes a delete with RETURNING and requires exactly one row.
func (d Delete[T]) One(ctx context.Context, db Executor) (T, error) {
	var zero T
	if len(d.returning) == 0 {
		return zero, errors.New("query: delete has no returning")
	}
	return oneCols(ctx, db, d.returning, func() (*sql.SQL, error) {
		return db.BuildDeleteQuery(d.Spec())
	})
}

func allCols[T any](ctx context.Context, db Executor, cols []Column[T], build func() (*sql.SQL, error)) ([]T, error) {
	rows, err := queryRows(ctx, db, build)
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

func oneCols[T any](ctx context.Context, db Executor, cols []Column[T], build func() (*sql.SQL, error)) (T, error) {
	rows, err := queryRows(ctx, db, build)
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

func oneRow[T any](rows Rows, scan func(sql.Row) (T, error)) (T, error) {
	var zero T
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return zero, err
		}
		return zero, ErrNoRows
	}
	item, err := scan(rows)
	if err != nil {
		return zero, err
	}
	if rows.Next() {
		return zero, ErrManyRows
	}
	if err := rows.Err(); err != nil {
		return zero, err
	}
	return item, nil
}

func scanCols[T any](row sql.Row, cols []Column[T], dest []any) (T, error) {
	var item T
	for i, col := range cols {
		ptr := col.FieldPtr(&item)
		if ptr == nil {
			return item, fmt.Errorf("query: column %s has no field pointer", col.ColName())
		}
		dest[i] = ptr
	}
	err := row.Scan(dest...)
	return item, err
}

func queryRows(ctx context.Context, db Executor, build func() (*sql.SQL, error)) (Rows, error) {
	q, err := renderSQL(db, build)
	if err != nil {
		return nil, err
	}
	return db.Query(ctx, q.SQL, q.Params...)
}

func execSQL(ctx context.Context, db Executor, build func() (*sql.SQL, error)) (int64, error) {
	q, err := renderSQL(db, build)
	if err != nil {
		return 0, err
	}
	return db.Exec(ctx, q.SQL, q.Params...)
}

func renderSQL(db dialect.Dialect, build func() (*sql.SQL, error)) (sql.Query, error) {
	built, err := build()
	if err != nil {
		return sql.Query{}, err
	}
	return sql.Render(built, db, sql.RenderConfig{})
}
