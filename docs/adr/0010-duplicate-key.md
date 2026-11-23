# ADR-0010：on duplicate key update 不并进 on conflict

- **状态**：已接受
- **日期**：2026-11-23
- **里程碑**：M8

## 背景

`query.Insert` 的冲突子句是按 PostgreSQL 的 `on conflict` 建的：目标列、目标 `where`、`do nothing`、更新 `where`。MySQL 的 `on duplicate key update` 没有这些。任何唯一键冲突都会触发，也不能指定部分索引。

M8 的约定是不把两者翻译成对方。插入链又在 `query` 里，MySQL 包加不了方法。

## 决策

`Insert` 增加 `OnDuplicateKeyUpdate`。它只把 `Set` 放进 `InsertSpec.Duplicate`，不填 `Conflict`。

MySQL 对 `values` 渲染成 `as newrow on duplicate key update`。`insert select` 不能把行别名放在 select 后面，所以套一层子查询，列名按插入列对齐，拟写入的值仍用 `mysql.NewRow`。PostgreSQL 看到 `DuplicateKey` 直接报错。`OnConflictDoNothing` 和 `OnConflictDoUpdate` 在 MySQL 上仍然报错。

## 后果

- `InsertSpec` 多了两个字段。这是规格要能表达第二种冲突子句，不是在 `sql/` 的渲染器里为 MySQL 加分支。
- 两种写法可以同时出现在链上。渲染时拒绝，不选其中一个。

## 被否决的方案

| 方案 | 否决理由 |
|---|---|
| 把 `OnConflictDoUpdate` 译成 `on duplicate key update` | 目标列和 `where` 会被丢掉，调用方看不出来 |
| 只在 MySQL 包里另写一条插入链 | 列、多行 `values` 和 `Set` 要再实现一遍 |
