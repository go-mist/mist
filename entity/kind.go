// Package entity holds the Go equivalent of drizzle's entityKind.
//
// drizzle uses the entityKind symbol for runtime type recognition (is(value, Table), chunkKindOf).
// Go's type identity is guaranteed by the compiler, so is(...) reduces to a type assertion and chunkKindOf reduces to a type
// switch. Kind is kept only so a dispatch result can be passed around as a value and asserted on; for example M2's
// bindIfParam must first decide whether a value is already a param/placeholder/column/table/view.
package entity

import "fmt"

// Kind identifies a value's role in an SQL fragment stream.
// The value order matches chunkKinds in drizzle-orm v1.0.0-rc.5 one to one.
type Kind uint8

const (
	Other Kind = iota
	String
	SQL
	Name
	Column
	Param
	Placeholder
	Table
	View
	Aliased
	Subquery
	ValueRows
)

var kindNames = [...]string{
	Other:       "other",
	String:      "string",
	SQL:         "sql",
	Name:        "name",
	Column:      "column",
	Param:       "param",
	Placeholder: "placeholder",
	Table:       "table",
	View:        "view",
	Aliased:     "aliased",
	Subquery:    "subquery",
	ValueRows:   "valueRows",
}

func (k Kind) String() string {
	if int(k) < len(kindNames) {
		return kindNames[k]
	}
	return fmt.Sprintf("entity.Kind(%d)", uint8(k))
}
