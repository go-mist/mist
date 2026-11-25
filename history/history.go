// Package history reads the migration snapshot graph.
// Each directory holds the schema after that migration and the ids it continues.
// A later migration does not rewrite an earlier directory.
package history

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path"
	"sort"

	"github.com/go-mist/mist/schema"
)

// Snapshot is one migration directory's snapshot.json.
type Snapshot struct {
	ID      string        `json:"id"`
	PrevIDs []string      `json:"prevIds"`
	Hash    string        `json:"hash"`
	Schema  schema.Schema `json:"schema"`
}

// State is the schema after every latest snapshot, and the ids the next migration continues.
type State struct {
	Schema schema.Schema
	Heads  []string
}

// Load reads snapshot.json from each migration directory on disk.
// A directory with up.sql and no snapshot is an error.
// A missing folder returns no snapshots.
func Load(folder string) ([]Snapshot, error) {
	if folder == "" {
		return nil, fmt.Errorf("open : no such file or directory")
	}
	return LoadFS(os.DirFS(folder))
}

// LoadFS reads snapshot.json from fsys. The root of fsys is the migrations directory.
func LoadFS(fsys fs.FS) ([]Snapshot, error) {
	snaps, _, err := readSnapshots(fsys, false)
	return snaps, err
}

// Conflicts reports latest snapshots that change the same object.
// A folder with no snapshot.json files returns nil, so a directory of hand-written SQL still migrates.
func Conflicts(folder string) error {
	if folder == "" {
		return nil
	}
	return ConflictsFS(os.DirFS(folder))
}

// ConflictsFS is Conflicts for an fs.FS rooted at the migrations directory.
func ConflictsFS(fsys fs.FS) error {
	if fsys == nil {
		return nil
	}
	snaps, err := relaxed(fsys)
	if err != nil || len(snaps) == 0 {
		return err
	}
	_, err = Resolve(snaps)
	return err
}

// Resolve merges the latest snapshots. One latest snapshot returns that schema.
// Several latest snapshots merge when each column, index, and constraint was changed by only one of them.
func Resolve(snaps []Snapshot) (State, error) {
	if len(snaps) == 0 {
		return State{}, nil
	}
	byID := make(map[string]Snapshot, len(snaps))
	referenced := map[string]struct{}{}
	for _, snap := range snaps {
		if _, ok := byID[snap.ID]; ok {
			return State{}, fmt.Errorf("ddl: snapshot %s is duplicated", snap.ID)
		}
		byID[snap.ID] = snap
		for _, prev := range snap.PrevIDs {
			referenced[prev] = struct{}{}
		}
	}
	for _, snap := range snaps {
		for _, prev := range snap.PrevIDs {
			if _, ok := byID[prev]; !ok {
				return State{}, fmt.Errorf("ddl: snapshot %s points at missing %s", snap.ID, prev)
			}
		}
		anc := ancestors(byID, snap.ID)
		if _, ok := anc[snap.ID]; ok {
			return State{}, fmt.Errorf("ddl: snapshot %s is part of a cycle", snap.ID)
		}
	}
	heads := make([]string, 0, 1)
	for _, snap := range snaps {
		if _, ok := referenced[snap.ID]; ok {
			continue
		}
		heads = append(heads, snap.ID)
	}
	sort.Strings(heads)
	if len(heads) == 0 {
		return State{}, fmt.Errorf("ddl: snapshots have no latest migration")
	}
	merged, err := mergeHeads(byID, heads)
	if err != nil {
		return State{}, err
	}
	return State{Schema: merged, Heads: heads}, nil
}

func relaxed(fsys fs.FS) ([]Snapshot, error) {
	snaps, sawSnapshot, err := readSnapshots(fsys, true)
	if err != nil || !sawSnapshot {
		return nil, err
	}
	return snaps, nil
}

// readSnapshots lists migration directories. When allowSQLOnly is set and the folder has
// no snapshot.json, SQL-only directories are left for migrate.Read.
func readSnapshots(fsys fs.FS, allowSQLOnly bool) ([]Snapshot, bool, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	var snaps []Snapshot
	var sqlOnly []string
	sawSnapshot := false
	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() == "meta" {
			continue
		}
		sqlPath := path.Join(entry.Name(), "up.sql")
		snapPath := path.Join(entry.Name(), "snapshot.json")
		_, sqlErr := fs.Stat(fsys, sqlPath)
		_, snapErr := fs.Stat(fsys, snapPath)
		if os.IsNotExist(sqlErr) && os.IsNotExist(snapErr) {
			continue
		}
		if sqlErr != nil && !os.IsNotExist(sqlErr) {
			return nil, false, sqlErr
		}
		if snapErr != nil && !os.IsNotExist(snapErr) {
			return nil, false, snapErr
		}
		if os.IsNotExist(snapErr) {
			sqlOnly = append(sqlOnly, entry.Name())
			continue
		}
		sawSnapshot = true
		if os.IsNotExist(sqlErr) {
			return nil, false, fmt.Errorf("ddl: %s/up.sql is missing", entry.Name())
		}
		raw, err := fs.ReadFile(fsys, snapPath)
		if err != nil {
			return nil, false, err
		}
		var snap Snapshot
		if err := json.Unmarshal(raw, &snap); err != nil {
			return nil, false, fmt.Errorf("ddl: failed to parse %s: %w", snapPath, err)
		}
		if snap.ID != entry.Name() {
			return nil, false, fmt.Errorf("ddl: snapshot %s has id %q", entry.Name(), snap.ID)
		}
		snaps = append(snaps, snap)
	}
	if len(sqlOnly) > 0 && (!allowSQLOnly || sawSnapshot) {
		sort.Strings(sqlOnly)
		return nil, sawSnapshot, fmt.Errorf("ddl: %s/snapshot.json is missing", sqlOnly[0])
	}
	return snaps, sawSnapshot, nil
}

func mergeHeads(byID map[string]Snapshot, heads []string) (schema.Schema, error) {
	if len(heads) == 0 {
		return schema.Schema{}, nil
	}
	if len(heads) == 1 {
		return byID[heads[0]].Schema, nil
	}
	common := intersectAncestors(byID, heads)
	maximal := maximalAncestors(byID, common)
	base, err := mergeHeads(byID, maximal)
	if err != nil {
		return schema.Schema{}, err
	}
	sides := make([]schema.Schema, len(heads))
	for i, id := range heads {
		sides[i] = byID[id].Schema
	}
	return mergeSchemas(base, sides, heads)
}

func intersectAncestors(byID map[string]Snapshot, heads []string) map[string]struct{} {
	var common map[string]struct{}
	for _, id := range heads {
		anc := ancestors(byID, id)
		if common == nil {
			common = anc
			continue
		}
		for id := range common {
			if _, ok := anc[id]; !ok {
				delete(common, id)
			}
		}
	}
	if common == nil {
		return map[string]struct{}{}
	}
	return common
}

func maximalAncestors(byID map[string]Snapshot, common map[string]struct{}) []string {
	ids := make([]string, 0, len(common))
	for id := range common {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		ancestorOfOther := false
		for _, other := range ids {
			if other == id {
				continue
			}
			if _, ok := ancestors(byID, other)[id]; ok {
				ancestorOfOther = true
				break
			}
		}
		if !ancestorOfOther {
			out = append(out, id)
		}
	}
	return out
}

func ancestors(byID map[string]Snapshot, id string) map[string]struct{} {
	out := map[string]struct{}{}
	var walk func(string)
	walk = func(id string) {
		snap, ok := byID[id]
		if !ok {
			return
		}
		for _, prev := range snap.PrevIDs {
			if _, seen := out[prev]; seen {
				continue
			}
			out[prev] = struct{}{}
			walk(prev)
		}
	}
	walk(id)
	return out
}
