//go:build integration

package oracle_test

import (
	"context"
	"os"
	"testing"

	"github.com/go-mist/mist"
	"github.com/go-mist/mist/ddl"
	oracleddl "github.com/go-mist/mist/ddl/oracle"
	"github.com/go-mist/mist/dialect/oracle"
	"github.com/go-mist/mist/diff"
	"github.com/go-mist/mist/introspect"
	"github.com/go-mist/mist/migrate"
	"github.com/go-mist/mist/op"
	"github.com/go-mist/mist/relation"
	"github.com/go-mist/mist/schema"
	"github.com/go-mist/mist/sql"
)

func TestOracleStatements(t *testing.T) {
	ctx := context.Background()
	db := openOracle(t, ctx)
	for _, stmt := range []string{
		`BEGIN EXECUTE IMMEDIATE 'DROP TABLE "mist_ora_posts"'; EXCEPTION WHEN OTHERS THEN IF SQLCODE != -942 THEN RAISE; END IF; END;`,
		`BEGIN EXECUTE IMMEDIATE 'DROP TABLE "mist_ora_users"'; EXCEPTION WHEN OTHERS THEN IF SQLCODE != -942 THEN RAISE; END IF; END;`,
		`CREATE TABLE "mist_ora_users" ("id" NUMBER(19) PRIMARY KEY, "name" VARCHAR2(40))`,
		`CREATE TABLE "mist_ora_posts" ("id" NUMBER(19) PRIMARY KEY, "author_id" NUMBER(19), "title" VARCHAR2(40))`,
		`INSERT INTO "mist_ora_users" ("id", "name") VALUES (1, 'alice')`,
		`INSERT INTO "mist_ora_users" ("id", "name") VALUES (2, 'bob')`,
		`INSERT INTO "mist_ora_posts" ("id", "author_id", "title") VALUES (10, 1, 'a')`,
		`INSERT INTO "mist_ora_posts" ("id", "author_id", "title") VALUES (11, 1, 'b')`,
	} {
		if _, err := db.Exec(ctx, stmt); err != nil {
			t.Fatal(err)
		}
	}
	users := oracleUsers()
	posts := oraclePosts()

	joined, err := oracle.Select(users.ID).From(users).
		InnerJoin(posts, op.Eq(posts.AuthorID, users.ID)).
		Where(op.Eq(users.ID, int64(1))).
		GroupBy(users.ID).
		Having(op.Gt(users.ID, int64(0))).
		OrderBy(op.Asc(users.ID)).
		All(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if len(joined) != 1 || joined[0].ID != 1 {
		t.Fatalf("join = %#v", joined)
	}

	names, err := oracle.Select(users.Name).From(users).Distinct().OrderBy(op.Asc(users.Name)).All(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 2 {
		t.Fatalf("distinct = %#v", names)
	}

	both, err := oracle.Select(users.ID).From(users).
		Union(oracle.Select(users.ID).From(users)).
		OrderBy(op.Asc(users.ID)).
		Limit(1).
		All(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if len(both) != 1 || both[0].ID != 1 {
		t.Fatalf("union = %#v", both)
	}

	left, err := oracle.Select(users.ID).From(users).
		Except(oracle.Select(users.ID).From(users).Where(op.Eq(users.ID, int64(2)))).
		OrderBy(op.Asc(users.ID)).
		All(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 1 || left[0].ID != 1 {
		t.Fatalf("except = %#v", left)
	}

	cte := oracle.CTE("chosen", oracle.Select(users.ID, users.Name).From(users).Where(op.Eq(users.ID, int64(2))))
	found, err := oracle.Select(cte.Col(users.Name)).From(cte).With(cte).All(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 1 || found[0].Name != "bob" {
		t.Fatalf("with = %#v", found)
	}

	locked, err := oracle.Select(users.ID).From(users).Where(op.Eq(users.ID, int64(1))).ForUpdate().One(ctx, db)
	if err != nil || locked.ID != 1 {
		t.Fatalf("%v %+v", err, locked)
	}

	if _, err := oracle.Insert(users).Columns(users.ID, users.Name).
		Select(oracle.Select(posts.ID, posts.Title).From(posts).Where(op.Eq(posts.ID, int64(10)))).
		Exec(ctx, db); err != nil {
		t.Fatal(err)
	}
	copied, err := oracle.Select(users.Name).From(users).Where(op.Eq(users.ID, int64(10))).One(ctx, db)
	if err != nil || copied.Name != "a" {
		t.Fatalf("%v %+v", err, copied)
	}
}

func TestOracleMigrate(t *testing.T) {
	ctx := context.Background()
	db := openOracle(t, ctx)
	dir := t.TempDir()
	body := "CREATE TABLE \"mist_ora_mig\" (\"id\" NUMBER(19) PRIMARY KEY)\n"
	if err := os.MkdirAll(dir+"/20261124120000_init", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir+"/20261124120000_init/migration.sql", []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		`BEGIN EXECUTE IMMEDIATE 'DROP TABLE "mist_ora_mig"'; EXCEPTION WHEN OTHERS THEN IF SQLCODE != -942 THEN RAISE; END IF; END;`,
		`BEGIN EXECUTE IMMEDIATE 'DROP TABLE "mist_ora_mig_log"'; EXCEPTION WHEN OTHERS THEN IF SQLCODE != -942 THEN RAISE; END IF; END;`,
	} {
		if _, err := db.Exec(ctx, stmt); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		_, _ = db.Exec(context.Background(), `DROP TABLE "mist_ora_mig"`)
		_, _ = db.Exec(context.Background(), `DROP TABLE "mist_ora_mig_log"`)
	})
	cfg := migrate.Config{Folder: dir, Table: "mist_ora_mig_log"}
	n, err := migrate.Run(ctx, oracle.Migrator(db), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("applied = %d", n)
	}
	again, err := migrate.Run(ctx, oracle.Migrator(db), cfg)
	if err != nil || again != 0 {
		t.Fatalf("second = %d %v", again, err)
	}
}

func TestOracleRelational(t *testing.T) {
	ctx := context.Background()
	db := openOracle(t, ctx)
	for _, stmt := range []string{
		`BEGIN EXECUTE IMMEDIATE 'DROP TABLE "ora_rqb_posts"'; EXCEPTION WHEN OTHERS THEN IF SQLCODE != -942 THEN RAISE; END IF; END;`,
		`BEGIN EXECUTE IMMEDIATE 'DROP TABLE "ora_rqb_users"'; EXCEPTION WHEN OTHERS THEN IF SQLCODE != -942 THEN RAISE; END IF; END;`,
		`CREATE TABLE "ora_rqb_users" ("id" NUMBER(19) PRIMARY KEY, "name" VARCHAR2(40) NOT NULL)`,
		`CREATE TABLE "ora_rqb_posts" ("id" NUMBER(19) PRIMARY KEY, "title" VARCHAR2(40) NOT NULL, "author_id" NUMBER(19) NOT NULL)`,
		`INSERT INTO "ora_rqb_users" ("id", "name") VALUES (1, 'alice')`,
		`INSERT INTO "ora_rqb_users" ("id", "name") VALUES (2, 'bob')`,
		`INSERT INTO "ora_rqb_posts" ("id", "title", "author_id") VALUES (10, 'a', 1)`,
		`INSERT INTO "ora_rqb_posts" ("id", "title", "author_id") VALUES (11, 'b', 1)`,
	} {
		if _, err := db.Exec(ctx, stmt); err != nil {
			t.Fatal(err)
		}
	}
	built, err := relation.Define([]relation.Table{
		{
			Name: "users", SQLName: "ora_rqb_users",
			New: func() any { return &rqbUser{} },
			Columns: []relation.Column{
				{Name: "id", Ptr: func(row any) any { return &row.(*rqbUser).ID }},
				{Name: "name", Ptr: func(row any) any { return &row.(*rqbUser).Name }},
			},
			Relations: []relation.Relation{func() relation.Relation {
				r := relation.Many("posts", "posts", "id", "author_id")
				r.Init = func(parent any) { parent.(*rqbUser).Posts = []rqbPost{} }
				r.Place = func(parent, child any) {
					parent.(*rqbUser).Posts = append(parent.(*rqbUser).Posts, *child.(*rqbPost))
				}
				return r
			}()},
		},
		{
			Name: "posts", SQLName: "ora_rqb_posts",
			New: func() any { return &rqbPost{} },
			Columns: []relation.Column{
				{Name: "id", Ptr: func(row any) any { return &row.(*rqbPost).ID }},
				{Name: "title", Ptr: func(row any) any { return &row.(*rqbPost).Title }},
				{Name: "author_id", Ptr: func(row any) any { return &row.(*rqbPost).AuthorID }},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := oracle.FindMany[rqbUser](ctx, db, built, "users", relation.Config{
		Order: relation.Asc("id"),
		With: []relation.With{{
			Name:  "posts",
			Query: &relation.Config{Order: relation.Desc("id")},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Name != "alice" || len(got[0].Posts) != 2 || got[0].Posts[0].ID != 11 {
		t.Fatalf("%#v", got)
	}
}

func TestOraclePushRoundTrip(t *testing.T) {
	ctx := context.Background()
	db := openOracle(t, ctx)
	want := schema.Schema{Tables: []schema.Table{{
		Name:       "mist_ora_round",
		PrimaryKey: []string{"id"},
		Columns: []schema.Column{
			{Name: "id", Type: schema.Type{Name: "bigint"}, NotNull: true, Identity: true},
			{Name: "title", Type: schema.Type{Name: "text"}, NotNull: true},
		},
	}}}
	sqlText, err := oracleddl.Render(diff.Diff(want, schema.Schema{}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(context.Background(), `DROP TABLE "mist_ora_round"`)
	})
	if _, err := db.Exec(ctx, `BEGIN EXECUTE IMMEDIATE 'DROP TABLE "mist_ora_round"'; EXCEPTION WHEN OTHERS THEN IF SQLCODE != -942 THEN RAISE; END IF; END;`); err != nil {
		t.Fatal(err)
	}
	if err := ddl.Execute(ctx, db, sqlText); err != nil {
		t.Fatal(err)
	}
	actual, err := introspect.ReadOracle(ctx, db, "")
	if err != nil {
		t.Fatal(err)
	}
	var got schema.Schema
	for _, table := range actual.Tables {
		if table.Name == "mist_ora_round" {
			got.Tables = append(got.Tables, table)
		}
	}
	changes := diff.Diff(want, got)
	if len(changes) != 0 {
		for _, change := range changes {
			t.Log(change.Summary)
		}
		t.Fatalf("%d changes", len(changes))
	}
}

func openOracle(t *testing.T, ctx context.Context) *oracle.DB {
	t.Helper()
	dsn := os.Getenv("ORACLE_DSN")
	if dsn == "" {
		t.Skip("ORACLE_DSN is not set")
	}
	db, err := oracle.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

type oraUser struct {
	ID   int64
	Name string
}

type oraUserTable struct {
	mist.TableBase
	ID   mist.Col[oraUser, int64]
	Name mist.Col[oraUser, string]
}

func (oraUserTable) Row() *oraUser { return nil }
func (t oraUserTable) Columns() []sql.Chunk {
	return []sql.Chunk{t.ID, t.Name}
}
func (t oraUserTable) Scan(row mist.Row, dst *oraUser) error {
	return row.Scan(&dst.ID, &dst.Name)
}

func oracleUsers() oraUserTable {
	return oraUserTable{
		TableBase: mist.TableBase{SQLName: "mist_ora_users"},
		ID:        mist.Col[oraUser, int64]{Table: "mist_ora_users", Name: "id", Ptr: func(v *oraUser) any { return &v.ID }},
		Name:      mist.Col[oraUser, string]{Table: "mist_ora_users", Name: "name", Ptr: func(v *oraUser) any { return &v.Name }},
	}
}

type oraPost struct {
	ID       int64
	AuthorID int64
	Title    string
}

type oraPostTable struct {
	mist.TableBase
	ID       mist.Col[oraPost, int64]
	AuthorID mist.Col[oraPost, int64]
	Title    mist.Col[oraPost, string]
}

func (oraPostTable) Row() *oraPost          { return nil }
func (t oraPostTable) Columns() []sql.Chunk { return []sql.Chunk{t.ID, t.AuthorID, t.Title} }
func (t oraPostTable) Scan(row mist.Row, dst *oraPost) error {
	return row.Scan(&dst.ID, &dst.AuthorID, &dst.Title)
}

func oraclePosts() oraPostTable {
	return oraPostTable{
		TableBase: mist.TableBase{SQLName: "mist_ora_posts"},
		ID:        mist.Col[oraPost, int64]{Table: "mist_ora_posts", Name: "id", Ptr: func(v *oraPost) any { return &v.ID }},
		AuthorID:  mist.Col[oraPost, int64]{Table: "mist_ora_posts", Name: "author_id", Ptr: func(v *oraPost) any { return &v.AuthorID }},
		Title:     mist.Col[oraPost, string]{Table: "mist_ora_posts", Name: "title", Ptr: func(v *oraPost) any { return &v.Title }},
	}
}

type rqbUser struct {
	ID    int64
	Name  string
	Posts []rqbPost
}

type rqbPost struct {
	ID       int64
	Title    string
	AuthorID int64
}
