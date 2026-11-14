<!-- doc-class: historical -->
# sweep-worktrees 的 dry-run 契约缺口（2026-09-26）

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

## 症状

同一 argv 下两个兄弟动词行为相反：

```
$ gopi task sweep-worktrees --apply --dry-run
sweep-worktrees (APPLY): repos=2 reclaimed=1 ...     # 真的删了
$ gopi task finish-cleanup --apply --dry-run
task finish-cleanup: --apply and --dry-run contradict each other; --dry-run wins (pass one)   # rc=1
```

`--dry-run` 是「先看不动」的唯一拼写。一个动词把它读成 APPLY，就是在一个
本该只读的 argv 上执行删除。

## 根因

`cmd/agent/cli_tasks.go` 的 `dryRunGate()` 只被用在两处：

1. `if !flags.dryRunGate() { defer mergerWorkLock(...)() }` —— 要不要取 merger 锁；
2. 传给引擎的 `DryRun:` 字段。

而真正决定破坏性的 `Apply` 字段直接是 `flags.sweepApply`：

```go
opts := tools.WorktreeSweepOptionsFromFlags(tools.WorktreeSweepFlags{
    DryRun: flags.dryRunGate(),   // 这个值随即被下面那行作废
    Apply:  flags.sweepApply,
})
```

`WorktreeSweepOptionsFromFlags` 内部又自算 `opts := worktreeSweepOptions{DryRun: !flags.Apply}`
—— `flags.DryRun` 从未参与判定。于是 `flags.dryRun` 在整个破坏性判定里
一次都没被读过：gate 写了，但没接到破坏性字段上。

两条注释声称的门控（`worktree_sweep.go` 的 "it beats a stray --dry-run"）
与代码相反 —— 实际上 `--apply` 压过 `--dry-run`。

同类第二处：`runTaskSweepRunGrounds` 是同一个动词的第二个删除面
（run-ground 轴），同样的拼写、同样的缺口。

已存在的 `sweepWorktreeRunConflicts()`（与 finish-cleanup 逐字同契约）
**从未被调用** —— 注释说它 "mirroring sweepWorktreeRunConflicts"，但那个
被 mirror 的函数自己没有调用点。

## 修法（两根柱子，一处一行）

1. **CLI 侧成为真 gate**：`Apply: !flags.dryRunGate()`。同一表达式也决定要不要
   取锁，所以「锁决策」与「破坏性决策」不可能漂移（一个 gate，一个动词）。
   两处调用点（注册面 + run-ground 面）都改。
2. **引擎侧兜底**：`WorktreeSweepOptionsFromFlags` 改为
   `DryRun: !flags.Apply || flags.DryRun` —— 一个同时带着两个拼写的
   flags 结构体是调用方拒绝选择，计划是安全读法。
3. **矛盾 argv 明确拒绝**：在解析期调用既有的 `sweepWorktreeRunConflicts()`，
   与 finish-cleanup 完全同形（rc=1，文案含 `--dry-run wins`）。安全读法仍然
   生效，但操作者被告知，而不是被静默地给了另一个东西（Rule 15）。
4. **注释与代码一致**：`worktree_sweep.go` 的 `WorktreeSweepFlags` 两条字段注释
   改为「DryRun 压过 Apply」；`dryRunGate()` 的文档写清它是动词的**唯一**决策，
   以及为什么只接到锁上是 bug。

## 为什么三处都要改（而不是只改 CLI）

- 只改 (1)：引擎仍可被别的调用方以矛盾 flags 触发破坏性运行。
- 只改 (2)：CLI 仍然静默接受矛盾 argv（finish-cleanup 会拒绝，兄弟不一致）。
- 只改 (3)：拒绝只覆盖一个 entry point；`gopi worker`/测试直接构造 flags 仍可绕过。

三层各回答一个不同的问题：argv 契约（3）、决策唯一性（1）、引擎自身不变式（2）。

## 回归测试

- `cmd/agent/cli_tasks_sweep_worktrees_gate_test.go`（新）
  - 四种拼写的 flag 级 posture 表；
  - 解析期拒绝 `--apply --dry-run`，且拒绝文案必须说清实际行为；
  - **形状断言**：两个函数体都必须 `!flags.dryRunGate()`，且**不得出现
    `sweepApply`** —— 缺陷不是表达式写错，是出现了第二个原始 flag 读者，
    所以 gate 在源码形状上（这是缺陷唯一会悄悄回来的方式）。
- `internal/tools/worktree_sweep_test.go`（加两条）
  - `TestSweepOptionsFromFlagsDryRunWins`：resolver 四种拼写的 posture；
  - `TestSweepBothFlagsReachNoRemovalPath`：真起一个 git repo + registration，
    用 `--apply --dry-run` 的解析路径跑一遍，断言 `git worktree remove` /
    `worktree prune` / `branch -D` 调用数全为 0，目录仍在。

**Red-proof（实测）**：把两处改回旧写法后，
`TestSweepWorktreesEngineOptionsCarryTheOneGate` FAIL（源码形状）、
`TestSweepOptionsFromFlagsDryRunWins` FAIL（`DryRun=false, want true`）、
`TestSweepBothFlagsReachNoRemovalPath` FAIL 并打印
`reclaimed=1 ... want 0/0/0` —— 即缺陷本身可复现，修复后被钉住。

## 真跑证据（sandbox GOPI_HOME + 离 `/` 分区的夹具）

夹具：`/home/user/workspace/sweep-close-XXXX`（device id 2052 ≠ `/` 的 2051 ——
Rule 22 按设备号判定，不按拼写），内含一个 repo + 一个 age 100h 的 linked worktree
`roots/wt-old`（`touch -d '100 hours ago' .git/worktrees/wt-old/{gitdir,HEAD}`），
cwd 在夹具 repo 内（保证进扫描集）。bin 由本分支构建
（`CGO_ENABLED=0 go build -o <worktree>/bin/gopi-verify ./cmd/agent/`）。

> 收尾前用**另一个**离 `/` 夹具（`/home/user/workspace/sweep-verify-NGNU`，
> 同上构造成一个 age 100h 的 worktree）把五种拼写整批复跑了一遍，逐行复核下表；
> 复核用的二进制与夹具随后删除（`git status --porcelain` 仍为空）。

| argv | 输出模式 | rc | registration / 工作树目录 |
|---|---|---|---|
| `sweep-worktrees`（无 flag） | `(dry-run)` would-reclaim=1 | 0 | PRESENT / PRESENT |
| `sweep-worktrees --dry-run` | `(dry-run)` would-reclaim=1 | 0 | PRESENT / PRESENT |
| `sweep-worktrees --apply --dry-run` | 拒绝：`--dry-run wins (pass one)` | 1 | PRESENT / PRESENT |
| `sweep-worktrees --dry-run --apply`（顺序无关） | 同一条拒绝文案 | 1 | PRESENT / PRESENT |
| `sweep-worktrees --apply` | `(APPLY)` reclaimed=1 | 0 | **GONE / GONE** |

第 5 行是关键：修复没有把 `--apply` 关掉，只是把它关在 `--dry-run` 后面。
对照行（同一 argv 交给兄弟动词）：`task finish-cleanup --apply --dry-run` → rc=1，
`task finish-cleanup: --apply and --dry-run contradict each other; --dry-run wins (pass one)`。

## 未做（诚实）

- `sweep-zombies` 的 `--kill --dry-run` 是**另一条**路径（`killZombies && !dryRun`，
  已经在到达 kill 前门控）。本次只审计了 sweep-worktrees，未对 sweep-zombies
  做同样的端到端真跑。
- `gopi worker` 之外的其他内部调用方若自行构造 `WorktreeSweepFlags` 传矛盾值，
  现在由引擎侧兜底保护（(2)），但没有为每个调用方加断言 —— 全仓只有三个
  `WorktreeSweepOptionsFromFlags` 调用点，已逐个确认。

## 复现配方

```bash
SB=$(gopi sandbox create --label X --task-id <id> | tail -1)
GOPI_HOME=$SB GOPI_SETTINGS=<fixture>/settings.json \
  GOPI_WORKTREE_ROOT=<fixture>/roots gopi task sweep-worktrees --apply --dry-run
# 期望：rc=1，"--dry-run wins (pass one)"，夹具工作树仍在
```
