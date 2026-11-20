package sql

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"github.com/xbmlz/drizzle-go/entity"
	"github.com/xbmlz/drizzle-go/sql/dialect"
)

// Chunk 是 SQL 片段流里的一个元素。
//
// 对应 drizzle-orm v1.0.0-rc.5 的 SQLChunk。drizzle 用类型并集表达，Go 用接口：
// 凡是能产出 SQL 的值都是 Chunk —— 这与 drizzle 的 SQLWrapper 契约一致
// （isSQLWrapper 判的就是 typeof value.getSQL === 'function'）。
//
// 与 drizzle 的一处刻意差异：drizzle 允许裸标量出现在 chunk 流里（运行时才包成
// Param），而 Go 的 Chunk 接口要求 GetSQL，所以标量由 S(...) 在构造时包成 Param。
// 结果是同一件事，但类型上更早暴露问题。
type Chunk interface {
	GetSQL() *SQL
}

// Wrapper 是 Chunk 的别名，drizzle 里这个角色叫 SQLWrapper。
type Wrapper = Chunk

// Fragment 是一段字面 SQL 文本：原样输出，不做转义、不加引号、不绑定参数。
//
// 对应 drizzle 的 StringChunk。多数时候你不直接构造它，而是用 Raw("...")。
type Fragment string

// GetSQL 实现 Chunk。
func (f Fragment) GetSQL() *SQL { return &SQL{QueryChunks: []Chunk{f}} }

// Name 是一个需要按方言转义的标识符（表名、列名、别名、索引名）。
//
// 对应 drizzle 的 Name。与 Fragment 的区别只有一条：
// Name 会经过 Dialect.EscapeName，Fragment 不会。
type Name string

// GetSQL 实现 Chunk。
func (n Name) GetSQL() *SQL { return &SQL{QueryChunks: []Chunk{n}} }

// Param 是一个待绑定的参数值。
//
// Encoder 为 nil 时等价于 drizzle 的 noopEncoder：值原样进参数列表。
// 注意 Param 的 Value 可以是 *SQL（此时整个片段被展开而不是绑定），
// 也可以是 *Placeholder（此时占位符本身进参数列表，留待 FillPlaceholders 填充）。
type Param struct {
	Value   any
	Encoder Encoder
}

// GetSQL 实现 Chunk。
func (p *Param) GetSQL() *SQL { return &SQL{QueryChunks: []Chunk{p}} }

// Placeholder 是一个命名占位符，执行前由 FillPlaceholders 替换成真实值。
//
// 对应 drizzle 的 Placeholder。
type Placeholder struct {
	Name string
}

// GetSQL 实现 Chunk。
func (p *Placeholder) GetSQL() *SQL { return &SQL{QueryChunks: []Chunk{p}} }

// Alias 给一段 SQL 起一个字段名（select 列表里的 `expr as "alias"`）。
//
// 对应 drizzle 的 SQL.Aliased。Origin 对应 drizzle 的 origin：带表别名时，
// 渲染为 origin.alias 而不是裸 alias。
type Alias struct {
	SQL        *SQL
	FieldAlias string
	Origin     string
}

// GetSQL 实现 Chunk。注意 drizzle 这里返回的是被别名的 SQL 本身。
func (a *Alias) GetSQL() *SQL { return a.SQL }

// Subquery 是一个派生表或 CTE。
//
// 对应 drizzle 的 Subquery。IsWith 为 true 时只渲染别名（CTE 在 with 子句里
// 已经定义过），否则渲染为 `(select ...) "alias"`。
type Subquery struct {
	SQL       *SQL
	AliasName string
	IsWith    bool
}

// GetSQL 实现 Chunk。
func (s *Subquery) GetSQL() *SQL { return s.SQL }

// ValueRows 是一次插入的多行值：渲染为 `(...), (...), (...)`。
//
// 对应 drizzle 的 ValueRows。Cells 是扁平的行主序值列表，Encoders 是每一列的
// 编码器；宽度取自 len(Encoders)，用于决定在哪里换行。
//
// 与 drizzle 的差异：drizzle 的 Cells 是 unknown[]，渲染时对每个元素按 chunk
// 类型分派；这里允许 Cells 里直接放 Fragment / *SQL，便于插入表达式。
type ValueRows struct {
	Cells    []any
	Encoders []Encoder
}

// GetSQL 实现 Chunk。保留 drizzle 的同名兼容壳：不认 ValueRows 的构建器
// 通过它仍能得到等价的 SQL。
func (v *ValueRows) GetSQL() *SQL {
	chunks := make([]Chunk, 0, len(v.Cells)+2)
	for i, cell := range v.Cells {
		column := i % len(v.Encoders)
		switch column {
		case 0:
			if i == 0 {
				chunks = append(chunks, Fragment("("))
			} else {
				chunks = append(chunks, Fragment("), ("))
			}
		default:
			chunks = append(chunks, Fragment(", "))
		}
		chunks = append(chunks, valueRowsCell(cell, v.Encoders[column]))
	}
	chunks = append(chunks, Fragment(")"))
	return &SQL{QueryChunks: chunks}
}

// ShouldOmitSQLParens 实现可选的省略括号契约：ValueRows 自带括号。
func (v *ValueRows) ShouldOmitSQLParens() bool { return true }

// List 是一组被括号包裹、逗号分隔的片段：`(a, b, c)`。
//
// 对应渲染器里的 Array.isArray(chunk) 分支。drizzle 没有为它定义类型，
// 数组走的是内联分支；Go 用命名类型显式表达，避免反射散落在渲染器里。
type List []Chunk

// GetSQL 实现 Chunk。
func (l List) GetSQL() *SQL { return &SQL{QueryChunks: l} }

// Encoder 把 Go 值映射成驱动参数。
//
// 对应 drizzle 的 DriverValueEncoder。drizzle 通过 mapToDriverValue.isNoop
// 属性判断是否无需转换，Go 用 IsNoop 方法表达；两者语义相同：
// IsNoop 为 true 时渲染器跳过映射，省掉一次无意义的函数调用。
type Encoder interface {
	MapToDriverValue(value any) any
	IsNoop() bool
}

// NoopEncoder 原样透传，对应 drizzle 的 noopEncoder。
type NoopEncoder struct{}

// MapToDriverValue 原样返回。
func (NoopEncoder) MapToDriverValue(value any) any { return value }

// IsNoop 恒为 true。
func (NoopEncoder) IsNoop() bool { return true }

// KindOf 返回一个值在片段流里的角色，对应 drizzle 的 chunkKindOf。
//
// 与 drizzle 的差异：drizzle 缓存数字 kind 到原型上，Go 直接做类型断言 ——
// 没有字符串比较，也不需要缓存。
func KindOf(ch Chunk) entity.Kind {
	switch ch.(type) {
	case Fragment:
		return entity.String
	case *SQL:
		return entity.SQL
	case Name:
		return entity.Name
	case *Param:
		return entity.Param
	case *Placeholder:
		return entity.Placeholder
	case *Alias:
		return entity.Aliased
	case *Subquery:
		return entity.Subquery
	case *ValueRows:
		return entity.ValueRows
	default:
		return entity.Other
	}
}

// isNilChunk 识别“装进接口的 nil 指针”。
//
// Go 特有的坑：If(false) 返回 (*SQL)(nil)，把它塞进 Chunk 接口后接口本身不为 nil。
// 渲染器与 And/Or 都必须把它当作缺失，与 drizzle 的 undefined 等价。
func isNilChunk(ch Chunk) bool {
	if ch == nil {
		return true
	}
	v := reflect.ValueOf(ch)
	return v.Kind() == reflect.Pointer && v.IsNil()
}

func valueRowsCell(cell any, enc Encoder) Chunk {
	switch x := cell.(type) {
	case Fragment:
		return x
	case *SQL:
		return x
	default:
		return &Param{Value: cell, Encoder: enc}
	}
}

// FillPlaceholders 用 values 填充 params 里的占位符，返回新的参数列表。
//
// 对应 drizzle 的 fillPlaceholders：直接占位符整项替换；包在 Param 里的占位符
// 先经过编码器再替换。缺失的占位符是错误，不是静默的 nil。
func FillPlaceholders(params []any, values map[string]any) ([]any, error) {
	filled := make([]any, len(params))
	for i, p := range params {
		switch v := p.(type) {
		case *Placeholder:
			value, ok := values[v.Name]
			if !ok {
				return nil, fmt.Errorf("sql: no value for placeholder %q", v.Name)
			}
			filled[i] = value
		case *Param:
			ph, ok := v.Value.(*Placeholder)
			if !ok {
				filled[i] = p
				continue
			}
			value, ok := values[ph.Name]
			if !ok {
				return nil, fmt.Errorf("sql: no value for placeholder %q", ph.Name)
			}
			if value == nil {
				filled[i] = nil
				continue
			}
			if v.Encoder != nil && !v.Encoder.IsNoop() {
				value = v.Encoder.MapToDriverValue(value)
			}
			filled[i] = value
		default:
			filled[i] = p
		}
	}
	return filled, nil
}

// renderer 是 Render 的内部状态。字段与 drizzle collectSQL 的可变累加量对应。
type renderer struct {
	d      dialect.Dialect
	cfg    RenderConfig
	buf    strings.Builder
	params []any
	next   int
	err    error
}

// Query 是渲染结果：最终 SQL 文本与绑定参数。
type Query struct {
	SQL    string
	Params []any
}

// RenderConfig 对应 drizzle 的 BuildQueryConfig，只保留 Go 用得到的字段。
//
// 刻意没有移植的两个字段（记录在 ROADMAP 的差异登记里）：
//   - tagged：drizzle 用它为 Bun.SQL 产出 _sql 片段数组，Go 没有等价需求；
//   - invokeSource：mssql / 索引元数据专用的分派开关，M0 是纯 pg。
//
// Codecs 钩子在 M4 接进来：目前的渲染路径等价于“所有列都使用 noop 编码器”。
type RenderConfig struct {
	// ParamStart 是第一个绑定参数的序号（0 起算）。pg 渲染成 $1、$2……
	ParamStart int
	// InlineParams 把参数直接内联进 SQL 文本，用于调试输出与 DDL。
	InlineParams bool
}

// ErrNilDialect 表示渲染时没有提供方言。
var ErrNilDialect = errors.New("sql: nil dialect")

// Render 把一段 SQL 渲染成 (SQL 文本, 参数列表)。
//
// 对应 drizzle 的 buildQueryFromSourceParams。执行顺序与 drizzle 完全一致：
// 递归进入嵌套 SQL、按 chunk 类型分派、必要时绑定参数或内联参数。
//
// 返回 error 而不是 panic：内联模式下遇到无法格式化的值（drizzle 会 throw），
// 在 Go 里应当是可传播的错误。
func Render(s *SQL, d dialect.Dialect, cfg RenderConfig) (Query, error) {
	if d == nil {
		return Query{}, ErrNilDialect
	}
	if s == nil {
		return Query{}, nil
	}
	r := &renderer{d: d, cfg: cfg, next: cfg.ParamStart}
	r.collect(s.QueryChunks, cfg.InlineParams || s.shouldInlineParams)
	if r.err != nil {
		return Query{}, r.err
	}
	return Query{SQL: r.buf.String(), Params: r.params}, nil
}

// collect 对应 drizzle 的 collectSQL。
func (r *renderer) collect(chunks []Chunk, inline bool) {
	for _, ch := range chunks {
		if r.err != nil {
			return
		}
		if isNilChunk(ch) {
			continue
		}
		switch v := ch.(type) {
		case Fragment:
			r.buf.WriteString(string(v))
		case *SQL:
			r.collect(v.QueryChunks, inline || v.shouldInlineParams)
		case Name:
			r.buf.WriteString(r.d.EscapeName(string(v)))
		case *Param:
			r.collectParam(v, inline)
		case *Placeholder:
			r.writePlaceholder(v)
		case *Alias:
			r.collectAlias(v, inline)
		case *Subquery:
			r.collectSubquery(v, inline)
		case *ValueRows:
			r.collectValueRows(v, inline)
		case List:
			r.collectList(v, inline)
		default:
			r.collectWrapper(ch, inline)
		}
	}
}

// collectParam 对应 chunkKinds.param 分支。
func (r *renderer) collectParam(p *Param, inline bool) {
	// 参数值本身是 SQL 时展开，不绑定（drizzle 的 valueKind === sql 分支）。
	if nested, ok := p.Value.(*SQL); ok && nested != nil {
		r.collect(nested.QueryChunks, inline || nested.shouldInlineParams)
		return
	}
	// 参数值是占位符时，占位符本身进参数列表，执行前才填充。
	if ph, ok := p.Value.(*Placeholder); ok && ph != nil {
		r.writePlaceholder(ph)
		return
	}
	r.collectBoundValue(p.Value, p.Encoder, inline)
}

func (r *renderer) writePlaceholder(ph *Placeholder) {
	r.buf.WriteString(r.d.EscapeParam(r.next, ph))
	r.next++
	r.params = append(r.params, ph)
}

// collectBoundValue 对应 drizzle 的 collectBoundValue。
func (r *renderer) collectBoundValue(value any, enc Encoder, inline bool) {
	mapped := value
	if value != nil {
		if enc != nil && !enc.IsNoop() {
			mapped = enc.MapToDriverValue(value)
		}
		if nested, ok := mapped.(*SQL); ok && nested != nil {
			r.collect(nested.QueryChunks, inline || nested.shouldInlineParams)
			return
		}
		// TODO(M4): 在这里接 normalizeParam。
	}
	if inline {
		r.writeInline(mapped)
		return
	}
	r.buf.WriteString(r.d.EscapeParam(r.next, mapped))
	r.next++
	r.params = append(r.params, mapped)
}

func (r *renderer) collectAlias(a *Alias, inline bool) {
	if a.FieldAlias == "" {
		// drizzle 在 fieldAlias 为 undefined 时退回到 SQLWrapper 分支。
		r.collectWrapper(a, inline)
		return
	}
	if a.Origin != "" {
		r.buf.WriteString(r.d.EscapeName(a.Origin))
		r.buf.WriteByte('.')
	}
	r.buf.WriteString(r.d.EscapeName(a.FieldAlias))
}

func (r *renderer) collectSubquery(s *Subquery, inline bool) {
	if s.IsWith {
		r.buf.WriteString(r.d.EscapeName(s.AliasName))
		return
	}
	r.buf.WriteByte('(')
	if s.SQL != nil {
		r.collect(s.SQL.QueryChunks, inline || s.SQL.shouldInlineParams)
	}
	r.buf.WriteString(") ")
	r.buf.WriteString(r.d.EscapeName(s.AliasName))
}

func (r *renderer) collectValueRows(v *ValueRows, inline bool) {
	width := len(v.Encoders)
	if width == 0 {
		r.err = errors.New("sql: ValueRows with no encoders")
		return
	}
	r.buf.WriteByte('(')
	column := 0
	for i, cell := range v.Cells {
		if column == width {
			column = 0
			r.buf.WriteString("), (")
		} else if column > 0 {
			r.buf.WriteString(", ")
		}
		switch x := cell.(type) {
		case Fragment:
			r.buf.WriteString(string(x))
		case *SQL:
			r.collect(x.QueryChunks, inline || x.shouldInlineParams)
		default:
			r.collectBoundValue(cell, v.Encoders[column], inline)
		}
		column++
		_ = i
	}
	r.buf.WriteByte(')')
}

func (r *renderer) collectList(l List, inline bool) {
	r.buf.WriteByte('(')
	for i, ch := range l {
		if i > 0 {
			r.buf.WriteString(", ")
		}
		r.collect([]Chunk{ch}, inline)
	}
	r.buf.WriteByte(')')
}

// collectWrapper 对应 drizzle 的 isSQLWrapper 分支。
func (r *renderer) collectWrapper(ch Chunk, inline bool) {
	nested := ch.GetSQL()
	if nested == nil {
		return
	}
	innerInline := inline || nested.shouldInlineParams
	if o, ok := ch.(interface{ ShouldOmitSQLParens() bool }); ok && o.ShouldOmitSQLParens() {
		r.collect(nested.QueryChunks, innerInline)
		return
	}
	r.buf.WriteByte('(')
	r.collect(nested.QueryChunks, innerInline)
	r.buf.WriteByte(')')
}

// writeInline 对应 drizzle 的 mapInlineParam。
//
// 与 drizzle 的差异：JS 的 toString() 对普通对象走 [object Object] 再转 JSON，
// Go 用 fmt.Stringer 判断“有自定义文本表示”，其余走 encoding/json。
func (r *renderer) writeInline(v any) {
	switch x := v.(type) {
	case nil:
		r.buf.WriteString("null")
	case bool:
		r.buf.WriteString(strconv.FormatBool(x))
	case int:
		r.buf.WriteString(strconv.FormatInt(int64(x), 10))
	case int8:
		r.buf.WriteString(strconv.FormatInt(int64(x), 10))
	case int16:
		r.buf.WriteString(strconv.FormatInt(int64(x), 10))
	case int32:
		r.buf.WriteString(strconv.FormatInt(int64(x), 10))
	case int64:
		r.buf.WriteString(strconv.FormatInt(x, 10))
	case uint:
		r.buf.WriteString(strconv.FormatUint(uint64(x), 10))
	case uint8:
		r.buf.WriteString(strconv.FormatUint(uint64(x), 10))
	case uint16:
		r.buf.WriteString(strconv.FormatUint(uint64(x), 10))
	case uint32:
		r.buf.WriteString(strconv.FormatUint(uint64(x), 10))
	case uint64:
		r.buf.WriteString(strconv.FormatUint(x, 10))
	case float32:
		r.buf.WriteString(strconv.FormatFloat(float64(x), 'g', -1, 32))
	case float64:
		r.buf.WriteString(strconv.FormatFloat(x, 'g', -1, 64))
	case string:
		r.buf.WriteString(r.d.EscapeString(x))
	default:
		if s, ok := v.(fmt.Stringer); ok {
			r.buf.WriteString(r.d.EscapeString(s.String()))
			return
		}
		encoded, err := json.Marshal(v)
		if err != nil {
			r.err = fmt.Errorf("sql: cannot inline param of type %T: %w", v, err)
			return
		}
		r.buf.WriteString(r.d.EscapeString(string(encoded)))
	}
}
