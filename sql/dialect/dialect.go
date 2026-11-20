// Package dialect 定义方言对渲染器的最小承诺。
//
// 设计原则 4：方言 = 三个 escape 函数 + 若干 build 函数。这里只有前者。
// Build*Query 住在各方言包里（见 ROADMAP M2），不进这个接口 —— 否则每加一个
// 方言实现，所有方言都要补一堆用不到的方法。
package dialect

// Dialect 是渲染 SQL 片段流时方言必须提供的全部能力。
//
// drizzle-orm v1.0.0-rc.5 的对应物是 BuildQueryConfig 里的
// escapeName / escapeParam / escapeString 三个回调。
type Dialect interface {
	// EscapeName 转义一个标识符（表名、列名、别名）。
	EscapeName(name string) string
	// EscapeParam 生成第 num 个参数（从 0 开始）的占位符。
	// value 在 pg 上用不到，但 mysql 的 ? 与 mssql 的 @p1 需要它做类型推断。
	EscapeParam(num int, value any) string
	// EscapeString 转义一个字符串字面量，供内联参数模式使用。
	EscapeString(str string) string
}
