package dm

import (
	"context"
	"fmt"
	"strings"

	"github.com/go-mist/mist/migrate"
	"github.com/go-mist/mist/query"
)

// Migrator uses this pool as a migration connection.
// Dameng DDL can commit the current transaction, so Tx runs the callback on this connection
// without a database transaction. The lock is a row lock on another connection and stays until the call returns.
func Migrator(db *DB) migrate.Conn {
	if db == nil {
		return nil
	}
	return dmConn{db: db}
}

type dmConn struct {
	db *DB
}

func (c dmConn) Driver() string { return "dm" }

func (c dmConn) Exec(ctx context.Context, query string, args ...any) (int64, error) {
	return c.db.Exec(ctx, query, args...)
}

func (c dmConn) Query(ctx context.Context, query string, args ...any) (query.Rows, error) {
	return c.db.Query(ctx, query, args...)
}

// Tx does not open a database transaction. A later rollback would not undo DDL that already committed.
func (c dmConn) Tx(ctx context.Context, fn func(migrate.Conn) error) error {
	return fn(c)
}

// SessionLock holds a row in the lock table until unlock rolls the dedicated connection back.
func (c dmConn) SessionLock(ctx context.Context, schema, name string) (func() error, error) {
	if c.db == nil || c.db.sqldb == nil {
		return nil, fmt.Errorf("migrate: dm has no connection")
	}
	table := `"__mist_lock"`
	if schema != "" {
		table = `"` + schema + `"."__mist_lock"`
	}
	create := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s ("name" VARCHAR(128) PRIMARY KEY)`, table)
	if _, err := c.db.Exec(ctx, create); err != nil && !alreadyExists(err) {
		return nil, err
	}
	merge := fmt.Sprintf(`MERGE INTO %s t USING (SELECT ? AS "name" FROM dual) s ON (t."name" = s."name") WHEN NOT MATCHED THEN INSERT ("name") VALUES (s."name")`, table)
	if _, err := c.db.Exec(ctx, merge, name); err != nil {
		return nil, err
	}
	conn, err := c.db.sqldb.Conn(ctx)
	if err != nil {
		return nil, err
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	var got string
	hold := fmt.Sprintf(`SELECT "name" FROM %s WHERE "name" = ? FOR UPDATE`, table)
	if err := tx.QueryRowContext(ctx, hold, name).Scan(&got); err != nil {
		_ = tx.Rollback()
		_ = conn.Close()
		return nil, err
	}
	return func() error {
		defer conn.Close()
		return tx.Rollback()
	}, nil
}

func alreadyExists(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "已存在") || strings.Contains(msg, "-2116") || strings.Contains(msg, "already exists")
}
