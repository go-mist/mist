package dm_test

import (
	"strings"
	"testing"

	"github.com/go-mist/mist/dialect/dm"
	"github.com/go-mist/mist/relation"
	"github.com/go-mist/mist/sql"
)

func TestRelationalSQL(t *testing.T) {
	s := rqbSchema(t)
	got := mustRQB(t, s, "users", relation.Config{With: []relation.With{{Name: "posts"}}}, relation.ModeMany)
	if strings.Contains(got.SQL, "lateral") || strings.Contains(got.SQL, "json_object") || strings.Contains(got.SQL, "json_arrayagg") {
		t.Fatalf("sql = %s", got.SQL)
	}
	if !strings.Contains(got.SQL, "listagg(") || !strings.Contains(got.SQL, `within group (order by "id")`) {
		t.Fatalf("sql = %s", got.SQL)
	}
	if !strings.Contains(got.SQL, `from "posts" as "d1" where "d0"."id" = "d1"."author_id"`) {
		t.Fatalf("sql = %s", got.SQL)
	}
	if !strings.Contains(got.SQL, `dump("id") like 'Typ=8%'`) || !strings.Contains(got.SQL, `chr(10)`) {
		t.Fatalf("sql = %s", got.SQL)
	}

	one := mustRQB(t, s, "posts", relation.Config{With: []relation.With{{Name: "author"}}}, relation.ModeMany)
	if strings.Contains(one.SQL, "listagg(") || !strings.Contains(one.SQL, "limit ?") {
		t.Fatalf("sql = %s", one.SQL)
	}
	if len(one.Params) != 1 || one.Params[0] != 1 {
		t.Fatalf("params %#v", one.Params)
	}

	page := mustRQB(t, s, "users", relation.Config{
		With:   []relation.With{{Name: "posts"}},
		Order:  relation.Asc("id"),
		Limit:  relation.Int(10),
		Offset: relation.Int(5),
	}, relation.ModeMany)
	if !strings.Contains(page.SQL, `from (select "d0".* from "users" as "d0" order by "d0"."id" asc limit ? offset ?) as "d0" order by "d0"."id" asc`) {
		t.Fatalf("sql = %s", page.SQL)
	}
	if len(page.Params) != 2 || page.Params[0] != 10 || page.Params[1] != 5 {
		t.Fatalf("params %#v", page.Params)
	}

	ordered := mustRQB(t, s, "users", relation.Config{With: []relation.With{{
		Name:  "posts",
		Query: &relation.Config{Order: relation.Desc("id"), Limit: relation.Int(2)},
	}}}, relation.ModeMany)
	if !strings.Contains(ordered.SQL, `row_number() over (order by "d1"."id" desc) as "ord"`) || !strings.Contains(ordered.SQL, `order by "ord"`) {
		t.Fatalf("sql = %s", ordered.SQL)
	}

	like := mustRQB(t, s, "users", relation.Config{Where: relation.ILike("name", "a%")}, relation.ModeMany)
	if !strings.Contains(like.SQL, `lower("d0"."name") like lower(?)`) {
		t.Fatalf("sql = %s", like.SQL)
	}
}

func TestRelationalThroughAndCast(t *testing.T) {
	members := throughSchema(t)
	got := mustRQB(t, members, "users", relation.Config{With: []relation.With{{Name: "groups"}}}, relation.ModeMany)
	if !strings.Contains(got.SQL, `inner join "members" as "tr0" on "tr0"."group_id" = "d1"."id" where "d0"."id" = "tr0"."user_id"`) {
		t.Fatalf("sql = %s", got.SQL)
	}
	exists := mustRQB(t, members, "users", relation.Config{Where: relation.Exists("groups")}, relation.ModeMany)
	if !strings.Contains(exists.SQL, `exists (select * from "groups" as "f0" inner join "members" as "ft0"`) {
		t.Fatalf("sql = %s", exists.SQL)
	}

	casted, err := relation.Define([]relation.Table{
		{
			Name:      "parents",
			Columns:   []relation.Column{{Name: "id"}},
			Relations: []relation.Relation{relation.Many("kids", "kids", "id", "parent_id")},
		},
		{Name: "kids", Columns: []relation.Column{{Name: "id"}, {Name: "parent_id"}, {Name: "amount", CastInJSON: "text"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	sqlText := mustRQB(t, casted, "parents", relation.Config{With: []relation.With{{Name: "kids"}}}, relation.ModeMany)
	if !strings.Contains(sqlText.SQL, `cast("d1"."amount" as varchar(8188))`) {
		t.Fatalf("sql = %s", sqlText.SQL)
	}
	bad, err := relation.Define([]relation.Table{
		{
			Name:      "parents",
			Columns:   []relation.Column{{Name: "id"}},
			Relations: []relation.Relation{relation.Many("kids", "kids", "id", "parent_id")},
		},
		{Name: "kids", Columns: []relation.Column{{Name: "parent_id"}, {Name: "amount", CastInJSON: "numeric"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = dm.BuildRelationalQuery(bad, "parents", relation.Config{With: []relation.With{{Name: "kids"}}}, relation.ModeMany)
	if err == nil || !strings.Contains(err.Error(), "accepts only text") {
		t.Fatal(err)
	}
}

func TestRelationalErrors(t *testing.T) {
	s := rqbSchema(t)
	if _, err := dm.BuildRelationalQuery(s, "users", relation.Config{With: []relation.With{{Name: "nope"}}}, relation.ModeMany); err == nil {
		t.Fatal("missing relation")
	}
	if _, err := dm.BuildRelationalQuery(s, "users", relation.Config{Strategy: "fast"}, relation.ModeMany); err == nil {
		t.Fatal("bad strategy")
	}
}

func rqbSchema(t *testing.T) *relation.Schema {
	t.Helper()
	s, err := relation.Define([]relation.Table{
		{
			Name:      "users",
			Columns:   []relation.Column{{Name: "id"}, {Name: "name"}},
			Relations: []relation.Relation{relation.Many("posts", "posts", "id", "author_id")},
		},
		{
			Name:    "posts",
			Columns: []relation.Column{{Name: "id"}, {Name: "title"}, {Name: "author_id"}},
			Relations: []relation.Relation{
				relation.One("author", "users", "author_id", "id"),
				relation.Many("comments", "comments", "id", "post_id"),
			},
		},
		{Name: "comments", Columns: []relation.Column{{Name: "id"}, {Name: "body"}, {Name: "post_id"}}},
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

func mustRQB(t *testing.T, s *relation.Schema, table string, cfg relation.Config, mode string) sql.Query {
	t.Helper()
	built, err := dm.BuildRelationalQuery(s, table, cfg, mode)
	if err != nil {
		t.Fatal(err)
	}
	got, err := (dm.Dialect{}).SQLToQuery(built.Query)
	if err != nil {
		t.Fatal(err)
	}
	return got
}
