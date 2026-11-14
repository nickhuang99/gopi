<!-- doc-class: historical -->
# Task Report: 分支坟场 → 台账任务 (branch-backlog)

## Completed
- **① HP push-back → branch-pushed 提示任务** (`cmd/agent/cli_tasks.go`):
  `writebackOutcome` push-back 成功后调用 `notifyBranchPushed(branch, repo)` —
  本地台账立即落一条 `branch-pushed` 标签的提示任务（summary:
  `branch-pushed: <branch> awaits merge decision (pushed to <repo>)`），
  payload 写明 merge 决定权在 verify/merge 闭环、左置冲突面放大（P2.5 教训）。
  best-effort：submit 失败只打日志，push 本身已成功不受影响。
- **② serve tick 分支清点 → 自动 submit merge-backlog 任务**:
  `runTaskServe` 每个 pass 在 stale sweep 之后跑 `serveBranchScan()`
  （10min throttle，同 stale 模式；`--no-branch-scan` 逃生门）。
  `listUnmergedBranches` 用 `git branch --no-merged master` +
  `git branch -r --no-merged master`（HP push-back 落在 peerbox/* remote），
  过滤 `HEAD`/`*/HEAD` 别名。每个 ledger 未跟踪的分支自动 submit 一条
  `merge-backlog` 任务（priority=2 — merge 腐烂快，仅次于 bug）。
- **Dedupe / re-arm 语义**: `tools.TaskOpenMergeBacklogBranches()` 查询
  pending/in_progress 且 tags 含 merge-backlog 的行，按 summary 约定键
  `merge-backlog: <branch> ...` 提取已跟踪分支。关闭行（决策已记录）后
  分支仍未合并 → 后续 tick 重新暴露（同 stale sweep re-arm）。
- **测试** (`cmd/agent/cli_branch_scan_test.go`, 5 个测试全 PASS):
  - `TestNotifyBranchPushed_RecordsHintTask` — push-back → 台账出现 hint 行
  - `TestAgentBranchScan_TwoBranchesTwoRows` — **Epic 实测: 造 2 分支 → 台账出现恰好 2 条 merge-backlog 任务**；第二次 pass dedupe-silent
  - `TestAgentBranchScan_ClosedRowRearms` — 关闭行后 re-arm 重新提交
  - `TestServeBranchScanThrottle` — 10min throttle no-op
  - `TestListUnmergedBranches_FiltersRemoteHEAD` — HEAD 别名过滤

## Not Done
- `branch-pushed` hint 行与 merge-backlog 行目前是**两种独立行**，没有
  交叉引用（hint 行 tag=branch-pushed，branch-scan 不认它为已跟踪）。
  若 HP 与 serve 都报同一分支，会出现两条行（一条 hint 一条 backlog）。
  可后续统一：branch-scan 把 branch-pushed 行也视为已跟踪。
- `dedupeRecentTaskLimit` 常量暂未用（当时考虑 submit 前 grep 最近 N 行
  去重，最终用 SQL face 取代）；下次触碰此文件时可清理。
- merge-backlog 行由谁执行（PA 验收 merge / serve claim）未强制 ——
  payload 已写明决策路径（safe-merge.sh / archive），执行选择留给运行时。

## Verification
- Build: `go build ./...` → OK
- Tests: `go test ./cmd/agent/` 全 PASS（含新 5 个 branch-backlog 测试）；
  `go test ./internal/tools/` 仅 `TestEnsureTagsRegeneratesForNewerTS`
  失败 — **预存在**（git stash 干净 HEAD 上同样 FAIL，环境性，非本次引入）
- Commit: `d853c1d feat(branch-backlog): push-back records branch-pushed hint + serve tick auto-submits merge-backlog tasks`（分支 feat/branch-backlog-merge-tasks，worktree ~/workspace/gopi-branch-backlog）
