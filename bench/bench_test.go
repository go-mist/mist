package bench

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/glebarez/sqlite"
	"github.com/jmoiron/sqlx"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/sqlitedialect"
	"github.com/xbmlz/mist/op"
	mist "github.com/xbmlz/mist/sqlite"
	"gorm.io/gorm"

	"github.com/xbmlz/mist/bench/ent"
	"github.com/xbmlz/mist/bench/ent/user"

	_ "github.com/glebarez/go-sqlite"
)

// The same statement for every library:
//
//	select id, name, age from users where age > ? order by id asc limit 20
//
// sqlx has no query builder. Its build benchmark only assembles the SQL string and the argument.
// ent's build benchmark uses ent's SQL builder. Scan and concurrent execution use the generated client.

const (
	rowsN = 200
	ageGT = 10
	limit = 20
	query = "select id, name, age from users where age > ? order by id asc limit ?"
)

type row struct {
	bun.BaseModel `bun:"table:users"`
	ID            int64  `db:"id" gorm:"column:id;primaryKey" bun:"id,pk"`
	Name          string `db:"name" gorm:"column:name" bun:"name"`
	Age           int64  `db:"age" gorm:"column:age" bun:"age"`
}

func (row) TableName() string { return "users" }

func seed(tb testing.TB) string {
	tb.Helper()
	path := filepath.Join(tb.TempDir(), "bench.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		tb.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`create table users (id integer primary key, name text not null, age integer not null)`); err != nil {
		tb.Fatal(err)
	}
	for i := 1; i <= rowsN; i++ {
		if _, err := db.Exec(`insert into users (id, name, age) values (?, ?, ?)`, i, "u", i%50); err != nil {
			tb.Fatal(err)
		}
	}
	if _, err := db.Exec(`pragma journal_mode=wal`); err != nil {
		tb.Fatal(err)
	}
	return path
}

var (
	sinkSQL  string
	sinkArgs []any
)

func BenchmarkBuild(b *testing.B) {
	b.Run("mist", func(b *testing.B) {
		d := mist.Dialect{}
		b.ReportAllocs()
		for b.Loop() {
			q, err := mist.Select(users.ID, users.Name, users.Age).
				From(users).
				Where(op.Gt(users.Age, ageGT)).
				OrderBy(op.Asc(users.ID)).
				Limit(limit).
				SQL(d)
			if err != nil {
				b.Fatal(err)
			}
			sinkSQL, sinkArgs = q.SQL, q.Params
		}
	})

	b.Run("sqlx", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			sinkSQL = query
			sinkArgs = []any{ageGT, limit}
		}
	})

	b.Run("gorm", func(b *testing.B) {
		db := openGorm(b, seed(b))
		b.ReportAllocs()
		for b.Loop() {
			sinkSQL = db.ToSQL(func(tx *gorm.DB) *gorm.DB {
				var out []row
				return tx.Model(&row{}).Where("age > ?", ageGT).Order("id asc").Limit(limit).Find(&out)
			})
		}
	})

	b.Run("ent", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			s := entsql.Dialect(dialect.SQLite).
				Select("id", "name", "age").
				From(entsql.Table("users")).
				Where(entsql.GT("age", ageGT)).
				OrderBy(entsql.Asc("id")).
				Limit(limit)
			sinkSQL, sinkArgs = s.Query()
		}
	})

	b.Run("bun", func(b *testing.B) {
		db := openBun(b, seed(b))
		b.ReportAllocs()
		for b.Loop() {
			q := db.NewSelect().Model((*row)(nil)).Column("id", "name", "age").
				Where("age > ?", ageGT).Order("id ASC").Limit(limit)
			sinkSQL = q.String()
		}
	})
}

func BenchmarkScan(b *testing.B) {
	path := seed(b)
	ctx := context.Background()

	b.Run("mist", func(b *testing.B) {
		db := openMist(b, path)
		b.ReportAllocs()
		for b.Loop() {
			out, err := mist.Select(users.ID, users.Name, users.Age).
				From(users).
				Where(op.Gt(users.Age, ageGT)).
				OrderBy(op.Asc(users.ID)).
				All(ctx, db)
			if err != nil || len(out) == 0 {
				b.Fatalf("%v rows=%d", err, len(out))
			}
		}
	})

	b.Run("sqlx", func(b *testing.B) {
		db := openSQLX(b, path)
		b.ReportAllocs()
		for b.Loop() {
			var out []row
			if err := db.Select(&out, "select id, name, age from users where age > ? order by id asc", ageGT); err != nil || len(out) == 0 {
				b.Fatalf("%v rows=%d", err, len(out))
			}
		}
	})

	b.Run("gorm", func(b *testing.B) {
		db := openGorm(b, path)
		b.ReportAllocs()
		for b.Loop() {
			var out []row
			if err := db.Where("age > ?", ageGT).Order("id asc").Find(&out).Error; err != nil || len(out) == 0 {
				b.Fatalf("%v rows=%d", err, len(out))
			}
		}
	})

	b.Run("ent", func(b *testing.B) {
		client := openEnt(b, path)
		b.ReportAllocs()
		for b.Loop() {
			out, err := client.User.Query().Where(user.AgeGT(ageGT)).Order(user.ByID()).All(ctx)
			if err != nil || len(out) == 0 {
				b.Fatalf("%v rows=%d", err, len(out))
			}
		}
	})

	b.Run("bun", func(b *testing.B) {
		db := openBun(b, path)
		b.ReportAllocs()
		for b.Loop() {
			var out []row
			err := db.NewSelect().Model(&out).Column("id", "name", "age").Where("age > ?", ageGT).Order("id ASC").Scan(ctx)
			if err != nil || len(out) == 0 {
				b.Fatalf("%v rows=%d", err, len(out))
			}
		}
	})
}

func BenchmarkConcurrent(b *testing.B) {
	path := seed(b)
	ctx := context.Background()

	b.Run("mist", func(b *testing.B) {
		db := openMist(b, path)
		b.ReportAllocs()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				out, err := mist.Select(users.ID, users.Name, users.Age).
					From(users).
					Where(op.Gt(users.Age, ageGT)).
					OrderBy(op.Asc(users.ID)).
					Limit(limit).
					All(ctx, db)
				if err != nil || len(out) != limit {
					b.Fatalf("%v rows=%d", err, len(out))
				}
			}
		})
	})

	b.Run("sqlx", func(b *testing.B) {
		db := openSQLX(b, path)
		b.ReportAllocs()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				var out []row
				if err := db.Select(&out, query, ageGT, limit); err != nil || len(out) != limit {
					b.Fatalf("%v rows=%d", err, len(out))
				}
			}
		})
	})

	b.Run("gorm", func(b *testing.B) {
		db := openGorm(b, path)
		b.ReportAllocs()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				var out []row
				err := db.Where("age > ?", ageGT).Order("id asc").Limit(limit).Find(&out).Error
				if err != nil || len(out) != limit {
					b.Fatalf("%v rows=%d", err, len(out))
				}
			}
		})
	})

	b.Run("ent", func(b *testing.B) {
		client := openEnt(b, path)
		b.ReportAllocs()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				out, err := client.User.Query().Where(user.AgeGT(ageGT)).Limit(limit).All(ctx)
				if err != nil || len(out) != limit {
					b.Fatalf("%v rows=%d", err, len(out))
				}
			}
		})
	})

	b.Run("bun", func(b *testing.B) {
		db := openBun(b, path)
		b.ReportAllocs()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				var out []row
				err := db.NewSelect().Model(&out).Column("id", "name", "age").
					Where("age > ?", ageGT).Order("id ASC").Limit(limit).Scan(ctx)
				if err != nil || len(out) != limit {
					b.Fatalf("%v rows=%d", err, len(out))
				}
			}
		})
	})
}

func TestScanOnce(t *testing.T) {
	path := seed(t)
	ctx := context.Background()
	db := openMist(t, path)
	out, err := mist.Select(users.ID, users.Name, users.Age).From(users).Where(op.Gt(users.Age, ageGT)).All(ctx, db)
	if err != nil || len(out) == 0 {
		t.Fatalf("%v rows=%d", err, len(out))
	}
}

func openMist(tb testing.TB, path string) *mist.DB {
	tb.Helper()
	db, err := mist.Open(context.Background(), path)
	if err != nil {
		tb.Fatal(err)
	}
	db.SQLDB().SetMaxOpenConns(8)
	tb.Cleanup(func() { _ = db.Close() })
	return db
}

func openSQLX(tb testing.TB, path string) *sqlx.DB {
	tb.Helper()
	db, err := sqlx.Open("sqlite", path)
	if err != nil {
		tb.Fatal(err)
	}
	db.SetMaxOpenConns(8)
	tb.Cleanup(func() { _ = db.Close() })
	return db
}

func openGorm(tb testing.TB, path string) *gorm.DB {
	tb.Helper()
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		tb.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		tb.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(8)
	tb.Cleanup(func() { _ = sqlDB.Close() })
	return db
}

func openBun(tb testing.TB, path string) *bun.DB {
	tb.Helper()
	sqldb, err := sql.Open("sqlite", path)
	if err != nil {
		tb.Fatal(err)
	}
	sqldb.SetMaxOpenConns(8)
	db := bun.NewDB(sqldb, sqlitedialect.New())
	tb.Cleanup(func() { _ = db.Close() })
	return db
}

func openEnt(tb testing.TB, path string) *ent.Client {
	tb.Helper()
	sqldb, err := sql.Open("sqlite", path)
	if err != nil {
		tb.Fatal(err)
	}
	sqldb.SetMaxOpenConns(8)
	drv := entsql.OpenDB(dialect.SQLite, sqldb)
	client := ent.NewClient(ent.Driver(drv))
	tb.Cleanup(func() { _ = client.Close() })
	return client
}
