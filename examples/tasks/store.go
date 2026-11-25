package main

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/go-mist/mist"
	"github.com/go-mist/mist/dialect/mysql"
	"github.com/go-mist/mist/dialect/pg"
	"github.com/go-mist/mist/migrate"
	"github.com/go-mist/mist/op"
	"github.com/go-mist/mist/query"
)

type taskStore interface {
	Add(ctx context.Context, title string) (Task, error)
	List(ctx context.Context) ([]Task, error)
	Done(ctx context.Context, id int64) error
	Delete(ctx context.Context, id int64) error
}

type dbStore struct {
	db    mist.DB
	idSQL string
}

func openStore(ctx context.Context, driver, dsn, migrations string) (*dbStore, error) {
	if dsn == "" {
		return nil, errors.New("set -dsn or the driver environment variable")
	}
	switch driver {
	case "pg":
		db, err := pg.Open(ctx, dsn)
		if err != nil {
			return nil, err
		}
		if _, err := migrate.Run(ctx, pg.Migrator(db), migrate.Config{Folder: migrations}); err != nil {
			_ = db.Close()
			return nil, err
		}
		return &dbStore{db: db, idSQL: "select lastval()"}, nil
	case "mysql":
		db, err := mysql.Open(ctx, dsn)
		if err != nil {
			return nil, err
		}
		if _, err := migrate.Run(ctx, mysql.Migrator(db), migrate.Config{Folder: migrations}); err != nil {
			_ = db.Close()
			return nil, err
		}
		return &dbStore{db: db, idSQL: "select last_insert_id()"}, nil
	default:
		return nil, fmt.Errorf("driver %q is not pg or mysql", driver)
	}
}

func (s *dbStore) Close() { _ = s.db.Close() }

func (s *dbStore) Add(ctx context.Context, title string) (Task, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return Task{}, errTitle
	}
	var created Task
	err := s.db.Tx(ctx, func(tx mist.Tx) error {
		if _, err := query.NewInsert(Tasks).Columns(Tasks.Title).Values(title).Exec(ctx, tx); err != nil {
			return err
		}
		id, err := insertedID(ctx, tx, s.idSQL)
		if err != nil {
			return err
		}
		row, err := query.NewSelect(Tasks.ID, Tasks.Title, Tasks.Done).
			From(Tasks).
			Where(op.Eq(Tasks.ID, id)).
			One(ctx, tx)
		if err != nil {
			return err
		}
		if _, err := query.NewInsert(Events).Columns(Events.TaskID, Events.Action).Values(row.ID, "add").Exec(ctx, tx); err != nil {
			return err
		}
		created = row
		return nil
	})
	return created, err
}

func (s *dbStore) List(ctx context.Context) ([]Task, error) {
	return query.NewSelect(Tasks.ID, Tasks.Title, Tasks.Done).
		From(Tasks).
		OrderBy(op.Asc(Tasks.ID)).
		All(ctx, s.db)
}

func (s *dbStore) Done(ctx context.Context, id int64) error {
	return s.db.Tx(ctx, func(tx mist.Tx) error {
		if _, err := query.NewSelect(Tasks.ID).From(Tasks).Where(op.Eq(Tasks.ID, id)).ForUpdate().One(ctx, tx); err != nil {
			return err
		}
		if _, err := query.NewUpdate(Tasks).Set(Tasks.Done, true).Where(op.Eq(Tasks.ID, id)).Exec(ctx, tx); err != nil {
			return err
		}
		_, err := query.NewInsert(Events).Columns(Events.TaskID, Events.Action).Values(id, "done").Exec(ctx, tx)
		return err
	})
}

func (s *dbStore) Delete(ctx context.Context, id int64) error {
	return s.db.Tx(ctx, func(tx mist.Tx) error {
		if _, err := query.NewDelete(Events).Where(op.Eq(Events.TaskID, id)).Exec(ctx, tx); err != nil {
			return err
		}
		n, err := query.NewDelete(Tasks).Where(op.Eq(Tasks.ID, id)).Exec(ctx, tx)
		if err != nil {
			return err
		}
		if n == 0 {
			return query.ErrNoRows
		}
		return nil
	})
}

func insertedID(ctx context.Context, tx mist.Tx, query string) (int64, error) {
	rows, err := tx.Query(ctx, query)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return 0, err
		}
		return 0, errors.New("insert did not return an id")
	}
	var raw any
	if err := rows.Scan(&raw); err != nil {
		return 0, err
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	switch v := raw.(type) {
	case int64:
		return v, nil
	case int32:
		return int64(v), nil
	case uint64:
		return int64(v), nil
	case []byte:
		return strconv.ParseInt(string(v), 10, 64)
	case string:
		return strconv.ParseInt(v, 10, 64)
	default:
		return 0, fmt.Errorf("insert id has type %T", raw)
	}
}
