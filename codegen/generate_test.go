package codegen_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/xbmlz/drizzle-go/codegen"
)

func TestGenerateMatchesFixture(t *testing.T) {
	dir := fixtureDir(t)
	got, err := codegen.GenerateSource(dir)
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(filepath.Join(dir, "schema_gen.go"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("schema_gen.go is stale\n--- got ---\n%s", got)
	}
}

func TestGenerateIdempotent(t *testing.T) {
	src := filepath.Join(fixtureDir(t), "schema.go")
	body, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "schema.go"), body, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := codegen.Generate(dir); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(filepath.Join(dir, "schema_gen.go"))
	if err != nil {
		t.Fatal(err)
	}
	if err := codegen.Generate(dir); err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(filepath.Join(dir, "schema_gen.go"))
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatal("second generate changed schema_gen.go")
	}
}

func TestGenerateRejects(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "missing Table",
			body: "type User struct { ID int64 `drizzle:\"id,bigserial,pk\"` }\n",
			want: "没有 Table()",
		},
		{
			name: "go type mismatch",
			body: "type User struct { ID string `drizzle:\"id,bigserial,pk\"` }\nfunc (User) Table() pg.TableMeta { return pg.TableMeta{Name: \"users\"} }\n",
			want: "应是 int64",
		},
		{
			name: "nullable must be pointer",
			body: "type User struct { Email string `drizzle:\"email,text\"` }\nfunc (User) Table() pg.TableMeta { return pg.TableMeta{Name: \"users\"} }\n",
			want: "应是 *string",
		},
		{
			name: "pk must not be pointer",
			body: "type User struct { ID *int64 `drizzle:\"id,bigserial,pk\"` }\nfunc (User) Table() pg.TableMeta { return pg.TableMeta{Name: \"users\"} }\n",
			want: "非空",
		},
		{
			name: "dangling reference",
			body: "type Post struct { AuthorID int64 `drizzle:\"author_id,bigint,notnull,references=users.id\"` }\nfunc (Post) Table() pg.TableMeta { return pg.TableMeta{Name: \"posts\"} }\n",
			want: "找不到表",
		},
		{
			name: "unknown sql type",
			body: "type User struct { Payload string `drizzle:\"payload,jsonb,notnull\"` }\nfunc (User) Table() pg.TableMeta { return pg.TableMeta{Name: \"users\"} }\n",
			want: "jsonb",
		},
		{
			name: "compound key stays in Table",
			body: "type User struct { ID int64 `drizzle:\"id,bigserial,pk\"` }\nfunc (User) Table() pg.TableMeta { return pg.TableMeta{Name: \"users\", PrimaryKey: []string{\"id\"}} }\n",
			want: "只读取 Name",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			src := "package schema\n\nimport \"github.com/xbmlz/drizzle-go/pg\"\n\n" + tt.body
			if err := os.WriteFile(filepath.Join(dir, "schema.go"), []byte(src), 0o644); err != nil {
				t.Fatal(err)
			}
			err := codegen.Generate(dir)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Generate() error = %v, want substring %q", err, tt.want)
			}
			if _, statErr := os.Stat(filepath.Join(dir, "schema_gen.go")); !os.IsNotExist(statErr) {
				t.Fatal("schema_gen.go was written despite errors")
			}
		})
	}
}

func fixtureDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Join(filepath.Dir(file), "..", "internal", "schemafixture")
}
