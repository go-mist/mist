//go:build integration

// Package testdb opens a temporary database for integration tests.
//
// DRIZZLE_PG_DSN and DRIZZLE_MYSQL_DSN, when set, are used as-is so a local
// server still works. Otherwise PostgreSQL and MySQL start with testcontainers.
// SQLite is a temporary file: the driver embeds the engine and does not talk to a server.
// One container is started per test process and reused by every test in that process.
// Do not run two packages against the same DRIZZLE_PG_DSN at the same time; they drop the same tables.
package testdb

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go/modules/mysql"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

const (
	pgImage    = "postgres:16-alpine"
	mysqlImage = "mysql:8.4"
)

var (
	pgOnce sync.Once
	pgDSN  string
	pgErr  error

	myOnce sync.Once
	myDSN  string
	myErr  error
)

// PostgresDSN returns DRIZZLE_PG_DSN or starts postgres:16-alpine.
func PostgresDSN(t *testing.T) string {
	t.Helper()
	if dsn := os.Getenv("DRIZZLE_PG_DSN"); dsn != "" {
		return dsn
	}
	pgOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		c, err := postgres.Run(ctx, pgImage,
			postgres.WithDatabase("drizzle"),
			postgres.WithUsername("postgres"),
			postgres.WithPassword("postgres"),
			postgres.BasicWaitStrategies(),
		)
		if err != nil {
			pgErr = err
			return
		}
		pgDSN, pgErr = c.ConnectionString(ctx, "sslmode=disable")
	})
	if pgErr != nil {
		t.Fatalf("testdb: start postgres (set DRIZZLE_PG_DSN to use an existing server): %v", pgErr)
	}
	return pgDSN
}

// MySQLDSN returns DRIZZLE_MYSQL_DSN or starts mysql:8.4.
// The DSN includes parseTime=true.
func MySQLDSN(t *testing.T) string {
	t.Helper()
	if dsn := os.Getenv("DRIZZLE_MYSQL_DSN"); dsn != "" {
		return dsn
	}
	myOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		c, err := mysql.Run(ctx, mysqlImage,
			mysql.WithDatabase("mist"),
			mysql.WithUsername("root"),
			mysql.WithPassword("root"),
		)
		if err != nil {
			myErr = err
			return
		}
		myDSN, myErr = c.ConnectionString(ctx, "parseTime=true")
	})
	if myErr != nil {
		t.Fatalf("testdb: start mysql (set DRIZZLE_MYSQL_DSN to use an existing server): %v", myErr)
	}
	return myDSN
}

// SQLiteDSN returns a path to a new empty database file.
func SQLiteDSN(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "mist.db")
}
