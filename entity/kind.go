// Package entity 保存 drizzle 的 entityKind 在 Go 里的等价物。
//
// drizzle 用 entityKind 符号做运行时类型识别（is(value, Table)、chunkKindOf）。
// Go 的类型身份由编译器保证，is(...) 退化为类型断言，chunkKindOf 退化为类型
// switch。这里保留 Kind 只是为了让“分派结果”能作为值传递与断言，例如 M2 的
// bindIfParam 需要先判断一个值是否已经是 param/placeholder/column/table/view。
package entity

import "fmt"

// Kind 标识一个值在 SQL 片段流里的角色。
// 取值顺序与 drizzle-orm v1.0.0-rc.5 的 chunkKinds 一一对应。
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
