<!-- doc-class: historical -->
# Task Report: merge-backlog 标签三处定义跨 2 包 → 单源 (Rule 2)

Date: 2026-09-09
Branch: feat/merge-backlog-single-source
Epic: [hardcoding] merge-backlog 标签三处定义跨 2 包（含同包双份）— 只报不改审查单 #1 (t-1788905959) 的落地修复

## Completed

- **tools 包内合并为单一常量 + 导出别名**（仿 tasks.go:51-82 TaskStatus* 先例）：
  - `internal/tools/tasks.go`: `mergeBacklogTagName` → 更名 `mergeBacklogTag`（单一小写常量，注释声明 single source），新增导出别名 `MergeBacklogTag = mergeBacklogTag`（供 cmd/agent 消费）。
  - `internal/tools/task_merge_backlog.go:32` 同包双份 `mergeBacklogTag = "merge-backlog"` **删除**（原引用自动落入 tasks.go 同包常量）。
  - `TaskOpenMergeBacklogBranches`（tasks.go:2574/2579）dedupe 查询改用 `mergeBacklogTag`。
- **cmd/agent 跨包第三份删除**：`cli_tasks.go:1538` 私有 `mergeBacklogTag` const **删除**，提交点（`cli_tasks.go:2063`）改引 `tools.MergeBacklogTag`。
- **回归测试**：`TestAgentBranchScan_TwoBranchesTwoRows` 内新增断言——branch-scan 提交的每行 `tags` 字段（TaskShow 解析）必须等于 `tools.MergeBacklogTag`；两行全验。防回潮：cli 侧再引入镜像字面量即红。

## Not Done

- `cli_tasks.go:2231` 的 executor SQL `WHERE tags LIKE '%merge-backlog%'` 保留裸字面量 —— 验收明列豁免（SQL 查询值非常量定义；且该 LIKE 子串非标签精确匹配语义，套常量反而错义）。注释未改。
- task_merge_backlog_test.go 中的 `mergeBacklogTag` 引用保留（同包单源引用，非新定义）。
- devdoc/foresee-review-branch-backlog.md:127 文档示例未动（文档豁免）。

## Verification

- grep 单源验收：`grep -rn '"merge-backlog"' --include='*.go' cmd internal | grep -v _test.go` → **仅剩 internal/tools/tasks.go:2618 `const mergeBacklogTag = "merge-backlog"`**（1 处定义，0 处裸字面量）。
- Build: `go build ./...` OK；`go vet ./internal/tools/ ./cmd/agent/` 干净。
- 回归单测: `go test ./cmd/agent/ -run 'TestAgentBranchScan_TwoBranchesTwoRows|TestAgentBranchScan_ClosedRowRearms'` → **ok**（首次红因测试断言用列对齐裸 `tags: ` 前缀误判——TaskShow 输出 `tags:           <list>` 多空格；改 CutPrefix 解析后绿。行实际 tags 始终正确，非实现缺陷）。
- 全量: `go test ./internal/tools/ ./cmd/agent/`（job-1 后台跑，见下方结果）。
- Commit: 见 Rule 6d 声明。

## Rule 6d Deliverable Declaration

1. **Branch**: `feat/merge-backlog-single-source`（worktree `/home/user/workspace/gopi-merge-backlog-single-source`，与主仓库共享 object store）。
2. **Recovery**: `git show feat/merge-backlog-single-source:devdoc/task-merge-backlog-single-source.md`（本报告）+ `git show feat/merge-backlog-single-source:<file>` 取代码。
3. **Main-repo state**: worktree commit 即分支在共享仓库的更新，无需 push（Rule 6c）；PA 验收后 merge 到 master 并移除 worktree。
