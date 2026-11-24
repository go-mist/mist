package main

import (
	"context"
	"errors"
	"strings"

	"github.com/go-mist/mist"
	"github.com/go-mist/mist/migrate"
	"github.com/go-mist/mist/op"
	"github.com/go-mist/mist/pg"
	"github.com/go-mist/mist/query"
)

type taskStore interface {
	Add(ctx context.Context, title string) (Task, error)
	List(ctx context.Context) ([]Task, error)
	Done(ctx context.Context, id int64) error
	Delete(ctx context.Context, id int64) error
}

type dbStore struct {
	db *pg.DB
}

func openStore(ctx context.Context, dsn, migrations string) (*dbStore, error) {
	if dsn == "" {
		return nil, errors.New("set -dsn, DATABASE_URL, or DRIZZLE_PG_DSN")
	}
	db, err := pg.Open(ctx, dsn)
	if err != nil {
		return nil, err
	}
	if _, err := migrate.Run(ctx, pg.Migrator(db), migrate.Config{Folder: migrations}); err != nil {
		db.Close()
		return nil, err
	}
	return &dbStore{db: db}, nil
}

func (s *dbStore) Close() { s.db.Close() }

func (s *dbStore) Add(ctx context.Context, title string) (Task, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return Task{}, errTitle
	}
	var created Task
	err := s.db.Tx(ctx, func(tx mist.Tx) error {
		row, err := pg.Insert(Tasks).
			Columns(Tasks.Title).
			Values(title).
			Returning(Tasks.ID, Tasks.Title, Tasks.Done).
			One(ctx, tx)
		if err != nil {
			return err
		}
		_, err = pg.Insert(Events).
			Columns(Events.TaskID, Events.Action).
			Values(row.ID, "add").
			Exec(ctx, tx)
		created = row
		return err
	})
	return created, err
}

func (s *dbStore) List(ctx context.Context) ([]Task, error) {
	return pg.Select(Tasks.ID, Tasks.Title, Tasks.Done).
		From(Tasks).
		OrderBy(op.Asc(Tasks.ID)).
		All(ctx, s.db)
}

func (s *dbStore) Done(ctx context.Context, id int64) error {
	return s.db.Tx(ctx, func(tx mist.Tx) error {
		_, err := pg.Select(Tasks.ID).
			From(Tasks).
			Where(op.Eq(Tasks.ID, id)).
			ForUpdate().
			One(ctx, tx)
		if err != nil {
			return err
		}
		_, err = pg.Update(Tasks).
			Set(Tasks.Done, true).
			Where(op.Eq(Tasks.ID, id)).
			Exec(ctx, tx)
		if err != nil {
			return err
		}
		_, err = pg.Insert(Events).
			Columns(Events.TaskID, Events.Action).
			Values(id, "done").
			Exec(ctx, tx)
		return err
	})
}

func (s *dbStore) Delete(ctx context.Context, id int64) error {
	return s.db.Tx(ctx, func(tx mist.Tx) error {
		_, err := pg.Delete(Events).
			Where(op.Eq(Events.TaskID, id)).
			Exec(ctx, tx)
		if err != nil {
			return err
		}
		n, err := pg.Delete(Tasks).
			Where(op.Eq(Tasks.ID, id)).
			Exec(ctx, tx)
		if err != nil {
			return err
		}
		if n == 0 {
			return query.ErrNoRows
		}
		return nil
	})
}
