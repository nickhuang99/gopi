<!-- doc-class: normative -->
# 第二道闸：run-ground 锚点把「claim 时的 worker 还活着」当成「这块钱的 run 还在跑」（2026-10-07）

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

任务 `01a1151f-5a0f-7c3a-ac06-736f63717a66`（bug）。本文件是**标准叙述（normative）**：
它陈述的规则作用于 master 当前代码，并给出可解析的锚点。

姊妹篇：`devdoc/worktree-dirty-residue.md`（第一道闸 —— run 的结束**没有被上报**）。
本文件是第二道闸 —— run 的结束**没有被采信**。

## 0. 一句话

`internal/tools/run_ground_anchor.go` 的 `RunGroundAnchorLiveness` 只用**一个事实**
（锚点里记的 pid 还活着）判断"这块钱还有人在用"。而锚点记的 pid 是**claim 这行时的进程**，
在本机实测中它是一个**长命 `gopi worker` 循环**：同一个 pid 依次 claim 了一行又一行。
于是：**该 worker 活着 ⇒ 它历史上 claim 过的每一块钱都永久 LIVE**，哪怕对应的 run
早在几小时前就结束了（行已是 `completed` / `failed`）。目录因此永久滞留。

## 1. 缺陷（判据）

`RunGroundAnchorLiveness`（`internal/tools/run_ground_anchor.go`）的 rung ① 原话：

```
① the recorded INCARNATION is alive → LIVE (the taker itself)
```

它的理由串是 `"the run that took this ground (pid <pid>) is still here"`。
**这句话在长命 worker 的形状下是假的**：pid 是 claim 时的**外层 worker**，
不是"这一次 run"。pid 与 run 之间没有一一对应——一个 worker 进程先后跑了很多次 run。

判据（可复算）：若一块 ground 的锚点 pid 活着、而该锚点 task_id 对应的账本行**早已终态**，
修复前 `RunGroundAnchorLiveness` 仍判 LIVE、`gopi task sweep-worktrees --dry-run`
对该块仍输出 `keep [run-ground-anchor] … the run that took this ground (pid …) is still here`。

## 2. 实测现场（Rule 31：全部真跑，逐字）

本机 `/home/user/work/GoPi`，2026-10-07。三块滞留地面的锚点（`~/.gopi/logs/removal-audit.log`）：

```
target=/home/user/workspace/gopi-01a113ac57927bb6950402126e224aee
  task_id=01a113ac-5792-7bb6-9504-02126e224aee  holder_pid=3577762  holder_start=109368714
target=/home/user/workspace/gopi-01a114949f6f759186bc63eaaf18e154
  task_id=01a11494-9f6f-7591-86bc-63eaaf18e154  holder_pid=3577762  holder_start=109368714
target=/home/user/workspace/gopi-01a113c544f774239bf9db6a989d038b
  task_id=01a113c5-44f7-7423-9bf9-db6a989d038b  holder_pid=3509539  holder_start=109295712
```

同一个 pid 认领了三行中的两行，而这两行的账本状态是 `failed` 与 `completed`。
进程本身（Rule 24：以 `/proc/<pid>/{cgroup,cwd}` 为准，不用 `pgrep -f`）：

```
$ for p in 3577762 3509539; do [ -d /proc/$p ] && echo "pid $p ALIVE" || echo "pid $p gone"; done
pid 3577762 ALIVE
pid 3509539 gone
$ tr '\0' ' ' < /proc/3577762/cmdline ; head -1 /proc/3577762/cgroup
gopi worker
0::/user.slice/user-1000.slice/session-1174.scope
```

修复前 `bin/gopi task sweep-worktrees --dry-run`（本任务 worktree 内构建的二进制，
在 `/home/user/work/GoPi` 里跑）逐字：

```
run-ground anchors: collectable=1702 kept=294
  keep        [run-ground-anchor] /home/user/workspace/gopi-01a113ac57927bb6950402126e224aee  kept: the run that took this ground (pid 3577762) is still here
  keep        [run-ground-anchor] /home/user/workspace/gopi-01a113c544f774239bf9db6a989d038b  kept: the ground is younger than the floor (6h26m0s < 24h0m0s)
  keep        [run-ground-anchor] /home/user/workspace/gopi-01a114949f6f759186bc63eaaf18e154  kept: the run that took this ground (pid 3577762) is still here
```

**同一缺陷的两个面**：`01a113c5` 没被 rung ① 锁住，只因 claim 它的 pid（3509539）**恰好死了**，
于是走 rung ② 而挂在 age floor 上。也就是说，锁的成立与不成立取决于
"claim 它的是一个长命 worker 还是一次性进程"，而**账本里这两种长得一模一样**。

## 3. 修复：把"一个事实"换成两个独立事实

`RunGroundAnchorLiveness` 现在问**两个互相独立**的问题，**任一为 LIVE 即 LIVE**：

- **LAUNCH**（`runGroundLaunchLiveness`，原阶梯不改）：锚点记的**启动上下文**
  （先算 incarnation，再算 session）还活着吗？每一级不确定都 KEEP（Rule 15a）。
- **LEDGER**（`groundRowLookup.rowLifeEnded`）：锚点**自己写下的 task_id**
  对应的账本行，其生命是否已经结束？

只有 **LAUNCH 说活着、同时 LEDGER 说这行已经结束** 时，LIVE 才被**放宽**为可回收；
其余一律 KEEP。方向只有一条，且每一格都是既有判据：

| 情形 | 判据 | 结果 |
|---|---|---|
| 锚点没写 task_id | 无账本事实可问 | 沿用 LAUNCH 的答案（原行为不变） |
| 账本打开失败 / 行不存在 | 未知 | KEEP（unknown is never dead） |
| 行仍 `in_progress` 且 ended_at 未过期 | 生命未结束 | KEEP |
| 行终态（completed/failed/cancelled/verified/superseded） | 账本自己宣布结束 | 放宽 |
| 活状态 + ended_at 已过 enqueue grace | 账本自相矛盾（既有谓词） | 放宽 |

**没有新造分类器**（Rule 14）：第 3、4、5 行用的就是
`worktreeSweepRowLifeEnded`（`internal/tools/worktree_sweep.go`）——
注册表扫掠各 rung 已经在问的同一个"账本自己说这行结束了吗"，
以及 `taskCleanupEligibleStatus`（`internal/tools/task_finish_cleanup.go`）的终态集合。
本文件**只新增两件东西**：一次按 task_id 的只读账本查（`taskByID`，
读句柄由 `taskSyncDB` 打开，与既有代码同一个入口），和一句真话。

**理由串也一起改了，这是修复的一部分**：rung ① 现在说的是
`"the launch context that took this ground (pid <pid>) is still alive"`。
原来的句子在 worker 形状下是**屏幕上的一句假话**——那正是 Rule 15 的反面：
一个"拒绝了但说错了理由"的判定，操作者据此无法判断该做什么。

**dirty 的红线不在本阶梯**：`RunGroundCollect` 调 `git worktree remove` **不带 `--force`**，
所以含未提交工作的 worktree 由 git 自己拒绝（skip 串引用 git 的原话）。
本文件不新增第二个 porcelain 解析器——Rule 30 的线由**拥有这些文件的工具**守着。

## 4. 为什么不能靠"降 age 阈值"（被否决的候选）

- **降 `--min-age-h`／默认 age floor**：age 是"多久没人碰过"的**代理量**，
  与"run 是否结束"没有因果关系。`01a11494` 的实测 age 只有 3h，而它的 run 早已结束；
  `01a1151f`（本任务）的 age 也很小，而它的 run **正活着**。阈值调小会立刻误收活 run 的地面，
  调大则继续滞留——同一个标量无法同时满足两边，因为它问的不是那个问题。
- **`--force`／新增清理脚本／新 daemon（Rule 35）**：绕过判据而不是修判据；
  而且把"该不该收"变成"谁敢按下去"。
- **把 rung ① 整个删掉**：那会把锁全拆了——活 run 的地面会被收走。
  正控（§5 的 `01a0sg-live`）就是为这条准备的。
- **候选 A（锚点改记"这一次 run"的子进程/会话）**：方向正确但**证据不在锚点里**——
  `TaskGroundAnchorRecord` 在**取地面那一刻**执行，此刻它只知道自己的 pid 与 session，
  "这一次 run 的 id"就是 task_id，而 task_id 已经在锚点里了。真正缺的是**去账本核一次**，
  这就是本修复。候选 B（用 pid 的当前 cwd 反查）被否决：那是**推断所有权**，
  与 `devdoc/PRINCIPLE-deterministic-lifecycle-ownership.md` 的方向相反——
  所有权由所有者在取得时**记录**，而不是事后从 `/proc` 反推。

## 5. 验证（Rule 31 的真跑，逐字）

红（**HEAD 版本的 `run_ground_anchor.go`**，测试文件是本分支的）：

```
$ go test ./internal/tools/ -run TestGroundLivenessBelievesTheLedgerWhenTheClaimWorkerOutlivesTheRow -v
=== RUN   TestGroundLivenessBelievesTheLedgerWhenTheClaimWorkerOutlivesTheRow
    run_ground_anchor_test.go:625: terminal-row ground: live=true reason="the run that took this ground (pid 2196454) is still here"
    run_ground_anchor_test.go:627: the ledger says this run is over (row 01a0sg-finished is failed) while only the CLAIM WORKER is alive — the ground must be collectable: the run that took this ground (pid 2196454) is still here
--- FAIL: TestGroundLivenessBelievesTheLedgerWhenTheClaimWorkerOutlivesTheRow (0.08s)
FAIL
```

绿（修复后，同一条测试）：

```
=== RUN   TestGroundLivenessBelievesTheLedgerWhenTheClaimWorkerOutlivesTheRow
    run_ground_anchor_test.go:625: terminal-row ground: live=false reason="the launch context is alive but the run is over: task 01a0sg-finished is failed — a claim-time worker outlives the row it claimed, so this ground is the run's leftovers, not a live run"
    run_ground_anchor_test.go:637: in_progress-row ground: live=true reason="the launch context that took this ground (pid 2197840) is still alive"
    run_ground_anchor_test.go:648: read-out:
          collectable [run-ground-anchor] …/gopi-second-gate-finished  the launch context is alive but the run is over: task 01a0sg-finished is failed — …
          keep        [run-ground-anchor] …/gopi-second-gate-live  kept: the launch context that took this ground (pid 2197840) is still alive
--- PASS: TestGroundLivenessBelievesTheLedgerWhenTheClaimWorkerOutlivesTheRow (0.08s)
=== RUN   TestGroundLivenessKeepsWhenTheLedgerCannotBeAsked
    run_ground_anchor_test.go:685: a row the ledger does not hold: live=true reason="the launch context that took this ground (pid 2197840) is still alive"
    run_ground_anchor_test.go:685: an anchor that names no row: live=true reason="the launch context that took this ground (pid 2197840) is still alive"
--- PASS
```

测试形状就是缺陷的形状：**同一个活着的 pid（测试进程本身）认领两块地面**，
一块的行终态、一块的行仍 `in_progress`。第二块是**正控**——没有它，修法可能是"把锁拆了"
而不是"把锁放宽到正确的位置"。另加 `TestGroundLivenessKeepsWhenTheLedgerCannotBeAsked`：
锚点写的行不存在、以及锚点没写行，都必须 KEEP（unknown is never dead）。

真跑（`/home/user/work/GoPi`，本分支二进制，**同一命令**）：

```
$ /home/user/workspace/gopi-01a1151f…/bin/gopi task sweep-worktrees --dry-run --min-age-h 1
  collectable [run-ground-anchor] /home/user/workspace/gopi-01a114949f6f759186bc63eaaf18e154  the launch context is alive but the run is over: task 01a11494-9f6f-7591-86bc-63eaaf18e154 is completed — a claim-time worker outlives the row it claimed, so this ground is the run's leftovers, not a live run
  keep        [run-ground-anchor] /home/user/workspace/gopi-01a113ac57927bb6950402126e224aee  kept: a live process still stands in the ground (cwd/fd/exe)
  report      [dirty-worktree] /home/user/workspace/gopi-01a114949f6f759186bc63eaaf18e154 (dirty (uncommitted work) — left in place: ?? screen-1791348588.png)
```

两点值得单独记下：

- `01a11494` 的锚点轴变 `collectable`，而**注册表轴仍是 `report`**：地面里有一个
  `?? screen-1791348588.png`（未提交的产物），Rule 30 的闸自己把它挡住了。
  这正是"锚点说可以收"与"目录里还有东西"应该给出的**不同**答案——两个轴各说各的事实。
- `01a113ac` 的锚点轴理由是 `a live process still stands in the ground`（`/proc/<pid>/cwd`
  实测有进程站在里面），**不是** age floor——它与"claim pid 是长命 worker"这条判据已经无关。

## 6. 处置这三块地面（本轮**不删任何目录**）

逐条（判据均为本轮的实测输出，Rule 30 的线：不 commit、不 stash、不删）：

| 地面 | 行状态 | 本轮处置 | 依据 |
|---|---|---|---|
| `gopi-01a114949f6f759186bc63eaaf18e154` | `completed` | **未收回**：锚点轴已 `collectable`，但目录持有 `?? screen-1791348588.png`，`git worktree remove`（无 `--force`）被 git 自己拒绝 | 未提交的产物是工作，不是垃圾；`RunGroundCollect` 的 skip 会引用 git 的原话 |
| `gopi-01a113ac57927bb6950402126e224aee` | `failed` | **明确不动**：`M` 三个 `cmd/agent/*_test.go`（实测不在 master）＋目录里有活进程（`/proc/<pid>/cwd` 实测 `gopi session resume 01a113ac…`） | 唯一副本；救援行 `01a11488` 仍在队列（pending）。锚点轴的理由是"有活进程站在里面"，与 claim pid 无关 |
| `gopi-01a113c5…`（第三块） | `completed` | **已在扫描期间被回收**（目录与 `task/01a113c5…` 分支均已不存在，`removal-audit.log` 无本 arm 的 removal 记录） | 本轮 `--dry-run` 只读，`--apply` 从未执行；**这不是本任务的动作**，如实记录 |
| `gopi-crelease`（`task/crelease`） | 另一活行 | **明确不动**：`escalate [dirty-worktree]`（`M internal/tools/task_parent_gate_test.go`，无行拥有） | 未提交的改动；本任务不碰别人的 worktree（Rule 29） |

## 7. 为什么"多久没人发现"

两道闸是**叠加**的，任何一道单独存在都不会让目录滞留到被注意到：

1. 第一道闸让"run 结束"**没有被上报** → 行停在 `in_progress`；
2. 第二道闸让"run 已经结束"**没有被采信** → 即使行到了终态，地面仍被 keep。

两道都对时，`sweep-worktrees` 每轮都**正确**地输出 `keep`，而理由串是一句听起来合理的话
（"the run … is still here"）——**没有任何计数器、没有任何 exit code 变红**。
本机的实测值：锚点轴 `collectable=1702 kept=294`，也就是说这条缺陷只影响
"claim 它的是长命 worker"的那一部分地面（本机是 3 块），
**小到不会引起注意，大到足以让目录永久留着**。
谁该处理：`gopi task sweep-worktrees` 的操作者（回收动作本来就只在 `--apply` 下发生），
本修复之后它的锚点轴会**在理由串里直接说出账本事实**，不再需要人去翻 `/proc`。

## 8. 锚点（本文件对 master 的可解析断点）

- 两个事实的合成点：`internal/tools/run_ground_anchor.go` 的 `RunGroundAnchorLiveness` /
  `runGroundAnchorLiveness`。
- 启动上下文阶梯（未改动，只改名与理由串）：同文件 `runGroundLaunchLiveness`。
- 账本这一次读：同文件 `groundRowLookup.rowLifeEnded` → `taskByID`
  （`internal/tools/tasks.go`）／`taskSyncDB`（`internal/tools/memory_sync_task.go`）。
- 沿用的既有谓词：`worktreeSweepRowLifeEnded`（`internal/tools/worktree_sweep.go`）、
  `taskCleanupEligibleStatus`（`internal/tools/task_finish_cleanup.go`）。
- 删除动作（不带 `--force`）：同文件 `RunGroundCollect` → `taskGroundGit`。
