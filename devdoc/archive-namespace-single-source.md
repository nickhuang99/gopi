<!-- doc-class: historical -->
# Task Report: archive 标签命名空间跨包镜像 → 单源 (Rule 2)

Date: 2026-09-09
Branch: `feat/archive-namespace-single-source`
Epic: [hardcoding] archive 标签命名空间跨包镜像（注释自认重复）— 只报不改审查单落地修复

## Completed

- **tools 包内定义单一来源 + 导出别名**（仿 `tasks.go` TaskStatus* 先例 + aa517b1 MergeBacklogTag）：
  - `internal/tools/tasks.go`: 新增 `archiveTagRefPrefix = "refs/tags/archive/"` 与 `archiveBranchPrefix = "archive/"`（注释声明 single source，Rule 2），以及导出别名 `ArchiveTagRefPrefix` / `ArchiveBranchPrefix`（供 cmd/agent 消费）。
- **3 处镜像常量删除**：
  - `internal/tools/task_merge_backlog.go:43` 私有 `archiveTagRefPrefix` 删除（引用落入 tasks.go 同包常量）。
  - `internal/tools/task_finish_cleanup.go:64` `finishCleanupArchiveTagPrefix` 删除，`finishCleanupBranchExempt` 改引 `archiveBranchPrefix`。
  - `cmd/agent/cli_tasks.go:1558/1561` 私有 `archiveTagRefPrefix` + `archiveTagShortPrefix` 删除（后者本就 dead code），`archiveTagTips` 改引 `tools.ArchiveTagRefPrefix`。注释明言的自认重复（"kept local to avoid a cross-package dependency"）随之消除。
- **inline 字面量迁移**（原本 5 处散落的 `"archive/"+branch`）：
  - `repo_sweep.go` `repoSweepArchiveExempt`（merger sweep 豁免）
  - `repo_resolve_row.go` `repoSweepArchiveBranch`（归档打 tag）
  - `task_deliverable_branch.go` `taskBranchExempt`（deliverable 分支豁免）
  - `task_merge_executor.go` `mergeExecSafeMergeBlock`（safe-merge 守卫的 a) archive-tag 检查）
- **safe-merge 守卫黑名单成员改为推导**：`mergeExecBlacklistPrefixes = "loser/," + archiveBranchPrefix` —— safe-merge 守卫与 finish-cleanup 豁免共用同一常量来源，永不再漂移（验收要求）。
- **回归测试**（`task_merged_cleanup_test.go`）：
  - `TestArchiveNamespace_SingleSource`: 断言 `archiveBranchPrefix`/`archiveTagRefPrefix` 值、导出别名同源、finish-cleanup 豁免 archive/ 分支、safe-merge 黑名单含 archive/ 成员（防守卫与豁免漂移）。
  - `TestArchiveNamespace_MirrorLiteralGate`: grep-gate 扫描 internal/tools + cmd/agent 全部生产 Go 文件，`"refs/tags/archive/"` / `"archive/"` 字面量只允许出现在 `internal/tools/tasks.go` —— 防回潮（aa517b1 单字面量门禁的同构）。

## Not Done

- `scripts/safe-merge.sh` 内 `BAD_PREFIXES=("loser/" "archive/")` 与 `git tag -l "archive/$BRANCH"` 未动 —— shell 守卫是独立协议实现（Go 镜像已收敛），改 shell 引用 Go 常量不现实；文档/devdoc 中的字面量豁免。
- 测试文件中的 `"archive/..."` 字符串（fixture tag 名等）豁免 —— 测试数据非常量定义。

## Verification

- **grep 单源验收**：`grep -rn '"refs/tags/archive/"\|"archive/"' internal/tools/*.go cmd/agent/*.go | grep -v _test.go` → **仅剩 internal/tools/tasks.go:2652/2653**（1 处定义，0 处镜像）。
- Build: `go build ./...` OK；`go vet ./internal/tools/ ./cmd/agent/` 干净。
- 回归单测: `go test ./internal/tools/ -run 'TestArchiveNamespace|TestMergedCleanupExemptBranches'` → ok；`go test ./internal/tools/ -run 'TestMergeExec|TestMergedCleanup|TestFinishCleanup|TestMergerRepoSweep|TestBranchDeliverable|TestRepoResolve'` → ok (21.7s)；`go test ./cmd/agent/ -run 'TestAgentBranchScan|TestArchiveTagTips'` → ok (2.0s)。
- 全量（master 上逐包验证）: `go test ./internal/tools/` → **ok (419.9s)**；`go test ./cmd/agent/` → **ok (140.1s)**。注: 首次合并同跑时 cmd/agent 出现一次已知 singlenode flake（`TestRunWorkerClaimLocalOnlyWithNoPeers` 类, devdoc/bugfix-taskrun-briefing-summary-payload-2026-09-03.md 已记录 "flaked once in a full run (passes 3× isolated)"）；该测试与本改动无关（未触碰 singlenode 路径），隔离运行 PASS (3.85s)，两包独立全量均绿。
- Commit: `d6acfff`（见 Rule 6d 声明）。

## Rule 6d Deliverable Declaration

1. **Branch**: `feat/archive-namespace-single-source`（worktree `/home/user/workspace/gopi-archive-ns`，与主仓库共享 object store）。
2. **Recovery**: `git show feat/archive-namespace-single-source:devdoc/archive-namespace-single-source.md`（本报告）+ `git show feat/archive-namespace-single-source:<file>` 取代码。
3. **Main-repo state**: worktree commit 即分支在共享仓库的更新，无需 push（Rule 6c）；PA 验收后 merge 到 master 并移除 worktree。
