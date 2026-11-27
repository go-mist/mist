// Package ddl writes and checks migration folders. The SQL inside a folder comes from a dialect renderer.
package ddl

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

// Render turns a diff into the body of up.sql.
type Render func(changes []diff.Change) (string, error)

var migrationName = regexp.MustCompile(`^[A-Za-z0-9_]+$`)

// Plan compares the desired structure with the snapshot in the migrations directory.
func Plan(folder string, desired schema.Schema, render Render) (string, []diff.Change, error) {
	base, err := baseline(folder)
	if err != nil {
		return "", nil, err
	}
	changes := diff.Diff(desired, base)
	sql, err := render(changes)
	if err != nil {
		return "", nil, err
	}
	return sql, changes, nil
}

// WriteMigration writes the next change as a new directory containing up.sql, down.sql, and snapshot.json. down.sql starts as a placeholder. migrate down refuses to run that placeholder.
func WriteMigration(folder, name string, at time.Time, desired schema.Schema, render Render) (Written, error) {
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
	sql, changes, err := Plan(folder, desired, render)
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
		return Written{}, fmt.Errorf("ddl: create migrations directory %s: %w", folder, err)
	}
	dirName, err := uniqueFolder(folder, at, name)
	if err != nil {
		return Written{}, err
	}
	dir := filepath.Join(folder, dirName)
	if err := os.Mkdir(dir, 0o755); err != nil {
		return Written{}, fmt.Errorf("ddl: create migration directory %s: %w", dir, err)
	}
	sqlBytes := []byte(sql)
	if err := os.WriteFile(filepath.Join(dir, migrate.UpFile), sqlBytes, 0o644); err != nil {
		os.RemoveAll(dir)
		return Written{}, fmt.Errorf("ddl: write %s: %w", filepath.Join(dir, migrate.UpFile), err)
	}
	if err := os.WriteFile(filepath.Join(dir, migrate.DownFile), []byte(migrate.DownPlaceholder), 0o644); err != nil {
		os.RemoveAll(dir)
		return Written{}, fmt.Errorf("ddl: write %s: %w", filepath.Join(dir, migrate.DownFile), err)
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

// Check compares the desired structure with the snapshot. A changed schema returns the changes. A changed migration file returns an error.
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
		return fmt.Errorf("ddl: directory %s has %d migrations and %d snapshots", folder, len(got), len(snaps))
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
			return "", fmt.Errorf("ddl: read migration directory %s: %w", folder, err)
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
	path := filepath.Join(dir, "snapshot.json")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return fmt.Errorf("ddl: write %s: %w", path, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("ddl: write %s: %w", path, err)
	}
	return nil
}

// OrderTables orders tables so a referenced table comes first. The bool is false when foreign keys form a cycle.
func OrderTables(tables []schema.Table) ([]schema.Table, bool) {
	byName := map[string]schema.Table{}
	indeg := map[string]int{}
	deps := map[string][]string{}
	var order []string
	for _, table := range tables {
		byName[table.Name] = table
		order = append(order, table.Name)
		indeg[table.Name] = 0
	}
	for _, table := range tables {
		seen := map[string]struct{}{}
		for _, fk := range table.ForeignKeys {
			if _, ok := byName[fk.RefTable]; !ok || fk.RefTable == table.Name {
				continue
			}
			if _, ok := seen[fk.RefTable]; ok {
				continue
			}
			seen[fk.RefTable] = struct{}{}
			deps[fk.RefTable] = append(deps[fk.RefTable], table.Name)
			indeg[table.Name]++
		}
	}
	var ready []string
	for _, name := range order {
		if indeg[name] == 0 {
			ready = append(ready, name)
		}
	}
	var out []schema.Table
	for len(ready) > 0 {
		name := ready[0]
		ready = ready[1:]
		out = append(out, byName[name])
		for _, child := range deps[name] {
			indeg[child]--
			if indeg[child] == 0 {
				ready = append(ready, child)
			}
		}
	}
	if len(out) != len(tables) {
		return nil, false
	}
	return out, true
}
