package bench

import (
	"github.com/xbmlz/mist/sql"
	"github.com/xbmlz/mist/sqlite"
)

// User is the row every library reads. The table is users(id, name, age).
type User struct {
	ID   int64
	Name string
	Age  int64
}

type usersTable struct {
	sqlite.TableBase
	ID   *sqlite.Col[User, int64]
	Name *sqlite.Col[User, string]
	Age  *sqlite.Col[User, int64]
}

func (usersTable) Scan(row sqlite.Row, dst *User) error {
	return row.Scan(&dst.ID, &dst.Name, &dst.Age)
}

func (usersTable) Row() *User { return nil }

func (t usersTable) Columns() []sql.Chunk {
	return []sql.Chunk{t.ID, t.Name, t.Age}
}

var users = &usersTable{
	TableBase: sqlite.TableBase{SQLName: "users"},
	ID: &sqlite.Col[User, int64]{
		Table: "users",
		Name:  "id",
		Ptr:   func(v *User) any { return &v.ID },
	},
	Name: &sqlite.Col[User, string]{
		Table: "users",
		Name:  "name",
		Ptr:   func(v *User) any { return &v.Name },
	},
	Age: &sqlite.Col[User, int64]{
		Table: "users",
		Name:  "age",
		Ptr:   func(v *User) any { return &v.Age },
	},
}
