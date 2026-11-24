package migrate

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// breakpoint matches the rc.5 split marker verbatim.
const breakpoint = "--> statement-breakpoint"

// Migration is one migration on disk. Hash is computed from the raw file bytes. Splitting does not change it.
type Migration struct {
	Name         string
	Path         string
	Statements   []string
	Hash         string
	FolderMillis int64
}

// Read reads a migrations directory. migration.sql in each subdirectory is one migration, ordered by directory name.
// The first 14 characters of the directory name are a UTC timestamp YYYYMMDDHHMMSS, stored in created_at.
func Read(folder string) ([]Migration, error) {
	if folder == "" {
		return nil, fmt.Errorf("migrate: missing migrations directory")
	}
	journal := filepath.Join(folder, "meta", "_journal.json")
	if _, err := os.Stat(journal); err == nil {
		return nil, &JournalError{Path: journal}
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("migrate: read %s: %w", journal, err)
	}
	entries, err := os.ReadDir(folder)
	if err != nil {
		return nil, fmt.Errorf("migrate: read directory %s: %w", folder, err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	out := make([]Migration, 0, len(names))
	for _, name := range names {
		path := filepath.Join(folder, name, "migration.sql")
		raw, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("migrate: read %s: %w", path, err)
		}
		millis, err := FolderMillis(name)
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256(raw)
		out = append(out, Migration{
			Name:         name,
			Path:         path,
			Statements:   splitSQL(string(raw)),
			Hash:         hex.EncodeToString(sum[:]),
			FolderMillis: millis,
		})
	}
	return out, nil
}

// FolderMillis parses the first 14 characters of a directory name as UTC milliseconds. An invalid date returns an error and is not rolled into the next month.
func FolderMillis(name string) (int64, error) {
	if len(name) < 14 {
		return 0, fmt.Errorf("migrate: migration directory %q does not start with a 14-digit timestamp", name)
	}
	prefix := name[:14]
	for _, r := range prefix {
		if r < '0' || r > '9' {
			return 0, fmt.Errorf("migrate: migration directory %q does not start with a 14-digit timestamp", name)
		}
	}
	year, _ := strconv.Atoi(prefix[0:4])
	month, _ := strconv.Atoi(prefix[4:6])
	day, _ := strconv.Atoi(prefix[6:8])
	hour, _ := strconv.Atoi(prefix[8:10])
	minute, _ := strconv.Atoi(prefix[10:12])
	second, _ := strconv.Atoi(prefix[12:14])
	got := time.Date(year, time.Month(month), day, hour, minute, second, 0, time.UTC)
	if got.Year() != year || int(got.Month()) != month || got.Day() != day || got.Hour() != hour || got.Minute() != minute || got.Second() != second {
		return 0, fmt.Errorf("migrate: migration directory %q has an invalid timestamp", name)
	}
	return got.UnixMilli(), nil
}

func splitSQL(body string) []string {
	parts := strings.Split(body, breakpoint)
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		stmt := strings.TrimSpace(part)
		if stmt == "" {
			continue
		}
		out = append(out, stmt)
	}
	return out
}
