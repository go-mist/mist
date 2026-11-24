package mist

import (
	"context"

	"github.com/go-mist/mist/query"
)

// DB is a connection. pg.Open, mysql.Open, sqlite.Open, and oracle.Open all return this.
// The application chooses which Open to call. Query code after that uses DB and Tx.
type DB interface {
	query.Executor
	Close() error
	Tx(ctx context.Context, fn func(Tx) error) error
}

// Tx is a transaction or a savepoint. It renders with the same dialect as the connection that opened it.
type Tx interface {
	query.Executor
	Tx(ctx context.Context, fn func(Tx) error) error
}
