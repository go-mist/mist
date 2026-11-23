# ADR-0012：insert ignore 不并进 on conflict do nothing

- **状态**：已接受
- **日期**：2026-11-23
- **里程碑**：M8

## 背景

MySQL 的 `insert ignore` 在唯一键冲突时丢掉新行，语句继续，旧行不变。它还会忽略另一些数据错误。PostgreSQL 的 `on conflict do nothing` 只处理声明的冲突目标，可以带 `where`，其它错误照样失败。

两者都是「冲突时不报错」，但作用范围不一样。

## 决策

`Insert` 增加 `Ignore`。MySQL 渲染成 `insert ignore into`。它可以和 `OnDuplicateKeyUpdate` 同时出现，语句里两个子句都写。PostgreSQL 看到 `Ignore` 直接报错。`OnConflictDoNothing` 在 MySQL 上仍然报错。

## 后果

- `InsertSpec` 多一个字段。这是要表达第二种插入修饰，不是在 `sql/` 里为 MySQL 加分支。
- 调用方必须显式写 `Ignore`。不会把 `OnConflictDoNothing` 译过来。

## 被否决的方案

| 方案 | 否决理由 |
|---|---|
| 把 `OnConflictDoNothing` 译成 `insert ignore` | 目标列和 `where` 会被丢掉，其它数据错误的行为也不一样 |
| 只在 MySQL 包里另写插入链 | 列和多行 `values` 要再实现一遍 |
