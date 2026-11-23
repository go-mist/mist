package pg

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/go-mist/mist/diff"
	"github.com/go-mist/mist/migrate"
	"github.com/go-mist/mist/schema"
)

// Written is one write to disk. An empty Name means the schema did not change and no new directory was written.
type Written struct {
	Name    string
	Changes []diff.Change
}

// snapshotFile is the schema after one migration. It sits next to that migration's SQL.
// PrevIDs names the snapshots this one was generated from. A later migration does not rewrite it.
type snapshotFile struct {
	ID      string        `json:"id"`
	PrevIDs []string      `json:"prevIds"`
	Hash    string        `json:"hash"`
	Schema  schema.Schema `json:"schema"`
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

// WriteMigration writes the next change as a new directory containing migration.sql and snapshot.json.
// The snapshot records the schema after this migration, the sha256 of that SQL file, and the id of the snapshot it continues.
// Earlier directories are left unchanged. It does not write meta/_journal.json. The migrator rejects a directory that contains that file.
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
	head, hasHead, err := current(folder)
	if err != nil {
		return Written{}, err
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
	sqlBytes := []byte(sql)
	if err := os.WriteFile(filepath.Join(dir, "migration.sql"), sqlBytes, 0o644); err != nil {
		os.RemoveAll(dir)
		return Written{}, err
	}
	sum := sha256.Sum256(sqlBytes)
	prev := []string{}
	if hasHead {
		prev = []string{head.ID}
	}
	snap := snapshotFile{ID: dirName, PrevIDs: prev, Hash: hex.EncodeToString(sum[:]), Schema: desired}
	if err := writeSnapshot(dir, snap); err != nil {
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
	head, ok, err := current(folder)
	if err != nil {
		return nil, err
	}
	if !ok {
		return diff.Diff(desired, schema.Schema{}), nil
	}
	return diff.Diff(desired, head.Schema), nil
}

func baseline(folder string) (schema.Schema, error) {
	if folder == "" {
		return schema.Schema{}, nil
	}
	head, ok, err := current(folder)
	if err != nil {
		return schema.Schema{}, err
	}
	if !ok {
		return schema.Schema{}, nil
	}
	return head.Schema, nil
}

// current is the snapshot that no later migration continues. The folder is empty when ok is false.
func current(folder string) (snapshotFile, bool, error) {
	snaps, err := loadSnapshots(folder)
	if err != nil || len(snaps) == 0 {
		return snapshotFile{}, false, err
	}
	if err := matchSQL(folder, snaps); err != nil {
		return snapshotFile{}, false, err
	}
	byID := make(map[string]snapshotFile, len(snaps))
	referenced := map[string]struct{}{}
	for _, snap := range snaps {
		byID[snap.ID] = snap
		for _, prev := range snap.PrevIDs {
			referenced[prev] = struct{}{}
		}
	}
	for _, snap := range snaps {
		for _, prev := range snap.PrevIDs {
			if _, ok := byID[prev]; !ok {
				return snapshotFile{}, false, fmt.Errorf("ddl: snapshot %s points at missing %s", snap.ID, prev)
			}
		}
	}
	var heads []string
	var head snapshotFile
	for _, snap := range snaps {
		if _, ok := referenced[snap.ID]; ok {
			continue
		}
		heads = append(heads, snap.ID)
		head = snap
	}
	if len(heads) != 1 {
		return snapshotFile{}, false, fmt.Errorf("ddl: migrations %s do not have one latest snapshot", strings.Join(heads, ", "))
	}
	return head, true, nil
}

func matchSQL(folder string, snaps []snapshotFile) error {
	got, err := migrate.Read(folder)
	if err != nil {
		return err
	}
	byName := make(map[string]string, len(got))
	for _, mig := range got {
		byName[mig.Name] = mig.Hash
	}
	if len(got) != len(snaps) {
		return fmt.Errorf("ddl: found %d migrations, %d snapshots", len(got), len(snaps))
	}
	for _, snap := range snaps {
		hash, ok := byName[snap.ID]
		if !ok || hash != snap.Hash {
			return fmt.Errorf("ddl: migration %s does not match the snapshot", snap.ID)
		}
	}
	return nil
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

func loadSnapshots(folder string) ([]snapshotFile, error) {
	entries, err := os.ReadDir(folder)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []snapshotFile
	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() == "meta" {
			continue
		}
		sqlPath := filepath.Join(folder, entry.Name(), "migration.sql")
		snapPath := filepath.Join(folder, entry.Name(), "snapshot.json")
		_, sqlErr := os.Stat(sqlPath)
		_, snapErr := os.Stat(snapPath)
		if os.IsNotExist(sqlErr) && os.IsNotExist(snapErr) {
			continue
		}
		if sqlErr != nil && !os.IsNotExist(sqlErr) {
			return nil, sqlErr
		}
		if snapErr != nil && !os.IsNotExist(snapErr) {
			return nil, snapErr
		}
		if os.IsNotExist(sqlErr) {
			return nil, fmt.Errorf("ddl: %s/migration.sql is missing", entry.Name())
		}
		if os.IsNotExist(snapErr) {
			return nil, fmt.Errorf("ddl: %s/snapshot.json is missing", entry.Name())
		}
		raw, err := os.ReadFile(snapPath)
		if err != nil {
			return nil, err
		}
		var snap snapshotFile
		if err := json.Unmarshal(raw, &snap); err != nil {
			return nil, fmt.Errorf("ddl: failed to parse %s: %w", snapPath, err)
		}
		if snap.ID != entry.Name() {
			return nil, fmt.Errorf("ddl: snapshot %s has id %q", entry.Name(), snap.ID)
		}
		out = append(out, snap)
	}
	return out, nil
}

func writeSnapshot(dir string, snap snapshotFile) error {
	raw, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	tmp := filepath.Join(dir, "snapshot.json.tmp")
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(dir, "snapshot.json"))
}
