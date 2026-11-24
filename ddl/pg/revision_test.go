package pg

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-mist/mist/diff"
	"github.com/go-mist/mist/history"
	"github.com/go-mist/mist/migrate"
	"github.com/go-mist/mist/schema"
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
	firstSnap, err := os.ReadFile(filepath.Join(folder, first.Name, "snapshot.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(firstSnap), "email") || strings.Contains(string(firstSnap), second.Name) {
		t.Fatal("first snapshot was rewritten")
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

func TestCommutativeBranches(t *testing.T) {
	at := time.Date(2026, 11, 23, 5, 0, 0, 0, time.UTC)
	root := t.TempDir()
	init, err := WriteMigration(root, "init", at, usersV1())
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(root, init.Name, "snapshot.json"))
	if err != nil {
		t.Fatal(err)
	}
	left := t.TempDir()
	copyMigrations(t, root, left)
	avatar, err := WriteMigration(left, "avatar", at.Add(time.Second), usersAvatar())
	if err != nil {
		t.Fatal(err)
	}
	right := t.TempDir()
	copyMigrations(t, root, right)
	bio, err := WriteMigration(right, "bio", at.Add(2*time.Second), usersBio())
	if err != nil {
		t.Fatal(err)
	}
	both := t.TempDir()
	copyMigrations(t, left, both)
	copyMigrations(t, right, both)
	pending, err := Check(both, usersBoth())
	if err != nil || len(pending) != 0 {
		t.Fatal(err, pending)
	}
	noop, err := WriteMigration(both, "noop", at.Add(3*time.Second), usersBoth())
	if err != nil || noop.Name != "" {
		t.Fatal(err, noop.Name)
	}
	extra, err := WriteMigration(both, "extra", at.Add(4*time.Second), usersExtra())
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(both, extra.Name, "migration.sql"))
	if err != nil {
		t.Fatal(err)
	}
	sql := string(body)
	if !strings.Contains(sql, `"extra"`) || strings.Contains(sql, `"avatar"`) || strings.Contains(sql, `"bio"`) {
		t.Fatal(sql)
	}
	raw, err := os.ReadFile(filepath.Join(both, extra.Name, "snapshot.json"))
	if err != nil {
		t.Fatal(err)
	}
	var snap history.Snapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		t.Fatal(err)
	}
	if strings.Join(snap.PrevIDs, ",") != avatar.Name+","+bio.Name {
		t.Fatal(snap.PrevIDs)
	}
	after, err := os.ReadFile(filepath.Join(both, init.Name, "snapshot.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("init snapshot was rewritten")
	}
}

func TestBranchConflictStops(t *testing.T) {
	at := time.Date(2026, 11, 23, 6, 0, 0, 0, time.UTC)
	root := t.TempDir()
	if _, err := WriteMigration(root, "init", at, usersV2()); err != nil {
		t.Fatal(err)
	}
	left := t.TempDir()
	copyMigrations(t, root, left)
	required, err := WriteMigration(left, "required", at.Add(time.Second), usersEmailRequired())
	if err != nil {
		t.Fatal(err)
	}
	right := t.TempDir()
	copyMigrations(t, root, right)
	def, err := WriteMigration(right, "def", at.Add(2*time.Second), usersEmailDefault())
	if err != nil {
		t.Fatal(err)
	}
	both := t.TempDir()
	copyMigrations(t, left, both)
	copyMigrations(t, right, both)
	entries, err := os.ReadDir(both)
	if err != nil {
		t.Fatal(err)
	}
	_, err = Check(both, usersEmailRequired())
	if err == nil || !strings.Contains(err.Error(), required.Name) || !strings.Contains(err.Error(), def.Name) || !strings.Contains(err.Error(), "table users column email") {
		t.Fatal(err)
	}
	if _, err := WriteMigration(both, "again", at.Add(3*time.Second), usersEmailRequired()); err == nil || !strings.Contains(err.Error(), "column email") {
		t.Fatal(err)
	}
	if _, err := migrate.Run(context.Background(), nil, migrate.Config{Folder: both}); err == nil || !strings.Contains(err.Error(), "column email") {
		t.Fatal(err)
	}
	got, err := os.ReadDir(both)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(entries) {
		t.Fatalf("directories %d", len(got))
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

func usersAvatar() schema.Schema {
	return addColumn(usersV1(), schema.Column{Name: "avatar", Type: schema.Type{Name: "text"}})
}

func usersBio() schema.Schema {
	return addColumn(usersV1(), schema.Column{Name: "bio", Type: schema.Type{Name: "text"}})
}

func usersBoth() schema.Schema {
	return addColumn(usersAvatar(), schema.Column{Name: "bio", Type: schema.Type{Name: "text"}})
}

func usersExtra() schema.Schema {
	return addColumn(usersBoth(), schema.Column{Name: "extra", Type: schema.Type{Name: "text"}})
}

func usersEmailRequired() schema.Schema {
	s := usersV2()
	s.Tables[0].Columns[2].NotNull = true
	return s
}

func usersEmailDefault() schema.Schema {
	s := usersV2()
	s.Tables[0].Columns[2].Default = "'a@b.c'"
	return s
}

func addColumn(s schema.Schema, col schema.Column) schema.Schema {
	s.Tables[0].Columns = append(append([]schema.Column{}, s.Tables[0].Columns...), col)
	return s
}

func copyMigrations(t *testing.T, src, dst string) {
	t.Helper()
	entries, err := os.ReadDir(src)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		to := filepath.Join(dst, entry.Name())
		if err := os.MkdirAll(to, 0o755); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"migration.sql", "snapshot.json"} {
			body, err := os.ReadFile(filepath.Join(src, entry.Name(), name))
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(to, name), body, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
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
