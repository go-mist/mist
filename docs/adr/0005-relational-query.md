# ADR-0005：关系查询的 SQL 形状

- **状态**：已接受
- **日期**：2026-11-20
- **里程碑**：M5
- **相关**：[ROADMAP M5](../../ROADMAP.md#m5-关系查询rqb)、[ADR-0002](./0002-query-syntax-and-scan.md)

## 背景

rc.5 的 `PgDialect.collectRelationalQuery` 决定关系查询长什么样。`strategy` 不写时是 `optimized`。`basic` 保持同一层查询的外形；`optimized` 只在「这一层带了关系，并且有分页或关系条件」时，把这一层表包进子查询。两种外形都要先写死，再写 Go。

`one` / `many` 用 lateral join 把子行收成 JSON。多对多走中间表。`comment` 写在该层查询末尾。`withCache` 不改变 SQL。

## 决策

### 1. 别名

根表和每一层关系目标表用 `d0`、`d1`、`d2`。`where` 里的关系子查询用 `f0`、`f1`。排序子查询用 `o0`、`o1`。同一层里两个互不嵌套的 `exists` 都用 `f0`，因为它们分属不同子查询。

中间表跟在目标表后面：`with` 用 `tr0`，`exists` 用 `ft0`，排序用 `ot0`。数字是父层深度。`on` 是中间表的目标列等于目标表。`where` 是源表等于中间表的源列。

列在 select 列表里写成 `"别名"."列名" as "键"`。键是 schema 里的列键，用来对上 JSON 字段。

### 2. `one` 和 `many`

`many` 接在 select 列表后面的是 `"posts"."r" as "posts"`，连接是：

```sql
left join lateral(select coalesce(json_agg(row_to_json("t".*)), '[]') as "r" from (
  select ... from "posts" as "d1" where "d0"."id" = "d1"."author_id"
) as "t") as "posts" on true
```

`one` 换成 `row_to_json("t".*) "r"`，没有 `coalesce`，也没有 `as`。内层固定 `limit` 一个参数 `1`。关系条件写在内层 `where`，连接条件是 `on true`。

没有 `with` 的三层 `users → posts → comments`，`optimized` 和 `basic` 的 SQL 相同。这一层没有分页，也没有关系条件，不会包外层子查询。

多对多在目标表别名后写 `inner join`。包裹时，这个 join 留在子查询里面、`where` 前面。`relation.ManyOf` 从反向关系推断时，会交换中间表的源列和目标列。

`extras` 按调用方写的表达式渲染成 `(expr) as "键"`，放在列后面、关系列前面。表达式里要用这一层的别名，例如 `d0`。根行按 `Ptr` 扫描。嵌套行从 JSON 按键写入。

`comment` 接在该层 `limit` 后面，前面有一个空格。字符串里的 `/*` 和 `*/` 会被拆开。`CommentTags` 按键排序，值用 `encodeURIComponent`，空值跳过。空注释不写入 SQL。

`WithCache` 不改 SQL。只有根配置参与缓存。值为 true 但没有 `Cache` 时返回错误。`MemoryCache` 只按 SQL 和参数记住 JSON 结果，不会在表变更时丢掉旧值。

### 3. `strategy`

空字符串和 `optimized` 都按 optimized。`basic` 永不包裹。

包裹条件：这一层的 `with` 非空，并且下面任一成立：

- 写了 `offset`
- 写了 `limit` 且写了 `orderBy`
- `where` 里有关系条件或 `RAW`

`findFirst` 视为带 `limit 1`。关系连接自己的等值条件不算关系筛选，不会因此包裹。

包裹之后，这一层的 `where` 放进内层。又满足分页时，`order by` / `limit` / `offset` 也放进内层，外层再写一次 `order by`，不再写 `limit` / `offset`。只因关系条件而包裹时，`order by` / `limit` / `offset` 留在外层。

对应的两条 SQL：

```sql
select "d0"."id" as "id", "d0"."name" as "name", "posts"."r" as "posts"
from (select "d0".* from "users" as "d0" order by "d0"."id" asc limit $1 offset $2) as "d0"
left join lateral(...) as "posts" on true
order by "d0"."id" asc
```

```sql
select "d0"."id" as "id", "d0"."name" as "name", "posts"."r" as "posts"
from (select "d0".* from "users" as "d0" where exists (select * from "posts" as "f0" where "d0"."id" = "f0"."author_id")) as "d0"
left join lateral(...) as "posts" on true
```

`basic` 不包，`where` / `order by` / `limit` / `offset` 都写在 lateral join 后面。

### 4. `where` 和 `orderBy`

筛选用构造出来的数据，不用回调。多个条件的括号与 `sql.And` / `sql.Or` 相同：两个及以上是 `((a) and (b))`，一个条件不加这层括号。

列条件和关系条件写在一起时，关系条件变成 `exists` 或 `not exists`。`NOT` 再包一层 `not (...)`。布尔假是 `not exists`，外面不再套 `not`。

`desc(count)` 是标量子查询：

```sql
order by (select count(*) from "posts" as "o0" where "d0"."id" = "o0"."author_id") desc
```

`many` 上按列聚合必须带 `min` / `max` / `count` / `avg` / `first`。`one` 上的列不能带聚合，子查询末尾是字面量 `limit 1`。

### 5. 结果怎么放进结构体

根行的列和 `extras` 用字段地址交给驱动 `Scan`。关系列扫成 JSON。嵌套对象按 select 列表里的键，把 JSON 写进对应字段的地址。

不遍历结构体找字段。`many` 的空数组要调用 `Init`，否则 Go 里的 nil 切片和 `[]` 分不开。`Place` 负责把子行放进父行。`one` 的 JSON null 不调用 `Place`，字段保持 nil。

嵌套层才套 JSON cast。列上的 `CastInJSON` 非空，并且只含字母、数字和下划线时，写成 `"别名"."列"::<CastInJSON>`。`numeric`、`timestamp`、`timestamptz`、`bigint` 用 `text`，避免 JSON 数字丢掉精度。根行仍交给 pgx，不套这段 cast。`bytea` 不 cast；`row_to_json` 给出的 `\x` 文本用 `ReadHexBytes`。时间文本用 `ReadTextTime`。

### 6. 调用方仍然要写的部分

嵌套字段要在 schema 上写 `New`、`Ptr`、`Init`、`Place`。没有生成「行类型加上关系」的类型。`extras` 的表达式由调用方写成 SQL 片段，库不把原表名改成别名。

## 后果

- 关系查询不改变 `Select[T]` 和 `Chunk`。入口是 `pg.FindMany[T]` / `pg.FindFirst[T]`。
- 默认 strategy 与 rc.5 相同，是 `optimized`。
- 嵌套结果的字段由调用方在 schema 上写好 `New`、`Ptr`、`Init`、`Place`。没有生成「行类型 ∩ 关系」的类型。
