<!-- doc-class: historical -->
# Task Report: tasks 台账表 + gopi task CLI（task-as-memory）

日期: 2026-08-28
分支: feat/tasks-ledger（worktree: ~/workspace/gopi-tasks-ledger）
设计简报: ~/work/hermes-agent/devdoc/memory-catchup-brief.md（A/B/D/E 段）

## Completed

- **tasks 表**（`internal/tools/tasks.go`，ensureSchema 同款模式：硬编码 SQL + CREATE IF NOT EXISTS 幂等自愈 + PRAGMA 探测缺列动态 ALTER）：
  - 列：`id INTEGER PK AUTOINCREMENT / task_id TEXT NOT NULL UNIQUE / summary TEXT NOT NULL / status TEXT NOT NULL DEFAULT 'in_progress' CHECK(status IN ('in_progress','completed','failed','cancelled')) / session_id TEXT DEFAULT '' / created_at REAL NOT NULL / updated_at REAL NOT NULL / ended_at REAL（可空）`
  - 索引：`idx_tasks_status(status)`、`idx_tasks_task_id(task_id)` —— status 列第一天进查询（--status 过滤走索引，value-signal 教训：排序列不做死列）
  - WAL + busy_timeout 5s + SetMaxOpenConns(1) 沿用 openDB 现有配置
  - 生命周期：terminal 状态（completed/failed/cancelled）UPDATE 时盖章 ended_at；回退 in_progress 清空；仅改摘要不动 ended_at
- **引擎函数**（CLI 与未来 harness 共用，零重渲染）：
  - `TaskNew(summary, taskID, sessionID)` —— task_id 缺省自动生成 `t-<unix秒>`，碰撞加 `-N` 后缀（≤10 次）；显式 task_id 重复 → 报错不静默改写
  - `TaskList(statusFilter)` —— 新→旧（created_at DESC, id DESC），空过滤=全部，非法 status → 报错
  - `TaskShow(taskID)` —— 单条全字段详情；缺失 → 报错 exit 1（code-search miss 语义）
  - `TaskUpdate(taskID, status, summary)` —— 至少一项；非法 status 拒绝；**无 delete 路径**（append-only，nick 铁则）
- **CLI 子命令族** `gopi task`（`cmd/agent/cli_tasks.go`，模板=cli_memory.go 惯例：包级 const usage / 手写 flagset `--k v` 与 `--k=v` 双形态 / -h→usage+exit 0 / 缺参→stderr+exit 1 / --db→GOPI_MEMORY_DB）：
  - `gopi task list [--status X] [--db PATH]`
  - `gopi task show <task_id> [--db PATH]`
  - `gopi task new "<summary>" [--task-id X] [--db PATH]`
  - `gopi task update <task_id> [--status X] [--summary "Y"] [--db PATH]`
  - 逐子命令 flag 白名单（taskFlagMember，跨子命令 flag 拒绝，镜像 memberOf 严格校验）
  - main.go dispatch 链注册 `isTasksSubcommand`（在 top-level -h scan 之前）
- **FormatUsage 注册**（`internal/config/help.go` Subcommands 段 + `usage_test.go` 防回归断言 TestTopLevelUsageShowsTaskLedger + 已有 `gopi help` 裸词别名覆盖）
- **schema 挂载点**：`initKnowledgeBase`（新库首建）+ `migrateDefaultTriggers`（默认库迁移钩子：pre-tasks 的 ~/.gopi/memory.db 首次打开即补表，零迁移脚本）
- **外库保护**：`openTasksDB` 拒绝在 foreign distilled DB 建表 —— 检查 gopi 专属 memory_feedback 表存在性（hermes 生产库有 distilled_qa 无 memory_feedback → 拒绝，绝不 CREATE TABLE 进外库，extend "never mutate foreign DBs" 从行级到 schema 级）

## Not Done（诚实声明）

- **`gopi log --task` 过滤族：跳过**。理由：toolFamilies 机制按 session JSONL 的 tool_use 事件名过滤，而 `gopi task` 是 CLI 子命令不是 LLM 工具（Rule 3 工具最小化——本设计从头就不暴露 task 工具），session 里不存在 task 工具调用事件，加 `--task` 族 = 匹配零事件 = 死过滤，直接违反 memory-lowquality 教训（"不做只展示不生效的死列"）。任务相关内容检索已有 `--grep`/`--filter` 覆盖，[TASK_LIST] 块在 assistant 文本层，非 tool_use。若未来 harness 自动登记任务（session_start/end 钩子）再补。
- **memory_feedback 的 task_id 聚合展示**：本期不做（简报 E 段明确"留未来"）。
- **loop.go 运行时集成**：刻意不做（Stateless 哲学——运行时任务状态仍 LLM 每轮携带，tasks 表只做持久化记录层/审计，绝不碰 tasklist/todoState/心跳/nudge）。

## 测试结果

- `go test ./...`：9/9 包 PASS（见 Verification）
- 新增 `internal/tools/tasks_test.go`（11 个）：auto-ID 生成 / 显式 ID + 重复拒绝 / 空 summary 拒绝 / list 全量+status 过滤+新→旧序 / 非法 status 拒绝 / show 详情+缺失报错 / 生命周期 ended_at 盖章与清除 / 仅改摘要不动 ended_at / update 三错误路径 / **外库拒绝**（模拟 hermes 形状：distilled_qa 无 memory_feedback）
- 新增 `cmd/agent/cli_tasks_test.go`（8 个）：四子命令 parse（含 `--k=v` 双形态）/ 8 组错误路径（未知 flag、跨子命令 flag、多 positionals、空 update）/ usage 文本无 delete 广告 + delete verb 解析拒绝 / 全流程 E2E（new→list→update completed→show→status 过滤，走共享引擎函数）
- `usage_test.go`：TestTopLevelUsageListsSubcommands 加 "gopi task" + 新增 TestTopLevelUsageShowsTaskLedger
- 既有 memory/FTS/feedback 测试无回归（全量跑过）

## 手工 E2E（临时 GOPI_MEMORY_DB，未碰真实库）

```bash
$ export GOPI_MEMORY_DB=/tmp/gopi-tasks-e2e.db
$ ./bin/gopi task new "测试 tasks 台账 CLI" --task-id t-e2e-001
Task created: t-e2e-001 (status=in_progress)
$ ./bin/gopi task list
1 task(s):
#1  t-e2e-001  [in_progress]  测试 tasks 台账 CLI  (created 2026-08-28 14:0x:xx)
$ ./bin/gopi task update t-e2e-001 --status completed
Task updated: t-e2e-001 (status=completed)
$ ./bin/gopi task show t-e2e-001
task_id:    t-e2e-001
status:     completed
summary:    测试 tasks 台账 CLI
session_id:
created_at: 2026-08-28 14:0x:xx
updated_at: 2026-08-28 14:0x:xx
ended_at:   2026-08-28 14:0x:xx
$ ./bin/gopi task list --status in_progress
No tasks found.
$ ./bin/gopi task delete t-e2e-001   # 无 delete 子命令 → usage + exit 1
```

验证落盘：`sqlite3 /tmp/gopi-tasks-e2e.db '.tables'` → tasks / distilled_qa / memory_feedback 三表齐；`SELECT * FROM tasks` 行存在。

## 关键设计决策

1. **服务定位=审计追溯**：tasks 表是持久化记录层，不是运行时控制（Stateless 哲学不倒）。CLI 子命令不算新 LLM 工具（Rule 3 先例：gopi log/checkpoint）。
2. **append-only**：无 delete 子命令、无 delete 引擎函数、CHECK 约束兜底 —— nick 铁则 "LLM 仅能新增/改摘要/改状态，任何场景不能删除；记录永久留存用于追溯"。
3. **外库保护**：tasks 只落 gopi 自己的 memory.db（默认路径或 gopi-owned --db 文件）。hermes 生产库（distilled_qa 无 memory_feedback）拒绝打开写 —— 绝不 CREATE TABLE 进外库。
4. **value-signal 第一天生效**：status 过滤走 idx_tasks_status；排序 = created_at DESC。memory-lowquality 根因教训（排序列 inert）直接规避。
5. **session_id 列预留**：CLI 暂不暴露 --session-id（spec 最小面），引擎函数已接受参数，harness 未来自动登记可用。

## 变更文件

- `internal/tools/tasks.go`（新，引擎 + schema）
- `internal/tools/tasks_test.go`（新）
- `internal/tools/memory.go`（initKnowledgeBase + migrateDefaultTriggers 挂 ensureTasksSchema）
- `cmd/agent/cli_tasks.go`（新，CLI 子命令族）
- `cmd/agent/cli_tasks_test.go`（新）
- `cmd/agent/main.go`（dispatch 链）
- `cmd/agent/usage_test.go`（防回归断言）
- `internal/config/help.go`（FormatUsage Subcommands）
- `devdoc/tasks-ledger-impl-report.md`（本文件）

## Verification

- Build: `go build ./...` OK（CGO_ENABLED=0 静态单二进制惯例未破）
- Tests: `go test ./...` 9/9 包 PASS
- Commit: 见下方 commit hash（push 后报告）
- Branch: `feat/tasks-ledger`（worktree 共享主仓库 refs，即已在主仓库）

## 交付物声明（Rule 6d）

- 分支名：`feat/tasks-ledger`
- 恢复命令：`git show feat/tasks-ledger:devdoc/tasks-ledger-impl-report.md`
- 主仓库确认：worktree 提交即入主仓库 refs（~/work/GoPi），master 未动，未 merge（PA 验收后处理）
