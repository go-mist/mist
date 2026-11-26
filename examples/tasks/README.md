# tasks

A task list. The command line and the HTTP server use the Go standard library. `-driver` selects PostgreSQL or MySQL.

From this directory:

```
export DATABASE_URL=postgres://postgres:postgres@127.0.0.1:5432/app?sslmode=disable
go run . add "buy milk"
go run . list
go run . done 1
go run . delete 1
go run . serve
```

`-dsn` overrides the environment. PostgreSQL reads `DATABASE_URL`, then `DRIZZLE_PG_DSN`. MySQL reads `DRIZZLE_MYSQL_DSN`. `-migrations` defaults to `migrations` for PostgreSQL and `migrations/mysql` for MySQL.

`serve` listens on `:8080`.

| Method | Path | Body |
|---|---|---|
| `GET` | `/tasks` | |
| `POST` | `/tasks` | `{"title":"buy milk"}` |
| `POST` | `/tasks/{id}/done` | |
| `DELETE` | `/tasks/{id}` | |

`schema.go` is the table. `go generate` writes `schema_gen.go`. `migrations/20261123135846_init/` holds `up.sql`, `down.sql`, and `snapshot.json`. `migrations/mysql/20261124110000_init/` holds `up.sql` and `down.sql`. `down.sql` is still the placeholder, so `migrate down` refuses it until that file is replaced. Each `add` and `done` writes the task and a `task_events` row in one transaction. `delete` removes the events, then the task, and rolls back when the task is missing.
