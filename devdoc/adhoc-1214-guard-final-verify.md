<!-- doc-class: historical -->
# Task Report: feat/adhoc-1214-guard 收尾验证 — ambient-config 依赖测试全绿

## Completed

- **分支事实澄清（关键发现）**：Epic 简报中的 `feat/adhoc-1214-guard` 分支在 ~/work/GoPi **不存在**
  （`git log` 报 unknown revision，`git worktree list` 无对应 worktree）。实测修复 commit
  `c78668a`（test(tools): pin GOPI_SYNC_PEERS in server-resolve test）连同其上游
  `ff7ea1b`（ad-hoc 1214 guard）与 `debb099`（docs 任务报告）**已全部包含在 master**
  （`git merge-base --is-ancestor` 确认）。因此收尾验证直接在 **master** 执行，无需任何分支操作。
- **验收 1：干净 shell 跑 `go test ./internal/tools/`** — PASS。
  干净 shell 定义：`env -u GOPI_SETTINGS -u GOPI_REASONING_EFFORT -u GOPI_THINKING_BUDGET
  -u GOPI_VISION_MODEL -u GOPI_VISION_BASE_URL -u GOPI_EXECUTOR_SESSION_ID`（剥离全部 ambient
  GOPI_* 配置；剥离前后 `env | grep -c '^GOPI_'` 从 7 → 0）。
  经 job_start 后台运行 523.1s（job-1）：`ok github.com/nick/gopi/internal/tools 459.072s`。
- **验收 2：无回归三包** — PASS。同干净 shell（job-2，153.6s）：
  - `ok github.com/nick/gopi/internal/deepseek 0.064s`
  - `ok github.com/nick/gopi/internal/agent 69.927s`
  - `ok github.com/nick/gopi/internal/config (cached)`
- **验收 3：ambient-config 依赖扫描** — 复核了宿主机 `~/.gopi/settings.json` 的实际 ambient
  面（本机 peerbox：`sync_peers: [hp, peerbox]`，`compute_nodes` 含 hp/rpi5）。逐个核查依赖点：
  - `memory_sync_delta_test.go:266` TestSyncDeltaServerResolvesConflict →
    `t.Setenv(config.SyncPeersEnv, "client-node-under-test")`（c78668a 已钉）✅
  - `memory_sync_bidir_e2e_test.go:198` TestBidirEpic2 → `t.Setenv(config.SyncPeersEnv, "peer-under-test")` ✅
  - `memory_sync_delta_test.go:210` → `t.Setenv(config.SyncPeersEnv, self)` ✅
  - 全部 memory_sync 测试 DB 隔离走 `t.Setenv("GOPI_MEMORY_DB", …)` / `t.Setenv("GOPI_HOME", …)` ✅
  - `taskzombie_test.go`：hostname 只用于 ClaimedBy 戳比对（进程主动 stamp 自己，非 ambient 判定），
    通知 env（GOPI_TASK_NOTIFY_WEBHOOK/ASYNC 等）已全部 hermetic 钉死 ✅
  - hostname 泄漏面：`syncIsServerNode()`（memory_sync_delta.go:393）是唯一读 hostname+peers 的生产路径，
    其调用点已全部被上述 Setenv 钉住。
  - **结论：无新增 ambient 依赖测试需要修复；427 个 tools 测试在干净 shell 全绿。**
- **目标测试定点复跑**：`go test -run "TestSyncDeltaServerResolvesConflict|TestBidirEpic" -v` →
  5/5 PASS（含原失败点 TestSyncDeltaServerResolvesConflict 3.50s PASS）。

## Not Done

- 无。Epic 三项验收全部完成且无生产代码改动（本任务为纯验证收尾）。
- 一条记录用 task_submit 归档了分支事实（t-1788172497），属流程占位，不产生工作项。

## Verification

- Build: 源码未改，`go test` 隐含编译通过（两包均编译+跑通）
- Tests: `./internal/tools/` 427 tests PASS（459.072s）；
  `./internal/deepseek/ ./internal/agent/ ./internal/config/` 全 ok；
  定点 -run 5/5 PASS
- Commit: 无新 commit（无代码改动）；验证基线 = master @ 6e0a718

## 分支/恢复声明（Rule 6d）

- 交付物位置：**master** 分支（~/work/GoPi），验证基线 commit `6e0a718`
- 恢复命令：`cd ~/work/GoPi && git show 6e0a718:devdoc/adhoc-1214-guard-final-verify.md`
- 本报告即 devdoc（Rule 5/7）
