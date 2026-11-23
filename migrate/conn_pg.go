package migrate

import (
	"context"

	"github.com/xbmlz/mist/pg"
	"github.com/xbmlz/mist/query"
)

// UsePG uses pg.DB as the migration connection. Statements inside a transaction run on that transaction's own connection.
func UsePG(db *pg.DB) Conn {
	if db == nil {
		return nil
	}
	return pgConn{db: db}
}

type pgConn struct {
	db *pg.DB
}

func (p pgConn) Exec(ctx context.Context, sql string, args ...any) (int64, error) {
	return p.db.Exec(ctx, sql, args...)
}

func (p pgConn) Query(ctx context.Context, sql string, args ...any) (query.Rows, error) {
	return p.db.Query(ctx, sql, args...)
}

func (p pgConn) Tx(ctx context.Context, fn func(Conn) error) error {
	return p.db.Tx(ctx, func(tx *pg.Tx) error {
		return fn(pgTx{tx: tx})
	})
}

type pgTx struct {
	tx *pg.Tx
}

func (p pgTx) Exec(ctx context.Context, sql string, args ...any) (int64, error) {
	return p.tx.Exec(ctx, sql, args...)
}

func (p pgTx) Query(ctx context.Context, sql string, args ...any) (query.Rows, error) {
	return p.tx.Query(ctx, sql, args...)
}

func (p pgTx) Tx(ctx context.Context, fn func(Conn) error) error {
	return p.tx.Tx(ctx, func(tx *pg.Tx) error {
		return fn(pgTx{tx: tx})
	})
}
