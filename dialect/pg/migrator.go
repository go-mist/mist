package pg

import (
	"context"

	"github.com/go-mist/mist"
	"github.com/go-mist/mist/migrate"
	"github.com/go-mist/mist/query"
)

// Migrator uses this pool as a migration connection.
// Statements inside a transaction run on that transaction's own connection.
func Migrator(db *DB) migrate.Conn {
	if db == nil {
		return nil
	}
	return pgConn{db: db}
}

type pgConn struct {
	db *DB
}

func (p pgConn) Driver() string { return "pg" }

func (p pgConn) Exec(ctx context.Context, sql string, args ...any) (int64, error) {
	return p.db.Exec(ctx, sql, args...)
}

func (p pgConn) Query(ctx context.Context, sql string, args ...any) (query.Rows, error) {
	return p.db.Query(ctx, sql, args...)
}

func (p pgConn) Tx(ctx context.Context, fn func(migrate.Conn) error) error {
	return p.db.Tx(ctx, func(tx mist.Tx) error {
		return fn(pgTx{tx: tx.(*Tx)})
	})
}

type pgTx struct {
	tx *Tx
}

func (p pgTx) Driver() string { return "pg" }

func (p pgTx) Exec(ctx context.Context, sql string, args ...any) (int64, error) {
	return p.tx.Exec(ctx, sql, args...)
}

func (p pgTx) Query(ctx context.Context, sql string, args ...any) (query.Rows, error) {
	return p.tx.Query(ctx, sql, args...)
}

func (p pgTx) Tx(ctx context.Context, fn func(migrate.Conn) error) error {
	return p.tx.Tx(ctx, func(tx mist.Tx) error {
		return fn(pgTx{tx: tx.(*Tx)})
	})
}
