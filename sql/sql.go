package sql

// SQL 是一段待渲染的 SQL 片段，由一串 Chunk 组成。
//
// 对应 drizzle 的 SQL 类。drizzle 的 `sql` 标签函数在 Go 里没有等价物
// （Go 没有模板字符串），因此由 S(...) 承担同样的职责：把字面量、片段、
// 参数按顺序拼成一串 chunk。
//
// 重要差异：Go 的 S(...) 把裸字符串当作**绑定参数**，而不是字面量 ——
// 这是安全默认值，与 drizzle 标签函数“插值即绑定”的保证一致。
// 字面量必须显式用 Raw("...") 或 Fragment("...")。
type SQL struct {
	QueryChunks []Chunk

	shouldInlineParams bool
}

// GetSQL 实现 Chunk。SQL 是它自己的 wrapper。
func (s *SQL) GetSQL() *SQL { return s }

// Append 把另一段 SQL 的 chunk 追加到末尾，并返回自身。
// 与 drizzle 的 append 一致：原地修改。
func (s *SQL) Append(q *SQL) *SQL {
	if q == nil {
		return s
	}
	s.QueryChunks = append(s.QueryChunks, q.QueryChunks...)
	return s
}

// InlineParams 打开内联参数模式：参数直接写进 SQL 文本而不是绑定。
func (s *SQL) InlineParams() *SQL {
	s.shouldInlineParams = true
	return s
}

// If 在 cond 为 true 时返回自身，否则返回 (*SQL)(nil)。
// 与 drizzle 的 if 一致，调用方通常把它直接喂给 And/Or/S。
//
// 注意：返回值是 nil 指针而不是 nil 接口，塞进 Chunk 后接口本身不为 nil。
// 渲染器与 And/Or 用 isNilChunk 识别这种情况，与 drizzle 的 undefined 等价。
func (s *SQL) If(cond bool) *SQL {
	if !cond {
		return nil
	}
	return s
}

// As 给这段 SQL 起一个字段别名，对应 drizzle 的 sql`...`.as("alias")。
func (s *SQL) As(alias string) *Alias {
	return &Alias{SQL: s, FieldAlias: alias}
}

// S 按顺序拼装一段 SQL。
//
// 参数规则：
//   - Chunk（*SQL、Fragment、Name、Param、*Placeholder、*Alias、*Subquery、
//     *ValueRows、List，以及 M1 之后的表与列）原样进入片段流；
//   - nil 被视为“缺失”，直接跳过 —— 对应 drizzle 标签里 `param !== undefined`
//     的过滤；
//   - []any、[]Chunk 和 List 展开成括号列表。其它切片（含 []int）整个作为
//     一个参数绑定；要按元素展开请用 ListOf。[]byte 始终是一个参数；
//   - 其余值（包括裸 string）统一包装成 *Param 绑定。要绑定 NULL 请显式传
//     NewParam(nil, nil)，因为裸 nil 会被跳过。
func S(parts ...any) *SQL {
	chunks := make([]Chunk, 0, len(parts))
	for _, p := range parts {
		if ch, ok := toChunk(p); ok {
			chunks = append(chunks, ch)
		}
	}
	return &SQL{QueryChunks: chunks}
}

// Raw 构造一段字面 SQL：不转义、不加引号、不绑定参数。
// 对应 drizzle 的 sql.raw(str)。
func Raw(str string) *SQL {
	return &SQL{QueryChunks: []Chunk{Fragment(str)}}
}

// Empty 返回空片段，对应 drizzle 的 sql.empty()。
func Empty() *SQL { return &SQL{} }

// Join 用 separator 连接一组片段。
// 对应 drizzle 的 sql.join()：separator 为 nil 或列表为空时只做浅拷贝。
func Join(chunks []Chunk, separator Chunk) *SQL {
	if separator == nil || len(chunks) == 0 {
		return &SQL{QueryChunks: append([]Chunk(nil), chunks...)}
	}
	result := make([]Chunk, 0, len(chunks)*2-1)
	for i, ch := range chunks {
		if i > 0 {
			result = append(result, separator)
		}
		result = append(result, ch)
	}
	return &SQL{QueryChunks: result}
}

// Identifier 构造一个待转义的标识符，对应 drizzle 的 sql.identifier(value)。
func Identifier(value string) Name { return Name(value) }

// NewParam 构造一个绑定参数。encoder 为 nil 时使用 noop 语义。
// 对应 drizzle 的 sql.param(value, encoder)。
func NewParam(value any, encoder Encoder) *Param {
	return &Param{Value: value, Encoder: encoder}
}

// NewPlaceholder 构造一个命名占位符，对应 drizzle 的 sql.placeholder(name)。
func NewPlaceholder(name string) *Placeholder {
	return &Placeholder{Name: name}
}

// NewSubquery 构造一个派生表：渲染为 `(...) "alias"`。
func NewSubquery(query *SQL, aliasName string) *Subquery {
	return &Subquery{SQL: query, AliasName: aliasName}
}

// ListOf 把一组值包装成 List，用于 SQL 标签里数组插值的展开语义：
// ListOf([]int{1, 2, 3}) 渲染为 `($1, $2, $3)`。
//
// 为什么需要泛型版本：Go 的 ...any 参数不能接收 []int 这类具体切片，
// 而 drizzle 的数组展开对任意元素类型都成立。
func ListOf[T any](values []T) List {
	out := make(List, len(values))
	for i, v := range values {
		out[i] = toListElement(v)
	}
	return out
}

// toChunk 把任意值规整成 chunk。第二个返回值表示该值是否保留
// （false 只对应 nil，与 drizzle 过滤 undefined 的行为一致）。
func toChunk(v any) (Chunk, bool) {
	switch x := v.(type) {
	case nil:
		return nil, false
	case Chunk:
		if isNilChunk(x) {
			return nil, false
		}
		return x, true
	case []byte:
		// []byte 在 Go 里是切片，但语义上是单值（bytea），不展开。
		return &Param{Value: x}, true
	}
	if list, ok := sliceToList(v); ok {
		return list, true
	}
	return &Param{Value: v}, true
}

// toListElement 规整一个列表元素：列表里的 nil 绑定为 NULL 参数，
// 对应 drizzle 数组分支里 null 走 param 的行为。
func toListElement(v any) Chunk {
	if v == nil {
		return &Param{Value: nil}
	}
	ch, _ := toChunk(v)
	return ch
}

// sliceToList 只展开已经是 chunk 列表的值。
// []int 这类具体切片不在这里展开：反射展开在热路径上，调用方用 ListOf。
func sliceToList(v any) (List, bool) {
	switch x := v.(type) {
	case []Chunk:
		return List(x), true
	case List:
		return x, true
	case []any:
		return anyList(x), true
	default:
		return nil, false
	}
}

func anyList(values []any) List {
	out := make(List, len(values))
	for i, v := range values {
		out[i] = toListElement(v)
	}
	return out
}
