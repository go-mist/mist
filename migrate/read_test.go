package migrate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFolderMillis(t *testing.T) {
	got, err := FolderMillis("20241120083000_init")
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2024, 11, 20, 8, 30, 0, 0, time.UTC).UnixMilli()
	if got != want {
		t.Fatalf("millis = %d, want %d", got, want)
	}
	if _, err := FolderMillis("20241131000000_bad"); err == nil {
		t.Fatal("expected invalid day")
	}
	if _, err := FolderMillis("short"); err == nil {
		t.Fatal("expected short name")
	}
}

func TestReadSplitsAndSorts(t *testing.T) {
	dir := t.TempDir()
	writeMigration(t, dir, "20240102000000_b", "select 2;\n--> statement-breakpoint\n\n--> statement-breakpoint\nselect 3;\n")
	writeMigration(t, dir, "20240101000000_a", "select 1;")
	got, err := Read(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Name != "20240101000000_a" || got[1].Name != "20240102000000_b" {
		t.Fatalf("order = %#v", names(got))
	}
	if len(got[1].Statements) != 2 || got[1].Statements[0] != "select 2;" || got[1].Statements[1] != "select 3;" {
		t.Fatalf("statements = %#v", got[1].Statements)
	}
	if got[0].Hash == got[1].Hash || len(got[0].Hash) != 64 {
		t.Fatalf("hash = %s", got[0].Hash)
	}
}

func TestReadNamesTheDirectory(t *testing.T) {
	_, err := Read(filepath.Join(t.TempDir(), "missing"))
	if err == nil || !strings.Contains(err.Error(), "migrate: read directory") {
		t.Fatal(err)
	}
	dir := t.TempDir()
	name := "20240101000000_init"
	if err := os.Mkdir(filepath.Join(dir, name), 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, name, "up.sql")
	if err := os.Mkdir(file, 0o755); err != nil {
		t.Fatal(err)
	}
	_, err = Read(dir)
	if err == nil || !strings.Contains(err.Error(), "migrate: read "+name+"/up.sql") {
		t.Fatal(err)
	}
}

func TestReadRejectsJournal(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "meta"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "meta", "_journal.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Read(dir)
	if err == nil {
		t.Fatal("expected journal error")
	}
	if _, ok := err.(*JournalError); !ok {
		t.Fatalf("error = %T %v", err, err)
	}
}

func TestPendingFillsGapAndChecksHash(t *testing.T) {
	local := []Migration{
		{Name: "a", Path: "a/up.sql", Hash: "1"},
		{Name: "b", Path: "b/up.sql", Hash: "2"},
		{Name: "c", Path: "c/up.sql", Hash: "3"},
	}
	got, err := pending(local, []appliedRow{{Name: "c", Hash: "3"}, {Name: "a", Hash: "1"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "b" {
		t.Fatalf("pending = %#v", names(got))
	}
	_, err = pending(local, []appliedRow{{Name: "a", Hash: "changed"}})
	hashErr, ok := err.(*HashError)
	if !ok || hashErr.Name != "a" || hashErr.Path != "a/up.sql" {
		t.Fatalf("hash error = %v", err)
	}
}

func writeMigration(t *testing.T, root, name, body string) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "up.sql"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func names(ms []Migration) []string {
	out := make([]string, len(ms))
	for i, m := range ms {
		out[i] = m.Name
	}
	return out
}
