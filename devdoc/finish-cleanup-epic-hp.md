<!-- doc-class: normative -->
# Epic 报告: HP client 收尾四步链 + server merge 链（2026-09-01）

## Epic
HP client 收尾四步链（push→worktree remove→branch -d，dirty 跳过，豁免分支）+
server merge 链。任务域: taskregister.go / serve tick pass。

## Completed

### 1. 四步链本体 — 已由 peer1 侧实现，本次 merge 收编本地
`peer1/feat/finish-session-cleanup`（8f7e824）已含完整实现，merge 进本地分支：
- `internal/tools/task_finish_cleanup.go`（477 行）:
  `TaskFinishCleanupSessionID(taskID)` — ①守卫（terminal + deliverable.repo +
  worktree clean + `IsServerNode()` client gate）②push `<peer0>:~/work/GoPi`
  （幂等）③`git worktree remove --force`（仅②成功后）④`git branch -d`（-d 拒删
  → push 成功才允许 -D 兜底，顺序即安全论证）。豁免: master/main +
  `hp-unique-maxtokens-work` + `archive/*`；`GOPI_FINISH_CLEANUP_EXEMPT` 追加。
  fail-soft 逐步报告（`FinishCleanupStep`）。
- `internal/agent/taskregister.go` finishSessionTask 末尾钩子（session-end）。
- `cmd/agent/cli_tasks.go` `serveFinishCleanupSweep()`（serve tick pass，
  10min throttle，LIMIT 10，兜底被杀进程没跑钩子的行）。

### 2. merge 链收编（本地分支 feat/task-id-collision）
- `1cbcdd0` merge peer1/feat/finish-session-cleanup（四步链本体；冲突:
  memory_sync_task_test.go 双侧独立测试并保留）
- `186886c` merge feat/branch-backlog-merge-tasks（serve tick 分支清点
  auto-submit merge-backlog；冲突: cli_tasks.go serve pass 双方 sweep 并存）
- `4e5466f` merge peer1/feat/node-role-banner（node identity role/sync_to
  banner + briefing；冲突: memory_sync_task.go import 块 + 常量块双方并集）
- server merge 链按 devdoc/finish-session-cleanup.md 复用
  `scripts/safe-merge.sh`（✅ 守卫实测通过）+ `scripts/post-merge-cleanup.sh`
  （archive tag + branch -d），不新建 server 代码（b 项并入本单）。

### 3. 联调修复（四步链在此真实拓扑下才能跑对的前置）
- `83ed846` fix(test): bidir e2e 方向感知 sync_peers pin — server 裁决 vs
  client 保留行的判定不得依赖 ambient settings.json（388be0a 模式扩展；
  TestBidir2DivergentTerminalsConverge 在 HP 宿主必现红的根因修复）
- 236fb03/5546861（并行实例 t-1788173459 交付，本会话 park+协调）:
  IsServerNode config role 优先（bug044）+ N1 fail-safe client 默认
- `425b573` fix(test): runTaskServe wiring 测试预挂 branch-scan/finish-cleanup
  节流钟 — branch scan 用 CWD 仓库探针、ledger 在 TempDir 的跨域泄漏回归
- `4c67a63`+e3e3988+`1833a4c` config 单一 ResetNodeIdentityForTest seam +
  Role/SyncTo 双声明（string vs NodeRole）去重 — server-resolve 测试改用
  GOPI_NODE_ROLE=server 声明（236fb03 后 peers-pin 已无法伪造 server）

### 4. Epic 实测（go test -run TestFinishCleanup 6/6 PASS + 本机 gate 实证）
- ① clean+terminal → 链走通（push OK→worktree 消失→branch 删净，
  TestFinishCleanupChainCleanWorktree / IdempotentPush）
- ② dirty → skip 活 Bond 保护（TestFinishCleanupChainDirtyWorktree）
- ③ 未 push（push 失败）→ 链停在 step②，branch+worktree 完好
  （TestFinishCleanupChainPushFailKeepsBranch）
- ④ master/hp-unique-maxtokens-work/archive/* 豁免
  （TestFinishCleanupBranchExempt）+ 本机实测: HP（role=client,
  sync_to=peer1）`IsServerNode()==false` → client gate 正确放行（探针测试过）
- 全量: internal/tools + cmd/agent + internal/agent + internal/config OK

## Not Done / 归属澄清
- feat/sync-conflict-completed 分支本地已不存在（90a6315 merge 早已进
  master 系）——Epic 描述的"worktree+分支仍挂着"现状已由并行实例/后续
  merge 消化；四步链兜底逻辑（sweep）保证未来同类残留自动收尾。
- TestJobNoNoticeOnKill / TestMemorySearchNonBlocking 偶发红 = 并行实例
  干扰（失败证据含非本测试的 'sleep 1' job notice / 共享 notice 队列），
  隔离重跑均 PASS，与本改动无关。
- 并行实例 t-1788173459（同仓 15min 共存）在其间 landing 了 adoption/
  git-sync/IsServerNode 工作；本单在冲突处做并集保留，未回滚其内容。

## Verification
- Build: `CGO_ENABLED=0 go build ./...` OK（另出 /tmp/gopi-final）
- Tests: 4 包全绿（tools 53s / agent 8s / cmd 11s / config 0.05s；2 个
  既有 flaky 隔离 PASS 已归因）
- Commit: 本分支 tip `1833a4c`（关键: 1cbcdd0/186886c/4e5466f/
  83ed846/425b573/4c67a63/1833a4c）

## 交付声明（Rule 6d）
- 分支: `feat/task-id-collision`（main checkout /home/user/work/GoPi，
  worktree 共享 refs，无需 push）
- 恢复: `git -C ~/work/GoPi show feat/task-id-collision:devdoc/finish-cleanup-epic-hp.md`
