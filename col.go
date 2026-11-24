package mist

import (
	"unsafe"

	"github.com/go-mist/mist/query"
	"github.com/go-mist/mist/sql"
)

// Row is one row of a query result.
type Row = sql.Row

// TableMeta is the table information a user declares in Table().
// Column-level modifiers stay on field tags. Composite primary keys, indexes, and CHECKs are written here.
// The same value is used for every database. The open connection decides the SQL type.
type TableMeta struct {
	Name       string
	PrimaryKey []string
	Indexes    []Index
	Checks     []Check
}

// Index is an index on a table. When Unique is true it is a unique index.
type Index struct {
	Name    string
	Columns []string
	Unique  bool
}

// Check is a CHECK on a table. Expr is a raw SQL expression.
type Check struct {
	Name string
	Expr string
}

// ColumnSpec is the column constraint the generator copies from a field tag.
// Queries and scans do not read it. Creating tables and migrating later use this struct and do not read the tag again.
type ColumnSpec struct {
	Name       string
	SQLType    string
	NotNull    bool
	PrimaryKey bool
	Unique     bool
	UniqueName string
	Default    string
	References string
	OnDelete   string
	OnUpdate   string
	Length     int
	Precision  int
	Scale      int
	Generated  string
	Identity   bool
}

// TableBase is embedded in a generated table descriptor. It provides the table name and renders the table as an identifier.
type TableBase struct {
	SQLName string
}

// Ident returns the table name. The table part is empty because this value is the table.
func (b TableBase) Ident() (table, name string) { return "", b.SQLName }

// StablePtr is the address of this table. Only a pointer value has it, so a copied table is not cached.
func (b *TableBase) StablePtr() uintptr { return uintptr(unsafe.Pointer(b)) }

// GetSQL implements sql.Chunk.
func (b TableBase) GetSQL() *sql.SQL {
	return sql.S(sql.Identifier(b.SQLName))
}

// ShouldOmitSQLParens keeps the table name from being wrapped in parentheses.
func (TableBase) ShouldOmitSQLParens() bool { return true }

// Col is a column descriptor. T is the row struct and V is the Go type of the field.
// Ptr takes the field address during a scan. Generated code fills it in, without reflection.
type Col[T, V any] struct {
	Table string
	Name  string
	Ptr   func(*T) any
	// When Codec is nil this column is a passthrough: scan and bind do no type conversion.
	Codec sql.Encoder
}

// Ident returns the table and column names. An empty table means a bare identifier.
func (c Col[T, V]) Ident() (table, name string) { return c.Table, c.Name }

// StablePtr is the address of this column. Only a pointer value has it, so a copied column is not cached.
func (c *Col[T, V]) StablePtr() uintptr { return uintptr(unsafe.Pointer(c)) }

// GetSQL renders the column as an identifier pair. Quoting happens when the connection renders the statement.
func (c Col[T, V]) GetSQL() *sql.SQL {
	if c.Table == "" {
		return sql.S(sql.Identifier(c.Name))
	}
	return sql.S(sql.Identifier(c.Table), sql.Raw("."), sql.Identifier(c.Name))
}

// ShouldOmitSQLParens keeps a column reference from being wrapped in parentheses.
func (Col[T, V]) ShouldOmitSQLParens() bool { return true }

// ColName returns the column name without a table name.
func (c Col[T, V]) ColName() string { return c.Name }

// TableName returns the table this column belongs to.
func (c Col[T, V]) TableName() string { return c.Table }

// WithTable copies this column and replaces the table name with alias. CTE columns use it.
func (c Col[T, V]) WithTable(alias string) query.Column[T] {
	c.Table = alias
	return c
}

// DriverEncoder returns this column's parameter encoder. nil means passthrough.
func (c Col[T, V]) DriverEncoder() sql.Encoder {
	if c.Codec == nil || c.Codec.IsNoop() {
		return nil
	}
	return c.Codec
}

// FieldPtr returns this column's field address on the row. Ptr is filled in by generated code; a scan does not use reflection.
func (c Col[T, V]) FieldPtr(row *T) any {
	if c.Ptr == nil {
		return nil
	}
	return c.Ptr(row)
}
