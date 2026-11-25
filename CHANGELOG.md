# Changelog

## v0.1.0 — 2026-11-25

First tagged release of `github.com/go-mist/mist`. Versions before 1.0.0 can still change the API.

### Databases

PostgreSQL, MySQL, MariaDB, SQLite, Oracle, SQL Server, and Dameng. MariaDB uses the MySQL driver. HighGo and KingbaseES in PostgreSQL-compatible mode use the PostgreSQL driver. pgx does not speak SM3. HighGo has not been run on a HighGo server. One KingbaseES build, V008R003C002B0320, accepted the PostgreSQL statements that were tried.

### Migrations

A migration directory holds `up.sql`, `down.sql`, and `snapshot.json`. `migrate.Run` applies `up.sql`. The hash is the bytes of `up.sql`. `down.sql` is reserved and is not applied. `Config.FS` reads the same directory from an `fs.FS`, including `embed.FS`. `ddl -write` still writes the files on disk before the program is built.

### Boundaries

- `down.sql` is not executed.
- Dameng has no `FindMany`. Migration, `ddl`, and `push` have not been run on a Dameng server.
- MySQL and MariaDB commit each DDL statement, so a failed migration can leave earlier statements applied.
- Oracle and Dameng do not wrap DDL in a transaction.
- `returning` and `on conflict` stay errors. They are not rewritten as `OUTPUT`, `MERGE`, or `RETURNING INTO`.
