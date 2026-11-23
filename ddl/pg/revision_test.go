package pg

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xbmlz/mist/diff"
	"github.com/xbmlz/mist/migrate"
	"github.com/xbmlz/mist/schema"
)

func TestThreeRevisions(t *testing.T) {
	folder := t.TempDir()
	at := time.Date(2026, 11, 23, 2, 0, 0, 0, time.UTC)
	first, err := WriteMigration(folder, "init", at, usersV1())
	if err != nil {
		t.Fatal(err)
	}
	if first.Name != "20261123020000_init" {
		t.Fatal(first.Name)
	}
	second, err := WriteMigration(folder, "email", at.Add(time.Second), usersV2())
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(folder, second.Name, "migration.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), `CREATE TABLE "users"`) || !strings.Contains(string(body), `ADD COLUMN "email"`) {
		t.Fatal(string(body))
	}
	third, err := WriteMigration(folder, "posts", at.Add(2*time.Second), usersV3())
	if err != nil {
		t.Fatal(err)
	}
	posts, err := os.ReadFile(filepath.Join(folder, third.Name, "migration.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(posts), `CREATE TABLE "posts"`) || !strings.Contains(string(posts), "FOREIGN KEY") {
		t.Fatal(string(posts))
	}
	again, err := WriteMigration(folder, "posts", at.Add(3*time.Second), usersV3())
	if err != nil {
		t.Fatal(err)
	}
	if again.Name != "" {
		t.Fatal(again.Name)
	}
	migs, err := migrate.Read(folder)
	if err != nil {
		t.Fatal(err)
	}
	if len(migs) != 3 {
		t.Fatalf("migrations %d", len(migs))
	}
	pending, err := Check(folder, usersV2())
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) == 0 || pending[0].Kind == diff.CreateTable {
		t.Fatalf("%+v", pending)
	}
	same, err := Check(folder, usersV3())
	if err != nil || len(same) != 0 {
		t.Fatal(err, same)
	}
}

func TestEditedMigrationFailsCheck(t *testing.T) {
	folder := t.TempDir()
	at := time.Date(2026, 11, 23, 3, 0, 0, 0, time.UTC)
	written, err := WriteMigration(folder, "init", at, usersV1())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(folder, written.Name, "migration.sql")
	if err := os.WriteFile(path, []byte("select 1;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Check(folder, usersV1()); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatal(err)
	}
}

func TestMigrationsWithoutSnapshot(t *testing.T) {
	folder := t.TempDir()
	dir := filepath.Join(folder, "20261123040000_hand")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "migration.sql"), []byte("select 1;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Check(folder, usersV1()); err == nil || !strings.Contains(err.Error(), "snapshot.json") {
		t.Fatal(err)
	}
}

func usersV1() schema.Schema {
	return schema.Schema{Tables: []schema.Table{{
		Name:       "users",
		PrimaryKey: []string{"id"},
		Columns: []schema.Column{
			{Name: "id", Type: schema.Type{Name: "bigserial"}, NotNull: true},
			{Name: "name", Type: schema.Type{Name: "text"}, NotNull: true},
		},
	}}}
}

func usersV2() schema.Schema {
	s := usersV1()
	s.Tables[0].Columns = append(s.Tables[0].Columns, schema.Column{Name: "email", Type: schema.Type{Name: "text"}})
	s.Tables[0].Uniques = []schema.Unique{{Name: "users_email_key", Columns: []string{"email"}}}
	return s
}

func usersV3() schema.Schema {
	s := usersV2()
	s.Tables = append(s.Tables, schema.Table{
		Name:       "posts",
		PrimaryKey: []string{"id"},
		Columns: []schema.Column{
			{Name: "id", Type: schema.Type{Name: "bigserial"}, NotNull: true},
			{Name: "author_id", Type: schema.Type{Name: "bigint"}, NotNull: true},
			{Name: "title", Type: schema.Type{Name: "text"}, NotNull: true},
		},
		ForeignKeys: []schema.ForeignKey{{
			Name: "posts_author_id_fkey", Columns: []string{"author_id"},
			RefTable: "users", RefColumns: []string{"id"}, OnDelete: "cascade",
		}},
	})
	return s
}
