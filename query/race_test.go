package query_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/xbmlz/mist/internal/schemafixture"
	"github.com/xbmlz/mist/op"
	"github.com/xbmlz/mist/pg"
)

func TestBuilderConcurrentRender(t *testing.T) {
	q := pg.Select(schemafixture.Users.ID, schemafixture.Users.Name).
		From(schemafixture.Users).
		Where(op.Eq(schemafixture.Users.Name, "alice")).
		OrderBy(op.Desc(schemafixture.Users.ID)).
		Limit(10)
	var wg sync.WaitGroup
	errCh := make(chan error, 100)
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := q.SQL(pg.Dialect{})
			if err != nil {
				errCh <- err
				return
			}
			if got.SQL == "" {
				t.Error("empty sql")
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Error(err)
	}
}

func TestConcurrentReadWrite(t *testing.T) {
	read := pg.Select[schemafixture.User]().From(schemafixture.Users)
	write := pg.Insert(schemafixture.Users).
		Columns(schemafixture.Users.ID, schemafixture.Users.Name).
		Values(int64(1), "alice")
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			db := &fakeDB{rows: [][]any{{int64(1), "alice", (*string)(nil), time.Unix(1, 0).UTC()}}}
			if _, err := read.All(context.Background(), db); err != nil {
				t.Error(err)
			}
		}()
		go func() {
			defer wg.Done()
			db := &fakeDB{}
			if _, err := write.Exec(context.Background(), db); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
}
