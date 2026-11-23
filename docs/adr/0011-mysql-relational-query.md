# ADR-0011：MySQL 关系查询不用 PostgreSQL 的 JSON 形状

- **状态**：已接受
- **日期**：2026-11-23
- **里程碑**：M8

## 背景

ADR-0005 把关系查询的 SQL 定成 PostgreSQL：`left join lateral` 加 `row_to_json` / `json_agg`。MySQL 8.4 有 `lateral`，没有这两个函数。聚合要用 `json_arrayagg(json_object(...))`。

包裹条件也不一样。PostgreSQL 在有关系、并且出现 offset、或 limit 配 order、或关系条件时，把当前表包进子查询。MySQL 的优化器不会把 `order by` / `limit` 下推到 lateral join 下面，所以 drizzle-orm v1.0.0-rc.5 的 mysql-core 只在「有 limit，并且写了 order，或者写了 offset 且 where 里没有关系条件」时包裹。只因为关系条件而包，MySQL 上可能更慢。

## 决策

`mysql.BuildRelationalQuery` 单独拼 MySQL 的形状。`relation` 和 `pg` 的 SQL 不动。

`many` 是：

```sql
left join lateral(select coalesce(json_arrayagg(json_object('id', `id`)), json_array()) as `r` from (
  select ...
) as `t`) as `posts` on true
```

`one` 换成 `json_object(...)`，没有 `coalesce`。内层 `limit 1`。字段列表按这一层选出的键，不使用 `t.*`。

嵌套的 `many` 带了 `order by` 时，内层 select 末尾加 `row_number() over (order by ...)`。`json_arrayagg` 自己不接受 `order by`，这一列不进 JSON 对象。

只写 `offset` 时补 `limit 18446744073709551615`。drizzle 的 mysql-core 会写出单独的 `offset`，MySQL 拒绝执行。

嵌套层不写 `::类型`。`CastInJSON` 为 `text` 时写成 `cast(列 as char)`，让 `decimal`、`datetime` 和超出 JSON 安全整数的 `bigint` 以字符串进 JSON。其它 cast 名直接报错。没写 `CastInJSON` 的整数仍是 JSON 数字。`cast as char` 出来的时间没有时区，`ReadTextTime` 按 UTC 解析。`op.ILike` 仍收成 `lower(列) like lower(参数)`。多对多仍走中间表的 `inner join`，别名是 `tr` / `ft` / `ot`。`through`、关系 `exists`、`count` 排序和 `extras` 已在 MySQL 8.4 上跑过。

## 后果

- PostgreSQL 的关系查询 SQL 保持 ADR-0005。
- 两种方言的包裹条件不同。把 PostgreSQL 的 `ShouldWrap` 用到 MySQL 上会把「只有关系条件」也包进去，和 rc.5 的 mysql-core 不一致。
- `sql/` 和 `query/` 没有为这条路径加分支。

## 被否决的方案

| 方案 | 否决理由 |
|---|---|
| 把 `row_to_json` 原样发给 MySQL | 服务器没有这个函数 |
| 让 MySQL 调用 `relation.ShouldWrap` | 包裹条件和 rc.5 的 mysql-core 不同 |
