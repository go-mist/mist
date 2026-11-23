# ADR-0008：DDL 中间表示的边界

- **状态**：已接受
- **日期**：2026-11-23
- **里程碑**：M7
- **相关**：[ADR-0004](./0004-schema-source.md)、[ADR-0009](./0009-ddl-api.md)

## 背景

迁移 SQL 要比较两份结构：结构体里声明的，和数据库里实际的。比较之前先变成同一份结构，否则 diff 会把 Go 的 tag 和 `information_schema` 的列缠在一起。

drizzle-kit 还会处理视图、触发器、函数、分区、权限、扩展。这些对象的差异很容易猜错，猜错的代价是删数据或改掉用户手写的 SQL。

## 决策

中间表示放在 `schema`，不带方言。v1 只收录这些对象：

- 表
- 列：类型名、长度、精度、数组维数、非空、默认值、`generated` 表达式、identity
- 主键
- 唯一约束
- 索引（名字、列、是否唯一）
- CHECK
- 外键
- enum（类型名，以及已知的取值）

不收录视图、触发器、函数、分区、权限、扩展、策略、序列（identity / serial 带出来的序列除外，不单独 diff）。

列按名字配对。一边消失、另一边出现，记成 drop 再 add，不推断 rename。drop 表、drop 列、改列类型、drop enum 标成破坏性变更。

enum 的取值不一定写在结构体上。取值未知时，只要类型名还在，就不把它当成需要重建。取值已知而且两边不一样，记成一笔变更，渲染时直接报错，不生成 `ALTER TYPE`。

`default`、`generated`、CHECK 表达式按 schema 源码原样放进 SQL，不加引号。类型变更渲染成 `SET DATA TYPE`，不带 `USING`。主键约束在还没反读到真实名字时，按 PostgreSQL 的默认名 `{表}_pkey` 删除。

`generated` 表达式发生变化时不生成 SQL，渲染直接报错。PostgreSQL 不能就地改这个表达式。

反读 PostgreSQL 时做三处归一，否则结构体里的写法应用之后对不上库里的目录：

- `bigint` / `integer` / `smallint` 加上 `nextval(...)` 默认值，折回 `bigserial` / `serial` / `smallserial`，并去掉这份默认值。identity 保持整数类型，不折成 serial。
- 外键动作 `NO ACTION` 记成空字符串。
- 字面量默认值末尾由数据库补上的 `::text` 这类转型会去掉。CHECK 和 `generated` 外面多包的一层括号会去掉。

## 后果

- `diff` 只看这份中间表示。`ddl/pg` 只负责把变更写成 PostgreSQL 语句。
- introspection 以后把 `information_schema` 和必要的 `pg_catalog` 填进同一份结构，不另造一套。
- 改名要用户自己把 drop + add 改成 `RENAME`。

## 被否决的方案

| 方案 | 否决理由 |
|---|---|
| 按位置或类型相似猜测 rename | 猜错会丢列上的数据 |
| 把视图和函数放进第一版 | diff 会变成无底洞，而且容易覆盖手写 SQL |
| 取值未知时 drop 再 create enum | 会删掉库里已有的取值 |
