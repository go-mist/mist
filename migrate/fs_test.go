package migrate

import (
	"context"
	"embed"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"
)

//go:embed all:testdata/embed
var embedMigrations embed.FS

func TestReadEmbeddedDirectory(t *testing.T) {
	files, err := openFS(Config{FS: embedMigrations, Folder: "testdata/embed"})
	if err != nil {
		t.Fatal(err)
	}
	got, err := ReadFS(files)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "20240101000000_init" {
		t.Fatalf("migrations = %#v", names(got))
	}
	if len(got[0].Statements) != 1 || got[0].Statements[0] != "select 1;" {
		t.Fatalf("statements = %#v", got[0].Statements)
	}
	sub, err := fs.Sub(embedMigrations, "testdata/embed")
	if err != nil {
		t.Fatal(err)
	}
	rooted, err := ReadFS(sub)
	if err != nil {
		t.Fatal(err)
	}
	if len(rooted) != 1 || rooted[0].Hash != got[0].Hash {
		t.Fatalf("rooted = %#v", rooted)
	}
}

func TestRunReadsSnapshotFromFS(t *testing.T) {
	files := fstest.MapFS{
		"migrations/20240101000000_init/up.sql":        {Data: []byte("select 1;\n")},
		"migrations/20240101000000_init/down.sql":      {Data: []byte(DownPlaceholder)},
		"migrations/20240101000000_init/snapshot.json": {Data: []byte("{")},
	}
	_, err := Run(context.Background(), nil, Config{FS: files, Folder: "migrations"})
	if err == nil || !strings.Contains(err.Error(), "snapshot.json") {
		t.Fatal(err)
	}
}

func TestRunEmbeddedStopsBeforeConnection(t *testing.T) {
	_, err := Run(context.Background(), nil, Config{FS: embedMigrations, Folder: "testdata/embed"})
	if err == nil || !strings.Contains(err.Error(), "missing connection") {
		t.Fatal(err)
	}
}

func TestOpenFSMissingEmbedDirectory(t *testing.T) {
	_, err := openFS(Config{FS: embedMigrations, Folder: "missing"})
	if err == nil || !strings.Contains(err.Error(), "migrate: open missing") {
		t.Fatal(err)
	}
}
