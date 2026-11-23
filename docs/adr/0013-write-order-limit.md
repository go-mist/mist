# ADR-0013：update / delete 的 order by 和 limit 只在 MySQL 单表上写

- **状态**：已接受
- **日期**：2026-11-23
- **里程碑**：M8

## 背景

MySQL 的单表 `update` 和 `delete` 可以在 `where` 后面写 `order by` 和 `limit`，用来只改或只删排序后的前几行。多表形式没有这两个子句。PostgreSQL 的 `update` / `delete` 也没有。

链在 `query` 里，MySQL 包加不了方法。

## 决策

`Update` 和 `Delete` 增加 `OrderBy` 和 `Limit`。`Limit(0)` 渲染成 `limit 0`。MySQL 单表把它们写在 `where` 后面。多表时直接报错。PostgreSQL 看到其中任何一个就报错，不改写成子查询。

## 后果

- `UpdateSpec` 和 `DeleteSpec` 多了排序和行数字段。这是规格要能表达 MySQL 的单表写法，不是在 `sql/` 里加分支。
- 没调用 `Limit` 时不写 `limit`。`LimitSet` 用来区分「没写」和 `0`。

## 被否决的方案

| 方案 | 否决理由 |
|---|---|
| 在 PostgreSQL 上改成 `where id in (select ... limit)` | 调用方没要求包一层，锁和可见性都不一样 |
| 多表时悄悄丢掉 order by / limit | 语句会改到更多行 |
