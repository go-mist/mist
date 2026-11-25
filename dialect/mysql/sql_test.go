package mysql_test

import (
	"strings"
	"testing"

	"github.com/go-mist/mist/dialect/mysql"
	"github.com/go-mist/mist/internal/schemafixture"
	"github.com/go-mist/mist/op"
	"github.com/go-mist/mist/sql"
)

func TestEscape(t *testing.T) {
	d := mysql.Dialect{}
	if got := d.EscapeName("na`me"); got != "`na``me`" {
		t.Fatal(got)
	}
	if got := d.EscapeParam(0, nil); got != "?" {
		t.Fatal(got)
	}
	if got := d.EscapeString("a'b"); got != "'a''b'" {
		t.Fatal(got)
	}
}

func TestBasicSQL(t *testing.T) {
	u := schemafixture.Users
	p := schemafixture.Posts
	cases := []struct {
		name string
		sql  string
		run  func() (sql.Query, error)
	}{
		{"select where limit", "select `users`.`id`, `users`.`name` from `users` where `users`.`name` = ? order by `users`.`created_at` desc limit ?", func() (sql.Query, error) {
			return mysql.Select(u.ID, u.Name).From(u).Where(op.Eq(u.Name, "alice")).OrderBy(op.Desc(u.CreatedAt)).Limit(10).SQL(mysql.Dialect{})
		}},
		{"inner join", "select `users`.`name` from `users` inner join `posts` on `posts`.`author_id` = `users`.`id`", func() (sql.Query, error) {
			return mysql.Select(u.Name).From(u).InnerJoin(p, op.Eq(p.AuthorID, u.ID)).SQL(mysql.Dialect{})
		}},
		{"insert", "insert into `users` (`name`) values (?), (?)", func() (sql.Query, error) {
			return mysql.Insert(u).Columns(u.Name).Values("ann").Values("bob").SQL(mysql.Dialect{})
		}},
		{"update", "update `users` set `name` = ? where `users`.`id` = ?", func() (sql.Query, error) {
			return mysql.Update(u).Set(u.Name, "ann").Where(op.Eq(u.ID, int64(1))).SQL(mysql.Dialect{})
		}},
		{"delete", "delete from `users` where `users`.`id` = ?", func() (sql.Query, error) {
			return mysql.Delete(u).Where(op.Eq(u.ID, int64(1))).SQL(mysql.Dialect{})
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
	p := schemafixture.Posts
	if _, err := mysql.Select(u.ID).From(u).FullJoin(p, op.Eq(p.AuthorID, u.ID)).SQL(mysql.Dialect{}); err == nil || !strings.Contains(err.Error(), "full join") {
		t.Fatal(err)
	}
	if _, err := mysql.Select(u.ID).From(u).DistinctOn(u.ID).SQL(mysql.Dialect{}); err == nil || !strings.Contains(err.Error(), "distinct on") {
		t.Fatal(err)
	}
	if _, err := mysql.Insert(u).Columns(u.Name).Values("ann").Returning(u.ID).SQL(mysql.Dialect{}); err == nil || !strings.Contains(err.Error(), "returning") {
		t.Fatal(err)
	}
	if _, err := mysql.Select(u.ID).From(u).Top(1).SQL(mysql.Dialect{}); err == nil || !strings.Contains(err.Error(), "top") {
		t.Fatal(err)
	}
	if _, err := mysql.Insert(u).Columns(u.Name).Values("ann").Output(u.ID).SQL(mysql.Dialect{}); err == nil || !strings.Contains(err.Error(), "output") {
		t.Fatal(err)
	}
	if _, err := mysql.Insert(u).Columns(u.ID).Values(int64(1)).OnConflictDoNothing(u.ID).SQL(mysql.Dialect{}); err == nil || !strings.Contains(err.Error(), "on conflict") {
		t.Fatal(err)
	}
}
