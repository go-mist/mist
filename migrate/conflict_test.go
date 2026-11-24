package migrate

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHandWrittenSQLSkipsSnapshotMerge(t *testing.T) {
	dir := t.TempDir()
	folder := filepath.Join(dir, "20240101000000_hand")
	if err := os.Mkdir(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folder, "up.sql"), []byte("select 1;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Run(context.Background(), nil, Config{Folder: dir})
	if err == nil || !strings.Contains(err.Error(), "missing connection") {
		t.Fatal(err)
	}
}
