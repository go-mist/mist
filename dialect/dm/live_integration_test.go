//go:build integration

package dm_test

import (
	"context"
	"os"
	"testing"

	"github.com/go-mist/mist"
	"github.com/go-mist/mist/dialect/dm"
	"github.com/go-mist/mist/op"
	"github.com/go-mist/mist/query"
	"github.com/go-mist/mist/sql"
)

func TestDMStatements(t *testing.T) {
	dsn := os.Getenv("DM_DSN")
	if dsn == "" {
		t.Skip("DM_DSN is not set")
	}
	ctx := context.Background()
	db, err := dm.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	for _, stmt := range []string{
		`DROP TABLE IF EXISTS "mist_dm_posts"`,
		`DROP TABLE IF EXISTS "mist_dm_users"`,
		`CREATE TABLE "mist_dm_users" ("id" BIGINT NOT NULL PRIMARY KEY, "name" VARCHAR(40) NOT NULL)`,
		`CREATE TABLE "mist_dm_posts" ("id" BIGINT NOT NULL PRIMARY KEY, "author_id" BIGINT NOT NULL, "title" VARCHAR(40) NOT NULL)`,
	} {
		if _, err := db.Exec(ctx, stmt); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		_, _ = db.Exec(context.Background(), `DROP TABLE IF EXISTS "mist_dm_posts"`)
		_, _ = db.Exec(context.Background(), `DROP TABLE IF EXISTS "mist_dm_users"`)
	})

	users := dmUsers()
	posts := dmPosts()
	if _, err := dm.Insert(users).Columns(users.ID, users.Name).Values(int64(1), "alice").Values(int64(2), "bob").Exec(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, err := dm.Insert(posts).Columns(posts.ID, posts.AuthorID, posts.Title).Values(int64(10), int64(1), "a").Values(int64(11), int64(1), "b").Exec(ctx, db); err != nil {
		t.Fatal(err)
	}
	joined, err := dm.Select(users.ID).From(users).
		InnerJoin(posts, op.Eq(posts.AuthorID, users.ID)).
		Where(op.Eq(users.Name, "alice")).
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
	names, err := dm.Select(users.Name).From(users).Distinct().OrderBy(op.Asc(users.Name)).All(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 2 || names[0].Name != "alice" || names[1].Name != "bob" {
		t.Fatalf("names = %#v", names)
	}
	matched, err := dm.Select(users.ID).From(users).Where(op.ILike(users.Name, "AL%")).OrderBy(op.Asc(users.ID)).All(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if len(matched) != 1 || matched[0].ID != 1 {
		t.Fatalf("ilike = %#v", matched)
	}
	both, err := dm.Select(users.ID).From(users).
		Union(dm.Select(users.ID).From(users)).
		OrderBy(op.Asc(users.ID)).
		Limit(2).
		All(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if len(both) != 2 {
		t.Fatalf("union = %#v", both)
	}
	second, err := dm.Select(users.ID).From(users).OrderBy(op.Asc(users.ID)).Offset(1).Limit(1).One(ctx, db)
	if err != nil || second.ID != 2 {
		t.Fatalf("offset = %+v %v", second, err)
	}
	tree := dm.Recursive("tree", dm.Select(users.ID).From(users).Where(op.Eq(users.ID, int64(1))))
	rooted, err := dm.Select(users.ID).From(users).With(tree).Where(op.Eq(users.ID, int64(1))).One(ctx, db)
	if err != nil || rooted.ID != 1 {
		t.Fatalf("with = %+v %v", rooted, err)
	}
	locked, err := dm.Select(users.ID).From(users).Where(op.Eq(users.ID, int64(1))).ForUpdate(query.RowLock{NoWait: true}).One(ctx, db)
	if err != nil || locked.ID != 1 {
		t.Fatalf("for update = %+v %v", locked, err)
	}
	if _, err := dm.Insert(users).Columns(users.ID, users.Name).
		Select(dm.Select(posts.ID, posts.Title).From(posts).Where(op.Eq(posts.ID, int64(10)))).
		Exec(ctx, db); err != nil {
		t.Fatal(err)
	}
	copied, err := dm.Select(users.Name).From(users).Where(op.Eq(users.ID, int64(10))).One(ctx, db)
	if err != nil || copied.Name != "a" {
		t.Fatalf("insert select = %+v %v", copied, err)
	}
	err = db.Tx(ctx, func(tx mist.Tx) error {
		_, err := dm.Update(users).Set(users.Name, "amy").Where(op.Eq(users.ID, int64(1))).Exec(ctx, tx)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := dm.Delete(users).Where(op.Eq(users.ID, int64(2))).Exec(ctx, db); err != nil {
		t.Fatal(err)
	}
	left, err := dm.Select(users.Name).From(users).Where(op.Eq(users.ID, int64(1))).One(ctx, db)
	if err != nil || left.Name != "amy" {
		t.Fatalf("left = %+v %v", left, err)
	}
}

type dmUser struct {
	ID   int64
	Name string
}

type dmUserTable struct {
	mist.TableBase
	ID   mist.Col[dmUser, int64]
	Name mist.Col[dmUser, string]
}

func (dmUserTable) Row() *dmUser { return nil }
func (t dmUserTable) Columns() []sql.Chunk {
	return []sql.Chunk{t.ID, t.Name}
}
func (t dmUserTable) Scan(row mist.Row, dst *dmUser) error {
	return row.Scan(&dst.ID, &dst.Name)
}

func dmUsers() dmUserTable {
	return dmUserTable{
		TableBase: mist.TableBase{SQLName: "mist_dm_users"},
		ID:        mist.Col[dmUser, int64]{Table: "mist_dm_users", Name: "id", Ptr: func(v *dmUser) any { return &v.ID }},
		Name:      mist.Col[dmUser, string]{Table: "mist_dm_users", Name: "name", Ptr: func(v *dmUser) any { return &v.Name }},
	}
}

type dmPost struct {
	ID       int64
	AuthorID int64
	Title    string
}

type dmPostTable struct {
	mist.TableBase
	ID       mist.Col[dmPost, int64]
	AuthorID mist.Col[dmPost, int64]
	Title    mist.Col[dmPost, string]
}

func (dmPostTable) Row() *dmPost { return nil }
func (t dmPostTable) Columns() []sql.Chunk {
	return []sql.Chunk{t.ID, t.AuthorID, t.Title}
}
func (t dmPostTable) Scan(row mist.Row, dst *dmPost) error {
	return row.Scan(&dst.ID, &dst.AuthorID, &dst.Title)
}

func dmPosts() dmPostTable {
	return dmPostTable{
		TableBase: mist.TableBase{SQLName: "mist_dm_posts"},
		ID:        mist.Col[dmPost, int64]{Table: "mist_dm_posts", Name: "id", Ptr: func(v *dmPost) any { return &v.ID }},
		AuthorID:  mist.Col[dmPost, int64]{Table: "mist_dm_posts", Name: "author_id", Ptr: func(v *dmPost) any { return &v.AuthorID }},
		Title:     mist.Col[dmPost, string]{Table: "mist_dm_posts", Name: "title", Ptr: func(v *dmPost) any { return &v.Title }},
	}
}
