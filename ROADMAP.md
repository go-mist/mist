# ROADMAP

本文档是 mist 的实施计划。**Why / Features 见 [README.md](./README.md)。**

- 参考基准：**drizzle-orm v1.0.0-rc.5**（793 个 `.ts` 文件，约 109,555 行）
- 当前状态：**M0–M9 已完成**（2026-11-23）。生成器读语法树，写出列描述符、扫描函数和 `ColumnSpecs()`。查询链能渲染并在 pgx 池和 `database/sql` 上执行。`numeric`、`jsonb`、`interval`、`bytea`、数组、`uuid`、枚举在参数上带类型后缀。关系查询的 `one` / `many` / `through` 能一次拉出嵌套结果，SQL 见 [ADR-0005](./docs/adr/0005-relational-query.md)。没有 join 时，select 列表仍带表名，见差异登记。迁移器读目录、在一个事务里应用，并用 `pg_advisory_xact_lock` 串行。M7 已完成（2026-11-23）：20 张表的迁移应用到空库再反读，diff 是空的。`mist push` 默认只打印 SQL。M8 已完成：MySQL 8.4 跑通查询矩阵里服务器接受的语句。`on duplicate key update` 单独写，不从 `on conflict` 翻译，见 [ADR-0010](./docs/adr/0010-duplicate-key.md)。DDL、迁移、introspect、`push` 仍只做 PostgreSQL。
- 排期单位：**人周**，按单人全职粗估，只用于判断量级，不是承诺

---

## 目录

- [总体策略](#总体策略)
- [状态定义与完成标准](#状态定义与完成标准)
- [阶段划分](#阶段划分)
- [里程碑详述](#里程碑详述)
  - [M0 地基：SQL 片段层与渲染器](#m0-地基sql-片段层与渲染器)
  - [M1 Schema 定义与代码生成](#m1-schema-定义与代码生成)
  - [M2 查询构建器（PostgreSQL）](#m2-查询构建器postgresql)
  - [M3 执行层：驱动、会话、事务](#m3-执行层驱动会话事务)
  - [M4 Codecs 类型映射](#m4-codecs-类型映射)
  - [M5 关系查询（RQB）](#m5-关系查询rqb)
  - [M6 迁移器](#m6-迁移器)
  - [M7 DDL diff 与 CLI](#m7-ddl-diff-与-cli)
  - [M8 第二方言：MySQL](#m8-第二方言mysql)
  - [M9 SQLite](#m9-sqlite)
  - [M10 生态与打磨](#m10-生态与打磨)
- [依赖关系](#依赖关系)
- [横切工作](#横切工作)
- [ADR 清单](#adr-清单)
- [与 drizzle-orm 的差异登记](#与-drizzle-orm-的差异登记)
- [Go 还原度评估](#go-还原度评估)
- [抄 / 丢 / 换 / 补：逐层处置表](#抄--丢--换--补逐层处置表)
- [版本与兼容策略](#版本与兼容策略)
- [风险登记](#风险登记)
- [明确推迟](#明确推迟)
- [里程碑总表](#里程碑总表)

---

## 总体策略

三条主线，按依赖顺序推进：

1. **先做「数据 → SQL」这一半。** chunk 模型、渲染器、查询构建器、方言。这一半决定了整个库的形态，而且**不需要数据库就能测** —— 与 drizzle 的输出逐字节对拍即可。
2. **再做「数据 → Go 值」这一半。** codegen、codecs、结果映射、关系查询。
3. **最后做「工具」这一半。** 迁移器、DDL diff、CLI。

这个顺序的理由：M0–M2 完成之后，「能不能复刻 drizzle 的设计」这个问题就有答案了。如果那一半做不出 SQL 语感，后面全部没有意义。**先证伪，再投入。**

**v1.0 的门槛**：M0–M7 完成，pg 稳定，且至少被一个第二方言（M8）反向验证过抽象是干净。

---

## 状态定义与完成标准

| 状态 | 含义 |
|---|---|
| `未开工` | 只有计划，没有代码 |
| `进行中` | 已开始，未达验收标准 |
| `已完成` | 达到该里程碑的**全部**验收标准 |
| `已冻结` | 接口定稿，允许新增不允许改签名 |

**每个里程碑的 Definition of Done**（全部满足才算完成）：

1. 有对应包，公开 API 有 godoc 注释
2. 有单元测试；涉及 SQL 输出的，有 golden 文件
3. 有集成测试（涉及数据库时），CI 可跑
4. README / ROADMAP 的状态列已更新
5. 该里程碑产生的设计决策已落 ADR（见 [ADR 清单](#adr-清单)）

**对拍是怎么做的**：在 `internal/testutil` 里维护一份 fixture（Go 侧写查询、TS 侧写等价查询），跑 `tsc` 版本得到 `(sql, params)`，与 Go 侧输出做**逐字节比较**。这是唯一能防止「看起来对、实际不对」的手段。

---

## 阶段划分

| 阶段 | 里程碑 | 内容 | 粗估 |
|---|---|---|---|
| **第一阶段：证明可复刻** | M0 – M2 | SQL 片段、schema、查询构建器 | 9 – 13 周 |
| **第二阶段：能跑起来** | M3 – M4 | 驱动、事务、类型映射 | 5 – 7 周 |
| **第三阶段：关系查询** | M5 | RQB（最重的一块之一） | 5 – 7 周 |
| **第四阶段：迁移与工具** | M6 – M7 | 迁移器、DDL diff、CLI | 10 – 15 周 |
| **第五阶段：多方言** | M8 – M9 | MySQL、SQLite | 5 – 7 周 |
| **持续** | M10 | 生态、文档、性能 | — |

合计 **34 – 49 人周**（约 8 – 12 个月，单人全职）。其中 M5 与 M7 是两个大头，各自可能翻倍。

---

## 里程碑详述

### M0 地基：SQL 片段层与渲染器

**状态**：已完成（2026-11-17）· **粗估**：2 – 3 周 · **依赖**：无

#### 目标

把 drizzle 的 `sql/sql.ts` + `sql/common-chunks.ts` + 三个 escape 函数移植成一个 Go 包。这是整个库的地基，后面每一层都建立在 chunk 模型上。

#### 交付物

| 包 / 类型 | 内容 |
|---|---|
| `entity/` | `Kind`（`entityKind` 的 Go 等价物）与 `String()`；类型身份交给编译器，`Kind` 只承载“分派结果” |
| `sql/chunk.go` | `Chunk` 接口（契约为 `GetSQL() *SQL`）、`Fragment` / `Name` / `Param` / `Placeholder` / `Alias` / `Subquery` / `ValueRows` / `List` / `Encoder`、渲染器与 `FillPlaceholders` |
| `sql/sql.go` | `SQL` 结构体（持有 `[]Chunk`）、`S(...)`、`Raw`、`Empty`、`Join`、`Identifier`、`NewParam`、`NewPlaceholder`、`NewSubquery`、`ListOf` |
| `sql/condition.go` | `And` / `Or` / `Not`（对应 rc.5 `sql/expressions/conditions.ts` 里不依赖列类型的三个组合子） |
| `sql/dialect/` | `Dialect` 接口：`EscapeName` / `EscapeParam` / `EscapeString` |
| `pg/dialect.go` | pg 的三个转义函数 + `SQLToQuery` / `SQLToQueryInline` |
| `internal/testutil` | golden fixture 加载与逐字节比对 |
| `sql/bench_test.go` | 渲染基准（bind / inline / nested 三组） |

> 列与表的 chunk（`Column` / `Table` / `View`）不在 M0：它们随 schema 一起在 M1 出现。
> 渲染器已经为它们留出分支位置，M1 只需插入 `case`，不改分派结构。

#### 关键设计决策

- **参数编号由渲染器持有，不是由 chunk 持有。** 对应 rc.5 的 `out` + `params` 累加结构：渲染过程中维护一个单调递增的计数器，每个 `Param` chunk 在渲染时取号。
- **`Fragment` 与 `Raw` 必须分开。** rc.5 的 `Chunk` 类型允许裸 `string`，但运行时把它当参数绑定处理 —— 这是类型在说谎。Go 侧不做这个妥协。
- **裸字符串按参数绑定。** Go 没有模板字符串，`S(...)` 无法区分字面量与插值；默认按参数绑定，字面量必须显式 `Raw("...")`。理由与取舍见 [ADR-0001](./docs/adr/0001-chunk-model.md)。
- **方言是接口，不是回调字段。** `Render(query, dialect, cfg)`：`RenderConfig` 只带 `ParamStart int` 与 `InlineParams bool`，三个转义函数由 `dialect.Dialect` 提供。
- **不移植 `tagged` 与 `invokeSource`。** 前者是 Bun.SQL 的缓存优化，后者是 mssql / drizzle-kit 的分派开关；等有真实需求再加。
- **nil 有两种含义。** `S(..., nil)` 是缺失（跳过），`NewParam(nil, nil)` 是绑定 NULL；`(*SQL)(nil)` 由 `isNilChunk` 统一当缺失处理。

#### 验收标准

1. `sql.S(sql.Raw("select "), sql.Identifier("id"), sql.Raw(" from "), sql.Identifier("users"), sql.Raw(" where "), sql.Identifier("id"), sql.Raw(" = "), sql.NewParam("x", nil))` 产出 `select "id" from "users" where "id" = $1` 且参数为 `["x"]`
2. 空片段与 nil / typed-nil 参数被跳过，不产生 `$` 空洞 —— 对齐 rc.5 `sql` 标签的 `if (strings[0])` / `if (param !== undefined)` 语义
3. 内联模式产出可直接粘进 psql 的字符串（字符串按 `escapeString` 转义、数字不转义、nil 为 `null`）
4. golden 覆盖：嵌套子查询、`ValueRows` 多行 values、别名；`Placeholder` 与 `And` / `Or` / `Not` 由单元测试覆盖（占位符的参数是对象，不适合逐字节 golden）
5. `sql/bench_test.go` 有三组渲染基准，记录每次渲染的分配次数

#### 测试与覆盖率

drizzle-orm 自身没有发布覆盖率阈值（`drizzle-orm/vitest.config.ts` 只开了 typecheck），
所以「覆盖率对齐」按两层执行：

1. **用例级对齐**：上游 `drizzle-orm/tests/sql-builder.test.ts` 的每个用例在
   `sql/condition_test.go` 里有同名对应（`TestSqlBuilder...`）；`is.test.ts` 的运行时类型判断
   在 Go 里退化为类型断言，由 `entity` 的值域测试覆盖。
2. **包级下限**（`go test ./... -cover`，数值为 2026-11-17 的实测）：

| 包 | 覆盖下限 | 实测 |
|---|---|---|
| `entity` | ≥ 95% | 100.0% |
| `sql` | ≥ 95% | 97.8% |
| `pg` | ≥ 95% | 100.0% |
| `internal/testutil` | ≥ 70%（测试基建，错误分支按需覆盖） | 74.3% |
| `sql/dialect` | 仅接口，无语句 | — |

未覆盖的部分主要是“无法在 Go 里构造的失败路径”（例如测试基建自身读文件失败）。
新增代码不允许把下限拉低；若确有必要，在本节说明原因。

#### 主要风险

- Go 的接口 + 类型断言分派可能比 TS 的构造器 `WeakMap` 慢。**接受**，见 README 的「不要拿性能当卖点」。实测（i5-13500H，20000 次）：bind 渲染 706 ns / 17 allocs，inline 512 ns / 13 allocs，嵌套子查询 312 ns / 8 allocs。分配次数是后续优化的真实指标，ns 只作参考。

---

### M1 Schema 定义与代码生成

**状态**：已完成（2026-11-20） · **粗估**：3 – 4 周 · **依赖**：M0

生成器用 `go/parser` 读结构体，不编译、不执行 schema 包。本包里的嵌入结构体会展开成列。跨包嵌入和泛型在生成阶段报错并指出行号。`Table()` 可读 `Name`、`PrimaryKey`、`Indexes`、`Checks`。列修饰写进生成的 `ColumnSpecs()`。`jsonb` 生成 `json.RawMessage`，`numeric` 生成 `string`。二次生成无 diff。删掉字段后若留下旧的 `schema_gen.go`，编译失败。

#### 目标

让用户用 Go struct + tag 描述 schema，`go generate` 产出类型安全的表与列描述符。**不引入运行时反射读 schema。**

#### 交付物

| 包 / 类型 | 内容 |
|---|---|
| `pg/types.go` | pg 类型常量与列修饰符：`BigSerial` / `Text` / `Timestamptz` / `Numeric` / `JsonB` / … |
| `codegen/parser.go` | 解析 `mist:"..."` tag |
| `codegen/validate.go` | tag 校验：非法类型名、重复列名、悬空 `references`、自相矛盾的修饰符 |
| `codegen/emit.go` | 生成 `schema_gen.go` |
| `codegen/templates/` | `text/template` 模板 |
| `cmd/mist/generate.go` | `mist generate` 子命令 |

#### tag 语法（草案）

```
`mist:"<column_name>,<type>,<modifier>,<modifier>..."`

修饰符：
  pk | notnull | unique | unique=<name>
  default=<literal> | defaultnow
  references=<table>.<column>[,ondelete=<action>][,onupdate=<action>]
  generated=<expr> | identity
  length=<n> | precis=<p,s>
```

#### 生成产物形态

```go
// Code generated by mist. DO NOT EDIT.

var UsersName = pg.Col[User, string]{
	Name: "name",
	Ptr:  func(u *User) any { return &u.Name },
}

func scanUser(row pg.Row, u *User) error {
	return row.Scan(&u.ID, &u.Name, &u.Email, &u.CreatedAt)
}
```

表级信息来自 `func (User) Table() pg.TableMeta`，不从类型名猜表名。见 [ADR-0004](./docs/adr/0004-schema-source.md)。

#### 关键设计决策

- **codegen 读语法树，不执行 schema 包。** `Table()` 必须是 `return pg.TableMeta{...}` 字面量。本包嵌入结构体在生成时展开。跨包嵌入和泛型直接报错。不在 `go generate` 里运行用户代码。
- **生成产物同包**，便于用户跳转与阅读；不做 `_gen` 子包（避免 import cycle）。
- **生成器必须幂等**：相同输入产出字节相同。CI 里加 `go generate && git diff --exit-code`。

#### 验收标准

1. README 里的 `User` / `Post` 两个 struct 能生成可编译的 `schema_gen.go`
2. 非法 tag（`bigintt`、重复列名、`references=nonexistent.id`）在 `go generate` 阶段报错并指出行号
3. 二次运行 `go generate` 无 diff
4. 删除一个字段后重新生成，残留引用会**编译失败**而不是静默失效
5. 生成的列引用能正确用于 M2 的表达式（`op.Eq(Users.Name, "x")` 类型检查通过）

#### 主要风险

- **零值与未设置**已由 [ADR-0003](./docs/adr/0003-zero-and-unset.md) 定稿：`Limit(0)` 就是 `LIMIT 0`；SQL NULL 只是可空列的 `*T`；写入列由列清单决定。
- tag 只表达列级约束。表名、复合主键、索引、CHECK 放在 `Table()`。见 [ADR-0004](./docs/adr/0004-schema-source.md)。

---

### M2 查询构建器（PostgreSQL）

**状态**：已完成（2026-11-20） · **粗估**：4 – 6 周 · **依赖**：M0、M1

`pg/fixture_test.go` 锁住至少 40 条 SQL。同一个 builder 被 100 个 goroutine 渲染，`-race` 下无竞争。没有 join 时 select 列表仍写 `"users"."id"`，drizzle rc.5 写 `"id"`。where、order by、join 的形状与 rc.5 一致。这是有意保留：列引用只有 `GetSQL` 一种形状。见差异登记。

已落地：`pg.Select`、`From` / `Where` / `OrderBy` / `Limit` / `Offset`，五种 join，`pg.Insert` / `pg.Update` / `pg.Delete`（列清单、多行 values、`on conflict do nothing`、`set`、`where`、`returning`），`op` 的比较、`and` / `or` / `not`、`asc` / `desc`、`inArray` / `notInArray`、`between` / `notBetween`、`like` / `ilike` 及否定、`isNull` / `isNotNull`、`exists` / `notExists`。空 `inArray` 是 `1 = 0`，空 `notInArray` 是 `1 = 1`。标量子查询、`op.In` / `op.NotIn` 的 `in (select)`、`exists` 里的 select，以及 `As(别名)` 派生表可渲染。`insert` 的 `values` 可以放标量子查询，也可以改成 `insert ... select`。`groupBy` / `having` 可渲染，后写的覆盖先写的。链是不可变节点，分叉共享前缀。`All` / `One` / `Exec` 已接到执行层。`op.Count` / `Sum` / `Avg` / `Min` / `Max` 及 `count` / `sum` / `avg` 的 `distinct` 可用于 `groupBy`、`having`、`orderBy`。聚合单值用 `pg.Value[T]` 读取，多列用 `pg.Scan` 和调用方写的扫描函数。`pg.Select` 的行类型仍然来自表。`on conflict do update` 可带冲突目标、`set`、目标 `where` 和更新 `where`。集合运算 `union` / `union all` / `intersect` / `intersect all` / `except` / `except all` 可渲染。`select` 可以带 `with` / `with recursive`。`update` 可以 `from`，再接 inner / left / right / full join。`insert` / `update` / `delete` 也可以 `with` / `with recursive`。`delete` 可以 `using`。`select` 可以 `distinct` / `distinct on` / `for update`。算术是 `add` / `sub` / `mul` / `div`。Go 里函数和类型不能同名，所以类型留在 `query`，入口函数放在 `pg`。被连接的表是 `sql.Chunk`：方法不能再声明类型参数，行类型仍由 `From` 决定。

#### 目标

`select` / `insert` / `update` / `delete`，以及表达式系统。这是「SQL 语感」的兑现处。

#### 交付物

| 包 / 类型 | 内容 |
|---|---|
| `query/select.go` | `Select` builder：`From` / `Where` / `OrderBy` / `Limit` / `Offset` / `GroupBy` / `Having` / `InnerJoin` / `LeftJoin` / `RightJoin` / `FullJoin` / `CrossJoin` |
| `query/insert.go` | `Insert`：`Values` / `OnConflictDoNothing` / `OnConflictDoUpdate` / `Returning` |
| `query/update.go` | `Update`：`Set` / `Where` / `Returning` / `From` |
| `query/delete.go` | `Delete`：`Where` / `Returning` |
| `query/setops.go` | `Union` / `UnionAll` / `Intersect` / `Except` |
| `query/cte.go` | `With` / `WithRecursive` |
| `op/` | 调用点上的 `Eq` / `And` / `Desc` 等，语法对应 drizzle 的具名导入。实现委托给 `sql` 的 chunk |
| `sql/agg.go` | `Count` / `Sum` / `Avg` / `Min` / `Max` + `Distinct` |
| `pg/dialect_query.go` 等 | `BuildSelectQuery` / `BuildInsertQuery` / `BuildUpdateQuery` / `BuildDeleteQuery`（`pg/` 下的方言实现，`query/` 只负责拼装） |

#### 关键设计决策

- **构建器是不可变节点链表**，分叉共享前缀。见 [ADR-0002](./docs/adr/0002-query-syntax-and-scan.md)。
- **M2 的构建器能渲染出 `(SQL, error)`，不连接数据库。** 执行终端 `All(ctx)` / `One(ctx)` 的类型在 M2 定下来，真正查询在 M3。
- **`Where` 接受一个条件**，与 drizzle 一致。多个条件用 `op.And` / `op.Or`。

#### 验收标准

1. README 里的 select 例子与 drizzle rc.5 的差别只有 select 列表的表名限定，已登记
2. fixture 矩阵（至少 40 条）覆盖：多 join、子查询、exists、in (subquery)、group by + having、聚合 + distinct、union、CTE、on conflict do update
3. `insert` 批量 values 使用**单个** `insert ... values (...), (...)`，不是循环单条
4. `update ... set x = <subquery>` 与 `set x = sql.Raw(...)` 都支持
5. builder 可安全并发使用（同一个 builder 被两个 goroutine 渲染不产生数据竞争），有 `-race` 测试

#### 主要风险

- select 是 drizzle 里最大的文件之一（`pg-core/query-builders/select.ts` 1395 行）。**不要试图一次覆盖全部组合**，按 fixture 矩阵增量推进。
- 行类型由 `Table[T]` 带进 `Select[T]`，`All` 返回 `[]T`。见 [ADR-0002](./docs/adr/0002-query-syntax-and-scan.md)。

---

### M3 执行层：驱动、会话、事务

**状态**：已完成（2026-11-20） · **粗估**：2 – 3 周 · **依赖**：M2

`docker-compose.yml` 起 PostgreSQL。集成测试覆盖 CRUD、错误和 panic 回滚、savepoint、ctx 取消。连接池用 pgxpool 和 `database/sql`，参数写在各自的配置上。100 个 goroutine 并发读写有 `-race` 测试。整行 `scanUser` 的 allocs/op 与手写 `rows.Scan(&u.ID, ...)` 相同。

已落地：`pg.Open` / `OpenConfig`（pgxpool）、`std.Open`（`database/sql`）、`DB.Tx`、嵌套 `Tx.Tx`（savepoint）、`All` / `One` / `Exec`、`Prepare`。整行走生成的 `Scan`，子集按列上的 `FieldPtr` 填字段。错误或 panic 回滚事务。`Prepare` 固定 SQL 文本和扫描计划，执行时用 `sql.FillPlaceholders` 换参数。pgx 池不占连接，服务端语句由 pgx 按文本缓存在每条连接上；pgx 事务会 `PREPARE`；`database/sql` 使用 `*sql.Stmt`。Go 的方法不能声明类型参数，因此没有 `db.Select`，调用是 `pg.Select(...).All(ctx, db)`。`std.DB` 实现同一个 `query.Executor`。

#### 目标

让构建出来的 SQL 真的能跑。

#### 交付物

| 包 / 类型 | 内容 |
|---|---|
| `session/session.go` | 方言无关的 `Session` 接口与执行流程：`Query` / `Exec` / `PreparedQuery` |
| `session/tx.go` | `Tx(ctx, func(tx *Tx) error) error`、savepoint 嵌套 |
| `pg/scan.go` | 驱动 `Row` 接口。扫描函数由 codegen 生成，本包不反射 |
| `pg/db.go` | `DB` 门面：`Select()` / `Insert()` / `Update()` / `Delete()` / `Query` |
| `pgx/` | pgx v5 驱动的 `Session` 实现 |
| `std/` | `database/sql` 的 `Session` 实现 |

#### 关键设计决策

- **只有一套实现，没有 `async/` 与 `effect/` 的重复。** 这是 Go 相对 rc.5 最直接的减法：`context.Context` 就是那套东西。
- **`All(ctx)` 返回 `[]T`，`T` 来自 `From` 的表。** 整行走生成的 `scanUser`，子集走列上的 `Ptr`。没有 `Scan(dest any)`。见 [ADR-0002](./docs/adr/0002-query-syntax-and-scan.md)。

#### 验收标准

1. `docker compose up` 起一个 pg，集成测试覆盖 CRUD
2. 事务内 error → 回滚；panic → 回滚（defer recover）
3. 嵌套事务用 savepoint，内层回滚不影响外层
4. `context` 取消能中断查询，连接归还池
5. 连接池参数可配，且不重复造池
6. `-race` 下并发 100 goroutine 读写无竞争

#### 主要风险

- 结果扫描的分配。整行 `All` 的 allocs/op 必须与手写 `rows.Scan(&u.ID, ...)` 相同，否则 M3 不算完成。见 [ADR-0002](./docs/adr/0002-query-syntax-and-scan.md)。

---

### M4 Codecs 类型映射

**状态**：已完成（2026-11-20） · **粗估**：3 – 4 周 · **依赖**：M0、M3

`pg/codecs` 提供六槽位 `Codec`。nil 编码器是直通，整数和 `text` 的生成列不挂 codec，整行扫描的分配次数不变。`numeric` 参数是 `$1::numeric`，数组是 `$1::text[]` / `$1::int4[][]`。没登记的类型名调用 `Bind` 会报「没有注册 codec」。几何、范围、向量这些类型不在这批里。

#### 目标

把 pg 类型与 Go 类型之间的转换收敛成一个六槽位接口，让 `numeric`、`jsonb`、`interval`、`bytea`、数组这些「不好直接扫」的类型能正确往返。

#### 交付物

| 包 / 类型 | 内容 |
|---|---|
| `pg/codecs/codec.go` | `Codec` 接口：`Cast` / `CastInJson` / `CastParam` / `Normalize` / `NormalizeInJson` / `NormalizeParam` |
| `pg/codecs/registry.go` | `CodecCollection`：按 pg oid / 类型名解析 |
| `pg/codecs/numeric.go` | `numeric` ↔ `decimal` / `string` |
| `pg/codecs/json.go` | `json` / `jsonb` ↔ `struct` / `map` / `json.RawMessage` |
| `pg/codecs/interval.go` | `interval` ↔ `time.Duration` / 自定义 |
| `pg/codecs/bytes.go` | `bytea` ↔ `[]byte`（含 hex 转义） |
| `pg/codecs/array.go` | 一维 / 多维数组的类型与 cast |
| `pg/codecs/time.go` | `timestamptz` / `date` / `time` |
| `pg/codecs/ints.go` | 各整数类型的范围检查 |
| `pg/codecs/register.go` | 用户自定义 codec 的注册入口 |

#### 关键设计决策

- **`CastParam` 负责给参数加类型后缀**（pg 的 `$1::numeric`）。这不是可选的优化 —— 没有它，`numeric` 参数会被当成 `text`。
- **noop 快速路径**：Go 侧用「Codec 为 nil」表示直通，而不是 rc.5 的 `isNoop` 哨兵字段。
- **数组维度是列属性，不是 codec 属性。** 与 rc.5 一致：`castParam(name, column, dimensions)`。

#### 验收标准

1. 以下类型往返一致（写入 → 读出 → 深比较）：`numeric(20,6)`、`jsonb`（嵌套数组）、`interval`、`bytea`、`text[]`、`int4[][]`、`timestamptz`（含时区）、`uuid`、`enum`
2. `null` 能区分「db null」与「Go 零值」
3. 数组元素的 cast 在参数侧正确（`$1::text[]`）
4. 用户能注册一个自定义 codec 并生效，有示例
5. 类型不匹配时报错信息指出列名与期望类型

#### 主要风险

- pg 类型全集很大（rc.5 的 `pg-core/codecs.ts` 1187 行）。**不追求一次覆盖全集**，先覆盖上表，其余按需增量。未支持的类型给出明确的「未注册 codec」错误，不静默走字符串。

---

### M5 关系查询（RQB）

**状态**：已完成（2026-11-20） · **粗估**：5 – 7 周（可能翻倍） · **依赖**：M2、M3、M4

`one` / `many` 的 SQL 与 rc.5 的 `collectRelationalQuery` 对齐，两种 strategy 都在。[ADR-0005](./docs/adr/0005-relational-query.md) 写了包裹条件和 JSON 形状。10k 父行 × 10 子行是一条查询；会话把 `log_statement` 设成 `all`，条数用 pgx 的 `QueryTracer` 数，测试进程读不到数据库日志文件。

`through`、`comment`、`extras` 和嵌套 JSON cast 已补上。`withCache` 不改 SQL；`WithCache` 为 true 时要传入 `Cache`。`MemoryCache` 只按 SQL 和参数存结果，表变更后不会自动失效。嵌套字段要在 schema 上写 `New`、`Ptr`、`Init`、`Place`，没有生成「行类型加上关系」的类型。

#### 目标

把「一次查询拉出嵌套结构」这件事做成 drizzle 的样子：**配置是数据，不是回调**。

#### 交付物

| 包 / 类型 | 内容 |
|---|---|
| `relation/schema.go` | `Define` / `DefinePart`，对应 rc.5 `defineRelations` / `defineRelationsPart` |
| `relation/config.go` | `Config`：`With` / `Columns` / `Where` / `Order` / `Extras` / `Limit` / `Offset` / `Strategy` / `Comment` / `WithCache` |
| `relation/filter.go` | `And` / `Or` / `Not` / `RawSQL`，以及列条件、`Exists` |
| `relation/order.go` | `Asc` / `Desc` / `CountOrder` / `Aggregate` |
| `relation/errors.go` | 未知关系、未知列、没有选出列 |
| `pg/dialect_rqb.go` | `BuildRelationalQuery`，对应 `collectRelationalQuery` |
| `relation/mapper.go` | 根行按字段地址扫描，嵌套 JSON 写入这些地址 |
| `relation/strategy.go` | `basic` / `optimized` 的包裹判断 |

#### 关键设计决策

- **rc.5 的 `where` 是纯数据对象**，这让 Go 的还原度显著提高（相对 0.x 的回调形式）。Go 侧需要构造函数来表达：`q.Eq(..., ...)`、`q.And(...)`、`q.Raw(...)`。
- **`Strategy` 的语义以 rc.5 `collectRelationalQuery` 为准，M5 开工前必须先出 ADR-0005**：两种 strategy 各自生成什么 SQL、默认值、失败模式。**这条没定清楚不要动手。**
- **嵌套映射不反射。** 叶子行走生成的扫描函数。关系查询不得改 `Select[T]` 与 `Chunk`。见 [ADR-0002](./docs/adr/0002-query-syntax-and-scan.md)。

#### 验收标准

1. 三层嵌套（users → posts → comments）的 `findMany` 生成的 SQL 与 drizzle rc.5 一致
2. `Columns` 只选部分列时，SQL 的 projection 精确匹配
3. `where` 里同时出现列条件与关系条件（如「有评论的帖子」）时生成 `exists` 子查询
4. `orderBy` 支持跨关系聚合排序（`desc(count)`）
5. `limit` / `offset` 在嵌套关系内生效（lateral join 或子查询，取决于 strategy）
6. 结果正确映射到嵌套 struct，`many` 关系是 slice 且顺序稳定（按 `orderBy`）
7. 有一条明确的错误路径：请求一个 `With` 里未声明的表 → 明确报错，不是空结果
8. 大结果集（10k 父行 × 10 子行）不产生 N+1 查询（用 pg 的 `log_statement` 断言）

#### 主要风险

- **这是最可能失控的里程碑。** rc.5 里 `relations.ts` 3014 行、`collectRelationalQuery` 数百行。缓解：先只支持 `basic`，`optimized` 后置；先只支持 `one` / `many`，`through` 后置。
- 嵌套组装的分配量。叶子扫描已经是生成函数；组装层如果按行额外分配，要在基准里单独可见。

---

### M6 迁移器

**状态**：已完成（2026-11-20） · **粗估**：2 – 3 周 · **依赖**：M3

`migrate.Run` 读取子目录里的 `migration.sql`。一次调用里的语句放在同一个事务中，失败则整批回滚。事务开始后先拿 `pg_advisory_xact_lock`，再读已应用的 `name`。空语句会被丢掉，sha256 仍按文件原文计算。目录名前 14 位必须是合法的 UTC 时间。CLI 的 `mist migrate` 留在 M7。

#### 目标

把 rc.5 的迁移语义完整搬过来 —— 包括它那些容易做错的地方。

#### 交付物

| 包 / 类型 | 内容 |
|---|---|
| `migrate/read.go` | 读迁移目录，按目录名排序，`--> statement-breakpoint` 切分，计算 sha256 |
| `migrate/table.go` | v1 表结构 DDL |
| `migrate/migrate.go` | 主流程 |
| `migrate/lock.go` | 并发保护（pg advisory lock） |
| `migrate/errors.go` | hash 不匹配、未应用迁移被修改等错误 |

#### 必须对齐的 rc.5 语义

- **迁移表**：`id SERIAL PRIMARY KEY, hash text NOT NULL, created_at bigint, name text, applied_at timestamptz DEFAULT now()`
- **幂等判定：`name` 集合成员关系。** 不是 0.45 的 `created_at` 水位线。这是一个容易搞错且后果严重的点。
- **不实现 `upToV1`。** 新项目直接建 v1 表；检测到旧表结构时给出明确错误与迁移指引，不静默处理。
- **hash 校验**：已应用的迁移内容变了 → 报错。不静默跳过。

#### 验收标准

1. 空库 migrate → 全部应用；再 migrate 一次 → 零操作
2. 两个进程同时 migrate → 只应用一次，另一个等待
3. 修改一个已应用迁移文件 → 明确报错，指出文件名
4. `--> statement-breakpoint` 正确切分，且单条语句失败时整体回滚（在事务型方言上）
5. 迁移目录里存在但未应用的中间迁移 → 按序补齐，不跳过
6. 有一个「20 个迁移文件的真实目录」的集成测试

#### 主要风险

- 并发保护在事务型方言简单（advisory lock），在非事务型方言（如某些 serverless driver）需要别的方案。**v1 只做事务型方言**，其余推迟。

---

### M7 DDL diff 与 CLI

**状态**：已完成（2026-11-23） · **粗估**：8 – 12 周（最重） · **依赖**：M1、M6

`mist ddl -migrations -write` 对照 `meta/snapshot.json` 写下一次迁移。`mist migrate` 应用目录。`mist check` 对照快照和每个 `migration.sql` 的 sha256，不重放 SQL。`mist push` 对照数据库打印 SQL，默认不执行；终端确认或 `--force` 才执行。删表、删列、改类型标成破坏性。20 张表、enum、复合主键、外键、索引和默认值的迁移，应用到空库再反读，diff 是空的。`mist generate` 仍只写 Go 描述符。边界见 [ADR-0008](./docs/adr/0008-ddl-model.md)。

#### 目标

drizzle-kit 的等价物：从 schema 生成迁移，或直接推到数据库。

#### 交付物

| 包 / 类型 | 内容 |
|---|---|
| `introspect/pg.go` | 从 `information_schema` + `pg_catalog` 反读实际结构 |
| `schema/model.go` | 方言无关的 schema 中间表示（表、列、索引、约束、enum、序列） |
| `diff/diff.go` | 期望 vs 实际 → 变更集 |
| `ddl/pg/emit.go` | 变更集 → SQL 语句 |
| `cmd/mist/generate.go` | `generate` 子命令 |
| `cmd/mist/migrate.go` | `migrate` 子命令 |
| `cmd/mist/push.go` | `push` 子命令 |
| `cmd/mist/check.go` | `check` 子命令 |

#### 关键设计决策

- **`push` 有破坏性。** 删除列 / 删除表必须在交互式确认下进行；非交互环境需要显式 `--force`。默认输出 SQL 而不执行。
- **改名检测不做自动推断。** 看到「旧列没了、新列来了」时**不猜**这是 rename，而是生成 drop + add，并打印提示要求用户手工改成 rename。理由：猜错的代价是丢数据。
- **中间表示是方言无关的**，`ddl/pg` 只是渲染器。这样 M8 加 mysql 时不用重写 diff。

#### 验收标准

1. 对一个中等复杂度 schema（20 表、enum、复合主键、外键、索引、默认值）：
   - `generate` 产出的迁移能在空库上应用
   - 应用后 `introspect` 再 `diff` → **空变更集**
2. 破坏性变更（drop column / drop table / 改类型）单独归类，`push` 时确认
3. `check` 能发现「迁移文件与 schema 不一致」
4. 有一个「改三次 schema、生成三个迁移、依次应用」的端到端测试
5. diff 的每个变更项都能追溯回 schema 里的具体位置

#### 主要风险

- **这是整个项目最可能失控的一块。** drizzle-kit 是独立的大工程，且历史上 bug 最多的部分就是 diff。缓解：
  - 先只支持 pg
  - 先只做「生成」不做「交互式修复」
  - 明确不支持：视图、触发器、函数、分区、权限、扩展
  - introspection 只读 `information_schema` 能拿到的，深度 `pg_catalog` 查询按需加

---

### M8 第二方言：MySQL

**状态**：已完成（2026-11-23） · **粗估**：3 – 4 周 · **依赖**：M2（M6/M7 可选，这一刀没有搬）

已落地的是 `mysql.Dialect` 和 `mysql.Open`。M2 的查询矩阵在 `mysql/fixture_test.go`：select / insert / update / delete、`with`、`union` / `intersect` / `except`、`for update of` 都能渲染。只写 `offset` 时补 `limit 18446744073709551615`，因为 MySQL 不允许单独的 `offset`。多表 update / delete 写成逗号来源，不沿用 PostgreSQL 的 `from` / `using` 位置。同一张表出现两次时，MySQL 会拒绝执行。`full join`、`distinct on`、`returning`、`on conflict` 直接报错。`op.ILike` 写成 `lower(列) like lower(参数)`，PostgreSQL 仍是 `ilike`。`mysql.FindMany` 用 `lateral` 和 `json_object` / `json_arrayagg`，包裹条件也和 PostgreSQL 不同，见 [ADR-0011](./docs/adr/0011-mysql-relational-query.md)。`on duplicate key update` 用 `OnDuplicateKeyUpdate` 和 `mysql.NewRow`，见 [ADR-0010](./docs/adr/0010-duplicate-key.md)。`insert ignore` 用 `Insert.Ignore`，见 [ADR-0012](./docs/adr/0012-insert-ignore.md)。单表 `update` / `delete` 可以写 `order by` 和 `limit`，多表不行，见 [ADR-0013](./docs/adr/0013-write-order-limit.md)。`insert select` 套一层子查询，让 `mysql.NewRow` 仍指向拟写入的行。执行走 `database/sql` 和 `go-sql-driver/mysql`。`go test -tags integration ./mysql/` 已在 Kiri 的 MySQL 8.4（库 `mist`，`127.0.0.1:3306`）上通过，覆盖 join、`with`、集合运算、`offset`、`for update of`、`on duplicate key update`（含 `insert select`），以及插入、查询、更新、删除。`mysql/codecs` 覆盖 `decimal`、`json`、`datetime`、`blob`，占位符保持 `?`。没有数组，布尔仍是 `tinyint(1)`，扫描不收成 `bool`。`Table()` 返回 `mysql.TableMeta` 时，生成器写出 `mysql.Col`。`internal/mysqlfixture` 的生成结果已在 MySQL 8.4 上往返。`timestamptz`、`jsonb`、`boolean`、数组会报错。`sql/` 没有为 MySQL 加分支。`query.Insert` 增加了 `OnDuplicateKeyUpdate`，见 ADR-0010。

#### 目标

**这不是「多支持一个数据库」，而是对抽象的一次检验。** 如果加 mysql 需要改动 `sql/` 或 `query/` 的核心类型，说明 M0/M2 的抽象漏了。

#### 交付物

- `mysql/`：类型、dialect、codecs（至少基础类型）
- 不新增 `mysql/query/`：builder 复用 `query/`，方言差异只落在 `mysql/dialect*.go` 与 `mysql/codecs/`。如果复用不了，说明 M2 的抽象有问题，应先修抽象
- `mysqlx/` 或 `go-sql-driver/mysql` 驱动适配

#### 必须处理的方言差异

| 维度 | pg | mysql |
|---|---|---|
| 参数占位 | `$1` | `?` |
| 标识符转义 | `"x"` | `` `x` `` |
| returning | 支持 | 仅在部分版本/mariadb |
| on conflict | `on conflict ... do update` | `on duplicate key update` |
| schema | 有 | 无（database 概念） |
| 类型 | `jsonb` / `text[]` / `uuid` / `timestamptz` | `json` / 无数组 / `char(36)` / `datetime` |

#### 验收标准

1. M2 的 fixture 矩阵在 mysql 上跑通（不可用的语法在 fixture 里标注方言限制）
2. `sql/` 与 `query/` 的核心类型**没有**为 mysql 增加分支（只允许接口新增方法由各方言实现）
3. 如果确实需要改核心类型，改动被记录为「M0/M2 抽象缺陷」，并在 ADR 里说明

#### 主要风险

- mysql 的 `on duplicate key update` 语义与 `on conflict` 不等价（影响 `returning`）。**如实标注能力差异**，不假装统一。

#### 完成时的边界

- `sql/` 没有 MySQL 分支。`query` 上为 MySQL 专有子句加的字段见 ADR-0010、ADR-0012、ADR-0013。`for share` 两边相同。关系查询的形状见 ADR-0011。
- `insert values` 不能带 `with`。`insert select` 把 `with` 放在列清单和 `select` 之间，MySQL 不接受写在 `insert` 前面。
- 同一张表在一条多表语句里出现两次时，MySQL 拒绝执行。
- DDL、introspect、migrate、`push` 仍只做 PostgreSQL。
- 没有数组、`uuid`、`timestamptz`、`jsonb`。布尔是 `tinyint(1)`，扫描不收成 `bool`。

---

### M9 SQLite

**状态**：已完成（2026-11-23） · **粗估**：2 – 3 周 · **依赖**：M8

默认驱动是 [glebarez/go-sqlite](https://github.com/glebarez/go-sqlite) v1.23.0，引擎是 `modernc.org/sqlite`。SQLite 编进二进制，`CGO_ENABLED=0` 就能打开库，不加载系统 `libsqlite3`。嵌入的版本是 3.53.3。

#### 完成时的边界

- 双引号标识符、`?` 占位符。select / insert / update / delete、`with`、集合运算、`full join`、`on conflict`、`returning`、`insert or ignore` 已执行。集合运算两侧不加括号，这版 SQLite 拒绝语句以 `(` 开头。
- `ilike` 收成 `lower(左) like lower(右)`。`distinct on`、`for update` / `for share`、`on duplicate key update`、`delete ... using`、`update` / `delete` 的 `order by` / `limit` 直接报错。
- `sqlite.FindMany` 跑通 `one` / `many` / `through`、`extras`、`count` 排序和 `exists`。没有 `lateral`，形状见 [ADR-0014](./docs/adr/0014-sqlite-relational-query.md)。`numeric` 不保留小数末尾的 0。
- 改列由 `ddl/sqlite.Render` 建成新表、拷数据、删旧表、改名，并已在库上执行。就地加唯一约束、CHECK、外键会报错，除非同一张表因为改列或改主键而重建。没有 enum 和 identity。
- 迁移目录、`check`、`push`、introspect 仍只做 PostgreSQL。`sql/` 没有 SQLite 分支。

#### 目标

第三个方言，验证「方言扩展是天级工作」这个判断是否成立。

#### 特殊之处

- 类型系统弱（动态类型 + 类型亲和性）
- 无原生 `boolean` / `timestamptz`
- 3.39 之前没有 `right join` / `full join`。嵌入的 3.53.3 有，`full join` 已执行
- `returning` 需要 3.35 及以上。嵌入的版本有，已执行
- DDL 极为受限（改列类型要重建表）

#### 验收标准

1. 基础 CRUD + 关系查询在 sqlite 上跑通
2. **ddl 生成器能处理「改列」这种情况**（sqlite 需要 create new + copy + drop + rename），或者在 `check` 阶段明确拒绝并说明原因
3. 从 M0 到 M9，`sql/` 核心类型零改动（或改动被记录）

---

### M10 生态与打磨

**状态**：进行中 · **持续** · **依赖**：M0 起持续

| 项 | 内容 | 状态 |
|---|---|---|
| Golden 测试工具 | `testutil.Run` 对拍一个目录里的 JSON。加 `Case` 后 `go test -update-golden` 写出 fixture | 已完成 |
| testcontainers | 集成测试在没有 DSN 时用 testcontainers 起 PostgreSQL 16 和 MySQL 8.4。SQLite 是临时文件，驱动嵌在进程里。GitHub Actions 走这条路径 | 已完成 |
| Benchmark | `bench/` 对比 mist、sqlx、gorm、ent、bun 的查询构建、结果映射、并发执行。Actions 把 `bench.txt` 存成 artifact | 已完成 |
| 示例项目 | REST 服务 + CLI 两个完整示例 | 未开工 |
| 文档站 | pkg.go.dev 之外的教程、迁移指南（从 gorm / sqlc 过来） | 未开工 |
| 发布流程 | tag、changelog、语义化版本 | 未开工 |
| 错误信息 | 所有用户可见错误都必须包含「是哪张表、哪一列、期望什么」 | 未开工 |

---

## 依赖关系

```
M0 ──┬──> M1 ──┬──> M2 ──┬──> M3 ──┬──> M5
     │        │        │        │
     │        │        │        └──> M6 ──> M7
     │        │        │
     │        │        └──> M4 ──┘（M5 需要 M4）
     │        │
     │        └──> M7（DDL 需要 schema 中间表示）
     │
     └──> M2

M2 ──> M8 ──> M9
```

可并行的部分：

- M4 与 M6 可以并行（M6 只需要 M3 的 session）
- M8 / M9 可以在 M7 之前插队（如果多方言比 DDL 更重要）
- M10 的 benchmark 从 M2 就可以开始

---

## 横切工作

这些不属于任何单个里程碑，但要贯穿始终。

| 项 | 何时 | 内容 |
|---|---|---|
| CI | M0 | `go build` / `go vet` / `golangci-lint` / `go test -race` / golden 对拍 |
| Golden 对拍基建 | M0 | 见 [状态定义](#状态定义与完成标准) |
| 错误信息规范 | M1 | 错误必须可定位：包、操作、表、列 |
| 生成代码幂等检查 | M1 | CI 里 `go generate && git diff --exit-code` |
| API 变更检测 | M2 起 | 用 `apidiff` 或类似工具，破坏性变更必须进 changelog |
| 文档同步 | 全程 | 每个里程碑完成时更新 README / ROADMAP 状态列 |

---

## ADR 清单

架构决策记录放在 `docs/adr/`。每个决策一个文件，格式：背景 / 决策 / 后果 / 备选方案。

| 编号 | 决策 | 触发时机 | 状态 |
|---|---|---|---|
| ADR-0001 | chunk 模型：`GetSQL` 接口，裸字符串按参数绑定 | M0 开工前 | 已接受 |
| ADR-0002 | 查询链贴近 drizzle；`Select[T]`；生成函数扫描，无反射 | M2 开工前 | 已接受 |
| ADR-0003 | 子句看调用，NULL 用 `*T`，写入看列清单 | M1 内 | 已接受 |
| ADR-0004 | 结构体 + tag 是列的真相源，表名在 `Table()` | M1 开工前 | 已接受 |
| ADR-0005 | RQB 的 `basic` 与 `optimized` 两种 strategy 的确切语义 | M5 开工前 | **必须定稿** |
| ADR-0006 | 结果行映射 | M3 | 不另写，见 ADR-0002 |
| ADR-0007 | 迁移并发保护在非事务型方言上的方案 | M6 内 | 待定（v1 可不解决） |
| ADR-0008 | DDL diff 的中间表示边界：支持哪些对象 | M7 开工前 | 已接受 |
| ADR-0009 | 是否提供 `mist` CLI 之外的程序化 API | M7 内 | 已接受 |
| ADR-0010 | `on duplicate key update` 不并进 `on conflict` | M8 内 | 已接受 |
| ADR-0011 | MySQL 关系查询不用 PostgreSQL 的 JSON 形状 | M8 内 | 已接受 |
| ADR-0012 | `insert ignore` 不并进 `on conflict do nothing` | M8 内 | 已接受 |
| ADR-0013 | `update` / `delete` 的 `order by` 和 `limit` 只在 MySQL 单表上写 | M8 内 | 已接受 |
| ADR-0014 | SQLite 关系查询不用 `lateral` | M9 内 | 已接受 |

---

## 与 drizzle-orm 的差异登记

**主动偏离**（有意为之，不是还没做）：

| 项 | drizzle-orm rc.5 | mist | 理由 |
|---|---|---|---|
| `Chunk` 的类型成员 | 允许裸 `string`，运行时按参数绑定 | 不允许，必须显式 `Fragment` / `Raw` | TS 的类型在说谎，Go 不复制这个缺陷 |
| `sync` / `async` / `effect` 三套实现 | `async/db.ts` 与 `effect/db.ts` 各 823 行 | 一套 `context.Context` API | Go 不需要 effect 系统 |
| 迁移表升级 `upToV1` | 约 300 行 | 不实现 | 新项目直接建 v1 表 |
| `Column.primary` vs `config.primaryKey` | 两个名字并存 | 统一为 `Primary` | 兼容期的产物 |
| `ValueRows.getSQL()` | legacy shim | 保留 | `Chunk` 的契约就是 `GetSQL`；且 M2 之前构建器可按普通片段处理多行 values |
| 裸字符串语义 | 模板里的字面量，插值绑定 | **插值也绑定**；字面量必须 `Raw(...)` | Go 没有模板字符串，安全默认值优先，见 [ADR-0001](./docs/adr/0001-chunk-model.md) |
| `tagged` 模式 | `_sql` 片段数组，供 Bun.SQL 缓存 | 不移植 | Go 无对应消费方 |
| `invokeSource` | mssql / drizzle-kit 的分派开关 | 不移植 | M0 只有 pg，有需要时再加 |
| `sql.fromList` | 标注 deprecated | 不引入 | 用 `sql.Join` 替代 |
| `Where` 参数 | 单个 `SQL \| undefined` | 单个条件，用 `op.And` / `op.Or` 组合 | 与 drizzle 的调用形状一致，见 [ADR-0002](./docs/adr/0002-query-syntax-and-scan.md) |
| 结果类型推导 | 类型级解释器 `BuildQueryResult` | `Table[T]` 把 `T` 带进 `Select[T]`，`All` 返回 `[]T` | Go 无类型级计算，见 [ADR-0002](./docs/adr/0002-query-syntax-and-scan.md) |
| 行映射 | `new Function` 运行时生成，或反射 | 只有生成的扫描函数 | 反射会把库做成 bun / gorm，见 [ADR-0002](./docs/adr/0002-query-syntax-and-scan.md) |
| `db.query.users` | `Proxy` 动态属性 | 生成包里的具体字段 | Go 无 Proxy |
| `chunkKinds` 数字分派 | 原型 `Symbol` 缓存 + `WeakMap` | 接口 + 类型断言 | JS 特有的分派优化，Go 的接口断言已足够快 |
| `commonStringChunks` 驻留 | 约 70 个单例片段 | 不驻留（字符串常量足够） | Go 的字符串常量不进堆分配 |
| `inArray` 的参数 | 列表或一条 SQL | 列表是 `op.InArray`，子查询是 `op.In` / `op.NotIn` | Go 不能重载 |
| 无 join 时的 select 列表 | `"id", "name"` | `"users"."id", "users"."name"` | 列的 `GetSQL` 只有带表名一种形状。有 join 时两边都带表名 |
| 关系查询入口 | `db.query.users.findMany({...})` | `pg.FindMany[T](ctx, db, schema, "users", cfg)` | 没有 Proxy。嵌套字段由 schema 上的 `Place` 写入调用方的结构体，不生成交叉类型 |
| 迁移 hash | 写入 sha256，`getMigrationsToRun` 只按 `name` 跳过 | 已应用文件的 sha256 变了就返回 `HashError`，带目录名 | 验收要求指出文件名 |
| 迁移并发 | pg 的 `migrate` 没有锁 | 事务内 `pg_advisory_xact_lock`，拿到锁后再读 `name` | 两个连接同时跑只应用一次 |

**未完成**（还没做，不是不做）：

见 [明确推迟](#明确推迟)。

---

## Go 还原度评估

基于 rc.5 逐层阅读后的判断。**「设计还原度」= 能不能表达同样的语义；「语法还原度」= 写出来的代码像不像。**

| 层 | 设计还原度 | 语法还原度 | 说明 |
|---|---|---|---|
| SQL 片段层（chunk 模型 / 渲染器） | 100% | 55% | 语义完全可表达；`` sql`...` `` 模板标签无法复制，改为 `sql.S(...)` |
| Schema 定义 | 100% | 85% | struct tag 与 drizzle 的 builder 表达力接近，缺类型级推导 |
| 查询 DSL（builder） | 100% | 70% | 链式调用天然适合 Go；泛型方法受限 |
| 关系查询（RQB） | 100% | **90%** | rc.5 把 `where` 数据化之后，这一层反而成了还原度最高的 |
| 结果类型推导 | — | **0%** | `BuildQueryResult` 是类型级解释器，Go 完全不可表达，只能力能由 codegen 补 |
| 迁移器 | 100% | 95% | 逻辑是纯 IO + SQL，几乎 1:1 |
| Codecs | 100% | 90% | 六槽位接口可表达；noop 哨兵改为 nil |
| 运行时机制（分派 / 映射 / JIT） | 90% | 80% | 语义可表达，实现手段完全不同 |

**一句话结论：语义 95% 可复刻，语法 70% 可复刻，类型推导 0% 可复刻 —— 而类型推导这一块恰恰要靠 codegen 补回来。**

---

## 抄 / 丢 / 换 / 补：逐层处置表

| 处置 | 对象 |
|---|---|
| **该抄** | chunk 模型；三个 escape 函数 + `Build*Query` 的方言接口；codecs 六槽位；数据化的 `where` / `orderBy` / `columns`；迁移的 `name` 集合幂等；`--> statement-breakpoint` 切分；v1 迁移表结构；`between` / `inArray` 等表达式生成的 SQL 形状 |
| **该丢** | `entityKind` 字符串标签（改用接口断言）；`StringChunk` 驻留；`chunkKinds` 数字分派 + `WeakMap`；`new Function` JIT mapper；`tagged` 渲染模式与 `_sql` 缓存字段；`async/` + `effect/` 双份实现；`upToV1`；一切 `@deprecated` 的兼容层 |
| **该换** | 类型级推导 → codegen；回调式 `where` → 数据对象 + 构造函数；`Proxy` 动态属性 → 生成的字段；运行时 JIT 映射 → 生成期 mapper；模板字面量类型（`asc(count)`）→ 常量 + 校验函数 |
| **该补** | DDL diff（rc.5 里完全不在这半边，属于 drizzle-kit）；introspection；CLI；`context.Context` 贯穿；Go 惯用的错误处理（`(T, error)` 而非 throw） |

---

## 版本与兼容策略

| 阶段 | 版本 | 承诺 |
|---|---|---|
| 设计期 | — | 无 API，只有文档 |
| 开发期 | `v0.x` | **随时可能破坏 API**，不做兼容承诺；changelog 里写清破坏点 |
| 稳定期 | `v1.0` | M0–M7 完成 + pg 稳定 + 至少一个第二方言验证过抽象 |
| 之后 | `v1.x` | 遵循语义化版本；破坏性变更等到 `v2` |

**对 drizzle-orm 的跟随策略**：跟随**设计**，不跟随版本号。rc.5 的某些特性（如 `strategy`、`extras`）如果在 Go 侧价值低，可以不实现 —— 但必须在差异登记里写明。

**Go 最小版本**：跟随 Go 的两个最新小版本的惯例，`go.mod` 里写明。

---

## 风险登记

| # | 风险 | 影响 | 概率 | 缓解 |
|---|---|---|---|---|
| R1 | **M5（RQB）复杂度失控** | 高 | 高 | 先做 `basic` strategy；先做 `one`/`many`；`through` 与 `optimized` 后置；每个子功能单独验收 |
| R2 | **M7（DDL diff）成为无底洞** | 高 | 高 | 只做 pg；只做 generate 不做自动修复；明确不支持视图/触发器/函数/分区；改名不猜 |
| R3 | **结果类型推导缺失导致 API 难用** | 高 | 中 | `From(Users)` 确定 `[]User`。不提供 `map[string]any` 扫描 |
| R4 | **生成扫描比手写 Scan 多分配** | 中 | 中 | M3 验收：整行 allocs/op 与手写 `rows.Scan` 相同 |
| R5 | **Go 的零值语义让 API 出现隐性陷阱** | 中 | 高 | 已由 [ADR-0003](./docs/adr/0003-zero-and-unset.md) 定稿；测试覆盖 `Limit(0)` 与 `Set(col, 0)` |
| R6 | **方言抽象漏了，加 mysql 时要改核心** | 中 | 中 | M8 的验收标准里明确要求「核心类型零改动」；改动必须记录 |
| R7 | **范围膨胀**（想同时做 studio / seed / 多语言客户端） | 高 | 中 | README 的非目标清单就是防线；新需求先问「是不是 v1 之后」 |
| R8 | **单人项目的时间风险** | 高 | 高 | 里程碑可独立交付；M0–M2 就是一个有价值的最小闭环（SQL 构建器） |

---

## 明确推迟

以下**明确不在 v1.0 范围**，写在这里是为了防止范围蔓延：

| 项 | 为什么推迟 |
|---|---|
| drizzle-studio 等价物（Web UI） | 独立产品，与「SQL 构建器」这个核心无关 |
| drizzle-seed 等价物 | 依赖 RQB 与伪随机数据生成，v1 之后 |
| mssql / singlestore / cockroachdb 方言 | 价值密度低；pg + mysql + sqlite 已覆盖大多数场景 |
| bun-sql / d1 / neon-http / libsql 等 serverless 驱动 | 每个都要处理「无长连接 / 无事务」的特殊性，v1 后逐个评估 |
| `mist pull`（从数据库反推 schema 代码） | 需要 introspection + 代码生成的反向链路，v1 后 |
| 迁移文件的自动 reconcile（自动改名检测） | 猜错的代价是丢数据，见 M7 的设计决策 |
| GraphQL / OpenAPI 生成 | 生态层，不在核心 |
| 连接池之外的可观测性（慢查询、追踪集成） | 交给 `database/sql` 生态与驱动 |

---

## 里程碑总表

| # | 里程碑 | 依赖 | 粗估 | 主要产出 | 状态 |
|---|---|---|---|---|---|
| M0 | 地基：SQL 片段层与渲染器 | — | 2–3 周 | `sql/`、`pg/dialect`、`internal/testutil` | 已完成 |
| M1 | Schema 定义与代码生成 | M0 | 3–4 周 | `pg/types`、`codegen/`、`cmd/mist generate` | 已完成 |
| M2 | 查询构建器（pg） | M0、M1 | 4–6 周 | `query/*`、`op` | 已完成 |
| M3 | 执行层 | M2 | 2–3 周 | `pg.DB`、`std.DB`、事务 | 已完成 |
| M4 | Codecs | M0、M3 | 3–4 周 | `pg/codecs/` | 已完成 |
| M5 | 关系查询（RQB） | M2–M4 | 5–7 周 | `relation/`、`pg/dialect_rqb` | 已完成 |
| M6 | 迁移器 | M3 | 2–3 周 | `migrate/` | 已完成 |
| M7 | DDL diff 与 CLI | M1、M6 | 8–12 周 | `introspect/`、`diff/`、`ddl/pg`、CLI | 已完成 |
| M8 | 第二方言：MySQL | M2 | 3–4 周 | `mysql/` | 已完成 |
| M9 | SQLite | M8 | 2–3 周 | `sqlite/`、`ddl/sqlite` | 已完成 |
| M10 | 生态与打磨 | 持续 | — | 测试工具、基准、示例、文档 | 进行中 |

**合计：34 – 49 人周。**

---

## 变更记录

| 日期 | 变更 |
|---|---|
| 2026-11-16 | 初版。参考基准锁定 drizzle-orm v1.0.0-rc.5 |
| 2026-11-20 | 接受 ADR-0002 / 0003 / 0004：查询链贴近 drizzle，生成函数扫描，零值三套机制，schema 以结构体为真相源 |
| 2026-11-20 | M1 生成器补上 `Columns` 与 `Row`。M2 开始：select 链可渲染 SQL |
| 2026-11-20 | select 链加上 inner / left / right / full / cross join |
| 2026-11-20 | insert / update / delete 可渲染。列清单决定写入，零值照写 |
| 2026-11-20 | pgx 连接池可执行 select / insert / update / delete。事务、savepoint、ctx 取消已接上 |
| 2026-11-20 | database/sql 驱动 `std.Open`。同一条查询链可在 pgx 池和 database/sql 上执行 |
| 2026-11-20 | `Prepare` 复用 SQL 文本和扫描计划。pgx 池走语句缓存，事务和 database/sql 真正预处理 |
| 2026-11-20 | 表达式补上 `inArray`、`between`、`like` / `ilike`、`isNull`、`exists` 及各自的否定。空列表按 drizzle 渲染成 `1 = 0` / `1 = 1` |
| 2026-11-20 | select 加上 `groupBy` / `having`。后写的子句覆盖先写的，位置在 `where` 与 `order by` 之间 |
| 2026-11-20 | 聚合函数 `count` / `sum` / `avg` / `min` / `max`，以及 `count` / `sum` / `avg` 的 `distinct`。`Count()` 渲染成 `count(*)` |
| 2026-11-20 | 聚合值用 `pg.Value[T]` 或 `pg.Scan` 读取。扫描函数由调用方按列顺序写出，不反射 |
| 2026-11-20 | `on conflict do update`。冲突目标、`set`、两处 `where`，拟插入的值写 `pg.Excluded` |
| 2026-11-20 | 集合运算 `union` / `union all` / `intersect` / `intersect all` / `except` / `except all`。两侧加括号，运算后的排序去掉表名 |
| 2026-11-20 | select 的 `with` / `with recursive`。CTE 可当 `From`，列用 `cte.Col` 换表名 |
| 2026-11-20 | 标量子查询、`in (select)`、`exists (select)` 和 `As` 派生表。派生表的列用别名 |
| 2026-11-20 | `update` 的 `from`，以及 inner / left / right / full join。`set` 之后、`where` 之前 |
| 2026-11-20 | `insert` / `update` / `delete` 也可以 `with` / `with recursive`。CTE 写在语句前面 |
| 2026-11-20 | `insert` 的 `values` 可以放标量子查询，也可以 `insert ... select`。两条不能一起写 |
| 2026-11-20 | `Table()` 解析复合主键、索引、CHECK。`jsonb` 对应 `json.RawMessage`，`numeric` 对应 `string` |
| 2026-11-20 | `delete ... using`、`distinct` / `distinct on`、`for update`，以及 `add` / `sub` / `mul` / `div` |
| 2026-11-20 | M1 写出 `ColumnSpecs()`，展开本包嵌入字段。M2 锁住 40 条 SQL，并登记无 join 时 select 列表的表名差异。M3 补上 100 路并发和整行扫描分配对比 |
| 2026-11-20 | M4：`numeric`、`jsonb`、`bytea`、`interval`、数组、`uuid`、枚举的 codec。参数带类型后缀。未知类型不静默当成字符串 |
| 2026-11-20 | M5：`one` / `many` 关系查询。SQL 按 ADR-0005，含 `optimized` 包裹、`exists` 和 `desc(count)`。`through` 仍会报错 |
| 2026-11-20 | M5 遗留：`through`、`comment`、`extras` 读取、嵌套 `numeric` / 时间 / `bigint` 的 `::text`。`WithCache` 使用调用方提供的 `Cache` |
| 2026-11-20 | M6：`migrate.Run` 按目录应用 v1 迁移表。幂等看 `name`。已应用文件变了报目录名。事务内 advisory lock |
| 2026-11-23 | M7 开始：`schema` / `diff` / `ddl/pg`。`mist ddl` 对照空库打印建表 SQL。不推断改名。introspect 未做 |
| 2026-11-23 | `introspect.Read` 从 PostgreSQL 反读表、列、约束、索引和 enum。`bigserial` 从 `nextval` 折回。应用后再 diff 为空 |
| 2026-11-23 | `mist ddl -write` 按快照写迁移目录。`mist migrate` 应用。`mist check` 对照快照和文件哈希。三次生成再应用，反读后 diff 为空 |
| 2026-11-23 | M7：`mist push` 默认只打印 SQL，确认或 `--force` 才执行。20 张表的迁移应用到空库再反读，diff 为空 |
| 2026-11-23 | M8 开始：MySQL 的反引号、`?` 占位符，以及 select / insert / update / delete。没有的语法报错，不改 `sql/` 和 `query/` |
| 2026-11-23 | M8：MySQL 渲染 `with` 和集合运算。M2 查询矩阵在 `mysql/fixture_test.go`，方言限制单独成组 |
| 2026-11-23 | M8：MySQL 上跑通 join、`with`、集合运算、`offset` 和 `for update of`。单独的 `offset` 补上最大 `limit`。`ilike` 被服务器拒绝 |
| 2026-11-23 | M8：`OnDuplicateKeyUpdate` 渲染 `on duplicate key update`。PostgreSQL 拒绝这个子句。见 ADR-0010 |
| 2026-11-23 | M8：MySQL 的 `decimal` / `json` / `datetime` / `blob` 参数不加 `::类型`。往返已在 MySQL 8.4 上跑过。没有数组 |
| 2026-11-23 | M8：多表 update / delete 用逗号来源。重复表名由 MySQL 拒绝 |
| 2026-11-23 | M8：`insert select` 的 `on duplicate key update` 套一层子查询，`mysql.NewRow` 仍指向拟写入的行 |
| 2026-11-23 | M8：生成器认 `mysql.TableMeta`，写出 `mysql.Col`。PostgreSQL 专有类型会报错 |
| 2026-11-23 | M8：生成的 `m8_notes` 在 MySQL 8.4 上插入再整行读回 |
| 2026-11-23 | M8：MySQL 把 `ilike` 渲染成 `lower(列) like lower(参数)`，大小写不敏感的匹配已在库上跑过 |
| 2026-11-23 | M8：`mysql.FindMany` 在 MySQL 8.4 上拉出 `one` / `many` 嵌套。SQL 用 `json_object`，见 ADR-0011 |
| 2026-11-23 | M8：MySQL 上跑通 `through`、关系 `exists`、`count` 排序，以及根行和嵌套行的 `extras` |
| 2026-11-23 | M8：嵌套 JSON 的 `CastInJSON: "text"` 写成 `cast(列 as char)`。`decimal`、`datetime` 和超大 `bigint` 已在 MySQL 8.4 上读回 |
| 2026-11-23 | M8：`Insert.Ignore` 渲染 `insert ignore`。冲突行被丢掉，旧行不变。PostgreSQL 拒绝这个修饰。见 ADR-0012 |
| 2026-11-23 | M8：单表 `update` / `delete` 可以写 `order by` 和 `limit`。多表和 PostgreSQL 都拒绝。见 ADR-0013 |
| 2026-11-23 | `ForShare` 渲染 `for share`。PostgreSQL 和 MySQL 都写这个子句，`of`、`nowait`、`skip locked` 沿用 `for update` |
| 2026-11-23 | M8 完成。查询矩阵里 MySQL 接受的语句已在 8.4 上执行。`insert select` 的 `with` 放在列清单后面。DDL 仍只做 PostgreSQL |
| 2026-11-23 | M9 开始：SQLite 默认驱动是 glebarez/go-sqlite，`CGO_ENABLED=0` 即可。基础 CRUD、`on conflict`、`insert or ignore` 已跑过。关系查询和改列 DDL 未做 |
| 2026-11-23 | M9 完成。`sqlite.FindMany` 在嵌入的 3.53.3 上跑通 `one` / `many` / `through`。改列重建表已执行。见 ADR-0014 |
| 2026-11-23 | 查询日志。`logger.Default` 的行格式与 drizzle-orm 的 `DefaultLogger` 相同。不传 logger 时不写。pg、std、mysql、sqlite 的查询、执行和预处理语句都会记 |
| 2026-11-23 | M10 开始。`testutil.Run` 按目录对拍 golden。集成测试用 testcontainers 起 PostgreSQL 和 MySQL，SQLite 仍是临时文件。`bench/` 对比 sqlx、gorm、ent、bun，Actions 保存基准输出 |
