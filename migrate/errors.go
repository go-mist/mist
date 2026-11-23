package migrate

import "fmt"

// HashError means a migration already recorded in the database does not match the file on disk.
type HashError struct {
	Name string
	Path string
}

func (e *HashError) Error() string {
	return fmt.Sprintf("migrate: applied migration %q changed; file is %s", e.Name, e.Path)
}

// OldTableError means the migration table is still v0 and has no name column.
type OldTableError struct {
	Schema string
	Table  string
}

func (e *OldTableError) Error() string {
	return fmt.Sprintf("migrate: migration table %q.%q has no name column; that is a v0 layout. This library does not run upToV1. Run drizzle-kit up, or add name and applied_at yourself", e.Schema, e.Table)
}

// JournalError means the directory is still an old drizzle-kit journal.
type JournalError struct {
	Path string
}

func (e *JournalError) Error() string {
	return fmt.Sprintf("migrate: found %s, an old drizzle-kit directory. Upgrade drizzle-kit and run drizzle-kit up first", e.Path)
}
