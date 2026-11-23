package mysql_test

import (
	"strings"
	"testing"

	"github.com/xbmlz/mist/mysql"
	"github.com/xbmlz/mist/relation"
	"github.com/xbmlz/mist/sql"
)

func TestRelationalSQL(t *testing.T) {
	s := rqbSchema(t)
	postsJSON := `coalesce(json_arrayagg(json_object('id', ` + "`id`" + `, 'title', ` + "`title`" + `, 'author_id', ` + "`author_id`" + `)), json_array())`
	postsJoin := ` left join lateral(select ` + postsJSON + ` as ` + "`r`" + ` from (select ` + "`d1`" + `.` + "`id`" + ` as ` + "`id`" + `, ` + "`d1`" + `.` + "`title`" + ` as ` + "`title`" + `, ` + "`d1`" + `.` + "`author_id`" + ` as ` + "`author_id`" + ` from ` + "`posts`" + ` as ` + "`d1`" + ` where ` + "`d0`" + `.` + "`id`" + ` = ` + "`d1`" + `.` + "`author_id`" + `) as ` + "`t`" + `) as ` + "`posts`" + ` on true`
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
			sql: "select `d0`.`id` as `id`, `d0`.`name` as `name`, `posts`.`r` as `posts` from `users` as `d0` left join lateral(select coalesce(json_arrayagg(json_object('id', `id`, 'title', `title`, 'author_id', `author_id`, 'comments', `comments`)), json_array()) as `r` from (select `d1`.`id` as `id`, `d1`.`title` as `title`, `d1`.`author_id` as `author_id`, `comments`.`r` as `comments` from `posts` as `d1` left join lateral(select coalesce(json_arrayagg(json_object('id', `id`, 'body', `body`, 'post_id', `post_id`)), json_array()) as `r` from (select `d2`.`id` as `id`, `d2`.`body` as `body`, `d2`.`post_id` as `post_id` from `comments` as `d2` where `d1`.`id` = `d2`.`post_id`) as `t`) as `comments` on true where `d0`.`id` = `d1`.`author_id`) as `t`) as `posts` on true",
		},
		{
			name:   "one",
			table:  "posts",
			mode:   relation.ModeMany,
			cfg:    relation.Config{With: []relation.With{{Name: "author"}}},
			sql:    "select `d0`.`id` as `id`, `d0`.`title` as `title`, `d0`.`author_id` as `author_id`, `author`.`r` as `author` from `posts` as `d0` left join lateral(select json_object('id', `id`, 'name', `name`) as `r` from (select `d1`.`id` as `id`, `d1`.`name` as `name` from `users` as `d1` where `d0`.`author_id` = `d1`.`id` limit ?) as `t`) as `author` on true",
			params: []any{1},
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
			sql:    "select `d0`.`id` as `id`, `d0`.`name` as `name`, `posts`.`r` as `posts` from `users` as `d0` left join lateral(select coalesce(json_arrayagg(json_object('id', `id`, 'title', `title`, 'author_id', `author_id`)), json_array()) as `r` from (select `d1`.`id` as `id`, `d1`.`title` as `title`, `d1`.`author_id` as `author_id`, row_number() over (order by `d1`.`id` desc) from `posts` as `d1` where `d0`.`id` = `d1`.`author_id` order by `d1`.`id` desc limit ? offset ?) as `t`) as `posts` on true",
			params: []any{2, 1},
		},
		{
			name:  "root page",
			table: "users",
			mode:  relation.ModeMany,
			cfg: relation.Config{
				With:   []relation.With{{Name: "posts"}},
				Order:  relation.Asc("id"),
				Limit:  relation.Int(10),
				Offset: relation.Int(5),
			},
			sql:    "select `d0`.`id` as `id`, `d0`.`name` as `name`, `posts`.`r` as `posts` from (select `d0`.* from `users` as `d0` order by `d0`.`id` asc limit ? offset ?) as `d0`" + postsJoin + " order by `d0`.`id` asc",
			params: []any{10, 5},
		},
		{
			name:  "relation filter stays outside",
			table: "users",
			mode:  relation.ModeMany,
			cfg: relation.Config{
				With:  []relation.With{{Name: "posts"}},
				Where: relation.Exists("posts"),
			},
			sql: "select `d0`.`id` as `id`, `d0`.`name` as `name`, `posts`.`r` as `posts` from `users` as `d0`" + postsJoin + " where exists (select * from `posts` as `f0` where `d0`.`id` = `f0`.`author_id`)",
		},
		{
			name:   "ilike",
			table:  "users",
			mode:   relation.ModeMany,
			cfg:    relation.Config{Where: relation.ILike("name", "a%")},
			sql:    "select `d0`.`id` as `id`, `d0`.`name` as `name` from `users` as `d0` where lower(`d0`.`name`) like lower(?)",
			params: []any{"a%"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := mustRQB(t, s, tc.table, tc.cfg, tc.mode)
			if got.SQL != tc.sql {
				t.Fatalf("sql\n%s\nwant\n%s", got.SQL, tc.sql)
			}
			if len(got.Params) != len(tc.params) {
				t.Fatalf("params %#v", got.Params)
			}
			for i := range tc.params {
				if got.Params[i] != tc.params[i] {
					t.Fatalf("param %d = %#v", i, got.Params[i])
				}
			}
		})
	}
}

func TestRelationalThroughSQL(t *testing.T) {
	members := throughSchema(t)
	withSQL := "select `d0`.`id` as `id`, `d0`.`name` as `name`, `groups`.`r` as `groups` from `users` as `d0` left join lateral(select coalesce(json_arrayagg(json_object('id', `id`, 'name', `name`)), json_array()) as `r` from (select `d1`.`id` as `id`, `d1`.`name` as `name` from `groups` as `d1` inner join `members` as `tr0` on `tr0`.`group_id` = `d1`.`id` where `d0`.`id` = `tr0`.`user_id`) as `t`) as `groups` on true"
	cases := []struct {
		name string
		cfg  relation.Config
		sql  string
	}{
		{name: "with", cfg: relation.Config{With: []relation.With{{Name: "groups"}}}, sql: withSQL},
		{
			name: "exists",
			cfg:  relation.Config{Where: relation.Exists("groups")},
			sql:  "select `d0`.`id` as `id`, `d0`.`name` as `name` from `users` as `d0` where exists (select * from `groups` as `f0` inner join `members` as `ft0` on `ft0`.`group_id` = `f0`.`id` where `d0`.`id` = `ft0`.`user_id`)",
		},
		{
			name: "count",
			cfg:  relation.Config{Order: relation.CountOrder("groups", "desc")},
			sql:  "select `d0`.`id` as `id`, `d0`.`name` as `name` from `users` as `d0` order by (select count(*) from `groups` as `o0` inner join `members` as `ot0` on `ot0`.`group_id` = `o0`.`id` where `d0`.`id` = `ot0`.`user_id`) desc",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := mustRQB(t, members, "users", tc.cfg, relation.ModeMany)
			if got.SQL != tc.sql {
				t.Fatalf("sql\n%s\nwant\n%s", got.SQL, tc.sql)
			}
		})
	}
	inferred := mustRQB(t, inferredThroughSchema(t), "users", relation.Config{With: []relation.With{{Name: "groups"}}}, relation.ModeMany)
	if inferred.SQL != withSQL {
		t.Fatalf("inferred through\n%s", inferred.SQL)
	}
}

func TestRelationalJSONCastSQL(t *testing.T) {
	s, err := relation.Define([]relation.Table{
		{
			Name:      "parents",
			Columns:   []relation.Column{{Name: "id"}},
			Relations: []relation.Relation{relation.Many("kids", "kids", "id", "parent_id")},
		},
		{
			Name: "kids",
			Columns: []relation.Column{
				{Name: "id"},
				{Name: "parent_id"},
				{Name: "amount", CastInJSON: "text"},
				{Name: "n", CastInJSON: "text"},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := mustRQB(t, s, "parents", relation.Config{With: []relation.With{{Name: "kids"}}}, relation.ModeMany)
	want := "select `d0`.`id` as `id`, `kids`.`r` as `kids` from `parents` as `d0` left join lateral(select coalesce(json_arrayagg(json_object('id', `id`, 'parent_id', `parent_id`, 'amount', `amount`, 'n', `n`)), json_array()) as `r` from (select `d1`.`id` as `id`, `d1`.`parent_id` as `parent_id`, cast(`d1`.`amount` as char) as `amount`, cast(`d1`.`n` as char) as `n` from `kids` as `d1` where `d0`.`id` = `d1`.`parent_id`) as `t`) as `kids` on true"
	if got.SQL != want {
		t.Fatalf("sql\n%s\nwant\n%s", got.SQL, want)
	}
	bad, err := relation.Define([]relation.Table{
		{
			Name:      "parents",
			Columns:   []relation.Column{{Name: "id"}},
			Relations: []relation.Relation{relation.Many("kids", "kids", "id", "parent_id")},
		},
		{Name: "kids", Columns: []relation.Column{{Name: "id"}, {Name: "parent_id"}, {Name: "amount", CastInJSON: "numeric"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = mysql.BuildRelationalQuery(bad, "parents", relation.Config{With: []relation.With{{Name: "kids"}}}, relation.ModeMany)
	if err == nil || !strings.Contains(err.Error(), "accepts only text") {
		t.Fatal(err)
	}
}

func TestRelationalErrors(t *testing.T) {
	s := rqbSchema(t)
	_, err := mysql.BuildRelationalQuery(s, "users", relation.Config{With: []relation.With{{Name: "nope"}}}, relation.ModeMany)
	if err == nil {
		t.Fatal("missing relation")
	}
	_, err = mysql.BuildRelationalQuery(s, "users", relation.Config{Strategy: "fast"}, relation.ModeMany)
	if err == nil {
		t.Fatal("bad strategy")
	}
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
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
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

func mustRQB(t *testing.T, s *relation.Schema, table string, cfg relation.Config, mode string) sql.Query {
	t.Helper()
	built, err := mysql.BuildRelationalQuery(s, table, cfg, mode)
	if err != nil {
		t.Fatal(err)
	}
	got, err := (mysql.Dialect{}).SQLToQuery(built.Query)
	if err != nil {
		t.Fatal(err)
	}
	return got
}
