<!-- doc-class: normative -->
# worktree 生命周期体系 4/5：调用者普查与「只删零读者」的裁决

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

**系列**：worktree 退场系列 4/5（阶段 3 见 `devdoc/chat-cleanup-authorization.md`）。
**基线**：`3c6dad3`。**范围**：`internal/tools/worktree_sweep*.go`、
`internal/worktreerelease`、`internal/worktreeroots`、`internal/scratch/worktree_stub.go` +
`worktree_root_residue.go`、`internal/standards/worktree_root_gate.go`。

---

## 0. 一句话结论

**这个体系的绝大部分不是「旧通道专属代码」，而是两条通道共用的目录生命周期设施**。
普查把 263 个符号逐个对到读者上，只有 4 个零读者（已删），其余 259 个
每一个都有本文件之外的活读者。因此本阶段的删除量是**符号级**（4 个声明、23 行），
不是文件级；硬删文件会同时删掉 clone 通道正在走的路径——这正是载荷里
「不得误删 clone 通道也在用的路径计算/释放逻辑」要拦的事。

## 1. 「两条通道」到底是什么

| 通道 | 触发 | 工作区从哪来 | 代码 |
|---|---|---|---|
| worktree 通道 | `settings.dev_repo` 为空 | `git worktree add -b <branch> <root>/<task-id>`（linked worktree） | `internal/tools/task_run_ground.go` |
| clone 通道 | `settings.dev_repo` 非空（**本机即是**：`peer1:/home/user/work/gopi`） | `devworkspace.Open` → 独立 clone 到 `workspace_root/<task-id>` | `internal/devworkspace` + `cmd/agent/devworkspace_channel.go` |

关键事实（**MEASURED**，`git worktree list` / `git remote -v` 于本 run 工作区）：
本 run 自己的工作区 `/home/user/workspace/01a150f4-…` **是一个 clone**
（`origin peer1:/home/user/work/gopi`，`git worktree list` 只有它自己），
不是 linked worktree。也就是说：**本阶段的验证就发生在 clone 通道上**。

## 2. 为什么这些包在 clone 通道下仍然必须活着

`worktreeroots`（roots.go 427 行 + scratch_pin.go 123 行）：38 个符号里 **30 个**有集外生产读者，
散布在 45 个包里（`internal/config`、`internal/scratch`、`internal/daemoncwd`、
`internal/sandbox`、`internal/testutil`、`cmd/agent`…）。它回答的问题不是
「worktree 放哪」，而是**「这条路径在不在一个已声明的根里」**——clone 通道的
workspace 正落在 `workspace_root`（`/home/user/workspace`）之下，判据同一个。
`internal/devworkspace` 自己不 import 它（`grep worktreeroots. internal/devworkspace/` 为空），
但 `cmd/agent` 与 `internal/config` 在 clone 通道的准备/收尾两侧都用它。
**判定：保留。** 改造方向见 §5。

`worktreerelease`（129 行）：5 个符号里 3 个有集外生产读者 ——
`ReleaseReporting`（`cmd/agent/cli_tasks.go`、`cli_resume.go`、`cli_worker_resume.go`、
`internal/agent/taskregister.go`；`merge_run.go` 这个读者已随顶层 merge 动词于
2026-11-01 消失）、`ProcessRootPath`
（`internal/tools/task_reclaim_self.go`、`task_resume_ground.go`、`cli_tasks_retry_delivery.go` (deleted with the verb)）、
`TestProcessExecutableSuffix`（`cmd/agent/cli_daemon_spawn.go`）。
它做的是「进程退出时把自己 cwd 里的登记释放掉」，与工作区是 worktree 还是 clone **无关**
——clone 通道的 run 崩溃同样要释放。**判定：保留。**

`internal/standards/worktree_root_gate.go`：这是「禁止把 `/dev/shm`、`/tmp`、
`~/workspace` 字面量写死在代码里」的 AST 闸门，检验的是**存活代码的写法**，
不检验通道。它 16 个符号里唯一有集外读者的是 `testFileSuffix`（被同包的测试面共享）。
**判定：保留**——删掉它，等于把 2026-09-13 那次 tmpfs 事故的回归路径重新打开。

`internal/scratch/worktree_stub.go` + `worktree_root_residue.go`：
32 + 20 个符号，生产读者集中在 `cmd/agent/cli_scratch_gc_stubs.go` 与
`cli_scratch_gc_roots.go`（两个 operator 动词）以及 `internal/uiclient`。
它们回收的是 scratch 根下**任何**工作区形状（stub / residue / foreign），
不是 worktree 通道的产物。**判定：保留。**

`internal/tools/worktree_sweep*.go`：见 §3，它是唯一**部分**可删的面。

## 3. sweep 面的普查结果（唯一有删除的面）

`worktree_sweep.go`(2729) + `worktree_sweep_chat.go`(448) + `worktree_sweep_memory_base.go`(216)
共 183 个符号。判据不是「能不能跑到」，而是**「删掉它，外面还有谁编译不过」**：

| 组 | 符号数 | 集外生产读者 | 裁决 |
|---|---|---|---|
| `worktree_sweep.go` | 147 | 30 | 保留（1 个零读者已删） |
| `worktree_sweep_chat.go` | 26 | 3 | 保留（2 个零读者已删） |
| `worktree_sweep_memory_base.go` | 10 | 4 | 保留 |

有集外读者的 30 个里，真正跑语料的是「拿 `git worktree list` 当**读工具**」的那一层：
`worktreeSweepRegistrations`、`worktreeSweepCreatedAt`、`worktreeSweepRoots`、
`worktreeSweepCommonGitDir`、`worktreeSweepMainCheckoutOf`、`worktreeSweepTaskRows`、
`worktreeSweepSelfPosition`、`worktreeSweepRowLifeEnded` —— 读者是
`task_reclaim_self.go`、`task_resume_ground.go`、`task_ground_reconcile.go`、
`guest_row_guard.go`、`branch_attribution.go`、`run_ground_anchor.go`。
后四个是 clone 通道也在走的收尾/归属判断（例如 `TaskRunGroundForClone`
经同一个 anchor 记录器落账）。**删 sweep 会连带删掉这些读者脚下的实现。**

已删的 4 个（**零读者**：本文件外无编译单元读它，测试也不读）：

| 符号 | 定义 | 为什么是零读者 |
|---|---|---|
| `worktreeSweepPlan` | worktree_sweep.go | 决策阶段包装器，唯一动作是调用 `worktreeSweepJudge`；所有调用点早已直接调 `worktreeSweepJudge` |
| `worktreeSweepRepoSettingsKey` | worktree_sweep.go | repo 表经 `config` 读取，这个字面量没有读者 |
| `worktreeSweepChatGroundDirPrefix` | worktree_sweep_chat.go | 只是 `TaskGroundDirPrefix` 的别名，无人读 |
| `worktreeSweepChatFreshDirMarker` | worktree_sweep_chat.go | 一个没有任何路径会发出的 reason 串 |

`-23 行 / +0`，`go build ./internal/tools && go vet ./internal/tools` 干净。

## 4. 「只旧通道走到」的判定为什么在 sweep 上不成立

`gopi task sweep-worktrees` 是**operator 动词**，不是通道的一部分；
`cmd/agent/task_lifecycle_tick.go` 的第四条腿（`serveWorktreeReapSweep`，
APPLY 模式）也在同一份实现上跑，而这条 tick 在 clone 通道下照跑
（tick 不看 `dev_repo`）。它回收的对象是「git 里登记了但没人要的 linked worktree」：
clone 通道**不产生**这类登记，但**也不禁止** operator 手工 `git worktree add`
或历史遗留登记存在，删除实现会让这些登记永久无人回收（`worktree_reap_reach_test.go`
钉住的正是「这条腿必须可达」）。**判定：保留动词 + 保留实现，把「clone 通道下
sweep 的收益面收缩」写成后续任务（§5）。**

## 5. 保留面的改造方向（本阶段只记录，不改）

1. **`worktreeroots` 的命名已与语义脱节**：它现在回答的是「路径是否在已声明根内」，
   而名字说的是 worktree。建议改名为 `declaredroots`（或保留类型、只改包注释），
   并把 `WorkspaceDirName` / `RoleWorktree` 的角色表按「工作区形状」而不是
   「worktree」重组。改名的成本是一次 45 包引用面替换，需要在 clone 通道全绿的
   一棵树上单独一个 commit。
2. **`worktreerelease` 真正缺的不是删除而是统一**：它按 cwd 释放进程登记，
   而 clone 通道的 workspace 收尾由 `devworkspace` 的 `finish`/`exhausted` 负责。
   两条收尾路径并存会留下「cwd 已释放、workspace 未释放」的窗口。
   后续任务应先画出两条路径的时序图，再决定是否把 clone 收尾接进 `ReleaseReporting`。
3. **sweep 的收益面收缩**：clone 通道下 `git worktree list` 通常只有主检出，
   sweep 的主体（登记×账本配对）几乎恒空。可考虑把 `serveWorktreeReapSweep`
   改成「主机上仍有 linked 登记时才跑」（一次 `git worktree list` 计数，
   零登记即跳过），省掉每个 tick 的 git 进程。这是行为优化，不是删除。
4. **`worktree_root_gate` 应与 clone 通道的新字面量对齐**：闸门现在的规则集只认
   `/dev/shm`、`/tmp`、`~/workspace`；clone 通道引入了 `workspace_root` 设置项，
   后续应把「裸写 workspace_root 的值」也纳入同一闸门。

## 6. 残余 grep（本阶段结束时的真值）

```
$ grep -rn "sweep-worktrees\|worktreerelease\|worktreeroots" --include=*.go . | wc -l
（见任务报告 §7 的实测输出 —— 全部是活代码的引用，无一可删）
$ grep -rn "worktreeSweepPlan\b\|worktreeSweepRepoSettingsKey\|worktreeSweepChatGroundDirPrefix\|worktreeSweepChatFreshDirMarker" --include=*.go .
（空）
```
