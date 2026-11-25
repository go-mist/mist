package pg_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/go-mist/mist/dialect/pg"
	"github.com/go-mist/mist/relation"
	"github.com/go-mist/mist/sql"
)

func TestRelationalSQLMatchesRC5(t *testing.T) {
	s := rqbSchema(t)
	cases := []struct {
		name   string
		table  string
		mode   string
		cfg    relation.Config
		sql    string
		params []any
	}{
		{
			name:  "three levels",
			table: "users",
			mode:  relation.ModeMany,
			cfg: relation.Config{With: []relation.With{{
				Name: "posts",
				Query: &relation.Config{With: []relation.With{{
					Name: "comments",
				}}},
			}}},
			sql: `select "d0"."id" as "id", "d0"."name" as "name", "posts"."r" as "posts" from "users" as "d0" left join lateral(select coalesce(json_agg(row_to_json("t".*)), '[]') as "r" from (select "d1"."id" as "id", "d1"."title" as "title", "d1"."author_id" as "author_id", "comments"."r" as "comments" from "posts" as "d1" left join lateral(select coalesce(json_agg(row_to_json("t".*)), '[]') as "r" from (select "d2"."id" as "id", "d2"."body" as "body", "d2"."post_id" as "post_id" from "comments" as "d2" where "d1"."id" = "d2"."post_id") as "t") as "comments" on true where "d0"."id" = "d1"."author_id") as "t") as "posts" on true`,
		},
		{
			name:  "three levels basic",
			table: "users",
			mode:  relation.ModeMany,
			cfg: relation.Config{
				Strategy: relation.StrategyBasic,
				With: []relation.With{{
					Name: "posts",
					Query: &relation.Config{With: []relation.With{{
						Name: "comments",
					}}},
				}},
			},
			sql: `select "d0"."id" as "id", "d0"."name" as "name", "posts"."r" as "posts" from "users" as "d0" left join lateral(select coalesce(json_agg(row_to_json("t".*)), '[]') as "r" from (select "d1"."id" as "id", "d1"."title" as "title", "d1"."author_id" as "author_id", "comments"."r" as "comments" from "posts" as "d1" left join lateral(select coalesce(json_agg(row_to_json("t".*)), '[]') as "r" from (select "d2"."id" as "id", "d2"."body" as "body", "d2"."post_id" as "post_id" from "comments" as "d2" where "d1"."id" = "d2"."post_id") as "t") as "comments" on true where "d0"."id" = "d1"."author_id") as "t") as "posts" on true`,
		},
		{
			name:  "columns",
			table: "users",
			mode:  relation.ModeMany,
			cfg: relation.Config{
				Columns: relation.Only("id", "name"),
				With: []relation.With{{
					Name:  "posts",
					Query: &relation.Config{Columns: relation.Only("id", "title")},
				}},
			},
			sql: `select "d0"."id" as "id", "d0"."name" as "name", "posts"."r" as "posts" from "users" as "d0" left join lateral(select coalesce(json_agg(row_to_json("t".*)), '[]') as "r" from (select "d1"."id" as "id", "d1"."title" as "title" from "posts" as "d1" where "d0"."id" = "d1"."author_id") as "t") as "posts" on true`,
		},
		{
			name:  "exists",
			table: "posts",
			mode:  relation.ModeMany,
			cfg: relation.Config{Where: relation.And(
				relation.Eq("title", "hi"),
				relation.Exists("comments"),
			)},
			sql:    `select "d0"."id" as "id", "d0"."title" as "title", "d0"."author_id" as "author_id" from "posts" as "d0" where (("d0"."title" = $1) and (exists (select * from "comments" as "f0" where "d0"."id" = "f0"."post_id")))`,
			params: []any{"hi"},
		},
		{
			name:  "count order",
			table: "users",
			mode:  relation.ModeMany,
			cfg:   relation.Config{Order: relation.CountOrder("posts", "desc")},
			sql:   `select "d0"."id" as "id", "d0"."name" as "name" from "users" as "d0" order by (select count(*) from "posts" as "o0" where "d0"."id" = "o0"."author_id") desc`,
		},
		{
			name:  "nested limit",
			table: "users",
			mode:  relation.ModeMany,
			cfg: relation.Config{With: []relation.With{{
				Name: "posts",
				Query: &relation.Config{
					Order:  relation.Desc("id"),
					Limit:  relation.Int(2),
					Offset: relation.Int(1),
				},
			}}},
			sql:    `select "d0"."id" as "id", "d0"."name" as "name", "posts"."r" as "posts" from "users" as "d0" left join lateral(select coalesce(json_agg(row_to_json("t".*)), '[]') as "r" from (select "d1"."id" as "id", "d1"."title" as "title", "d1"."author_id" as "author_id" from "posts" as "d1" where "d0"."id" = "d1"."author_id" order by "d1"."id" desc limit $1 offset $2) as "t") as "posts" on true`,
			params: []any{2, 1},
		},
		{
			name:  "root page optimized",
			table: "users",
			mode:  relation.ModeMany,
			cfg: relation.Config{
				With:   []relation.With{{Name: "posts"}},
				Order:  relation.Asc("id"),
				Limit:  relation.Int(10),
				Offset: relation.Int(5),
			},
			sql:    `select "d0"."id" as "id", "d0"."name" as "name", "posts"."r" as "posts" from (select "d0".* from "users" as "d0" order by "d0"."id" asc limit $1 offset $2) as "d0" left join lateral(select coalesce(json_agg(row_to_json("t".*)), '[]') as "r" from (select "d1"."id" as "id", "d1"."title" as "title", "d1"."author_id" as "author_id" from "posts" as "d1" where "d0"."id" = "d1"."author_id") as "t") as "posts" on true order by "d0"."id" asc`,
			params: []any{10, 5},
		},
		{
			name:  "root page basic",
			table: "users",
			mode:  relation.ModeMany,
			cfg: relation.Config{
				Strategy: relation.StrategyBasic,
				With:     []relation.With{{Name: "posts"}},
				Order:    relation.Asc("id"),
				Limit:    relation.Int(10),
				Offset:   relation.Int(5),
			},
			sql:    `select "d0"."id" as "id", "d0"."name" as "name", "posts"."r" as "posts" from "users" as "d0" left join lateral(select coalesce(json_agg(row_to_json("t".*)), '[]') as "r" from (select "d1"."id" as "id", "d1"."title" as "title", "d1"."author_id" as "author_id" from "posts" as "d1" where "d0"."id" = "d1"."author_id") as "t") as "posts" on true order by "d0"."id" asc limit $1 offset $2`,
			params: []any{10, 5},
		},
		{
			name:   "one",
			table:  "posts",
			mode:   relation.ModeMany,
			cfg:    relation.Config{With: []relation.With{{Name: "author"}}},
			sql:    `select "d0"."id" as "id", "d0"."title" as "title", "d0"."author_id" as "author_id", "author"."r" as "author" from "posts" as "d0" left join lateral(select row_to_json("t".*) "r" from (select "d1"."id" as "id", "d1"."name" as "name" from "users" as "d1" where "d0"."author_id" = "d1"."id" limit $1) as "t") as "author" on true`,
			params: []any{1},
		},
		{
			name:  "wrap relation filter",
			table: "users",
			mode:  relation.ModeMany,
			cfg: relation.Config{
				With:  []relation.With{{Name: "posts"}},
				Where: relation.Exists("posts"),
			},
			sql: `select "d0"."id" as "id", "d0"."name" as "name", "posts"."r" as "posts" from (select "d0".* from "users" as "d0" where exists (select * from "posts" as "f0" where "d0"."id" = "f0"."author_id")) as "d0" left join lateral(select coalesce(json_agg(row_to_json("t".*)), '[]') as "r" from (select "d1"."id" as "id", "d1"."title" as "title", "d1"."author_id" as "author_id" from "posts" as "d1" where "d0"."id" = "d1"."author_id") as "t") as "posts" on true`,
		},
		{
			name:  "aggregate columns",
			table: "users",
			mode:  relation.ModeMany,
			cfg: relation.Config{Order: relation.ByRel("posts", relation.Orders(
				relation.Aggregate("title", "desc", "max"),
				relation.Aggregate("id", "asc", "min"),
			))},
			sql: `select "d0"."id" as "id", "d0"."name" as "name" from "users" as "d0" order by (select max("o0"."title") from "posts" as "o0" where "d0"."id" = "o0"."author_id") desc, (select min("o0"."id") from "posts" as "o0" where "d0"."id" = "o0"."author_id") asc`,
		},
		{
			name:  "one order",
			table: "posts",
			mode:  relation.ModeMany,
			cfg:   relation.Config{Order: relation.ByRel("author", relation.Asc("name"))},
			sql:   `select "d0"."id" as "id", "d0"."title" as "title", "d0"."author_id" as "author_id" from "posts" as "d0" order by (select "o0"."name" from "users" as "o0" where "d0"."author_id" = "o0"."id" limit 1) asc`,
		},
		{
			name:  "field ops",
			table: "users",
			mode:  relation.ModeMany,
			cfg: relation.Config{Where: relation.And(
				relation.And(relation.Like("name", "a%"), relation.IsNotNull("name")),
				relation.And(relation.Gt("id", 1), relation.Lt("id", 10)),
			)},
			sql:    `select "d0"."id" as "id", "d0"."name" as "name" from "users" as "d0" where (((("d0"."name" like $1) and (("d0"."name" is not null)))) and ((("d0"."id" > $2) and ("d0"."id" < $3))))`,
			params: []any{"a%", 1, 10},
		},
		{
			name:  "not exists",
			table: "users",
			mode:  relation.ModeMany,
			cfg:   relation.Config{Where: relation.Not(relation.Exists("posts"))},
			sql:   `select "d0"."id" as "id", "d0"."name" as "name" from "users" as "d0" where not (exists (select * from "posts" as "f0" where "d0"."id" = "f0"."author_id"))`,
		},
		{
			name:  "and or",
			table: "users",
			mode:  relation.ModeMany,
			cfg: relation.Config{Where: relation.Or(
				relation.Eq("name", "a"),
				relation.And(
					relation.Eq("name", "b"),
					relation.WhereRel("posts", relation.Eq("title", "x")),
				),
			)},
			sql:    `select "d0"."id" as "id", "d0"."name" as "name" from "users" as "d0" where (("d0"."name" = $1) or ((("d0"."name" = $2) and (exists (select * from "posts" as "f0" where (("d0"."id" = "f0"."author_id") and ("f0"."title" = $3)))))))`,
			params: []any{"a", "b", "x"},
		},
		{
			name:  "except column",
			table: "users",
			mode:  relation.ModeMany,
			cfg:   relation.Config{Columns: relation.Except("name")},
			sql:   `select "d0"."id" as "id" from "users" as "d0"`,
		},
		{
			name:   "find first",
			table:  "users",
			mode:   relation.ModeFirst,
			cfg:    relation.Config{Where: relation.Eq("name", "a")},
			sql:    `select "d0"."id" as "id", "d0"."name" as "name" from "users" as "d0" where "d0"."name" = $1 limit $2`,
			params: []any{"a", 1},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := mustRQB(t, s, tc.table, tc.cfg, tc.mode)
			if got.SQL != tc.sql {
				t.Fatalf("sql\n%s\nwant\n%s", got.SQL, tc.sql)
			}
			if tc.params != nil && !reflect.DeepEqual(got.Params, tc.params) {
				t.Fatalf("params %#v", got.Params)
			}
		})
	}
}

func TestRelationalErrors(t *testing.T) {
	s := rqbSchema(t)
	_, err := pg.BuildRelationalQuery(s, "users", relation.Config{With: []relation.With{{Name: "nope"}}}, relation.ModeMany)
	if err == nil || !strings.Contains(err.Error(), "unknown relation") {
		t.Fatalf("unknown with: %v", err)
	}
	_, err = pg.BuildRelationalQuery(s, "users", relation.Config{Columns: relation.Only()}, relation.ModeMany)
	if err == nil || !strings.Contains(err.Error(), "selected no columns") {
		t.Fatalf("no fields: %v", err)
	}
	_, err = pg.BuildRelationalQuery(s, "users", relation.Config{Strategy: "fast"}, relation.ModeMany)
	if err == nil || !strings.Contains(err.Error(), "strategy") {
		t.Fatalf("strategy: %v", err)
	}
}

func TestInferredOneMatchesExplicit(t *testing.T) {
	s, err := relation.Define([]relation.Table{
		{
			Name:    "users",
			Columns: []relation.Column{{Name: "id"}, {Name: "name"}},
			Relations: []relation.Relation{
				relation.Many("posts", "posts", "id", "author_id"),
			},
		},
		{
			Name:    "posts",
			Columns: []relation.Column{{Name: "id"}, {Name: "title"}, {Name: "author_id"}},
			Relations: []relation.Relation{
				relation.OneOf("author", "users"),
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := mustRQB(t, s, "posts", relation.Config{With: []relation.With{{Name: "author"}}}, relation.ModeMany)
	want := `select "d0"."id" as "id", "d0"."title" as "title", "d0"."author_id" as "author_id", "author"."r" as "author" from "posts" as "d0" left join lateral(select row_to_json("t".*) "r" from (select "d1"."id" as "id", "d1"."name" as "name" from "users" as "d1" where "d0"."author_id" = "d1"."id" limit $1) as "t") as "author" on true`
	if got.SQL != want {
		t.Fatalf("sql\n%s", got.SQL)
	}
}

func TestRelationalLeftoversSQL(t *testing.T) {
	s := rqbSchema(t)
	plain := mustRQB(t, s, "users", relation.Config{Where: relation.Eq("name", "a")}, relation.ModeMany)
	cachedBuilt, err := pg.BuildRelationalQuery(s, "users", relation.Config{
		Where:     relation.Eq("name", "a"),
		WithCache: relation.Bool(true),
	}, relation.ModeMany)
	if err != nil {
		t.Fatal(err)
	}
	cached, err := (pg.Dialect{}).SQLToQuery(cachedBuilt.Query)
	if err != nil {
		t.Fatal(err)
	}
	if cached.SQL != plain.SQL {
		t.Fatalf("withCache changed sql\n%s", cached.SQL)
	}
	if cachedBuilt.WithCache == nil || !*cachedBuilt.WithCache {
		t.Fatal("root WithCache was dropped")
	}
	nested := mustRQB(t, s, "users", relation.Config{With: []relation.With{{
		Name:  "posts",
		Query: &relation.Config{WithCache: relation.Bool(true), Comment: ""},
	}}}, relation.ModeMany)
	base := mustRQB(t, s, "users", relation.Config{With: []relation.With{{Name: "posts"}}}, relation.ModeMany)
	if nested.SQL != base.SQL {
		t.Fatalf("nested withCache changed sql\n%s", nested.SQL)
	}

	cases := []struct {
		name  string
		table string
		cfg   relation.Config
		sql   string
	}{
		{
			name:  "comment text",
			table: "users",
			cfg:   relation.Config{Comment: "trace"},
			sql:   `select "d0"."id" as "id", "d0"."name" as "name" from "users" as "d0" /*trace*/`,
		},
		{
			name:  "comment escapes delimiters",
			table: "users",
			cfg:   relation.Config{Comment: "a /* b */ c"},
			sql:   `select "d0"."id" as "id", "d0"."name" as "name" from "users" as "d0" /*a / * b * / c*/`,
		},
		{
			name:  "comment tags",
			table: "users",
			cfg:   relation.Config{CommentTags: map[string]string{"route": "a'b", "app": "demo", "skip": ""}},
			sql:   `select "d0"."id" as "id", "d0"."name" as "name" from "users" as "d0" /*app='demo',route='a\'b'*/`,
		},
		{
			name:  "nested comment",
			table: "users",
			cfg: relation.Config{With: []relation.With{{
				Name:  "posts",
				Query: &relation.Config{Comment: "inner"},
			}}},
			sql: `select "d0"."id" as "id", "d0"."name" as "name", "posts"."r" as "posts" from "users" as "d0" left join lateral(select coalesce(json_agg(row_to_json("t".*)), '[]') as "r" from (select "d1"."id" as "id", "d1"."title" as "title", "d1"."author_id" as "author_id" from "posts" as "d1" where "d0"."id" = "d1"."author_id" /*inner*/) as "t") as "posts" on true`,
		},
		{
			name:  "extra",
			table: "users",
			cfg: relation.Config{Extras: []relation.Extra{{
				Key:  "nlen",
				Expr: sql.Raw(`length("users"."name")`),
			}}},
			sql: `select "d0"."id" as "id", "d0"."name" as "name", (length("users"."name")) as "nlen" from "users" as "d0"`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := mustRQB(t, s, tc.table, tc.cfg, relation.ModeMany)
			if got.SQL != tc.sql {
				t.Fatalf("sql\n%s\nwant\n%s", got.SQL, tc.sql)
			}
		})
	}

	members := throughSchema(t)
	throughCases := []struct {
		name string
		cfg  relation.Config
		sql  string
	}{
		{
			name: "with",
			cfg:  relation.Config{With: []relation.With{{Name: "groups"}}},
			sql:  `select "d0"."id" as "id", "d0"."name" as "name", "groups"."r" as "groups" from "users" as "d0" left join lateral(select coalesce(json_agg(row_to_json("t".*)), '[]') as "r" from (select "d1"."id" as "id", "d1"."name" as "name" from "groups" as "d1" inner join "members" as "tr0" on "tr0"."group_id" = "d1"."id" where "d0"."id" = "tr0"."user_id") as "t") as "groups" on true`,
		},
		{
			name: "exists",
			cfg:  relation.Config{Where: relation.Exists("groups")},
			sql:  `select "d0"."id" as "id", "d0"."name" as "name" from "users" as "d0" where exists (select * from "groups" as "f0" inner join "members" as "ft0" on "ft0"."group_id" = "f0"."id" where "d0"."id" = "ft0"."user_id")`,
		},
		{
			name: "count",
			cfg:  relation.Config{Order: relation.CountOrder("groups", "desc")},
			sql:  `select "d0"."id" as "id", "d0"."name" as "name" from "users" as "d0" order by (select count(*) from "groups" as "o0" inner join "members" as "ot0" on "ot0"."group_id" = "o0"."id" where "d0"."id" = "ot0"."user_id") desc`,
		},
	}
	for _, tc := range throughCases {
		t.Run("through "+tc.name, func(t *testing.T) {
			got := mustRQB(t, members, "users", tc.cfg, relation.ModeMany)
			if got.SQL != tc.sql {
				t.Fatalf("sql\n%s\nwant\n%s", got.SQL, tc.sql)
			}
		})
	}
	inferred := mustRQB(t, inferredThroughSchema(t), "users", relation.Config{With: []relation.With{{Name: "groups"}}}, relation.ModeMany)
	if inferred.SQL != throughCases[0].sql {
		t.Fatalf("inferred through\n%s", inferred.SQL)
	}

	casted := mustRQB(t, castSchema(t), "users", relation.Config{With: []relation.With{{Name: "posts"}}}, relation.ModeMany)
	wantCast := `select "d0"."id" as "id", "d0"."amount" as "amount", "posts"."r" as "posts" from "users" as "d0" left join lateral(select coalesce(json_agg(row_to_json("t".*)), '[]') as "r" from (select "d1"."id" as "id", "d1"."author_id" as "author_id", "d1"."amount"::text as "amount", "d1"."at"::text as "at", "d1"."n"::text as "n", "d1"."payload" as "payload", "d1"."qty" as "qty" from "posts" as "d1" where "d0"."id" = "d1"."author_id") as "t") as "posts" on true`
	if casted.SQL != wantCast {
		t.Fatalf("cast\n%s\nwant\n%s", casted.SQL, wantCast)
	}
	_, err = pg.BuildRelationalQuery(badCastSchema(t), "users", relation.Config{With: []relation.With{{Name: "posts"}}}, relation.ModeMany)
	if err == nil || !strings.Contains(err.Error(), "JSON cast") {
		t.Fatalf("bad cast: %v", err)
	}
}

func throughSchema(t *testing.T) *relation.Schema {
	t.Helper()
	s, err := relation.Define([]relation.Table{
		{
			Name:    "users",
			Columns: []relation.Column{{Name: "id"}, {Name: "name"}},
			Relations: []relation.Relation{
				relation.ManyThrough("groups", "groups", "id", "id", "members", "user_id", "group_id"),
			},
		},
		{Name: "groups", Columns: []relation.Column{{Name: "id"}, {Name: "name"}}},
		{Name: "members", Columns: []relation.Column{{Name: "user_id"}, {Name: "group_id"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func inferredThroughSchema(t *testing.T) *relation.Schema {
	t.Helper()
	s, err := relation.Define([]relation.Table{
		{
			Name:      "users",
			Columns:   []relation.Column{{Name: "id"}, {Name: "name"}},
			Relations: []relation.Relation{relation.ManyOf("groups", "groups")},
		},
		{
			Name:    "groups",
			Columns: []relation.Column{{Name: "id"}, {Name: "name"}},
			Relations: []relation.Relation{
				relation.ManyThrough("users", "users", "id", "id", "members", "group_id", "user_id"),
			},
		},
		{Name: "members", Columns: []relation.Column{{Name: "user_id"}, {Name: "group_id"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func castSchema(t *testing.T) *relation.Schema {
	t.Helper()
	s, err := relation.Define([]relation.Table{
		{
			Name: "users",
			Columns: []relation.Column{
				{Name: "id"},
				{Name: "amount", CastInJSON: "text"},
			},
			Relations: []relation.Relation{relation.Many("posts", "posts", "id", "author_id")},
		},
		{
			Name: "posts",
			Columns: []relation.Column{
				{Name: "id"},
				{Name: "author_id"},
				{Name: "amount", CastInJSON: "text"},
				{Name: "at", CastInJSON: "text"},
				{Name: "n", CastInJSON: "text"},
				{Name: "payload"},
				{Name: "qty"},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func badCastSchema(t *testing.T) *relation.Schema {
	t.Helper()
	s, err := relation.Define([]relation.Table{
		{
			Name:      "users",
			Columns:   []relation.Column{{Name: "id"}},
			Relations: []relation.Relation{relation.Many("posts", "posts", "id", "author_id")},
		},
		{
			Name: "posts",
			Columns: []relation.Column{
				{Name: "id"},
				{Name: "author_id"},
				{Name: "amount", CastInJSON: "text;select"},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func rqbSchema(t *testing.T) *relation.Schema {
	t.Helper()
	s, err := relation.Define([]relation.Table{
		{
			Name:    "users",
			Columns: []relation.Column{{Name: "id"}, {Name: "name"}},
			Relations: []relation.Relation{
				relation.Many("posts", "posts", "id", "author_id"),
			},
		},
		{
			Name:    "posts",
			Columns: []relation.Column{{Name: "id"}, {Name: "title"}, {Name: "author_id"}},
			Relations: []relation.Relation{
				relation.One("author", "users", "author_id", "id"),
				relation.Many("comments", "comments", "id", "post_id"),
			},
		},
		{
			Name:    "comments",
			Columns: []relation.Column{{Name: "id"}, {Name: "body"}, {Name: "post_id"}},
			Relations: []relation.Relation{
				relation.One("post", "posts", "post_id", "id"),
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func mustRQB(t *testing.T, s *relation.Schema, table string, cfg relation.Config, mode string) sql.Query {
	t.Helper()
	built, err := pg.BuildRelationalQuery(s, table, cfg, mode)
	if err != nil {
		t.Fatal(err)
	}
	got, err := (pg.Dialect{}).SQLToQuery(built.Query)
	if err != nil {
		t.Fatal(err)
	}
	return got
}
