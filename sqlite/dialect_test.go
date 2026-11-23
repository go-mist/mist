package sqlite_test

import (
	"strings"
	"testing"

	"github.com/xbmlz/mist/internal/schemafixture"
	"github.com/xbmlz/mist/op"
	"github.com/xbmlz/mist/sqlite"
)

func TestEscape(t *testing.T) {
	d := sqlite.Dialect{}
	if got := d.EscapeName(`we"ird`); got != `"we""ird"` {
		t.Fatal(got)
	}
	if got := d.EscapeParam(0, "x"); got != "?" {
		t.Fatal(got)
	}
	if got := d.EscapeParam(3, nil); got != "?" {
		t.Fatal(got)
	}
	if got := d.EscapeString("O'Reilly"); got != "'O''Reilly'" {
		t.Fatal(got)
	}
}

func TestSelectPlaceholder(t *testing.T) {
	u := schemafixture.Users
	got, err := sqlite.Select(u.ID, u.Name).From(u).Where(op.Eq(u.Name, "alice")).SQL(sqlite.Dialect{})
	if err != nil {
		t.Fatal(err)
	}
	want := `select "users"."id", "users"."name" from "users" where "users"."name" = ?`
	if got.SQL != want {
		t.Fatalf("sql\n got %s\nwant %s", got.SQL, want)
	}
	if len(got.Params) != 1 || got.Params[0] != "alice" {
		t.Fatalf("%#v", got.Params)
	}
}

func TestInsertOrIgnore(t *testing.T) {
	u := schemafixture.Users
	got, err := sqlite.Insert(u).Columns(u.ID, u.Name).Values(int64(1), "ada").Ignore().SQL(sqlite.Dialect{})
	if err != nil {
		t.Fatal(err)
	}
	want := `insert or ignore into "users" ("id", "name") values (?, ?)`
	if got.SQL != want {
		t.Fatalf("sql\n got %s\nwant %s", got.SQL, want)
	}
}

func TestOnConflict(t *testing.T) {
	u := schemafixture.Users
	got, err := sqlite.Insert(u).Columns(u.ID, u.Name).Values(int64(1), "ada").
		OnConflictDoUpdate(u.ID).Set(u.Name, sqlite.Excluded(u.Name)).SQL(sqlite.Dialect{})
	if err != nil {
		t.Fatal(err)
	}
	want := `insert into "users" ("id", "name") values (?, ?) on conflict ("id") do update set "name" = excluded."name"`
	if got.SQL != want {
		t.Fatalf("sql\n got %s\nwant %s", got.SQL, want)
	}
}

func TestILikeRewritten(t *testing.T) {
	u := schemafixture.Users
	got, err := sqlite.Select(u.ID).From(u).Where(op.ILike(u.Name, "%Ali%")).SQL(sqlite.Dialect{})
	if err != nil {
		t.Fatal(err)
	}
	want := `select "users"."id" from "users" where lower("users"."name") like lower(?)`
	if got.SQL != want {
		t.Fatalf("sql\n got %s\nwant %s", got.SQL, want)
	}
}

func TestUnionSQL(t *testing.T) {
	u := schemafixture.Users
	got, err := sqlite.Select(u.Name).From(u).Where(op.Eq(u.Name, "alice")).
		Union(sqlite.Select(u.Name).From(u).Where(op.Eq(u.Name, "bob"))).
		SQL(sqlite.Dialect{})
	if err != nil {
		t.Fatal(err)
	}
	want := `select "users"."name" from "users" where "users"."name" = ? union select "users"."name" from "users" where "users"."name" = ?`
	if got.SQL != want {
		t.Fatalf("sql\n got %s\nwant %s", got.SQL, want)
	}
}

func TestDialectLimits(t *testing.T) {
	u := schemafixture.Users
	p := schemafixture.Posts
	cases := []struct {
		name string
		want string
		run  func() error
	}{
		{"distinct on", "distinct on", func() error {
			_, err := sqlite.Select(u.ID, u.Name).From(u).DistinctOn(u.ID).SQL(sqlite.Dialect{})
			return err
		}},
		{"for update", "for update", func() error {
			_, err := sqlite.Select(u.ID).From(u).ForUpdate().SQL(sqlite.Dialect{})
			return err
		}},
		{"duplicate key", "on duplicate key", func() error {
			_, err := sqlite.Insert(u).Columns(u.ID).Values(int64(1)).OnDuplicateKeyUpdate().Set(u.Name, "a").SQL(sqlite.Dialect{})
			return err
		}},
		{"ignore with conflict", "insert or ignore", func() error {
			_, err := sqlite.Insert(u).Columns(u.ID).Values(int64(1)).Ignore().OnConflictDoNothing(u.ID).SQL(sqlite.Dialect{})
			return err
		}},
		{"delete using", "using", func() error {
			_, err := sqlite.Delete(u).Using(p).SQL(sqlite.Dialect{})
			return err
		}},
		{"update limit", "order by or limit", func() error {
			_, err := sqlite.Update(u).Set(u.Name, "a").OrderBy(op.Asc(u.ID)).Limit(1).SQL(sqlite.Dialect{})
			return err
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.run()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatal(err)
			}
		})
	}
}
