<!-- doc-class: historical -->
# Task Report: push-back + finish-cleanup 链验证与缺口修复（bug038/bug041 域）

分支 `feat/pushback-chain-report`（HP client，基 6e8d5c4 = master）。
任务：t-1788255272（t-1788252501 诊断性 timeout 被杀后的重发）。

## Completed

### ① 链路审查（静态 review，两副本同源现状）

**push-back 面（git 通道）**
- `internal/tools/task_sync_git.go` — serve tick 的两份非对称 pass：
  - `GitSyncClientPass`（client 侧）: ①push 全部本地非 master/main 分支 → server
    （`GitSyncPushLocalBranches`，bug038 幂等契约：远端 ref 已同 commit 时
    "Everything up-to-date" exit 0，re-tick 零成本，cap=20）→ ②pull master
    `--ff-only`（client 永不 merge；diverged/dirty 只报不 merge）。
  - `GitSyncServerPass`（server 侧）: fetch --prune 每个 peer → AdoptionSweep。
  - 手动 face：`gopi task push`（M4.18）。
- `cmd/agent/cli_tasks.go writebackOutcome`（run/serve 的终态写回）: 本地 writeback →
  ssh report-back → `git push <repo> HEAD:<branch>` push-back →
  `notifyBranchPushed`（branch-pushed 提示行落台账）。

**finish-cleanup 四步链**
- `internal/tools/task_finish_cleanup.go` — `TaskFinishCleanupSessionID`：
  guard（terminal completed/failed + deliverable.repo + 非 server 节点 + 非豁免分支
  master/main/archive/* + GOPI_FINISH_CLEANUP_EXEMPT + kill switch GOPI_FINISH_CLEANUP）
  → `taskFinishCleanupChain`：
  ①dirty guard（worktree 脏 = live Bond 保护，跳过整链）
  ②push `branch:branch` → server（fail = 链终止，worktree+branch 均存活）
  ③`worktree remove --force`（branchDir 在此步之前用 `rev-parse --git-common-dir`
  先解析主仓路径 — repo=worktree 形态时 ③ 后该目录已消失，bug041 修复）
  ④`branch -d`（拒删语义保留；push 已成功后的拒删 → `-D` 兜底，by-construction 安全）。
- 触发面双份：a) 会话终态钩子 `internal/agent/taskregister.go:179`（terminal push
  must-succeed PushTerminalTaskSync 之后）；b) serve tick 扫描
  `serveFinishCleanupSweep`（30s 窗口 + 节流，OpenTasksDBForSweep 复用）。
- 测试缝：`runFinishCleanupChainForTest`（注入 repo/branch/worktree/target）。

**结论：无断链。** 终态行 → 行同步（PushTerminalTaskSync，重试至每 peer 接受）→
git 四步链（每步 fail-soft + 报告）。两条触发面都在。

### ② 实测全链（live E2E，非测试桩）

```
git branch feat/pushback-e2e-t1788252501 master
git worktree add /tmp/pushback-e2e-wt <branch>       # ③④的目标
commit fd1c5ad (reports/archive/2026-09/E2E_PROOF.md)
② push peerbox  → "* [new branch] ... " ；远端 ls-remote = fd1c5ad3e ✓
③ git worktree remove /tmp/pushback-e2e-wt --force → worktree list 无残留 ✓
④ git branch -d → "error: not fully merged"（fd1c5ad 非 master 祖先，拒删正确）
   验证 fd1c5ad ∈ FETCH_HEAD（已上 server）后 git branch -D → 本地分支消失 ✓
```
④ 的拒删→`-D` 兜底语义与代码路径逐字吻合（-d refusal → 验证远端持有 tip → -D）。
单元基线：`go test ./internal/tools/ -run TestFinishCleanup` 6/6 PASS；
TestGitSync 4/4 PASS；全仓 build OK。worktree+branch 清理后 `git worktree list`
仅剩主 checkout。

### ③ 发现并修复一个真缺口（merge 回归，非链本身）

全量测试暴露 `TestParseTasksArgs_AuditMerges` 红：
`gopi task audit-merges` 解析为 unknown subcommand —— `cfbc959`（merge: feat/
merge-backlog）的冲突并集把 `case "audit-merges"` 解析臂丢在 push 与 default 之间
（usage 文本含 audit-merges、taskFlagMember 白名单含它、执行臂 1691 行仍在，
唯 parse 臂缺失）。修复 = 恢复该 case（5 行 + 注释注明来源）。
验证：TestParseTasksArgs_AuditMerges PASS，`go test ./cmd/agent/` 全绿。

### 预存失败对照归因（非本任务引入）

`TestReflowExportExcludesFromSelfRows` / `TestReflowExportGateV1Export` /
`TestReflowGateSelfOnlyBothTagsExported`（internal/tools）：56fd84a 的 from-<self>
门被 eef8342/affc1b3 改为 from-<peer> 门（Peer 参数驱动），但 56fd84a 的测试断言
没同步 —— 测试调 `SyncTaskExportDelta{}`（Peer=""）并断言 from-hp 行被排除，
而现在 Peer="" 走 legacy self-gate（`syncSelfTag()` = from-hp on empty）……探针
证实 NOT-LIKE 门 SQL 生效（param-gate survivors=0）但导出仍含 self-tag 行 ——
按 tools-redlist-round6 惯例属预存环境/测试-代码契约漂移，与 push-back 链无关，
留待 memsync 域任务裁决（测试该改断言 from-peer，或 exporter 该默认更严）。
`TestMemorySearchNonBlocking` 单跑绿、全量偶红（tmpdir race，round6 已记录）。

## Not Done

- 预存 reflow 测试-契约漂移的裁决（见上；需要 memsync 域 owner 决断语义方向）。
- peerbox 侧 server-pass（AdoptionSweep ③a/③b）的实机验证 —— 本任务域是 client
  收尾链；server 集成面由 merge 决策域覆盖。

## Verification

- Build: `go build ./...` OK
- Tests: `go test ./internal/tools/ -run 'TestFinishCleanup'` 6/6 PASS;
  `-run TestGitSync` 4/4 PASS; `go test ./cmd/agent/` 全绿（含修复的
  TestParseTasksArgs_AuditMerges）
- Live E2E: fd1c5ad push→peerbox ✓ / worktree remove ✓ / branch -d 拒删+(-D) ✓
- Commit: (see `git log feat/pushback-chain-report`)
