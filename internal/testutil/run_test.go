package testutil

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/xbmlz/mist/sql"
)

func TestRunComparesFixture(t *testing.T) {
	dir := t.TempDir()
	body := "{\n  \"sql\": \"select 1\",\n  \"params\": [1]\n}\n"
	if err := os.WriteFile(filepath.Join(dir, "one.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	Run(t, dir, []Case{{
		Name: "one",
		Build: func() (sql.Query, error) {
			return sql.Query{SQL: "select 1", Params: []any{1}}, nil
		},
	}})
}

func TestRunUpdateWritesFixture(t *testing.T) {
	old := *updateGolden
	*updateGolden = true
	t.Cleanup(func() { *updateGolden = old })

	dir := t.TempDir()
	Run(t, dir, []Case{{
		Name: "fresh",
		Build: func() (sql.Query, error) {
			return sql.Query{SQL: "select 1", Params: []any{"a"}}, nil
		},
	}})
	got, err := os.ReadFile(filepath.Join(dir, "fresh.json"))
	if err != nil {
		t.Fatal(err)
	}
	want := "{\n  \"sql\": \"select 1\",\n  \"params\": [\n    \"a\"\n  ]\n}\n"
	if string(got) != want {
		t.Fatalf("fixture:\n%s", got)
	}
}
