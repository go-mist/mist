package pg

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/xbmlz/mist/logger"
	"github.com/xbmlz/mist/query"
)

// stmtSeq names server-side statements on a transaction. Statements on the pool take no name; see poolStmt.
var stmtSeq atomic.Int64

// DB is a PostgreSQL connection pool. It uses pgx's pool and does not build another.
type DB struct {
	pool   *pgxpool.Pool
	logger logger.Logger
	Dialect
}

// Option configures Open and OpenConfig.
type Option func(*DB)

// WithLogger sets the query logger. nil leaves logging off.
func WithLogger(l logger.Logger) Option {
	return func(db *DB) { db.logger = l }
}

// Tx is a transaction or a savepoint. It implements the same query.Executor as DB.
type Tx struct {
	tx     pgx.Tx
	logger logger.Logger
	Dialect
}

// Open opens a pool from a connection string and checks that the database is reachable before returning.
func Open(ctx context.Context, dsn string, opts ...Option) (*DB, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, err
	}
	return OpenConfig(ctx, cfg, opts...)
}

// OpenConfig opens a connection from a pool config the caller prepared. MaxConns and similar parameters are set on cfg.
func OpenConfig(ctx context.Context, cfg *pgxpool.Config, opts ...Option) (*DB, error) {
	if cfg == nil {
		return nil, errors.New("pg: missing pool config")
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	db := &DB{pool: pool}
	for _, opt := range opts {
		if opt != nil {
			opt(db)
		}
	}
	return db, nil
}

// Close closes the connection pool.
func (db *DB) Close() {
	if db.pool != nil {
		db.pool.Close()
	}
}

// Query runs a query.
func (db *DB) Query(ctx context.Context, sql string, args ...any) (query.Rows, error) {
	if db.pool == nil {
		return nil, errors.New("pg: no connection pool")
	}
	logger.Log(db.logger, sql, args)
	return db.pool.Query(ctx, sql, args...)
}

// Exec runs a statement and returns the number of affected rows.
func (db *DB) Exec(ctx context.Context, sql string, args ...any) (int64, error) {
	if db.pool == nil {
		return 0, errors.New("pg: no connection pool")
	}
	logger.Log(db.logger, sql, args)
	tag, err := db.pool.Exec(ctx, sql, args...)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// Prepare records the SQL text. Execution is still handed to the connection pool.
// pgx caches server-side statements on each connection by text, so this does not hold a connection.
func (db *DB) Prepare(_ context.Context, sql string) (query.Stmt, error) {
	if db.pool == nil {
		return nil, errors.New("pg: no connection pool")
	}
	return &poolStmt{pool: db.pool, sql: sql, logger: db.logger}, nil
}

// Tx opens a transaction. It rolls back when fn returns an error or panics, and commits otherwise.
func (db *DB) Tx(ctx context.Context, fn func(*Tx) error) error {
	if db.pool == nil {
		return errors.New("pg: no connection pool")
	}
	return transact(ctx, db.pool.Begin, db.logger, fn)
}

// Query runs a query in the transaction.
func (t *Tx) Query(ctx context.Context, sql string, args ...any) (query.Rows, error) {
	if t.tx == nil {
		return nil, errors.New("pg: no transaction")
	}
	logger.Log(t.logger, sql, args)
	return t.tx.Query(ctx, sql, args...)
}

// Exec runs a statement in the transaction.
func (t *Tx) Exec(ctx context.Context, sql string, args ...any) (int64, error) {
	if t.tx == nil {
		return 0, errors.New("pg: no transaction")
	}
	logger.Log(t.logger, sql, args)
	tag, err := t.tx.Exec(ctx, sql, args...)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// Prepare runs PREPARE on the current transaction's connection. Later execution uses the statement name.
// Close runs DEALLOCATE. Call it before the transaction ends.
func (t *Tx) Prepare(ctx context.Context, sqlText string) (query.Stmt, error) {
	if t.tx == nil {
		return nil, errors.New("pg: no transaction")
	}
	name := fmt.Sprintf("drizzle_stmt_%d", stmtSeq.Add(1))
	if _, err := t.tx.Prepare(ctx, name, sqlText); err != nil {
		return nil, err
	}
	return &txStmt{tx: t.tx, name: name, sql: sqlText, logger: t.logger}, nil
}

// Tx opens a savepoint in the current transaction.
func (t *Tx) Tx(ctx context.Context, fn func(*Tx) error) error {
	if t.tx == nil {
		return errors.New("pg: no transaction")
	}
	return transact(ctx, t.tx.Begin, t.logger, fn)
}

func transact(ctx context.Context, begin func(context.Context) (pgx.Tx, error), lg logger.Logger, fn func(*Tx) error) (err error) {
	raw, err := begin(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if p := recover(); p != nil {
			_ = raw.Rollback(context.WithoutCancel(ctx))
			panic(p)
		}
	}()
	if err = fn(&Tx{tx: raw, logger: lg}); err != nil {
		_ = raw.Rollback(context.WithoutCancel(ctx))
		return err
	}
	return raw.Commit(ctx)
}

// poolStmt hands the same SQL to the connection pool on each call. pgx caches the server-side statement per connection.
type poolStmt struct {
	pool   *pgxpool.Pool
	sql    string
	logger logger.Logger
}

func (s *poolStmt) Query(ctx context.Context, args ...any) (query.Rows, error) {
	logger.Log(s.logger, s.sql, args)
	return s.pool.Query(ctx, s.sql, args...)
}

func (s *poolStmt) Exec(ctx context.Context, args ...any) (int64, error) {
	logger.Log(s.logger, s.sql, args)
	tag, err := s.pool.Exec(ctx, s.sql, args...)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func (s *poolStmt) Close() error { return nil }

// txStmt executes by the name returned from PREPARE.
type txStmt struct {
	tx     pgx.Tx
	name   string
	sql    string
	logger logger.Logger
}

func (s *txStmt) Query(ctx context.Context, args ...any) (query.Rows, error) {
	logger.Log(s.logger, s.sql, args)
	return s.tx.Query(ctx, s.name, args...)
}

func (s *txStmt) Exec(ctx context.Context, args ...any) (int64, error) {
	logger.Log(s.logger, s.sql, args)
	tag, err := s.tx.Exec(ctx, s.name, args...)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func (s *txStmt) Close() error {
	if s == nil || s.tx == nil {
		return nil
	}
	err := s.tx.Conn().Deallocate(context.Background(), s.name)
	if err != nil {
		return err
	}
	s.tx = nil
	return nil
}
