package relation

import "testing"

func TestDefineInfersReverse(t *testing.T) {
	s, err := Define([]Table{
		{
			Name:    "users",
			Columns: []Column{{Name: "id"}, {Name: "name"}},
			Relations: []Relation{
				Many("posts", "posts", "id", "author_id"),
			},
		},
		{
			Name:    "posts",
			Columns: []Column{{Name: "id"}, {Name: "author_id"}},
			Relations: []Relation{
				OneOf("author", "users"),
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	rel, ok := s.tables["posts"].Relation("author")
	if !ok {
		t.Fatal("missing author")
	}
	if len(rel.From) != 1 || rel.From[0] != "author_id" || rel.To[0] != "id" {
		t.Fatalf("author from/to = %v / %v", rel.From, rel.To)
	}
	if !rel.Reversed {
		t.Fatal("borrowed where should be marked reversed")
	}
}

func TestDefineRejectsCollision(t *testing.T) {
	_, err := Define([]Table{{
		Name:      "users",
		Columns:   []Column{{Name: "name"}},
		Relations: []Relation{Many("name", "posts", "name", "id")},
	}})
	if err == nil {
		t.Fatal("expected collision")
	}
}

func TestDefinePartSkipsBareTables(t *testing.T) {
	s, err := DefinePart([]Table{
		{Name: "users", Columns: []Column{{Name: "id"}}, Relations: nil},
		{
			Name:      "posts",
			Columns:   []Column{{Name: "id"}, {Name: "author_id"}},
			Relations: []Relation{},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Get("users"); ok {
		t.Fatal("users should be omitted")
	}
	if _, ok := s.Get("posts"); !ok {
		t.Fatal("posts should stay")
	}
}
