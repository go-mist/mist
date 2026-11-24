package migrate

import (
	"errors"
	"fmt"
	"strings"
)

const lockTable = "__drizzle_lock"

// engine is the SQL for one database this repository migrates.
// pg, mysql, and sqlite are the known names. A new database is added in this repository.
type engine struct {
	name string
}

func engineFor(name string) (engine, error) {
	switch name {
	case "pg", "postgres":
		return engine{name: "pg"}, nil
	case "mysql":
		return engine{name: "mysql"}, nil
	case "sqlite":
		return engine{name: "sqlite"}, nil
	default:
		return engine{}, fmt.Errorf("migrate: driver %q is not pg, mysql, or sqlite", name)
	}
}

func (e engine) normalize(cfg Config) (string, string, error) {
	schema := cfg.Schema
	if schema == "" {
		if e.name == "sqlite" {
			schema = "main"
		} else {
			schema = defaultSchema
		}
	}
	if e.name == "sqlite" && schema != "main" {
		return "", "", fmt.Errorf("migrate: sqlite has no schema %q", schema)
	}
	table := cfg.Table
	if table == "" {
		table = defaultTable
	}
	if !identRE.MatchString(schema) || !identRE.MatchString(table) {
		return "", "", fmt.Errorf("migrate: schema %q or table %q is not a safe identifier", schema, table)
	}
	return schema, table, nil
}

func (e engine) qual(schema, table string) string {
	if e.name == "sqlite" {
		return quoteIdent(table)
	}
	q := quoteIdent
	if e.name == "mysql" {
		q = quoteBacktick
	}
	return q(schema) + "." + q(table)
}

func (e engine) ph(n int) string {
	if e.name == "pg" {
		return fmt.Sprintf("$%d", n)
	}
	return "?"
}

func (e engine) schemaSQL(schema string) string {
	switch e.name {
	case "sqlite":
		return ""
	case "mysql":
		return "CREATE DATABASE IF NOT EXISTS " + quoteBacktick(schema)
	default:
		return "CREATE SCHEMA IF NOT EXISTS " + quoteIdent(schema)
	}
}

func (e engine) createTable(schema, table string) string {
	switch e.name {
	case "mysql":
		return fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
				id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
				hash text NOT NULL,
				created_at bigint,
				name text,
				applied_at timestamp NULL DEFAULT CURRENT_TIMESTAMP
			)`, e.qual(schema, table))
	case "sqlite":
		return fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
				id integer PRIMARY KEY,
				hash text NOT NULL,
				created_at integer,
				name text,
				applied_at text DEFAULT CURRENT_TIMESTAMP
			)`, e.qual(schema, table))
	default:
		return CreateTableSQL(schema, table)
	}
}

// prepare runs before the transaction. SQLite creates the lock table here.
// The write that takes the lock runs inside the transaction.
func (e engine) prepare(schema, table string) []string {
	if e.name != "sqlite" {
		return nil
	}
	return []string{
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (name text NOT NULL PRIMARY KEY)`, quoteIdent(lockTable)),
	}
}

// txLock is the statement that runs inside the transaction before reading applied names.
// MySQL holds GET_LOCK on the connection instead, because DDL commits the transaction.
func (e engine) txLock(schema, table string) (string, []any) {
	switch e.name {
	case "pg":
		return "select pg_advisory_xact_lock($1)", []any{advisoryKey(schema, table)}
	case "sqlite":
		return fmt.Sprintf(
			`insert into %s (name) values (?) on conflict(name) do update set name = excluded.name`,
			quoteIdent(lockTable),
		), []any{table}
	default:
		return "", nil
	}
}

func (e engine) preamble() string {
	if e.name == "sqlite" {
		return "PRAGMA busy_timeout = 60000"
	}
	return ""
}

func (e engine) columnsSQL(schema, table string) (string, []any) {
	switch e.name {
	case "sqlite":
		return fmt.Sprintf("select name from pragma_table_info(%s)", quoteLiteral(table)), nil
	case "mysql":
		return "select column_name from information_schema.columns where table_schema = ? and table_name = ? order by ordinal_position", []any{schema, table}
	default:
		return "select column_name from information_schema.columns where table_schema = $1 and table_name = $2 order by ordinal_position", []any{schema, table}
	}
}

func (e engine) appliedSQL(schema, table string) string {
	return fmt.Sprintf("select hash, name from %s", e.qual(schema, table))
}

func (e engine) insertSQL(schema, table string) string {
	return fmt.Sprintf(
		"insert into %s (%s, %s, %s) values (%s, %s, %s)",
		e.qual(schema, table),
		e.quote("hash"), e.quote("created_at"), e.quote("name"),
		e.ph(1), e.ph(2), e.ph(3),
	)
}

func (e engine) quote(name string) string {
	if e.name == "mysql" {
		return quoteBacktick(name)
	}
	return quoteIdent(name)
}

func (e engine) retryable(err error) bool {
	if err == nil {
		return false
	}
	switch e.name {
	case "pg":
		var state interface{ SQLState() string }
		return errors.As(err, &state) && state.SQLState() == "23505"
	case "mysql":
		msg := err.Error()
		return strings.Contains(msg, "Error 1062") || strings.Contains(msg, "Error 1007") || strings.Contains(msg, "Error 1050")
	default:
		return false
	}
}

func quoteBacktick(name string) string {
	return "`" + strings.ReplaceAll(name, "`", "``") + "`"
}

func quoteLiteral(name string) string {
	return "'" + strings.ReplaceAll(name, "'", "''") + "'"
}
