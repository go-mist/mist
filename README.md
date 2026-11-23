# mist

SQL query builder for Go. PostgreSQL, MySQL, and SQLite. SQLite is compiled into the binary, so `CGO_ENABLED=0` is enough.

## Install

```
go get github.com/go-mist/mist
go install github.com/go-mist/mist/cmd/mist@latest
```

## Define a table

Columns live on a struct. The table name lives on `Table()`. `go generate` writes the column descriptors and the scan function next to the struct. It does not run your program.

```go
package schema

import (
	"time"

	"github.com/go-mist/mist/pg"
)

//go:generate mist generate

type User struct {
	ID        int64     `mist:"id,bigserial,pk"`
	Name      string    `mist:"name,text,notnull"`
	Email     *string   `mist:"email,text,unique"`
	CreatedAt time.Time `mist:"created_at,timestamptz,notnull,default=now()"`
}

func (User) Table() pg.TableMeta { return pg.TableMeta{Name: "users"} }
```

The generated value is `Users`. MySQL and SQLite use the same tags with `mysql.TableMeta` and `sqlite.TableMeta`.

## Open

```go
db, err := pg.Open(ctx, os.Getenv("DATABASE_URL"))
if err != nil {
	return err
}
defer db.Close()
```

MySQL takes a [go-sql-driver](https://github.com/go-sql-driver/mysql) DSN, such as `user:pass@tcp(127.0.0.1:3306)/app`. SQLite takes a file path or `:memory:`.

```go
db, err := mysql.Open(ctx, dsn)
db, err := sqlite.Open(ctx, "app.db")
```

The examples below use `pg`. The same calls exist on `mysql` and `sqlite`.

## Select

```go
user, err := pg.Select(Users.ID, Users.Name, Users.Email).
	From(Users).
	Where(op.Eq(Users.ID, int64(1))).
	One(ctx, db)

users, err := pg.Select(Users.ID, Users.Name).
	From(Users).
	Where(op.Gt(Users.ID, int64(0))).
	OrderBy(op.Asc(Users.ID)).
	Limit(100).
	All(ctx, db)
```

`One` returns `query.ErrNoRows` when the result is empty and `query.ErrManyRows` when it is not unique. `Take` returns the first row and ignores the rest.

## Insert

```go
n, err := pg.Insert(Users).
	Columns(Users.ID, Users.Name).
	Values(int64(1), "ada").
	Exec(ctx, db)
```

`n` is the number of rows affected. Columns you leave out stay at the database default.

## Update

```go
n, err := pg.Update(Users).
	Set(Users.Name, "bea").
	Where(op.Eq(Users.ID, int64(1))).
	Exec(ctx, db)
```

## Delete

```go
n, err := pg.Delete(Users).
	Where(op.Eq(Users.ID, int64(1))).
	Exec(ctx, db)
```

## Transactions

`Tx` commits when the function returns nil and rolls back when it returns an error. A `Tx` inside a `Tx` is a savepoint. Queries take the `*Tx` the same way they take `*DB`.

```go
err := db.Tx(ctx, func(tx *pg.Tx) error {
	_, err := pg.Insert(Users).
		Columns(Users.ID, Users.Name).
		Values(int64(2), "cam").
		Exec(ctx, tx)
	return err
})
```

## Migrate

Generate the next migration from the schema package, then apply the folder. Both commands are PostgreSQL. The migration table is `drizzle.__drizzle_migrations`. Each migration is a directory. `snapshot.json` is the schema after that migration. A later `ddl -write` adds a new directory and leaves the old one unchanged.

```
migrations/20261123135846_init/migration.sql
migrations/20261123135846_init/snapshot.json
```

```
mist ddl -dir . -migrations migrations -write -name init
mist migrate -migrations migrations
```

`DATABASE_URL` can be passed as `-dsn`, or set `DRIZZLE_PG_DSN`. From Go:

```go
applied, err := migrate.Run(ctx, migrate.UsePG(db), migrate.Config{
	Folder: "migrations",
})
```

`mist check` compares the folder with the snapshot. `mist push` prints the SQL that would change the database, and applies it with `--force`.

## Example

[`examples/tasks`](examples/tasks) is a task list. The command line and the HTTP server use the standard library. It generates the schema, applies the PostgreSQL migrations, and runs insert, select, update, delete, and transactions.

```
cd examples/tasks
export DATABASE_URL=postgres://postgres:postgres@127.0.0.1:5432/app?sslmode=disable
go run . add "buy milk"
go run . serve
```

## Benchmark

SQLite in one process, `CGO_ENABLED=0`, Intel i5-13500H. Insert one row, update one row, read one row by primary key, read 100 rows. Nanoseconds per operation, lower is better. mist and sqlx are the mean of five runs.

| | mist | sqlx | bun | ent | gorm |
|---|---:|---:|---:|---:|---:|
| Insert | 8156 | 7886 | 15711 | 15422 | 20800 |
| Update | 5755 | 5299 | 8018 | 20544 | 9461 |
| Read | 6972 | 6704 | 10802 | 10987 | 9717 |
| Read 100 | 67094 | 83694 | 75176 | 82110 | 101367 |

The mist table in that run uses pointer columns, so repeated statement text is reused. Generated columns are values, and that path builds the text on each call.

What is done, and what is not: [ROADMAP.md](ROADMAP.md). License is not chosen yet.

Thanks to [drizzle-orm](https://github.com/drizzle-team/drizzle-orm) for the shape of the query API.
