package schemafixture

import (
	"time"

	"github.com/xbmlz/drizzle-go/pg"
)

//go:generate go run github.com/xbmlz/drizzle-go/cmd/drizzle-go generate

// Note 不是表。没有 drizzle 标签，生成器必须忽略它。
type Note struct {
	Text string
}

type User struct {
	ID        int64     `drizzle:"id,bigserial,pk"`
	Name      string    `drizzle:"name,text,notnull"`
	Email     *string   `drizzle:"email,text,unique"`
	CreatedAt time.Time `drizzle:"created_at,timestamptz,notnull,default=now()"`
}

func (User) Table() pg.TableMeta { return pg.TableMeta{Name: "users"} }

type Post struct {
	ID       int64  `drizzle:"id,bigserial,pk"`
	AuthorID int64  `drizzle:"author_id,bigint,notnull,references=users.id,ondelete=cascade"`
	Title    string `drizzle:"title,text,notnull"`
}

func (Post) Table() pg.TableMeta { return pg.TableMeta{Name: "posts"} }
