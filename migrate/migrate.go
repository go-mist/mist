package migrate

import (
	"context"
	"errors"
	"fmt"
	"regexp"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/go-mist/mist/query"
)

// Conn is a connection that can open a transaction. Attach pg.DB with UsePG.
type Conn interface {
	Exec(ctx context.Context, sql string, args ...any) (int64, error)
	Query(ctx context.Context, sql string, args ...any) (query.Rows, error)
	Tx(ctx context.Context, fn func(Conn) error) error
}

// Config names the migrations directory and the location of the migration table.
type Config struct {
	Folder string
	Schema string
	Table  string
}

// Run applies migrations in the directory whose names are not yet recorded. It returns how many were newly applied.
// An already applied name applies zero migrations the next time. A name missing in the middle is filled in, without consulting the created_at watermark.
// If an already recorded file's contents have changed, Run returns HashError, and the message includes the directory name.
func Run(ctx context.Context, conn Conn, cfg Config) (int, error) {
	if conn == nil {
		return 0, fmt.Errorf("migrate: missing connection")
	}
	schema, table, err := normalize(cfg)
	if err != nil {
		return 0, err
	}
	local, err := Read(cfg.Folder)
	if err != nil {
		return 0, err
	}
	if err := execIdempotent(ctx, conn, "CREATE SCHEMA IF NOT EXISTS "+quoteIdent(schema)); err != nil {
		return 0, err
	}
	columns, err := tableColumns(ctx, conn, schema, table)
	if err != nil {
		return 0, err
	}
	if len(columns) == 0 {
		if err := execIdempotent(ctx, conn, CreateTableSQL(schema, table)); err != nil {
			return 0, err
		}
	} else if !hasColumn(columns, "name") {
		return 0, &OldTableError{Schema: schema, Table: table}
	}
	applied := 0
	err = conn.Tx(ctx, func(tx Conn) error {
		if _, err := tx.Exec(ctx, "select pg_advisory_xact_lock($1)", advisoryKey(schema, table)); err != nil {
			return err
		}
		rows, err := appliedRows(ctx, tx, schema, table)
		if err != nil {
			return err
		}
		pending, err := pending(local, rows)
		if err != nil {
			return err
		}
		insert := fmt.Sprintf(`insert into %s.%s ("hash", "created_at", "name") values ($1, $2, $3)`, quoteIdent(schema), quoteIdent(table))
		for _, m := range pending {
			for _, stmt := range m.Statements {
				if _, err := tx.Exec(ctx, stmt); err != nil {
					return fmt.Errorf("migrate: migration %q failed: %w", m.Name, err)
				}
			}
			if _, err := tx.Exec(ctx, insert, m.Hash, m.FolderMillis, m.Name); err != nil {
				return err
			}
		}
		applied = len(pending)
		return nil
	})
	if err != nil {
		return 0, err
	}
	return applied, nil
}

type appliedRow struct {
	Name string
	Hash string
}

func pending(local []Migration, rows []appliedRow) ([]Migration, error) {
	byName := make(map[string]string, len(rows))
	for _, row := range rows {
		if row.Name == "" {
			continue
		}
		byName[row.Name] = row.Hash
	}
	out := make([]Migration, 0, len(local))
	for _, m := range local {
		hash, ok := byName[m.Name]
		if !ok {
			out = append(out, m)
			continue
		}
		if hash != m.Hash {
			return nil, &HashError{Name: m.Name, Path: m.Path}
		}
	}
	return out, nil
}

func appliedRows(ctx context.Context, conn Conn, schema, table string) ([]appliedRow, error) {
	q := fmt.Sprintf(`select hash, name from %s.%s`, quoteIdent(schema), quoteIdent(table))
	rows, err := conn.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []appliedRow
	for rows.Next() {
		var hash string
		var name *string
		if err := rows.Scan(&hash, &name); err != nil {
			return nil, err
		}
		if name == nil {
			continue
		}
		out = append(out, appliedRow{Name: *name, Hash: hash})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func tableColumns(ctx context.Context, conn Conn, schema, table string) ([]string, error) {
	rows, err := conn.Query(ctx, `
		select column_name from information_schema.columns
		where table_schema = $1 and table_name = $2
		order by ordinal_position`, schema, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var cols []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		cols = append(cols, name)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return cols, nil
}

func execIdempotent(ctx context.Context, conn Conn, stmt string) error {
	var last error
	for range 5 {
		_, err := conn.Exec(ctx, stmt)
		if err == nil {
			return nil
		}
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			last = err
			continue
		}
		return err
	}
	return last
}

func hasColumn(cols []string, name string) bool {
	for _, col := range cols {
		if col == name {
			return true
		}
	}
	return false
}

var identRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func normalize(cfg Config) (string, string, error) {
	schema := cfg.Schema
	if schema == "" {
		schema = defaultSchema
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
