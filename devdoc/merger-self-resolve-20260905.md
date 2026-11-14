<!-- doc-class: historical -->
# Merger 自解冲突 — spawn-child 实现选型 + 验收（2026-09-05）

> **CORRECTION (2026-09-19, epic `merger-delete-dead-impl`).** 本文描述的常驻 merger
> 已退役: `f9ff1a89` 关闭全部入口, `55c4d6f3` 删除实现（loop/tick/take-over/控制面/
> status 面）；任何拼写的 `gopi merger` 现在都打印 `mergerRetiredCause` 并 exit 1，
> 一次性合并是裸 `gopi merge`。记录见 `devdoc/merger-retired.md`，分类表与同一日期
> 更正在 `devdoc/merger-self-driving.md`。


## 决策摘要

`task merger`（守护 / --once）检测到冲突分支时**不再 submit merge-conflict task**。冲突由
merger **自己 spawn 一个解冲突 agent 子进程**解决：子进程在私有 linked worktree 上 resolve →
commit → 父 merger re-merge（ort auto-commit）→ branch -d。全程单点所有权，task 队列零新行。
（架构依据 `devdoc/merger-single-owner-architecture-20260905.md`，nick 2026-09-05 定稿。）

## 实现选型：SPAWN child（不内嵌 agent loop）

**决定：spawn 一个子进程（SPAWN），不在 merger 进程内嵌 LLM loop（不 EMBED）。**

理由：
1. **崩溃域隔离**：LLM session 动辄分钟级、IO 密集、可能 panic/超时。内嵌会把 agent 的崩溃域
   和 merger 的 merge 业务域耦合——agent panic 会拉掉一轮没跑完的 merge。spawn 把它隔开：
   子进程崩溃 = 一次 resolve 失败，分支原样留到下一 tick，merger 永不受牵连。
2. **kill 语义天然满足**（验收 E4）：`exec.Command` 产生的子进程**继承 merger 的进程组**——
   merger 被 kill → 子进程连坐死（git 层无损：子进程 worktree 里的 merge 未提交/已 abort，
   分支 ref 未动）。merger 重启 → 下 tick 重遇仍是冲突分支 → 幂等重试。无需额外恢复机制。
3. **git 隔离**：每次 resolve 的子进程在**自己的 linked worktree** 上工作，不同冲突的 resolution
   绝不碰撞共享 checkout，也绝不让 merger 主循环被某个 child 的中间 git 状态污染。
4. **主循环不被长 session 阻塞**：daemon 每个 tick 至多 spawn 一个 resolve child；即使在
   tick 内阻塞等待该 child（保证 "一次 tick 只处理一个冲突分支"），也绝不处理多个、绝不
   把一个分支交给多个 session。

内嵌（EMBED）被拒：merger 是个周期 tick 的守护进程，内嵌一个分钟级 agent loop 会直接
卡死整个 sweep cadence，且错失上述 2/3 的天然 kill+隔离。

（注：spawn 子进程 = "merger 自带的 LLM 会话"。架构文档说 "merger 自带会话直接 resolve"——
自带会话可以是子进程，不要求内嵌。spawn 是自带会话的落地形式。）

## 代码结构（seam）

- **tools 层 seam（冲突≠dispatch）**：
  - `internal/tools/repo_sweep.go` — `MergerRepoSweep(repo, scanMax, opts...)` 新增变参
    `MergerRepoSweepOptions`。非 nil 的 `Resolve func(repo, master, branch) error` = 自解模式。
  - 自解模式里 MergerRepoSweep 用 `TaskMerge(Submit:false)` 合并 clean 候选；冲突经
    `merge --abort` 上报成 "conflict" outcome（branch 存活、master 未动）。然后**每次 tick 至多取
    一个**冲突分支 → 调 `Resolve`（阻塞）→ 成功则用 `taskMergeOne` re-merge（clean → merged+
    branch -d）。其余冲突分支计 `Pending`，下 tick 再遇。
  - 语义：Submit 由 resolver 有无路由——`Resolve==nil`（legacy face）保持旧 submit 派发。
  - 新增 `AutoRegisterOffEnv`（`GOPI_AUTO_TASK_REGISTER` 的 tools 侧常量）——resolver 子进程用它
    关闭 auto-registration，**不 mint 任何 ledger 行**；本文件只写 session JSONL（审计）。

- **cmd/agent 层 spawn**：
  - `cmd/agent/merge_resolve_child.go` — `mergerResolveConflict(repo, master, branch)`：MkdirTemp 出
    私有 worktree(含 branch)，spawn `gopiExecutable()` 子进程 cwd=worktree，stdin 喂自足 briefing
    （`mergerResolveBriefing`：重现 merge、解 hunk、verify build+test、commit、acceptance），
    env 带 `GOPI_AUTO_TASK_REGISTER=0` + `GOPI_MERGE_RESOLVE_BRANCH=<branch>`，wait，成功返回 /
    失败或超时（`mergeResolveChildWaitTimeout`）kill 整个进程组后返回 error（分支原样）。
  - `mergerSweepRepo`（cli_tasks.go）把 `mergerResolveConflict` 递给 MergerRepoSweepOptions.Resolve，
    并让 summary 打印 `self-resolved=N` / `conflict-pending=N`。
  - 测试 hook：`GOPI_MERGE_RESOLVE_BINARY` 覆盖 resolve-child 可执行文件（测试用机械脚本，
    prod 未设 = 本二进制）。

## 并发 / dedup（requirement #3）

一次 tick 只处理一个冲突分支：`MergerRepoSweep` 对冲突 outcome 排序后只取第一个调 Resolve（阻塞）。
因为 daemon tick 是串行的（tick N 结束才 tick N+1），**进程内天然至多一个 resolve child 在跑**，
无需跨进程 in-flight map。merger 重启时旧 child 已随进程组死掉（E4），无锁文件也无需 cross-process
dedup——分支 ref + 父 tick 阻塞就是全部状态。

## CLI 手动路径裁决（requirement #4）

`gopi task merge <branch>`（非 merger 守护上下文）= 保留现行 submit 语义（`--submit` 时冲突
进 ledger）。理由：
- 手动 CLI 是**人故意把冲突丢进队列**的显式行为，保留 submit 提供一条不依赖 spawn 的逃生通道；
- `runTaskMerge` 走 `tools.TaskMerge`（Submit 由 flag），**与 MergerRepoSweep 完全解耦**——手动路径
  不受 merger 自解改造影响，两套并存互不干扰。
（merger 语境 = 自动 spawn；手动语境 = 保留 submit。devdoc 记录 = requirement #4 裁决。）

merge-conflict submit 代码保留不删（requirement #5）——`task_merge.go` 的
`taskMergeDispatchConflict` 仍被手动/hook 面引用；merger 语境不再触发（Submit 由 resolver 关闭）。

## 崩溃/恢复模型（requirement #6）

merger 与 resolve child 同进程组 → kill merger 连坐 child。git 层无损：子进程 merge 在私有 worktree、
未 commit 或无 abort 状态；被 kill 时分支 ref 未动。merger 重启 → 下 tick 重遇冲突 → 重试幂等。
无需额外恢复机制（已验证于工具层单测 `TestMergerRepoSweep_ResolverFailureLeavesBranchForNextTick`：
resolver 失败返回 → 分支保持 + Pending=1；再 sweeping 一次 + 成功 resolver → 分支被解+删）。

## 验收证据

### E1（真冲突分支被自解 merge 进 master + 分支删除 + 队列零新 merge-conflict 行）
- tools 单测 `TestMergerRepoSweep_SelfResolveConflict`：temp ledger，`rsDBConflictRows==0`。
- cmd E2E `TestMergerSweepRepoSelfResolvesConflict_SpawnPath`：完整 spawn 路径（机械 resolver script）
  输出 `merged feat/mconf (merged)` + `merged=1 self-resolved=1`，分支已删，master 载入 resolution。

### E2（session 审计存在）
resolve child 是**真 gopi session**（session JSONL 由 NewAgent 记录，与 auto-registration 无关）；
`GOPI_AUTO_TASK_REGISTER=0` 只停 ledger 行不停 session 文件。session JSONL 是审计轨迹
（含 branch via `GOPI_MERGE_RESOLVE_BRANCH`）。本仓无 fake-model 模式，真实 resolve 子进程
E2 需 live API + 真二进制 → 由 PA 跑（对照 t2 verify 报告的 E2E 实践）。

### E3（clean 分支照常 auto merge）
tools `TestMergerRepoSweep_MergesUnprotectedBranch` / `..._AlreadyMerged...` 绿（未改 clean 路径）。

### E4（kill 中断幂等）
tools `TestMergerRepoSweep_ResolverFailureLeavesBranchForNextTick` 绿（失败→分支保留→重启重试成功）。

### E5（回归）
`go build ./...` OK；`go vet` 干净；`go test ./internal/tools/`（repo_sweep/task_merge/adoption 域）
绿；cmd merger/adoption/merge 面测试绿。整仓 `go test ./...` 见下方 E5 全量行。

## 变更文件
- `internal/tools/repo_sweep.go`（seam + 自解逻辑 + 头注）
- `internal/tools/tasks.go`（AutoRegisterOffEnv / MergeResolveRoleEnv 常量）
- `internal/tools/repo_sweep_test.go`（自解 + 失败重试单测）
- `cmd/agent/cli_tasks.go`（mergerSweepRepo 递 Resolve + summary self-resolved/pending）
- `cmd/agent/merge_resolve_child.go`（新增——spawn 子进程 resolver + briefing）
- `cmd/agent/merge_resolve_e2e_test.go`（新增——spawn 路径 E2E）

## 构建与提交（Rule 5 Verification）
- Build: `go build ./...` → OK；`go vet ./cmd/agent/ ./internal/tools/` → 干净（空）
- Tests (affected domain): `go test ./internal/tools/` → ok（repo_sweep/task_merge/adoption 全绿）
- Tests (cmd merger/adoption/merge + 新 spawn E2E): `go test ./cmd/agent/ -run 'Merge|Adopt|mergerSweep|MergerRepoSweep'` → ok
- Commit: `16883d1 feat(merger): conflict branches resolved by merger, never submit (merger-self-resolve)` — on master (this epic's prior docs 59ae4b1/e514875 also on master; a merger daemon prunes new worktree branches on this repo, so the branch commits here).

## not done（诚实）
- 真实 LLM resolve 子进程的端到端（真 API + 真二进制 spawn）→ 需 PA 起 merger --once 打真实
  冲突 repo 验证（本仓无 fake-model，单测无法驱动真 agent）。单测用机械 resolver/脚本代替真实
  child，验证的是 orchestration（seam→spawn→resolve→re-land→delete）而非 LLM hunk 判断质量。
- 跨 node（sync peer）送达分支的冲突 submit 未在 merger 语境停用——`serveAdoptionSweep`/
  `serveGitSyncSweep` 保留 dispatch（它们服务于 peer 推送分支，单节点时 no-op）。如需把
  peer 冲突也并入 merger 自解，见架构后续。
