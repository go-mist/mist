# ROADMAP

本文档是 drizzle-go 的实施计划。**Why / Features 见 [README.md](./README.md)。**

- 参考基准：**drizzle-orm v1.0.0-rc.5**（793 个 `.ts` 文件，约 109,555 行）
- 当前状态：**M0 已完成**。查询语法、零值、schema 真相源已由 ADR-0002 / 0003 / 0004 定稿，M1 未开工
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

**状态**：未开工 · **粗估**：3 – 4 周 · **依赖**：M0

#### 目标

让用户用 Go struct + tag 描述 schema，`go generate` 产出类型安全的表与列描述符。**不引入运行时反射读 schema。**

#### 交付物

| 包 / 类型 | 内容 |
|---|---|
| `pg/types.go` | pg 类型常量与列修饰符：`BigSerial` / `Text` / `Timestamptz` / `Numeric` / `JsonB` / … |
| `codegen/parser.go` | 解析 `drizzle:"..."` tag |
| `codegen/validate.go` | tag 校验：非法类型名、重复列名、悬空 `references`、自相矛盾的修饰符 |
| `codegen/emit.go` | 生成 `schema_gen.go` |
| `codegen/templates/` | `text/template` 模板 |
| `cmd/drizzle-go/generate.go` | `drizzle-go generate` 子命令 |

#### tag 语法（草案）

```
`drizzle:"<column_name>,<type>,<modifier>,<modifier>..."`

修饰符：
  pk | notnull | unique | unique=<name>
  default=<literal> | defaultnow
  references=<table>.<column>[,ondelete=<action>][,onupdate=<action>]
  generated=<expr> | identity
  length=<n> | precis=<p,s>
```

#### 生成产物形态

```go
// Code generated by drizzle-go. DO NOT EDIT.

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

- **codegen 通过 `go run` 载入用户的 schema 包**，而不是自己解析 AST。理由：AST 解析要处理泛型、别名、嵌入字段、跨包引用，成本远高于「编译一次再执行」。代价是 schema 包必须能编译通过 —— 这是**特征**，不是缺陷。
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
- tag 只表达列级约束。表名、复合主键、索引、CHECK 放在 `Table()`。M1 先解析表名和复合主键。见 [ADR-0004](./docs/adr/0004-schema-source.md)。

---

### M2 查询构建器（PostgreSQL）

**状态**：未开工 · **粗估**：4 – 6 周 · **依赖**：M0、M1

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

1. README 里的 select 例子输出逐字节等于 drizzle rc.5 的输出
2. fixture 矩阵（至少 40 条）覆盖：多 join、子查询、exists、in (subquery)、group by + having、聚合 + distinct、union、CTE、on conflict do update
3. `insert` 批量 values 使用**单个** `insert ... values (...), (...)`，不是循环单条
4. `update ... set x = <subquery>` 与 `set x = sql.Raw(...)` 都支持
5. builder 可安全并发使用（同一个 builder 被两个 goroutine 渲染不产生数据竞争），有 `-race` 测试

#### 主要风险

- select 是 drizzle 里最大的文件之一（`pg-core/query-builders/select.ts` 1395 行）。**不要试图一次覆盖全部组合**，按 fixture 矩阵增量推进。
- 行类型由 `Table[T]` 带进 `Select[T]`，`All` 返回 `[]T`。见 [ADR-0002](./docs/adr/0002-query-syntax-and-scan.md)。

---

### M3 执行层：驱动、会话、事务

**状态**：未开工 · **粗估**：2 – 3 周 · **依赖**：M2

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

**状态**：未开工 · **粗估**：3 – 4 周 · **依赖**：M0、M3

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

**状态**：未开工 · **粗估**：5 – 7 周（可能翻倍） · **依赖**：M2、M3、M4

#### 目标

把「一次查询拉出嵌套结构」这件事做成 drizzle 的样子：**配置是数据，不是回调**。

#### 交付物

| 包 / 类型 | 内容 |
|---|---|
| `relation/define.go` | `DefineRelations` / `DefineRelationsPart`（对应 rc.5 `defineRelations`） |
| `relation/config.go` | `DBQueryConfig`：`With` / `Columns` / `Where` / `OrderBy` / `Extras` / `Limit` / `Offset` / `Strategy` / `Comment` / `WithCache` |
| `relation/filter.go` | `RelationsFilter`：`AND` / `OR` / `NOT` / `RAW` + 列条件 / 关系条件 |
| `relation/order.go` | 排序：`Asc` / `Desc`（如 `q.Desc(Posts.CreatedAt)`）+ 跨关系聚合排序（`desc(count)` / `asc(max)`，对应 rc.5 的 `RelationsAggregatedOrderDirection`） |
| `relation/errors.go` | 「表未被查询」「关系不存在」这类错误的运行时检查 |
| `pg/dialect_rqb.go` | `BuildRelationalQuery` + `collectRelationalQuery` 的移植 |
| `relation/mapper.go` | 嵌套结果组装。叶子行复用 ADR-0002 的生成扫描函数，不反射 |
| `relation/strategy.go` | `basic` / `optimized` 两种 strategy 的分支 |

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

**状态**：未开工 · **粗估**：2 – 3 周 · **依赖**：M3

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

**状态**：未开工 · **粗估**：8 – 12 周（最重） · **依赖**：M1、M6

#### 目标

drizzle-kit 的等价物：从 schema 生成迁移，或直接推到数据库。

#### 交付物

| 包 / 类型 | 内容 |
|---|---|
| `introspect/pg.go` | 从 `information_schema` + `pg_catalog` 反读实际结构 |
| `schema/model.go` | 方言无关的 schema 中间表示（表、列、索引、约束、enum、序列） |
| `diff/diff.go` | 期望 vs 实际 → 变更集 |
| `ddl/pg/emit.go` | 变更集 → SQL 语句 |
| `cmd/drizzle-go/generate.go` | `generate` 子命令 |
| `cmd/drizzle-go/migrate.go` | `migrate` 子命令 |
| `cmd/drizzle-go/push.go` | `push` 子命令 |
| `cmd/drizzle-go/check.go` | `check` 子命令 |

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

**状态**：未开工 · **粗估**：3 – 4 周 · **依赖**：M2（M6/M7 可选）

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

---

### M9 SQLite

**状态**：未开工 · **粗估**：2 – 3 周 · **依赖**：M8

#### 目标

第三个方言，验证「方言扩展是天级工作」这个判断是否成立。

#### 特殊之处

- 类型系统弱（动态类型 + 类型亲和性）
- 无原生 `boolean` / `timestamptz`
- 无 `right join`（老版本）/ 无 `full join`
- `returning` 需要较新版本
- DDL 极为受限（改列类型要重建表）

#### 验收标准

1. 基础 CRUD + 关系查询在 sqlite 上跑通
2. **ddl 生成器能处理「改列」这种情况**（sqlite 需要 create new + copy + drop + rename），或者在 `check` 阶段明确拒绝并说明原因
3. 从 M0 到 M9，`sql/` 核心类型零改动（或改动被记录）

---

### M10 生态与打磨

**状态**：未开工 · **持续** · **依赖**：M0 起持续

| 项 | 内容 |
|---|---|
| Golden 测试工具 | 把 fixture 对拍做成可复用流程，任何人能加用例 |
| testcontainers | 集成测试起临时 pg / mysql / sqlite |
| Benchmark | 查询构建、结果映射、并发执行三组基准，CI 记录历史 |
| 示例项目 | REST 服务 + CLI 两个完整示例 |
| 文档站 | pkg.go.dev 之外的教程、迁移指南（从 gorm / sqlc 过来） |
| 发布流程 | tag、changelog、语义化版本 |
| 错误信息 | 所有用户可见错误都必须包含「是哪张表、哪一列、期望什么」 |

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
| ADR-0008 | DDL diff 的中间表示边界：支持哪些对象 | M7 开工前 | 待定 |
| ADR-0009 | 是否提供 `drizzle-go` CLI 之外的程序化 API | M7 内 | 待定 |

---

## 与 drizzle-orm 的差异登记

**主动偏离**（有意为之，不是还没做）：

| 项 | drizzle-orm rc.5 | drizzle-go | 理由 |
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
| `drizzle-go pull`（从数据库反推 schema 代码） | 需要 introspection + 代码生成的反向链路，v1 后 |
| 迁移文件的自动 reconcile（自动改名检测） | 猜错的代价是丢数据，见 M7 的设计决策 |
| GraphQL / OpenAPI 生成 | 生态层，不在核心 |
| 连接池之外的可观测性（慢查询、追踪集成） | 交给 `database/sql` 生态与驱动 |

---

## 里程碑总表

| # | 里程碑 | 依赖 | 粗估 | 主要产出 | 状态 |
|---|---|---|---|---|---|
| M0 | 地基：SQL 片段层与渲染器 | — | 2–3 周 | `sql/`、`pg/dialect`、`internal/testutil` | 已完成 |
| M1 | Schema 定义与代码生成 | M0 | 3–4 周 | `pg/types`、`codegen/`、`cmd/drizzle-go generate` | 未开工 |
| M2 | 查询构建器（pg） | M0、M1 | 4–6 周 | `query/*`、`sql/expr` | 未开工 |
| M3 | 执行层 | M2 | 2–3 周 | `session/`、`pg/scan`、`pg/db`、`pgx/`、`std/` | 未开工 |
| M4 | Codecs | M0、M3 | 3–4 周 | `pg/codecs/` | 未开工 |
| M5 | 关系查询（RQB） | M2–M4 | 5–7 周 | `relation/`、`pg/dialect_rqb` | 未开工 |
| M6 | 迁移器 | M3 | 2–3 周 | `migrate/` | 未开工 |
| M7 | DDL diff 与 CLI | M1、M6 | 8–12 周 | `introspect/`、`diff/`、`ddl/pg`、CLI | 未开工 |
| M8 | 第二方言：MySQL | M2 | 3–4 周 | `mysql/` | 未开工 |
| M9 | SQLite | M8 | 2–3 周 | `sqlite/` | 未开工 |
| M10 | 生态与打磨 | 持续 | — | 测试工具、基准、示例、文档 | 未开工 |

**合计：34 – 49 人周。**

---

## 变更记录

| 日期 | 变更 |
|---|---|
| 2026-11-16 | 初版。参考基准锁定 drizzle-orm v1.0.0-rc.5 |
| 2026-11-20 | 接受 ADR-0002 / 0003 / 0004：查询链贴近 drizzle，生成函数扫描，零值三套机制，schema 以结构体为真相源 |
