<!-- doc-class: historical -->
# 跑整包 `./cmd/agent` 会把调用者 ground 的 HEAD 切走 —— 到底修在哪一层，以及为什么还要一条**包级**守卫

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

> 任务 `01a17e4b-2b86-7691-9cd1-61c898c61863`（2026-10-29）。基线 `903e54f` 里已经含有
> 三次 close-out clone-ground 修复（`96a853d` / `7925bfd` / `ef09449`），所以这一行的第一件事
> 不是修，而是**用真实命令确认它是否仍复现**。结论：在这条基线上**不复现**；缺的是
> 「跑完之后调用者 HEAD 未移动」这条**包级**守卫。这份文档记两件事——判据的层级，和守卫的层级。

## 一、实测：不复现

在**本 run 自己的 clone ground**（`/home/user/workspace/01a17e4b-…`，`.git` 是目录 ⇒
`GroundModelClone`，分支 `task/01a17e4b2b8676919cd161c898c61863`）里，跑整组
`go test ./cmd/agent/ -run 'TestResume' -count=1`，**同时每 0.2s 轮询一次分支名**：

```
=== BEFORE ===
task/01a17e4b2b8676919cd161c898c61863
903e54fccfb6d8c106cb5277e3a509ee56cb486d
=== run group TestResume, poll HEAD every 0.2s ===
go test exit=0
ok  	github.com/nick/gopi/cmd/agent	38.286s
=== AFTER ===
task/01a17e4b2b8676919cd161c898c61863
switches=0
=== reflog ===
903e54f HEAD@{2026-10-29 03:42:17 +08:00}: checkout: moving from master to task/01a17e4b2b8676919cd161c898c61863
903e54f HEAD@{2026-10-29 03:42:16 +08:00}: clone: from peerbox:/home/user/work/gopi
```

`switches=0`、exit=0、reflog 里除了 ground 自己的诞生那两条之外**没有任何 checkout 记录**。
即三条修复提交在「整组 TestResume」这个形状上已经把门关住了。

（原始输出：`~/.gopi/sessions/01a17e4b-…/evidence/repro-this-ground.txt`。）

## 二、三条修复各自回答什么（为什么它们合起来才够）

| 提交 | 判据 | 位置 |
|---|---|---|
| `96a853d` | 目录是 clone **且**是本 run 被**公告**的 ground（`GOPI_RUN_GROUND`） | close-out 面 |
| `7925bfd` | 下沉到唯一实现点 `FinishCleanupMainCheckoutRestore`，并加「站在 `task/<id>` 上 = 某人的 ground」 | 实现点 |
| `ef09449` | 目录**叶子名**是 uuid（channel 的 `<root>/<row-id>` 形状）+ 配置 root 形状 | 实现点 |

`7925bfd` 的账本地址判据是其中**最不容易被 profile 骗到**的一条：`task/<id>` 是 Rule 6a 说的
「分支名就是本行的地址」，任何 `GOPI_HOME` 都改不掉它。而 `cmd/agent` 的测试恰恰会给子 gopi
一次性 `GOPI_HOME`，所以「路径形状」那一条在 suite 子进程里是瞎的——这正是 `7925bfd` 里
`TestFinishCleanupMainRestoreRecognisesAGroundByItsLedgerAddress` 的存在理由。

## 三、缺的那一条：守卫的层级

既有回归测试是 `TestResumeCloseoutDoesNotMoveTheTreeUnderTest`
（`cmd/agent/cli_resume_session_target_test.go`），它守的是**它自己 drive 的那一次 resume**。
而被测出来的触发形状是 **整组 / 整包**：一个**无关的** case spawn 的子 gopi 走进实现点，
把调用者的 checkout 移走。于是一个按 case 写的断言可以**绿着**，另一个 case 把树挪走。

> 判据是「这个 run 跑完之后，调用者的 checkout 还在原处吗」——这是 RUN 的性质，不是某个 case 的性质。

所以读取必须发生在 `m.Run()` 的两侧，即 `TestMain`。新增
`cmd/agent/suite_caller_head_guard_test.go`：

- `armSuiteCallerHeadGuard()` —— `m.Run()` **之前**取一次快照：`checkout` / `branch` / `head` / `porcelain`；
- `reportSuiteCallerHeadAtCloseOut(before)` —— `m.Run()` **之后**再读一次，比较**三个属性**：
  分支名变了、HEAD 指向的 commit 变了、原本干净现在脏了。任何一个动了就返回非零，
  `TestMain` 把它变成 **suite 的 exit code**（与既有 leaked-child sweep 同一个形状）。
- 消息里**打印 SWITCH 前后的分支名**（`SUITE CALLER-HEAD GUARD: SWITCH %s → %s`），这就是
  「修复前会 FAIL 并打印分支名」那条验收要求的落点。

### 为什么**不**把它挪回去

守卫只读、只报、**不修**。两个理由：挪回去会**藏起是哪个 case 干的**（而 locating 那一个
正是这条守卫的价值）；而且那个 checkout 未必是本进程有权动的（Rule 29）。

### 为什么还需要 `TestSuiteCallerHeadGuardIsArmed`

一条被 `TestMain` 调用才生效的守卫，删掉接线就**静默失效**——而失效的守卫和干净的 run
在输出上无法区分。这是个已经付过两次代价的真空形状（Rule 36 / Rule 42 的 "vacuum"）。
所以接线本身是一条断言：`armSuiteCallerHeadGuard()` 没被调用时它红。

## 四、红证（两次，各自 mutate 后还原）

**红证 1 —— 接线**（删掉 `armSuiteCallerHeadGuard()` 调用）：

```
--- FAIL: TestSuiteCallerHeadGuardIsArmed (0.00s)
    suite_caller_head_guard_test.go:195: the caller-HEAD guard was never armed: TestMain must call
    armSuiteCallerHeadGuard() before m.Run(), or nothing in this package checks that the checkout it
    runs in is still on its branch afterwards
```

**红证 2 —— 比较器**（把分支比较改成 `if false &&`，即「对分支移动失明」）：

```
--- FAIL: TestSuiteCallerHeadGuardReportsAMovedHead (0.04s)
    suite_caller_head_guard_test.go:247: the guard reported NO movement for a checkout that went
    "task/fixture-caller-head-guard" → "master" — a comparator blind to this is the vacuum that let
    eleven REFUSED arms read as a code verdict
```

还原判据用**显式 revision**，不用 index-relative diff（Rule 5 的 restore 判据）：

```
$ test "$(git hash-object cmd/agent/suite_caller_head_guard_test.go)" = \
       "$(git rev-parse HEAD:cmd/agent/suite_caller_head_guard_test.go)" && echo RESTORE_PROVEN_BY_BLOB
RESTORE_PROVEN_BY_BLOB
```

## 五、绿证

```
SUITE CALLER-HEAD GUARD: the caller's checkout held still
  (/home/user/workspace/01a17e4b-… on task/01a17e4b2b8676919cd161c898c61863 at 903e54fccfb6)
ok  	github.com/nick/gopi/cmd/agent	6.105s
```

—— 注意这一行是**在真实 ground 里**读出来的：它报的是调用者自己那个目录，不是任何 fixture。
另有 `TestSuiteCallerHeadGuardCallsAnUnmovedCheckoutUnchanged` 作绿对照：没动的 checkout 必须
报 0，否则「永远报非零」的比较器会冒充一条能用的红证。

## 六、这条守卫覆盖不到什么（如实）

它只能看见**跑这个包期间**调用者 checkout 的移动。它看不见：`-run` 之外的其它包、
`go test ./...` 一次跑多包时**别的包**的 case 移走的 checkout、以及在 suite 之外
（`gopi worker` 收尾、sweep tick）发生的同一种移动。那些走的是同一条实现点
（`FinishCleanupMainCheckoutRestore`），由 `internal/tools` 的三条判据把守，不是这条守卫的事。
