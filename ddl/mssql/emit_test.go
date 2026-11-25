package mssql_test

import (
	"strings"
	"testing"

	mssqlddl "github.com/go-mist/mist/ddl/mssql"
	"github.com/go-mist/mist/diff"
	"github.com/go-mist/mist/schema"
)

func TestCreateJSONColumn(t *testing.T) {
	want := schema.Schema{Tables: []schema.Table{{
		Name:       "notes",
		PrimaryKey: []string{"id"},
		Columns: []schema.Column{
			{Name: "id", Type: schema.Type{Name: "bigint"}, NotNull: true, Identity: true},
			{Name: "body", Type: schema.Type{Name: "json"}, NotNull: true},
		},
	}}}
	got, err := mssqlddl.Render(diff.Diff(want, schema.Schema{}))
	if err != nil {
		t.Fatal(err)
	}
	for _, piece := range []string{
		"[id] bigint IDENTITY(1,1) NOT NULL PRIMARY KEY",
		"[body] nvarchar(max) NOT NULL",
		"CHECK (isjson([body]) = 1)",
	} {
		if !strings.Contains(got, piece) {
			t.Fatalf("missing %s\n%s", piece, got)
		}
	}
}
