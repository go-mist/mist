//go:build integration

package pg_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/go-mist/mist/internal/schemafixture"
	"github.com/go-mist/mist/internal/testdb"
	"github.com/go-mist/mist/op"
	"github.com/go-mist/mist/pg"
)

func TestConcurrentCRUD(t *testing.T) {
	ctx := context.Background()
	db := openRaceDB(t, ctx)
	resetUsers(t, ctx, db)

	const n = 100
	var wg sync.WaitGroup
	errCh := make(chan error, n*2)
	for i := 1; i <= n; i++ {
		id := int64(i)
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := pg.Insert(schemafixture.Users).
				Columns(schemafixture.Users.ID, schemafixture.Users.Name).
				Values(id, fmt.Sprintf("u%d", id)).
				Exec(ctx, db); err != nil {
				errCh <- err
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatal(err)
	}

	errCh = make(chan error, n)
	var wg2 sync.WaitGroup
	for i := 1; i <= n; i++ {
		id := int64(i)
		wg2.Add(1)
		go func() {
			defer wg2.Done()
			row, err := pg.Select(schemafixture.Users.Name).
				From(schemafixture.Users).
				Where(op.Eq(schemafixture.Users.ID, id)).
				One(ctx, db)
			if err != nil {
				errCh <- err
				return
			}
			if row.Name != fmt.Sprintf("u%d", id) {
				errCh <- fmt.Errorf("id %d name %s", id, row.Name)
			}
		}()
	}
	wg2.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatal(err)
	}
}

func openRaceDB(t *testing.T, ctx context.Context) *pg.DB {
	t.Helper()
	dsn := testdb.PostgresDSN(t)
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.MaxConns = 32
	db, err := pg.OpenConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	return db
}
