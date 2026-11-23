# mist

Go query builder in the shape of [drizzle-orm](https://github.com/drizzle-team/drizzle-orm). The SQL you write is the SQL that runs. Column types come from `go generate`.

PostgreSQL, MySQL, and SQLite. The SQLite driver is pure Go (`CGO_ENABLED=0`).

```
go get github.com/xbmlz/mist
```

```go
db, err := pg.Open(ctx, os.Getenv("DRIZZLE_PG_DSN"))
rows, err := pg.Select(Users.ID, Users.Name).
	From(Users).
	Where(op.Eq(Users.ID, int64(1))).
	All(ctx, db)
```

`Users` is generated from the table struct. MySQL is `mysql.Open`, SQLite is `sqlite.Open`.

What is done, and what is not: [ROADMAP.md](ROADMAP.md).

## Benchmark

SQLite in one process, `CGO_ENABLED=0`, Intel i5-13500H. The four calls match the usual Go ORM comparison: insert one row, update one row, read one row by primary key, read 100 rows. `go test -bench=. -benchtime=200ms` in `bench/`. Nanoseconds per operation, lower is better. mist and sqlx are the mean of five runs. bun, ent, and gorm are from the same machine and settings.

| | mist | sqlx | bun | ent | gorm |
|---|---:|---:|---:|---:|---:|
| Insert | 8156 | 7886 | 15711 | 15422 | 20800 |
| Update | 5755 | 5299 | 8018 | 20544 | 9461 |
| Read | 6972 | 6704 | 10802 | 10987 | 9717 |
| Read 100 | 67094 | 83694 | 75176 | 82110 | 101367 |

The mist table in this benchmark uses pointer columns, so repeated statement text is reused. Generated columns are values, and that path builds the text on each call.

Guides will live in a separate docs site. License is not chosen yet.
