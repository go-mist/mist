# ADR-0004：schema 的真相源

- **状态**：已接受
- **日期**：2026-11-20
- **里程碑**：M1
- **相关**：[ADR-0002](./0002-query-syntax-and-scan.md)、[ADR-0003](./0003-zero-and-unset.md)

## 背景

列必须对应一个可扫描的 Go 字段，否则 ADR-0002 的生成扫描函数没有落点。tag 能描述单列，表达不了表名、复合主键、复合唯一、索引和 CHECK。纯 builder 再描述一遍列，就会和结构体各写各的；从 builder 生成结构体，又要把 `json` 和校验 tag 搬进 DSL。

运行时不读 tag。tag 只在 `go generate` 时读一次。

## 决策

用户手写的结构体是列的真相源。表名和表级约束放在 `Table()` 里。

```go
type User struct {
    ID        int64     `mist:"id,bigserial,pk"`
    Name      string    `mist:"name,text,notnull"`
    Email     *string   `mist:"email,text,unique"`
    CreatedAt time.Time `mist:"created_at,timestamptz,notnull,default=now()"`
}

func (User) Table() pg.TableMeta {
    return pg.TableMeta{Name: "users"}
}
```

tag 只留列级修饰：`pk`、`notnull`、`unique`、`default`、`references=表.列`、`ondelete`、`onupdate`、`length`、`precision`。单列外键留在 tag 里。复合主键、复合外键、索引、CHECK 只写在 `Table()`。字段上有 `pk`，`Table()` 里又写了 `PrimaryKey`，生成器报错并指出行号。

没有 `Table()` 就生成失败。不从类型名猜测表名。

生成物是同包的描述符和扫描函数，例如 `Users`、`Users.Name`（`Col[User, string]`）、`Users.Name.Ptr`、`scanUser`。不保留 tag 字符串，不建 schema 缓存。

生成器要拦住：SQL 类型和 Go 类型对不上、可空列没用 `*T`、非空列用了指针、列名重复、`references` 悬空、单列 `pk` 与 `Table().PrimaryKey` 冲突。

## 后果

- README 里的 `User` 必须有 `Table()`，单列主键修饰符是 `pk`。
- `Table()` 解析 `Name`、复合主键、索引和 CHECK。列级 `unique`、`default`、外键、长度、精度、`generated`、`identity` 写进生成的 `ColumnSpecs()`。
- 结构体上不挂 `Save` / `Find`，也没有钩子。它是扫描目标，不是模型。

## 被否决的方案

| 方案 | 否决理由 |
|---|---|
| 纯 builder 作为真相源 | 列与扫描字段会分成两份，或 DSL 重新实现一遍 tag |
| 表级约束写进 tag | tag 会变成另一门语言 |
| 从类型名复数化得到表名 | `User` → `users` 这类规则会猜错 |
| 运行时解析 tag 并缓存 | 这是 gorm / bun 的 schema 模型 |
