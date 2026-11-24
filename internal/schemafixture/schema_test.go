package schemafixture

import (
	"testing"
	"time"

	"github.com/go-mist/mist/dialect/pg"
	"github.com/go-mist/mist/sql"
)

func TestUsersSQL(t *testing.T) {
	got, err := (pg.Dialect{}).SQLToQuery(sql.S(
		sql.Raw("select "), Users.ID, sql.Raw(", "), Posts.AuthorID,
		sql.Raw(" from "), Users,
	))
	if err != nil {
		t.Fatal(err)
	}
	want := `select "users"."id", "posts"."author_id" from "users"`
	if got.SQL != want || got.Params != nil {
		t.Fatalf("got %#v want %s", got, want)
	}
}

func TestUsersScan(t *testing.T) {
	email := "a@b.c"
	created := time.Unix(10, 0).UTC()
	var user User
	err := Users.Scan(stubRow{email: &email, created: created}, &user)
	if err != nil {
		t.Fatal(err)
	}
	if user.ID != 7 || user.Name != "alice" || user.Email == nil || *user.Email != email || !user.CreatedAt.Equal(created) {
		t.Fatalf("scan = %#v", user)
	}

	// Ptr must point at a field of this row. The scan plan does not use reflection.
	if got := Users.Name.Ptr(&user); got != &user.Name {
		t.Fatalf("Name.Ptr = %p, want %p", got, &user.Name)
	}
}

type stubRow struct {
	email   *string
	created time.Time
}

func (s stubRow) Scan(dest ...any) error {
	*dest[0].(*int64) = 7
	*dest[1].(*string) = "alice"
	*dest[2].(**string) = s.email
	*dest[3].(*time.Time) = s.created
	return nil
}
