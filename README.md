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

Guides will live in a separate docs site. License is not chosen yet.
