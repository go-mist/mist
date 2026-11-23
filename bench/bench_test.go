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
	gormlog "gorm.io/gorm/logger"

	"github.com/xbmlz/mist/bench/ent"
	"github.com/xbmlz/mist/bench/ent/user"

	_ "github.com/glebarez/go-sqlite"
)

// The same four operations as the usual Go ORM comparison
// (efectn/go-orm-benchmarks): insert one row, update one row,
// read one row by primary key, and read a 100-row slice.
// Each library is called through its own API inside the loop.
// The benchmark does not prepare a statement ahead of the timer.

const (
	rowsN  = 100
	sliceN = 100
)

type row struct {
	bun.BaseModel `bun:"table:users"`
	ID            int64  `db:"id" gorm:"column:id;primaryKey;autoIncrement" bun:"id,pk,autoincrement"`
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
	if _, err := db.Exec(`pragma synchronous=off`); err != nil {
		tb.Fatal(err)
	}
	return path
}

func BenchmarkInsert(b *testing.B) {
	ctx := context.Background()
	b.Run("mist", func(b *testing.B) {
		db := openMist(b, seed(b))
		b.ReportAllocs()
		for b.Loop() {
			_, err := mist.Insert(users).Columns(users.Name, users.Age).Values("n", int64(20)).Exec(ctx, db)
			if err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("sqlx", func(b *testing.B) {
		db := openSQLX(b, seed(b))
		b.ReportAllocs()
		for b.Loop() {
			if _, err := db.Exec(`insert into users (name, age) values (?, ?)`, "n", int64(20)); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("gorm", func(b *testing.B) {
		db := openGorm(b, seed(b))
		b.ReportAllocs()
		for b.Loop() {
			if err := db.Create(&row{Name: "n", Age: 20}).Error; err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("ent", func(b *testing.B) {
		client := openEnt(b, seed(b))
		b.ReportAllocs()
		for b.Loop() {
			if err := client.User.Create().SetName("n").SetAge(20).Exec(ctx); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("bun", func(b *testing.B) {
		db := openBun(b, seed(b))
		b.ReportAllocs()
		for b.Loop() {
			_, err := db.NewInsert().Model(&row{Name: "n", Age: 20}).Exec(ctx)
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkUpdate(b *testing.B) {
	ctx := context.Background()
	b.Run("mist", func(b *testing.B) {
		db := openMist(b, seed(b))
		b.ReportAllocs()
		for b.Loop() {
			_, err := mist.Update(users).Set(users.Name, "n").Set(users.Age, int64(21)).Where(op.Eq(users.ID, int64(1))).Exec(ctx, db)
			if err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("sqlx", func(b *testing.B) {
		db := openSQLX(b, seed(b))
		b.ReportAllocs()
		for b.Loop() {
			if _, err := db.Exec(`update users set name = ?, age = ? where id = ?`, "n", int64(21), int64(1)); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("gorm", func(b *testing.B) {
		db := openGorm(b, seed(b))
		b.ReportAllocs()
		for b.Loop() {
			err := db.Model(&row{}).Where("id = ?", int64(1)).Updates(map[string]any{"name": "n", "age": int64(21)}).Error
			if err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("ent", func(b *testing.B) {
		client := openEnt(b, seed(b))
		b.ReportAllocs()
		for b.Loop() {
			err := client.User.UpdateOneID(1).SetName("n").SetAge(21).Exec(ctx)
			if err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("bun", func(b *testing.B) {
		db := openBun(b, seed(b))
		b.ReportAllocs()
		for b.Loop() {
			_, err := db.NewUpdate().Model((*row)(nil)).Set("name = ?", "n").Set("age = ?", int64(21)).Where("id = ?", int64(1)).Exec(ctx)
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkRead(b *testing.B) {
	ctx := context.Background()
	b.Run("mist", func(b *testing.B) {
		db := openMist(b, seed(b))
		b.ReportAllocs()
		for b.Loop() {
			out, err := mist.Select(users.ID, users.Name, users.Age).From(users).Where(op.Eq(users.ID, int64(1))).Take(ctx, db)
			if err != nil || out.ID != 1 {
				b.Fatalf("%v id=%d", err, out.ID)
			}
		}
	})
	b.Run("sqlx", func(b *testing.B) {
		db := openSQLX(b, seed(b))
		b.ReportAllocs()
		for b.Loop() {
			var out row
			if err := db.Get(&out, `select id, name, age from users where id = ?`, int64(1)); err != nil || out.ID != 1 {
				b.Fatalf("%v id=%d", err, out.ID)
			}
		}
	})
	b.Run("gorm", func(b *testing.B) {
		db := openGorm(b, seed(b))
		b.ReportAllocs()
		for b.Loop() {
			var out row
			if err := db.Where("id = ?", int64(1)).Take(&out).Error; err != nil || out.ID != 1 {
				b.Fatalf("%v id=%d", err, out.ID)
			}
		}
	})
	b.Run("ent", func(b *testing.B) {
		client := openEnt(b, seed(b))
		b.ReportAllocs()
		for b.Loop() {
			out, err := client.User.Get(ctx, 1)
			if err != nil || out.ID != 1 {
				b.Fatalf("%v", err)
			}
		}
	})
	b.Run("bun", func(b *testing.B) {
		db := openBun(b, seed(b))
		b.ReportAllocs()
		for b.Loop() {
			var out row
			err := db.NewSelect().Model(&out).Where("id = ?", int64(1)).Limit(1).Scan(ctx)
			if err != nil || out.ID != 1 {
				b.Fatalf("%v id=%d", err, out.ID)
			}
		}
	})
}

func BenchmarkReadSlice(b *testing.B) {
	ctx := context.Background()
	b.Run("mist", func(b *testing.B) {
		db := openMist(b, seed(b))
		b.ReportAllocs()
		for b.Loop() {
			out, err := mist.Select(users.ID, users.Name, users.Age).From(users).Where(op.Gt(users.ID, int64(0))).Limit(sliceN).All(ctx, db)
			if err != nil || len(out) != sliceN {
				b.Fatalf("%v rows=%d", err, len(out))
			}
		}
	})
	b.Run("sqlx", func(b *testing.B) {
		db := openSQLX(b, seed(b))
		b.ReportAllocs()
		for b.Loop() {
			var out []row
			err := db.Select(&out, `select id, name, age from users where id > ? limit ?`, int64(0), sliceN)
			if err != nil || len(out) != sliceN {
				b.Fatalf("%v rows=%d", err, len(out))
			}
		}
	})
	b.Run("gorm", func(b *testing.B) {
		db := openGorm(b, seed(b))
		b.ReportAllocs()
		for b.Loop() {
			var out []row
			err := db.Where("id > ?", int64(0)).Limit(sliceN).Find(&out).Error
			if err != nil || len(out) != sliceN {
				b.Fatalf("%v rows=%d", err, len(out))
			}
		}
	})
	b.Run("ent", func(b *testing.B) {
		client := openEnt(b, seed(b))
		b.ReportAllocs()
		for b.Loop() {
			out, err := client.User.Query().Where(user.IDGT(0)).Limit(sliceN).All(ctx)
			if err != nil || len(out) != sliceN {
				b.Fatalf("%v rows=%d", err, len(out))
			}
		}
	})
	b.Run("bun", func(b *testing.B) {
		db := openBun(b, seed(b))
		b.ReportAllocs()
		for b.Loop() {
			var out []row
			err := db.NewSelect().Model(&out).Where("id > ?", int64(0)).Limit(sliceN).Scan(ctx)
			if err != nil || len(out) != sliceN {
				b.Fatalf("%v rows=%d", err, len(out))
			}
		}
	})
}

func TestRoundTrip(t *testing.T) {
	ctx := context.Background()
	path := seed(t)
	db := openMist(t, path)
	if _, err := mist.Insert(users).Columns(users.Name, users.Age).Values("n", int64(20)).Exec(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, err := mist.Update(users).Set(users.Name, "n").Where(op.Eq(users.ID, int64(1))).Exec(ctx, db); err != nil {
		t.Fatal(err)
	}
	one, err := mist.Select(users.ID, users.Name, users.Age).From(users).Where(op.Eq(users.ID, int64(1))).One(ctx, db)
	if err != nil || one.Name != "n" {
		t.Fatalf("%v %#v", err, one)
	}
	all, err := mist.Select(users.ID, users.Name, users.Age).From(users).Where(op.Gt(users.ID, int64(0))).Limit(sliceN).All(ctx, db)
	if err != nil || len(all) != sliceN {
		t.Fatalf("%v rows=%d", err, len(all))
	}
}

func tune(tb testing.TB, db *sql.DB) {
	tb.Helper()
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if _, err := db.Exec(`pragma synchronous=off`); err != nil {
		tb.Fatal(err)
	}
}

func openMist(tb testing.TB, path string) *mist.DB {
	tb.Helper()
	db, err := mist.Open(context.Background(), path)
	if err != nil {
		tb.Fatal(err)
	}
	tune(tb, db.SQLDB())
	tb.Cleanup(func() { _ = db.Close() })
	return db
}

func openSQLX(tb testing.TB, path string) *sqlx.DB {
	tb.Helper()
	db, err := sqlx.Open("sqlite", path)
	if err != nil {
		tb.Fatal(err)
	}
	tune(tb, db.DB)
	tb.Cleanup(func() { _ = db.Close() })
	return db
}

func openGorm(tb testing.TB, path string) *gorm.DB {
	tb.Helper()
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{
		SkipDefaultTransaction: true,
		PrepareStmt:            false,
		Logger:                 gormlog.Default.LogMode(gormlog.Silent),
	})
	if err != nil {
		tb.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		tb.Fatal(err)
	}
	tune(tb, sqlDB)
	tb.Cleanup(func() { _ = sqlDB.Close() })
	return db
}

func openBun(tb testing.TB, path string) *bun.DB {
	tb.Helper()
	sqldb, err := sql.Open("sqlite", path)
	if err != nil {
		tb.Fatal(err)
	}
	tune(tb, sqldb)
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
	tune(tb, sqldb)
	drv := entsql.OpenDB(dialect.SQLite, sqldb)
	client := ent.NewClient(ent.Driver(drv))
	tb.Cleanup(func() { _ = client.Close() })
	return client
}
