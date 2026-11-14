<!-- doc-class: historical -->
# Task Report: branch-scan + merge-backlog audit 归档可达性过滤（幻影行根治）

Epic: HP client 2026-09-01 会话 9e6a507c 关闭 hp/master merge-backlog 幻影行
（devdoc/hp-master-merge-decision.md, commit 859db09）。幻影根因之一：serve
branch-scan 用 `git branch --no-merged master` / `git branch -r --no-merged master`
清点，不区分已被 archive tag 归档的败者分支 → 归档分支反复被扫入并重新提交
merge-backlog 行（closed row re-arm 语义放大，75 行台账噪音）。

## Completed

### 1. branch-scan 侧（cmd/agent/cli_tasks.go — Epic 指定主目标）
- 新增 `archiveTagTips()`：每次 scan pass 一次
  `git for-each-ref refs/tags/archive/ --format=%(refname:short)`，失败降级
  （stderr 一行 + nil → 过滤退化为 no-op，扫描绝不因此失明）。
- 新增 `isBranchArchived(branch, tags)`：逐分支
  `git merge-base --is-ancestor <tag> <tip>`，exit 0 = tag 可达 tip = 已归档败者，
  跳过不提交。逐分支一次 rev-list 代价，受 serveBranchScanThrottleMin（10min）
  节流；每次探测受 gitBranchListTimeout（10s）封顶。
- `listUnmergedBranches()` 接入过滤：HEAD 过滤后、追加 out 前
  `if isBranchArchived(...) { continue }`。
- Rule 1 常量：`archiveTagRefPrefix`（"refs/tags/archive/"）、
  `archiveTagShortPrefix`、`archiveTagScanTimeout`（5s）。tag 前缀语义镜像
  internal/tools/task_finish_cleanup.go 的 finishCleanupArchiveTagPrefix，
  不跨包导出（注释注明）。
- 与 dedupe 语义分层：dedupe（TaskOpenMergeBacklogBranches）管 open row 掩蔽；
  本过滤管"分支已归档，无需行"。两层互不替代。

### 2. audit 侧（internal/tools/task_merge_backlog.go — E2E 实测发现的第二行源）
真实 serve 验证时发现：归档败者分支仍被 serveMergeBacklogAudit
（AuditMergeBacklog，"merge <branch> into master" 行）反复提交——同一幻影根因
的第二条路径（ahead≥1 且无 dedupe 行覆盖）。Epic 根治目标覆盖两条提交路径，
故同修：
- 新增 `archiveTipReachable(repo, tag, tip)`（merge-base --is-ancestor）+ 
  `archiveTagAncestry(repo, branches)`（for-each-ref 一次 + 命中即短路；空
  namespace / 列举失败 → 原样返回，退化 no-op）。
- `AuditMergeBacklog` 在 discover 后、dedupe/submit 前接入
  `branches = archiveTagAncestry(...)`。
- Rule 1 常量 `archiveTagRefPrefix`（包内独立声明，与 cmd/agent 侧互不依赖）。
- Discover test seam 保持不变（过滤作用于 seam 返回值之后，真实 git 路径在
  单测里用真实 sandbox repo 驱动）。

### 3. 单元测试（两处）
cmd/agent/cli_branch_scan_test.go：
- `TestAgentBranchScan_ArchivedLoserSkipped`：archive tag 可达的败者不提交 +
  同仓普通 unmerged 分支仍提交（1 行且命名 live 分支）。
- `TestListUnmergedBranches_ArchiveTagUnreachableNotFiltered`：tag 不可达
  （败者 tip 在分叉侧，独立分支不含 tag 提交）→ 独立分支存活。几何注释：
  ancestry 方向 tag→tip，过滤只命中"包含归档提交"的分支。
- `TestIsBranchArchived_NoTagsDegradedNoOp`：nil/空 tag 集合 → not-archived。
- `TestArchiveTagTips_ListsArchiveNamespace`：short-form 只列 archive/*。
internal/tools/task_merge_backlog_test.go：
- `TestAuditMergeBacklog_ArchivedLoserNotSubmitted`：真实 sandbox repo，
  败者被过滤、live 分支恰好 1 行且 summary 命名 live 分支。
- `TestArchiveTagAncestry_DegradedNoOp`：空 namespace → no-op。

### 4. 测试期间发现并修正的认知错误（根因习惯 Rule 10.3/10.4）
初版 "archive tag 不可达" 测试把 tag 打在 master 上——分支从 master 分叉即
包含 tag 提交，按 `--is-ancestor <tag> <tip>` 语义必被过滤（测试红）。根因
不是实现错误而是 fixture 几何错误：归档约定 tag 指败者的分叉 tip（败者永不
被 merge），"不可达" fixture 应为独立分叉分支。已修正 fixture 并把几何注释
写进测试。

## Not Done
- `git branch -r --no-merged master` 的远端分支（peerbox/*）同样被过滤覆盖
  （isBranchArchived 对 remote-tracking 名同样工作），但未为远端专属路径
  单独加 fixture 场景（本地 fixture 已覆盖 merge-base 语义；远端分支与本地
  分支在 git 层无差别）。
- cmd/agent 与 internal/tools 各持一份 archiveTagRefPrefix 常量（有意为之：
  导出会引入 cmd→internal API 面；两处常量注释互相指认）。若未来出现第三个
  使用方，再上移到共享包。
- scripts/e2e-merge-backlog.sh 未扩展（手写 sandbox fixture 已覆盖验收 4）。

## Verification
- Build: `go build ./...` OK；`go vet ./cmd/agent/ ./internal/tools/` OK
- Build(binary): `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` OK
- Tests:
  - `go test ./cmd/agent/ -run 'TestAgentBranchScan|TestListUnmergedBranches|TestIsBranchArchived|TestArchiveTagTips' -count=1` → ok（9 场景含既有回归）
  - `go test ./internal/tools/ -run 'TestAuditMergeBacklog|TestArchiveTagAncestry' -count=1` → ok
- Real-serve E2E（sandbox repo：master + 归档败者 hp-unique-maxtokens-work
  （tag archive/hp-unique-maxtokens-20260901）+ feat/e2e-live）：
  修复前 serve --once 提交 2 行（branch-scan 1 + audit 1，audit 行点名败者
  "merge hp-unique-maxtokens-work into master" pending）；修复后败者行 0、
  live 行 2（branch-scan + audit 各一行，均只命名 feat/e2e-live）。
- Commit: 见 git log（逐文件 add：cmd/agent/cli_tasks.go,
  cmd/agent/cli_branch_scan_test.go, internal/tools/task_merge_backlog.go,
  internal/tools/task_merge_backlog_test.go, devdoc/branch-scan-archive-filter.md）

## 收尾补充（branch 最终状态）

- 分支基线修正：初建 worktree 基于 859db09，但真实 master 线上缺两块——
  a) afe50fd（parseTasksArgs 恢复 audit-merges parse arm，TestParseTasksArgs_AuditMerges
  在 859db09 上必红，已 cherry-pick）；b) peerbox/master 16c391a（post-receive
  钩子事件驱动 audit-merges —— 恰是本过滤必须覆盖的 audit 面）。分支已合并
  16c391a：`merge peerbox/master` → 分支 tip 3da6409 之后。
- 全量套件与干净 859db09 基线对照：失败集相同（TestMemorySearchNonBlocking /
  TestReflowExport* 环境性，见上 Not Done；TestJobNoNoticeOnKill 为并发 flake，
  单跑 pass），本分支零新增失败。
- git log 链：3da6409(fix) ← 995c062(cherry-pick afe50fd) ← 859db09 + 16c391a。
- safe-merge.sh 验证：分支仍被本 worktree 占用 → 守卫按设计拒绝（Rule 9 前置
  守卫工作正常）；PA merge 时应先 remove worktree 再 merge。

## Deliverable 声明（Rule 6d）

1. 分支：feat/branch-scan-archive-filter（tip 含 3da6409）
2. 恢复命令：git show feat/branch-scan-archive-filter:devdoc/branch-scan-archive-filter.md
   （worktree 共享主仓 refs，commit 已在 /home/user/work/GoPi 可见）
3. 主仓确认：git log --oneline feat/branch-scan-archive-filter -1 → 3da6409
