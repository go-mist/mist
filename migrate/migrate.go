package migrate

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/go-mist/mist/history"
	"github.com/go-mist/mist/query"
)

// Conn is a connection that can open a transaction.
// pg.Migrator, mysql.Migrator, and sqlite.Migrator return one.
// Driver is "pg", "mysql", "sqlite", "oracle", or "mssql".
type Conn interface {
	Exec(ctx context.Context, sql string, args ...any) (int64, error)
	Query(ctx context.Context, sql string, args ...any) (query.Rows, error)
	Tx(ctx context.Context, fn func(Conn) error) error
	Driver() string
}

// sessionLocker is MySQL. GET_LOCK survives the commit that DDL performs,
// and unlock runs after the migration transaction has finished.
type sessionLocker interface {
	SessionLock(ctx context.Context, schema, name string) (unlock func() error, err error)
}

// Config names where the migrations live and where the migration table is.
// Folder is a disk directory when FS is nil.
// FS reads that same layout from an fs.FS, including embed.FS.
// When both are set, Folder is the directory inside FS.
type Config struct {
	Folder string
	FS     fs.FS
	Schema string
	Table  string
}

// Run applies migrations in the directory whose names are not yet recorded. It returns how many were newly applied.
// An already applied name applies zero migrations the next time. A name missing in the middle is filled in, without consulting the created_at watermark.
// If an already recorded file's contents have changed, Run returns HashError, and the message includes the directory name.
func Run(ctx context.Context, conn Conn, cfg Config) (int, error) {
	files, eng, schema, table, unlock, err := prepare(ctx, conn, cfg)
	if err != nil {
		return 0, err
	}
	defer func() {
		if unlock != nil {
			_ = unlock()
		}
	}()
	local, err := ReadFS(files)
	if err != nil {
		return 0, err
	}
	applied := 0
	err = conn.Tx(ctx, func(tx Conn) error {
		if stmt := eng.preamble(); stmt != "" {
			if _, err := tx.Exec(ctx, stmt); err != nil {
				return err
			}
		}
		if stmt, args := eng.txLock(schema, table); stmt != "" {
			if _, err := tx.Exec(ctx, stmt, args...); err != nil {
				return err
			}
		}
		rows, err := appliedRows(ctx, tx, eng, schema, table)
		if err != nil {
			return err
		}
		pending, err := pending(local, rows)
		if err != nil {
			return err
		}
		insert := eng.insertSQL(schema, table)
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
	if unlock != nil {
		err = unlock()
		unlock = nil
	}
	if err != nil {
		return 0, err
	}
	return applied, nil
}

// prepare opens the migration files, takes the session lock, and creates the migration table.
// The caller releases unlock. A nil unlock means this driver locks inside the transaction.
func prepare(ctx context.Context, conn Conn, cfg Config) (fs.FS, engine, string, string, func() error, error) {
	files, err := openFS(cfg)
	if err != nil {
		return nil, engine{}, "", "", nil, err
	}
	if err := history.ConflictsFS(files); err != nil {
		return nil, engine{}, "", "", nil, err
	}
	if conn == nil {
		return nil, engine{}, "", "", nil, fmt.Errorf("migrate: missing connection")
	}
	eng, err := engineFor(conn.Driver())
	if err != nil {
		return nil, engine{}, "", "", nil, err
	}
	schema, table, err := eng.normalize(cfg)
	if err != nil {
		return nil, engine{}, "", "", nil, err
	}
	var unlock func() error
	if locker, ok := conn.(sessionLocker); ok {
		unlock, err = locker.SessionLock(ctx, schema, fmt.Sprintf("mist:%d", advisoryKey(schema, table)))
		if err != nil {
			return nil, engine{}, "", "", nil, err
		}
	}
	fail := func(err error) (fs.FS, engine, string, string, func() error, error) {
		if unlock != nil {
			_ = unlock()
		}
		return nil, engine{}, "", "", nil, err
	}
	if stmt := eng.schemaSQL(schema); stmt != "" {
		if err := execIdempotent(ctx, conn, eng, stmt); err != nil {
			return fail(err)
		}
	}
	for _, stmt := range eng.prepare(schema, table) {
		if err := execIdempotent(ctx, conn, eng, stmt); err != nil {
			return fail(err)
		}
	}
	columns, err := tableColumns(ctx, conn, eng, schema, table)
	if err != nil {
		return fail(err)
	}
	if len(columns) == 0 {
		if err := execIdempotent(ctx, conn, eng, eng.createTable(schema, table)); err != nil {
			return fail(err)
		}
	} else if !hasName(columns) {
		return fail(&OldTableError{Schema: schema, Table: table})
	}
	return files, eng, schema, table, unlock, nil
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

func appliedRows(ctx context.Context, conn Conn, eng engine, schema, table string) ([]appliedRow, error) {
	rows, err := conn.Query(ctx, eng.appliedSQL(schema, table))
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

func tableColumns(ctx context.Context, conn Conn, eng engine, schema, table string) ([]string, error) {
	stmt, args := eng.columnsSQL(schema, table)
	rows, err := conn.Query(ctx, stmt, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var cols []string
	for rows.Next() {
		name, err := scanText(rows)
		if err != nil {
			return nil, err
		}
		cols = append(cols, name)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return cols, nil
}

func execIdempotent(ctx context.Context, conn Conn, eng engine, stmt string) error {
	var last error
	for range 5 {
		_, err := conn.Exec(ctx, stmt)
		if err == nil {
			return nil
		}
		if eng.retryable(err) {
			last = err
			continue
		}
		return err
	}
	return last
}

func scanText(rows query.Rows) (string, error) {
	var raw any
	if err := rows.Scan(&raw); err != nil {
		return "", err
	}
	switch v := raw.(type) {
	case string:
		return v, nil
	case []byte:
		return string(v), nil
	case nil:
		return "", nil
	default:
		return fmt.Sprint(v), nil
	}
}

var identRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// openFS returns the migrations directory. A nil FS reads Folder from disk.
// A set FS uses Folder as a directory inside it; an empty Folder is the root of FS.
func openFS(cfg Config) (fs.FS, error) {
	if cfg.FS != nil {
		if cfg.Folder == "" {
			return cfg.FS, nil
		}
		sub, err := fs.Sub(cfg.FS, filepath.ToSlash(cfg.Folder))
		if err != nil {
			return nil, fmt.Errorf("migrate: open %s: %w", cfg.Folder, err)
		}
		if _, err := fs.Stat(sub, "."); err != nil {
			return nil, fmt.Errorf("migrate: open %s: %w", cfg.Folder, err)
		}
		return sub, nil
	}
	if cfg.Folder == "" {
		return nil, fmt.Errorf("migrate: missing migrations directory")
	}
	return os.DirFS(cfg.Folder), nil
}

func hasName(cols []string) bool {
	for _, col := range cols {
		if strings.EqualFold(col, "name") {
			return true
		}
	}
	return false
}
