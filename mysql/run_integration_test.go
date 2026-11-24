//go:build integration

package mysql_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/go-mist/mist"
	"github.com/go-mist/mist/internal/schemafixture"
	"github.com/go-mist/mist/mysql"
	"github.com/go-mist/mist/op"
	"github.com/go-mist/mist/query"
	"github.com/go-mist/mist/sql"
)

func TestSQLRuns(t *testing.T) {
	ctx := context.Background()
	db := openMySQL(t, ctx)
	if _, err := db.Exec(ctx, `drop table if exists posts`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `drop table if exists users`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `create table users (
		id bigint primary key,
		name varchar(64) not null,
		email varchar(64) null,
		created_at datetime(6) not null
	)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `create table posts (
		id bigint primary key,
		author_id bigint not null,
		title varchar(64) not null
	)`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(context.Background(), `drop table if exists posts`)
		_, _ = db.Exec(context.Background(), `drop table if exists users`)
	})
	u := schemafixture.Users
	p := schemafixture.Posts
	bobAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	aliceAt := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	if _, err := mysql.Insert(u).Columns(u.ID, u.Name, u.CreatedAt).
		Values(int64(1), "bob", bobAt).
		Values(int64(7), "alice", aliceAt).
		Exec(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, err := mysql.Insert(p).Columns(p.ID, p.AuthorID, p.Title).
		Values(int64(1), int64(7), "hello").
		Exec(ctx, db); err != nil {
		t.Fatal(err)
	}

	t.Run("where limit", func(t *testing.T) {
		rows, err := mysql.Select(u.ID, u.Name).From(u).
			Where(op.And(op.Eq(u.Name, "alice"), op.Gt(u.ID, 0))).
			OrderBy(op.Desc(u.CreatedAt)).Limit(10).All(ctx, db)
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 1 || rows[0].ID != 7 || rows[0].Name != "alice" {
			t.Fatalf("%+v", rows)
		}
	})
	t.Run("limit zero", func(t *testing.T) {
		rows, err := mysql.Select(u.ID).From(u).Limit(0).All(ctx, db)
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 0 {
			t.Fatalf("%+v", rows)
		}
	})
	t.Run("offset", func(t *testing.T) {
		rows, err := mysql.Select(u.ID).From(u).OrderBy(op.Asc(u.ID)).Offset(1).All(ctx, db)
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 1 || rows[0].ID != 7 {
			t.Fatalf("%+v", rows)
		}
	})
	t.Run("join", func(t *testing.T) {
		rows, err := mysql.Select(u.Name).From(u).InnerJoin(p, op.Eq(p.AuthorID, u.ID)).All(ctx, db)
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 1 || rows[0].Name != "alice" {
			t.Fatalf("%+v", rows)
		}
	})
	t.Run("union", func(t *testing.T) {
		left := mysql.Select(u.Name).From(u).Where(op.Eq(u.Name, "alice"))
		right := mysql.Select(u.Name).From(u).Where(op.Eq(u.Name, "bob"))
		got := names(t, ctx, db, left.Union(right))
		if len(got) != 2 {
			t.Fatal(got)
		}
	})
	t.Run("intersect", func(t *testing.T) {
		left := mysql.Select(u.Name).From(u).Where(op.Eq(u.Name, "alice"))
		right := mysql.Select(u.Name).From(u).Where(op.Eq(u.Name, "bob"))
		if got := names(t, ctx, db, left.Intersect(right)); len(got) != 0 {
			t.Fatal(got)
		}
	})
	t.Run("except", func(t *testing.T) {
		left := mysql.Select(u.Name).From(u)
		right := mysql.Select(u.Name).From(u).Where(op.Eq(u.Name, "bob"))
		got := names(t, ctx, db, left.Except(right))
		if len(got) != 1 || got[0] != "alice" {
			t.Fatal(got)
		}
	})
	t.Run("with", func(t *testing.T) {
		active := mysql.CTE("active", mysql.Select(u.ID, u.Name).From(u).Where(op.Eq(u.Name, "alice")))
		rows, err := mysql.Select(active.Col(u.Name)).From(active).With(active).All(ctx, db)
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 1 || rows[0].Name != "alice" {
			t.Fatalf("%+v", rows)
		}
	})
	t.Run("derived", func(t *testing.T) {
		sq := mysql.Select(u.ID, u.Name).From(u).Where(op.Eq(u.Name, "alice")).As("sq")
		rows, err := mysql.Select(sq.Col(u.Name)).From(sq).All(ctx, db)
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 1 || rows[0].Name != "alice" {
			t.Fatalf("%+v", rows)
		}
	})
	t.Run("in and exists", func(t *testing.T) {
		sub := mysql.Select(u.ID).From(u).Where(op.Eq(u.Name, "alice"))
		rows, err := mysql.Select(u.Name).From(u).Where(op.In(u.ID, sub)).All(ctx, db)
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 1 || rows[0].Name != "alice" {
			t.Fatalf("%+v", rows)
		}
		posts := mysql.Select(p.ID).From(p).Where(op.Eq(p.AuthorID, u.ID))
		rows, err = mysql.Select(u.Name).From(u).Where(op.Exists(posts)).All(ctx, db)
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 1 || rows[0].Name != "alice" {
			t.Fatalf("%+v", rows)
		}
	})
	t.Run("empty in", func(t *testing.T) {
		rows, err := mysql.Select(u.ID).From(u).Where(op.InArray(u.ID, []int64{})).All(ctx, db)
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 0 {
			t.Fatalf("%+v", rows)
		}
	})
	t.Run("for share of", func(t *testing.T) {
		err := db.Tx(ctx, func(tx mist.Tx) error {
			rows, err := mysql.Select(u.ID).From(u).ForShare(query.RowLock{Of: []sql.Chunk{u}, SkipLocked: true}).All(ctx, tx)
			if err != nil {
				return err
			}
			if len(rows) != 2 {
				return fmt.Errorf("locked %d rows", len(rows))
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	})
	t.Run("for update of", func(t *testing.T) {
		err := db.Tx(ctx, func(tx mist.Tx) error {
			rows, err := mysql.Select(u.ID).From(u).ForUpdate(query.RowLock{Of: []sql.Chunk{u}, SkipLocked: true}).All(ctx, tx)
			if err != nil {
				return err
			}
			if len(rows) != 2 {
				return fmt.Errorf("locked %d rows", len(rows))
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	})
	t.Run("on duplicate key", func(t *testing.T) {
		_, err := mysql.Insert(u).Columns(u.ID, u.Name, u.CreatedAt).
			Values(int64(1), "ada", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)).
			OnDuplicateKeyUpdate().
			Set(u.Name, mysql.NewRow(u.Name)).
			Exec(ctx, db)
		if err != nil {
			t.Fatal(err)
		}
		one, err := mysql.Select(u.Name).From(u).Where(op.Eq(u.ID, int64(1))).One(ctx, db)
		if err != nil {
			t.Fatal(err)
		}
		if one.Name != "ada" {
			t.Fatal(one.Name)
		}
	})
	t.Run("insert ignore", func(t *testing.T) {
		_, err := mysql.Insert(u).Ignore().Columns(u.ID, u.Name, u.CreatedAt).
			Values(int64(1), "zoe", bobAt).
			Values(int64(9), "zoe", bobAt).
			Exec(ctx, db)
		if err != nil {
			t.Fatal(err)
		}
		kept, err := mysql.Select(u.Name).From(u).Where(op.Eq(u.ID, int64(1))).One(ctx, db)
		if err != nil {
			t.Fatal(err)
		}
		if kept.Name != "ada" {
			t.Fatal(kept.Name)
		}
		added, err := mysql.Select(u.Name).From(u).Where(op.Eq(u.ID, int64(9))).One(ctx, db)
		if err != nil {
			t.Fatal(err)
		}
		if added.Name != "zoe" {
			t.Fatal(added.Name)
		}
	})
	t.Run("update and delete order limit", func(t *testing.T) {
		_, err := mysql.Update(u).Set(u.Name, "zz").Where(op.Gt(u.ID, int64(0))).OrderBy(op.Desc(u.ID)).Limit(1).Exec(ctx, db)
		if err != nil {
			t.Fatal(err)
		}
		top, err := mysql.Select(u.Name).From(u).Where(op.Eq(u.ID, int64(9))).One(ctx, db)
		if err != nil {
			t.Fatal(err)
		}
		if top.Name != "zz" {
			t.Fatal(top.Name)
		}
		kept, err := mysql.Select(u.Name).From(u).Where(op.Eq(u.ID, int64(7))).One(ctx, db)
		if err != nil {
			t.Fatal(err)
		}
		if kept.Name != "alice" {
			t.Fatal(kept.Name)
		}
		_, err = mysql.Delete(u).Where(op.Gte(u.ID, int64(9))).OrderBy(op.Asc(u.ID)).Limit(1).Exec(ctx, db)
		if err != nil {
			t.Fatal(err)
		}
		_, err = mysql.Select(u.ID).From(u).Where(op.Eq(u.ID, int64(9))).One(ctx, db)
		if !errors.Is(err, query.ErrNoRows) {
			t.Fatal(err)
		}
	})
	t.Run("ilike", func(t *testing.T) {
		rows, err := mysql.Select(u.ID, u.Name).From(u).Where(op.ILike(u.Name, "ALICE")).All(ctx, db)
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 1 || rows[0].Name != "alice" {
			t.Fatalf("%+v", rows)
		}
	})
}

func TestMultiTableWrite(t *testing.T) {
	ctx := context.Background()
	db := openMySQL(t, ctx)
	for _, stmt := range []string{
		`drop table if exists m8j_posts`,
		`drop table if exists m8j_users`,
		`drop table if exists posts`,
		`drop table if exists users`,
		`create table users (
			id bigint primary key,
			name varchar(64) not null,
			email varchar(64) null,
			created_at datetime(6) not null
		)`,
		`create table posts (
			id bigint primary key,
			author_id bigint not null,
			title varchar(64) not null
		)`,
	} {
		if _, err := db.Exec(ctx, stmt); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		_, _ = db.Exec(context.Background(), `drop table if exists posts`)
		_, _ = db.Exec(context.Background(), `drop table if exists users`)
	})
	u := schemafixture.Users
	p := schemafixture.Posts
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if _, err := mysql.Insert(u).Columns(u.ID, u.Name, u.CreatedAt).
		Values(int64(1), "bob", at).
		Values(int64(7), "alice", at).
		Exec(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, err := mysql.Insert(p).Columns(p.ID, p.AuthorID, p.Title).
		Values(int64(1), int64(7), "hello").
		Exec(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, err := mysql.Update(u).Set(u.Name, p.Title).From(p).Where(op.Eq(p.AuthorID, u.ID)).Exec(ctx, db); err != nil {
		t.Fatal(err)
	}
	alice, err := mysql.Select(u.Name).From(u).Where(op.Eq(u.ID, int64(7))).One(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if alice.Name != "hello" {
		t.Fatal(alice.Name)
	}
	bob, err := mysql.Select(u.Name).From(u).Where(op.Eq(u.ID, int64(1))).One(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if bob.Name != "bob" {
		t.Fatal(bob.Name)
	}
	if _, err := mysql.Delete(p).Using(u).Where(op.Eq(p.AuthorID, u.ID)).Exec(ctx, db); err != nil {
		t.Fatal(err)
	}
	left, err := mysql.Select(p.ID).From(p).All(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 0 {
		t.Fatalf("%+v", left)
	}
	built, err := mysql.Update(u).Set(u.Name, p.Title).From(p).LeftJoin(u, op.Eq(p.AuthorID, u.ID)).Where(op.Eq(p.AuthorID, u.ID)).SQL(mysql.Dialect{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, built.SQL, built.Params...); err == nil {
		t.Fatal("the same table appears twice; MySQL should reject it")
	}
}

func TestInsertSelectDuplicate(t *testing.T) {
	ctx := context.Background()
	db := openMySQL(t, ctx)
	if _, err := db.Exec(ctx, `drop table if exists users`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `create table users (
		id bigint primary key,
		name varchar(64) not null,
		email varchar(64) null,
		created_at datetime(6) null
	)`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(context.Background(), `drop table if exists users`)
	})
	u := schemafixture.Users
	if _, err := mysql.Insert(u).Columns(u.ID, u.Name, u.Email).Values(int64(1), "bob", "ada").Exec(ctx, db); err != nil {
		t.Fatal(err)
	}
	_, err := mysql.Insert(u).Columns(u.ID, u.Name).
		Select(mysql.Select(u.ID, u.Email).From(u).Where(op.Eq(u.ID, int64(1)))).
		OnDuplicateKeyUpdate().
		Set(u.Name, mysql.NewRow(u.Name)).
		Exec(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	one, err := mysql.Select(u.Name).From(u).Where(op.Eq(u.ID, int64(1))).One(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if one.Name != "ada" {
		t.Fatal(one.Name)
	}
}

func TestMatrixRuns(t *testing.T) {
	ctx := context.Background()
	db := openMySQL(t, ctx)
	for _, stmt := range []string{
		`drop table if exists posts`,
		`drop table if exists users`,
		`create table users (
			id bigint primary key,
			name varchar(64) not null,
			email varchar(64) null,
			created_at datetime(6) not null
		)`,
		`create table posts (
			id bigint primary key,
			author_id bigint not null,
			title varchar(64) not null
		)`,
	} {
		if _, err := db.Exec(ctx, stmt); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		_, _ = db.Exec(context.Background(), `drop table if exists posts`)
		_, _ = db.Exec(context.Background(), `drop table if exists users`)
	})
	u := schemafixture.Users
	p := schemafixture.Posts
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if _, err := mysql.Insert(u).Columns(u.ID, u.Name, u.CreatedAt).
		Values(int64(1), "bob", at).
		Values(int64(7), "alice", at).
		Values(int64(8), "alice", at).
		Exec(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, err := mysql.Insert(p).Columns(p.ID, p.AuthorID, p.Title).Values(int64(1), int64(7), "hello").Exec(ctx, db); err != nil {
		t.Fatal(err)
	}
	on := op.Eq(p.AuthorID, u.ID)

	t.Run("group having", func(t *testing.T) {
		rows, err := mysql.Select(u.Name).From(u).Where(op.Gt(u.ID, 0)).GroupBy(u.Name).Having(sql.Raw("count(*) > 1")).OrderBy(op.Asc(u.Name)).All(ctx, db)
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 1 || rows[0].Name != "alice" {
			t.Fatalf("%+v", rows)
		}
	})
	t.Run("right and cross join", func(t *testing.T) {
		right, err := mysql.Select(u.ID).From(u).RightJoin(p, on).All(ctx, db)
		if err != nil {
			t.Fatal(err)
		}
		if len(right) != 1 || right[0].ID != 7 {
			t.Fatalf("%+v", right)
		}
		cross, err := mysql.Select(u.ID).From(u).CrossJoin(p).All(ctx, db)
		if err != nil {
			t.Fatal(err)
		}
		if len(cross) != 3 {
			t.Fatalf("%d", len(cross))
		}
	})
	t.Run("distinct and sets", func(t *testing.T) {
		names, err := mysql.Select(u.Name).From(u).Distinct().OrderBy(op.Asc(u.Name)).All(ctx, db)
		if err != nil {
			t.Fatal(err)
		}
		if len(names) != 2 || names[0].Name != "alice" || names[1].Name != "bob" {
			t.Fatalf("%+v", names)
		}
		left := mysql.Select(u.ID, u.Name).From(u).Where(op.Eq(u.Name, "alice"))
		right := mysql.Select(u.ID, u.Name).From(u).Where(op.Eq(u.Name, "bob"))
		for _, q := range []query.Select[schemafixture.User]{
			left.UnionAll(mysql.Select(u.ID, u.Name).From(u).Where(op.Eq(u.Name, "alice"))),
			left.IntersectAll(left),
			left.ExceptAll(right),
		} {
			if _, err := q.All(ctx, db); err != nil {
				t.Fatal(err)
			}
		}
	})
	t.Run("with recursive", func(t *testing.T) {
		cte := mysql.Recursive("active", mysql.Select(u.Name).From(u))
		rows, err := mysql.Select(cte.Col(u.Name)).From(cte).With(cte).All(ctx, db)
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 3 {
			t.Fatalf("%d", len(rows))
		}
	})
	t.Run("not in not exists between", func(t *testing.T) {
		notIn, err := mysql.Select(u.Name).From(u).Where(op.NotIn(u.ID, mysql.Select(u.ID).From(u).Where(op.Eq(u.Name, "bob")))).All(ctx, db)
		if err != nil {
			t.Fatal(err)
		}
		if len(notIn) != 2 {
			t.Fatalf("%+v", notIn)
		}
		missing, err := mysql.Select(u.Name).From(u).Where(op.NotExists(mysql.Select(p.ID).From(p).Where(op.Eq(p.AuthorID, u.ID)))).OrderBy(op.Asc(u.ID)).All(ctx, db)
		if err != nil {
			t.Fatal(err)
		}
		if len(missing) != 2 || missing[0].Name != "bob" {
			t.Fatalf("%+v", missing)
		}
		between, err := mysql.Select(u.ID).From(u).Where(op.Between(u.ID, 7, 8)).All(ctx, db)
		if err != nil {
			t.Fatal(err)
		}
		if len(between) != 2 {
			t.Fatalf("%+v", between)
		}
		notLike, err := mysql.Select(u.ID).From(u).Where(op.NotILike(u.Name, "ALICE")).All(ctx, db)
		if err != nil {
			t.Fatal(err)
		}
		if len(notLike) != 1 || notLike[0].ID != 1 {
			t.Fatalf("%+v", notLike)
		}
	})
	t.Run("aggregates", func(t *testing.T) {
		n, err := mysql.Value[int64](op.Count()).From(u).One(ctx, db)
		if err != nil {
			t.Fatal(err)
		}
		if n != 3 {
			t.Fatal(n)
		}
		sum, err := mysql.Value[int64](op.Sum(u.ID)).From(u).One(ctx, db)
		if err != nil {
			t.Fatal(err)
		}
		if sum != 16 {
			t.Fatal(sum)
		}
	})
	t.Run("for update nowait", func(t *testing.T) {
		err := db.Tx(ctx, func(tx mist.Tx) error {
			rows, err := mysql.Select(u.ID).From(u).ForUpdate(query.RowLock{NoWait: true}).All(ctx, tx)
			if err != nil {
				return err
			}
			if len(rows) != 3 {
				return fmt.Errorf("locked %d rows", len(rows))
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	})
	t.Run("cte write", func(t *testing.T) {
		src := mysql.CTE("src", mysql.Select(p.ID, p.AuthorID, p.Title).From(p))
		if _, err := mysql.Insert(p).Ignore().Columns(p.ID, p.AuthorID, p.Title).Select(mysql.Select(src.Col(p.ID), src.Col(p.AuthorID), src.Col(p.Title)).From(src)).With(src).Exec(ctx, db); err != nil {
			t.Fatal(err)
		}
		if _, err := mysql.Insert(u).Columns(u.ID, u.Name, u.CreatedAt).Values(int64(20), "temp", at).Exec(ctx, db); err != nil {
			t.Fatal(err)
		}
		gone := mysql.CTE("gone", mysql.Select(u.ID).From(u).Where(op.Eq(u.ID, int64(20))))
		if _, err := mysql.Update(u).Set(u.Name, mysql.Select(p.Title).From(p).Where(op.Eq(p.ID, int64(1)))).Where(op.Eq(u.ID, int64(20))).Exec(ctx, db); err != nil {
			t.Fatal(err)
		}
		one, err := mysql.Select(u.Name).From(u).Where(op.Eq(u.ID, int64(20))).One(ctx, db)
		if err != nil {
			t.Fatal(err)
		}
		if one.Name != "hello" {
			t.Fatal(one.Name)
		}
		if _, err := mysql.Delete(u).Where(op.In(u.ID, mysql.Select(gone.Col(u.ID)).From(gone).Where(op.Eq(gone.Col(u.ID), int64(20))))).With(gone).Exec(ctx, db); err != nil {
			t.Fatal(err)
		}
		_, err = mysql.Select(u.ID).From(u).Where(op.Eq(u.ID, int64(20))).One(ctx, db)
		if !errors.Is(err, query.ErrNoRows) {
			t.Fatal(err)
		}
	})
}

func names(t *testing.T, ctx context.Context, db *mysql.DB, q query.Select[schemafixture.User]) []string {
	t.Helper()
	rows, err := q.All(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]string, len(rows))
	for i := range rows {
		out[i] = rows[i].Name
	}
	return out
}
