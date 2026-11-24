package oracle

import (
	"context"
	"fmt"

	"github.com/go-mist/mist/migrate"
	"github.com/go-mist/mist/query"
)

// Migrator uses this pool as a migration connection.
// Oracle DDL commits the current transaction, so Tx runs the callback on this connection
// without a database transaction. The lock is a row lock on another connection and stays until the call returns.
func Migrator(db *DB) migrate.Conn {
	if db == nil {
		return nil
	}
	return oracleConn{db: db}
}

type oracleConn struct {
	db *DB
}

func (c oracleConn) Driver() string { return "oracle" }

func (c oracleConn) Exec(ctx context.Context, query string, args ...any) (int64, error) {
	return c.db.Exec(ctx, query, args...)
}

func (c oracleConn) Query(ctx context.Context, query string, args ...any) (query.Rows, error) {
	return c.db.Query(ctx, query, args...)
}

// Tx does not open a database transaction. A CREATE TABLE would commit it, so a later rollback would not undo the migration.
func (c oracleConn) Tx(ctx context.Context, fn func(migrate.Conn) error) error {
	return fn(c)
}

// SessionLock holds a row in the lock table until unlock rolls the dedicated connection back.
func (c oracleConn) SessionLock(ctx context.Context, schema, name string) (func() error, error) {
	if c.db == nil || c.db.sqldb == nil {
		return nil, fmt.Errorf("migrate: oracle has no connection")
	}
	table := `"__drizzle_lock"`
	if schema != "" {
		table = `"` + schema + `"."__drizzle_lock"`
	}
	create := fmt.Sprintf(`BEGIN EXECUTE IMMEDIATE 'CREATE TABLE %s ("name" VARCHAR2(128) PRIMARY KEY)'; EXCEPTION WHEN OTHERS THEN IF SQLCODE != -955 THEN RAISE; END IF; END;`, table)
	if _, err := c.db.Exec(ctx, create); err != nil {
		return nil, err
	}
	merge := fmt.Sprintf(`MERGE INTO %s t USING (SELECT :1 AS "name" FROM dual) s ON (t."name" = s."name") WHEN NOT MATCHED THEN INSERT ("name") VALUES (s."name")`, table)
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
	hold := fmt.Sprintf(`SELECT "name" FROM %s WHERE "name" = :1 FOR UPDATE`, table)
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
