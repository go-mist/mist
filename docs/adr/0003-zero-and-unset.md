# ADR-0003：零值与「未设置」

- **状态**：已接受
- **日期**：2026-11-20
- **里程碑**：M1、M2
- **相关**：[ADR-0002](./0002-query-syntax-and-scan.md)、[ADR-0004](./0004-schema-source.md)

## 背景

Go 的零值分不出「没写」和「写成零」。`Limit(0)` 与不写 limit、SQL NULL 与空字符串、`Age: 0` 与「不更新 age」是三件不同的事。gorm 用反射猜字段写了没有，猜错就会把零值漏写或把未设置的列写进去。

## 决策

三件事用三种机制，不共用一个可选类型。

1. **子句是否出现，看调用了没有。** `Limit(0)` 渲染成 `LIMIT 0`。不调用 `Limit` 就没有 limit 子句。`Offset` 相同。标记是构建器节点里的一个 `bool` 加值，不出现在公开签名上。
2. **SQL NULL 只由可空列的 `*T` 表示。** 非空列的类型是 `T`。`nil` 指针就是 NULL，不做第二种解释。可空性和字段类型是否一致，由 `go generate` 检查（见 ADR-0004）。
3. **写入哪些列，由列清单决定。** 清单里的零值照写。要数据库默认值，就不要把那一列放进清单。`update` 用 `Set(Users.Age, 0)`。

```go
db.Insert(Users).
    Columns(Users.Name, Users.Email).
    Values("alice", "a@example.com")

db.Update(Users).
    Set(Users.Age, 0).
    Where(op.Eq(Users.ID, id))
```

不引入 `Option[T]` 或 `samber/mo`。关系查询的配置字面量还没做；真做的时候也不能把 `*T` 同时用来表示「没设 limit」和 SQL NULL。

## 后果

- 测试要覆盖 `Limit(0)`、可空列的 `nil`、以及 `Set(col, 0)` 确实写进 SQL。
- 插入默认列（`bigserial`、`default=now()`）必须显式排除在列清单之外，不能靠零值跳过。

## 被否决的方案

| 方案 | 否决理由 |
|---|---|
| 构建器公开 `*int` | `nil` 与 SQL NULL 的 `*T` 混成同一个概念 |
| 按零值跳过字段 | `0` 和「不更新」无法同时表达，这是 gorm 的行为 |
| 全库一个 `Option[T]` | 热路径上多一层包装；子句存在与否用调用本身就够了 |
