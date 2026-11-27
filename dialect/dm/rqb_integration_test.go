//go:build integration

package dm_test

import (
	"context"
	"os"
	"testing"

	"github.com/go-mist/mist/dialect/dm"
	"github.com/go-mist/mist/query"
	"github.com/go-mist/mist/relation"
)

func TestDMRelational(t *testing.T) {
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
		`DROP TABLE IF EXISTS "mist_dm_rqb_members"`,
		`DROP TABLE IF EXISTS "mist_dm_rqb_posts"`,
		`DROP TABLE IF EXISTS "mist_dm_rqb_groups"`,
		`DROP TABLE IF EXISTS "mist_dm_rqb_users"`,
		`CREATE TABLE "mist_dm_rqb_users" ("id" BIGINT NOT NULL PRIMARY KEY, "name" VARCHAR(40) NOT NULL)`,
		`CREATE TABLE "mist_dm_rqb_posts" ("id" BIGINT NOT NULL PRIMARY KEY, "title" VARCHAR(40) NOT NULL, "author_id" BIGINT NOT NULL)`,
		`CREATE TABLE "mist_dm_rqb_groups" ("id" BIGINT NOT NULL PRIMARY KEY, "name" VARCHAR(40) NOT NULL)`,
		`CREATE TABLE "mist_dm_rqb_members" ("user_id" BIGINT NOT NULL, "group_id" BIGINT NOT NULL)`,
		`INSERT INTO "mist_dm_rqb_users" ("id", "name") VALUES (1, 'alice')`,
		`INSERT INTO "mist_dm_rqb_users" ("id", "name") VALUES (2, 'bob')`,
		`INSERT INTO "mist_dm_rqb_posts" ("id", "title", "author_id") VALUES (10, 'a"b', 1)`,
		`INSERT INTO "mist_dm_rqb_posts" ("id", "title", "author_id") VALUES (11, 'b', 1)`,
		`INSERT INTO "mist_dm_rqb_groups" ("id", "name") VALUES (7, 'crew')`,
		`INSERT INTO "mist_dm_rqb_members" ("user_id", "group_id") VALUES (1, 7)`,
	} {
		if _, err := db.Exec(ctx, stmt); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		for _, stmt := range []string{
			`DROP TABLE IF EXISTS "mist_dm_rqb_members"`,
			`DROP TABLE IF EXISTS "mist_dm_rqb_posts"`,
			`DROP TABLE IF EXISTS "mist_dm_rqb_groups"`,
			`DROP TABLE IF EXISTS "mist_dm_rqb_users"`,
		} {
			_, _ = db.Exec(context.Background(), stmt)
		}
	})

	built, err := relation.Define([]relation.Table{
		{
			Name: "users", SQLName: "mist_dm_rqb_users",
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
			Name: "posts", SQLName: "mist_dm_rqb_posts",
			New: func() any { return &rqbPost{} },
			Columns: []relation.Column{
				{Name: "id", Ptr: func(row any) any { return &row.(*rqbPost).ID }},
				{Name: "title", Ptr: func(row any) any { return &row.(*rqbPost).Title }},
				{Name: "author_id", Ptr: func(row any) any { return &row.(*rqbPost).AuthorID }},
			},
			Relations: []relation.Relation{func() relation.Relation {
				r := relation.One("author", "users", "author_id", "id")
				r.Place = func(parent, child any) {
					user := *child.(*rqbUser)
					parent.(*rqbPost).Author = &user
				}
				return r
			}()},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := dm.FindMany[rqbUser](ctx, db, built, "users", relation.Config{
		Order: relation.Asc("id"),
		With: []relation.With{{
			Name:  "posts",
			Query: &relation.Config{Order: relation.Desc("id")},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Name != "alice" || len(got[0].Posts) != 2 || got[0].Posts[0].ID != 11 || got[0].Posts[0].Title != "b" || got[0].Posts[1].Title != `a"b` {
		t.Fatalf("many %#v", got)
	}
	if got[1].Name != "bob" || len(got[1].Posts) != 0 {
		t.Fatalf("empty many %#v", got[1])
	}

	posts, err := dm.FindMany[rqbPost](ctx, db, built, "posts", relation.Config{
		Where: relation.Eq("id", int64(10)),
		With:  []relation.With{{Name: "author"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(posts) != 1 || posts[0].Author == nil || posts[0].Author.Name != "alice" || posts[0].Title != `a"b` {
		t.Fatalf("one %#v", posts)
	}

	_, err = dm.FindFirst[rqbUser](ctx, db, built, "users", relation.Config{Where: relation.Eq("id", int64(9))})
	if err != query.ErrNoRows {
		t.Fatalf("missing = %v", err)
	}

	linked, err := relation.Define([]relation.Table{
		{
			Name: "users", SQLName: "mist_dm_rqb_users",
			New: func() any { return &rqbMember{} },
			Columns: []relation.Column{
				{Name: "id", Ptr: func(row any) any { return &row.(*rqbMember).ID }},
				{Name: "name", Ptr: func(row any) any { return &row.(*rqbMember).Name }},
			},
			Relations: []relation.Relation{func() relation.Relation {
				r := relation.ManyThrough("groups", "groups", "id", "id", "members", "user_id", "group_id")
				r.Init = func(parent any) { parent.(*rqbMember).Groups = []rqbGroup{} }
				r.Place = func(parent, child any) {
					parent.(*rqbMember).Groups = append(parent.(*rqbMember).Groups, *child.(*rqbGroup))
				}
				return r
			}()},
		},
		{
			Name: "groups", SQLName: "mist_dm_rqb_groups",
			New: func() any { return &rqbGroup{} },
			Columns: []relation.Column{
				{Name: "id", Ptr: func(row any) any { return &row.(*rqbGroup).ID }},
				{Name: "name", Ptr: func(row any) any { return &row.(*rqbGroup).Name }},
			},
		},
		{Name: "members", SQLName: "mist_dm_rqb_members", Columns: []relation.Column{{Name: "user_id"}, {Name: "group_id"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	groups, err := dm.FindMany[rqbMember](ctx, db, linked, "users", relation.Config{
		Order: relation.Asc("id"),
		With:  []relation.With{{Name: "groups"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 2 || len(groups[0].Groups) != 1 || groups[0].Groups[0].Name != "crew" || len(groups[1].Groups) != 0 {
		t.Fatalf("through %#v", groups)
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
	Author   *rqbUser
}

type rqbMember struct {
	ID     int64
	Name   string
	Groups []rqbGroup
}

type rqbGroup struct {
	ID   int64
	Name string
}
