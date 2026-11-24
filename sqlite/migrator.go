package sqlite

import (
	"context"

	"github.com/go-mist/mist"
	"github.com/go-mist/mist/migrate"
	"github.com/go-mist/mist/query"
)

// Migrator uses this pool as a migration connection.
// The migration table is in the main database. SQLite has no schema.
func Migrator(db *DB) migrate.Conn {
	if db == nil {
		return nil
	}
	return sqliteConn{db: db}
}

type sqliteConn struct {
	db *DB
}

func (s sqliteConn) Driver() string { return "sqlite" }

func (s sqliteConn) Exec(ctx context.Context, query string, args ...any) (int64, error) {
	return s.db.Exec(ctx, query, args...)
}

func (s sqliteConn) Query(ctx context.Context, query string, args ...any) (query.Rows, error) {
	return s.db.Query(ctx, query, args...)
}

func (s sqliteConn) Tx(ctx context.Context, fn func(migrate.Conn) error) error {
	return s.db.Tx(ctx, func(tx mist.Tx) error {
		return fn(sqliteTx{tx: tx.(*Tx)})
	})
}

type sqliteTx struct {
	tx *Tx
}

func (s sqliteTx) Driver() string { return "sqlite" }

func (s sqliteTx) Exec(ctx context.Context, query string, args ...any) (int64, error) {
	return s.tx.Exec(ctx, query, args...)
}

func (s sqliteTx) Query(ctx context.Context, query string, args ...any) (query.Rows, error) {
	return s.tx.Query(ctx, query, args...)
}

func (s sqliteTx) Tx(ctx context.Context, fn func(migrate.Conn) error) error {
	return s.tx.Tx(ctx, func(tx mist.Tx) error {
		return fn(sqliteTx{tx: tx.(*Tx)})
	})
}
