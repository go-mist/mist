package ddl

import (
	"context"
	"fmt"
	"strings"

	"github.com/go-mist/mist/diff"
)

const breakpoint = "--> statement-breakpoint"

// Execute runs statements split on statement-breakpoint. The caller owns the transaction.
func Execute(ctx context.Context, db interface {
	Exec(ctx context.Context, sql string, args ...any) (int64, error)
}, sqlText string) error {
	for _, stmt := range Split(sqlText) {
		if _, err := db.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("ddl: %s: %w", stmt, err)
		}
	}
	return nil
}

// Split divides migration SQL on the statement breakpoint.
func Split(sqlText string) []string {
	var out []string
	for _, part := range strings.Split(sqlText, breakpoint) {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

// HasDestructive reports a dropped table, a dropped column, a changed type, or a dropped enum among the changes.
func HasDestructive(changes []diff.Change) bool {
	for _, change := range changes {
		if change.Destructive {
			return true
		}
	}
	return false
}
