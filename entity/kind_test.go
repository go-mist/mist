package entity_test

import (
	"testing"

	"github.com/go-mist/mist/entity"
)

func TestKindString(t *testing.T) {
	tests := []struct {
		kind entity.Kind
		want string
	}{
		{entity.Other, "other"},
		{entity.String, "string"},
		{entity.SQL, "sql"},
		{entity.Name, "name"},
		{entity.Column, "column"},
		{entity.Param, "param"},
		{entity.Placeholder, "placeholder"},
		{entity.Table, "table"},
		{entity.View, "view"},
		{entity.Aliased, "aliased"},
		{entity.Subquery, "subquery"},
		{entity.ValueRows, "valueRows"},
	}

	for _, tt := range tests {
		if got := tt.kind.String(); got != tt.want {
			t.Fatalf("Kind(%d).String() = %q, want %q", tt.kind, got, tt.want)
		}
	}

	// An out-of-range value must also have a readable output and must not panic — it usually comes from version-mismatch debug info.
	if got := entity.Kind(200).String(); got != "entity.Kind(200)" {
		t.Fatalf("out-of-range Kind.String() = %q", got)
	}
}
