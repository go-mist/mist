package mysql

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/go-mist/mist"
	"github.com/go-mist/mist/migrate"
	"github.com/go-mist/mist/query"
)

// Migrator uses this pool as a migration connection.
// MySQL DDL commits the current transaction, so the lock is GET_LOCK on a held connection
// and stays until the migration returns.
func Migrator(db *DB) migrate.Conn {
	if db == nil {
		return nil
	}
	return mysqlConn{db: db}
}

type mysqlConn struct {
	db *DB
}

func (m mysqlConn) Driver() string { return "mysql" }

func (m mysqlConn) Exec(ctx context.Context, query string, args ...any) (int64, error) {
	return m.db.Exec(ctx, query, args...)
}

func (m mysqlConn) Query(ctx context.Context, query string, args ...any) (query.Rows, error) {
	return m.db.Query(ctx, query, args...)
}

func (m mysqlConn) Tx(ctx context.Context, fn func(migrate.Conn) error) error {
	return m.db.Tx(ctx, func(tx mist.Tx) error {
		return fn(mysqlTx{tx: tx.(*Tx)})
	})
}

// SessionLock takes MySQL GET_LOCK. The caller releases it after the migration transaction commits.
// The name is at most 64 characters.
func (m mysqlConn) SessionLock(ctx context.Context, name string) (func() error, error) {
	if m.db == nil || m.db.sqldb == nil {
		return nil, fmt.Errorf("migrate: mysql has no connection")
	}
	if len(name) > 64 {
		return nil, fmt.Errorf("migrate: mysql lock name is longer than 64 characters")
	}
	conn, err := m.db.sqldb.Conn(ctx)
	if err != nil {
		return nil, err
	}
	var got sql.NullInt64
	if err := conn.QueryRowContext(ctx, "SELECT GET_LOCK(?, -1)", name).Scan(&got); err != nil {
		_ = conn.Close()
		return nil, err
	}
	if !got.Valid || got.Int64 != 1 {
		_ = conn.Close()
		return nil, fmt.Errorf("migrate: mysql lock %q was not acquired", name)
	}
	return func() error {
		defer conn.Close()
		var released sql.NullInt64
		return conn.QueryRowContext(context.Background(), "SELECT RELEASE_LOCK(?)", name).Scan(&released)
	}, nil
}

type mysqlTx struct {
	tx *Tx
}

func (m mysqlTx) Driver() string { return "mysql" }

func (m mysqlTx) Exec(ctx context.Context, query string, args ...any) (int64, error) {
	return m.tx.Exec(ctx, query, args...)
}

func (m mysqlTx) Query(ctx context.Context, query string, args ...any) (query.Rows, error) {
	return m.tx.Query(ctx, query, args...)
}

func (m mysqlTx) Tx(ctx context.Context, fn func(migrate.Conn) error) error {
	return m.tx.Tx(ctx, func(tx mist.Tx) error {
		return fn(mysqlTx{tx: tx.(*Tx)})
	})
}
