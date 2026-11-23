package pg_test

import (
	"strings"
	"testing"

	"github.com/go-mist/mist/internal/schemafixture"
	"github.com/go-mist/mist/op"
	"github.com/go-mist/mist/pg"
	"github.com/go-mist/mist/query"
	"github.com/go-mist/mist/sql"
)

// TestSQLMatrix locks the SQL shape of the query builder. At least 40 cases, covering join, subquery,
// set operation, CTE, aggregation, conflict update, and delete using.
func TestSQLMatrix(t *testing.T) {
	u := schemafixture.Users
	p := schemafixture.Posts
	on := op.Eq(p.AuthorID, u.ID)
	left := pg.Select(u.ID, u.Name).From(u).Where(op.Eq(u.Name, "alice"))
	right := pg.Select(u.ID, u.Name).From(u).Where(op.Eq(u.Name, "bob"))
	active := pg.CTE("active", pg.Select(u.ID, u.Name).From(u).Where(op.Eq(u.Name, "alice")))
	sq := pg.Select(u.ID).From(u).Where(op.Eq(u.Name, "alice")).As("sq")

	cases := []struct {
		name string
		sql  string
		run  func() (sql.Query, error)
	}{
		{"select where and", `select "users"."id", "users"."name" from "users" where (("users"."name" = $1) and ("users"."id" > $2)) order by "users"."created_at" desc limit $3`, func() (sql.Query, error) {
			return pg.Select(u.ID, u.Name).From(u).Where(op.And(op.Eq(u.Name, "alice"), op.Gt(u.ID, 0))).OrderBy(op.Desc(u.CreatedAt)).Limit(10).SQL(pg.Dialect{})
		}},
		{"select whole row", `select "users"."id", "users"."name", "users"."email", "users"."created_at" from "users"`, func() (sql.Query, error) {
			return pg.Select[schemafixture.User]().From(u).SQL(pg.Dialect{})
		}},
		{"limit zero", `select "users"."id" from "users" limit $1`, func() (sql.Query, error) {
			return pg.Select(u.ID).From(u).Limit(0).SQL(pg.Dialect{})
		}},
		{"offset zero", `select "users"."id" from "users" offset $1`, func() (sql.Query, error) {
			return pg.Select(u.ID).From(u).Offset(0).SQL(pg.Dialect{})
		}},
		{"order desc replaces", `select "users"."id" from "users" order by "users"."name" desc limit $1`, func() (sql.Query, error) {
			return pg.Select(u.ID).From(u).OrderBy(op.Asc(u.ID)).OrderBy(op.Desc(u.Name)).Limit(1).SQL(pg.Dialect{})
		}},
		{"group having", `select "users"."name" from "users" where "users"."id" > $1 group by "users"."name" having count(*) > 1 order by "users"."name" asc`, func() (sql.Query, error) {
			return pg.Select(u.Name).From(u).Where(op.Gt(u.ID, 0)).GroupBy(u.Name).Having(sql.Raw("count(*) > 1")).OrderBy(op.Asc(u.Name)).SQL(pg.Dialect{})
		}},
		{"count distinct order", `select "users"."name" from "users" group by "users"."name" having count("users"."id") > $1 order by count("users"."id") desc`, func() (sql.Query, error) {
			return pg.Select(u.Name).From(u).GroupBy(u.Name).Having(op.Gt(op.Count(u.ID), 1)).OrderBy(op.Desc(op.Count(u.ID))).SQL(pg.Dialect{})
		}},
		{"inner left join", `select "users"."name" from "users" inner join "posts" on "posts"."author_id" = "users"."id" left join "posts" on "posts"."author_id" = "users"."id" where "users"."name" = $1`, func() (sql.Query, error) {
			return pg.Select(u.Name).From(u).InnerJoin(p, on).LeftJoin(p, on).Where(op.Eq(u.Name, "alice")).SQL(pg.Dialect{})
		}},
		{"right join", `select "users"."id" from "users" right join "posts" on "posts"."author_id" = "users"."id"`, func() (sql.Query, error) {
			return pg.Select(u.ID).From(u).RightJoin(p, on).SQL(pg.Dialect{})
		}},
		{"full join", `select "users"."id" from "users" full join "posts" on "posts"."author_id" = "users"."id"`, func() (sql.Query, error) {
			return pg.Select(u.ID).From(u).FullJoin(p, on).SQL(pg.Dialect{})
		}},
		{"cross join", `select "users"."id" from "users" cross join "posts"`, func() (sql.Query, error) {
			return pg.Select(u.ID).From(u).CrossJoin(p).SQL(pg.Dialect{})
		}},
		{"distinct", `select distinct "users"."name" from "users"`, func() (sql.Query, error) {
			return pg.Select(u.Name).From(u).Distinct().SQL(pg.Dialect{})
		}},
		{"distinct on for update", `select distinct on ("users"."id") "users"."id", "users"."name" from "users" order by "users"."id" asc for update of "users" skip locked`, func() (sql.Query, error) {
			return pg.Select(u.ID, u.Name).From(u).DistinctOn(u.ID).OrderBy(op.Asc(u.ID)).ForUpdate(query.RowLock{Of: []sql.Chunk{u}, SkipLocked: true}).SQL(pg.Dialect{})
		}},
		{"for update nowait", `select "users"."id" from "users" for update nowait`, func() (sql.Query, error) {
			return pg.Select(u.ID).From(u).ForUpdate(query.RowLock{NoWait: true}).SQL(pg.Dialect{})
		}},
		{"for share", `select "users"."id" from "users" for share of "users" nowait`, func() (sql.Query, error) {
			return pg.Select(u.ID).From(u).ForShare(query.RowLock{Of: []sql.Chunk{u}, NoWait: true}).SQL(pg.Dialect{})
		}},
		{"for update", `select "users"."id" from "users" for update`, func() (sql.Query, error) {
			return pg.Select(u.ID).From(u).ForUpdate().SQL(pg.Dialect{})
		}},
		{"union", `(select "users"."id", "users"."name" from "users" where "users"."name" = $1) union (select "users"."id", "users"."name" from "users" where "users"."name" = $2) order by "name" asc limit $3`, func() (sql.Query, error) {
			return left.Union(right).OrderBy(op.Asc(u.Name)).Limit(10).SQL(pg.Dialect{})
		}},
		{"union all", `(select "users"."name" from "users" order by "users"."id" asc) union all (select "users"."name" from "users")`, func() (sql.Query, error) {
			return pg.Select(u.Name).From(u).OrderBy(op.Asc(u.ID)).UnionAll(pg.Select(u.Name).From(u)).SQL(pg.Dialect{})
		}},
		{"intersect", `(select "users"."id", "users"."name" from "users" where "users"."name" = $1) intersect (select "users"."id", "users"."name" from "users" where "users"."name" = $2)`, func() (sql.Query, error) {
			return left.Intersect(right).SQL(pg.Dialect{})
		}},
		{"intersect all", `(select "users"."id", "users"."name" from "users" where "users"."name" = $1) intersect all (select "users"."id", "users"."name" from "users" where "users"."name" = $2)`, func() (sql.Query, error) {
			return left.IntersectAll(right).SQL(pg.Dialect{})
		}},
		{"except", `(select "users"."id", "users"."name" from "users" where "users"."name" = $1) except (select "users"."id", "users"."name" from "users" where "users"."name" = $2)`, func() (sql.Query, error) {
			return left.Except(right).SQL(pg.Dialect{})
		}},
		{"except all", `(select "users"."id", "users"."name" from "users" where "users"."name" = $1) except all (select "users"."id", "users"."name" from "users" where "users"."name" = $2)`, func() (sql.Query, error) {
			return left.ExceptAll(right).SQL(pg.Dialect{})
		}},
		{"with", `with "active" as (select "users"."id", "users"."name" from "users" where "users"."name" = $1) select "active"."name" from "active" where "active"."id" = $2`, func() (sql.Query, error) {
			return pg.Select(active.Col(u.Name)).From(active).With(active).Where(op.Eq(active.Col(u.ID), int64(7))).SQL(pg.Dialect{})
		}},
		{"with recursive", `with recursive "active" as (select "users"."name" from "users") select "active"."name" from "active"`, func() (sql.Query, error) {
			cte := pg.Recursive("active", pg.Select(u.Name).From(u))
			return pg.Select(cte.Col(u.Name)).From(cte).With(cte).SQL(pg.Dialect{})
		}},
		{"in subquery", `select "users"."name" from "users" where "users"."id" in (select "users"."id" from "users" where "users"."name" = $1)`, func() (sql.Query, error) {
			sub := pg.Select(u.ID).From(u).Where(op.Eq(u.Name, "alice"))
			return pg.Select(u.Name).From(u).Where(op.In(u.ID, sub)).SQL(pg.Dialect{})
		}},
		{"not in subquery", `select "users"."name" from "users" where "users"."id" not in (select "users"."id" from "users" where "users"."name" = $1)`, func() (sql.Query, error) {
			sub := pg.Select(u.ID).From(u).Where(op.Eq(u.Name, "alice"))
			return pg.Select(u.Name).From(u).Where(op.NotIn(u.ID, sub)).SQL(pg.Dialect{})
		}},
		{"exists", `select "users"."name" from "users" where exists (select "posts"."id" from "posts" where "posts"."author_id" = "users"."id")`, func() (sql.Query, error) {
			sub := pg.Select(p.ID).From(p).Where(op.Eq(p.AuthorID, u.ID))
			return pg.Select(u.Name).From(u).Where(op.Exists(sub)).SQL(pg.Dialect{})
		}},
		{"not exists", `select "users"."name" from "users" where not exists (select "posts"."id" from "posts" where "posts"."author_id" = "users"."id")`, func() (sql.Query, error) {
			sub := pg.Select(p.ID).From(p).Where(op.Eq(p.AuthorID, u.ID))
			return pg.Select(u.Name).From(u).Where(op.NotExists(sub)).SQL(pg.Dialect{})
		}},
		{"derived", `select "sq"."id" from (select "users"."id" from "users" where "users"."name" = $1) "sq" where "sq"."id" = $2`, func() (sql.Query, error) {
			return pg.Select(sq.Col(u.ID)).From(sq).Where(op.Eq(sq.Col(u.ID), int64(1))).SQL(pg.Dialect{})
		}},
		{"in array", `select "users"."id" from "users" where "users"."id" in ($1, $2)`, func() (sql.Query, error) {
			return pg.Select(u.ID).From(u).Where(op.InArray(u.ID, []int64{1, 2})).SQL(pg.Dialect{})
		}},
		{"empty in array", `select "users"."id" from "users" where 1 = 0`, func() (sql.Query, error) {
			return pg.Select(u.ID).From(u).Where(op.InArray(u.ID, []int64{})).SQL(pg.Dialect{})
		}},
		{"empty not in array", `select "users"."id" from "users" where 1 = 1`, func() (sql.Query, error) {
			return pg.Select(u.ID).From(u).Where(op.NotInArray(u.ID, []int64{})).SQL(pg.Dialect{})
		}},
		{"between like null", `select "users"."id" from "users" where (("users"."id" between $1 and $2) and ("users"."name" like $3) and (("users"."email" is null)))`, func() (sql.Query, error) {
			return pg.Select(u.ID).From(u).Where(op.And(op.Between(u.ID, 1, 3), op.Like(u.Name, "a%"), op.IsNull(u.Email))).SQL(pg.Dialect{})
		}},
		{"ilike not null", `select "users"."id" from "users" where (("users"."name" ilike $1) and (("users"."email" is not null)))`, func() (sql.Query, error) {
			return pg.Select(u.ID).From(u).Where(op.And(op.ILike(u.Name, "a%"), op.IsNotNull(u.Email))).SQL(pg.Dialect{})
		}},
		{"arithmetic", `select "users"."id" from "users" where "users"."id" = (("users"."id" + $1) * $2)`, func() (sql.Query, error) {
			return pg.Select(u.ID).From(u).Where(op.Eq(u.ID, op.Mul(op.Add(u.ID, 1), 2))).SQL(pg.Dialect{})
		}},
		{"count star", `select count(*) from "users" where "users"."name" = $1`, func() (sql.Query, error) {
			return pg.Value[int64](op.Count()).From(u).Where(op.Eq(u.Name, "alice")).SQL(pg.Dialect{})
		}},
		{"sum avg min max", `select sum("users"."id"), avg(distinct "users"."id"), min("users"."id"), max("users"."id") from "users"`, func() (sql.Query, error) {
			return pg.Scan(func(sql.Row, *struct{}) error { return nil }, op.Sum(u.ID), op.AvgDistinct(u.ID), op.Min(u.ID), op.Max(u.ID)).From(u).SQL(pg.Dialect{})
		}},
		{"insert batch conflict nothing", `insert into "users" ("name", "email") values ($1, $2), ($3, $4) on conflict do nothing returning "users"."id", "users"."created_at"`, func() (sql.Query, error) {
			return pg.Insert(u).Columns(u.Name, u.Email).Values("a", "a@b.c").Values("b", nil).OnConflictDoNothing().Returning(u.ID, u.CreatedAt).SQL(pg.Dialect{})
		}},
		{"insert on conflict update", `insert into "users" ("id", "name") values ($1, $2) on conflict ("id") where ("users"."email" is not null) do update set "name" = excluded."name", "email" = $3 where "users"."name" <> $4`, func() (sql.Query, error) {
			return pg.Insert(u).Columns(u.ID, u.Name).Values(int64(1), "a").OnConflictDoUpdate(u.ID).OnConflictWhere(op.IsNotNull(u.Email)).Set(u.Name, pg.Excluded(u.Name)).Set(u.Email, nil).UpdateWhere(op.Ne(u.Name, "x")).SQL(pg.Dialect{})
		}},
		{"insert select", `insert into "users" ("id", "name") select "users"."id", "users"."name" from "users" where "users"."name" = $1`, func() (sql.Query, error) {
			return pg.Insert(u).Columns(u.ID, u.Name).Select(pg.Select(u.ID, u.Name).From(u).Where(op.Eq(u.Name, "alice"))).SQL(pg.Dialect{})
		}},
		{"insert scalar subquery", `insert into "users" ("id", "name") values ($1, (select "users"."name" from "users" where "users"."id" = $2))`, func() (sql.Query, error) {
			sub := pg.Select(u.Name).From(u).Where(op.Eq(u.ID, int64(1)))
			return pg.Insert(u).Columns(u.ID, u.Name).Values(int64(2), sub).SQL(pg.Dialect{})
		}},
		{"insert with", `with "gone" as (select "users"."id" from "users" where "users"."name" = $1) insert into "users" ("id", "name") values ($2, $3)`, func() (sql.Query, error) {
			gone := pg.CTE("gone", pg.Select(u.ID).From(u).Where(op.Eq(u.Name, "alice")))
			return pg.Insert(u).Columns(u.ID, u.Name).Values(int64(9), "z").With(gone).SQL(pg.Dialect{})
		}},
		{"update subquery", `update "users" set "name" = (select "users"."name" from "users" where "users"."id" = $1) where "users"."id" = $2`, func() (sql.Query, error) {
			sub := pg.Select(u.Name).From(u).Where(op.Eq(u.ID, int64(1)))
			return pg.Update(u).Set(u.Name, sub).Where(op.Eq(u.ID, int64(2))).SQL(pg.Dialect{})
		}},
		{"update raw zero", `update "users" set "name" = $1, "id" = $2, "email" = lower("email") where "users"."id" = $3 returning "users"."name"`, func() (sql.Query, error) {
			return pg.Update(u).Set(u.Name, "").Set(u.ID, 0).Set(u.Email, sql.Raw(`lower("email")`)).Where(op.Eq(u.ID, int64(1))).Returning(u.Name).SQL(pg.Dialect{})
		}},
		{"update from join", `update "users" set "name" = "posts"."title" from "posts" left join "users" on "posts"."author_id" = "users"."id" where "posts"."author_id" = "users"."id"`, func() (sql.Query, error) {
			return pg.Update(u).Set(u.Name, p.Title).From(p).LeftJoin(u, on).Where(on).SQL(pg.Dialect{})
		}},
		{"update with", `with "gone" as (select "users"."id" from "users" where "users"."name" = $1) update "users" set "name" = $2 where "users"."id" in (select "gone"."id" from "gone")`, func() (sql.Query, error) {
			gone := pg.CTE("gone", pg.Select(u.ID).From(u).Where(op.Eq(u.Name, "alice")))
			return pg.Update(u).Set(u.Name, "ann").Where(op.In(u.ID, pg.Select(gone.Col(u.ID)).From(gone))).With(gone).SQL(pg.Dialect{})
		}},
		{"delete where", `delete from "users" where "users"."id" = $1 returning "users"."id"`, func() (sql.Query, error) {
			return pg.Delete(u).Where(op.Eq(u.ID, int64(1))).Returning(u.ID).SQL(pg.Dialect{})
		}},
		{"delete all", `delete from "users"`, func() (sql.Query, error) {
			return pg.Delete(u).SQL(pg.Dialect{})
		}},
		{"delete using", `delete from "users" using "posts" where "users"."id" = "posts"."author_id"`, func() (sql.Query, error) {
			return pg.Delete(u).Using(p).Where(op.Eq(u.ID, p.AuthorID)).SQL(pg.Dialect{})
		}},
		{"delete with", `with "gone" as (select "users"."id" from "users" where "users"."name" = $1) delete from "users" where "users"."id" in (select "gone"."id" from "gone")`, func() (sql.Query, error) {
			gone := pg.CTE("gone", pg.Select(u.ID).From(u).Where(op.Eq(u.Name, "alice")))
			return pg.Delete(u).Where(op.In(u.ID, pg.Select(gone.Col(u.ID)).From(gone))).With(gone).SQL(pg.Dialect{})
		}},
	}
	if len(cases) < 40 {
		t.Fatalf("matrix has %d cases, want at least 40", len(cases))
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.run()
			if err != nil {
				t.Fatal(err)
			}
			if got.SQL != tc.sql {
				t.Fatalf("sql\n got %s\nwant %s", got.SQL, tc.sql)
			}
		})
	}
}

func TestUpdateOrderLimitRejected(t *testing.T) {
	u := schemafixture.Users
	_, err := pg.Update(u).Set(u.Name, "a").OrderBy(op.Asc(u.ID)).Limit(1).SQL(pg.Dialect{})
	if err == nil || !strings.Contains(err.Error(), "order by") {
		t.Fatal(err)
	}
	_, err = pg.Delete(u).Where(op.Gt(u.ID, 0)).Limit(1).SQL(pg.Dialect{})
	if err == nil || !strings.Contains(err.Error(), "order by") {
		t.Fatal(err)
	}
}

func TestInsertIgnoreRejected(t *testing.T) {
	u := schemafixture.Users
	_, err := pg.Insert(u).Ignore().Columns(u.Name).Values("ann").SQL(pg.Dialect{})
	if err == nil || !strings.Contains(err.Error(), "insert ignore") {
		t.Fatal(err)
	}
}

func TestOnDuplicateKeyUpdateRejected(t *testing.T) {
	u := schemafixture.Users
	_, err := pg.Insert(u).Columns(u.Name).Values("ann").OnDuplicateKeyUpdate().Set(u.Name, "ada").SQL(pg.Dialect{})
	if err == nil || !strings.Contains(err.Error(), "on duplicate key update") {
		t.Fatal(err)
	}
}
