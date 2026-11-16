# drizzle-go

> 用 Go 复刻 [drizzle-orm](https://github.com/drizzle-team/drizzle-orm) 的设计 —— 保留 SQL 语感，把类型安全交给代码生成。

**状态：设计阶段**。当前仓库只有一个 `go.mod`，本文档与 [ROADMAP.md](./ROADMAP.md) 描述的是**目标形态**，不是既有能力。所有 `Features` 章节的条目目前状态均为「设计」。

任务排期与验收标准见 [ROADMAP.md](./ROADMAP.md)。

---

## 目录

- [Why drizzle-go](#why-drizzle-go)
- [Why Go](#why-go)
- [Features](#features)
- [非目标](#非目标)
- [设计原则](#设计原则)
- [目标 API（草案）](#目标-api草案)
- [包结构（规划）](#包结构规划)
- [与 drizzle-orm 的对应关系](#与-drizzle-orm-的对应关系)
- [参考基准](#参考基准)
- [许可协议](#许可协议)

---

## Why drizzle-go

### Go 的 ORM 版图上缺一块

| 方案 | 优势 | 缺什么 |
|---|---|---|
| **gorm** | 生态最大、上手最快 | 运行时反射、隐式行为多、类型弱、生成的 SQL 不可预测 |
| **ent** | 强类型、图模型清晰、codegen 成熟 | schema 用 Go DSL 描述、生成代码体量大、查询 API 是 ent 自己的范式，**丢失 SQL 语感** |
| **sqlc** | SQL 即真相、类型安全、零运行时开销 | 只有静态 SQL，**没有查询构建器**，动态条件要手拼字符串 |
| **sqlx** | 轻、贴近 `database/sql` | 本质只是 `Scan` 辅助 |
| **原生 `database/sql`** | 完全可控 | 全部手写 |

drizzle-orm 占据的那个位置 —— **类型安全 + 保留 SQL 语感 + 结构化配置** —— 在 Go 里是空的。

drizzle-go 就是去填这个空位。它的比较对象不是 drizzle-orm，而是 gorm / ent / sqlc。

### 三条具体的诉求

1. **写下的代码要能看出 SQL。** 不想要 `db.Where("name = ?", n).Preload("Posts")` 这种「说不上来最后发了什么 SQL」的体验。
2. **类型错误要在编译期暴露。** 列名写错、类型不匹配、`returning` 的列不存在 —— 这些不该等到运行时。
3. **动态查询要能组合。** 条件按业务拼装，而不是靠字符串拼接或 `if` 分支里反复传参。

---

## Why Go

这一节回答另一个问题：既然已经有 drizzle-orm，为什么还要在 Go 里重做一遍？

### 1. 部署形态

drizzle-orm 运行需要 Node / Bun / Deno 之一 + `node_modules` + 构建链（tsc / esbuild / tsx）。参考实现（v1.0.0-rc.5）`src/` 目录有 793 个 `.ts` 文件、约 11 万行，再叠加各 driver 的 peer deps（`pg`、`postgres`、`mysql2`、`better-sqlite3`、`@libsql/client`、`@neondatabase/serverless` ……），部署镜像动辄数百 MB。

drizzle-go 编译出来是**单个静态二进制**，可直接放进 `scratch` 或 `distroless` 镜像。`CGO_ENABLED=0 go build` 交叉编译到 `linux/amd64` 之后，拷过去就能跑。

受这个红利影响最直接的两个场景：

- **CLI 与运维脚本** —— 一个二进制拷到机器上就能连库查数据，不需要 `npx`、不需要运行时。
- **冷启动敏感的 serverless** —— Go 二进制是毫秒级起步，Node 需要加载整张模块图。差别不是「快一点」，是数量级。

### 2. 工具链收敛

drizzle-orm 的日常开发循环需要四套工具、三种配置格式：`tsc`（类型检查）+ `drizzle-kit`（schema 元数据 / 迁移）+ `tsx` 或 `esbuild`（运行）+ 格式化工具。

drizzle-go 的循环只有两个命令：`go build` 和 `go generate`。**codegen 是 `go:generate` 流水线的一部分，不是独立工具。**

### 3. 没有历史包袱

这是逐行阅读 rc.5 之后最实在的一条。rc.5 里有一批代码，存在理由仅仅是「兼容 0.x」：

| rc.5 中的东西 | 存在理由 | drizzle-go 需要吗 |
|---|---|---|
| `upToV1` 约 300 行升级逻辑 | 把 0.x 的迁移表升级到 v1 结构 | **不需要**，新项目直接建 v1 表 |
| `Chunk` 类型允许裸 `string`，但运行时走参数绑定 | 类型兼容 | **不需要**，显式区分 `Fragment` / `Raw` |
| `ValueRows.getSQL()` legacy shim | 兼容旧调用 | 不需要 |
| `sql.fromList` 上的 `@deprecated` | 兼容旧 API | 不引入 |
| `Column.primary` 而 `config` 仍叫 `primaryKey` | 改名兼容期 | 统一命名 |
| `async/` 与 `effect/` 两套各 823 行的 `db.ts` | 两套 effect 集成 | 一份带 `context.Context` 的实现 |

合计是数千行的纯兼容成本，新项目可以完全不吃。

### 4. 方言扩展成本低一个数量级

TS 版加一个方言，要写一整套高阶类型（hkt）：

```ts
PgRelationalQueryHKTBase / PgRelationalQueryHKT / PgRelationalQueryKind<...>
```

而这些存在的唯一目的，是让方法返回正确的**类型**。`PgDialect` 本体其实只有十四个成员，且全部是「数据 → SQL 字符串」的纯函数：

```
escapeName / escapeParam / escapeString
buildSelectQuery / buildInsertQuery / buildUpdateQuery / buildDeleteQuery
buildSetOperations / buildSetOperationQuery / buildRefreshMaterializedViewQuery
buildRelationalQuery / sqlToQuery / _sqlToQuery
```

Go 里方言就是一个接口 + 三个 escape 函数 + 若干 `Build*Query`。新增一个方言是**天级的**工作量，不是周级。

### 5. 生成代码是可读、可调试的

TS 用户悬停 `db.query.users.findMany()` 的返回类型时，得到的往往是一层套一层、展开不完的交叉类型与条件类型；大型 schema 下还需要 `skipLibCheck` 来压住 `tsc` 的耗时。

Go 版 codegen 生成的是**普通结构体**：

```go
type User struct {
	ID        int64
	Name      string
	Email     string
	CreatedAt time.Time
}
```

gopls 的跳转、重命名、补全全部原生可用，而且用户**能读、能改、能审查**生成结果 —— 不是悬停出一个不可读的类型表达式。

### 一条诚实的说明：不要拿性能当卖点

rc.5 通篇在做 JavaScript 侧的运行期优化：

- `commonStringChunks` 把约 70 个高频 SQL 片段驻留成单例
- `chunkKinds` 数字化分派 + 原型上的 `Symbol` 缓存 + `WeakMap` 构造器表，绕开字符串比较
- `useJitMappers` 用 `new Function` **运行时生成行映射代码**

V8 优化之后由 `new Function` 生成的映射器，很可能比 Go 的反射映射**更快**。因此：

> 单条查询构建的稳态性能，drizzle-go 大概率是**打平或略输**，因为它没有 JIT 这条路。

Go 真正赢的只有两条，且都不是「更快」，而是「更省」：

- **冷启动** —— 毫秒级 vs 秒级
- **内存基线** —— 几 MB vs 几十 MB 的堆占用

对一个已经跑着长驻 Node 服务的项目，迁到 Go 换来的主查询 QPS 提升可能接近于零。**性能不是这个项目的立项理由。**

---

## Features

图例：`设计` = 已规划未开工 · `实现中` = 正在开发 · `已支持` = 可用。当前全部为 `设计`。

### Schema 定义

| 特性 | 状态 | 说明 |
|---|---|---|
| 用 Go struct + tag 描述 schema | 设计 | 唯一的真相源，可以在 IDE 里直接跳转 |
| `go:generate` 产出列描述符 | 设计 | 生成 `Column` / `Table` 值，**不做运行时反射读 schema** |
| 编译期与生成期双重校验 | 设计 | 非法 tag、未知类型、悬空外键在 `go generate` 阶段报错 |
| 生成产物可读可改 | 设计 | 不做混淆，不生成巨型 switch |

### SQL 片段层

| 特性 | 状态 | 说明 |
|---|---|---|
| `Chunk` 模型 | 设计 | `s` 串片段 / 列引用 / 参数 / 占位符 / 子查询 / 表 / 别名 的统一表示 |
| `sql` 模板的 Go 等价物 | 设计 | `sql.S("select * from ", Users, " where ", eq(...))` 形式的显式拼接 |
| 参数编号与方言化 | 设计 | pg `$1`、mysql `?`、mssql `@p1` 由方言决定 |
| 内联参数模式 | 设计 | 用于调试输出与 DDL |
| Golden test 对齐 | 设计 | 同一份 fixture 在 drizzle 与 drizzle-go 上输出逐字节相同的 SQL |

### 查询构建器

| 特性 | 状态 | 说明 |
|---|---|---|
| `select` | 设计 | `from` / `where` / `orderBy` / `limit` / `offset` / `groupBy` / `having` / 各类 join |
| 子查询 | 设计 | 标量子查询、`exists`、`in`、派生表 |
| 集合运算 | 设计 | `union` / `unionAll` / `intersect` / `except` |
| `insert` | 设计 | 批量 values、`onConflictDoNothing` / `onConflictDoUpdate`、`returning` |
| `update` | 设计 | `set` / `where` / `returning`，支持 `sql` 表达式赋值 |
| `delete` | 设计 | `where` / `returning` |
| 表达式系统 | 设计 | `eq/ne/gt/gte/lt/lte/inArray/notInArray/like/ilike/isNull/isNotNull/between/and/or/not/exists` |
| 聚合函数 | 设计 | `count/sum/avg/min/max` + `distinct` |
| CTE（`with`） | 设计 | 递归与非递归 |
| Prepared statement 复用 | 设计 | 同一 SQL 形状多次执行时复用底层 prepared query |

### 关系查询（RQB）

| 特性 | 状态 | 说明 |
|---|---|---|
| 关系定义 | 设计 | 显式声明 `one` / `many`，支持 `through` 多对多 |
| 结构化配置 | 设计 | `with` / `columns` / `where` / `orderBy` / `extras` / `limit` / `offset` |
| 数据化 `where` | 设计 | 对齐 rc.5：不是回调，是数据对象；`RAW` 是唯一的 SQL 逃生口 |
| 聚合排序 | 设计 | `desc(count)`、`asc(max)` 这类跨关系聚合排序 |
| `basic` / `optimized` 两种 strategy | 设计 | 具体语义在 M5 开工前出 ADR 定稿 |
| 嵌套结果映射 | 设计 | 默认反射映射到目标 struct，可选用 codegen 生成映射器 |

### 类型映射（Codecs）

| 特性 | 状态 | 说明 |
|---|---|---|
| 六槽位 codec | 设计 | `cast / castInJson / castParam / normalize / normalizeInJson / normalizeParam` |
| 数组维度 | 设计 | 一维 / 多维数组的类型与 cast |
| noop 快速路径 | 设计 | 零值判定，跳过无意义转换 |
| pg 类型全集 | 设计 | `numeric` / `interval` / `jsonb` / `bytea` / 各整数与时间类型 |
| 自定义 codec 注册 | 设计 | 用户可为自有类型注册编解码 |

### 迁移

| 特性 | 状态 | 说明 |
|---|---|---|
| 从目录读取迁移 | 设计 | 目录即真相源，`--> statement-breakpoint` 切分语句 |
| 迁移表 | 设计 | **直接采用 v1 结构**，不实现 `upToV1` |
| 幂等 | 设计 | 以 `name` 集合判定已应用，与 rc.5 一致 |
| 哈希校验 | 设计 | 已应用迁移内容变更时明确报错，不静默跳过 |
| 并发安全 | 设计 | 多实例同时 `migrate` 只生效一次（advisory lock 或等价机制） |
| DDL 生成 | 设计 | schema → 迁移 SQL，见下 |

### DDL 与 CLI

| 特性 | 状态 | 说明 |
|---|---|---|
| schema introspection | 设计 | 从数据库反读实际结构 |
| schema diff | 设计 | 期望结构 vs 实际结构，产出变更 |
| `drizzle-go generate` | 设计 | 生成迁移文件 |
| `drizzle-go migrate` | 设计 | 应用迁移 |
| `drizzle-go push` | 设计 | 直接推到数据库，不走迁移文件 |
| `drizzle-go check` | 设计 | 校验迁移文件与 schema 是否一致 |

### 执行层

| 特性 | 状态 | 说明 |
|---|---|---|
| pgx 驱动 | 设计 | 首选驱动 |
| `database/sql` 驱动 | 设计 | 兼容面更广 |
| `context.Context` 贯穿 | 设计 | 取消、超时、链路追踪 |
| 事务 | 设计 | `db.Tx(ctx, func(tx *Tx) error {...})`，回滚由 error 决定 |
| 嵌套事务 | 设计 | savepoint |
| 连接池 | 设计 | 复用驱动自身的池，不另造 |

### 工具与生态

| 特性 | 状态 | 说明 |
|---|---|---|
| Golden SQL 测试工具 | 设计 | 与 drizzle 参考实现输出对拍 |
| testcontainers 集成 | 设计 | 集成测试起临时数据库 |
| Benchmark 套件 | 设计 | 查询构建、结果映射、并发执行三组 |
| 示例项目 | 设计 | 至少覆盖 REST 服务与 CLI 两种用法 |

---

## 非目标

明确**不做**的事，避免范围失控：

1. **不做 ActiveRecord 风格。** 模型结构体不携带 `Save()` / `Find()` 方法。查询从 `db` 出发，不是从数据出发。
2. **不做隐式查询。** 没有「访问关联字段自动懒加载」。所有 SQL 都是你写下的。
3. **不追求 100% API 兼容 drizzle-orm。** 对齐的是**设计**，不是逐行翻译。Go 做不到的形式（类型级推导、模板字面量类型、Proxy）会换实现方式。
4. **不做运行时反射驱动的 schema。** schema 只来自代码生成。
5. **不做 GraphQL / 前端集成 / React hooks。** 那是 drizzle-orm 生态的另一半。
6. **不做运维特性**（监控面板、慢查询分析、连接路由）。
7. **v1.0 不做 Web UI。** drizzle-studio 的等价物不在范围内。

---

## 设计原则

1. **SQL 优先。** 你写的东西要能一眼看出它会变成什么 SQL。做不到的抽象就不要。
2. **类型来自代码生成，不来自反射。** 反射只允许出现在两处：结果行映射、以及 codegen 加速不了的自定义场景。
3. **显式优于隐式。** 没有隐式查询、没有隐式事务、没有隐式迁移。
4. **方言 = 三个 escape 函数 + 若干 build 函数。** 任何让方言接口膨胀的设计都要重新考虑。
5. **单向数据流。** `schema → 描述符 → chunks → (SQL string, args)`。不允许反向依赖。
6. **不为自己留包袱。** 新项目没有兼容义务，历史遗留形式一律不引入。
7. **诚实标注状态。** 文档不夸大能力，「设计」不写成「已支持」。

---

## 目标 API（草案）

> 以下全部是**设计稿**，最终形态会在对应里程碑开工前以 ADR 定稿。写在这里是为了说明设计意图，不是承诺。

### Schema

```go
// schema/schema.go —— 用户手写，唯一真相源
package schema

//go:generate go run github.com/xbmlz/drizzle-go/cmd/drizzle-go generate

type User struct {
	ID        int64     `drizzle:"id,bigserial,primary"`
	Name      string    `drizzle:"name,text,notnull"`
	Email     string    `drizzle:"email,text,notnull,unique"`
	CreatedAt time.Time `drizzle:"created_at,timestamptz,default=now()"`
}

type Post struct {
	ID       int64  `drizzle:"id,bigserial,primary"`
	AuthorID int64  `drizzle:"author_id,bigint,notnull,references=users.id"`
	Title    string `drizzle:"title,text,notnull"`
}
```

`go generate` 产出同包的 `schema_gen.go`：`Users`、`Posts` 两个表描述符，以及 `Users.Name` 这类列引用。

### 查询构建

```go
rows, err := db.Select().
	From(Users).
	Where(and(
		eq(Users.Name, "alice"),
		gt(Users.ID, 100),
	)).
	OrderBy(desc(Users.CreatedAt)).
	Limit(10).
	All(ctx)
```

生成的 SQL：

```sql
select "users"."id", "users"."name", "users"."email", "users"."created_at"
from "users"
where "users"."name" = $1 and "users"."id" > $2
order by "users"."created_at" desc
limit $3
```

`args = ["alice", 100, 10]`

### 写入

```go
db.Insert(Users).
	Values(User{Name: "alice", Email: "a@example.com"}).
	OnConflictDoNothing().
	Returning(Users.ID, Users.CreatedAt)
```

```sql
insert into "users" ("name", "email") values ($1, $2)
on conflict do nothing
returning "users"."id", "users"."created_at"
```

### 关系查询

```go
res, err := db.Query.Users.FindMany(ctx, q.Users{
	Columns: q.Cols(Users.ID, Users.Name),
	With: q.With{
		"posts": q.Posts{
			Columns: q.Cols(Posts.ID, Posts.Title),
			OrderBy: q.Desc(Posts.CreatedAt),
			Limit:   10,
		},
	},
	Where: q.Eq(Users.Name, "alice"),
})
```

> 注意：rc.5 里 `where` 是数据对象而非回调，Go 无法写裸字面量对象，因此用 `q.Eq(...)` 这类构造函数表达同等结构。

---

## 包结构（规划）

```
entity/           运行时类型标识（entityKind 的 Go 等价物）
sql/              chunk 模型、SQL 片段、渲染器、表达式系统
sql/dialect/      Dialect 接口与方言无关的共享逻辑
pg/               PostgreSQL 方言（类型、dialect 实现、DDL 片段）
pg/codecs/        pg 类型 ↔ Go 类型映射
query/            查询构建器（方言无关部分）
relation/         关系定义与 RQB 配置结构
session/          执行会话与事务（方言无关流程）
migrate/          迁移器
introspect/       从数据库反读实际结构
diff/             期望 schema vs 实际 schema 的变更集
ddl/              变更集 → 方言 DDL
pgx/              pgx 驱动
std/              database/sql 驱动
codegen/          schema 代码生成
cmd/drizzle-go/   CLI
internal/testutil golden 测试与对拍工具
```

---

## 与 drizzle-orm 的对应关系

| 概念 | drizzle-orm (rc.5) | drizzle-go |
|---|---|---|
| SQL 片段 | `` sql`...` `` 模板标签 | `sql.S(...)` 显式拼接 |
| 原始字符串 | `sql.raw()` | `sql.Raw(...)` |
| 参数 | `Param` | `sql.Param` |
| 列引用 | column chunk | 生成包里的列描述符 |
| 渲染入口 | `buildQueryFromSourceParams` | `sql.Render` |
| 方言钩子 | `escapeName` / `escapeParam` / `escapeString` | 同名方法 |
| 类型映射 | `codecs.ts`（六槽位） | `pg/codecs`（六槽位） |
| 关系查询编译 | `collectRelationalQuery` | `Dialect.BuildRelationalQuery` |
| 迁移 | `readMigrationFiles` + `migrate` | `migrate` |
| DDL / 工具 | drizzle-kit | `cmd/drizzle-go` |

---

## 参考基准

设计与语义对齐 **drizzle-orm v1.0.0-rc.5**：

- 793 个 `.ts` 源文件，约 109,555 行
- 只对齐**设计**，不逐行翻译
- 行为差异记录在 [ROADMAP.md 的差异登记](./ROADMAP.md#与-drizzle-orm-的差异登记)一节

---

## 许可协议

待定。
