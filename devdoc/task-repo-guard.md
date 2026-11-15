<!-- doc-class: normative -->
# Task queue FIFO + priority + repo guardrail (the operator 三问)

**Date:** 2026-02-26 · **Branch:** feat/task-id-collision · **Files:** internal/tools/tasks.go, internal/tools/tasks_repo_guard_test.go, cmd/agent/cli_tasks.go, internal/agent/loop.go, devdoc/task-repo-guard.md

## Context

the operator 三问：FIFO/FILO? latest 饿死 oldest? vcmi 跨项目任务无 repo 无优先级?
诊断前两项已被 **M4.11 (308014a, t-1788087991) 解决**：TaskNextFiltered 显式 `ORDER BY COALESCE(priority,5) ASC, created_at ASC, id ASC`（FIFO + priority=1 插队），list 显示 `created_at DESC` 是设计非执行序。

## What was done this task (③ repo 护栏)

1. **`tasks.go` 新增两个导出函数**（紧邻 TaskNextFiltered，vcmi 类灾难预防——payload 无 repo 且 summary 提到非 gopi 路径的跨项目单，领取时先警告）：
   - `taskGuardBannedTokens = []string{"vcmi", "evennia"}` — 已知非本仓项目词表。
   - `TaskRepoGuardWarnings(t *Task) []string` — 纯判定：`deliverable` 无 `"repo"` key 且 summary/payload 含 banned token → 建议句。跨包复用。
   - `TaskRepoGuardWarn(taskID string) []string` — `taskByID` 取行后调上面。
2. **`cmd/agent/cli_tasks.go` runTaskRun**：claim 后 TaskStart 前打印 guard 行（`WARN task <id> has no deliverable.repo …execute in the gopi repo only`）——**警告不拒领**，护栏不拦正常执行。
3. **`internal/agent/loop.go` task_submit 工具描述**：`when needed` 追加 "prefer a repo (deliverable.repo) when the work targets another repo"。
4. **测试 `internal/tools/tasks_repo_guard_test.go`**：5 用例（无 repo+含 token→警；有 repo→不警；无关 summary→不警；NULL deliverable→警；banned token 在 payload→警）。banned tokens 用局部 var 影子以便测试隔离。
5. **存量 vcmi 单 t-1788062477**：本就是 cancelled → 语义已处置，无库变更。

## Key decisions

- **警告 ≠ 拒领**：拒领会拦掉合法的本地单（如改名前的 memory_sync 单）；完整 deliverable 结构化是 M4.9 后续。
- Guard 判定放 internal/tools（导出）而非 cmd/agent：单测可注入行、不依赖 cli。
- Summary 同时检查 banned token 和无 repo：guard 纯字段级，不读文件系统。

## Verification

`go test ./internal/tools/ -run 'TaskRepoGuard' -v` → 5/5 PASS；
`go build ./...` 全仓编译通过。

## Related

- 1164 (TaskNextFiltered WHERE 拼接 / empty-payload skip 哲学——guard 同层)
- 1103 (tasks.go 函数化 schema 模式)
