//go:build integration

package sqlite_test

import (
	"context"
	"testing"

	"github.com/go-mist/mist/internal/testdb"
	"github.com/go-mist/mist/op"
	"github.com/go-mist/mist/sql"
	"github.com/go-mist/mist/sqlite"
)

type rowUser struct {
	ID   int64
	Name string
}

type usersTable struct {
	sqlite.TableBase
	ID   sqlite.Col[rowUser, int64]
	Name sqlite.Col[rowUser, string]
}

func (usersTable) Row() *rowUser { return nil }

func (t usersTable) Columns() []sql.Chunk { return []sql.Chunk{t.ID, t.Name} }

func (usersTable) Scan(row sqlite.Row, dst *rowUser) error {
	return row.Scan(&dst.ID, &dst.Name)
}

var users = usersTable{
	TableBase: sqlite.TableBase{SQLName: "m9_users"},
	ID: sqlite.Col[rowUser, int64]{
		Table: "m9_users",
		Name:  "id",
		Ptr:   func(v *rowUser) any { return &v.ID },
	},
	Name: sqlite.Col[rowUser, string]{
		Table: "m9_users",
		Name:  "name",
		Ptr:   func(v *rowUser) any { return &v.Name },
	},
}

type rowPost struct {
	ID       int64
	AuthorID int64
	Title    string
}

type postsTable struct {
	sqlite.TableBase
	ID       sqlite.Col[rowPost, int64]
	AuthorID sqlite.Col[rowPost, int64]
	Title    sqlite.Col[rowPost, string]
}

func (postsTable) Row() *rowPost { return nil }

func (t postsTable) Columns() []sql.Chunk { return []sql.Chunk{t.ID, t.AuthorID, t.Title} }

func (postsTable) Scan(row sqlite.Row, dst *rowPost) error {
	return row.Scan(&dst.ID, &dst.AuthorID, &dst.Title)
}

var posts = postsTable{
	TableBase: sqlite.TableBase{SQLName: "m9_posts"},
	ID: sqlite.Col[rowPost, int64]{
		Table: "m9_posts",
		Name:  "id",
		Ptr:   func(v *rowPost) any { return &v.ID },
	},
	AuthorID: sqlite.Col[rowPost, int64]{
		Table: "m9_posts",
		Name:  "author_id",
		Ptr:   func(v *rowPost) any { return &v.AuthorID },
	},
	Title: sqlite.Col[rowPost, string]{
		Table: "m9_posts",
		Name:  "title",
		Ptr:   func(v *rowPost) any { return &v.Title },
	},
}

func openDB(t *testing.T) *sqlite.DB {
	t.Helper()
	ctx := context.Background()
	db, err := sqlite.Open(ctx, testdb.SQLiteDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestCRUD(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	var version string
	rows, err := db.Query(ctx, "select sqlite_version()")
	if err != nil {
		t.Fatal(err)
	}
	if !rows.Next() {
		t.Fatal(rows.Err())
	}
	if err := rows.Scan(&version); err != nil {
		t.Fatal(err)
	}
	rows.Close()
	if version == "" {
		t.Fatal("empty sqlite_version")
	}
	t.Log("sqlite", version)

	if _, err := db.Exec(ctx, `create table m9_users (id integer primary key, name text not null)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `create table m9_posts (id integer primary key, author_id integer not null, title text not null)`); err != nil {
		t.Fatal(err)
	}

	inserted, err := sqlite.Insert(users).Columns(users.ID, users.Name).
		Values(int64(1), "bob").
		Values(int64(7), "alice").
		Returning(users.ID, users.Name).
		All(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if len(inserted) != 2 || inserted[1].Name != "alice" {
		t.Fatalf("%+v", inserted)
	}
	if _, err := sqlite.Insert(posts).Columns(posts.ID, posts.AuthorID, posts.Title).
		Values(int64(1), int64(7), "hello").
		Exec(ctx, db); err != nil {
		t.Fatal(err)
	}

	got, err := sqlite.Select(users.ID, users.Name).From(users).
		Where(op.Eq(users.Name, "alice")).All(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != 7 {
		t.Fatalf("%+v", got)
	}

	joined, err := sqlite.Select(users.Name).From(users).
		InnerJoin(posts, op.Eq(posts.AuthorID, users.ID)).All(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if len(joined) != 1 || joined[0].Name != "alice" {
		t.Fatalf("%+v", joined)
	}

	full, err := sqlite.Select(users.Name).From(users).
		FullJoin(posts, op.Eq(posts.AuthorID, users.ID)).All(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if len(full) != 2 {
		t.Fatalf("%+v", full)
	}

	unioned, err := sqlite.Select(users.Name).From(users).Where(op.Eq(users.Name, "alice")).
		Union(sqlite.Select(users.Name).From(users).Where(op.Eq(users.Name, "bob"))).
		All(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if len(unioned) != 2 {
		t.Fatalf("%+v", unioned)
	}

	active := sqlite.CTE("active", sqlite.Select(users.ID, users.Name).From(users).Where(op.Eq(users.Name, "alice")))
	withRows, err := sqlite.Select(active.Col(users.Name)).From(active).With(active).All(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if len(withRows) != 1 || withRows[0].Name != "alice" {
		t.Fatalf("%+v", withRows)
	}

	if _, err := sqlite.Update(users).Set(users.Name, "ann").Where(op.Eq(users.ID, int64(1))).Exec(ctx, db); err != nil {
		t.Fatal(err)
	}
	renamed, err := sqlite.Select(users.Name).From(users).Where(op.Eq(users.ID, int64(1))).All(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if len(renamed) != 1 || renamed[0].Name != "ann" {
		t.Fatalf("%+v", renamed)
	}

	n, err := sqlite.Delete(users).Where(op.Eq(users.ID, int64(1))).Exec(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatal(n)
	}

	if _, err := sqlite.Insert(users).Columns(users.ID, users.Name).
		Values(int64(7), "zoe").
		OnConflictDoUpdate(users.ID).Set(users.Name, sqlite.Excluded(users.Name)).
		Exec(ctx, db); err != nil {
		t.Fatal(err)
	}
	conflicted, err := sqlite.Select(users.Name).From(users).Where(op.Eq(users.ID, int64(7))).All(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if len(conflicted) != 1 || conflicted[0].Name != "zoe" {
		t.Fatalf("%+v", conflicted)
	}

	if _, err := sqlite.Insert(users).Columns(users.ID, users.Name).
		Values(int64(7), "nope").
		Ignore().
		Exec(ctx, db); err != nil {
		t.Fatal(err)
	}
	kept, err := sqlite.Select(users.Name).From(users).Where(op.Eq(users.ID, int64(7))).All(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if len(kept) != 1 || kept[0].Name != "zoe" {
		t.Fatalf("%+v", kept)
	}

	matched, err := sqlite.Select(users.ID).From(users).Where(op.ILike(users.Name, "ZOE")).All(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if len(matched) != 1 || matched[0].ID != 7 {
		t.Fatalf("%+v", matched)
	}

	err = db.Tx(ctx, func(tx *sqlite.Tx) error {
		if _, err := sqlite.Insert(users).Columns(users.ID, users.Name).Values(int64(9), "tmp").Exec(ctx, tx); err != nil {
			return err
		}
		err := tx.Tx(ctx, func(inner *sqlite.Tx) error {
			if _, err := sqlite.Update(users).Set(users.Name, "rolled").Where(op.Eq(users.ID, int64(9))).Exec(ctx, inner); err != nil {
				return err
			}
			return errRollback
		})
		if err != errRollback {
			return err
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	after, err := sqlite.Select(users.Name).From(users).Where(op.Eq(users.ID, int64(9))).All(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != 1 || after[0].Name != "tmp" {
		t.Fatalf("%+v", after)
	}
}

var errRollback = errString("rollback")

type errString string

func (e errString) Error() string { return string(e) }
