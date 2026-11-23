# tasks

A task list. The command line and the HTTP server use the Go standard library. PostgreSQL is required, because migrations are PostgreSQL-only.

From this directory:

```
export DATABASE_URL=postgres://postgres:postgres@127.0.0.1:5432/app?sslmode=disable
go run . add "buy milk"
go run . list
go run . done 1
go run . delete 1
go run . serve
```

`-dsn` overrides `DATABASE_URL`. If both are empty, the program uses `DRIZZLE_PG_DSN`. `-migrations` defaults to `migrations`.

`serve` listens on `:8080`.

| Method | Path | Body |
|---|---|---|
| `GET` | `/tasks` | |
| `POST` | `/tasks` | `{"title":"buy milk"}` |
| `POST` | `/tasks/{id}/done` | |
| `DELETE` | `/tasks/{id}` | |

`schema.go` is the table. `go generate` writes `schema_gen.go`. The migration in `migrations/` came from `mist ddl`. Each `add` and `done` writes the task and a `task_events` row in one transaction. `delete` removes the events, then the task, and rolls back when the task is missing.
