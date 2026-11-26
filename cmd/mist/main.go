// Command mist is the library's command entry point.
// generate writes Go descriptors. ddl prints or writes the next migration. migrate applies a directory. check compares against the snapshot.
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/go-mist/mist"
	"github.com/go-mist/mist/codegen"
	"github.com/go-mist/mist/ddl"
	dmddl "github.com/go-mist/mist/ddl/dm"
	mssqlddl "github.com/go-mist/mist/ddl/mssql"
	mysqlddl "github.com/go-mist/mist/ddl/mysql"
	oracleddl "github.com/go-mist/mist/ddl/oracle"
	pgddl "github.com/go-mist/mist/ddl/pg"
	sqliteddl "github.com/go-mist/mist/ddl/sqlite"
	"github.com/go-mist/mist/dialect/dm"
	"github.com/go-mist/mist/dialect/mssql"
	"github.com/go-mist/mist/dialect/mysql"
	"github.com/go-mist/mist/dialect/oracle"
	"github.com/go-mist/mist/dialect/pg"
	"github.com/go-mist/mist/dialect/sqlite"
	"github.com/go-mist/mist/diff"
	"github.com/go-mist/mist/introspect"
	"github.com/go-mist/mist/migrate"
	"github.com/go-mist/mist/query"
	"github.com/go-mist/mist/schema"
)

var schemaIdent = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	switch os.Args[1] {
	case "generate":
		dir := dirFlag("generate")
		if err := codegen.Generate(dir); err != nil {
			fail(err)
		}
	case "ddl":
		ddlCmd(os.Args[2:])
	case "check":
		check(os.Args[2:])
	case "migrate":
		apply(os.Args[2:])
	case "down":
		downCmd(os.Args[2:])
	case "push":
		push(os.Args[2:])
	default:
		usage()
	}
}

func ddlCmd(args []string) {
	fs := flag.NewFlagSet("ddl", flag.ExitOnError)
	dir := fs.String("dir", ".", "schema package directory")
	migrations := fs.String("migrations", "", "migration folder")
	write := fs.Bool("write", false, "write the next migration into -migrations")
	name := fs.String("name", "schema", "suffix of the new migration folder")
	dialectName := fs.String("dialect", "pg", "pg, mysql, sqlite, oracle, mssql, or dm")
	_ = fs.Parse(args)
	want := load(*dir)
	render, err := renderer(*dialectName, want)
	if err != nil {
		fail(err)
	}
	if *migrations == "" {
		if *write {
			fail(fmt.Errorf("ddl: -write requires -migrations"))
		}
		sql, err := render(diff.Diff(want, schema.Schema{}))
		if err != nil {
			fail(err)
		}
		fmt.Print(sql)
		return
	}
	if *write {
		written, err := ddl.WriteMigration(*migrations, *name, time.Now().UTC(), want, render)
		if err != nil {
			fail(err)
		}
		warnDestructive(written.Changes)
		if written.Name == "" {
			fmt.Fprintln(os.Stderr, "no changes")
			return
		}
		fmt.Println(written.Name)
		return
	}
	sql, changes, err := ddl.Plan(*migrations, want, render)
	if err != nil {
		fail(err)
	}
	warnDestructive(changes)
	if sql == "" {
		fmt.Fprintln(os.Stderr, "no changes")
		return
	}
	fmt.Print(sql)
}

func check(args []string) {
	fs := flag.NewFlagSet("check", flag.ExitOnError)
	dir := fs.String("dir", ".", "schema package directory")
	migrations := fs.String("migrations", "", "migration folder")
	_ = fs.Parse(args)
	if *migrations == "" {
		fail(fmt.Errorf("check: -migrations is required"))
	}
	changes, err := ddl.Check(*migrations, load(*dir))
	if err != nil {
		fail(err)
	}
	if len(changes) == 0 {
		return
	}
	for _, change := range changes {
		fmt.Fprintln(os.Stderr, change.Summary)
	}
	os.Exit(1)
}

func apply(args []string) {
	flags := parseMigrate("migrate", args)
	ctx := context.Background()
	conn, closeConn, err := openMigrate(ctx, flags.driver, flags.dsn)
	if err != nil {
		fail(err)
	}
	defer closeConn()
	applied, err := migrate.Run(ctx, conn, flags.config())
	if err != nil {
		fail(err)
	}
	fmt.Println(applied)
}

func downCmd(args []string) {
	fs := flag.NewFlagSet("down", flag.ExitOnError)
	migrations := fs.String("migrations", "", "migration folder")
	driver := fs.String("driver", "pg", "pg, mysql, sqlite, oracle, mssql, or dm")
	dsn := fs.String("dsn", "", "connection string")
	schemaName := fs.String("schema", "", "schema of the migration table")
	table := fs.String("table", "", "migration table")
	to := fs.String("to", "", "roll back until this migration remains applied")
	all := fs.Bool("all", false, "roll back every applied migration")
	_ = fs.Parse(args)
	flags, err := finishMigrate(*migrations, *driver, *dsn, *schemaName, *table)
	if err != nil {
		fail(err)
	}
	if *all && *to != "" {
		fail(fmt.Errorf("down: -all and -to cannot both be set"))
	}
	ctx := context.Background()
	conn, closeConn, err := openMigrate(ctx, flags.driver, flags.dsn)
	if err != nil {
		fail(err)
	}
	defer closeConn()
	var n int
	if *all || *to != "" {
		n, err = migrate.DownTo(ctx, conn, flags.config(), *to)
	} else {
		n, err = migrate.Down(ctx, conn, flags.config())
	}
	if err != nil {
		fail(err)
	}
	fmt.Println(n)
}

type migrateOpts struct {
	migrations string
	driver     string
	dsn        string
	schema     string
	table      string
}

func (o migrateOpts) config() migrate.Config {
	return migrate.Config{Folder: o.migrations, Schema: o.schema, Table: o.table}
}

func parseMigrate(name string, args []string) migrateOpts {
	fs := flag.NewFlagSet(name, flag.ExitOnError)
	migrations := fs.String("migrations", "", "migration folder")
	driver := fs.String("driver", "pg", "pg, mysql, sqlite, oracle, mssql, or dm")
	dsn := fs.String("dsn", "", "connection string")
	schemaName := fs.String("schema", "", "schema of the migration table")
	table := fs.String("table", "", "migration table")
	_ = fs.Parse(args)
	opts, err := finishMigrate(*migrations, *driver, *dsn, *schemaName, *table)
	if err != nil {
		fail(err)
	}
	return opts
}

func finishMigrate(migrations, driver, dsn, schemaName, table string) (migrateOpts, error) {
	if migrations == "" {
		return migrateOpts{}, fmt.Errorf("migrate: -migrations is required")
	}
	if dsn == "" {
		switch driver {
		case "mysql":
			dsn = os.Getenv("DRIZZLE_MYSQL_DSN")
		case "oracle":
			dsn = os.Getenv("ORACLE_DSN")
		case "mssql":
			dsn = os.Getenv("MSSQL_DSN")
		case "dm":
			dsn = os.Getenv("DM_DSN")
		case "sqlite":
			dsn = ""
		default:
			dsn = os.Getenv("DRIZZLE_PG_DSN")
		}
	}
	if dsn == "" {
		return migrateOpts{}, fmt.Errorf("migrate: -dsn is required")
	}
	return migrateOpts{migrations: migrations, driver: driver, dsn: dsn, schema: schemaName, table: table}, nil
}

func openMigrate(ctx context.Context, driver, dsn string) (migrate.Conn, func() error, error) {
	switch driver {
	case "pg", "postgres":
		db, err := pg.Open(ctx, dsn)
		if err != nil {
			return nil, nil, err
		}
		return pg.Migrator(db), db.Close, nil
	case "mysql":
		db, err := mysql.Open(ctx, dsn)
		if err != nil {
			return nil, nil, err
		}
		return mysql.Migrator(db), db.Close, nil
	case "sqlite":
		db, err := sqlite.Open(ctx, dsn)
		if err != nil {
			return nil, nil, err
		}
		return sqlite.Migrator(db), db.Close, nil
	case "oracle":
		db, err := oracle.Open(ctx, dsn)
		if err != nil {
			return nil, nil, err
		}
		return oracle.Migrator(db), db.Close, nil
	case "mssql":
		db, err := mssql.Open(ctx, dsn)
		if err != nil {
			return nil, nil, err
		}
		return mssql.Migrator(db), db.Close, nil
	case "dm":
		db, err := dm.Open(ctx, dsn)
		if err != nil {
			return nil, nil, err
		}
		return dm.Migrator(db), db.Close, nil
	default:
		return nil, nil, fmt.Errorf("migrate: driver %q is not pg, mysql, sqlite, oracle, mssql, or dm", driver)
	}
}

func push(args []string) {
	fs := flag.NewFlagSet("push", flag.ExitOnError)
	dir := fs.String("dir", ".", "schema package directory")
	driver := fs.String("driver", "pg", "pg, mysql, sqlite, oracle, mssql, or dm")
	dsn := fs.String("dsn", "", "connection string")
	schemaName := fs.String("schema", "", "schema or database to read and apply")
	force := fs.Bool("force", false, "apply without prompting")
	_ = fs.Parse(args)
	if *dsn == "" {
		switch *driver {
		case "mysql":
			*dsn = os.Getenv("DRIZZLE_MYSQL_DSN")
		case "oracle":
			*dsn = os.Getenv("ORACLE_DSN")
		case "mssql":
			*dsn = os.Getenv("MSSQL_DSN")
		case "dm":
			*dsn = os.Getenv("DM_DSN")
		case "sqlite":
			*dsn = ""
		default:
			*dsn = os.Getenv("DRIZZLE_PG_DSN")
		}
	}
	if *dsn == "" {
		fail(fmt.Errorf("push: -dsn is required"))
	}
	if *schemaName != "" && !schemaIdent.MatchString(*schemaName) {
		fail(fmt.Errorf("push: schema %q is not a safe identifier", *schemaName))
	}
	ctx := context.Background()
	want := load(*dir)
	if err := pushApply(ctx, *driver, *dsn, *schemaName, want, *force); err != nil {
		fail(err)
	}
}

func renderer(name string, want schema.Schema) (ddl.Render, error) {
	switch name {
	case "", "pg", "postgres":
		return pgddl.Render, nil
	case "mysql":
		return mysqlddl.Render, nil
	case "oracle":
		return oracleddl.Render, nil
	case "mssql":
		return mssqlddl.Render, nil
	case "dm":
		return dmddl.Render, nil
	case "sqlite":
		return func(changes []diff.Change) (string, error) {
			return sqliteddl.Render(changes, want)
		}, nil
	default:
		return nil, fmt.Errorf("ddl: dialect %q is not pg, mysql, sqlite, oracle, mssql, or dm", name)
	}
}

func pushApply(ctx context.Context, driver, dsn, schemaName string, want schema.Schema, force bool) error {
	render, err := renderer(driver, want)
	if err != nil {
		return err
	}
	switch driver {
	case "", "pg", "postgres":
		if schemaName == "" {
			schemaName = "public"
		}
		db, err := pg.Open(ctx, dsn)
		if err != nil {
			return err
		}
		defer db.Close()
		return pushOne(ctx, driver, schemaName, want, render, force, db, func() (schema.Schema, error) {
			return introspect.Read(ctx, db, schemaName)
		}, func(sqlText string) error {
			quoted := pg.Dialect{}.EscapeName(schemaName)
			return db.Tx(ctx, func(tx mist.Tx) error {
				if _, err := tx.Exec(ctx, "create schema if not exists "+quoted); err != nil {
					return err
				}
				if _, err := tx.Exec(ctx, "set local search_path to "+quoted); err != nil {
					return err
				}
				return ddl.Execute(ctx, tx, sqlText)
			})
		})
	case "mysql":
		db, err := mysql.Open(ctx, dsn)
		if err != nil {
			return err
		}
		defer db.Close()
		if schemaName == "" {
			if err := introspectQueryString(ctx, db, &schemaName, "select database()"); err != nil {
				return err
			}
		}
		return pushOne(ctx, driver, schemaName, want, render, force, db, func() (schema.Schema, error) {
			return introspect.ReadMySQL(ctx, db, schemaName)
		}, func(sqlText string) error {
			return ddl.Execute(ctx, db, sqlText)
		})
	case "sqlite":
		db, err := sqlite.Open(ctx, dsn)
		if err != nil {
			return err
		}
		defer db.Close()
		return pushOne(ctx, driver, schemaName, want, render, force, db, func() (schema.Schema, error) {
			return introspect.ReadSQLite(ctx, db)
		}, func(sqlText string) error {
			return db.Tx(ctx, func(tx mist.Tx) error {
				return ddl.Execute(ctx, tx, sqlText)
			})
		})
	case "oracle":
		db, err := oracle.Open(ctx, dsn)
		if err != nil {
			return err
		}
		defer db.Close()
		return pushOne(ctx, driver, schemaName, want, render, force, db, func() (schema.Schema, error) {
			return introspect.ReadOracle(ctx, db, schemaName)
		}, func(sqlText string) error {
			return ddl.Execute(ctx, db, sqlText)
		})
	case "mssql":
		db, err := mssql.Open(ctx, dsn)
		if err != nil {
			return err
		}
		defer db.Close()
		if schemaName == "" {
			schemaName = "dbo"
		}
		return pushOne(ctx, driver, schemaName, want, render, force, db, func() (schema.Schema, error) {
			return introspect.ReadMSSQL(ctx, db, schemaName)
		}, func(sqlText string) error {
			return db.Tx(ctx, func(tx mist.Tx) error {
				return ddl.Execute(ctx, tx, sqlText)
			})
		})
	case "dm":
		db, err := dm.Open(ctx, dsn)
		if err != nil {
			return err
		}
		defer db.Close()
		return pushOne(ctx, driver, schemaName, want, render, force, db, func() (schema.Schema, error) {
			return introspect.ReadDM(ctx, db, schemaName)
		}, func(sqlText string) error {
			return ddl.Execute(ctx, db, sqlText)
		})
	default:
		return fmt.Errorf("push: driver %q is not pg, mysql, sqlite, oracle, mssql, or dm", driver)
	}
}

func pushOne(ctx context.Context, driver, schemaName string, want schema.Schema, render ddl.Render, force bool, _ any, read func() (schema.Schema, error), apply func(string) error) error {
	actual, err := read()
	if err != nil {
		if !strings.Contains(err.Error(), "does not exist") {
			return err
		}
		actual = schema.Schema{}
	}
	changes := diff.Diff(want, actual)
	sqlText, err := render(changes)
	if err != nil {
		return err
	}
	fmt.Print(sqlText)
	if sqlText == "" {
		fmt.Fprintln(os.Stderr, "no changes")
		return nil
	}
	warnDestructive(changes)
	if !acceptPush(force) {
		fmt.Fprintln(os.Stderr, "not applied")
		return nil
	}
	_ = driver
	_ = schemaName
	_ = ctx
	return apply(sqlText)
}

func introspectQueryString(ctx context.Context, db interface {
	Query(ctx context.Context, sql string, args ...any) (query.Rows, error)
}, dest *string, sql string) error {
	rows, err := db.Query(ctx, sql)
	if err != nil {
		return err
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return err
		}
		return fmt.Errorf("introspect: missing database")
	}
	return rows.Scan(dest)
}

func acceptPush(force bool) bool {
	if force {
		return true
	}
	if !isTerminal(os.Stdin) {
		return false
	}
	fmt.Fprint(os.Stderr, "apply to the database? [y/N] ")
	answer, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		return false
	}
	return pgddl.Confirm(answer)
}

func isTerminal(file *os.File) bool {
	info, err := file.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

func load(dir string) schema.Schema {
	want, err := codegen.LoadSchema(dir)
	if err != nil {
		fail(err)
	}
	return want
}

func warnDestructive(changes []diff.Change) {
	for _, change := range changes {
		if change.Destructive {
			fmt.Fprintf(os.Stderr, "destructive change: %s\n", change.Summary)
		}
	}
}

func dirFlag(name string) string {
	fs := flag.NewFlagSet(name, flag.ExitOnError)
	dir := fs.String("dir", ".", "schema package directory")
	_ = fs.Parse(os.Args[2:])
	return *dir
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: mist generate [-dir .]")
	fmt.Fprintln(os.Stderr, "       mist ddl [-dir .] [-dialect pg] [-migrations dir] [-write] [-name schema]")
	fmt.Fprintln(os.Stderr, "       mist check -dir . -migrations dir")
	fmt.Fprintln(os.Stderr, "       mist migrate -migrations dir [-driver pg] [-dsn dsn] [-schema mist] [-table __mist_migrations]")
	fmt.Fprintln(os.Stderr, "       mist down -migrations dir [-driver pg] [-dsn dsn] [-schema mist] [-table __mist_migrations] [-to name | -all]")
	fmt.Fprintln(os.Stderr, "       mist push [-dir .] [-driver pg] [-schema name] [-dsn dsn] [--force]")
	os.Exit(2)
}
