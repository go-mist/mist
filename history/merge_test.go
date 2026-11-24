package history

import (
	"strings"
	"testing"

	"github.com/go-mist/mist/schema"
)

func TestResolveMergesDifferentColumns(t *testing.T) {
	parent := users("id", "name")
	avatar := users("id", "name", "avatar")
	bio := users("id", "name", "bio")
	state, err := Resolve([]Snapshot{
		{ID: "p", Schema: parent},
		{ID: "a", PrevIDs: []string{"p"}, Schema: avatar},
		{ID: "b", PrevIDs: []string{"p"}, Schema: bio},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(state.Heads, ",") != "a,b" {
		t.Fatal(state.Heads)
	}
	if !hasColumn(state.Schema, "avatar") || !hasColumn(state.Schema, "bio") {
		t.Fatalf("%+v", state.Schema.Tables[0].Columns)
	}
}

func TestResolveFanInKeepsInheritedColumns(t *testing.T) {
	parent := users("id", "name")
	avatar := users("id", "name", "avatar")
	bio := users("id", "name", "bio")
	left := users("id", "name", "avatar", "bio", "left")
	right := users("id", "name", "avatar", "bio", "right")
	state, err := Resolve([]Snapshot{
		{ID: "p", Schema: parent},
		{ID: "a", PrevIDs: []string{"p"}, Schema: avatar},
		{ID: "b", PrevIDs: []string{"p"}, Schema: bio},
		{ID: "c", PrevIDs: []string{"a", "b"}, Schema: left},
		{ID: "d", PrevIDs: []string{"a", "b"}, Schema: right},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"avatar", "bio", "left", "right"} {
		if !hasColumn(state.Schema, name) {
			t.Fatal(name)
		}
	}
}

func TestResolveReportsTheColumn(t *testing.T) {
	parent := users("id", "email")
	required := users("id", "email")
	required.Tables[0].Columns[1].NotNull = true
	withDefault := users("id", "email")
	withDefault.Tables[0].Columns[1].Default = "'a@b.c'"
	_, err := Resolve([]Snapshot{
		{ID: "p", Schema: parent},
		{ID: "20261123050001_required", PrevIDs: []string{"p"}, Schema: required},
		{ID: "20261123050002_def", PrevIDs: []string{"p"}, Schema: withDefault},
	})
	if err == nil || !strings.Contains(err.Error(), "20261123050001_required") || !strings.Contains(err.Error(), "20261123050002_def") || !strings.Contains(err.Error(), "table users column email") {
		t.Fatal(err)
	}
}

func TestResolveDropAgainstColumnChange(t *testing.T) {
	parent := users("id", "name")
	added := users("id", "name", "email")
	_, err := Resolve([]Snapshot{
		{ID: "p", Schema: parent},
		{ID: "a", PrevIDs: []string{"p"}, Schema: schema.Schema{}},
		{ID: "b", PrevIDs: []string{"p"}, Schema: added},
	})
	if err == nil || !strings.Contains(err.Error(), "table users") {
		t.Fatal(err)
	}
}

func users(names ...string) schema.Schema {
	cols := make([]schema.Column, len(names))
	for i, name := range names {
		cols[i] = schema.Column{Name: name, Type: schema.Type{Name: "text"}}
	}
	return schema.Schema{Tables: []schema.Table{{
		Name:       "users",
		PrimaryKey: []string{"id"},
		Columns:    cols,
	}}}
}

func hasColumn(s schema.Schema, name string) bool {
	for _, col := range s.Tables[0].Columns {
		if col.Name == name {
			return true
		}
	}
	return false
}
