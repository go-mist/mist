package mssql

import (
	"context"

	"github.com/go-mist/mist"
	"github.com/go-mist/mist/migrate"
	"github.com/go-mist/mist/query"
)

// Migrator uses this pool as a migration connection.
// SQL Server runs the batch inside a transaction. The lock is sp_getapplock owned by that transaction.
func Migrator(db *DB) migrate.Conn {
	if db == nil {
		return nil
	}
	return mssqlConn{db: db}
}

type mssqlConn struct {
	db *DB
}

func (m mssqlConn) Driver() string { return "mssql" }

func (m mssqlConn) Exec(ctx context.Context, query string, args ...any) (int64, error) {
	return m.db.Exec(ctx, query, args...)
}

func (m mssqlConn) Query(ctx context.Context, query string, args ...any) (query.Rows, error) {
	return m.db.Query(ctx, query, args...)
}

func (m mssqlConn) Tx(ctx context.Context, fn func(migrate.Conn) error) error {
	return m.db.Tx(ctx, func(tx mist.Tx) error {
		return fn(mssqlTx{tx: tx.(*Tx)})
	})
}

type mssqlTx struct {
	tx *Tx
}

func (m mssqlTx) Driver() string { return "mssql" }

func (m mssqlTx) Exec(ctx context.Context, query string, args ...any) (int64, error) {
	return m.tx.Exec(ctx, query, args...)
}

func (m mssqlTx) Query(ctx context.Context, query string, args ...any) (query.Rows, error) {
	return m.tx.Query(ctx, query, args...)
}

func (m mssqlTx) Tx(ctx context.Context, fn func(migrate.Conn) error) error {
	return m.tx.Tx(ctx, func(tx mist.Tx) error {
		return fn(mssqlTx{tx: tx.(*Tx)})
	})
}
