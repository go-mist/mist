//go:build integration

package sqlite_test

import (
	"context"
	"testing"
	"time"

	"github.com/go-mist/mist/query"
	"github.com/go-mist/mist/relation"
	"github.com/go-mist/mist/sql"
	"github.com/go-mist/mist/sqlite"
)

type countingQueryer struct {
	db *sqlite.DB
	n  int
}

func (c *countingQueryer) Query(ctx context.Context, sqlText string, args ...any) (query.Rows, error) {
	c.n++
	return c.db.Query(ctx, sqlText, args...)
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
	Comments []rqbComment
	Author   *rqbUser
}

type rqbComment struct {
	ID     int64
	Body   string
	PostID int64
}

func TestRelationalRoundTrip(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	for _, stmt := range []string{
		`drop table if exists m9_rqb_comments`,
		`drop table if exists m9_rqb_posts`,
		`drop table if exists m9_rqb_users`,
		`create table m9_rqb_users (id bigint primary key, name varchar(64) not null)`,
		`create table m9_rqb_posts (id bigint primary key, title varchar(64) not null, author_id bigint not null)`,
		`create table m9_rqb_comments (id bigint primary key, body varchar(64) not null, post_id bigint not null)`,
		`insert into m9_rqb_users (id, name) values (1, 'alice'), (2, 'bob'), (3, 'cara')`,
		`insert into m9_rqb_posts (id, title, author_id) values (10, 'a', 1), (11, 'b', 1), (12, 'a', 2)`,
		`insert into m9_rqb_comments (id, body, post_id) values (100, 'c', 10), (101, 'd', 10)`,
	} {
		if _, err := db.Exec(ctx, stmt); err != nil {
			t.Fatal(err)
		}
	}
	schema := rqbLiveSchema(t)

	t.Run("nested", func(t *testing.T) {
		got, err := sqlite.FindMany[rqbUser](ctx, db, schema, "users", relation.Config{
			Order: relation.Asc("id"),
			With: []relation.With{{
				Name: "posts",
				Query: &relation.Config{
					Order: relation.Desc("id"),
					With:  []relation.With{{Name: "comments", Query: &relation.Config{Order: relation.Asc("id")}}},
				},
			}},
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 3 || got[0].Name != "alice" || got[2].Name != "cara" {
			t.Fatalf("users = %#v", got)
		}
		if len(got[0].Posts) != 2 || got[0].Posts[0].ID != 11 || got[0].Posts[1].ID != 10 {
			t.Fatalf("alice posts = %#v", got[0].Posts)
		}
		if len(got[0].Posts[1].Comments) != 2 || got[0].Posts[1].Comments[0].ID != 100 || got[0].Posts[1].Comments[1].Body != "d" {
			t.Fatalf("comments = %#v", got[0].Posts[1].Comments)
		}
		if len(got[0].Posts[0].Comments) != 0 {
			t.Fatalf("empty comments = %#v", got[0].Posts[0].Comments)
		}
		if len(got[2].Posts) != 0 {
			t.Fatalf("cara = %#v", got[2].Posts)
		}
	})

	t.Run("one", func(t *testing.T) {
		got, err := sqlite.FindMany[rqbPost](ctx, db, schema, "posts", relation.Config{
			Order: relation.Asc("id"),
			With:  []relation.With{{Name: "author"}},
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 3 || got[0].Author == nil || got[0].Author.Name != "alice" {
			t.Fatalf("posts = %#v", got)
		}
	})

	t.Run("first and cache", func(t *testing.T) {
		first, err := sqlite.FindFirst[rqbUser](ctx, db, schema, "users", relation.Config{Order: relation.Asc("id")})
		if err != nil {
			t.Fatal(err)
		}
		if first.ID != 1 || first.Name != "alice" {
			t.Fatalf("%+v", first)
		}
		counter := &countingQueryer{db: db}
		cache := &relation.MemoryCache{}
		cfg := relation.Config{
			Where:     relation.Eq("id", int64(1)),
			WithCache: relation.Bool(true),
			Cache:     cache,
		}
		got, err := sqlite.FindMany[rqbUser](ctx, counter, schema, "users", cfg)
		if err != nil {
			t.Fatal(err)
		}
		if counter.n != 1 || len(got) != 1 || got[0].Name != "alice" {
			t.Fatalf("%+v queries %d", got, counter.n)
		}
		again, err := sqlite.FindMany[rqbUser](ctx, counter, schema, "users", cfg)
		if err != nil {
			t.Fatal(err)
		}
		if counter.n != 1 || len(again) != 1 || again[0].Name != "alice" {
			t.Fatalf("%+v queries %d", again, counter.n)
		}
	})
}

type mmUser struct {
	ID     int64
	Name   string
	NLen   int
	Groups []mmGroup
}

type mmGroup struct {
	ID   int64
	Name string
	GLen int
}

func TestRelationalThrough(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	for _, stmt := range []string{
		`drop table if exists m9_mm_members`,
		`drop table if exists m9_mm_groups`,
		`drop table if exists m9_mm_users`,
		`create table m9_mm_users (id bigint primary key, name varchar(64) not null)`,
		`create table m9_mm_groups (id bigint primary key, name varchar(64) not null)`,
		`create table m9_mm_members (user_id bigint not null, group_id bigint not null)`,
		`insert into m9_mm_users (id, name) values (1, 'alice'), (2, 'bob'), (3, 'cara')`,
		`insert into m9_mm_groups (id, name) values (1, 'a'), (2, 'b')`,
		`insert into m9_mm_members (user_id, group_id) values (1, 1), (1, 2), (2, 1)`,
	} {
		if _, err := db.Exec(ctx, stmt); err != nil {
			t.Fatal(err)
		}
	}
	got, err := sqlite.FindMany[mmUser](ctx, db, mmSchema(t), "users", relation.Config{
		Order: relation.Asc("id"),
		Extras: []relation.Extra{{
			Key:  "nlen",
			Expr: sql.Raw(`length("d0"."name")`),
			Ptr:  func(row any) any { return &row.(*mmUser).NLen },
		}},
		With: []relation.With{{
			Name: "groups",
			Query: &relation.Config{
				Order: relation.Asc("id"),
				Extras: []relation.Extra{{
					Key:  "glen",
					Expr: sql.Raw(`length("d1"."name")`),
					Ptr:  func(row any) any { return &row.(*mmGroup).GLen },
				}},
			},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0].NLen != 5 || got[2].NLen != 4 {
		t.Fatalf("users = %#v", got)
	}
	if len(got[0].Groups) != 2 || got[0].Groups[0].Name != "a" || got[0].Groups[0].GLen != 1 || got[0].Groups[1].ID != 2 {
		t.Fatalf("alice groups = %#v", got[0].Groups)
	}
	if len(got[1].Groups) != 1 || got[1].Groups[0].Name != "a" {
		t.Fatalf("bob groups = %#v", got[1].Groups)
	}
	if len(got[2].Groups) != 0 {
		t.Fatalf("cara groups = %#v", got[2].Groups)
	}
	ordered, err := sqlite.FindMany[mmUser](ctx, db, mmSchema(t), "users", relation.Config{
		Order: relation.CountOrder("groups", "desc"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(ordered) != 3 || ordered[0].Name != "alice" || ordered[1].Name != "bob" || ordered[2].Name != "cara" {
		t.Fatalf("count order = %#v", ordered)
	}
	matched, err := sqlite.FindMany[mmUser](ctx, db, mmSchema(t), "users", relation.Config{
		Where: relation.Exists("groups"),
		Order: relation.Asc("id"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(matched) != 2 || matched[0].Name != "alice" || matched[1].Name != "bob" {
		t.Fatalf("exists = %#v", matched)
	}
}

type castParent struct {
	ID   int64
	Kids []castKid
}

type castKid struct {
	ID     int64
	Amount string
	At     time.Time
	N      int64
	Qty    int
}

func TestRelationalJSONCast(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	for _, stmt := range []string{
		`drop table if exists m9_cast_kids`,
		`drop table if exists m9_cast_parents`,
		`create table m9_cast_parents (id bigint primary key)`,
		`create table m9_cast_kids (
			id bigint primary key,
			parent_id bigint not null,
			amount numeric not null,
			at text not null,
			n bigint not null,
			qty int not null
		)`,
		`insert into m9_cast_parents (id) values (1)`,
		`insert into m9_cast_kids (id, parent_id, amount, at, n, qty) values (7, 1, 12.50, '2026-11-20 08:30:00', 9007199254740993, 4)`,
	} {
		if _, err := db.Exec(ctx, stmt); err != nil {
			t.Fatal(err)
		}
	}
	got, err := sqlite.FindMany[castParent](ctx, db, castLiveSchema(t), "parents", relation.Config{
		With: []relation.With{{
			Name:  "kids",
			Query: &relation.Config{Columns: relation.Except("parent_id")},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || len(got[0].Kids) != 1 {
		t.Fatalf("parents = %#v", got)
	}
	kid := got[0].Kids[0]
	wantAt := time.Date(2026, 11, 20, 8, 30, 0, 0, time.UTC)
	// SQLite numeric does not keep trailing zeros. 12.50 reads back as 12.5.
	if kid.Amount != "12.5" || kid.N != 9007199254740993 || kid.Qty != 4 || !kid.At.Equal(wantAt) {
		t.Fatalf("kid = %#v", kid)
	}
}

func castLiveSchema(t *testing.T) *relation.Schema {
	t.Helper()
	kids := relation.Many("kids", "kids", "id", "parent_id")
	kids.Init = func(parent any) { parent.(*castParent).Kids = []castKid{} }
	kids.Place = func(parent, child any) {
		parent.(*castParent).Kids = append(parent.(*castParent).Kids, *child.(*castKid))
	}
	s, err := relation.Define([]relation.Table{
		{
			Name:    "parents",
			SQLName: "m9_cast_parents",
			Columns: []relation.Column{
				{Name: "id", Ptr: func(row any) any { return &row.(*castParent).ID }},
			},
			New:       func() any { return &castParent{} },
			Relations: []relation.Relation{kids},
		},
		{
			Name:    "kids",
			SQLName: "m9_cast_kids",
			Columns: []relation.Column{
				{Name: "id", Ptr: func(row any) any { return &row.(*castKid).ID }},
				{Name: "parent_id"},
				{Name: "amount", CastInJSON: "text", Ptr: func(row any) any { return &row.(*castKid).Amount }},
				{Name: "at", CastInJSON: "text", Ptr: func(row any) any { return &row.(*castKid).At }, ReadJSON: relation.ReadTextTime},
				{Name: "n", CastInJSON: "text", Ptr: func(row any) any { return &row.(*castKid).N }, ReadJSON: relation.ReadTextInt},
				{Name: "qty", Ptr: func(row any) any { return &row.(*castKid).Qty }},
			},
			New: func() any { return &castKid{} },
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func mmSchema(t *testing.T) *relation.Schema {
	t.Helper()
	groups := relation.ManyThrough("groups", "groups", "id", "id", "members", "user_id", "group_id")
	groups.Init = func(parent any) { parent.(*mmUser).Groups = []mmGroup{} }
	groups.Place = func(parent, child any) {
		parent.(*mmUser).Groups = append(parent.(*mmUser).Groups, *child.(*mmGroup))
	}
	s, err := relation.Define([]relation.Table{
		{
			Name:    "users",
			SQLName: "m9_mm_users",
			Columns: []relation.Column{
				{Name: "id", Ptr: func(row any) any { return &row.(*mmUser).ID }},
				{Name: "name", Ptr: func(row any) any { return &row.(*mmUser).Name }},
			},
			New:       func() any { return &mmUser{} },
			Relations: []relation.Relation{groups},
		},
		{
			Name:    "groups",
			SQLName: "m9_mm_groups",
			Columns: []relation.Column{
				{Name: "id", Ptr: func(row any) any { return &row.(*mmGroup).ID }},
				{Name: "name", Ptr: func(row any) any { return &row.(*mmGroup).Name }},
			},
			New: func() any { return &mmGroup{} },
		},
		{
			Name:    "members",
			SQLName: "m9_mm_members",
			Columns: []relation.Column{{Name: "user_id"}, {Name: "group_id"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func rqbLiveSchema(t *testing.T) *relation.Schema {
	t.Helper()
	s, err := relation.Define([]relation.Table{
		{
			Name:    "users",
			SQLName: "m9_rqb_users",
			Columns: []relation.Column{
				{Name: "id", Ptr: func(row any) any { return &row.(*rqbUser).ID }},
				{Name: "name", Ptr: func(row any) any { return &row.(*rqbUser).Name }},
			},
			New: func() any { return &rqbUser{} },
			Relations: []relation.Relation{
				func() relation.Relation {
					r := relation.Many("posts", "posts", "id", "author_id")
					r.Init = func(parent any) { parent.(*rqbUser).Posts = []rqbPost{} }
					r.Place = func(parent, child any) {
						p := parent.(*rqbUser)
						p.Posts = append(p.Posts, *child.(*rqbPost))
					}
					return r
				}(),
			},
		},
		{
			Name:    "posts",
			SQLName: "m9_rqb_posts",
			Columns: []relation.Column{
				{Name: "id", Ptr: func(row any) any { return &row.(*rqbPost).ID }},
				{Name: "title", Ptr: func(row any) any { return &row.(*rqbPost).Title }},
				{Name: "author_id", Ptr: func(row any) any { return &row.(*rqbPost).AuthorID }},
			},
			New: func() any { return &rqbPost{} },
			Relations: []relation.Relation{
				func() relation.Relation {
					r := relation.Many("comments", "comments", "id", "post_id")
					r.Init = func(parent any) { parent.(*rqbPost).Comments = []rqbComment{} }
					r.Place = func(parent, child any) {
						p := parent.(*rqbPost)
						p.Comments = append(p.Comments, *child.(*rqbComment))
					}
					return r
				}(),
				func() relation.Relation {
					r := relation.One("author", "users", "author_id", "id")
					r.Place = func(parent, child any) { parent.(*rqbPost).Author = child.(*rqbUser) }
					return r
				}(),
			},
		},
		{
			Name:    "comments",
			SQLName: "m9_rqb_comments",
			Columns: []relation.Column{
				{Name: "id", Ptr: func(row any) any { return &row.(*rqbComment).ID }},
				{Name: "body", Ptr: func(row any) any { return &row.(*rqbComment).Body }},
				{Name: "post_id", Ptr: func(row any) any { return &row.(*rqbComment).PostID }},
			},
			New: func() any { return &rqbComment{} },
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}
