package codegen_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/xbmlz/mist/codegen"
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
			want: "no Table() method",
		},
		{
			name: "go type mismatch",
			body: "type User struct { ID string `drizzle:\"id,bigserial,pk\"` }\nfunc (User) Table() pg.TableMeta { return pg.TableMeta{Name: \"users\"} }\n",
			want: "should be int64",
		},
		{
			name: "nullable must be pointer",
			body: "type User struct { Email string `drizzle:\"email,text\"` }\nfunc (User) Table() pg.TableMeta { return pg.TableMeta{Name: \"users\"} }\n",
			want: "should be *string",
		},
		{
			name: "pk must not be pointer",
			body: "type User struct { ID *int64 `drizzle:\"id,bigserial,pk\"` }\nfunc (User) Table() pg.TableMeta { return pg.TableMeta{Name: \"users\"} }\n",
			want: "not null",
		},
		{
			name: "dangling reference",
			body: "type Post struct { AuthorID int64 `drizzle:\"author_id,bigint,notnull,references=users.id\"` }\nfunc (Post) Table() pg.TableMeta { return pg.TableMeta{Name: \"posts\"} }\n",
			want: "was not found",
		},
		{
			name: "unknown sql type",
			body: "type User struct { Payload string `drizzle:\"payload,geometry,notnull\"` }\nfunc (User) Table() pg.TableMeta { return pg.TableMeta{Name: \"users\"} }\n",
			want: "geometry",
		},
		{
			name: "pk conflicts with PrimaryKey",
			body: "type User struct { ID int64 `drizzle:\"id,bigserial,pk\"` }\nfunc (User) Table() pg.TableMeta { return pg.TableMeta{Name: \"users\", PrimaryKey: []string{\"id\"}} }\n",
			want: "conflict",
		},
		{
			name: "length on text",
			body: "type User struct { Name string `drizzle:\"name,text,notnull,length=8\"` }\nfunc (User) Table() pg.TableMeta { return pg.TableMeta{Name: \"users\"} }\n",
			want: "length is only for varchar",
		},
		{
			name: "identity with default",
			body: "type User struct { ID int64 `drizzle:\"id,bigint,notnull,identity,default=1\"` }\nfunc (User) Table() pg.TableMeta { return pg.TableMeta{Name: \"users\"} }\n",
			want: "identity cannot be combined with default",
		},
		{
			name: "cross package embed",
			body: "type User struct { time.Time `drizzle:\"created_at,timestamptz,notnull\"` }\nfunc (User) Table() pg.TableMeta { return pg.TableMeta{Name: \"users\"} }\n",
			want: "embedded fields from another package",
		},
		{
			name: "generic struct",
			body: "type Box[T any] struct { ID int64 `drizzle:\"id,bigint,pk\"` }\nfunc (b Box[T]) Table() pg.TableMeta { return pg.TableMeta{Name: \"boxes\"} }\n",
			want: "generics are not supported",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			src := "package schema\n\nimport \"github.com/xbmlz/mist/pg\"\n\n" + tt.body
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

func TestGenerateConstraintsAndTypes(t *testing.T) {
	body := `package schema

import (
	"encoding/json"

	"github.com/xbmlz/mist/pg"
)

type Member struct {
	TenantID int64           ` + "`drizzle:\"tenant_id,bigint,notnull\"`" + `
	ID       int64           ` + "`drizzle:\"id,bigint,notnull\"`" + `
	Body     json.RawMessage ` + "`drizzle:\"body,jsonb,notnull\"`" + `
	Amount   *string         ` + "`drizzle:\"amount,numeric\"`" + `
}

func (Member) Table() pg.TableMeta {
	return pg.TableMeta{
		Name:       "members",
		PrimaryKey: []string{"tenant_id", "id"},
		Indexes: []pg.Index{
			{Name: "members_amount_idx", Columns: []string{"amount"}, Unique: true},
		},
		Checks: []pg.Check{
			{Name: "members_id_pos", Expr: "id > 0"},
		},
	}
}
`
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "schema.go"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := codegen.Generate(dir); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "schema_gen.go"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(got)
	for _, want := range []string{
		`func (memberTable) PrimaryKey() []string {`,
		`"tenant_id", "id"`,
		`func (memberTable) Indexes() []pg.Index {`,
		`Name: "members_amount_idx"`,
		`Unique: true`,
		`func (memberTable) Checks() []pg.Check {`,
		`Expr: "id > 0"`,
		`pg.Col[Member, json.RawMessage]`,
		`pg.Col[Member, *string]`,
		`codecs.JSONB.Bind("body")`,
		`codecs.Numeric.Bind("amount")`,
		`func (memberTable) ColumnSpecs() []pg.ColumnSpec {`,
		`Name: "amount", SQLType: "numeric"`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %s\n%s", want, text)
		}
	}
}

func TestGenerateColumnModifiers(t *testing.T) {
	body := "package schema\n\nimport \"github.com/xbmlz/mist/pg\"\n\n" +
		"type Item struct {\n" +
		"\tID int64 `drizzle:\"id,bigint,notnull,identity\"`\n" +
		"\tCode string `drizzle:\"code,varchar,notnull,length=8,unique=items_code_idx\"`\n" +
		"\tAmount string `drizzle:\"amount,numeric,notnull,precis=20.6\"`\n" +
		"\tNote *string `drizzle:\"note,text,generated=\\\"upper(code)\\\"\"`\n" +
		"}\n\n" +
		"func (Item) Table() pg.TableMeta { return pg.TableMeta{Name: \"items\"} }\n"
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "schema.go"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := codegen.Generate(dir); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "schema_gen.go"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(got)
	for _, want := range []string{
		`Name: "id", SQLType: "bigint", NotNull: true, Identity: true`,
		`Name: "code", SQLType: "varchar", NotNull: true, Unique: true, UniqueName: "items_code_idx", Length: 8`,
		`Name: "amount", SQLType: "numeric", NotNull: true, Precision: 20, Scale: 6`,
		`Name: "note", SQLType: "text", Generated: "upper(code)"`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %s\n%s", want, text)
		}
	}
}

func TestGenerateEmbedsSamePackage(t *testing.T) {
	body := "package schema\n\nimport \"time\"\n\nimport \"github.com/xbmlz/mist/pg\"\n\n" +
		"type timestamps struct {\n" +
		"\tCreatedAt time.Time `drizzle:\"created_at,timestamptz,notnull,defaultnow\"`\n" +
		"}\n\n" +
		"type User struct {\n" +
		"\tID int64 `drizzle:\"id,bigserial,pk\"`\n" +
		"\ttimestamps\n" +
		"}\n\n" +
		"func (User) Table() pg.TableMeta { return pg.TableMeta{Name: \"users\"} }\n"
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "schema.go"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := codegen.Generate(dir); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "schema_gen.go"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(got)
	for _, want := range []string{
		`return row.Scan(&dst.ID, &dst.CreatedAt)`,
		`Default: "now()"`,
		`var Users = userTable{`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %s\n%s", want, text)
		}
	}
	if strings.Contains(text, "var Timestamps") || strings.Contains(text, "timestampTable") {
		t.Fatalf("embedded helper was emitted as a table\n%s", text)
	}
}

func TestGenerateMySQL(t *testing.T) {
	body := `package schema

import (
	"encoding/json"
	"time"

	"github.com/xbmlz/mist/mysql"
)

type Note struct {
	ID      int64           ` + "`drizzle:\"id,bigint,pk\"`" + `
	Title   string          ` + "`drizzle:\"title,varchar,notnull,length=32\"`" + `
	Body    json.RawMessage ` + "`drizzle:\"body,json,notnull\"`" + `
	Amount  *string         ` + "`drizzle:\"amount,decimal\"`" + `
	At      time.Time       ` + "`drizzle:\"at,datetime,notnull\"`" + `
	Payload []byte          ` + "`drizzle:\"payload,blob,notnull\"`" + `
	Flag    int8            ` + "`drizzle:\"flag,tinyint,notnull\"`" + `
}

func (Note) Table() mysql.TableMeta {
	return mysql.TableMeta{Name: "notes"}
}
`
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "schema.go"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := codegen.GenerateSource(dir)
	if err != nil {
		t.Fatal(err)
	}
	text := string(got)
	for _, want := range []string{
		"github.com/xbmlz/mist/mysql\"",
		"github.com/xbmlz/mist/mysql/codecs\"",
		"mysql.Col[Note, json.RawMessage]",
		`codecs.JSON.Bind("body")`,
		`codecs.Decimal.Bind("amount")`,
		`codecs.DateTime.Bind("at")`,
		`codecs.Blob.Bind("payload")`,
		"mysql.ColumnSpec",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %s\n%s", want, text)
		}
	}
	if strings.Contains(text, "mist/pg") {
		t.Fatalf("generated pg import\n%s", text)
	}
}

func TestGenerateMySQLRejects(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "timestamptz",
			body: "type Note struct { At time.Time `drizzle:\"at,timestamptz,notnull\"` }\nfunc (Note) Table() mysql.TableMeta { return mysql.TableMeta{Name: \"notes\"} }\n",
			want: "has no timestamptz",
		},
		{
			name: "boolean",
			body: "type Note struct { OK bool `drizzle:\"ok,boolean,notnull\"` }\nfunc (Note) Table() mysql.TableMeta { return mysql.TableMeta{Name: \"notes\"} }\n",
			want: "has no boolean",
		},
		{
			name: "array",
			body: "type Note struct { Tags []string `drizzle:\"tags,text[],notnull\"` }\nfunc (Note) Table() mysql.TableMeta { return mysql.TableMeta{Name: \"notes\"} }\n",
			want: "has no arrays",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			src := "package schema\n\nimport (\n\t\"time\"\n\n\t\"github.com/xbmlz/mist/mysql\"\n)\n\n" + tt.body
			if err := os.WriteFile(filepath.Join(dir, "schema.go"), []byte(src), 0o644); err != nil {
				t.Fatal(err)
			}
			err := codegen.Generate(dir)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Generate() error = %v, want substring %q", err, tt.want)
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
