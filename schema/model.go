// Package schema is the dialect-neutral structure used for DDL.
// Queries and scans do not read it. diff compares two Schemas, and ddl then renders statements.
package schema

// Loc is where a declaration sits in source. It may be empty when a database is read back.
type Loc struct {
	File string
	Line int
}

// Schema is a structure snapshot. The set of objects is described in ADR-0008.
type Schema struct {
	Enums  []Enum
	Tables []Table
}

// Enum is an enum type. Empty Values means only the type name is known, not the values.
type Enum struct {
	Name   string
	Values []string
	Loc    Loc
}

// Table is one table. Columns, the primary key, and constraints stay in declaration order.
type Table struct {
	Name           string
	Columns        []Column
	PrimaryKey     []string
	PrimaryKeyName string
	Uniques        []Unique
	Indexes        []Index
	Checks         []Check
	ForeignKeys    []ForeignKey
	Loc            Loc
}

// Column is one column. Default and Generated are raw SQL expressions.
type Column struct {
	Name      string
	Type      Type
	NotNull   bool
	Default   string
	Generated string
	Identity  bool
	Loc       Loc
}

// Type is a column type. When Enum is true, Name is an enum type name and must be quoted as an identifier.
type Type struct {
	Name      string
	Length    int
	Precision int
	Scale     int
	ArrayDims int
	Enum      bool
}

// Unique is a unique constraint.
type Unique struct {
	Name    string
	Columns []string
	Loc     Loc
}

// Index is an index. When Unique is true, it is a unique index and not a unique constraint.
type Index struct {
	Name    string
	Columns []string
	Unique  bool
	Loc     Loc
}

// Check is a CHECK. Expr is a raw SQL expression.
type Check struct {
	Name string
	Expr string
	Loc  Loc
}

// ForeignKey is a foreign key.
type ForeignKey struct {
	Name       string
	Columns    []string
	RefTable   string
	RefColumns []string
	OnDelete   string
	OnUpdate   string
	Loc        Loc
}

// UniqueName returns the unique-constraint name. An empty explicit uses PostgreSQL's default name.
func UniqueName(table, column, explicit string) string {
	if explicit != "" {
		return explicit
	}
	return table + "_" + column + "_key"
}

// ForeignKeyName returns the foreign-key name. An empty explicit uses PostgreSQL's default name.
func ForeignKeyName(table string, columns []string, explicit string) string {
	if explicit != "" {
		return explicit
	}
	name := table
	for _, column := range columns {
		name += "_" + column
	}
	return name + "_fkey"
}

// PrimaryKeyNameOf returns the primary-key constraint name. An empty explicit uses PostgreSQL's default name.
func PrimaryKeyNameOf(table, explicit string) string {
	if explicit != "" {
		return explicit
	}
	return table + "_pkey"
}
