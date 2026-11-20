# ADR-0002：查询语法与结果扫描

- **状态**：已接受
- **日期**：2026-11-20
- **里程碑**：M2、M3
- **相关**：[ROADMAP M2](../../ROADMAP.md#m2-查询构建器postgresql)、[ADR-0004](./0004-schema-source.md)

## 背景

drizzle-orm 的查询是一条 SQL 形状的链：`select().from().where(and(eq(...))).orderBy().limit()`，类型由列描述符在编译期约束。Go 没有方法级类型参数，也没有模板字符串。如果结果 API 做成 `Scan(dest any)`，扫描只能靠反射，库会退回 bun / gorm。

这个库只做一条流水线：带类型的 SQL 片段，渲染成 SQL，再用生成的函数扫进行结构体。语法贴近 drizzle，性能靠这条流水线，不靠事后换掉反射。

## 决策

### 1. 调用形状与 drizzle 对齐

```go
rows, err := db.Select(Users.ID, Users.Name).
    From(Users).
    Where(op.And(
        op.Eq(Users.Name, "alice"),
        op.Gt(Users.ID, 100),
    )).
    OrderBy(op.Desc(Users.CreatedAt)).
    Limit(10).
    All(ctx)
```

`Select`、`From`、`Where`、`OrderBy`、`Limit` 的顺序和语义与 drizzle 相同。条件、排序、聚合是函数，不是列上的方法。公开入口在包 `op`（`op.Eq`、`op.And`、`op.Desc`），实现落在 `sql` 的 chunk 上。M0 已有的 `sql.And` / `sql.Or` / `sql.Not` 继续作为片段组合子，`op.And` 调用它们，不另写一套语义。

`Where` 接受一个条件，和 drizzle 一样。多个条件用 `op.And` / `op.Or` 组合。

链的尽头是 `All(ctx)` 或 `One(ctx)`。`One` 没有行时返回 `ErrNoRows`。渲染出 `(SQL, args)` 的能力保留，M2 不需要数据库也能对拍。

### 2. 行类型来自表，不来自扫描目标

`Users` 的类型是 `Table[User]`。`From(Users)` 把构建器定成 `Select[User]`。`All` 返回 `[]User`。Go 允许类型参数放在接收者的类型上，所以不需要方法级类型参数。

`Select()` 不写列表示整行。`Select(Users.ID, Users.Name)` 是投影。别名用 `Users.Name.As("n")`，对应 drizzle 投影对象里的键。

### 3. select 列表就是扫描计划

生成的列带字段地址，生成的整行函数直接调用驱动：

```go
var UsersID = pg.Col[User, int64]{
    Name: "id",
    Ptr:  func(u *User) any { return &u.ID },
}

func scanUser(row pg.Row, u *User) error {
    return row.Scan(&u.ID, &u.Name, &u.Email, &u.CreatedAt)
}
```

- 整行：调用 `scanUser`。
- 子集：查询值保存这次选中列的 `Ptr`。列表在构建时确定，每一行只按它取地址。

不提供 `Scan(dest any)`，也不提供反射后备。手写的子集结构体要扫描，必须有自己的生成扫描函数。

`[]any` 只出现在驱动 `Scan(...any)` 的边界。指针放进接口不额外分配。这块缓冲区挂在查询值上，按行复用。

### 4. 构建器是不可变节点

每次 `Where` / `OrderBy` 分配一个小节点，分叉时共享前缀，渲染时走一遍。不按值复制整个构建器。同一查询形状再次执行时，SQL 文本和扫描计划一起复用，只换参数。

### 5. 验收

整行 `All` 的 allocs/op 与手写 `rows.Scan(&u.ID, ...)` 相同。多一次按行分配，这条设计就不成立。

## 后果

- 关系查询（M5）如果要做，只能在这条链之上拼 SQL，不能改 `Select[T]` 和 `Chunk` 的形状，也不能引入反射映射。ADR-0006 因此不另写：行映射只有生成函数这一条路。
- M0 的 `sliceToList` 仍用反射展开任意切片。M1 开工前删掉，列表只走 `ListOf[T]`。
- `op` 与 `sql` 的分工要在 godoc 里写清，避免调用方找到两个 `And`。

## 被否决的方案

| 方案 | 否决理由 |
|---|---|
| `Scan(ctx, dest any)` | 把反射锁进签名，慢路径会变成实际用法 |
| 包级 `All[T](ctx, query)` | `T` 保证不了和 select 列表一致，看起来像类型安全 |
| 列方法 `Users.Name.Eq("alice")` | 读起来像 bun / ent，不再是 drizzle 的函数式 SQL |
| `Where` 可变参数自动 AND | 与 drizzle 的单个条件不一致，出现第二种读法 |
| 大切片构建器按值拷贝 | 链式调用的分配随子句增长 |
| 先反射扫描，热路径以后再生成 | 反射会先变成 API |
