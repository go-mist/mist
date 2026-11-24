//go:build integration

package mssql_test

import (
	"context"
	"os"
	"testing"

	"github.com/go-mist/mist"
	"github.com/go-mist/mist/ddl"
	mssqlddl "github.com/go-mist/mist/ddl/mssql"
	"github.com/go-mist/mist/dialect/mssql"
	"github.com/go-mist/mist/diff"
	"github.com/go-mist/mist/introspect"
	"github.com/go-mist/mist/migrate"
	"github.com/go-mist/mist/op"
	"github.com/go-mist/mist/relation"
	"github.com/go-mist/mist/schema"
	"github.com/go-mist/mist/sql"
)

func TestMSSQLStatements(t *testing.T) {
	ctx := context.Background()
	db := openMSSQL(t, ctx)
	for _, stmt := range []string{
		`DROP TABLE IF EXISTS [mist_ms_posts]`,
		`DROP TABLE IF EXISTS [mist_ms_users]`,
		`CREATE TABLE [mist_ms_users] ([id] bigint NOT NULL PRIMARY KEY, [name] nvarchar(40) NOT NULL)`,
		`CREATE TABLE [mist_ms_posts] ([id] bigint NOT NULL PRIMARY KEY, [author_id] bigint NOT NULL, [title] nvarchar(40) NOT NULL)`,
	} {
		if _, err := db.Exec(ctx, stmt); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		_, _ = db.Exec(context.Background(), `DROP TABLE IF EXISTS [mist_ms_posts]`)
		_, _ = db.Exec(context.Background(), `DROP TABLE IF EXISTS [mist_ms_users]`)
	})
	users := mssqlUsers()
	posts := mssqlPosts()
	if _, err := mssql.Insert(users).Columns(users.ID, users.Name).Values(int64(1), "alice").Values(int64(2), "bob").Exec(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, err := mssql.Insert(posts).Columns(posts.ID, posts.AuthorID, posts.Title).Values(int64(10), int64(1), "a").Values(int64(11), int64(1), "b").Exec(ctx, db); err != nil {
		t.Fatal(err)
	}

	joined, err := mssql.Select(users.ID).From(users).
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

	names, err := mssql.Select(users.Name).From(users).Distinct().OrderBy(op.Asc(users.Name)).All(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 2 {
		t.Fatalf("distinct = %#v", names)
	}

	matched, err := mssql.Select(users.ID).From(users).Where(op.ILike(users.Name, "AL%")).OrderBy(op.Asc(users.ID)).All(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if len(matched) != 1 || matched[0].ID != 1 {
		t.Fatalf("ilike = %#v", matched)
	}

	both, err := mssql.Select(users.ID).From(users).
		Union(mssql.Select(users.ID).From(users)).
		OrderBy(op.Asc(users.ID)).
		Limit(1).
		All(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if len(both) != 1 || both[0].ID != 1 {
		t.Fatalf("union = %#v", both)
	}

	second, err := mssql.Select(users.ID).From(users).OrderBy(op.Asc(users.ID)).Offset(1).Limit(1).One(ctx, db)
	if err != nil || second.ID != 2 {
		t.Fatalf("%v %+v", err, second)
	}

	top, err := mssql.Select(users.Name).From(users).Top(1).OrderBy(op.Asc(users.ID)).All(ctx, db)
	if err != nil || len(top) != 1 || top[0].Name != "alice" {
		t.Fatalf("top %v %#v", err, top)
	}
	inserted, err := mssql.Insert(users).Columns(users.ID, users.Name).Values(int64(3), "cara").Output(users.ID, users.Name).All(ctx, db)
	if err != nil || len(inserted) != 1 || inserted[0].ID != 3 || inserted[0].Name != "cara" {
		t.Fatalf("output insert %v %#v", err, inserted)
	}
	changed, err := mssql.Update(users).Set(users.Name, "cara2").Where(op.Eq(users.ID, int64(3))).OutputInserted(users.Name).One(ctx, db)
	if err != nil || changed.Name != "cara2" {
		t.Fatalf("output update %v %+v", err, changed)
	}
	removed, err := mssql.Delete(users).Where(op.Eq(users.ID, int64(3))).Output(users.ID).One(ctx, db)
	if err != nil || removed.ID != 3 {
		t.Fatalf("output delete %v %+v", err, removed)
	}

	if _, err := mssql.Insert(users).Columns(users.ID, users.Name).
		Select(mssql.Select(posts.ID, posts.Title).From(posts).Where(op.Eq(posts.ID, int64(10)))).
		Exec(ctx, db); err != nil {
		t.Fatal(err)
	}
	copied, err := mssql.Select(users.Name).From(users).Where(op.Eq(users.ID, int64(10))).One(ctx, db)
	if err != nil || copied.Name != "a" {
		t.Fatalf("%v %+v", err, copied)
	}

	err = db.Tx(ctx, func(tx mist.Tx) error {
		_, err := mssql.Update(users).Set(users.Name, "amy").Where(op.Eq(users.ID, int64(1))).Exec(ctx, tx)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mssql.Delete(users).Where(op.Eq(users.ID, int64(2))).Exec(ctx, db); err != nil {
		t.Fatal(err)
	}
	left, err := mssql.Select(users.Name).From(users).Where(op.Eq(users.ID, int64(1))).One(ctx, db)
	if err != nil || left.Name != "amy" {
		t.Fatalf("%v %+v", err, left)
	}
}

func TestMSSQLMigrate(t *testing.T) {
	ctx := context.Background()
	db := openMSSQL(t, ctx)
	dir := t.TempDir()
	body := "CREATE TABLE [mist_ms_mig] ([id] bigint NOT NULL PRIMARY KEY)\n"
	if err := os.MkdirAll(dir+"/20261124130000_init", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir+"/20261124130000_init/migration.sql", []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		`DROP TABLE IF EXISTS [mist_ms_mig]`,
		`DROP TABLE IF EXISTS [dbo].[mist_ms_mig_log]`,
	} {
		if _, err := db.Exec(ctx, stmt); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		_, _ = db.Exec(context.Background(), `DROP TABLE IF EXISTS [mist_ms_mig]`)
		_, _ = db.Exec(context.Background(), `DROP TABLE IF EXISTS [dbo].[mist_ms_mig_log]`)
	})
	cfg := migrate.Config{Folder: dir, Table: "mist_ms_mig_log"}
	n, err := migrate.Run(ctx, mssql.Migrator(db), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("applied = %d", n)
	}
	again, err := migrate.Run(ctx, mssql.Migrator(db), cfg)
	if err != nil || again != 0 {
		t.Fatalf("second = %d %v", again, err)
	}
}

func TestMSSQLRelational(t *testing.T) {
	ctx := context.Background()
	db := openMSSQL(t, ctx)
	for _, stmt := range []string{
		`DROP TABLE IF EXISTS [ms_rqb_posts]`,
		`DROP TABLE IF EXISTS [ms_rqb_users]`,
		`CREATE TABLE [ms_rqb_users] ([id] bigint NOT NULL PRIMARY KEY, [name] nvarchar(40) NOT NULL)`,
		`CREATE TABLE [ms_rqb_posts] ([id] bigint NOT NULL PRIMARY KEY, [title] nvarchar(40) NOT NULL, [author_id] bigint NOT NULL)`,
		`INSERT INTO [ms_rqb_users] ([id], [name]) VALUES (1, N'alice'), (2, N'bob')`,
		`INSERT INTO [ms_rqb_posts] ([id], [title], [author_id]) VALUES (10, N'a', 1), (11, N'b', 1)`,
	} {
		if _, err := db.Exec(ctx, stmt); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		_, _ = db.Exec(context.Background(), `DROP TABLE IF EXISTS [ms_rqb_posts]`)
		_, _ = db.Exec(context.Background(), `DROP TABLE IF EXISTS [ms_rqb_users]`)
	})
	built, err := relation.Define([]relation.Table{
		{
			Name: "users", SQLName: "ms_rqb_users",
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
			Name: "posts", SQLName: "ms_rqb_posts",
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
	got, err := mssql.FindMany[rqbUser](ctx, db, built, "users", relation.Config{
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

func TestMSSQLPushRoundTrip(t *testing.T) {
	ctx := context.Background()
	db := openMSSQL(t, ctx)
	want := schema.Schema{Tables: []schema.Table{{
		Name:       "mist_ms_round",
		PrimaryKey: []string{"id"},
		Columns: []schema.Column{
			{Name: "id", Type: schema.Type{Name: "bigint"}, NotNull: true, Identity: true},
			{Name: "title", Type: schema.Type{Name: "text"}, NotNull: true},
			{Name: "code", Type: schema.Type{Name: "varchar", Length: 40}, NotNull: true},
			{Name: "body", Type: schema.Type{Name: "json"}, NotNull: true},
		},
	}}}
	sqlText, err := mssqlddl.Render(diff.Diff(want, schema.Schema{}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `DROP TABLE IF EXISTS [mist_ms_round]`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(context.Background(), `DROP TABLE IF EXISTS [mist_ms_round]`)
	})
	if err := ddl.Execute(ctx, db, sqlText); err != nil {
		t.Fatal(err)
	}
	actual, err := introspect.ReadMSSQL(ctx, db, "dbo")
	if err != nil {
		t.Fatal(err)
	}
	var got schema.Schema
	for _, table := range actual.Tables {
		if table.Name == "mist_ms_round" {
			got.Tables = append(got.Tables, table)
		}
	}
	if changes := diff.Diff(want, got); len(changes) != 0 {
		for _, change := range changes {
			t.Log(change.Kind, change.Summary)
		}
		t.Fatalf("%d changes\n%#v", len(changes), got)
	}
}

func openMSSQL(t *testing.T, ctx context.Context) *mssql.DB {
	t.Helper()
	dsn := os.Getenv("MSSQL_DSN")
	if dsn == "" {
		t.Skip("MSSQL_DSN is not set")
	}
	db, err := mssql.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

type msUser struct {
	ID   int64
	Name string
}

type msUserTable struct {
	mist.TableBase
	ID   mist.Col[msUser, int64]
	Name mist.Col[msUser, string]
}

func (msUserTable) Row() *msUser { return nil }
func (t msUserTable) Columns() []sql.Chunk {
	return []sql.Chunk{t.ID, t.Name}
}
func (t msUserTable) Scan(row mist.Row, dst *msUser) error {
	return row.Scan(&dst.ID, &dst.Name)
}

func mssqlUsers() msUserTable {
	return msUserTable{
		TableBase: mist.TableBase{SQLName: "mist_ms_users"},
		ID:        mist.Col[msUser, int64]{Table: "mist_ms_users", Name: "id", Ptr: func(v *msUser) any { return &v.ID }},
		Name:      mist.Col[msUser, string]{Table: "mist_ms_users", Name: "name", Ptr: func(v *msUser) any { return &v.Name }},
	}
}

type msPost struct {
	ID       int64
	AuthorID int64
	Title    string
}

type msPostTable struct {
	mist.TableBase
	ID       mist.Col[msPost, int64]
	AuthorID mist.Col[msPost, int64]
	Title    mist.Col[msPost, string]
}

func (msPostTable) Row() *msPost { return nil }
func (t msPostTable) Columns() []sql.Chunk {
	return []sql.Chunk{t.ID, t.AuthorID, t.Title}
}
func (t msPostTable) Scan(row mist.Row, dst *msPost) error {
	return row.Scan(&dst.ID, &dst.AuthorID, &dst.Title)
}

func mssqlPosts() msPostTable {
	return msPostTable{
		TableBase: mist.TableBase{SQLName: "mist_ms_posts"},
		ID:        mist.Col[msPost, int64]{Table: "mist_ms_posts", Name: "id", Ptr: func(v *msPost) any { return &v.ID }},
		AuthorID:  mist.Col[msPost, int64]{Table: "mist_ms_posts", Name: "author_id", Ptr: func(v *msPost) any { return &v.AuthorID }},
		Title:     mist.Col[msPost, string]{Table: "mist_ms_posts", Name: "title", Ptr: func(v *msPost) any { return &v.Title }},
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
