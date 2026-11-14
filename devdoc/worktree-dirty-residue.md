<!-- doc-class: normative -->
# 退出时留下 dirty worktree → 永久滞留：三处缺口与处置（2026-10-07）

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

任务 `01a114f3-98fd-7c05-aac1-65d6c7ab8d02`。本文件是**标准叙述（normative）**：
它陈述的规则仍然作用于 master 当前代码，并给出每条断言的锚点行号/命令。

## 0. 一句话

`gopi task run` 的成功臂在**自己的 completed 回写被拒**之后，仍然打印
`task <id> completed (elapsed …)` 并 `return nil`。于是 Rule 30 的残留闸
（正确地拒绝释放 dirty worktree）让目录原地滞留，而行在账本里**仍然是
`in_progress`**——操作者看到的是"完成"，没有任何人再回来处理它。

## 1. 三个缺口（层层递进，缺一不可）

### 缺口 ①：wrapper 成功臂从不回读账本

`tools.TaskRowTerminalAt`（"账本里这一行终态了吗"）在整个仓库只有**一个生产调用点**，
在 `if runErr != nil` 的**失败臂**里（`cmd/agent/cli_tasks.go:2698`）。成功臂在
`writebackOutcome(target, tools.TaskStatusCompleted, …)`（同文件 2717 行）之后**没有回读**，
直接打印并返回 nil。

### 缺口 ②：`writebackOutcome` 丢掉自己的错误

它把 `tools.TaskRunWriteback` 的错误只写 stderr（`writeback failed (continuing)`）就继续；
函数本身**没有返回值**，成功臂因此无从知道回写是否落地。拒绝与成功在 wrapper 内部**无法区分**。

### 缺口 ③：child 的 END-hook 收尾**不可能**补上这个洞

`internal/agent/taskregister.go` 的 `closeOutRunRow` → `tools.TaskRunCloseoutStaleRun` 被
`runCloseoutClaimEvidenceHolds` 挡住：**wrapper 的 claim pid 活着时它返回 false**
（"a live wrapper owns the row: not our close to make"）。而 wrapper 按定义活着——它就是
`cmd.Wait()` 的父进程。所以：**child 退出那一刻，行必然非终态**。

这一条决定了修复的位置：任何"在 child 退出时读账本"的检查都会把**每一条健康 run**
误判为异常死亡。唯一可问的问题是"**我自己的回写落地了吗**"，而它只能在回写**之后**问。

## 2. 实测现场（Rule 31：全部真跑，逐字）

`/home/user/.gopi/memory.db`（只读打开）里三条滞流行 + 其账本事件：

| 行 | 状态 | worktree 残留 | 账本事件 |
|---|---|---|---|
| `01a113ac` | failed | `M` 3 个 `cmd/agent/*_test.go`（+44/−12，**真成果**） | `completed-unclean-tracked-work` + `-refused` |
| `01a113c5` | completed | `?? internal/tools/zz_01a113c5_probe_test.go`（文件头自写 "Deleted before the commit" = 垃圾） | `completed-unclean` |
| `01a11494` | completed | `?? screen-1791348588.png`（1920×1080 截图 = 产物垃圾） | `completed-unclean` |

两条垃圾残留的副本已按字节归档到本会话目录（sha256 见 §5），**源目录未删**（见 §4）。

关键测量：这三行**都**留下了 `completed-unclean*` 事件——闸**确实**响了，
只是响在账本里，而**没有人**读。

## 3. 修复

- 新增 `cmd/agent/task_run_closeout_landed.go`：`taskRunCloseoutLanded`（回读）
  + `taskRunCloseoutEscalate`（升级报告，返回非 nil error）。
- `cmd/agent/cli_tasks.go` 成功臂末尾（2717 行之后）加**最后一道检查**：
  未落地 → 打印 `task <id> was NOT closed out — <原因>` 并返回错误（退出码非 0），
  **不再**打印 completion 行。
- `writebackOutcome` 改为返回本地回写的错误（远端 report-back / push 腿保持 best-effort，
  它们描述的是 stateless remote，不属于本地账本裁决）。

**没有做的事**（Rule 30 的线）：不 commit、不 stash、不清扫、不删任何东西；
不发明第二个状态。被拒的行**停在闸放它的地方**——修复只让退出码、控制台与账本**一致**。

## 4. 为什么"滞留"没有被自动清掉——三条 dirty 的处置结论

**结论：本轮不删任何目录。** 理由不是"忙"，是**证据不足**：

- `01a113ac` 的 `M` 改动**不在 master**（逐文件比对 `git hash-object` vs
  `git rev-parse master:<file>` 三者皆不同），而它的行已被判 `failed`。
  同一形态的救援**已经在队列里**：`01a11488`（"…其中 01a113ac 分支仍活着（唯一可救）"）。
  **在该行把所有者的改动提交/推回分支之前，删目录 = 不可逆地销毁唯一副本。**
- 两个 `??` 是垃圾，但**删它们不会让目录消失**：`01a113ac` 的目录在同一台机器上、
  由另一个活着的进程持有（`/proc/<pid>/cwd` 实测，Rule 24）。工具在此**没有反向保证**：
  删完可能仍是脏的，而删除是唯一不可逆的动作。
- 二者的**清理动作**都已具备条件且不依赖本任务：`01a11488`（救援 `01a113ac` 的成果）
  与 `gopi task sweep-worktrees`（回收已释放的地面）。

**值得单独记下的一条**：`01a11488` 的 brief 把 `01a113ac` 的 `failed` 判为
"报告形态裁决"（harness 要求字面 `Ran:` 行，原报告写成 `**Ran**`，于是整行判 failed、
**跳过 finish-cleanup 链**）。这是与本文同族的第二个"退出路径静默跳步"形态：
`status` 一个字段同时承载"工作失败"与"报告格式未过闸"两种语义，
下游（如 Rule 33 的 parent gate）只能看见 `failed`。

## 5. 证据（自填，Rule 5）

- 修复前（**master 二进制**跑新测试）：`wrapper exit code : 0`、
  `wrapper says 'completed' : true`、`ledger row status after : "in_progress"`、
  `row terminal? : false` → `--- FAIL`。
  逐字：`~/.gopi/sessions/01a114f3-…/evidence/proof-red-prefixbin.txt`
- 修复后：`wrapper exit code : 1`、`wrapper says 'completed' : false`、
  stderr 出现 `task <id> was NOT closed out — the ledger holds "in_progress" …`，
  同时 stderr 仍带 `writeback failed (continuing)`（告警保留）→ `--- PASS`（RED 臂）；
  GREEN 臂（闸未触发）仍 `exit 0` / `completed` / 无升级 → `--- PASS`。
  逐字：`…/evidence/proof-postfix-green.txt`
- 归档的垃圾残留：`…/evidence/foreign-ground-residue/`
  `zz_01a113c5_probe_test.go` sha256 `377fae8a…e8a010`、
  `screen-1791348588.png` sha256 `3f47d0bb…7952cdf`。
- `01a113ac` 的未提交改动补丁：`…/evidence/01a113ac-uncommitted-tracked-work.patch`。

## 6. 第二道闸（本次新发现，超出原修复范围 → 已提交独立任务）

第一道闸修好之后，目录**仍然收不回来**。`gopi task sweep-worktrees --dry-run`
（在 `/home/user/work/GoPi` 里跑，2026-10-07 14:5x）逐字：

```
sweep-worktrees (dry-run): repos=1 reclaimed=0 would-reclaim=0 reaped=0 branches-reaped=0 escalated=0
  report      [dirty-worktree] /home/user/workspace/gopi-01a113ac… (dirty … M cmd/agent/cli_annotate_test.go; …)
  report      [dirty-worktree] /home/user/workspace/gopi-01a113c5… (dirty … ?? internal/tools/zz_01a113c5_probe_test.go)
  report      [dirty-worktree] /home/user/workspace/gopi-01a11494… (dirty … ?? screen-1791348588.png)
  keep        [run-ground-anchor] /home/user/workspace/gopi-01a113ac…  kept: the run that took this ground (pid 3577762) is still here
  keep        [run-ground-anchor] /home/user/workspace/gopi-01a113c5…  kept: the ground is younger than the floor (5h57m0s < 24h0m0s)
  keep        [run-ground-anchor] /home/user/workspace/gopi-01a11494…  kept: the run that took this ground (pid 3577762) is still here
```

账本实测（只读）：`01a113ac` = `failed`，claim `peerbox:3577762`，claim 于 08:45:44；
`01a11494` = `completed`，claim **也是** `peerbox:3577762`，claim 于 12:23:40。
而 `3577762` 是**一个长命 worker 循环**（`cmdline` = `gopi worker`，start 07:53:02，至今活着）。

**判据**：`RunGroundAnchorLiveness`（`internal/tools/run_ground_anchor.go:233`）rung ①
把账本 claim 时的 pid 当作"这块钱的 run 还在跑"。当 claim 它的是一个**长命 worker**时，
这个 pid 在**该 run 结束之后很久**仍然活着 —— 于是这个 worker 历史上 claim 过的每一块钱
都被判 LIVE、永久收不回来，哪怕对应行的 run 早已终结。
`01a113c5` 没被这条锁住，只因它的 claim pid（3509539）恰好死了（走 rung ② 而挂在 age floor 上）
—— **同一缺陷的两个面：锁的成立取决于"claim 的是长命 worker 还是一次性进程"，而账本里两者长得一样。**

与本文同形、但**在另一层**：①让"run 已结束"没有被**上报**；②让"run 已结束"没有被**采信**。
两层叠加 = 目录永久滞留。修 ① 是本文；修 ② 已作为独立 bug 提交：
**`01a1151f-5a0f-7c3a-ac06-736f63717a66`**（含复现判据、候选修法与否决项、
以及"处置这 3 块地面"的明确指令），它按 parent gate 挂在本行之后释放。

**本任务不越界处置的证据**：两块 `??` 垃圾的副本已按字节归档到本会话目录
（§5 sha256），但**源目录与文件一概未动**——因为删除是唯一不可逆的动作，
而它是否足以让目录可回收，取决于第二道闸怎么修。

## 7. 锚点（本文件对 master 的可解析断点）

- 成功臂的最后一道检查：`cmd/agent/cli_tasks.go` 里 `taskRunCloseoutLanded` 调用点。
- 失败臂既有的唯一回读：同文件 `tools.TaskRowTerminalAt`（`runErr != nil` 分支内）。
- 回写错误的唯一出口：`writebackOutcome` 的 `return wbErr`。
- child 侧被阻断的收尾：`runCloseoutClaimEvidenceHolds`（`internal/tools` 的
  stale-run close-out 闸）。
