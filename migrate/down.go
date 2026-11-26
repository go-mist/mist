package migrate

import (
	"context"
	"fmt"
	"sort"
)

// downLatest asks rollback to undo only the newest applied migration.
const downLatest = "\x00"

// Down rolls back the latest applied migration and removes its row.
// It returns 1, or 0 when nothing is applied. A missing or placeholder down.sql returns an error and leaves the row in place.
// Each call runs that one migration in one transaction.
func Down(ctx context.Context, conn Conn, cfg Config) (int, error) {
	return rollback(ctx, conn, cfg, downLatest)
}

// DownTo rolls back every applied migration newer than name. name stays applied.
// An empty name rolls back every applied migration. Each migration runs in its own transaction, newest first.
// A later failure leaves the migrations already rolled back removed, and stops on the one that failed.
func DownTo(ctx context.Context, conn Conn, cfg Config, name string) (int, error) {
	return rollback(ctx, conn, cfg, name)
}

func rollback(ctx context.Context, conn Conn, cfg Config, target string) (int, error) {
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
	byName := make(map[string]Migration, len(local))
	for _, m := range local {
		byName[m.Name] = m
	}
	rows, err := appliedRows(ctx, conn, eng, schema, table)
	if err != nil {
		return 0, err
	}
	plan, err := downPlan(rows, target)
	if err != nil {
		return 0, err
	}
	steps := make([]downStep, 0, len(plan))
	for _, name := range plan {
		m, ok := byName[name]
		if !ok {
			return 0, fmt.Errorf("migrate: applied migration %q is not in the migrations directory", name)
		}
		recorded := hashByName(rows, name)
		if recorded != m.Hash {
			return 0, &HashError{Name: m.Name, Path: m.Path}
		}
		stmts, err := downStatements(files, name)
		if err != nil {
			return 0, err
		}
		steps = append(steps, downStep{name: name, hash: m.Hash, path: m.Path, statements: stmts})
	}
	done := 0
	del := eng.deleteSQL(schema, table)
	for _, step := range steps {
		err := conn.Tx(ctx, func(tx Conn) error {
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
			current, err := appliedRows(ctx, tx, eng, schema, table)
			if err != nil {
				return err
			}
			recorded := hashByName(current, step.name)
			if recorded == "" {
				return fmt.Errorf("migrate: migration %q is not applied", step.name)
			}
			if recorded != step.hash {
				return &HashError{Name: step.name, Path: step.path}
			}
			for _, stmt := range step.statements {
				if _, err := tx.Exec(ctx, stmt); err != nil {
					return fmt.Errorf("migrate: down %q failed: %w", step.name, err)
				}
			}
			if _, err := tx.Exec(ctx, del, step.name); err != nil {
				return err
			}
			return nil
		})
		if err != nil {
			return done, err
		}
		done++
	}
	if unlock != nil {
		err = unlock()
		unlock = nil
	}
	if err != nil {
		return done, err
	}
	return done, nil
}

type downStep struct {
	name       string
	hash       string
	path       string
	statements []string
}

func downPlan(rows []appliedRow, target string) ([]string, error) {
	names := make([]string, 0, len(rows))
	seen := map[string]struct{}{}
	for _, row := range rows {
		if row.Name == "" {
			continue
		}
		if _, ok := seen[row.Name]; ok {
			continue
		}
		seen[row.Name] = struct{}{}
		names = append(names, row.Name)
	}
	sort.Strings(names)
	if target == downLatest {
		if len(names) == 0 {
			return nil, nil
		}
		return []string{names[len(names)-1]}, nil
	}
	if target == "" {
		reverse(names)
		return names, nil
	}
	found := false
	newer := make([]string, 0)
	for _, name := range names {
		if name == target {
			found = true
			continue
		}
		if name > target {
			newer = append(newer, name)
		}
	}
	if !found {
		return nil, fmt.Errorf("migrate: down stops at %q, which is not applied", target)
	}
	reverse(newer)
	return newer, nil
}

func hashByName(rows []appliedRow, name string) string {
	for _, row := range rows {
		if row.Name == name {
			return row.Hash
		}
	}
	return ""
}

func reverse(names []string) {
	for i, j := 0, len(names)-1; i < j; i, j = i+1, j-1 {
		names[i], names[j] = names[j], names[i]
	}
}
