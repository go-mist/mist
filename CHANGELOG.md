# Changelog

## v0.2.0 — 2026-11-27

`migrate.Down` rolls back the latest applied migration. `migrate.DownTo` rolls back every applied migration newer than the named directory. An empty name rolls all of them back. `down.sql` is handwritten. A placeholder file is refused. Each down migration runs in its own transaction. `mist down` is the command.

The default migration table is now `mist.__mist_migrations`. v0.1.0 recorded applied migrations in `drizzle.__drizzle_migrations`. Set `Config.Schema` to `drizzle` and `Config.Table` to `__drizzle_migrations` to keep reading that table. The lock table is `__mist_lock`.

`Update` and `Delete` refuse to execute without `Where`. `AllRows` is the explicit full-table write. `Version` adds the increment and the comparison, and zero rows changed is `query.ErrVersionConflict`. `PreparedInsert.ExecBatch` repeats one prepared insert. `id.NewSnowflake` and `id.NewUUID` make values for `Values`. Generated tables have `As`. `logger.Timed` records driver elapsed time.

`page.Fetch` takes `page.Request` (`current`, `size`) and returns `page.Result` (`records`, `total`, `size`, `current`, `pages`). It runs `Select.RowCount`, then the same select with `limit` and `offset`. `NoCount` skips the count. `FetchAfter` continues after the last value of one ordered column and does not count. It does not parse SQL.

`dm.FindMany` loads `one`, `many`, and `through`. Dameng on the tested server has no `LATERAL` and no `JSON_ARRAYAGG`. Nested JSON is concatenated and collected with `LISTAGG`.

`mysql.FindMany` is a correlated subquery with `json_arrayagg`. The parent key is matched outside the derived table, which current MariaDB accepts. Ordered arrays are sorted from the `mfkord` value after the query returns.

Generate, encode, scan, and relation-filter errors that can name a column include the table, the column, and the expected type. Connect errors and migration-directory errors do not invent a table name.

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
