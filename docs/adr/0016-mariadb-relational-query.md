# ADR-0016：MySQL 关系查询不再用 LATERAL

- **状态**：已接受
- **日期**：2026-11-26
- **里程碑**：M10

## 背景

ADR-0011 把 MySQL 的关系查询写成 `left join lateral` 加 `json_object` / `json_arrayagg`。MySQL 8.4 接受 `LATERAL`。当前 MariaDB 在 `LATERAL` 上报语法错误。`mysql` 包同时覆盖 MariaDB，不另开方言。

## 决策

`mysql.BuildRelationalQuery` 把嵌套关系写成 `SELECT` 列表里的相关子查询。`many` 是 `json_arrayagg(json_object(...))`，空结果是 `json_array()`。`one` 是 `json_object(...)`。

派生表里不引用外层列。父键选成 `mfk0` 这类列，比较写在派生表外面。当前 MariaDB 拒绝派生表里的外层列，也拒绝 `LATERAL` 和 `cast(x as json)`。关系列直接放进 `json_object`，MySQL 8.4 和 MariaDB 11.4 都会把它嵌成 JSON，而不是字符串。

`one` 在派生表里按父键做 `row_number()`，外面只留 `mfkord = 1`。`many` 在带了 `order`、`limit` 或 `offset` 时同样编号，`partition by` 父键，再在外面按 `mfkord` 过滤。编号写进 JSON。读回来之后按这个编号排序，再丢掉该字段。MariaDB 的 `json_arrayagg` 不保留外层 `order by`，MySQL 8.4 又拒绝写在 `json_arrayagg` 里面的 `order by`，所以顺序放在读结果这一步。根查询的包裹条件仍和 ADR-0011 相同。嵌套层本身不再为了分页去包裹，限行改由 `mfkord` 完成。

PostgreSQL 继续用 `LATERAL`。`sql/` 和 `query/` 不加分支。

## 后果

- 同一条 SQL 能在 MySQL 8.4 和 MariaDB 11.4 上执行。已在这两台库上跑过 one、many、through。
- 带分页的嵌套关系会先给子表每一行算内层关系，再按 `ord` 丢掉不要的行。
- ADR-0011 里 `left join lateral` 的示例不再是 `mysql` 包发出的语句。
