//go:build integration

package pg

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/go-mist/mist/diff"
	"github.com/go-mist/mist/internal/testdb"
	"github.com/go-mist/mist/introspect"
	"github.com/go-mist/mist/migrate"
	"github.com/go-mist/mist/pg"
)

func TestRevisionsApply(t *testing.T) {
	folder := t.TempDir()
	at := time.Date(2026, 11, 23, 5, 0, 0, 0, time.UTC)
	if _, err := WriteMigration(folder, "init", at, usersV1()); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteMigration(folder, "email", at.Add(time.Second), usersV2()); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteMigration(folder, "posts", at.Add(2*time.Second), usersV3()); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	db := openRevDB(t, ctx)
	quoted := pg.Dialect{}.EscapeName("m7rev")
	if _, err := db.Exec(ctx, "drop schema if exists "+quoted+" cascade"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, "create schema "+quoted); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(context.Background(), "drop schema if exists "+quoted+" cascade")
		_, _ = db.Exec(context.Background(), "drop schema if exists "+pg.Dialect{}.EscapeName("m7revmig")+" cascade")
	})
	applied, err := migrate.Run(ctx, pg.Migrator(db), migrate.Config{Folder: folder, Schema: "m7revmig"})
	if err != nil {
		t.Fatal(err)
	}
	if applied != 3 {
		t.Fatalf("applied %d", applied)
	}
	got, err := introspect.Read(ctx, db, "m7rev")
	if err != nil {
		t.Fatal(err)
	}
	if changes := diff.Diff(usersV3(), got); len(changes) != 0 {
		t.Fatalf("%+v", changes)
	}
	again, err := migrate.Run(ctx, pg.Migrator(db), migrate.Config{Folder: folder, Schema: "m7revmig"})
	if err != nil || again != 0 {
		t.Fatal(err, again)
	}
}

func openRevDB(t *testing.T, ctx context.Context) *pg.DB {
	t.Helper()
	dsn := testdb.PostgresDSN(t)
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.MaxConns = 2
	cfg.ConnConfig.RuntimeParams["search_path"] = "m7rev"
	db, err := pg.OpenConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}
