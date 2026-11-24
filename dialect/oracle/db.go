package oracle

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/go-mist/mist"
	"github.com/go-mist/mist/logger"
	"github.com/go-mist/mist/query"
	_ "github.com/sijms/go-ora/v2"
)

// driverName is the database/sql name registered by go-ora.
const driverName = "oracle"

// DB is a database/sql connection pool. The dialect is Oracle.
type DB struct {
	sqldb  *sql.DB
	logger logger.Logger
	Dialect
}

// Option configures Open.
type Option func(*DB)

// WithLogger sets the query logger. nil leaves logging off.
func WithLogger(l logger.Logger) Option {
	return func(db *DB) { db.logger = l }
}

// Tx is a transaction. A nested Tx is a savepoint.
type Tx struct {
	tx     *sql.Tx
	next   *int
	logger logger.Logger
	Dialect
}

// Open opens Oracle. dsn is a go-ora URL, for example oracle://user:pass@host:1521/service.
func Open(ctx context.Context, dsn string, opts ...Option) (*DB, error) {
	if dsn == "" {
		return nil, errors.New("oracle: missing connection string")
	}
	sqldb, err := sql.Open(driverName, dsn)
	if err != nil {
		return nil, fmt.Errorf("oracle: connect: %w", err)
	}
	if err := sqldb.PingContext(ctx); err != nil {
		_ = sqldb.Close()
		return nil, fmt.Errorf("oracle: connect: %w", err)
	}
	db := &DB{sqldb: sqldb}
	for _, opt := range opts {
		if opt != nil {
			opt(db)
		}
	}
	return db, nil
}

// SQLDB returns the underlying connection pool.
func (db *DB) SQLDB() *sql.DB { return db.sqldb }

// Close closes the connection pool.
func (db *DB) Close() error {
	if db.sqldb == nil {
		return nil
	}
	return db.sqldb.Close()
}

// Query executes a query.
func (db *DB) Query(ctx context.Context, query string, args ...any) (query.Rows, error) {
	if db.sqldb == nil {
		return nil, errors.New("oracle: no connection pool")
	}
	logger.Log(db.logger, query, args)
	rows, err := db.sqldb.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return &sqlRows{rows: rows}, nil
}

// Exec executes a statement and returns the number of affected rows.
func (db *DB) Exec(ctx context.Context, query string, args ...any) (int64, error) {
	if db.sqldb == nil {
		return 0, errors.New("oracle: no connection pool")
	}
	logger.Log(db.logger, query, args)
	return execResult(db.sqldb.ExecContext(ctx, query, args...))
}

// Prepare calls database/sql PrepareContext.
func (db *DB) Prepare(ctx context.Context, sqlText string) (query.Stmt, error) {
	if db.sqldb == nil {
		return nil, errors.New("oracle: no connection pool")
	}
	stmt, err := db.sqldb.PrepareContext(ctx, sqlText)
	if err != nil {
		return nil, err
	}
	return &sqlStmt{stmt: stmt, sql: sqlText, logger: db.logger}, nil
}

// Tx starts a transaction. It rolls back when fn returns an error or panics, and commits otherwise.
func (db *DB) Tx(ctx context.Context, fn func(mist.Tx) error) error {
	if db.sqldb == nil {
		return errors.New("oracle: no connection pool")
	}
	raw, err := db.sqldb.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	seq := 0
	return finish(func() error { return raw.Rollback() }, func() error { return raw.Commit() }, func() error {
		return fn(&Tx{tx: raw, next: &seq, logger: db.logger})
	})
}

// Query executes a query inside the transaction.
func (t *Tx) Query(ctx context.Context, query string, args ...any) (query.Rows, error) {
	if t.tx == nil {
		return nil, errors.New("oracle: no transaction")
	}
	logger.Log(t.logger, query, args)
	rows, err := t.tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return &sqlRows{rows: rows}, nil
}

// Exec executes a statement inside the transaction.
func (t *Tx) Exec(ctx context.Context, query string, args ...any) (int64, error) {
	if t.tx == nil {
		return 0, errors.New("oracle: no transaction")
	}
	logger.Log(t.logger, query, args)
	return execResult(t.tx.ExecContext(ctx, query, args...))
}

// Prepare calls PrepareContext on the current transaction.
func (t *Tx) Prepare(ctx context.Context, sqlText string) (query.Stmt, error) {
	if t.tx == nil {
		return nil, errors.New("oracle: no transaction")
	}
	stmt, err := t.tx.PrepareContext(ctx, sqlText)
	if err != nil {
		return nil, err
	}
	return &sqlStmt{stmt: stmt, sql: sqlText, logger: t.logger}, nil
}

// Tx opens a savepoint inside the current transaction.
// Oracle keeps the savepoint until the outer transaction ends. A failed nested call rolls back to it.
func (t *Tx) Tx(ctx context.Context, fn func(mist.Tx) error) error {
	if t.tx == nil {
		return errors.New("oracle: no transaction")
	}
	name := t.savepoint()
	if _, err := t.tx.ExecContext(ctx, "SAVEPOINT "+name); err != nil {
		return err
	}
	return finish(
		func() error {
			_, err := t.tx.ExecContext(context.WithoutCancel(ctx), "ROLLBACK TO SAVEPOINT "+name)
			return err
		},
		func() error { return nil },
		func() error { return fn(&Tx{tx: t.tx, next: t.next, logger: t.logger}) },
	)
}

func (t *Tx) savepoint() string {
	*t.next++
	return fmt.Sprintf("mist_sp_%d", *t.next)
}

func finish(rollback, commit, fn func() error) (err error) {
	defer func() {
		if p := recover(); p != nil {
			_ = rollback()
			panic(p)
		}
	}()
	if err = fn(); err != nil {
		_ = rollback()
		return err
	}
	return commit()
}

func execResult(res sql.Result, err error) (int64, error) {
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

type sqlRows struct {
	rows *sql.Rows
	cerr error
}

func (r *sqlRows) Next() bool             { return r.rows.Next() }
func (r *sqlRows) Scan(dest ...any) error { return r.rows.Scan(dest...) }
func (r *sqlRows) Err() error {
	if err := r.rows.Err(); err != nil {
		return err
	}
	return r.cerr
}
func (r *sqlRows) Close() {
	if err := r.rows.Close(); err != nil && r.cerr == nil {
		r.cerr = err
	}
}

type sqlStmt struct {
	stmt   *sql.Stmt
	sql    string
	logger logger.Logger
}

func (s *sqlStmt) Query(ctx context.Context, args ...any) (query.Rows, error) {
	logger.Log(s.logger, s.sql, args)
	rows, err := s.stmt.QueryContext(ctx, args...)
	if err != nil {
		return nil, err
	}
	return &sqlRows{rows: rows}, nil
}

func (s *sqlStmt) Exec(ctx context.Context, args ...any) (int64, error) {
	logger.Log(s.logger, s.sql, args)
	return execResult(s.stmt.ExecContext(ctx, args...))
}

func (s *sqlStmt) Close() error {
	if s == nil || s.stmt == nil {
		return nil
	}
	err := s.stmt.Close()
	s.stmt = nil
	return err
}
