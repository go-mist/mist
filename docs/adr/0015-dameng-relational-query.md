# ADR-0015：达梦关系查询用字符串拼 JSON

- **状态**：已接受
- **日期**：2026-11-26
- **里程碑**：M10

## 背景

ADR-0005 的关系查询是 `left join lateral` 加 JSON 聚合。这台 DM8（`xuxuclassmate/dameng`）拒绝 `LATERAL`，也没有 `JSON_OBJECT`、`JSON_ARRAY`、`JSON_ARRAYAGG`。`JSONB_OBJECT_AGG` 只收一对键值，而且是聚合函数，不能放进 `LISTAGG`。

## 决策

`dm.BuildRelationalQuery` 单独拼达梦的形状。其它方言的 SQL 不动。

嵌套关系是相关子查询，不用 `LATERAL`。`many` 用 `LISTAGG` 把每一行拼成的对象收成数组，空结果是 `'[]'`。`one` 是同一段对象表达式，没有行时子查询是 SQL NULL。带了 `order by` 的 `many` 在内层加 `row_number() over (...) as "ord"`，`LISTAGG` 按 `"ord"` 排。没写排序时按第一个普通列排，避免对 JSON 文本排序。

对象不是 JSON 函数。每个值先看 `DUMP` 的类型码：`Typ=7` int、`Typ=8` bigint、`Typ=9` number/decimal、`Typ=11` float 写成 `trim(to_char(...))`。其它类型加引号，并转义反斜杠、引号和换行。已经是 JSON 文本的关系列原样拼进去。`CastInJSON` 为 `text` 时内层写成 `cast(列 as varchar(8188))`。只写 `offset` 时补 `limit 2147483647`。

包裹条件和 SQLite 相同：有 `with`、有 `limit`，并且写了 `order`，或者写了 `offset` 且 `where` 里没有关系条件。

## 后果

- `one`、`many`、`through` 已在这台 DM8 上跑过。
- `LISTAGG` 和 `varchar(8188)` 都有长度上限。超长的嵌套文本会在服务器上报错，而不是悄悄截成另一条语句。
- `sql/` 和 `query/` 没有为这条路径加分支。

## 被否决的方案

| 方案 | 否决理由 |
|---|---|
| 把 `LATERAL` 和 `JSON_ARRAYAGG` 原样发出去 | 这台服务器在这两个名字上报语法错误 |
| 用 `JSONB_OBJECT_AGG` 拼一行的多个字段 | 它只收一对参数，嵌进 `LISTAGG` 会报嵌套分组函数 |
