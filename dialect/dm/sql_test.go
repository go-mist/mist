package dm_test

import (
	"strings"
	"testing"

	"github.com/go-mist/mist/dialect/dm"
	"github.com/go-mist/mist/internal/schemafixture"
	"github.com/go-mist/mist/op"
	"github.com/go-mist/mist/query"
	"github.com/go-mist/mist/sql"
)

func TestEscape(t *testing.T) {
	d := dm.Dialect{}
	if got := d.EscapeName(`na"me`); got != `"na""me"` {
		t.Fatal(got)
	}
	if got := d.EscapeParam(0, nil); got != "?" {
		t.Fatal(got)
	}
	if got := d.EscapeParam(11, nil); got != "?" {
		t.Fatal(got)
	}
	if got := d.EscapeString("a'b"); got != "'a''b'" {
		t.Fatal(got)
	}
}

func TestBasicSQL(t *testing.T) {
	u := schemafixture.Users
	cases := []struct {
		name string
		sql  string
		run  func() (sql.Query, error)
	}{
		{"select where limit", `select "users"."id", "users"."name" from "users" where "users"."name" = ? order by "users"."id" asc limit ?`, func() (sql.Query, error) {
			return dm.Select(u.ID, u.Name).From(u).Where(op.Eq(u.Name, "alice")).OrderBy(op.Asc(u.ID)).Limit(10).SQL(dm.Dialect{})
		}},
		{"offset only", `select "users"."id" from "users" limit 2147483647 offset ?`, func() (sql.Query, error) {
			return dm.Select(u.ID).From(u).Offset(5).SQL(dm.Dialect{})
		}},
		{"insert", `insert into "users" ("name") values (?)`, func() (sql.Query, error) {
			return dm.Insert(u).Columns(u.Name).Values("ann").SQL(dm.Dialect{})
		}},
		{"insert two rows", `insert into "users" ("name") values (?), (?)`, func() (sql.Query, error) {
			return dm.Insert(u).Columns(u.Name).Values("ann").Values("bob").SQL(dm.Dialect{})
		}},
		{"update", `update "users" set "name" = ? where "users"."id" = ?`, func() (sql.Query, error) {
			return dm.Update(u).Set(u.Name, "ann").Where(op.Eq(u.ID, int64(1))).SQL(dm.Dialect{})
		}},
		{"delete", `delete from "users" where "users"."id" = ?`, func() (sql.Query, error) {
			return dm.Delete(u).Where(op.Eq(u.ID, int64(1))).SQL(dm.Dialect{})
		}},
		{"join group", `select "users"."id" from "users" inner join "posts" on "posts"."author_id" = "users"."id" group by "users"."id" having "users"."id" > ? order by "users"."id" asc`, func() (sql.Query, error) {
			return dm.Select(u.ID).From(u).InnerJoin(schemafixture.Posts, op.Eq(schemafixture.Posts.AuthorID, u.ID)).GroupBy(u.ID).Having(op.Gt(u.ID, int64(1))).OrderBy(op.Asc(u.ID)).SQL(dm.Dialect{})
		}},
		{"distinct", `select distinct "users"."name" from "users"`, func() (sql.Query, error) {
			return dm.Select(u.Name).From(u).Distinct().SQL(dm.Dialect{})
		}},
		{"union", `(select "users"."id" from "users") union (select "users"."id" from "users") order by "id" asc limit ?`, func() (sql.Query, error) {
			return dm.Select(u.ID).From(u).Union(dm.Select(u.ID).From(u)).OrderBy(op.Asc(u.ID)).Limit(2).SQL(dm.Dialect{})
		}},
		{"for update", `select "users"."id" from "users" where "users"."id" = ? for update nowait`, func() (sql.Query, error) {
			return dm.Select(u.ID).From(u).Where(op.Eq(u.ID, int64(1))).ForUpdate(query.RowLock{NoWait: true}).SQL(dm.Dialect{})
		}},
		{"insert select", `insert into "users" ("name") select "posts"."title" from "posts"`, func() (sql.Query, error) {
			return dm.Insert(u).Columns(u.Name).Select(dm.Select(schemafixture.Posts.Title).From(schemafixture.Posts)).SQL(dm.Dialect{})
		}},
		{"ilike", `select "users"."id" from "users" where lower("users"."name") like lower(?)`, func() (sql.Query, error) {
			return dm.Select(u.ID).From(u).Where(op.ILike(u.Name, "%a%")).SQL(dm.Dialect{})
		}},
		{"with", `with "tree" as (select "users"."id" from "users") select "users"."id" from "users"`, func() (sql.Query, error) {
			tree := dm.Recursive("tree", dm.Select(u.ID).From(u))
			return dm.Select(u.ID).From(u).With(tree).SQL(dm.Dialect{})
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.run()
			if err != nil {
				t.Fatal(err)
			}
			if got.SQL != tc.sql {
				t.Fatalf("got %s", got.SQL)
			}
		})
	}
}

func TestUnsupported(t *testing.T) {
	u := schemafixture.Users
	if _, err := dm.Insert(u).Columns(u.Name).Values("ann").Returning(u.ID).SQL(dm.Dialect{}); err == nil || !strings.Contains(err.Error(), "returning") {
		t.Fatal(err)
	}
	if _, err := dm.Select(u.ID).From(u).Top(1).SQL(dm.Dialect{}); err == nil || !strings.Contains(err.Error(), "top") {
		t.Fatal(err)
	}
	if _, err := dm.Insert(u).Columns(u.Name).Values("ann").Output(u.ID).SQL(dm.Dialect{}); err == nil || !strings.Contains(err.Error(), "output") {
		t.Fatal(err)
	}
	if _, err := dm.Select(u.ID).From(u).DistinctOn(u.ID).SQL(dm.Dialect{}); err == nil || !strings.Contains(err.Error(), "distinct on") {
		t.Fatal(err)
	}
	if _, err := dm.Select(u.ID).From(u).ForShare().SQL(dm.Dialect{}); err == nil || !strings.Contains(err.Error(), "for share") {
		t.Fatal(err)
	}
	if _, err := dm.Insert(u).Columns(u.ID).Values(int64(1)).OnConflictDoNothing(u.ID).SQL(dm.Dialect{}); err == nil || !strings.Contains(err.Error(), "on conflict") {
		t.Fatal(err)
	}
	if _, err := dm.Update(u).Set(u.Name, "ann").Limit(1).SQL(dm.Dialect{}); err == nil || !strings.Contains(err.Error(), "set and where") {
		t.Fatal(err)
	}
}

func TestColumnType(t *testing.T) {
	got, err := dm.ColumnType("int64", true, 0)
	if err != nil || got != "BIGINT IDENTITY(1,1)" {
		t.Fatalf("%s %v", got, err)
	}
	got, err = dm.ColumnType("string", false, 0)
	if err != nil || got != "CLOB" {
		t.Fatalf("%s %v", got, err)
	}
	got, err = dm.ColumnType("bool", false, 0)
	if err != nil || got != "BIT" {
		t.Fatalf("%s %v", got, err)
	}
	if _, err := dm.ColumnType("float64", false, 0); err == nil {
		t.Fatal("expected an error")
	}
}

func TestOpenNamesTheFailure(t *testing.T) {
	_, err := dm.Open(t.Context(), "")
	if err == nil || err.Error() != "dm: missing connection string" {
		t.Fatal(err)
	}
}
