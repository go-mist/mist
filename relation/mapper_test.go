package relation

import (
	"reflect"
	"testing"
)

type mapUser struct {
	ID    int64
	Name  string
	Posts []mapPost
}

type mapPost struct {
	ID       int64
	Title    string
	AuthorID int64
	Comments []mapComment
	Author   *mapUser
}

type mapComment struct {
	ID     int64
	Body   string
	PostID int64
}

func TestFillNestedJSON(t *testing.T) {
	s := mapSchema(t)
	user := &mapUser{ID: 1, Name: "alice"}
	raw := []byte(`[{"id":2,"title":"b","author_id":1,"comments":[{"id":8,"body":"a","post_id":2},{"id":9,"body":"c","post_id":2}]},{"id":1,"title":"a","author_id":1,"comments":[]}]`)
	sel := Selection{Rels: []RelSelection{{
		Name:   "posts",
		Target: "posts",
		Many:   true,
		Inner: Selection{
			Columns: []string{"id", "title", "author_id"},
			Rels: []RelSelection{{
				Name:   "comments",
				Target: "comments",
				Many:   true,
				Inner:  Selection{Columns: []string{"id", "body", "post_id"}},
			}},
		},
	}}}
	if err := s.Fill("users", sel, user, [][]byte{raw}); err != nil {
		t.Fatal(err)
	}
	want := []mapPost{
		{ID: 2, Title: "b", AuthorID: 1, Comments: []mapComment{{ID: 8, Body: "a", PostID: 2}, {ID: 9, Body: "c", PostID: 2}}},
		{ID: 1, Title: "a", AuthorID: 1, Comments: []mapComment{}},
	}
	if !reflect.DeepEqual(user.Posts, want) {
		t.Fatalf("posts = %#v", user.Posts)
	}
}

func TestFillNullOne(t *testing.T) {
	s := mapSchema(t)
	post := &mapPost{ID: 3}
	sel := Selection{Rels: []RelSelection{{
		Name:   "author",
		Target: "users",
		Inner:  Selection{Columns: []string{"id", "name"}},
	}}}
	if err := s.Fill("posts", sel, post, [][]byte{[]byte("null")}); err != nil {
		t.Fatal(err)
	}
	if post.Author != nil {
		t.Fatalf("author = %#v", post.Author)
	}
}

func mapSchema(t *testing.T) *Schema {
	t.Helper()
	s, err := Define([]Table{
		{
			Name: "users",
			Columns: []Column{
				{Name: "id", Ptr: func(row any) any { return &row.(*mapUser).ID }},
				{Name: "name", Ptr: func(row any) any { return &row.(*mapUser).Name }},
			},
			New: func() any { return &mapUser{} },
			Relations: []Relation{{
				Name: "posts", Kind: KindMany, Target: "posts", From: []string{"id"}, To: []string{"author_id"},
				Init:  func(parent any) { parent.(*mapUser).Posts = []mapPost{} },
				Place: func(parent, child any) { p := parent.(*mapUser); p.Posts = append(p.Posts, *child.(*mapPost)) },
			}},
		},
		{
			Name: "posts",
			Columns: []Column{
				{Name: "id", Ptr: func(row any) any { return &row.(*mapPost).ID }},
				{Name: "title", Ptr: func(row any) any { return &row.(*mapPost).Title }},
				{Name: "author_id", Ptr: func(row any) any { return &row.(*mapPost).AuthorID }},
			},
			New: func() any { return &mapPost{} },
			Relations: []Relation{
				{
					Name: "comments", Kind: KindMany, Target: "comments", From: []string{"id"}, To: []string{"post_id"},
					Init:  func(parent any) { parent.(*mapPost).Comments = []mapComment{} },
					Place: func(parent, child any) { p := parent.(*mapPost); p.Comments = append(p.Comments, *child.(*mapComment)) },
				},
				{
					Name: "author", Kind: KindOne, Target: "users", From: []string{"author_id"}, To: []string{"id"}, Optional: true,
					Place: func(parent, child any) { parent.(*mapPost).Author = child.(*mapUser) },
				},
			},
		},
		{
			Name: "comments",
			Columns: []Column{
				{Name: "id", Ptr: func(row any) any { return &row.(*mapComment).ID }},
				{Name: "body", Ptr: func(row any) any { return &row.(*mapComment).Body }},
				{Name: "post_id", Ptr: func(row any) any { return &row.(*mapComment).PostID }},
			},
			New: func() any { return &mapComment{} },
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}
