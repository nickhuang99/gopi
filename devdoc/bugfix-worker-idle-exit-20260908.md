<!-- doc-class: historical -->
# Task Report: Worker 无限 idle 等待修复（idle-exit）+ worktree 移除时机验证

## Summary

> **WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md** — the operator owns this; an agent must never run it (submit the work instead: `gopi task submit`).

Bug: `gopi worker`（无 --once）队列空后无限循环等待（30s 重查 + "kill to stop"，
workerDefaultMaxRounds=0=永不退出）。Fix: 连续 N 轮空队列后自动 exit 0。
同时验证 Bug 1（delivery-time worktree recycle）已被 698c28d 修复。

## Completed

### Bug 2: worker idle-exit（真 bug，已修）
- **新增 `workerDefaultIdleExits = 3`** 常量：连续 3 轮空队列（无任务执行）后自动 exit 0
  （3 × 30s 默认 idle interval ≈ 90s 无新任务即自灭）。
- **新增 `GOPI_WORKER_IDLE_EXITS` env**（Rule 2 单点读取 `workerIdleExits()`）：默认 3；
  `0` = 永不 idle 自灭（保留旧的 daemon wait 语义，供想常驻的运维显式选择）；负数/畸形
  值回退默认并告警。
- **loop 重写**（runTaskWorker）：非 --once 分支维护 `idleStreak` 计数——每次空队列
  verdict（含 1s settled recheck）`idleStreak++`；达到 N 即打印
  `worker: N idle round(s) with no new task — exiting 0` 并 return（exit 0 = drained 是
  成功非错误）。**busy 轮重置计数**（`ran==true` → idleStreak=0）。
- **--once 语义完全不变**：空队列立即 drain-exit 0（原分支原样保留）。
- **文档同步**：workerUsageHead（main.go）、cli_tasks.go worker block、config/help.go
  （FormatUsage worker block + GOPI_WORKER_IDLE_EXITS 入 --help-env 分组表）、
  settings.go EnvVars catalog（GOPI_WORKER_IDLE_EXITS 条目）。
- 设计符合 nick 9/1 哲学：worker 是任务消化工具（run and self-die），非守护进程；
  要常驻则显式 `GOPI_WORKER_IDLE_EXITS=0` 或外部循环重启。

### Bug 1: worktree 移除时机（验证已修，无代码改动）
- 运行真实二进制 E2E（TestWorkerServerDeliveryRecyclesThenMergerMerges + Dirty 变体）：
  worker 在 server 节点跑完任务 → **worktree 立即自动移除** → branch 存活 → 下一个
  merger pass 自动 merge 进 master（adopted=1）。Dirty worktree（未提交改动）fail-soft
  保留。698c28d backfill 修复生效，delivery-time recycle 正常触发，**无残留 edge**。

### 测试（Rule 5: 每 bug 带回归测试）
新增 `cmd/agent/cli_tasks_worker_idle_exit_test.go`：
- TestWorkerIdleExitsEnv: env 解析（默认 3 / 传值 / 0 / 畸形回退）
- TestWorkerSelfExitsAfterIdleRounds: N=1 空队列自灭（无 --once、无 max-round cap）
- TestWorkerBusyRoundResetsIdleStreak: busy 轮重置计数（"(1/3 idle rounds)" 出现 ≥2 次
  证明 reset；N=3 需 3 轮 reset 后的 drained 才退出）
- TestWorkerIdleExitZeroWaitsForever: 0 = 永不 idle 自灭（只允许 max-round cap 退出）

既有 worker/serve/help/usage 测试全绿（无回归）。

## Not Done
- GOPI_WORKER_IDLE_EXITS 未加入 settings.json settings 键映射（worker env 均为 "" 纯
  env——与 GOPI_WORKER_MAX_ROUNDS 同款，保持一致）。

## Verification
- Build: `go build ./...` OK；`CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` OK
- New tests: 4/4 PASS（go test -run 'TestWorkerIdleExitsEnv|...'）
- Worker regressions: PASS（WaitAndPicksUp / OnceSweepsZombie / HelpDoesNotRun /
  UsageHead / DrainedExit / Writeback / MaxRounds / Excluded / SyncObs / ServeTick）
- Bug-1 E2E: TestWorkerServerDeliveryRecyclesThenMergerMerges PASS（recycle→merge
  adopted=1）、TestWorkerServerDeliveryDirtyWorktreeKeepsBranch PASS
- Full suite: go test ./...（见下）
- Commit: 见 git log（feat/worker-idle-exit 分支）
