package mysql

import (
	"github.com/xbmlz/mist/query"
	"github.com/xbmlz/mist/sql"
)

// Row is one row of a query result. Generated code writes mysql.Row.
type Row = sql.Row

// TableMeta is the table information the user declares in Table().
// Column-level modifiers stay on field tags. Composite primary keys, indexes, and CHECK constraints are written here.
type TableMeta struct {
	Name       string
	PrimaryKey []string
	Indexes    []Index
	Checks     []Check
}

// Index is an index on the table. When Unique is true, the index is unique.
type Index struct {
	Name    string
	Columns []string
	Unique  bool
}

// Check is a CHECK on the table. Expr is a raw SQL expression.
type Check struct {
	Name string
	Expr string
}

// ColumnSpec is the column constraint the generator copies from a field tag.
// Queries and scans do not read it.
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

// TableBase is embedded in a table descriptor. It supplies the table name and renders the table as an identifier.
type TableBase struct {
	SQLName string
}

// GetSQL implements sql.Chunk.
func (b TableBase) GetSQL() *sql.SQL {
	return sql.S(sql.Identifier(b.SQLName))
}

// ShouldOmitSQLParens keeps the table name from being wrapped in parentheses.
func (TableBase) ShouldOmitSQLParens() bool { return true }

// Col is a column descriptor. T is the row struct, and V is the Go type of that field.
// Ptr takes the field address at scan time. The caller fills it in, and reflection is not used.
type Col[T, V any] struct {
	Table string
	Name  string
	Ptr   func(*T) any
	// When Codec is nil this column passes through, and binding does no type conversion.
	Codec sql.Encoder
}

// GetSQL renders the column as `table`.`column`. When the table name is empty, only the column name is rendered.
func (c Col[T, V]) GetSQL() *sql.SQL {
	if c.Table == "" {
		return sql.S(sql.Identifier(c.Name))
	}
	return sql.S(sql.Identifier(c.Table), sql.Raw("."), sql.Identifier(c.Name))
}

// ShouldOmitSQLParens keeps the column reference from being wrapped in parentheses.
func (Col[T, V]) ShouldOmitSQLParens() bool { return true }

// ColName returns the column name without a table name.
func (c Col[T, V]) ColName() string { return c.Name }

// WithTable copies this column and replaces the table name with alias.
func (c Col[T, V]) WithTable(alias string) query.Column[T] {
	c.Table = alias
	return c
}

// DriverEncoder returns this column's parameter encoder. nil means pass-through.
func (c Col[T, V]) DriverEncoder() sql.Encoder {
	if c.Codec == nil || c.Codec.IsNoop() {
		return nil
	}
	return c.Codec
}

// FieldPtr returns this column's field address on the row.
func (c Col[T, V]) FieldPtr(row *T) any {
	if c.Ptr == nil {
		return nil
	}
	return c.Ptr(row)
}
