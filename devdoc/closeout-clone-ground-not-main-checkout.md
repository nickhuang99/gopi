<!-- doc-class: historical -->
# closeout 的 main-checkout 还原腿把「本次 run 自己的 clone ground」当成共享主 checkout

> 任务 `01a17e6759f77c52bfa2150b154a2a2b`（2026-10-28）。这份文档记录一个实测缺陷的
> 症状、根因、判据与兼容性，供以后读这一行代码的人不必再挖一次。

## 症状（实测）

在**本 run 自己的 clone ground**（`/home/user/workspace/01a17e67-59f7-7c52-bfa2-150b154a2a2b`，
`.git` 是目录 ⇒ `devworkspace.GroundModelClone`）里跑复现命令：

```bash
cd /home/user/workspace/01a17e67-59f7-7c52-bfa2-150b154a2a2b
git rev-parse --abbrev-ref HEAD      # task/01a17e6759f77c52bfa2150b154a2a2b
go test -run TestAutoTaskRegisterOnAndEndHook ./internal/agent
git rev-parse --abbrev-ref HEAD      # master   ← 被切走了
```

原始输出（修复前）：

```
BEFORE: task/01a17e6759f77c52bfa2150b154a2a2b
ok  	github.com/nick/gopi/internal/agent	0.473s
AFTER: master
9ef4e74 HEAD@{0}: checkout: moving from task/01a17e6759f77c52bfa2150b154a2a2b to master
9ef4e74 HEAD@{1}: checkout: moving from master to task/01a17e6759f77c52bfa2150b154a2a2b
9ef4e74 HEAD@{2}: clone: from peerbox:/home/user/work/gopi
```

即：一条**活的 run** 在自己的工作目录里被切到了 `master`。修前本 ground 的 reflog 里
累计多条 `moving … to master`，全部由这一次次复现命令产生。

## 根因（5-why）

1. **谁切的分支**——`FinishCleanupRestoreMainCheckout`（`internal/tools/main_checkout_restore.go`），
   它调用 `FinishCleanupMainCheckoutRestore(ctx, repo, IntegrationBranchForRepo(repo))`，
   后者执行 `git checkout <integration>`。
2. **为什么它以为自己在动共享主 checkout**——它拿到的 `repo` 是
   `finishCleanupMainRepoDir(repo)` 的结果，而 `mergeExecMainRepo` 对一个 **clone** 会返回
   该 clone 自己（`--git-common-dir` 就是它自己的 `.git`）。所以「主 checkout」被解析成了
   **本 run 的 ground**。
3. **为什么两道既有守卫都没拦住**——
   - 守卫①「交付分支被某个 LINKED worktree 占着」：clone 的注册表里没有它
     （`finishCleanupBranchWorktree` 只读 `git worktree list`，clone 自己只列自己，
     且第一项总被当作 main 跳过），返回 `""`；
   - 守卫②「行记录了一个不是主 checkout 的 ground」：`worktree` 参数是**空串**
     （auto 注册行不记录 `deliverable.worktree`），条件
     `w != "" && !finishCleanupSameWorktreePath(main, w)` 直接短路为假。
4. **为什么恰好是这条路**——clone 通道的 auto 行把 `deliverable.repo` 记为**声明源**仓库
   （`dev_repo` → `/home/user/work/gopi`），而 run 实际站在 `<workspace_root>/<row-id>`；
   ground 本身由 `GOPI_RUN_GROUND` 公告给本进程。账本里的 repo 与「这条 run 真的站哪」
   是两个不同的事实，而该腿只读了**前者能解析出的目录**。
5. **更底层**——「主 checkout」是一个**关于共享性的判断**，却用
   「`--git-common-dir` 指向谁」来回答，而这个问题对 clone ground 的答案就是它自己。
   缺的不是一个检查，是**一个判据**。

## 判据

`.git` 的种类 ⇒ ground 模型，由 `devworkspace.GroundModelOf` 唯一裁决（Rules 13/14）。
但**「它是 clone」不足以取消资格**：仓库的**共享主 checkout 本身也是 clone**
（`~/work/gopi/.git` 是目录）。真正取消资格的是「它是**本 run 被公告的那个 ground**」——
`TaskGroundAnnounced()`（`GOPI_RUN_GROUND` / `GOPI_RUN_GROUND_BRANCH` 的唯一读取者，Rule 2）。
那个目录是**每个 run 一份、用完即弃**的，没有任何合并路径消费它。

新增判据函数（`internal/tools/main_checkout_restore.go`）：

```go
func finishCleanupCloneGroundSkipReason(dir, branch string) string
```

    model := devworkspace.GroundModelOf(dir)
    if model != devworkspace.GroundModelClone { return "" }        // 不是独立 clone：不关这条腿的事
    announced, _ := TaskGroundAnnounced()
    if announced == "" || !finishCleanupSameWorktreePath(announced, dir) { return "" }  // 不是本次 run 的公告 ground
    return <拒绝句>

接线位置：`FinishCleanupRestoreMainCheckout` 里**紧跟 `main := finishCleanupMainRepoDir(repo)`
之后、两道既有守卫之前**（这两道守卫对一个 clone ground 按构造就问错了问题）；
以及 `finishCleanupMainCheckoutPlannedStep` 里，使 **plan 与 apply 一致**（apply 拒绝的
目录，plan 不预测移动）。

返回 `ok=true` + 一句 detail：这与「LINKED worktree 不关这条腿的事」是同一种语义——
**不是拒绝，是否定问题本身**；同时 detail 让这一行进入行记录的 steps，操作员能读到
「有一个 parked 主 checkout 没有被还原，因为这里指的是本 run 自己的 workspace」
（Rule 15：沉默会让这个问题以后无法回答）。

## 兼容性

| 目录形状 | 是否本 run 公告的 ground | 修复后行为 |
|---|---|---|
| LINKED worktree | — | 不变（守卫①照旧跳过） |
| **CLONE（共享主 checkout，如 `~/work/gopi`）** | 否（或没有公告） | **不变：照旧还原到集成分支** |
| **CLONE（本 run 的 workspace）** | 是 | **新增：不移动，给出具名理由** |
| 非 git 目录 / 无 `.git` | — | `GroundModelMissing` ≠ clone，不受影响 |

回归测试的反向臂 `TestFinishCleanupRestoreMainCheckoutStillRepairsTheSharedCheckout` 就是
第二行：同一个 fixture（同样是 clone，`GroundModelOf` 同样报 clone），只把公告指向别处，
还原行为必须完全照旧。没有这一臂，「拒绝 clone」会退化成把真主 checkout 的修复一起取消。

## 证据

红 proof（生产修复的两处 guard 调用**删掉**、常量与 helper 保留，所以能编译、红是行为红）：

```
--- FAIL: TestFinishCleanupRestoreMainCheckoutLeavesTheRunsCloneGroundAlone (0.03s)
    main_checkout_clone_ground_test.go:78: the clone ground was moved to "master" (want it left on "feat/parked-by-a-run") — the close-out switched a LIVE run off its task branch
--- FAIL: TestFinishCleanupPlanPredictsNothingForACloneGround (0.02s)
    main_checkout_clone_ground_test.go:130: the plan predicted {Step:main-checkout … Detail:would on feat/parked-by-a-run, would put the main checkout back on master} for a clone ground the apply declines
FAIL	github.com/nick/gopi/internal/tools	0.572s
```

绿（同一组测试，修复在位）：

```
ok  	github.com/nick/gopi/internal/tools	0.520s
```

真 ground 上的复现命令（修复后）：

```
BEFORE: task/01a17e6759f77c52bfa2150b154a2a2b  reflog_lines=8
ok  	github.com/nick/gopi/internal/agent	0.458s
AFTER: task/01a17e6759f77c52bfa2150b154a2a2b  reflog_lines=8
```

## 第二次绕过：通告（announcement）只在"被通告的那个进程"里成立

上面那版修复把判据放在**通告**上（`TaskGroundAnnounced`：本进程是不是被 runner 交付了
这个 ground）。它挡住了自己那一行，但**同一小时内又被绕过一次** —— 因为通告是一对
**环境变量**，只有 runner 亲手 spawn 的那个进程读得到。换一个进程去动同一个 ground，
通告就答"不是我的 ground"。

实测（本 ground 的 post-checkout hook，2026-10-28T19:30:01）：

```
pid=2049361 cwd=/home/user/workspace/01a17e4b…  prev=b52e00c… new=b7e575c… flag=1
  2049334: git checkout master
  2049284: …/test-budget/run-…/gopi --prompt RP1-BOUNDARY-PLAIN-PROMPT-4f2a …
  2008494: …/agent.test -test.run=TestRunPathPlainPromptIsVerbatim
```

—— 一个**无关的 `cmd/agent` 测试** spawn 的子 gopi。它从不走 close-out 那层
（`FinishCleanupRestoreMainCheckout`），而是**直接调实现点**
`FinishCleanupMainCheckoutRestore`（sweep tick 与 merge executor 也直接调它）。

### 判据一：守卫下沉到唯一的实现点

三个调用者（`repo_sweep.go:1266`、`main_checkout_restore.go` 的 close-out 面、
`cmd/agent/merge_run.go:2244`）都经过 `FinishCleanupMainCheckoutRestore`，
所以在**那一处**加守卫才覆盖全体。**在调用者上加守卫，是其他调用者能绕开的守卫。**

### 判据二：`task/<id>` 就是账本地址，任何 profile 都改不掉

只看路径也不够。第一版下沉后的形状判据问的是"这个目录是不是**配置的 workspace root**
的**直接子目录**"，而**配置的 root 是每个进程自己的**：`cmd/agent` 的测试给子 gopi 一个
一次性 `GOPI_HOME`，于是真实的 ground 在它眼里就是"一个普通 repo"。实测
2026-10-28T19:50:56：

```
pid=2754764 cwd=/home/user/workspace/01a17e4b…  prev=0d839e4… new=b7e575c…
  2754729: git checkout master
  2754497: …/test-budget/run-…/tested-bin/gopi --prompt RP1-BOUNDARY-PLAIN-PROMPT-4f2a …
  2674138: go test -count=1 -timeout 45m -json ./cmd/agent
```

所以第二个判据问**这个 checkout 自己**：它站在 `task/<id>` 上（`TaskGroundBranchPrefix`，
Rule 6a 说的那条"分支名就是本行的地址"），那它就是**某个人的 ground**，不是这条腿可以搬的
主 checkout —— 与进程配置了什么 root 无关。

### 两条判据是互补的，不是重复的

| 判据 | 能看见 | 看不见 |
|---|---|---|
| 路径（`cloneWorkspaceDirShape` + `GroundModelOf`） | ground 站在**行声明的分支**上（如 `feat/…`）的情形 —— 那条分支不是账本地址 | 进程的 root 是临时目录时 |
| 账本地址（`task/<id>` 前缀） | 进程的 root 是临时目录时（suite 子进程） | ground 站在 `feat/…` 上时 |

两条各自都有专属的红证臂（`TestFinishCleanupMainRestoreLeavesACloneGroundOnAFeatureBranchAlone`
与 `TestFinishCleanupMainRestoreRecognisesAGroundByItsLedgerAddress`），因为**任何一条单独
存在时都有一种实测过的绕过形状**。

### 本轮取证

```
go build ./...                        → BUILD_OK
go test ./internal/tools/ -run 'CloneGround|MainCheckout|FinishCleanup|Restore' -count=1
                                      → ok  github.com/nick/gopi/internal/tools  31.519s
```

红证（逐个 mutate 后还原，每次还原后 grep `if false &&` 归零）：

```
# 账本地址判据
--- FAIL: TestFinishCleanupMainRestoreRecognisesAGroundByItsLedgerAddress
    detail="restored the main checkout: task/01a17e4b… → master in …"
# 路径判据
--- FAIL: TestFinishCleanupMainRestoreLeavesACloneGroundOnAFeatureBranchAlone
    detail="restored the main checkout: feat/declared-by-the-row → master in …/01a17e4b…"
```

---

## 追加（任务 `01a1811f-3f9e-7366-9112-80f99a251f04`，2026-10-28）：第三条判据 —— 目录名

上面两条判据**可以同时为假**，而 ground 仍然是真的。这是本任务实测到的第三个绕过形状：

| 事实 | 上面两条判据各自的回答 |
|---|---|
| 进程配置的 `workspace_root` 是别处（一次性 `GOPI_HOME`） | 路径判据 → **false** |
| ground 站在行**自己声明的分支**上（`devworkspace.Config.Branch`，如 `feat/…`，`devworkspace.Open` 每次复用它） | 账本地址判据 → **false** |

两个 false 同时成立时，修前实现照样走到 `git checkout master`。红证（本 run 的
`evidence/03-residual-hole-red.txt`，探针已删除，永久形状留在测试里）：

```
=== RUN   TestZZProbeResidualCloneGroundMove
    path guard=false  ledger guard=false
    ok=true detail="restored the main checkout: feat/declared-by-the-row → master in …/002"
    RESIDUAL HOLE: the restore moved the ground "feat/declared-by-the-row" → "master"
--- FAIL: TestZZProbeResidualCloneGroundMove
```

### 第三条判据：目录名就是行的 id

`devworkspace.Dir(root, taskID)` = `<root>/<the row's full id>`，所以**每次 run 的 ground
的叶子目录名是一个 canonical task id**（`fullid.IsUUID`）。三条判据里**只有这一条的事实
长在文件系统上**，而不在某个 profile 里 —— 没有任何 `GOPI_HOME` 能改掉一个目录的名字。

反向也被钉住（`TestFinishCleanupMainRestoreStillRepairsACheckoutThatIsNotAGroundName`）：
共享主 checkout 的叶子是 `gopi`，不是 uuid，所以真正的修复照旧发生 —— 否则"名字像 ground"
会悄悄变成"永不还原"，一台停在错分支上的主 checkout 再也没人修。

| 判据 | 事实住在哪里 | 单独存在时看不见的形状 |
|---|---|---|
| 路径（`cloneWorkspaceDirShape` + `GroundModelOf`） | 进程的配置 | root 是临时目录时 |
| 账本地址（`TaskGroundBranchPrefix`） | 分支名 | 行声明了自己的分支时 |
| **目录名（`fullid.IsUUID` on `filepath.Base`）** | **文件系统** | （三者同时为假时仍为假：名字被人工改掉——那条路不存在，行 id 由账本写） |

### 本 run 的取证（真实 clone ground，`task/01a1811f…`，HEAD `7925bfd`）

```
证据 02：make test-budget PKGS='./internal/tools'   → TEST_EXIT=0（3688 例，420s）
          HEAD_BEFORE = HEAD_AFTER = task/01a1811f3f9e7366911280f99a251f04 @ 7925bfd
          reflog 无新增 checkout 条目；post-checkout 钩子日志为空
证据 04：两条新臂在修前红（`detail="restored the main checkout: feat/… → master"`），
          修后绿
```
