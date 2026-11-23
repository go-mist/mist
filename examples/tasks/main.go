package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strconv"

	"github.com/go-mist/mist/query"
)

var errTitle = errors.New("title is required")

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "serve":
		os.Exit(serve(os.Args[2:]))
	case "add":
		os.Exit(add(os.Args[2:]))
	case "list":
		os.Exit(list(os.Args[2:]))
	case "done":
		os.Exit(done(os.Args[2:]))
	case "delete":
		os.Exit(delete(os.Args[2:]))
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `tasks is a small task list on PostgreSQL.

  tasks serve [-addr :8080] [-dsn dsn] [-migrations migrations]
  tasks add [-dsn dsn] [-migrations migrations] title
  tasks list [-dsn dsn] [-migrations migrations]
  tasks done [-dsn dsn] [-migrations migrations] id
  tasks delete [-dsn dsn] [-migrations migrations] id

The connection string is -dsn, else DATABASE_URL, else DRIZZLE_PG_DSN.
Startup applies the migrations directory with migrate.Run.
`)
}

type connFlags struct {
	dsn        string
	migrations string
}

func parseConn(args []string) (connFlags, []string) {
	fs := flag.NewFlagSet("tasks", flag.ExitOnError)
	c := connFlags{}
	fs.StringVar(&c.dsn, "dsn", "", "postgres connection string")
	fs.StringVar(&c.migrations, "migrations", "migrations", "migrations directory")
	_ = fs.Parse(args)
	if c.dsn == "" {
		c.dsn = os.Getenv("DATABASE_URL")
	}
	if c.dsn == "" {
		c.dsn = os.Getenv("DRIZZLE_PG_DSN")
	}
	return c, fs.Args()
}

func serve(args []string) int {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	addr := fs.String("addr", ":8080", "listen address")
	dsn := fs.String("dsn", "", "postgres connection string")
	migrations := fs.String("migrations", "migrations", "migrations directory")
	_ = fs.Parse(args)
	if *dsn == "" {
		*dsn = os.Getenv("DATABASE_URL")
	}
	if *dsn == "" {
		*dsn = os.Getenv("DRIZZLE_PG_DSN")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	store, err := openStore(ctx, *dsn, *migrations)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer store.Close()
	server := &http.Server{Addr: *addr, Handler: newMux(store)}
	go func() {
		<-ctx.Done()
		_ = server.Close()
	}()
	fmt.Fprintln(os.Stderr, "listening on", *addr)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

func add(args []string) int {
	c, rest := parseConn(args)
	if len(rest) == 0 {
		fmt.Fprintln(os.Stderr, errTitle)
		return 2
	}
	title := rest[0]
	for _, part := range rest[1:] {
		title += " " + part
	}
	ctx := context.Background()
	store, err := openStore(ctx, c.dsn, c.migrations)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer store.Close()
	task, err := store.Add(ctx, title)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Printf("%d\t%s\t%t\n", task.ID, task.Title, task.Done)
	return 0
}

func list(args []string) int {
	c, _ := parseConn(args)
	ctx := context.Background()
	store, err := openStore(ctx, c.dsn, c.migrations)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer store.Close()
	tasks, err := store.List(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	for _, task := range tasks {
		fmt.Printf("%d\t%s\t%t\n", task.ID, task.Title, task.Done)
	}
	return 0
}

func done(args []string) int {
	return withID(args, func(ctx context.Context, store *dbStore, id int64) error {
		return store.Done(ctx, id)
	})
}

func delete(args []string) int {
	return withID(args, func(ctx context.Context, store *dbStore, id int64) error {
		return store.Delete(ctx, id)
	})
}

func withID(args []string, fn func(context.Context, *dbStore, int64) error) int {
	c, rest := parseConn(args)
	if len(rest) != 1 {
		fmt.Fprintln(os.Stderr, "id is required")
		return 2
	}
	id, err := strconv.ParseInt(rest[0], 10, 64)
	if err != nil {
		fmt.Fprintln(os.Stderr, "bad id")
		return 2
	}
	ctx := context.Background()
	store, err := openStore(ctx, c.dsn, c.migrations)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer store.Close()
	if err := fn(ctx, store, id); err != nil {
		if errors.Is(err, query.ErrNoRows) {
			fmt.Fprintln(os.Stderr, "task not found")
			return 1
		}
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}
