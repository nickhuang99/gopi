<!-- doc-class: historical -->
# Task Report: CLI 面 SQL 状态字面量 → tools.TaskStatus 导出常量单源

## Summary
cmd/agent 的 SQL 状态字面量绕过 internal/tools 已导出的 TaskStatus* 常量（tasks.go:83 起，
任务状态词的单一事实源，同位置 taskStatus* 小写常量 + SQL CHECK 约束共用）。本次把 cmd/agent
非 test 源码内全部状态字面量改为引用导出常量，消除 CLI 扫描面与引擎面的状态来源分歧。

## Completed
- `cmd/agent/cli_tasks.go:1102`（注释）：`TaskNextFiltered's UPDATE ... WHERE status='pending'`
  → `WHERE status = tools.TaskStatusPending`（注释内引用改为常量名，随文重排至 80 列内）。
- `cmd/agent/cli_tasks.go:1856` `serveFinishCleanupQuery`（finish-cleanup 扫描）：
  `WHERE status IN ('completed','failed')` → 常量串拼接
  `IN ('` + tools.TaskStatusCompleted + `','` + tools.TaskStatusFailed + `')`。
- `cmd/agent/cli_tasks.go:2221` `serveMergeExecSweep`（merge-backlog 待行扫描）：
  `AND status IN ('pending','claimed')` → 同法拼 tools.TaskStatusPending / TaskStatusClaimed。
- 行为零变化：常量值 = 原字面量（`TaskStatusPending = taskStatusPending = "pending"`），SQL 产物逐字节相同。

## Not Done
- scripts/*.sh 与 *_test.go 中的状态字面量未动 —— 属 DB 夹具/运维脚本（验收明确豁免）。
- archive/taskingest.go 已存在的 `'pending','claimed','in_progress'` 串常量
  `taskIngestMirrorableSourceStatuses` 未动 —— 历史兼容面（验收豁免），作先例参照即可。
- `cmd/agent/cli_tasks.go:963` const-block 对齐的 gofmt 差异为 master 既有问题（stash 验证
  pristine 亦点名），非本次改动引入，未顺手修（Rule: 勿修既有缩进）。

## Verification
- 字面量 grep gate：`grep -rn "'pending'\|'claimed'\|'completed'\|'failed'\|'in_progress'" cmd/agent --include='*.go' | grep -v _test.go`
  → 退出码 1（**零命中**，全清零）。
- gofmt：本次改动区域 fmt 干净（唯一 `-l` 点名 = master 既有 :963 区）。
- Build：`go build ./...` OK（worktree 内）。
- go vet ./cmd/agent/：干净。
- Tests：`go test ./cmd/agent/ -timeout 600s`（job_start 后台跑）——
  master 基线（同一 commit，未改代码）单独跑 **ok 205.901s**；worktree 改后单独跑
  **ok 140.679s**，双绿。首轮 worktree 与 master 两套件**并发**跑时
  `TestWorkerBusyRoundResetsIdleStreak` FAIL（9.58s）—— 该测试含 1.5s sleep
  竞态时序（round 判定在 drained-verdict 1s recheck 窗口内），并发 CPU 争用下 flaky，
  与本次 SQL 常量改动无关（master 单独跑绿 + worktree 单独跑绿共同证伪关联性）。
  验收相关 finish-cleanup/serve-stale/merge-exec 测试均通过（无 FAIL 行）。
- 结论：改动行为零变化（常量值 = 原字面量），两基线单独跑绿即验收达成。

## Commit
- 分支：`feat/hardcode-tasks-const`（worktree `~/workspace/gopi-hardcode-tasks-const`，
  与主仓共享对象库，commit 即达主仓该分支）。
- 恢复命令：`git show feat/hardcode-tasks-const:devdoc/review-hardcode-taskstatus-2026.md`
