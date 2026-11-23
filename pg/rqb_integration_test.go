//go:build integration

package pg_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/go-mist/mist/internal/testdb"
	"github.com/go-mist/mist/pg"
	"github.com/go-mist/mist/relation"
	"github.com/go-mist/mist/sql"
)

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
	db := openTestDB(t, ctx)
	resetRQB(t, ctx, db)
	for _, stmt := range []string{
		`insert into rqb_users (id, name) values (1, 'alice'), (2, 'bob'), (3, 'cara')`,
		`insert into rqb_posts (id, title, author_id) values (10, 'a', 1), (11, 'b', 1), (12, 'a', 2), (13, 'z', 99)`,
		`insert into rqb_comments (id, body, post_id) values (100, 'c', 10), (101, 'd', 10)`,
	} {
		if _, err := db.Exec(ctx, stmt); err != nil {
			t.Fatal(err)
		}
	}
	schema := rqbLiveSchema(t)

	t.Run("nested", func(t *testing.T) {
		got, err := pg.FindMany[rqbUser](ctx, db, schema, "users", relation.Config{
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
		if len(got) != 3 || got[0].Name != "alice" || got[1].Name != "bob" || got[2].Name != "cara" {
			t.Fatalf("users = %#v", got)
		}
		if len(got[0].Posts) != 2 || got[0].Posts[0].ID != 11 || got[0].Posts[1].ID != 10 {
			t.Fatalf("alice posts = %#v", got[0].Posts)
		}
		if len(got[0].Posts[1].Comments) != 2 || got[0].Posts[1].Comments[0].ID != 100 || got[0].Posts[1].Comments[1].Body != "d" {
			t.Fatalf("comments = %#v", got[0].Posts[1].Comments)
		}
		if got[0].Posts[0].Comments == nil || len(got[0].Posts[0].Comments) != 0 {
			t.Fatalf("empty comments = %#v", got[0].Posts[0].Comments)
		}
		if len(got[1].Posts) != 1 || got[1].Posts[0].ID != 12 {
			t.Fatalf("bob posts = %#v", got[1].Posts)
		}
		if got[2].Posts == nil || len(got[2].Posts) != 0 {
			t.Fatalf("cara posts = %#v", got[2].Posts)
		}
	})

	t.Run("columns", func(t *testing.T) {
		got, err := pg.FindMany[rqbUser](ctx, db, schema, "users", relation.Config{
			Columns: relation.Only("id", "name"),
			Where:   relation.Eq("id", 1),
			With: []relation.With{{
				Name:  "posts",
				Query: &relation.Config{Columns: relation.Only("id", "title"), Order: relation.Asc("id")},
			}},
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 || got[0].Posts[0].AuthorID != 0 || got[0].Posts[0].Title != "a" {
			t.Fatalf("projection = %#v", got)
		}
	})

	t.Run("exists", func(t *testing.T) {
		got, err := pg.FindMany[rqbPost](ctx, db, schema, "posts", relation.Config{
			Where: relation.And(relation.Eq("title", "a"), relation.Exists("comments")),
			Order: relation.Asc("id"),
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 || got[0].ID != 10 {
			t.Fatalf("exists = %#v", got)
		}
	})

	t.Run("count order", func(t *testing.T) {
		got, err := pg.FindMany[rqbUser](ctx, db, schema, "users", relation.Config{
			Order: relation.CountOrder("posts", "desc"),
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 3 || got[0].Name != "alice" || got[1].Name != "bob" || got[2].Name != "cara" {
			t.Fatalf("count order = %#v", namesOf(got))
		}
	})

	t.Run("nested limit", func(t *testing.T) {
		got, err := pg.FindMany[rqbUser](ctx, db, schema, "users", relation.Config{
			Where: relation.Eq("id", 1),
			With: []relation.With{{
				Name: "posts",
				Query: &relation.Config{
					Order:  relation.Desc("id"),
					Limit:  relation.Int(1),
					Offset: relation.Int(0),
				},
			}},
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 || len(got[0].Posts) != 1 || got[0].Posts[0].ID != 11 {
			t.Fatalf("limit = %#v", got)
		}
	})

	t.Run("null one", func(t *testing.T) {
		got, err := pg.FindFirst[rqbPost](ctx, db, schema, "posts", relation.Config{
			Where: relation.Eq("id", 13),
			With:  []relation.With{{Name: "author"}},
		})
		if err != nil {
			t.Fatal(err)
		}
		if got.Author != nil {
			t.Fatalf("author = %#v", got.Author)
		}
		ok, err := pg.FindFirst[rqbPost](ctx, db, schema, "posts", relation.Config{
			Where: relation.Eq("id", 12),
			With:  []relation.With{{Name: "author"}},
		})
		if err != nil {
			t.Fatal(err)
		}
		if ok.Author == nil || ok.Author.Name != "bob" {
			t.Fatalf("author = %#v", ok.Author)
		}
	})
}

func TestRelationalNoNPlusOne(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t, ctx)
	resetRQB(t, ctx, db)
	if _, err := db.Exec(ctx, "set log_statement = 'all'"); err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		`insert into rqb_users (id, name) select g, 'u' || g from generate_series(1, 10000) g`,
		`insert into rqb_posts (id, title, author_id) select (u - 1) * 10 + k, 't', u from generate_series(1, 10000) u cross join generate_series(1, 10) k`,
	} {
		if _, err := db.Exec(ctx, stmt); err != nil {
			t.Fatal(err)
		}
	}
	tracer := &queryCounter{}
	traced := openTracedDB(t, ctx, tracer)
	tracer.n.Store(0)
	schema := rqbLiveSchema(t)
	got, err := pg.FindMany[rqbUser](ctx, traced, schema, "users", relation.Config{
		With: []relation.With{{Name: "posts"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if tracer.n.Load() != 1 {
		t.Fatalf("queries = %d, want 1", tracer.n.Load())
	}
	if len(got) != 10000 || len(got[0].Posts) != 10 {
		t.Fatalf("rows %d posts %d", len(got), len(got[0].Posts))
	}
}

type queryCounter struct {
	n atomic.Int32
}

func (c *queryCounter) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	c.n.Add(1)
	return ctx
}

func (c *queryCounter) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func openTracedDB(t *testing.T, ctx context.Context, tracer pgx.QueryTracer) *pg.DB {
	t.Helper()
	dsn := testdb.PostgresDSN(t)
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.MaxConns = 4
	cfg.ConnConfig.Tracer = tracer
	traced, err := pg.OpenConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(traced.Close)
	return traced
}

func namesOf(users []rqbUser) []string {
	out := make([]string, len(users))
	for i, u := range users {
		out[i] = u.Name
	}
	return out
}

func rqbLiveSchema(t *testing.T) *relation.Schema {
	t.Helper()
	s, err := relation.Define([]relation.Table{
		{
			Name:    "users",
			SQLName: "rqb_users",
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
			SQLName: "rqb_posts",
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
			SQLName: "rqb_comments",
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

type castParent struct {
	ID   int64
	Kids []castKid
}

type castKid struct {
	ID      int64
	Amount  string
	At      time.Time
	N       int64
	Payload []byte
	Qty     int
}

func TestRelationalLeftoversRoundTrip(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t, ctx)
	for _, stmt := range []string{
		"drop table if exists mm_members",
		"drop table if exists mm_groups",
		"drop table if exists mm_users",
		"drop table if exists cast_kids",
		"drop table if exists cast_parents",
		`create table mm_users (id int primary key, name text not null)`,
		`create table mm_groups (id int primary key, name text not null)`,
		`create table mm_members (user_id int not null, group_id int not null)`,
		`create table cast_parents (id int primary key)`,
		`create table cast_kids (id int primary key, parent_id int not null, amount numeric(10,2) not null, at timestamptz not null, n bigint not null, payload bytea not null, qty int not null)`,
		`insert into mm_users (id, name) values (1, 'alice'), (2, 'bob'), (3, 'cara')`,
		`insert into mm_groups (id, name) values (1, 'a'), (2, 'b')`,
		`insert into mm_members (user_id, group_id) values (1, 1), (1, 2), (2, 1)`,
		`insert into cast_parents (id) values (1)`,
		`insert into cast_kids (id, parent_id, amount, at, n, payload, qty) values (7, 1, 12.50, timestamptz '2026-11-20 08:30:00+08', 9007199254740993, '\x0102', 4)`,
	} {
		if _, err := db.Exec(ctx, stmt); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("through and extras", func(t *testing.T) {
		got, err := pg.FindMany[mmUser](ctx, db, mmSchema(t), "users", relation.Config{
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
		if got[2].Groups == nil || len(got[2].Groups) != 0 {
			t.Fatalf("cara groups = %#v", got[2].Groups)
		}
		ordered, err := pg.FindMany[mmUser](ctx, db, mmSchema(t), "users", relation.Config{
			Order: relation.CountOrder("groups", "desc"),
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(ordered) != 3 || ordered[0].Name != "alice" || ordered[1].Name != "bob" || ordered[2].Name != "cara" {
			t.Fatalf("count order = %#v", ordered)
		}
		matched, err := pg.FindMany[mmUser](ctx, db, mmSchema(t), "users", relation.Config{
			Where: relation.Exists("groups"),
			Order: relation.Asc("id"),
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(matched) != 2 || matched[0].Name != "alice" || matched[1].Name != "bob" {
			t.Fatalf("exists = %#v", matched)
		}
	})

	t.Run("json cast", func(t *testing.T) {
		got, err := pg.FindMany[castParent](ctx, db, castLiveSchema(t), "parents", relation.Config{
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
		wantAt := time.Date(2026, 11, 20, 8, 30, 0, 0, time.FixedZone("CST", 8*3600))
		if kid.Amount != "12.50" || kid.N != 9007199254740993 || kid.Qty != 4 || !kid.At.Equal(wantAt) || len(kid.Payload) != 2 || kid.Payload[0] != 1 || kid.Payload[1] != 2 {
			t.Fatalf("kid = %#v", kid)
		}
	})

	t.Run("cache", func(t *testing.T) {
		tracer := &queryCounter{}
		traced := openTracedDB(t, ctx, tracer)
		cache := &relation.MemoryCache{}
		cfg := relation.Config{
			Where:     relation.Eq("id", 1),
			WithCache: relation.Bool(true),
			Cache:     cache,
		}
		tracer.n.Store(0)
		first, err := pg.FindMany[mmUser](ctx, traced, mmSchema(t), "users", cfg)
		if err != nil {
			t.Fatal(err)
		}
		if tracer.n.Load() != 1 || len(first) != 1 || first[0].Name != "alice" {
			t.Fatalf("first %#v queries %d", first, tracer.n.Load())
		}
		tracer.n.Store(0)
		second, err := pg.FindMany[mmUser](ctx, traced, mmSchema(t), "users", cfg)
		if err != nil {
			t.Fatal(err)
		}
		if tracer.n.Load() != 0 || len(second) != 1 || second[0].Name != "alice" {
			t.Fatalf("second %#v queries %d", second, tracer.n.Load())
		}
	})
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
			SQLName: "mm_users",
			Columns: []relation.Column{
				{Name: "id", Ptr: func(row any) any { return &row.(*mmUser).ID }},
				{Name: "name", Ptr: func(row any) any { return &row.(*mmUser).Name }},
			},
			New:       func() any { return &mmUser{} },
			Relations: []relation.Relation{groups},
		},
		{
			Name:    "groups",
			SQLName: "mm_groups",
			Columns: []relation.Column{
				{Name: "id", Ptr: func(row any) any { return &row.(*mmGroup).ID }},
				{Name: "name", Ptr: func(row any) any { return &row.(*mmGroup).Name }},
			},
			New: func() any { return &mmGroup{} },
		},
		{
			Name:    "members",
			SQLName: "mm_members",
			Columns: []relation.Column{{Name: "user_id"}, {Name: "group_id"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
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
			SQLName: "cast_parents",
			Columns: []relation.Column{
				{Name: "id", Ptr: func(row any) any { return &row.(*castParent).ID }},
			},
			New:       func() any { return &castParent{} },
			Relations: []relation.Relation{kids},
		},
		{
			Name:    "kids",
			SQLName: "cast_kids",
			Columns: []relation.Column{
				{Name: "id", Ptr: func(row any) any { return &row.(*castKid).ID }},
				{Name: "parent_id"},
				{Name: "amount", CastInJSON: "text", Ptr: func(row any) any { return &row.(*castKid).Amount }},
				{Name: "at", CastInJSON: "text", Ptr: func(row any) any { return &row.(*castKid).At }, ReadJSON: relation.ReadTextTime},
				{Name: "n", CastInJSON: "text", Ptr: func(row any) any { return &row.(*castKid).N }, ReadJSON: relation.ReadTextInt},
				{Name: "payload", Ptr: func(row any) any { return &row.(*castKid).Payload }, ReadJSON: relation.ReadHexBytes},
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

func resetRQB(t *testing.T, ctx context.Context, db *pg.DB) {
	t.Helper()
	for _, stmt := range []string{
		"drop table if exists rqb_comments",
		"drop table if exists rqb_posts",
		"drop table if exists rqb_users",
		`create table rqb_users (id int primary key, name text not null)`,
		`create table rqb_posts (id int primary key, title text not null, author_id int not null)`,
		`create table rqb_comments (id int primary key, body text not null, post_id int not null)`,
	} {
		if _, err := db.Exec(ctx, stmt); err != nil {
			t.Fatal(err)
		}
	}
}
