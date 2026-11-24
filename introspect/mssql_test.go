package introspect

import (
	"testing"

	"github.com/go-mist/mist/schema"
)

func TestMSSQLJSONCheck(t *testing.T) {
	cols := []schema.Column{{Name: "body", Type: schema.Type{Name: "text"}}}
	got, ok := mssqlJSONColumn("(isjson([body])=(1))", cols)
	if !ok || got != "body" {
		t.Fatalf("%q %v", got, ok)
	}
	if _, ok := mssqlJSONColumn("(len([body])>(0))", cols); ok {
		t.Fatal("plain check was treated as json")
	}
}
