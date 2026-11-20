// Package pg 是 PostgreSQL 方言。
//
// 渲染器需要的三个转义函数，以及生成代码用的列描述符和表基座在这里。
// 查询编译与类型映射分别在 M2、M4。
package pg

import (
	"strconv"
	"strings"

	"github.com/xbmlz/drizzle-go/sql"
)

// Dialect 实现 dialect.Dialect 的最小契约。
//
// 对应 drizzle 的 PgDialect 在渲染器里的那部分行为：
//
//	escapeName   -> `"name"`，内部的双引号翻倍
//	escapeParam  -> `$1`、`$2`……
//	escapeString -> `'text'`，内部单引号翻倍
type Dialect struct{}

// EscapeName 用双引号包裹标识符，并把内部的 `"` 转义成 `""`。
// 与 drizzle 的 escapePgName 逐字节一致。
func (Dialect) EscapeName(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// EscapeParam 生成 pg 的位置参数，从 $1 开始。
// drizzle 的 PgDialect 忽略第二个参数（value），Go 为将来方言保留它。
func (Dialect) EscapeParam(num int, _ any) string {
	return "$" + strconv.Itoa(num+1)
}

// EscapeString 生成单引号字符串字面量，内部单引号翻倍。
func (Dialect) EscapeString(str string) string {
	return "'" + strings.ReplaceAll(str, "'", "''") + "'"
}

// SQLToQuery 用本方言把一段 SQL 渲染成 (SQL 文本, 参数)。
// 对应 drizzle 的 PgDialect.sqlToQuery。
func (Dialect) SQLToQuery(query *sql.SQL) (sql.Query, error) {
	return sql.Render(query, Dialect{}, sql.RenderConfig{})
}

// SQLToQueryInline 是内联参数模式，用于调试输出与 DDL。
//
// drizzle 没有对应方法：它靠 `query.inlineParams()` 原地改成内联模式。
// Go 保留两种入口，因为“同一段 SQL 的两种渲染”是测试与 CLI 的常见需求。
func (Dialect) SQLToQueryInline(query *sql.SQL) (sql.Query, error) {
	return sql.Render(query, Dialect{}, sql.RenderConfig{InlineParams: true})
}
