package mysql

import (
	"testing"

	"github.com/go-mist/mist/relation"
)

func TestSortManyJSON(t *testing.T) {
	raw := []byte(`[{"id":11,"comments":[{"id":101,"mfkord":2},{"id":100,"mfkord":1}],"mfkord":1},{"id":10,"comments":[],"mfkord":2}]`)
	inner := relation.Selection{
		Columns: []string{"id"},
		Rels: []relation.RelSelection{{
			Name:  "comments",
			Many:  true,
			Inner: relation.Selection{Columns: []string{"id"}},
		}},
	}
	got, err := sortManyJSON(raw, inner)
	if err != nil {
		t.Fatal(err)
	}
	want := `[{"comments":[{"id":100},{"id":101}],"id":11},{"comments":[],"id":10}]`
	if string(got) != want {
		t.Fatalf("got %s", got)
	}
}
