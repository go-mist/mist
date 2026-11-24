package mssql_test

import (
	"strings"
	"testing"

	"github.com/go-mist/mist/dialect/mssql"
	"github.com/go-mist/mist/internal/schemafixture"
	"github.com/go-mist/mist/op"
	"github.com/go-mist/mist/relation"
	"github.com/go-mist/mist/sql"
)

func TestEscape(t *testing.T) {
	d := mssql.Dialect{}
	if got := d.EscapeName("na]me"); got != "[na]]me]" {
		t.Fatal(got)
	}
	if got := d.EscapeParam(0, nil); got != "@p1" {
		t.Fatal(got)
	}
	if got := d.EscapeParam(11, nil); got != "@p12" {
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
		{"select top", `select top (10) [users].[id], [users].[name] from [users] where [users].[name] = @p1 order by [users].[id] asc`, func() (sql.Query, error) {
			return mssql.Select(u.ID, u.Name).From(u).Top(10).Where(op.Eq(u.Name, "alice")).OrderBy(op.Asc(u.ID)).SQL(mssql.Dialect{})
		}},
		{"select distinct top", `select distinct top (1) [users].[name] from [users]`, func() (sql.Query, error) {
			return mssql.Select(u.Name).From(u).Distinct().Top(1).SQL(mssql.Dialect{})
		}},
		{"select where fetch", `select [users].[id], [users].[name] from [users] where [users].[name] = @p1 order by [users].[id] asc offset 0 rows fetch next @p2 rows only`, func() (sql.Query, error) {
			return mssql.Select(u.ID, u.Name).From(u).Where(op.Eq(u.Name, "alice")).OrderBy(op.Asc(u.ID)).Limit(10).SQL(mssql.Dialect{})
		}},
		{"insert output", `insert into [users] ([name]) output inserted.[id], inserted.[name] values (@p1)`, func() (sql.Query, error) {
			return mssql.Insert(u).Columns(u.Name).Values("ann").Output(u.ID, u.Name).SQL(mssql.Dialect{})
		}},
		{"insert", `insert into [users] ([name]) values (@p1)`, func() (sql.Query, error) {
			return mssql.Insert(u).Columns(u.Name).Values("ann").SQL(mssql.Dialect{})
		}},
		{"insert two rows", `insert into [users] ([name]) values (@p1), (@p2)`, func() (sql.Query, error) {
			return mssql.Insert(u).Columns(u.Name).Values("ann").Values("bob").SQL(mssql.Dialect{})
		}},
		{"update output", `update [users] set [name] = @p1 output inserted.[name], deleted.[name] where [users].[id] = @p2`, func() (sql.Query, error) {
			return mssql.Update(u).Set(u.Name, "ann").OutputInserted(u.Name).OutputDeleted(u.Name).Where(op.Eq(u.ID, int64(1))).SQL(mssql.Dialect{})
		}},
		{"update", `update [users] set [name] = @p1 where [users].[id] = @p2`, func() (sql.Query, error) {
			return mssql.Update(u).Set(u.Name, "ann").Where(op.Eq(u.ID, int64(1))).SQL(mssql.Dialect{})
		}},
		{"delete output", `delete from [users] output deleted.[id] where [users].[id] = @p1`, func() (sql.Query, error) {
			return mssql.Delete(u).Output(u.ID).Where(op.Eq(u.ID, int64(1))).SQL(mssql.Dialect{})
		}},
		{"delete", `delete from [users] where [users].[id] = @p1`, func() (sql.Query, error) {
			return mssql.Delete(u).Where(op.Eq(u.ID, int64(1))).SQL(mssql.Dialect{})
		}},
		{"join group", `select [users].[id] from [users] inner join [posts] on [posts].[author_id] = [users].[id] group by [users].[id] having [users].[id] > @p1 order by [users].[id] asc`, func() (sql.Query, error) {
			return mssql.Select(u.ID).From(u).InnerJoin(schemafixture.Posts, op.Eq(schemafixture.Posts.AuthorID, u.ID)).GroupBy(u.ID).Having(op.Gt(u.ID, int64(1))).OrderBy(op.Asc(u.ID)).SQL(mssql.Dialect{})
		}},
		{"distinct", `select distinct [users].[name] from [users]`, func() (sql.Query, error) {
			return mssql.Select(u.Name).From(u).Distinct().SQL(mssql.Dialect{})
		}},
		{"union", `(select [users].[id] from [users]) union (select [users].[id] from [users]) order by [id] asc offset 0 rows fetch next @p1 rows only`, func() (sql.Query, error) {
			return mssql.Select(u.ID).From(u).Union(mssql.Select(u.ID).From(u)).OrderBy(op.Asc(u.ID)).Limit(2).SQL(mssql.Dialect{})
		}},
		{"full join", `select [users].[id] from [users] full join [posts] on [posts].[author_id] = [users].[id]`, func() (sql.Query, error) {
			return mssql.Select(u.ID).From(u).FullJoin(schemafixture.Posts, op.Eq(schemafixture.Posts.AuthorID, u.ID)).SQL(mssql.Dialect{})
		}},
		{"insert select", `insert into [users] ([name]) select [posts].[title] from [posts]`, func() (sql.Query, error) {
			return mssql.Insert(u).Columns(u.Name).Select(mssql.Select(schemafixture.Posts.Title).From(schemafixture.Posts)).SQL(mssql.Dialect{})
		}},
		{"ilike", `select [users].[id] from [users] where lower([users].[name]) like lower(@p1)`, func() (sql.Query, error) {
			return mssql.Select(u.ID).From(u).Where(op.ILike(u.Name, "%a%")).SQL(mssql.Dialect{})
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

func TestRelationalSQL(t *testing.T) {
	type post struct {
		ID       int64
		Title    string
		AuthorID int64
	}
	type user struct {
		ID    int64
		Name  string
		Posts []post
	}
	built, err := relation.Define([]relation.Table{
		{
			Name: "users", SQLName: "users",
			New: func() any { return &user{} },
			Columns: []relation.Column{
				{Name: "id", Ptr: func(row any) any { return &row.(*user).ID }},
				{Name: "name", Ptr: func(row any) any { return &row.(*user).Name }},
			},
			Relations: []relation.Relation{relation.Many("posts", "posts", "id", "author_id")},
		},
		{
			Name: "posts", SQLName: "posts",
			New: func() any { return &post{} },
			Columns: []relation.Column{
				{Name: "id", Ptr: func(row any) any { return &row.(*post).ID }},
				{Name: "title", Ptr: func(row any) any { return &row.(*post).Title }},
				{Name: "author_id", Ptr: func(row any) any { return &row.(*post).AuthorID }},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	q, err := mssql.BuildRelationalQuery(built, "users", relation.Config{
		Order: relation.Asc("id"),
		With:  []relation.With{{Name: "posts"}},
	}, relation.ModeMany)
	if err != nil {
		t.Fatal(err)
	}
	rendered, err := mssql.Dialect{}.SQLToQuery(q.Query)
	if err != nil {
		t.Fatal(err)
	}
	for _, piece := range []string{
		"outer apply (select coalesce((",
		"for json path",
		"from [posts] as [d1]",
		"order by [d0].[id] asc",
	} {
		if !strings.Contains(rendered.SQL, piece) {
			t.Fatalf("missing %s\n%s", piece, rendered.SQL)
		}
	}
}

func TestUnsupported(t *testing.T) {
	u := schemafixture.Users
	if _, err := mssql.Select(u.ID).From(u).Limit(1).SQL(mssql.Dialect{}); err == nil || !strings.Contains(err.Error(), "order by") {
		t.Fatal(err)
	}
	if _, err := mssql.Select(u.ID).From(u).ForUpdate().SQL(mssql.Dialect{}); err == nil || !strings.Contains(err.Error(), "for update") {
		t.Fatal(err)
	}
	if _, err := mssql.Insert(u).Columns(u.Name).Values("ann").Returning(u.ID).SQL(mssql.Dialect{}); err == nil || !strings.Contains(err.Error(), "returning") {
		t.Fatal(err)
	}
	if _, err := mssql.Select(u.ID).From(u).Top(1).Limit(1).OrderBy(op.Asc(u.ID)).SQL(mssql.Dialect{}); err == nil || !strings.Contains(err.Error(), "top cannot") {
		t.Fatal(err)
	}
	if _, err := mssql.Select(u.ID).From(u).Union(mssql.Select(u.ID).From(u)).Top(1).SQL(mssql.Dialect{}); err == nil || !strings.Contains(err.Error(), "top cannot") {
		t.Fatal(err)
	}
	if _, err := mssql.Insert(u).Columns(u.Name).Values("ann").OnConflictDoNothing().SQL(mssql.Dialect{}); err == nil || !strings.Contains(err.Error(), "on conflict") {
		t.Fatal(err)
	}
}

func TestOpenNamesTheFailure(t *testing.T) {
	_, err := mssql.Open(t.Context(), "")
	if err == nil || err.Error() != "mssql: missing connection string" {
		t.Fatal(err)
	}
}
