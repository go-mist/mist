# ADR-0014：SQLite 关系查询不用 lateral

- **状态**：已接受
- **日期**：2026-11-23
- **里程碑**：M9

## 背景

ADR-0005 的关系查询是 `left join lateral`。ADR-0011 在 MySQL 上仍用 `lateral`，只把 JSON 函数换成 `json_object` / `json_arrayagg`。mist 嵌入的 SQLite 3.53.3 没有 `lateral`。它有 `json_object`、`json_group_array` 和 `json()`。

`json_object` 会把文本再编成 JSON 字符串。嵌套关系的列本身已经是 JSON 文本，直接放进去会变成字符串，读的时候解不出数组。

## 决策

`sqlite.BuildRelationalQuery` 单独拼 SQLite 的形状。`relation`、`pg` 和 `mysql` 的 SQL 不动。

`many` 是相关子查询：

```sql
(select coalesce(json_group_array(json_object('id', "id")), json_array())
 from (select "d1"."id" as "id" from "posts" as "d1" where "d0"."id" = "d1"."author_id") as "t") as "posts"
```

`one` 换成 `json_object(...)`，没有 `coalesce`。内层 `limit 1`。嵌套的 `many` 带了 `order by` 时，内层加 `row_number() over (order by ...) as "ord"`，`json_group_array(... order by "ord")`。`ord` 不进 JSON。关系列放进 `json_object` 时写成 `json("comments")`。

根查询的包裹条件和 MySQL 相同：有 `with`、有 `limit`，并且写了 `order`，或者写了 `offset` 且 `where` 里没有关系条件。SQLite 允许单独的 `offset`，所以不补最大 `limit`。

`CastInJSON` 为 `text` 时写成 `cast(列 as text)`。其它 cast 名报错。`op.ILike` 仍收成 `lower(列) like lower(参数)`。

## 后果

- PostgreSQL 和 MySQL 的关系查询 SQL 不变。
- `numeric` 不保留小数末尾的 0。`12.50` 经 `cast as text` 读回来是 `12.5`。
- `sql/` 和 `query/` 没有为这条路径加分支。

## 被否决的方案

| 方案 | 否决理由 |
|---|---|
| 把 `lateral` 原样发给 SQLite | 3.53.3 在 `lateral` 后面报语法错误 |
| 把关系 JSON 文本直接放进 `json_object` | 嵌套数组会被编成字符串，解不出 `[]jsontext.Value` |
