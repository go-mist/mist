package pg

import "github.com/xbmlz/drizzle-go/sql"

// Row 是一行查询结果。database/sql 与 pgx 的 Row 都满足这个接口。
type Row interface {
	Scan(dest ...any) error
}

// TableMeta 是用户在 Table() 里声明的表信息。
// 生成器目前只读取 Name。
type TableMeta struct {
	Name string
}

// TableBase 嵌在生成的表描述符里，提供表名，并把表渲染成标识符。
type TableBase struct {
	SQLName string
}

// GetSQL 实现 sql.Chunk。
func (b TableBase) GetSQL() *sql.SQL {
	return sql.S(sql.Identifier(b.SQLName))
}

// ShouldOmitSQLParens 避免表名被包进括号。
func (TableBase) ShouldOmitSQLParens() bool { return true }

// Col 是一列的描述符。T 是行结构体，V 是该字段的 Go 类型。
// Ptr 在扫描时取出字段地址，生成代码填入，不使用反射。
type Col[T, V any] struct {
	Table string
	Name  string
	Ptr   func(*T) any
}

// GetSQL 把列渲染成 "table"."column"。表名为空时只渲染列名。
func (c Col[T, V]) GetSQL() *sql.SQL {
	if c.Table == "" {
		return sql.S(sql.Identifier(c.Name))
	}
	return sql.S(sql.Identifier(c.Table), sql.Raw("."), sql.Identifier(c.Name))
}

// ShouldOmitSQLParens 避免列引用被包进括号。
func (Col[T, V]) ShouldOmitSQLParens() bool { return true }
