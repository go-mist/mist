# ADR-0001：SQL 片段模型在 Go 里的表达方式

- **状态**：已接受
- **日期**：2026-11-17
- **里程碑**：M0
- **相关**：[ROADMAP M0](../../ROADMAP.md#m0-地基sql-片段层与渲染器)

## 背景

drizzle-orm v1.0.0-rc.5 的 SQL 片段模型由两部分组成：

- `Chunk`（公开类型并集）：`string | Table | View | AnyColumn | Name | Param | Placeholder | SQL`
- `SQLChunk`（内部类型并集）：`StringChunk | SQLChunk[] | SQLWrapper | SQL | Table | View | Subquery | AnyColumn | Param | Name | undefined | FakePrimitiveParam | Placeholder`

渲染器 `collectSQL` 用一个数字 kind 表分派（`chunkKinds`），并在原型上缓存 kind。

Go 没有类型并集、没有 `undefined`、没有基于原型的分派。核心问题是：**用什么表达“凡是能渲染进 SQL 的东西”**。

## 决策

### 1. `Chunk` 以 `GetSQL() *SQL` 为契约

```go
type Chunk interface{ GetSQL() *SQL }
```

理由是它与 drizzle 的 `SQLWrapper` 判定完全同构（`isSQLWrapper` 判的就是 `typeof value.getSQL === 'function'`）。好处是**开放集合**：M1 的表、列，以及未来任何用户自定义片段，只要实现 `GetSQL` 就能进入片段流，不需要修改 `sql` 包。

渲染器用类型 switch 按具体类型分派，顺序与 drizzle 的 `collectSQL` 逐条对齐（string → sql → name → param → placeholder → table → … → wrapper → array）。

### 2. `entity.Kind` 只作为“分派结果”的值

Go 不需要运行时类型标识（`is(value, Table)` 退化为类型断言），但**需要知道一个值属于哪一类**：M2 的 `bindIfParam` 必须先判断待绑定值是否已经是 param/placeholder/column/table/view。

因此保留 `entity.Kind` 与 `sql.KindOf(chunk)`，取值顺序与 rc.5 的 `chunkKinds` 一致。不保留的是 rc.5 的缓存机制（原型的 `Symbol` 属性 + `WeakMap` 构造器表）。

### 3. 字符串按参数绑定，字面量必须显式

Go 没有模板字符串，`S(...)` 无法区分“字面量”与“插值”。两条路都有代价：

- 裸字符串当字面量 → 用户数据一旦是 `string` 就直接拼进 SQL，**SQL 注入的默认值**；
- 裸字符串当参数 → 写字面量要 `Raw("...")`，啰嗦，但**默认安全**。

选择后者。这与 drizzle 标签函数“插值即绑定”的保证一致：`sql`${x}`` 里 `x` 永远是参数，字面量来自模板本身。Go 侧用 `Raw` / `Fragment` 显式标记“这是原文”。

### 4. nil 的两种含义分开

- `S(..., nil)`：**缺失**，跳过 —— 对应标签函数的 `if (param !== undefined)`；
- `NewParam(nil, nil)`：**绑定 NULL**；
- 列表元素为 nil（`ListOf([]any{..., nil})`）：绑定 NULL —— 对应数组分支里 `null` 走参数路径。

同时 `(*SQL)(nil)`（`If(false)` 的返回值）也必须当作缺失，由 `isNilChunk` 识别。这是 Go 特有的坑：nil 指针装进接口后接口本身不为 nil。

### 5. 方言接口只有三个转义函数

```go
type dialect.Dialect interface {
	EscapeName(name string) string
	EscapeParam(num int, value any) string
	EscapeString(str string) string
}
```

对应 rc.5 `BuildQueryConfig` 里的三个回调。**没有**沿用“配置结构体携带回调”的形式：Go 里接口比回调字段更好组合，也更容易在测试里替换。

`EscapeParam` 多带一个 `value` 参数（rc.5 的 pg 实现忽略它），为 MySQL 的 `?` 与未来需要按值形态取号的方言留出位置。

### 6. 不移植 `tagged` 与 `invokeSource`

- `tagged: true` 产出 `_sql` 片段数组，唯一消费者是 Bun.SQL 的缓存优化；Go 没有对应场景。
- `invokeSource` 是 mssql 与 drizzle-kit 元数据查询的分派开关；M0 只有 pg，等真正需要时再加。

### 7. 保留 `ValueRows.GetSQL()`

rc.5 为“不认识 ValueRows 的构建器”保留了这个兼容壳。Go 保留它有两个现实理由：`Chunk` 接口以 `GetSQL` 为契约（必须实现），以及 M2 之前构建器可以先按普通片段处理多行 values。

## 后果

**正面**

- 片段模型是开放集合，M1 加表/列不需要改 `sql` 包的结构。
- 安全默认值让“把用户输入拼进 SQL”需要显式动作。
- 分派是类型 switch，没有字符串比较，也不需要缓存；基准显示一次完整渲染 17 次分配（约 0.7 µs）。

**负面 / 需要长期注意**

- 渲染器的 `case` 顺序是语义的一部分（例如 Column 必须在 Wrapper 之前，否则 `GetSQL` 会自引用递归）。M1 加列类型时必须把它插到 `*Param` 之前、`default` 之前。
- 没有 `tagged` 模式，未来若接 Bun 类驱动需要额外工作。
- 字符串绑定语义与 drizzle 的写法差异较大，文档必须反复强调，否则用户会以为 `S("select ...")` 是字面量。

## 被否决的方案

| 方案 | 否决理由 |
|---|---|
| `Chunk` 为封闭标记接口（`isChunk()`） | 其他包（`pg`）无法为表/列实现未导出方法，除非把标记方法导出成无意义的 `Chunk()` |
| `[]any` 作为片段列表 | 放弃编译期检查；把“标量必须包成 Param”的规则推迟到运行时 |
| 格式字符串 API（`S("select * from {0}", tbl)`） | 需要解析花括号转义，且仍然无法阻止把用户输入当字面量 |
| 保留 rc.5 的 `StringChunk` 驻留表 | Go 的字符串常量不额外分配，驻留没有收益 |
