<!-- doc-class: normative -->
# close-out sweep 采到一个仍在飞行的仪表 —— scratch GC tick 的 join

Task: 01a13431968c7602b48fd2daf1aa6cc2 · 分支 `task/01a13431968c7602b48fd2daf1aa6cc2`
· 基线 `e7f3bf2`（不是 master）。

## 结论（一句话）

那一帧**不是泄漏，是采样时机**：`scratchGCSweepAsync` 是本进程自己的常驻仪表
（进程退出由内核回收，没有"泄漏"可言），但它的 stop 只 **signal**、不能 **join** ——
所以 close-out 在它飞行中采样时，dump 里和一条真泄漏长得一模一样。
修法 = 让 tick 可 join，close-out 先 join 再采样；**sweep 的门槛一个字节没动**
（没有按帧名排除，没有放行名单）。

## 根因链（5-why）

1. 症状：一条**全绿**的 -run 集合，整包 exit 1，stream 里有一条**没有 `Test` 字段**的
   `{"Action":"fail"}`。
2. 机制：`testmain_test.go` 的 close-out
   （`reportSuiteGoroutinesAtCloseOut`，testmain_test.go:1053）数到 parked goroutine
   就把 `code` 置 1。
3. 为什么 dump 里有它：`cli_tasks_worker.go` 的 round 里 `scratchGCSweepAsync(workerRoleLabel)`
   **故意把 sweep 放到调用者 goroutine 之外**（2026-10-03 的缺陷：sweep 的 /proc 扫描
   12–18s，而 drain 只在 round 顶部被消费，于是 `--restart --wait 20s` 等在一个已经
   消费了请求的 daemon 上）。异步是**设计**，不是 bug。
4. 为什么它在 swep 采样时还在：`scratchGCSweepAsync` 返回 `void`，**没有任何可等待的
   句柄** —— 调用方无法表达"等它结束"，所以 TestMain 的下一条指令（采样）必然可能
   撞上它。这是**能力缺失**，不是竞态运气：只要 sweep 还在飞，采样就看得见它。
5. 根因：**一个只能被 signal、不能被 join 的 stop，会让下游每一次 close-out 测量说谎**
   （同形先例：`startSuiteStallWatchdog`、`installFileWatcher` —— memory #5593 / #10548）。

## 判据（为什么不许调松 sweep）

- 本帧的**身份**是仪表，不是被测物：它在 worker loop 的生命周期里常驻，进程结束即回收。
  "让它别被数到"的两种廉价做法（按帧名加白名单 / 把阈值放宽）会**同时**把真泄漏放行 ——
  而 sweep 的全部价值就是"测试结束后还有谁在跑"。
- 正确的区分不是"帧名",而是**"采样时它还在不在飞行"**：把"可等待"这件事补上，采样就
  只可能看见**没人认领的工作**（真泄漏），飞行中的仪表会在采样前结束。
- 所以修法只能是：**让 tick 可 join**。sweep 的判定逻辑、阈值、帧收集规则**零改动**。

## 修法

`cmd/agent/scratch_gc_tick.go`：

1. `scratchGCSweepState` 增加 `done chan struct{}` —— **飞行句柄**：非 nil ⟺ 有 sweep
   goroutine 存在。
2. `scratchGCSweepAsync(role) func()` **返回 join**，并把同一句柄发布成
   `scratchGCSweepInFlightDone() <-chan struct{}`（给需要"带自己的上限等待"的调用方）。
3. goroutine 的 **defer 最后一句**才 `close(done)`，且关的是**本次飞行自己的局部句柄**
   （不是共享字段）—— 于是 "join 返回" ⟺ "goroutine 已结束"，连 defer 里那段慢扫描告警
   (`os.Stderr`) 与闩锁更新也一起被等掉。共享字段只在锁内、且 `if scratchGCSweepState.done == done`
   时被清掉。
4. single-flight 的 no-op 分支**也返回同一个 join**（一个飞行中的 tick 不是"不可等待"）。
5. `joinScratchGCSweepFlight()`：幂等、任何 goroutine 可调、空闲时立即返回；
   `resetScratchGCSweepFlight()`：测试用，把 `running` 与 `done` **一起**清掉
   （只清 running 会让后来的 joiner 等在一个陈旧的、可能已关闭的通道上），它**不取消**
   已经在跑的 sweep（没有谁能让别人的 goroutine 结束），只是不再把它发布给新 joiner。

**坑（实测，`panic: close of nil channel`）**：不许让 goroutine 去 close **共享闩锁字段**
（`close(scratchGCSweepState.done)` 在把字段置 nil 之后）—— 另一个写者（测试重置闩锁、
或下一轮 tick）可能已经把字段替换/清空，完成路径就会 close 一个 nil 或别人的通道。
本 run 第一版正是这么写的，`after-fix` 那一跑整包 panic（`goroutine 519`，
`scratch_gc_tick.go:438`）。关**捕获在起点的局部 `done`** 才安全。

`cmd/agent/testmain_test.go`：close-out 在数 goroutine **之前**先
`joinResidentSweepBeforeSampling()` —— **等的是"静默"，不是一个句柄**：
循环直到「没有发布中的飞行句柄」**且**「goroutine dump 里没有 sweep 帧」，
总上限 `suiteSweepFlightJoinWait`（30s）。**超时不是放行**：超时只打印一行"采样照旧"，
随后那次采样若看见 sweep 帧，它仍然被计成 parked、仍然 exit 1（一把卡死的 sweep 是缺陷，
必须报）。没有飞行时该函数立刻返回。

**为什么"等一次 join"不够（实测）**：第一次 `after-fix` 跑出来仍是 parked 1，帧是
`scratch.ScanHoldersWithin ← SweepSandboxesDetailed ← scratchGCTick`（同一个 tick 的另一个
arm）—— 因为 tick 从**本 suite 在进程内驱动的 worker loop** 里发出：飞行 N 一结束，
下一轮的 `scratchGCSweepAsync` 立刻起飞行 N+1，dump 照样看见它。所以要等**静默**：
"没有发布中的句柄"∧"dump 里没有帧"，而不只是"等一个句柄"。

`cmd/agent/cli_tasks_worker.go`：调用点把返回的 join **显式丢弃**并写明理由 ——
**在 round 边界等 sweep 就是 2026-10-03 那个缺陷本身**，异步性不许被这次修复带走。

`cmd/agent/scratch_gc_tick_flight_test.go`（新增，回归）：
`TestResidentSweepFlightIsJoinable`，用包内**已有**的帧扫描器
`goroutinesRunning(t, frame)`（chat_boundary_test.go:502，**不新增计数器**，Rule 14），
把飞行窗口做成确定的（复用既有的 `scratchGCSweepBody` 缝）：

- ① 起步：`scratchGCSweepAsync` 必须返回非 nil join；body 必须真的跑到。
- ② **负向对照**：飞行中**必须看得见**该 goroutine —— 否则 ④ 的"0"与"扫描器什么都看
  不见"无法区分。
- ③ **差值断言**：body 还被 hold 住时 join **不许返回**（实现成 signal/no-op 会在这里
  确定性变红：通道没被放行不是时序巧合）；放行后 join 必须返回。顺带断言 single-flight
  下并发 body 数 = 1、飞行中到达的 tick 返回的句柄仍然非 nil。
- ④ 绿：join 返回后扫描到 0 个该帧（重试到自己的上限，因为"goroutine 退出"与"dump 看
  得见"不是同一瞬间；**close-out 本身不重试**，它靠的是 join）。
- ⑤ 空闲面：没有飞行时 `scratchGCSweepInFlightDone()` 为 nil、`joinScratchGCSweepFlight()`
  立即返回（否则 close-out 会挂在空闲进程上）。

## 红证 / 绿证（同一命令、同一 -run 集合）

命令（两处一字不差，stream 见 session 目录）：

```
go test -count=1 -timeout 1200s -json -run '^(TestRunTaskWorker_OnceSweepsZombieFirst|TestRunTaskWorker_SurvivesFailedRun|TestWorkerIdleExitsEnv|TestWorkerSelfExitsAfterIdleRounds|TestWorkerBusyRoundResetsIdleStreak|TestWorkerIdleExitZeroWaitsForever|TestWorkerWaitsAndPicksUpNewTaskWithoutRestart|TestWorkerLoopDoesNotWaitOnTheResidentSweep|TestInstallFileWatcherIsUninstallable|TestChatBoundarySignalWatcherIsJoinedOnExit|TestReproWatcherBackspaceDoesNotEditTheLine)$' ./cmd/agent/
```

| | 修复前（基线 e7f3bf2） | 修复后（本分支） |
|---|---|---|
| stream | `baseline-clean.json` | `after-fix.json` |
| 9 个 test | 全 pass | 全 pass |
| `parked repository goroutines:` | **1** | **0** |
| `grep '"Action":"fail"' \| grep -vc '"Test"'` | **1** | **0** |
| exit | **1** | **0** |
| parked 帧函数 | `scratch.readProcFile`（飞行中的 sweep） | （无） |

同一集合再加本 run 的新回归测试（`after-fix-plus.json`）：12 个 test 全 pass，
parked 0、无 Test 的 fail 0、exit 0。

**注意计数是时序敏感的**：同一集合在不同 -timeout/调度下实测见过 3/6/7。所以判据只用
"同一命令同一集合"的前后差值，不引用别处记的数字。本次基线复现到的是 1（更早一次
实测是 7：6 帧 watcher + 1 帧 sweep，取证材料见
`~/.gopi/sessions/01a133e3-24e2-73f0-a514-bd0f0700bb23/artifacts/scratch-parked-frame.md`）。

**join 的定向红证**（回归测试自己咬得住）：把 `joinScratchGCSweepFlight` 的 `<-done`
改成 `_ = done`（即修复前的 signal-only 形状），
`go test -run '^TestResidentSweepFlightIsJoinable$' ./cmd/agent/` 在同一 git 树形状下
由 `ok` 变 `FAIL`（③ 的断言："the join returned while the sweep body was still running"），
恢复后 sha256 与变异前逐字节相同、再跑 `ok`。这一条的判据是**差值**，不是绝对计数：
只断言"join 之后 0 个 goroutine"的话，body 由测试自己放行，signal-only 实现也会绿。

## Rule 27 / Rule 30

- 没有造任何负载：窗口由既有的包内 seam（`scratchGCSweepBody`）做成确定的，
  没有 dd / while / 并发压测；sweep 的时长由宿主决定，测试不对它断言时间。
- 收尾 `git status --porcelain` 为空（Rule 30）。

## 观察到的第二处（按 Rule 41 提行，本 run 未顺手改）

`cmd/agent/cli_worker_control.go:2167` 的 `installFileWatcher` 在这条基线上**仍然只有
signal、没有 join**（`return func() { close(stopped) }`），调用点是
`defer workerCtx.installFileWatcher()()`。它的 fix 是 `exited` 通道 + `sync.Once` + join，
**不在这条基线上**；落到 master 的那一枚是 **`ba7592966`**
（`fix(cmd/agent): both stops JOIN — the watcher's uninstall and the resident sweep's flight`
—— 本文档本身就是那一枚写下的）：

```
$ git merge-base --is-ancestor ba7592966 e7f3bf2 && echo yes || echo no
no
$ git merge-base --is-ancestor ba7592966 e7f3bf2 ; echo "exit=$?"
exit=1          ← 真的"不"：rev 解得开，祖先关系查过，答案是否
```

（对照下面记的 `b2366a62a`：同样是 `no`，但隔离出来是 **exit=128** —— 那是"没查成"。）

**本节原先引的 fix rev 已更正（2026-10-14 实测，hosta）**。本节曾把该 fix 记成 commit
`b2366a62a`（`fix(worker): the file watcher gets an exit channel, and its installer joins it`），
并记录了一条 `git merge-base --is-ancestor b2366a62a e7f3bf2` → `no`。**那条记录在本机
不可复核，而且它把两件不同的事混进了同一个 `no`**：

- **实测**：`b2366a62a` 在**本仓库（hosta 的克隆/工作树）根本不解** ——
  `git cat-file -t b2366a62a` → `fatal: Not a valid object name`（**exit 128**）；
  `git cat-file --batch-all-objects --batch-check=… | grep -c '^b2366'` → **0**；
  `git rev-list --all | grep -c '^b2366a62a'` → **0**。
- 所以那个 `no` **不是"不是祖先"，是"rev 解不开"**：`git` 自己 exit **128**，
  而 `&& … || …` 把它吞成 `no` + 复合命令 exit **0**。它和"真查过祖先关系"长得一模一样，
  实际是"没查成"—— 判据是 `git merge-base … ; echo $?` → **128**。
- **该 rev 的真实身份**：它是 **peerbox 对端对象库**里的一枚悬挂对象 ——
  `ssh peerbox 'git cat-file -t b2366a62a'` → `commit`，`git show -s` 元数据与父提交
  `3544b690c` 都在，而 `git for-each-ref --contains b2366a62a` → **0 个 ref**、
  `git rev-list --all` 命中 **0**（**实测**：对象在库里 1 命中、可达 ref 0 个）。
  **"不被任何 ref 指向 ⇒ 不随 fetch/push 过桥 ⇒ hosta 永远看不到"是推断**，
  实测到的只有两侧的命中数。
- **同形但不同一枚**：`b2366a62a` 与 `ba7592966` 是同一目标的两版实现，
  同一文件 diff 的 `git patch-id --stable` 不同（`a2362019…` vs `72c49a7d…`）。
  所以本节原来的**事实主张也不完整**：修复确实落了地，落地点是 master 上的
  `ba7592966`，不是一枚悬挂对象。
- 原始输出：`~/.gopi/sessions/01a1355d-d0d3-7a1c-8c1b-ebb4325e74b7/artifacts/` 下的
  `evidence-b2366a62a-unresolvable.md`（本仓库侧）+ `evidence-peer-peerbox-b2366a62a.md`（peerbox 侧）。

**同段引用的"已提行/附件"在本机同样不可复核（同日实测）**：task 行
`01a1343d-d9c2-793c-a5e2-29538e340c09` 在 hosta 账本里没有
（`gopi task show 01a1343d-…` → `no task with task_id=…`），附件路径
`~/.gopi/sessions/01a13431-968c-7602-b48f-d2daf1aa6cc2/artifacts/bug-installfilewatcher-no-join.md`
在 hosta **不存在**（peerbox 上**也不存在**）。**推断**（未确认）：该行与附件产生于另一个
ground/session，从未落进本机账本，也未随附件同步过桥 —— 只记事实，不改下面两句的语义。

已提行：**task 01a1343d-d9c2-793c-a5e2-29538e340c09**（tags: bug），附件 =
`~/.gopi/sessions/01a13431-968c-7602-b48f-d2daf1aa6cc2/artifacts/bug-installfilewatcher-no-join.md`
+ 本次基线 stream。该行要的是**先确认那条分支能不能合**——不要两个分支各修一次同一处。
本 run 的 -run 集合没抓到这个形状（本次那 1 帧是 sweep），所以该行的复现步骤按其附件里的
命令执行。

## Not Done / 诚实边界

- 本 run 只复现到 **1** 帧（sweep）；6 帧 watcher 的形状来自更早一次运行的取证材料，
  本 run 没有复现它 —— 复现需要那条更早的 -run 集合。所以"修完这一处，这条集合就
  不会再出 watcher 帧"**未在本 run 证明**，watcher 行独立跟踪。
- `after-fix` 是**单次**运行（`-count=1`）。本帧的成因是"能力缺失"而不是概率竞态：
  只要 sweep 在飞，采样就可能看见它；单个样本的绿 + 定向红证（signal-only 变异必红）
  足以支撑判定，但没有做 N 次重复。
