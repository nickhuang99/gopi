<!-- doc-class: normative -->
# 死 workspace 当 repo：提交缝修复 + 拒绝文案按真实原因分支

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

任务 `01a15629-5082-7ab7-8607-2e20d2889c43`（epic 01a15629）。
修复对象：bug 01a1515c 的**第三半**。

## 现象（实测，不是推断）

三行真死的行，在 clone 通道被同一句话拒绝：

```
clone channel: repo "/home/user/workspace/01a1510e-9d29-7dca-b03b-9c7f899a533e"
  declares no build command in settings repos → …\.build — refusing to push a
  workspace that was never built (declare the command; it is never guessed)
```

（`gopi task show 01a154de-b196-761b-8e0d-18a51e07bc76` 的 `reason:` 字段就是这一句。）

而这句话是**假的**：那个目录不存在了。取证：

```
$ ls -d /home/user/workspace/01a1510e-9d29-7dca-b03b-9c7f899a533e
ls: cannot access '…': No such file or directory
$ gopi task show 01a1510e-9d29-7dca-b03b-9c7f899a533e | grep deliverable
deliverable.repo: /home/user/work/gopi
deliverable.branch: master
deliverable.worktree: /home/user/workspace/01a1510e-9d29-7dca-b03b-9c7f899a533e
```

也就是说：**父行自己的 deliverable 里写着活仓库**（`deliverable.repo = /home/user/work/gopi`），
而它那次 run 的 workspace（`deliverable.worktree`）在 push 落地后被回收了。
子行继承了 `deliverable.worktree` 那个路径当自己的 repo —— 一个既不存在、也没有 origin
可问的目录。于是「目录已死」被诊断成「没有 build 声明」，把操作员指向 settings 文件，
而真正的修复在**行**上（`gopi task update <id> --repo <path>`）。

## 为什么旧的 trace 回退救不了它

`internal/devworkspace/trace.go` 的回退（bug 01a1515c 第二半）问的是
`git remote get-url origin` —— 需要目录还在。目录没了，回退只能报
`… is not a git checkout (no .git)`，然后调用方仍然吐那句「declares no build」。

## 三处改动

### (a) 提交缝：死 workspace 的 durable-repo 修复

新增 `internal/tools/task_submit_durable_repo_repair.go`：

```go
func RepairDurableRepoForSubmit(repo string) (string, string)
```

判定链（每一环都是一个**事实**，不是猜测）：

1. `config.WorkspaceRootConfigured()` —— 沿用**已有的唯一读者**（Rule 1/13/14，不新建读者）。
   路径不在 workspace 根之下 → **原样返回，不 stat、不 git、不出声**。
2. 路径存在且 `taskRepoVerifiable` 通过（**已有的唯一校验器**）→ 原样返回。
3. 死路：去账本里找 `deliverable.worktree == 该路径` 的行，取那一行的 `deliverable.repo`；
   它必须是活 git 树 → 返回它 + 一行话（`TaskRepairDeadWorkspaceFmt`，含死路径、根、活仓库、行 id）。
4. 找不到行 / 那一行的 repo 也不活 → **原样返回**，并行一句话说明（`TaskRepairDeadWorkspaceMissFmt`）。
   绝不发明仓库（Rule 12 的同一条精神）。

**幂等**：返回值是活仓库，第二遍必走第 2 步 → identity + 静默，所以每个缝都能无脑调用。

接进三条自动提交缝（都**只在失败臂**上调用，所以正常路径零成本）：

| 缝 | 位置 | 触发条件 |
|---|---|---|
| 引擎默认 repo | `internal/tools/tasks.go` `taskSubmitResolveDefaultRepo` | `DurableRepoForSubmit(cwd)` 失败 |
| 引擎显式 repo | `internal/tools/tasks.go` `TaskSubmitReturningID` | `DurableRepoForSubmit(opts.Repo)` 失败 |
| 工具的预解析 | `internal/agent/loop.go` `case toolnames.ToolTaskSubmit` | 同上，失败时把 note 附进 tool 结果 |
| reframe 子行 | `internal/agent/reframe.go` `reframeChildRepo` | 每次（幂等；workspace 外的 repo 零开销） |

**只移动子行的值，不改正父行**：父行的 deliverable 是它那次 run 的真实记录，回收、
sweep、`task show` 都还需要看到 workspace 这个事实。在分解流程里改写一条已完成 run 的
记录，是拿别人的历史去换自己的方便。

### (b) 拒绝文案按真实原因分支

`internal/devworkspace/trace.go` 新增（Rule 1b：文案形状只有一个家）：

```go
const (
    RepoPathMissingReason = "the directory no longer exists on disk"
    RepoPathNotGitReason  = "the directory exists but is not a git work tree (it carries no .git entry)"
)
func RepoPathReadable(dir string) (bool, string)   // 纯 stat，不起 git 进程
```

`cmd/agent/devworkspace_channel.go` 的 `resolveCloneChannelBuild` 在查 settings **之前**先问
「这个路径能不能承载一个 build 声明」，不能就用**另一句**：

```
const RepoRefusalDeadPathFmt = "clone channel: repo %q is not usable: %s"
```

后面接 `— refusing to push a workspace that cannot be built (re-point the row at a live git
work tree: gopi task update <task_id> --repo <path-to-live-work-tree>)`
（remedy 用的是 `tools.TaskRepoRepairVerb` 这个**唯一拼写**），并同样写进通道日志。

**「declares no build command」那句话没有删，也没有改字**：它对「目录真在、settings 真没声明」
的情形依然逐字成立，`TestCloneChannelRefusesAnUndeclaredRepoOutsideTheRootVerbatim` 与
`TestCloneChannelRefusesWhenNeitherTheRepoNorItsSourceDeclaresABuild` 的逐字断言照旧通过。

### (c) 正常仓库逐字不变

`RepoPathReadable` 对活仓库返回 `true` 且**不 spawn git**；
`RepairDurableRepoForSubmit` 对 workspace 外的路径**连 stat 都不做**。
两条都由测试在**进程边界**上度量（PATH 上装计数 shim），不是从源码推的。

## 红证明（三组，全部实测）

| # | 变异 | 结果 |
|---|---|---|
| ① | `RepairDurableRepoForSubmit` 开头塞 `return repo, ""`（修复惰性） | `TestRepairDurableRepoRecoversADeadWorkspaceFromItsRow` FAIL；`TestReframedChildRepairsAParentRepoThatWasReclaimed` FAIL |
| ② | `RepoPathReadable` 分支改成永真（`if readable, why := true, ""; !readable`） | `TestCloneChannelNamesADeadRepoPathAsDead` 两个子用例 FAIL，**并且失败的正是那句假话**：`got: clone channel: repo "…" declares no build command in settings repos …` |
| ③ | 恢复后 | 两个包全绿；`grep -c MUTATION` = 0 |

①的两条红一起出现，说明它们测的确实是修复本身而不是夹具的巧合。

## 真跑取证（Rule 31，构建出的二进制）

sandbox：`/home/user/workspace/gopi-sandbox/sandbox-deadrepo-dea372fb`（`gopi tool sandbox create`）。

**A. 死路径的行，跑 clone 通道（新的真实原因）**

```
$ $SB/gopi worker 01a15629-aaaa-7bbb-8ccc-000000000001
clone channel: repo "/…/fix/workspace/01a15629-aaaa-7bbb-8ccc-000000000001" is not usable:
  the directory no longer exists on disk
gopi task: 01a15629-aaaa-…: clone channel: repo "…" is not usable: the directory no longer
  exists on disk — refusing to push a workspace that cannot be built
  (re-point the row at a live git work tree: gopi task update <task_id> --repo <path-to-live-work-tree>)
  — refusing to start the run in the repo's main checkout; …
EXIT=1
```

**B. 父行 workspace 被回收后，子行提交（修复那一跳）**

```
$ rm -rf fix/workspace/01a15629-dddd-7eee-8fff-000000000004        # 父 workspace 已回收
$ $SB/gopi task submit "split child inheriting a reclaimed workspace" \
     --task-id 01a15629-eeee-7fff-8000-000000000005 \
     --repo …/fix/workspace/01a15629-dddd-7eee-8fff-000000000004
task_submit: repo "…/fix/workspace/01a15629-dddd-7eee-8fff-000000000004" no longer exists under
  the workspace root "…/fix/workspace" — repaired to …/fix/source, the repository row
  01a15629-dddd-7eee-8fff-000000000004 recorded for that workspace (its deliverable.worktree)
Task queued: 01a15629-eeee-7fff-8000-000000000005 (status=pending, …)

$ $SB/gopi task show 01a15629-eeee-7fff-8000-000000000005 | grep deliverable.repo
deliverable.repo: …/fix/source          ← 活仓库，不再是那条死路径
```

**C. 正常仓库（workspace 外、已声明）的 run 不变**

```
$ $SB/gopi worker 01a15629-dddd-7eee-8fff-000000000004
task …: clone channel ON (…)
clone-channel: cloned …/fix/source → …/fix/workspace/01a15629-dddd-…-000000000004
clone-channel: workspace ready at … on branch master
```

没有 trace 行、没有 repair 行、没有新的拒绝 —— 与改动前逐字相同。

## 没做的（如实）

- **已存在的那批死行没有自动改回**。修复发生在提交缝，改的是新提交的行；
  `01a154de` 这类历史行仍然停在 `deliverable.repo = /home/user/work/gopi`（`task show`
  显示它今天其实指向活仓库），它们的 `reason` 字段里那句假话也**保留**，因为它是
  取证记录。回收历史行要走 `gopi task update --repo`，那是操作员的决定，不是本任务的。
- **origin 那条路走不通，已放弃**：目标目录已经不存在，`git remote get-url origin`
  问不了；所以恢复来源是**账本里那一行的 deliverable**，不是 origin。这是本任务与
  原始 briefing 的一处偏离，理由是实测：`ls` 都失败时 origin 无从谈起。
- 没有改 `internal/devworkspace` 的回退顺序、没有新增配置键、没有新增脚本/定时任务
  （Rule 35）。
