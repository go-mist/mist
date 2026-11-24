package history

import (
	"strings"
	"testing"
	"testing/fstest"
)

func TestLoadFSReadsSnapshot(t *testing.T) {
	files := fstest.MapFS{
		"20240101000000_init/up.sql": {Data: []byte("select 1;\n")},
		"20240101000000_init/snapshot.json": {Data: []byte(`{
			"id": "20240101000000_init",
			"prevIds": [],
			"hash": "abc",
			"schema": {}
		}`)},
	}
	snaps, err := LoadFS(files)
	if err != nil {
		t.Fatal(err)
	}
	if len(snaps) != 1 || snaps[0].ID != "20240101000000_init" || snaps[0].Hash != "abc" {
		t.Fatalf("%#v", snaps)
	}
}

func TestLoadMissingFolder(t *testing.T) {
	snaps, err := Load(t.TempDir() + "/missing")
	if err != nil || snaps != nil {
		t.Fatalf("snaps=%v err=%v", snaps, err)
	}
}

func TestConflictsFSRejectsBadSnapshot(t *testing.T) {
	files := fstest.MapFS{
		"20240101000000_init/up.sql":        {Data: []byte("select 1;\n")},
		"20240101000000_init/snapshot.json": {Data: []byte("{")},
	}
	err := ConflictsFS(files)
	if err == nil || !strings.Contains(err.Error(), "snapshot.json") {
		t.Fatal(err)
	}
}
