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
	pgddl "github.com/go-mist/mist/ddl/pg"
	sqliteddl "github.com/go-mist/mist/ddl/sqlite"
	"github.com/go-mist/mist/diff"
	"github.com/go-mist/mist/introspect"
	"github.com/go-mist/mist/migrate"
	"github.com/go-mist/mist/mysql"
	"github.com/go-mist/mist/pg"
	"github.com/go-mist/mist/schema"
	"github.com/go-mist/mist/sqlite"
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
		ddl(os.Args[2:])
	case "check":
		check(os.Args[2:])
	case "migrate":
		apply(os.Args[2:])
	case "push":
		push(os.Args[2:])
	default:
		usage()
	}
}

func ddl(args []string) {
	fs := flag.NewFlagSet("ddl", flag.ExitOnError)
	dir := fs.String("dir", ".", "schema package directory")
	migrations := fs.String("migrations", "", "migration folder")
	write := fs.Bool("write", false, "write the next migration into -migrations")
	name := fs.String("name", "schema", "suffix of the new migration folder")
	dialectName := fs.String("dialect", "pg", "pg or sqlite")
	_ = fs.Parse(args)
	want := load(*dir)
	if *dialectName == "sqlite" {
		if *migrations != "" || *write {
			fail(fmt.Errorf("sqlite: migration folders, check, and push are still PostgreSQL-only. Column changes use ddl/sqlite.Render"))
		}
		sql, err := sqliteddl.Render(diff.Diff(want, schema.Schema{}), want)
		if err != nil {
			fail(err)
		}
		fmt.Print(sql)
		return
	}
	if *dialectName != "pg" {
		fail(fmt.Errorf("ddl: unknown dialect %s", *dialectName))
	}
	if *migrations == "" {
		if *write {
			fail(fmt.Errorf("ddl: -write requires -migrations"))
		}
		sql, err := pgddl.Render(diff.Diff(want, schema.Schema{}))
		if err != nil {
			fail(err)
		}
		fmt.Print(sql)
		return
	}
	if *write {
		written, err := pgddl.WriteMigration(*migrations, *name, time.Now().UTC(), want)
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
	sql, changes, err := pgddl.Plan(*migrations, want)
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
	changes, err := pgddl.Check(*migrations, load(*dir))
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
	fs := flag.NewFlagSet("migrate", flag.ExitOnError)
	migrations := fs.String("migrations", "", "migration folder")
	driver := fs.String("driver", "pg", "pg, mysql, or sqlite")
	dsn := fs.String("dsn", "", "connection string")
	schemaName := fs.String("schema", "", "schema of the migration table")
	table := fs.String("table", "", "migration table")
	_ = fs.Parse(args)
	if *migrations == "" {
		fail(fmt.Errorf("migrate: -migrations is required"))
	}
	if *dsn == "" {
		switch *driver {
		case "mysql":
			*dsn = os.Getenv("DRIZZLE_MYSQL_DSN")
		case "sqlite":
			*dsn = ""
		default:
			*dsn = os.Getenv("DRIZZLE_PG_DSN")
		}
	}
	if *dsn == "" {
		fail(fmt.Errorf("migrate: -dsn is required"))
	}
	ctx := context.Background()
	conn, closeConn, err := openMigrate(ctx, *driver, *dsn)
	if err != nil {
		fail(err)
	}
	defer closeConn()
	applied, err := migrate.Run(ctx, conn, migrate.Config{
		Folder: *migrations,
		Schema: *schemaName,
		Table:  *table,
	})
	if err != nil {
		fail(err)
	}
	fmt.Println(applied)
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
	default:
		return nil, nil, fmt.Errorf("migrate: driver %q is not pg, mysql, or sqlite", driver)
	}
}

func push(args []string) {
	fs := flag.NewFlagSet("push", flag.ExitOnError)
	dir := fs.String("dir", ".", "schema package directory")
	dsn := fs.String("dsn", os.Getenv("DRIZZLE_PG_DSN"), "postgres dsn")
	schemaName := fs.String("schema", "public", "schema to read and apply")
	force := fs.Bool("force", false, "apply without prompting")
	_ = fs.Parse(args)
	if *dsn == "" {
		fail(fmt.Errorf("push: -dsn or DRIZZLE_PG_DSN is required"))
	}
	if !schemaIdent.MatchString(*schemaName) {
		fail(fmt.Errorf("push: schema %q is not a safe identifier", *schemaName))
	}
	ctx := context.Background()
	db, err := pg.Open(ctx, *dsn)
	if err != nil {
		fail(err)
	}
	defer db.Close()
	actual, err := introspect.Read(ctx, db, *schemaName)
	if err != nil {
		if !strings.Contains(err.Error(), "does not exist") {
			fail(err)
		}
		actual = schema.Schema{}
	}
	want := load(*dir)
	changes := diff.Diff(want, actual)
	sqlText, err := pgddl.Render(changes)
	if err != nil {
		fail(err)
	}
	fmt.Print(sqlText)
	if sqlText == "" {
		fmt.Fprintln(os.Stderr, "no changes")
		return
	}
	warnDestructive(changes)
	if !acceptPush(*force) {
		fmt.Fprintln(os.Stderr, "not applied")
		return
	}
	quoted := pg.Dialect{}.EscapeName(*schemaName)
	err = db.Tx(ctx, func(tx mist.Tx) error {
		if _, err := tx.Exec(ctx, "create schema if not exists "+quoted); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "set local search_path to "+quoted); err != nil {
			return err
		}
		return pgddl.Execute(ctx, tx, sqlText)
	})
	if err != nil {
		fail(err)
	}
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
	fmt.Fprintln(os.Stderr, "       mist migrate -migrations dir [-dsn dsn] [-schema drizzle]")
	fmt.Fprintln(os.Stderr, "       mist push [-dir .] [-schema public] [-dsn dsn] [--force]")
	os.Exit(2)
}
