package schemafixture

import (
	"time"

	"github.com/go-mist/mist/dialect/pg"
)

//go:generate go run github.com/go-mist/mist/cmd/mist generate

// Note is not a table. It has no mist tag, so the generator must ignore it.
type Note struct {
	Text string
}

type User struct {
	ID        int64     `mist:"id,bigserial,pk"`
	Name      string    `mist:"name,text,notnull"`
	Email     *string   `mist:"email,text,unique"`
	CreatedAt time.Time `mist:"created_at,timestamptz,notnull,default=now()"`
}

func (User) Table() pg.TableMeta { return pg.TableMeta{Name: "users"} }

type Post struct {
	ID       int64  `mist:"id,bigserial,pk"`
	AuthorID int64  `mist:"author_id,bigint,notnull,references=users.id,ondelete=cascade"`
	Title    string `mist:"title,text,notnull"`
}

func (Post) Table() pg.TableMeta { return pg.TableMeta{Name: "posts"} }
