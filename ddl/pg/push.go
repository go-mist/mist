package pg

import (
	"context"
	"fmt"
	"strings"

	"github.com/go-mist/mist/diff"
)

// Execute runs statements split on statement-breakpoint. The caller owns the transaction and search_path.
func Execute(ctx context.Context, db interface {
	Exec(ctx context.Context, sql string, args ...any) (int64, error)
}, sqlText string) error {
	for _, stmt := range splitSQL(sqlText) {
		if _, err := db.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("ddl: %s: %w", stmt, err)
		}
	}
	return nil
}

// Confirm accepts only y or yes. Empty input and any other answer do not apply.
func Confirm(answer string) bool {
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "y", "yes":
		return true
	default:
		return false
	}
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

func splitSQL(sqlText string) []string {
	var out []string
	for _, part := range strings.Split(sqlText, breakpoint) {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}
