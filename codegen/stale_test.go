package codegen_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/go-mist/mist/codegen"
)

func TestStaleGenerateDoesNotCompile(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Join(filepath.Dir(file), "..")
	dir := t.TempDir()
	mod := "module example.com/stale\n\ngo 1.26.4\n\nrequire github.com/go-mist/mist v0.0.0\n\nreplace github.com/go-mist/mist => " + root + "\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(mod), 0o644); err != nil {
		t.Fatal(err)
	}
	schema := "package stale\n\nimport \"github.com/go-mist/mist/pg\"\n\n" +
		"type User struct {\n" +
		"\tID int64 `mist:\"id,bigserial,pk\"`\n" +
		"\tEmail *string `mist:\"email,text\"`\n" +
		"}\n\n" +
		"func (User) Table() pg.TableMeta { return pg.TableMeta{Name: \"users\"} }\n"
	if err := os.WriteFile(filepath.Join(dir, "schema.go"), []byte(schema), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := codegen.Generate(dir); err != nil {
		t.Fatal(err)
	}
	without := "package stale\n\nimport \"github.com/go-mist/mist/pg\"\n\n" +
		"type User struct {\n" +
		"\tID int64 `mist:\"id,bigserial,pk\"`\n" +
		"}\n\n" +
		"func (User) Table() pg.TableMeta { return pg.TableMeta{Name: \"users\"} }\n"
	if err := os.WriteFile(filepath.Join(dir, "schema.go"), []byte(without), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "schema_test.go"), []byte("package stale\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "test", ".")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("stale schema_gen.go compiled\n%s", out)
	}
}
