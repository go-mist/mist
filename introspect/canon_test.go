package introspect

import (
	"testing"

	"github.com/xbmlz/mist/schema"
)

func TestCanonicalTypes(t *testing.T) {
	cases := []struct {
		fact typeFact
		want schema.Type
	}{
		{typeFact{Name: "int8", TypeKind: "b"}, schema.Type{Name: "bigint"}},
		{typeFact{Name: "bool", TypeKind: "b"}, schema.Type{Name: "boolean"}},
		{typeFact{Name: "varchar", TypeKind: "b", Mod: 12}, schema.Type{Name: "varchar", Length: 8}},
		{typeFact{Name: "numeric", TypeKind: "b", Mod: 655366}, schema.Type{Name: "numeric", Precision: 10, Scale: 2}},
		{typeFact{Name: "timestamptz", TypeKind: "b"}, schema.Type{Name: "timestamptz"}},
		{typeFact{Name: "status", TypeKind: "e", TableSchema: "app", TypeSchema: "app"}, schema.Type{Name: "status", Enum: true}},
		{
			typeFact{Category: "A", Dims: 1, ElemName: "text", ElemKind: "b", ElemSchema: "pg_catalog", TableSchema: "app"},
			schema.Type{Name: "text", ArrayDims: 1},
		},
	}
	for _, tc := range cases {
		got, err := canonicalType(tc.fact, "col")
		if err != nil {
			t.Fatal(err)
		}
		if got != tc.want {
			t.Fatalf("%+v got %+v", tc.fact, got)
		}
	}
}

func TestFoldSerialAndDefault(t *testing.T) {
	col := schema.Column{
		Type:    schema.Type{Name: "bigint"},
		Default: "nextval('app.users_id_seq'::regclass)",
	}
	foldColumn(&col)
	if col.Type.Name != "bigserial" || col.Default != "" {
		t.Fatalf("%+v", col)
	}

	text := schema.Column{Type: schema.Type{Name: "text"}, Default: "'x'::text"}
	foldColumn(&text)
	if text.Default != "'x'" {
		t.Fatal(text.Default)
	}

	id := schema.Column{Type: schema.Type{Name: "bigint"}, Identity: true, Default: "nextval('app.posts_id_seq'::regclass)"}
	foldColumn(&id)
	if id.Type.Name != "bigint" || id.Default != "" || !id.Identity {
		t.Fatalf("%+v", id)
	}
}

func TestUnwrapCheck(t *testing.T) {
	if got := unwrapExpr("((length(name) > 0))"); got != "length(name) > 0" {
		t.Fatal(got)
	}
	if got := unwrapExpr("(a > 0) AND (b > 0)"); got != "(a > 0) AND (b > 0)" {
		t.Fatal(got)
	}
}

func TestFKAction(t *testing.T) {
	got, err := fkAction("c")
	if err != nil || got != "cascade" {
		t.Fatal(got, err)
	}
	got, err = fkAction("a")
	if err != nil || got != "" {
		t.Fatal(got, err)
	}
}
