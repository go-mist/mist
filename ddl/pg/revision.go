package pg

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/xbmlz/mist/diff"
	"github.com/xbmlz/mist/migrate"
	"github.com/xbmlz/mist/schema"
)

// Written is one write to disk. An empty Name means the schema did not change and no new directory was written.
type Written struct {
	Name    string
	Changes []diff.Change
}

type snapshotFile struct {
	Schema     schema.Schema `json:"schema"`
	Migrations []snapMig     `json:"migrations"`
}

type snapMig struct {
	Name string `json:"name"`
	Hash string `json:"hash"`
}

var migrationName = regexp.MustCompile(`^[A-Za-z0-9_]+$`)

// Plan compares the desired structure with the snapshot in the migrations directory. An empty folder is compared with an empty database.
// When migration files exist but the snapshot does not, it returns an error, so an existing directory is not treated as an empty database and create-table statements are not generated again.
func Plan(folder string, desired schema.Schema) (string, []diff.Change, error) {
	base, err := baseline(folder)
	if err != nil {
		return "", nil, err
	}
	changes := diff.Diff(desired, base)
	sql, err := Render(changes)
	if err != nil {
		return "", nil, err
	}
	return sql, changes, nil
}

// WriteMigration writes the next change as migration.sql in a subdirectory and updates the snapshot.
// The snapshot records the schema after this migration, and the sha256 of each migration.sql.
// It does not write meta/_journal.json. The migrator rejects a whole directory that contains that file.
func WriteMigration(folder, name string, at time.Time, desired schema.Schema) (Written, error) {
	if folder == "" {
		return Written{}, fmt.Errorf("ddl: missing migrations directory")
	}
	if name == "" {
		name = "schema"
	}
	if !migrationName.MatchString(name) {
		return Written{}, fmt.Errorf("ddl: migration name %q may contain only letters, digits, and underscores", name)
	}
	if _, err := Check(folder, desired); err != nil {
		return Written{}, err
	}
	sql, changes, err := Plan(folder, desired)
	if err != nil {
		return Written{}, err
	}
	if sql == "" {
		return Written{Changes: changes}, nil
	}
	if err := os.MkdirAll(folder, 0o755); err != nil {
		return Written{}, err
	}
	dirName, err := uniqueFolder(folder, at, name)
	if err != nil {
		return Written{}, err
	}
	dir := filepath.Join(folder, dirName)
	if err := os.Mkdir(dir, 0o755); err != nil {
		return Written{}, err
	}
	if err := os.WriteFile(filepath.Join(dir, "migration.sql"), []byte(sql), 0o644); err != nil {
		os.RemoveAll(dir)
		return Written{}, err
	}
	if err := saveSnapshot(folder, desired); err != nil {
		os.RemoveAll(dir)
		return Written{}, err
	}
	return Written{Name: dirName, Changes: changes}, nil
}

// Check compares the desired structure with the snapshot. A changed schema returns the changes; a changed migration file returns an error.
func Check(folder string, desired schema.Schema) ([]diff.Change, error) {
	if folder == "" {
		return nil, fmt.Errorf("ddl: missing migrations directory")
	}
	snap, ok, err := loadSnapshot(folder)
	if err != nil {
		return nil, err
	}
	if !ok {
		has, err := hasMigrations(folder)
		if err != nil {
			return nil, err
		}
		if has {
			return nil, fmt.Errorf("ddl: %s already has migrations, but meta/snapshot.json is missing", folder)
		}
		return diff.Diff(desired, schema.Schema{}), nil
	}
	if err := matchMigrations(folder, snap.Migrations); err != nil {
		return nil, err
	}
	return diff.Diff(desired, snap.Schema), nil
}

func baseline(folder string) (schema.Schema, error) {
	if folder == "" {
		return schema.Schema{}, nil
	}
	snap, ok, err := loadSnapshot(folder)
	if err != nil {
		return schema.Schema{}, err
	}
	if ok {
		if err := matchMigrations(folder, snap.Migrations); err != nil {
			return schema.Schema{}, err
		}
		return snap.Schema, nil
	}
	has, err := hasMigrations(folder)
	if err != nil {
		return schema.Schema{}, err
	}
	if has {
		return schema.Schema{}, fmt.Errorf("ddl: %s already has migrations, but meta/snapshot.json is missing", folder)
	}
	return schema.Schema{}, nil
}

func matchMigrations(folder string, want []snapMig) error {
	got, err := migrate.Read(folder)
	if err != nil {
		return err
	}
	if len(got) != len(want) {
		return fmt.Errorf("ddl: found %d migrations, snapshot has %d", len(got), len(want))
	}
	for i := range want {
		if got[i].Name != want[i].Name || got[i].Hash != want[i].Hash {
			return fmt.Errorf("ddl: migration %s does not match the snapshot", got[i].Name)
		}
	}
	return nil
}

func hasMigrations(folder string) (bool, error) {
	entries, err := os.ReadDir(folder)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() == "meta" {
			continue
		}
		_, err := os.Stat(filepath.Join(folder, entry.Name(), "migration.sql"))
		if err == nil {
			return true, nil
		}
		if !os.IsNotExist(err) {
			return false, err
		}
	}
	return false, nil
}

func uniqueFolder(folder string, at time.Time, name string) (string, error) {
	for i := 0; i < 60; i++ {
		dirName := at.Add(time.Duration(i)*time.Second).UTC().Format("20060102150405") + "_" + name
		_, err := os.Stat(filepath.Join(folder, dirName))
		if os.IsNotExist(err) {
			return dirName, nil
		}
		if err != nil {
			return "", err
		}
	}
	return "", fmt.Errorf("ddl: every migration directory in this 60-second window already exists")
}

func loadSnapshot(folder string) (snapshotFile, bool, error) {
	raw, err := os.ReadFile(filepath.Join(folder, "meta", "snapshot.json"))
	if os.IsNotExist(err) {
		return snapshotFile{}, false, nil
	}
	if err != nil {
		return snapshotFile{}, false, err
	}
	var snap snapshotFile
	if err := json.Unmarshal(raw, &snap); err != nil {
		return snapshotFile{}, false, fmt.Errorf("ddl: failed to parse snapshot: %w", err)
	}
	return snap, true, nil
}

func saveSnapshot(folder string, desired schema.Schema) error {
	migs, err := migrate.Read(folder)
	if err != nil {
		return err
	}
	snap := snapshotFile{Schema: desired}
	for _, mig := range migs {
		snap.Migrations = append(snap.Migrations, snapMig{Name: mig.Name, Hash: mig.Hash})
	}
	raw, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	meta := filepath.Join(folder, "meta")
	if err := os.MkdirAll(meta, 0o755); err != nil {
		return err
	}
	tmp := filepath.Join(meta, "snapshot.json.tmp")
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(meta, "snapshot.json"))
}
