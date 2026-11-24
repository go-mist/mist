package pg

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/go-mist/mist/diff"
	"github.com/go-mist/mist/history"
	"github.com/go-mist/mist/migrate"
	"github.com/go-mist/mist/schema"
)

// Written is one write to disk. An empty Name means the schema did not change and no new directory was written.
type Written struct {
	Name    string
	Changes []diff.Change
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

// WriteMigration writes the next change as a new directory containing up.sql, down.sql, and snapshot.json. down.sql is reserved and is not applied.
// The snapshot records the schema after this migration, the sha256 of that SQL file, and the ids of the snapshots it continues.
// Two latest snapshots that change different columns, indexes, or constraints are both recorded. A shared object stops the write.
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
	state, hasHead, err := openHistory(folder)
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
	if err := os.WriteFile(filepath.Join(dir, migrate.UpFile), sqlBytes, 0o644); err != nil {
		os.RemoveAll(dir)
		return Written{}, err
	}
	if err := os.WriteFile(filepath.Join(dir, migrate.DownFile), []byte(migrate.DownPlaceholder), 0o644); err != nil {
		os.RemoveAll(dir)
		return Written{}, err
	}
	sum := sha256.Sum256(sqlBytes)
	prev := []string{}
	if hasHead {
		prev = append([]string{}, state.Heads...)
	}
	snap := history.Snapshot{ID: dirName, PrevIDs: prev, Hash: hex.EncodeToString(sum[:]), Schema: desired}
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
	state, ok, err := openHistory(folder)
	if err != nil {
		return nil, err
	}
	if !ok {
		return diff.Diff(desired, schema.Schema{}), nil
	}
	return diff.Diff(desired, state.Schema), nil
}

func baseline(folder string) (schema.Schema, error) {
	if folder == "" {
		return schema.Schema{}, nil
	}
	state, ok, err := openHistory(folder)
	if err != nil {
		return schema.Schema{}, err
	}
	if !ok {
		return schema.Schema{}, nil
	}
	return state.Schema, nil
}

// openHistory is the merged schema of the latest snapshots. ok is false when the folder has none.
func openHistory(folder string) (history.State, bool, error) {
	snaps, err := history.Load(folder)
	if err != nil || len(snaps) == 0 {
		return history.State{}, false, err
	}
	if err := matchSQL(folder, snaps); err != nil {
		return history.State{}, false, err
	}
	state, err := history.Resolve(snaps)
	if err != nil {
		return history.State{}, false, err
	}
	return state, true, nil
}

func matchSQL(folder string, snaps []history.Snapshot) error {
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

func writeSnapshot(dir string, snap history.Snapshot) error {
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
