package mysql_test

import (
	"strings"
	"testing"

	"github.com/go-mist/mist/dialect/mysql"
	"github.com/go-mist/mist/internal/schemafixture"
	"github.com/go-mist/mist/op"
	"github.com/go-mist/mist/query"
	"github.com/go-mist/mist/sql"
)

// TestSQLMatrix applies the M2 query matrix to MySQL. Placeholders are ?, and identifiers use backticks.
// Dialect limits live in TestDialectLimits: full join, distinct on, returning, and on conflict.
// op.ILike renders as lower(column) like lower(parameter).
// Multi-table update / delete uses comma sources. MySQL refuses to execute when the same table appears twice.
// Offset alone is filled in with limit 18446744073709551615, because MySQL does not allow a bare offset.
func TestSQLMatrix(t *testing.T) {
	u := schemafixture.Users
	p := schemafixture.Posts
	on := op.Eq(p.AuthorID, u.ID)
	left := mysql.Select(u.ID, u.Name).From(u).Where(op.Eq(u.Name, "alice"))
	right := mysql.Select(u.ID, u.Name).From(u).Where(op.Eq(u.Name, "bob"))
	active := mysql.CTE("active", mysql.Select(u.ID, u.Name).From(u).Where(op.Eq(u.Name, "alice")))
	sq := mysql.Select(u.ID).From(u).Where(op.Eq(u.Name, "alice")).As("sq")

	cases := []struct {
		name string
		sql  string
		run  func() (sql.Query, error)
	}{
		{"select where and", "select `users`.`id`, `users`.`name` from `users` where ((`users`.`name` = ?) and (`users`.`id` > ?)) order by `users`.`created_at` desc limit ?", func() (sql.Query, error) {
			return mysql.Select(u.ID, u.Name).From(u).Where(op.And(op.Eq(u.Name, "alice"), op.Gt(u.ID, 0))).OrderBy(op.Desc(u.CreatedAt)).Limit(10).SQL(mysql.Dialect{})
		}},
		{"select whole row", "select `users`.`id`, `users`.`name`, `users`.`email`, `users`.`created_at` from `users`", func() (sql.Query, error) {
			return mysql.Select[schemafixture.User]().From(u).SQL(mysql.Dialect{})
		}},
		{"limit zero", "select `users`.`id` from `users` limit ?", func() (sql.Query, error) {
			return mysql.Select(u.ID).From(u).Limit(0).SQL(mysql.Dialect{})
		}},
		{"offset zero", "select `users`.`id` from `users` limit 18446744073709551615 offset ?", func() (sql.Query, error) {
			return mysql.Select(u.ID).From(u).Offset(0).SQL(mysql.Dialect{})
		}},
		{"order desc replaces", "select `users`.`id` from `users` order by `users`.`name` desc limit ?", func() (sql.Query, error) {
			return mysql.Select(u.ID).From(u).OrderBy(op.Asc(u.ID)).OrderBy(op.Desc(u.Name)).Limit(1).SQL(mysql.Dialect{})
		}},
		{"group having", "select `users`.`name` from `users` where `users`.`id` > ? group by `users`.`name` having count(*) > 1 order by `users`.`name` asc", func() (sql.Query, error) {
			return mysql.Select(u.Name).From(u).Where(op.Gt(u.ID, 0)).GroupBy(u.Name).Having(sql.Raw("count(*) > 1")).OrderBy(op.Asc(u.Name)).SQL(mysql.Dialect{})
		}},
		{"count distinct order", "select `users`.`name` from `users` group by `users`.`name` having count(`users`.`id`) > ? order by count(`users`.`id`) desc", func() (sql.Query, error) {
			return mysql.Select(u.Name).From(u).GroupBy(u.Name).Having(op.Gt(op.Count(u.ID), 1)).OrderBy(op.Desc(op.Count(u.ID))).SQL(mysql.Dialect{})
		}},
		{"inner left join", "select `users`.`name` from `users` inner join `posts` on `posts`.`author_id` = `users`.`id` left join `posts` on `posts`.`author_id` = `users`.`id` where `users`.`name` = ?", func() (sql.Query, error) {
			return mysql.Select(u.Name).From(u).InnerJoin(p, on).LeftJoin(p, on).Where(op.Eq(u.Name, "alice")).SQL(mysql.Dialect{})
		}},
		{"right join", "select `users`.`id` from `users` right join `posts` on `posts`.`author_id` = `users`.`id`", func() (sql.Query, error) {
			return mysql.Select(u.ID).From(u).RightJoin(p, on).SQL(mysql.Dialect{})
		}},
		{"cross join", "select `users`.`id` from `users` cross join `posts`", func() (sql.Query, error) {
			return mysql.Select(u.ID).From(u).CrossJoin(p).SQL(mysql.Dialect{})
		}},
		{"distinct", "select distinct `users`.`name` from `users`", func() (sql.Query, error) {
			return mysql.Select(u.Name).From(u).Distinct().SQL(mysql.Dialect{})
		}},
		{"for update skip locked", "select `users`.`id`, `users`.`name` from `users` order by `users`.`id` asc for update skip locked", func() (sql.Query, error) {
			return mysql.Select(u.ID, u.Name).From(u).OrderBy(op.Asc(u.ID)).ForUpdate(query.RowLock{SkipLocked: true}).SQL(mysql.Dialect{})
		}},
		{"for update nowait", "select `users`.`id` from `users` for update nowait", func() (sql.Query, error) {
			return mysql.Select(u.ID).From(u).ForUpdate(query.RowLock{NoWait: true}).SQL(mysql.Dialect{})
		}},
		{"for update", "select `users`.`id` from `users` for update", func() (sql.Query, error) {
			return mysql.Select(u.ID).From(u).ForUpdate().SQL(mysql.Dialect{})
		}},
		{"for share of", "select `users`.`id` from `users` for share of `users` skip locked", func() (sql.Query, error) {
			return mysql.Select(u.ID).From(u).ForShare(query.RowLock{Of: []sql.Chunk{u}, SkipLocked: true}).SQL(mysql.Dialect{})
		}},
		{"for update of", "select `users`.`id` from `users` for update of `users`", func() (sql.Query, error) {
			return mysql.Select(u.ID).From(u).ForUpdate(query.RowLock{Of: []sql.Chunk{u}}).SQL(mysql.Dialect{})
		}},
		{"union", "(select `users`.`id`, `users`.`name` from `users` where `users`.`name` = ?) union (select `users`.`id`, `users`.`name` from `users` where `users`.`name` = ?) order by `name` asc limit ?", func() (sql.Query, error) {
			return left.Union(right).OrderBy(op.Asc(u.Name)).Limit(10).SQL(mysql.Dialect{})
		}},
		{"union all", "(select `users`.`name` from `users` order by `users`.`id` asc) union all (select `users`.`name` from `users`)", func() (sql.Query, error) {
			return mysql.Select(u.Name).From(u).OrderBy(op.Asc(u.ID)).UnionAll(mysql.Select(u.Name).From(u)).SQL(mysql.Dialect{})
		}},
		{"intersect", "(select `users`.`id`, `users`.`name` from `users` where `users`.`name` = ?) intersect (select `users`.`id`, `users`.`name` from `users` where `users`.`name` = ?)", func() (sql.Query, error) {
			return left.Intersect(right).SQL(mysql.Dialect{})
		}},
		{"intersect all", "(select `users`.`id`, `users`.`name` from `users` where `users`.`name` = ?) intersect all (select `users`.`id`, `users`.`name` from `users` where `users`.`name` = ?)", func() (sql.Query, error) {
			return left.IntersectAll(right).SQL(mysql.Dialect{})
		}},
		{"except", "(select `users`.`id`, `users`.`name` from `users` where `users`.`name` = ?) except (select `users`.`id`, `users`.`name` from `users` where `users`.`name` = ?)", func() (sql.Query, error) {
			return left.Except(right).SQL(mysql.Dialect{})
		}},
		{"except all", "(select `users`.`id`, `users`.`name` from `users` where `users`.`name` = ?) except all (select `users`.`id`, `users`.`name` from `users` where `users`.`name` = ?)", func() (sql.Query, error) {
			return left.ExceptAll(right).SQL(mysql.Dialect{})
		}},
		{"with", "with `active` as (select `users`.`id`, `users`.`name` from `users` where `users`.`name` = ?) select `active`.`name` from `active` where `active`.`id` = ?", func() (sql.Query, error) {
			return mysql.Select(active.Col(u.Name)).From(active).With(active).Where(op.Eq(active.Col(u.ID), int64(7))).SQL(mysql.Dialect{})
		}},
		{"with recursive", "with recursive `active` as (select `users`.`name` from `users`) select `active`.`name` from `active`", func() (sql.Query, error) {
			cte := mysql.Recursive("active", mysql.Select(u.Name).From(u))
			return mysql.Select(cte.Col(u.Name)).From(cte).With(cte).SQL(mysql.Dialect{})
		}},
		{"in subquery", "select `users`.`name` from `users` where `users`.`id` in (select `users`.`id` from `users` where `users`.`name` = ?)", func() (sql.Query, error) {
			sub := mysql.Select(u.ID).From(u).Where(op.Eq(u.Name, "alice"))
			return mysql.Select(u.Name).From(u).Where(op.In(u.ID, sub)).SQL(mysql.Dialect{})
		}},
		{"not in subquery", "select `users`.`name` from `users` where `users`.`id` not in (select `users`.`id` from `users` where `users`.`name` = ?)", func() (sql.Query, error) {
			sub := mysql.Select(u.ID).From(u).Where(op.Eq(u.Name, "alice"))
			return mysql.Select(u.Name).From(u).Where(op.NotIn(u.ID, sub)).SQL(mysql.Dialect{})
		}},
		{"exists", "select `users`.`name` from `users` where exists (select `posts`.`id` from `posts` where `posts`.`author_id` = `users`.`id`)", func() (sql.Query, error) {
			sub := mysql.Select(p.ID).From(p).Where(op.Eq(p.AuthorID, u.ID))
			return mysql.Select(u.Name).From(u).Where(op.Exists(sub)).SQL(mysql.Dialect{})
		}},
		{"not exists", "select `users`.`name` from `users` where not exists (select `posts`.`id` from `posts` where `posts`.`author_id` = `users`.`id`)", func() (sql.Query, error) {
			sub := mysql.Select(p.ID).From(p).Where(op.Eq(p.AuthorID, u.ID))
			return mysql.Select(u.Name).From(u).Where(op.NotExists(sub)).SQL(mysql.Dialect{})
		}},
		{"derived", "select `sq`.`id` from (select `users`.`id` from `users` where `users`.`name` = ?) `sq` where `sq`.`id` = ?", func() (sql.Query, error) {
			return mysql.Select(sq.Col(u.ID)).From(sq).Where(op.Eq(sq.Col(u.ID), int64(1))).SQL(mysql.Dialect{})
		}},
		{"in array", "select `users`.`id` from `users` where `users`.`id` in (?, ?)", func() (sql.Query, error) {
			return mysql.Select(u.ID).From(u).Where(op.InArray(u.ID, []int64{1, 2})).SQL(mysql.Dialect{})
		}},
		{"empty in array", "select `users`.`id` from `users` where 1 = 0", func() (sql.Query, error) {
			return mysql.Select(u.ID).From(u).Where(op.InArray(u.ID, []int64{})).SQL(mysql.Dialect{})
		}},
		{"empty not in array", "select `users`.`id` from `users` where 1 = 1", func() (sql.Query, error) {
			return mysql.Select(u.ID).From(u).Where(op.NotInArray(u.ID, []int64{})).SQL(mysql.Dialect{})
		}},
		{"between like null", "select `users`.`id` from `users` where ((`users`.`id` between ? and ?) and (`users`.`name` like ?) and ((`users`.`email` is null)))", func() (sql.Query, error) {
			return mysql.Select(u.ID).From(u).Where(op.And(op.Between(u.ID, 1, 3), op.Like(u.Name, "a%"), op.IsNull(u.Email))).SQL(mysql.Dialect{})
		}},
		{"ilike", "select `users`.`id` from `users` where ((lower(`users`.`name`) like lower(?)) and ((`users`.`email` is not null)))", func() (sql.Query, error) {
			return mysql.Select(u.ID).From(u).Where(op.And(op.ILike(u.Name, "a%"), op.IsNotNull(u.Email))).SQL(mysql.Dialect{})
		}},
		{"not ilike", "select `users`.`id` from `users` where lower(`users`.`name`) not like lower(?)", func() (sql.Query, error) {
			return mysql.Select(u.ID).From(u).Where(op.NotILike(u.Name, "a%")).SQL(mysql.Dialect{})
		}},
		{"arithmetic", "select `users`.`id` from `users` where `users`.`id` = ((`users`.`id` + ?) * ?)", func() (sql.Query, error) {
			return mysql.Select(u.ID).From(u).Where(op.Eq(u.ID, op.Mul(op.Add(u.ID, 1), 2))).SQL(mysql.Dialect{})
		}},
		{"count star", "select count(*) from `users` where `users`.`name` = ?", func() (sql.Query, error) {
			return mysql.Value[int64](op.Count()).From(u).Where(op.Eq(u.Name, "alice")).SQL(mysql.Dialect{})
		}},
		{"sum avg min max", "select sum(`users`.`id`), avg(distinct `users`.`id`), min(`users`.`id`), max(`users`.`id`) from `users`", func() (sql.Query, error) {
			return mysql.Scan(func(sql.Row, *struct{}) error { return nil }, op.Sum(u.ID), op.AvgDistinct(u.ID), op.Min(u.ID), op.Max(u.ID)).From(u).SQL(mysql.Dialect{})
		}},
		{"insert ignore", "insert ignore into `users` (`name`) values (?)", func() (sql.Query, error) {
			return mysql.Insert(u).Ignore().Columns(u.Name).Values("a").SQL(mysql.Dialect{})
		}},
		{"insert batch", "insert into `users` (`name`, `email`) values (?, ?), (?, ?)", func() (sql.Query, error) {
			return mysql.Insert(u).Columns(u.Name, u.Email).Values("a", "a@b.c").Values("b", nil).SQL(mysql.Dialect{})
		}},
		{"on duplicate key update", "insert into `users` (`id`, `name`) values (?, ?) as `newrow` on duplicate key update `name` = `newrow`.`name`", func() (sql.Query, error) {
			return mysql.Insert(u).Columns(u.ID, u.Name).Values(int64(1), "a").OnDuplicateKeyUpdate().Set(u.Name, mysql.NewRow(u.Name)).SQL(mysql.Dialect{})
		}},
		{"insert select duplicate", "insert into `users` (`id`, `name`) select `newrow`.`id`, `newrow`.`name` from (select `users`.`id` as `id`, `users`.`name` as `name` from `users` where `users`.`name` = ?) as `newrow` on duplicate key update `name` = `newrow`.`name`", func() (sql.Query, error) {
			return mysql.Insert(u).Columns(u.ID, u.Name).Select(mysql.Select(u.ID, u.Name).From(u).Where(op.Eq(u.Name, "alice"))).OnDuplicateKeyUpdate().Set(u.Name, mysql.NewRow(u.Name)).SQL(mysql.Dialect{})
		}},
		{"insert select", "insert into `users` (`id`, `name`) select `users`.`id`, `users`.`name` from `users` where `users`.`name` = ?", func() (sql.Query, error) {
			return mysql.Insert(u).Columns(u.ID, u.Name).Select(mysql.Select(u.ID, u.Name).From(u).Where(op.Eq(u.Name, "alice"))).SQL(mysql.Dialect{})
		}},
		{"insert scalar subquery", "insert into `users` (`id`, `name`) values (?, (select `users`.`name` from `users` where `users`.`id` = ?))", func() (sql.Query, error) {
			sub := mysql.Select(u.Name).From(u).Where(op.Eq(u.ID, int64(1)))
			return mysql.Insert(u).Columns(u.ID, u.Name).Values(int64(2), sub).SQL(mysql.Dialect{})
		}},
		{"insert select with", "insert into `users` (`id`, `name`) with `gone` as (select `users`.`id`, `users`.`name` from `users` where `users`.`name` = ?) select `gone`.`id`, `gone`.`name` from `gone`", func() (sql.Query, error) {
			gone := mysql.CTE("gone", mysql.Select(u.ID, u.Name).From(u).Where(op.Eq(u.Name, "alice")))
			return mysql.Insert(u).Columns(u.ID, u.Name).Select(mysql.Select(gone.Col(u.ID), gone.Col(u.Name)).From(gone)).With(gone).SQL(mysql.Dialect{})
		}},
		{"update subquery", "update `users` set `name` = (select `users`.`name` from `users` where `users`.`id` = ?) where `users`.`id` = ?", func() (sql.Query, error) {
			sub := mysql.Select(u.Name).From(u).Where(op.Eq(u.ID, int64(1)))
			return mysql.Update(u).Set(u.Name, sub).Where(op.Eq(u.ID, int64(2))).SQL(mysql.Dialect{})
		}},
		{"update raw zero", "update `users` set `name` = ?, `id` = ?, `email` = lower(\"email\") where `users`.`id` = ?", func() (sql.Query, error) {
			return mysql.Update(u).Set(u.Name, "").Set(u.ID, 0).Set(u.Email, sql.Raw(`lower("email")`)).Where(op.Eq(u.ID, int64(1))).SQL(mysql.Dialect{})
		}},
		{"update from", "update `users`, `posts` set `name` = `posts`.`title` where `posts`.`author_id` = `users`.`id`", func() (sql.Query, error) {
			return mysql.Update(u).Set(u.Name, p.Title).From(p).Where(on).SQL(mysql.Dialect{})
		}},
		{"update join", "update `users`, `posts` left join `users` on `posts`.`author_id` = `users`.`id` set `name` = `posts`.`title` where `posts`.`author_id` = `users`.`id`", func() (sql.Query, error) {
			return mysql.Update(u).Set(u.Name, p.Title).From(p).LeftJoin(u, on).Where(on).SQL(mysql.Dialect{})
		}},
		{"update with", "with `gone` as (select `users`.`id` from `users` where `users`.`name` = ?) update `users` set `name` = ? where `users`.`id` in (select `gone`.`id` from `gone`)", func() (sql.Query, error) {
			gone := mysql.CTE("gone", mysql.Select(u.ID).From(u).Where(op.Eq(u.Name, "alice")))
			return mysql.Update(u).Set(u.Name, "ann").Where(op.In(u.ID, mysql.Select(gone.Col(u.ID)).From(gone))).With(gone).SQL(mysql.Dialect{})
		}},
		{"delete where", "delete from `users` where `users`.`id` = ?", func() (sql.Query, error) {
			return mysql.Delete(u).Where(op.Eq(u.ID, int64(1))).SQL(mysql.Dialect{})
		}},
		{"delete all", "delete from `users`", func() (sql.Query, error) {
			return mysql.Delete(u).SQL(mysql.Dialect{})
		}},
		{"update order limit", "update `users` set `name` = ? where `users`.`id` > ? order by `users`.`id` desc limit ?", func() (sql.Query, error) {
			return mysql.Update(u).Set(u.Name, "z").Where(op.Gt(u.ID, 0)).OrderBy(op.Desc(u.ID)).Limit(1).SQL(mysql.Dialect{})
		}},
		{"delete order limit", "delete from `users` where `users`.`id` > ? order by `users`.`id` asc limit ?", func() (sql.Query, error) {
			return mysql.Delete(u).Where(op.Gt(u.ID, 0)).OrderBy(op.Asc(u.ID)).Limit(1).SQL(mysql.Dialect{})
		}},
		{"delete using", "delete `users` from `users`, `posts` where `users`.`id` = `posts`.`author_id`", func() (sql.Query, error) {
			return mysql.Delete(u).Using(p).Where(op.Eq(u.ID, p.AuthorID)).SQL(mysql.Dialect{})
		}},
		{"delete with", "with `gone` as (select `users`.`id` from `users` where `users`.`name` = ?) delete from `users` where `users`.`id` in (select `gone`.`id` from `gone`)", func() (sql.Query, error) {
			gone := mysql.CTE("gone", mysql.Select(u.ID).From(u).Where(op.Eq(u.Name, "alice")))
			return mysql.Delete(u).Where(op.In(u.ID, mysql.Select(gone.Col(u.ID)).From(gone))).With(gone).SQL(mysql.Dialect{})
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

func TestDialectLimits(t *testing.T) {
	u := schemafixture.Users
	p := schemafixture.Posts
	on := op.Eq(p.AuthorID, u.ID)
	cases := []struct {
		name string
		want string
		run  func() error
	}{
		{"full join", "full join", func() error {
			_, err := mysql.Select(u.ID).From(u).FullJoin(p, on).SQL(mysql.Dialect{})
			return err
		}},
		{"distinct on", "distinct on", func() error {
			_, err := mysql.Select(u.ID, u.Name).From(u).DistinctOn(u.ID).SQL(mysql.Dialect{})
			return err
		}},
		{"returning", "returning", func() error {
			_, err := mysql.Insert(u).Columns(u.Name).Values("ann").Returning(u.ID).SQL(mysql.Dialect{})
			return err
		}},
		{"on conflict", "on conflict", func() error {
			_, err := mysql.Insert(u).Columns(u.ID).Values(int64(1)).OnConflictDoNothing(u.ID).SQL(mysql.Dialect{})
			return err
		}},
		{"multi update limit", "multi-table update", func() error {
			_, err := mysql.Update(u).Set(u.Name, p.Title).From(p).Where(on).Limit(1).SQL(mysql.Dialect{})
			return err
		}},
		{"insert values with", "cannot use with", func() error {
			gone := mysql.CTE("gone", mysql.Select(u.ID).From(u))
			_, err := mysql.Insert(u).Columns(u.ID, u.Name).Values(int64(9), "z").With(gone).SQL(mysql.Dialect{})
			return err
		}},
		{"duplicate with conflict", "cannot be combined with on conflict", func() error {
			_, err := mysql.Insert(u).Columns(u.ID).Values(int64(1)).OnDuplicateKeyUpdate().OnConflictDoNothing().SQL(mysql.Dialect{})
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
