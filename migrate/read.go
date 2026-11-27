package migrate

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"
)

// breakpoint matches the rc.5 split marker verbatim.
const breakpoint = "--> statement-breakpoint"

// UpFile is the SQL Run applies. DownFile is the SQL Down applies. The hash is the bytes of up.sql.
const (
	UpFile   = "up.sql"
	DownFile = "down.sql"
)

// DownPlaceholder is the down.sql written when a migration directory is created.
// Down refuses to run it. Replace the file with the SQL that undoes up.sql.
const DownPlaceholder = "-- Replace this file with the SQL that undoes up.sql.\n"

// Migration is one migration on disk. Hash is computed from the raw file bytes. Splitting does not change it.
type Migration struct {
	Name         string
	Path         string
	Statements   []string
	Hash         string
	FolderMillis int64
}

// Read reads a migrations directory on disk. up.sql in each subdirectory is one migration, ordered by directory name. down.sql is not part of the hash.
// The first 14 characters of the directory name are a UTC timestamp YYYYMMDDHHMMSS, stored in created_at.
func Read(folder string) ([]Migration, error) {
	if folder == "" {
		return nil, fmt.Errorf("migrate: missing migrations directory")
	}
	return ReadFS(os.DirFS(folder))
}

// ReadFS reads the same layout as Read from fsys. The root of fsys is the migrations directory.
// embed.FS and os.DirFS both work. down.sql is not part of the hash.
func ReadFS(fsys fs.FS) ([]Migration, error) {
	if fsys == nil {
		return nil, fmt.Errorf("migrate: missing migrations directory")
	}
	journal := path.Join("meta", "_journal.json")
	if _, err := fs.Stat(fsys, journal); err == nil {
		return nil, &JournalError{Path: journal}
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("migrate: read %s: %w", journal, err)
	}
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, fmt.Errorf("migrate: read directory: %w", err)
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
		file := path.Join(name, UpFile)
		raw, err := fs.ReadFile(fsys, file)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("migrate: read %s: %w", file, err)
		}
		millis, err := FolderMillis(name)
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256(raw)
		out = append(out, Migration{
			Name:         name,
			Path:         file,
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

func downStatements(fsys fs.FS, name string) ([]string, error) {
	file := path.Join(name, DownFile)
	raw, err := fs.ReadFile(fsys, file)
	if os.IsNotExist(err) {
		return nil, fmt.Errorf("migrate: migration %q has no down.sql to run", name)
	}
	if err != nil {
		return nil, fmt.Errorf("migrate: read %s: %w", file, err)
	}
	stmts := executableSQL(splitSQL(string(raw)))
	if len(stmts) == 0 {
		return nil, fmt.Errorf("migrate: migration %q has no down.sql to run", name)
	}
	return stmts, nil
}

func executableSQL(stmts []string) []string {
	out := make([]string, 0, len(stmts))
	for _, stmt := range stmts {
		if strings.TrimSpace(stripLineComments(stmt)) == "" {
			continue
		}
		out = append(out, stmt)
	}
	return out
}

func stripLineComments(stmt string) string {
	var b strings.Builder
	for _, line := range strings.Split(stmt, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "--") {
			continue
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
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
