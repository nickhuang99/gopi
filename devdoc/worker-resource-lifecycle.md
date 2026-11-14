<!-- doc-class: historical -->
# worker 为 child gopi 分配的资源 —— 清点、分配/回收对称性、取消入口对照、回收方案

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

调查任务（不写实现）。测量时间 2026-10-03；代码取证基于本 worktree 的基线
`fbbf2808 delivery-point sync: merge master into the delivered branch before close-out`
（`git -C <this worktree> log --oneline -1`）。

**方法**：每条结论要么是 `file:line` 代码取证，要么是本机实测输出。查不出的写
"未找到"，不推测（Rule 15）。实测在**一个沙箱 profile**（`gopi sandbox create --label
probe-cancel-lifecycle --task-id probe-cancel` 造的私有一份 `GOPI_HOME`）里跑，不污染
操作者 profile。

---

## 0. 一句话结论

`gopi worker` 在 claim 一行之后、spawn child 之前，实测只**分配两种持久资源**：
**child 的 ground（linked worktree + branch）** 与 **它的 session 目录**；
其余八项（sandbox、GOPI_HOME、tmpdir、X11、端口、memory.db、锁、进程组）都是
**继承或进程生命周期内**的东西，不需要"分配"也就不需要"回收"。

**真正的缺口不在"分配"，在"谁在取消时知道要回收"**：
`task update --status cancelled` **不通知任何进程**——在跑的 child 与 worker 继续跑
（实测），而一旦 wrapper 在 close-out 之前死掉，ground 就只剩 sweep 这一条迟到的路。

---

## 1. 资源清单：十项，每项四问

| # | 资源 | 分配点（file:line） | 是否登记 | 谁回收 | 对称？ |
|---|---|---|---|---|---|
| 1 | child ground（linked worktree） | `internal/tools/task_run_ground.go:153` `TaskRunGroundFor` → `:583` `taskGroundCreate` → `:596` `git worktree add <dir> -b <branch> <head>`；调用点 `cmd/agent/cli_tasks.go:2483`（spawn 前的 ground 解析） | 是：ledger `deliverable.worktree` / `deliverable.branch`（`internal/tools/task_deliverable_worktree.go`）+ git 自身的 registration | ① 正常路径：wrapper 退出 → `cmd/agent/cli_tasks.go:2903` `taskRunReleaseOwnWorktrees` → `internal/tools/task_reclaim_self.go:1028` `ReclaimOwnWorktreesAtCloseoutWithin`；② 迟到路径：resident sweep `cmd/agent/cli_tasks.go:3745` `serveFinishCleanupCandidateIDs` + `WorktreeSweepRun` | 正常路径**对称**（实测，见 §3 实验A）；异常路径**不对称**（§3 实验B） |
| 2 | branch ref `task/<id>` | 同上（`git worktree add -b`，`task_run_ground.go:596`） | 是（`deliverable.branch`） | finish-cleanup 的 `git branch -d`；未 push 落地的分支被 `internal/tools/task_finish_cleanup.go:4266` `finishCleanupBranchForceDeleteReason` 拒绝强删 | 对称，但**只有跑到 finish-cleanup 才收** |
| 3 | sandbox profile | **worker 不分配**：`sandbox.Create` 全仓唯一调用点 `cmd/agent/cli_sandbox.go:369`（操作者/LLM 显式 `gopi sandbox create\|run`） | 是：`.sandbox-owner.json`（`internal/sandbox/sandbox.go:323-327`） | `internal/scratch/sandbox_gc.go:331` `os.RemoveAll`（`SweepSandboxes`），由 `gopi scratch-gc sandboxes` 与 worker/merger tick 的 `scratchGCTick` 调用 | 对称；实测 161/167 因**年龄**保留（§4 数字） |
| 4 | GOPI_HOME / profile | **不分配**：child **继承** worker 的 profile（`internal/tools/tasks.go:4163-4190` 明说 `GOPI_HOME / GOPI_MEMORY_DB` 是 child 合法继承项，只在 spawn seam 上剥离 `GOPI_EXECUTOR_SESSION_ID` 与祖先锚） | 进程**归属**按 `GOPI_HOME` 判定（`gopi worker --status` 的分 profile 清单） | 无人回收**进程本身**（见 §5 A3） | 继承项，无分配；但**另一 profile 的 worker 无回收者** |
| 5 | tmpdir / scratch | **不分配 per task**：`internal/config` 的 `ResolveTmpDirConfigured` 一层解析，`cmd/agent/tmpdir_inject.go:132` 发布进进程环境，child 继承（实测 child env `TMPDIR=/home/user/workspace/_tmp`） | 是：`internal/scratch/declared_trees.go` 的 declared tree 表 + 年龄常量（`:214` `GoTmpStaleAge=24h`、`:217` `RawLogStaleAge=72h`） | scratch GC（declared trees 的两条臂） | 对称（继承，无 per-task 分配） |
| 6 | 子进程树（child / 孙进程 / tool 子进程） | `cmd/agent/cli_tasks.go:2369` `exec.Command` + `:2385` `SysProcAttr{Setpgid:true}` | 部分：进程内 `registerPassChild`（`:2771`，`cmd/agent/child_registry.go:68`）——**只在 wrapper 存活期间存在**；ledger 侧另有 `proc_pid`/`proc_start_ticks`/`proc_cmd`（`internal/tools/task_holder_backfill.go:141`） | `internal/tools/run_closeout_reap.go:224` `RunCloseoutReapRunChildren`（R1 血统 env / R2 进程组 / R3 祖先；自建 session 且无血统记录者 veto） | **不对称**：回收只在 wrapper 自己的 close-out 里跑 |
| 7 | X11 DISPLAY | **不分配**：`cmd/agent/worker_display_preflight.go` 只**宣布**继承来的 DISPLAY（本机常量 `workerLocalDisplay = "Xvfb :99"`） | 无 | 无需（继承） | 无 per-task 分配；转发 display 随 forward 死亡，无法由 gopi 回收 |
| 8 | 端口 | **不分配**：child/worker 路径无监听端口；唯一 per-run 绑定是 `internal/shot/shot.go:634` `net.Listen("tcp","127.0.0.1:0")`（内核分配，进程退出即释放）。长期 listener 只在 `cmd/agent/cli_web.go:372` / `cli_ui.go:860` / voice，均非 worker 路径 | 无 | 内核（进程退出） | 对称 |
| 9 | memory.db 家族 | **不分配**：ledger 路径一层解析（`config.LedgerPath`），child 继承 | 是（ledger 本身就是登记处） | **永不回收**（append-only 设计） | 刻意不对称：实测 `~/.gopi/memory.db` 220 MB（`ls -la`），`~/.gopi/tasks.db` 0 字节遗留 |
| 10 | session / raw / log 目录 | `cmd/agent/task_session_cwd.go:135` `taskWorkdirEnter`（worker round 在 spawn 前：`cmd/agent/cli_tasks_worker.go:949`）→ `internal/tools/task_session.go:300` `taskEnsureTaskSessionDirs` / `ChdirTaskSessionDir` | 是：容器内的 `metadata.json` 就是"ledger 记得它"的凭据（`internal/scratch/sessions_scratch_dir.go` 的 candidate 谓词） | declared tree（`declared_trees.go:220` `SessionRawLogStaleAge=14d`；容器目录另受 `:247` `SessionScratchDirStaleAge=30d` 与 `:258` `SessionAttachmentsStaleAge=7d` 约束），且**只在 ledger 已遗忘该容器**时才收 | 有上界，但**极慢**：实测 81,222 个目录 / 约 80 GB（§4） |
| 11 | 单实例锁（worker.lock / serve.lock / home-gc lock） | `cmd/agent/cli_worker_control.go:175` `workerLockFilename="worker.lock"`；`cmd/agent/cli_tasks.go:56` `serveLockFilename="serve.lock"`；merger 复用 serve.lock（`cmd/agent/merger_work_lock.go:277`） | 锁文件本身：`internal/scratch/profile_root.go:311-321` 列为 live state | 进程退出 / `close(fd)` 即释放；**文件残留永不被 sweep 收**（同上，明标 never a candidate） | 对称（锁不会泄漏），但**文件永不自清** |

**术语修正（本任务的前提里有一项对不上）**：任务描述提到"虚拟 DB"。全仓检索
（`grep -rni "虚拟 DB\|virtual db\|VirtualDB"`）**未找到**任何生产者；实际的 DB 家族只有
上表第 9 项（`memory.db` + 0 字节的 `tasks.db` 遗留）。按 Rule 15 记为"未找到"，不做推测。

---

## 2. 取消入口 × 行为 对照表

五种入口，逐格取证。凡标注"实测"的格子来自 §3 的实验。

| 入口 | 谁在跑/谁被执行 | **在跑的 child 是否被通知** | row 终态 | ground（worktree/branch） | 锁 |
|---|---|---|---|---|---|
| `gopi worker --stop`（drain） | worker loop；先把请求落成**文件**再发信号（`cmd/agent/cli_worker_control.go:2022` `consumeDrainRequests`） | **否** —— drain 的语义就是"让 round 跑完"；`forwardWorkerStopToChild` 只在 bare-signal 与 force 两条路径被调用（`cmd/agent/cli_tasks_worker_stop.go:583`，调用点 `:740` 与 `:795`） | round 自己写 completed/failed | 走正常 close-out，释放 | loop 退出释放 |
| `gopi worker --force`（SIGUSR1，`cli_worker_control.go:246`） | 同上 | **是**：`terminateInFlightChild`（`cli_tasks_worker_stop.go:787`）→ `forwardWorkerStopToChild(true)` → `SIGKILL` | writeback failed（诚实） | close-out 仍跑，释放 | 释放 |
| 裸 `SIGTERM` / `SIGINT` | worker loop；`installWorkerStopSignals`（`cli_tasks_worker_stop.go:429`，信号集 `:69`） | **是**：先转 SIGTERM，有界等待 `workerStopGraceDefault` 后 `terminateInFlightChild` | failed | close-out 仍跑，释放 | 释放 |
| worker 被 `SIGKILL` / 被环境回收 | **无人** | **否** | 停在 `in_progress`（僵尸，靠 `SweepZombies` / stale sweep） | **close-out 永不跑** → 只剩迟到 sweep（§3 实验B） | flock 随进程消失；state 文件残留 |
| `gopi task update <id> --status cancelled` | **没有任何进程被通知** | **否** | `cancelled`（终态：`internal/tools/tasks.go:3641` 与 `:3652` 的 `taskStatusCancelled: {}`） | **否**，除非在跑的 close-out 接力（§3 实验A） | 否 |

三个非显然但已取证的细节：

1. **cancel 是终态，且写回方向是对的**：实测一个 cancelled 行上的 close-out 写回被拒——
   `gopi worker: writeback failed (continuing): task: illegal status transition cancelled → completed
   — cancelled is TERMINAL: no status write leaves it`。即"取消"不会被一次后到的成功伪造覆盖。
2. **cancel 不是一个"停止信号"**：`task update --status cancelled` 唯一的写入者是
   `internal/tools/tasks.go:4561` `taskUpdate` → `:4591` `taskUpdateStampedFrom` 的状态机；
   代码里**没有任何**"观察到某行变 cancelled"的读取者（全仓命中只有
   `repo_resolve_row.go:198` 的状态分类、`cli_tasks.go:3590/3616` 的候选查询、以及常量定义）。
3. **cancel 行常常进不了 sweep 的第一条候选查询**：`serveFinishCleanupQuery`
   （`cmd/agent/cli_tasks.go:3589`）要求 `deliverable LIKE '%"repo"%'`；第二条 stranded 查询
   （`:3615`）覆盖 `%"worktree"%`，但它被 `serveFinishCleanupWorktreeScanMax = 10`（`:3596`）限流。

---

## 3. 实测（sandbox profile，2026-10-03）

全部在一个 `gopi sandbox create --label probe-cancel-lifecycle --task-id probe-cancel` 造的
私有 profile 里、用 `GOPI_ALLOW_AGENT_SPAWN=1` 跑；探针行、探针 worktree、探针沙箱在
调查结束后已删除（§6）。

**实验 A —— 在跑的 round 被 cancel（worker 与 child 都活着）**
- cancel 后立刻：worker 与 child **都还活着**（`ps` 有），行已是 `cancelled`。
- 之后：worker 没有任何"这一行不再是我的"的观察点，继续跑完 → 它自己的 close-out 照常执行 →
  日志出现 `task <id>: finish-cleanup: … done=[align-before-delivery,pre-merge,worktree] worktree-freed=<dir>`，
  目录与 registration 都消失（`worktree exists after close-out: no`）。
- 结论：**cancel 之所以没留下残骸，是靠"被取消的那次运行恰好跑完了"**，不是靠 cancel 通知了谁。

**实验 B —— 在跑的 round 被 cancel，随后 wrapper 被杀（`kill -9`）**
- 15s 后：行仍是 `cancelled`，而 `git worktree list` 里那条 registration **仍在**，目录**仍在磁盘**。
- 无人回收：close-out（`ReclaimOwnWorktreesAtCloseoutWithin`）从未运行；ledger 侧的自助扫描
  `reclaimSelfRowsFrom`（`internal/tools/task_reclaim_self.go:318-329`）的候选集合是
  `status IN (in_progress, completed, failed)` —— **cancelled 不在其中**。
- 只剩 sweep：`reclaimSelfOwnWhy` 的 rung ① 年龄地板（`task_reclaim_self.go:903-905`，
  `reclaimSelfOwnMinAge = 5m`）会把它留住，除非 close-out 的 transient 标记在（它随进程死了）；
  并且只有**有 worker/serve 在跑**时这个 sweep 才会被驱动。
- 结论：**这是本任务找到的最明确的不对称**：cancel + wrapper 死亡 = ground 挂在那，直到有人
  手动 sweep。

**实验 C —— cancel 的终态语义（对照）**
- 见 §2 细节 1：close-out 的 `completed` 写回被状态机拒绝，方向正确。

**host 上的活体证据（不是历史叙述，是调查当刻仍在跑的进程）**
- 三个 `in_progress` 行（`gopi task list --status in_progress`，12:16）各有一个 live holder
  （`proc_pid` 命中 `ps`），例如 pid 199534 / 2454727，二者 `cgroup=…/session-1179.scope` /
  `session-1221.scope`（即**没有**脱离登录会话，Rule 24 的判据），其 child 的 `cwd` 正是各自
  行的 ground（`/home/user/workspace/gopi-<rowid>`）。
- 一个 **leaked worker**：pid 3670067，`cwd=/proc`，`cgroup=session-1177.scope`，
  `GOPI_HOME=/home/user/.gopi/sessions/<某个 rig 目录>/rig/home` —— 它由
  `gopi worker --status` 打印在"belong to ANOTHER profile — NOT counted"清单里。
  它从 09:27 活到 12:2x（`lstart` 取证）。**没有任何回收者**。这正是
  `cmd/agent/cli_worker_control.go` 自己注释里承认的形态（"A leaked test fixture — a worker
  started with HOME pointing at a sandbox — is a common origin"）。

---

## 4. 规模（Rule 38：数字必须自带生成器与测量点）

生成方式全部是**本机一次性命令**（不是仓库里的脚本，故这里给出命令原文，测量点 = 上表基线
`fbbf2808`，2026-10-03 12:2x）：

| 数字 | 命令 | 值 |
|---|---|---|
| session 容器目录数 | `ls /home/user/.gopi/sessions \| wc -l` | **81,222** |
| session 树字节数 | `du -sb /home/user/.gopi/sessions` | **80,037,900,409**（≈80 GB） |
| sandbox 目录数 | `ls -d /home/user/workspace/gopi-sandbox/*/ \| wc -l` | **167** |
| sandbox 带 owner 标记数 | `find …/gopi-sandbox -maxdepth 2 -name '.sandbox-owner.json' \| wc -l` | **160**（7 个无标记） |
| sandbox GC 判定 | `gopi scratch-gc sandboxes --dry-run` | **161 keep（160 条 `reason="not aged"`）** |
| linked worktree 数 | `git -C /home/user/work/GoPi worktree list` | **4**（含 master） |
| failed 未处置行 | `gopi task list --status in_progress` 尾部的 tally | **242** |
| ledger 大小 | `ls -la /home/user/.gopi/memory.db` | **220,860,416 B** |

读法：sandbox 侧**年龄门是唯一在挡的原因**（160 条 `not aged`），说明这套回收的瓶颈不是
判据而是窗口；session 侧则是"上界存在但极慢"的量级问题（14/30 天 × 8 万目录）。
两者都**不是**"没有回收者"。

---

## 5. 不对称清单 + 各自的确定性方案（不依赖 LLM）

每项须满足六条：**分配即登记 / 结束与取消同路径且幂等 / 不依赖 child 配合 /
拒绝要说出真原因 / 子进程边界讲清 / 复用既有机制**。

### A1 —— cancel 与在跑运行之间没有观察者
**事实**：§2 细节 2（无任何代码读"某行变 cancelled"）；实验 A 证明结果偶然正确。

**方案（复用既有机制，不新增依赖）**
- **分配即登记**：已经登记（`claimed_by` + `proc_pid`/`proc_start_ticks`/`proc_cmd`，
  `task_holder_backfill.go:141`；claim 时的写入点 `task_deliverable_worktree.go:976` 一带）。
- **取消与结束同路径且幂等**：`task update --status cancelled` 写入成功后，对**该行有 live holder**
  的情况，落一份**durable 的 stop 请求**——复用 worker 已有的 `workerStopRequestFile` 机制
  （`cli_worker_control.go:2022` `consumeDrainRequests` 读它），由 holder 的 worker 在下一次
  stop 检查点消费。重复投递是 no-op（其自身文档：`cli_tasks_worker_stop.go:304`
  "a repeated graceful request is a no-op"）。
- **不依赖 child 配合**：close-out 的 ground 释放**不经过 child**（`taskRunReleaseOwnWorktrees`
  先 `RunCloseoutReapRunChildren` 再释放），所以 child 不理会也能收。
- **拒绝要说出真原因**：holder 已死 → 不走请求，直接打印"holder gone"，交给 A2 的路径。
- **子进程边界讲清**：请求只送到 holder 的 **worker 进程**，不 kill child；由 worker 自己的契约
  （drain 等 round 跑完 / force 才 kill）决定 child 的命运。
- **复用**：`consumeDrainRequests` + `ReclaimOwnWorktreesAtCloseout` + 现有 ledger 列。

### A2 —— wrapper 在 close-out 之前死掉
**事实**：实验 B（ground 挂住）；`reclaimSelfRowsFrom` 的候选集**不含 cancelled**
（`task_reclaim_self.go:327`）。

**方案（两个动作，顺序不可颠倒）**
1. **先让候选集覆盖它**（最小、纯登记修复）：cancel 一行时，若其 `deliverable` 有
   `worktree` 而无 `repo`，从 worktree 反查 repo 并写入 `deliverable.repo`。
   `serveFinishCleanupQuery`（`cli_tasks.go:3589`）要求 `%"repo"%` ——**补上这个键就等于把它登记进了
   sweep 的视野**，而且走的是既有查询、既有 guard，不新增任何机制。
2. **再让"自己的 ground"免掉年龄地板**（只对**已死 holder** 的行）：把 close-out 那个
   transient 标记（`internal/tools/reclaim_own_ground.go`：文档明说"deliberately NOT an env var"）
   改成**落在行上的证据**（例如 `deliverable` 里一个 release-pending 键，或一条 task_event），
   由 `reclaimSelfOwnWhy` 的 rung ①（`task_reclaim_self.go:898`）读取。
   **明确不做的事**：不动 `reclaimSelfOwnMinAge`——它是"并发 round 正在创建"的保护，削它就是
   把 A2 换成"误收别人的 fresh ground"，更坏。
- **不依赖 child 配合**：只读 ledger + git registration。
- **拒绝要说真原因**：holder 仍活 → 保持现有 `reclaimSelfOwnLiveHolderReason`（rung ②）。
- **子进程边界讲清**：cwd 持有者仍 veto（`scratch.HeldByProcesses`，`task_reclaim_self.go:927`），
  自建 session 的服务仍被 `run_closeout_reap.go` 的 veto 放行。

### A3 —— 另一 profile 的 leaked worker 无回收者
**事实**：§3 活体证据（pid 3670067，跨 profile，11 小时无人管）。

**方案**
- **分配即登记**：`gopi worker --status` 已经**看得见**它（实测输出点名 pid 与 profile 路径），
  只是不算——把它从一行 note 升级为一条**可判定的孤儿谓词**。
- **谓词（两条都要成立）**：① **owner 已消失**（其 profile 的 session 目录已被 sweep 收走，
  或它的 launcher pid 已死）；② **空闲**（无 in-flight child）。
- **回收行动复用既有信号路径**：`terminateInFlightChild` 的同一条有界信号（SIGTERM → 有界等待 →
  SIGKILL），不是新的 kill 代码路径。
- **拒绝要说出真原因**：任何一项不可判定（environ 不可读、有 child、进程表读不到）→ KEEP，
  并打印为什么；与 `run_closeout_reap.go` 的"EVERY UNKNOWN IS A KEEP"同构。
- **禁止的判据**：**不能**用"不是本 profile"当回收理由——那会杀掉合法的沙箱运行。
- **复用**：`cli_worker_control.go` 的 profile 归属读 + resident tick。

### A4 —— profile 状态文件永不自清（**不改**，只记录）
`internal/scratch/profile_root.go:311-321` 把 `serve.lock` / `worker.lock` 明标为 live state、
never a candidate。这是**安全的**（锁不会泄漏，文件残留无害），写在这里是为了让后来者
不要把它当漏洞"修"成一个能删锁的 sweep。

### A5 —— session 容器 30 天 × 8 万目录（规模，不是泄漏）
**方案（可见性优先于调参）**：给 `gopi scratch-gc` 增加 `sessions` 面（与
`sandbox-gc --dry-run` 同形），输出每个候选的 verdict 与理由——现在只有 sandboxes 有这个面。
判据与年龄**都不动**（调短 = 删证据，`sessions_scratch_dir.go` 的 30 天地板有它自己的测量理由）。
- **拒绝要说真原因**：ledger 仍记得的容器 → 打印"ledger describes it"。

---

## 6. 实施路线图（分项 + 依赖顺序 + 独立验收）

| 序 | 项 | 依赖 | 独立验收标准 |
|---|---|---|---|
| R1 | cancel 时补齐 `deliverable.repo`（从 worktree 反查） | 无 | cancel 一个带 worktree 的行后 `gopi task show` 有 repo 键，且 `serveFinishCleanupWorktreeQuery`/`serveFinishCleanupQuery` 能命中它 |
| R2 | cancel 的 durable drain 请求（A1） | R1 | 复跑 §3 实验A 与实验B 的形状：**两者都必须释放 ground**；重复 cancel 不产生第二个请求 |
| R3 | ground-release 的持久证据（A2 第 2 步） | R2 | `kill -9` wrapper 后，sweep 在一次 tick 内释放该 ground；**同时**证明并发新 round 的 fresh ground 不被误收（负向测试） |
| R4 | 另一 profile 的孤儿 worker 回收（A3） | 无 | 造一个 leaked worker（无 child、profile 已消失）→ tick 后进程消失且有理由行；造一个**合法**沙箱 worker → tick 不得杀它 |
| R5 | 取消入口对照表进 help 文本 | 无 | `gopi worker -h` 的 stop 段落逐入口写明"是否通知在跑的 child"；`task update -h` 写明 cancel 不通知任何进程 |
| R6 | `gopi scratch-gc sessions` 面（A5） | 无 | 对 8 万目录的树给出与 `sandbox-gc --dry-run` 同形的 verdict+理由，只读 |

依赖顺序：**R1 → R2 → R3**；**R4 / R5 / R6 各自独立**，可与前一串并行派发。

---

## 7. Not Done（诚实）

- **没有改任何生产代码**：§5 的方案是设计，全部未经实现、未跑测试——这是调查任务的边界。
- **A2 的"落在行上的证据"未定形**：`deliverable` 里加键 与 `task_events` 两条路都可行，
  选哪条要看 `task_finish_cleanup` 的既有解析器，本任务未下钻到那个决定。
- **§3 实验只在一个 profile 与一次 SDK 模型运行下做过**：cancel 的行为与模型是否配合 bash
  超时有关；wrap 在不同模型/超时下的概率未测。
- **"虚拟 DB"**：全仓未找到任何生产者，按 Rule 15 记为未找到，不做推测（§1 末）。
- **242 个 failed 未处置行的历史成因未逐条归因**（只做了聚合数字，§4）。
