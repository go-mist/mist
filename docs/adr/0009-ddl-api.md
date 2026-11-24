# ADR-0009：DDL 的程序化 API

- **状态**：已接受
- **日期**：2026-11-23
- **里程碑**：M7
- **相关**：[ADR-0008](./0008-ddl-model.md)

## 背景

CLI 要把结构体变成 SQL，测试也要能不启动命令就比较两份 schema。如果逻辑只写在 `main` 里，库的调用方和测试都得再实现一遍。

## 决策

程序化 API 就是这几个包，CLI 只做参数和退出码：

- `schema`：中间表示
- `codegen.LoadSchema`：从结构体源码读出期望结构，不执行用户代码
- `diff.Diff`：期望结构对实际结构
- `ddl/pg.Render`：变更集写成 PostgreSQL 语句，语句之间用 `--> statement-breakpoint` 切开

`mist generate` 继续给 `go:generate` 写 Go 描述符。`mist ddl` 不带 `-migrations` 时对照空库，把 SQL 打到标准输出。带了 `-migrations` 就对照最新快照。只有一份最新快照时用它；有两份时，按列、索引、约束做三方合并，改得不一样就停。`-write` 新建一个目录，里面是这次的 `up.sql`、`down.sql` 和 `snapshot.json`。`down.sql` 只占位，不执行。快照记下这次 `up.sql` 的 sha256，以及它接着哪些快照。已经写过的目录不再改。不写 `meta/_journal.json`。

`mist check` 看期望结构是否还和快照一致，以及迁移文件有没有被改过。它不重放 SQL。`mist migrate` 调用 `migrate.Run` 应用目录。

## 后果

- 以后的 introspect、`push`、`check` 调用同一套 `diff` 和 `ddl/pg`，不在命令里再写一份规则。
- 落盘在 `WriteMigration`，不塞进 `Render`。`mist push` 默认只打印 SQL；终端确认或 `--force` 才在一个事务里执行。

## 被否决的方案

| 方案 | 否决理由 |
|---|---|
| 只提供 CLI | 验收要测 diff 和 SQL，不该依赖进程退出码 |
| 把 `mist generate` 改成生成迁移 | 这个子命令已经写进 `go:generate` 和 CI |
