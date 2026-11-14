<!-- doc-class: historical -->
# `cmd/agent` 的包级 fail：stop 必须 JOIN，而且它是**两个**成因

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

**Task:** `01a133e5-6013-7b39-97a7-dbe3a9b7a9eb` · **分支** `task/01a133e560137b3997a7dbe3a9b7a9eb`
· **Ground tip（基线判据）** `b888461`（本 ground 由 peer master 切出，**不是**本地 `master`）
· 前序：`devdoc/package-level-fail-two-causes.md`（Cause 1 nil `*sql.DB` 已修、Cause 2 已定位）

## 结论

一条 `{"Action":"fail"}` 而**没有 `Test` 字段**的行 = 包级裁决：测试二进制没跑到逐测裁决。
`cmd/agent` 的 `TestMain` 在 `m.Run()` 之后**测量**进程（子进程 sweep + goroutine sweep），
数到 parked repository goroutine 就把 `code` 置 1 —— 所以「close-out 那一刻还有谁在栈上」
成了一个**验收面**。本次把它归零，靠的是把**两个**独立的 stop 从 SIGNAL 改成 **JOIN**，
外加一处 close-out 前的静默等待；**sweep 的判定/阈值/帧规则一个字节没动**（没有按帧名排除、
没有白名单 —— 那是把缺陷盖住，Rule 15/35）。

| # | 被修对象 | 形状 | 为什么被测到 |
|---|---|---|---|
| 1 | `workerDaemonCtx.installFileWatcher`（`cmd/agent/cli_worker_control.go`） | `close(stopped)` 只 signal | in-process 驱动 worker loop 的测试**会返回**，返回后 close-out 采样到仍 RUNNABLE 的 poll 帧 |
| 2 | 常驻 scratch sweep 的飞行（`cmd/agent/scratch_gc_tick.go`，`scratchGCSweepAsync`） | 返回 `void`，**没有可等待的句柄** | sweep 故意跑在调用者 goroutine 之外（12–18s 的 /proc 扫描不许挡在 `--stop` 与消费它的 round 边界之间），飞行中与「真泄漏」在 dump 里**长得一样** |

两个成因同形，一句话：**一个只能被 signal、不能被 join 的 stop，会让下游每一次 close-out 测量说谎。**
同形先例是 `startSuiteStallWatchdog`（2026-09-25，`parked repository goroutines: 1` 指向 suite 自己的仪表）。

## 修法 1 —— watcher 的 uninstall JOIN（本 run 的主线）

```go
func (c workerDaemonCtx) installFileWatcher() (uninstall func()) {
	if !c.consumesRequests() {
		return noopWorkerFileWatcherStop
	}
	done := make(chan struct{})     // 请求
	exited := make(chan struct{})   // goroutine 的「已离开」
	var once sync.Once
	go func() {
		defer close(exited)
		t := time.NewTicker(workerFileWatchInterval)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				c.consumeDrainRequests()
			case <-done:
				return
			}
		}
	}()
	return func() {
		once.Do(func() { close(done) })
		<-exited
	}
}
```

- `close(done)` 只让 goroutine **RUNNABLE**；`<-exited` 才是「它已经离开栈」。
  `uninstall()` 返回之后帧必然不在，而 `TestMain` 的下一条指令就是采样。
- `sync.Once` 只包 `close`（第二次调用不再 panic），**join 在 once 之外** —— 每个调用者都要等。
- 值接收者不影响正确性：两个通道都在函数体内创建，是**引用类型**，启动方与关闭方保证同实例
  （briefing 约束 6）。
- 惰性路径返回**具名** `noopWorkerFileWatcherStop`，不是内联 `func() {}`：调用方可以无条件 defer，
  而「装了才会等」这件事有一个可指认的对象。

## 修法 1b —— 调用点的 defer 形状（一个真实的陷阱）

`cmd/agent/cli_tasks_worker.go` 的两个调用点改成：

```go
stopFileWatcher := workerCtx.installFileWatcher()
defer stopFileWatcher()
```

不是 `defer workerCtx.installFileWatcher()`（少一个括号，读者一个 typo 的距离）。实测（`artifacts/defer-semantics.txt`，
一段 5 行的独立程序，`defer install()()` 与 `defer install()` 对照）：

```
install / body-a / uninstall / body-b / install     ← b() 的 defer 只装了，从不卸
```

也就是说：少一个括号 = **装完就不管**（常驻进程里后果是**多一个** poll，进程退出即回收；测试里是泄漏），
而当前写的 `defer …installFileWatcher()()` 恰好等价于「装 + 卸」，只是把两件事挤在一行里、且 teardown 在栈里没有名字。
改成命名局部变量后两种错法都读得出来，`defer` 行也不再能在不改变语义的前提下被「简化」掉。

## 修法 2 —— scratch 飞行可 join（拉入同批工作，同一 run 验收）

`eabca844d`（同批 run `01a1343` 的产物，**当时没有任何 ref 指向它**，见下）：

- `scratchGCSweepAsync(role) func()` 返回本次飞行的 join，single-flight 的 no-op 分支返回**同一个**飞行 join；
- `testmain_test.go` 的 close-out 先 `joinResidentSweepBeforeSampling()` 等**静默**（无发布中的句柄 ∧ dump 无 sweep 帧，
  上限 30s），超时只 WARN、**采样照旧**（sweep 帧仍算 parked、仍 exit 1）。

## 常驻语义：逐字未变（论证）

- **生产路径从不调用 uninstall。** `gopi worker`（前台 loop）与 `gopi worker <task-id>` 在**进程退出**时结束；
  `installFileWatcher` 起的 poll 由此被内核回收 —— 和修之前完全一样。join 只对**会返回**的调用方（本包的测试）
  产生行为差异，而那种调用方在生产里不存在。
- poll 自身行为未动：仍是 `time.NewTicker(workerFileWatchInterval)`（2s）循环、每次 tick 调
  `consumeDrainRequests()`，直到进程死。通道没有被关，退出条件只有 `done`（只有 uninstall 会触发）。
- 这一点由 `TestFileWatcherIsResidentUntilStopped` 以**presence**（不是计时）钉住：跨两个 tick 之后 poll
  **仍在**栈上，且只在 uninstall 时才结束。
- scratch 侧同理：sweep 仍跑在调用者 goroutine 之外、节流未动、失败仍 fail-open；新增的只是「谁想等就能等」。

## 判据不许调松（为什么不许改 sweep）

sweep 的价值就是「测试结束后还有谁在跑」。两种廉价做法 —— 按帧名加白名单、把阈值放宽 —— 会**同时**
放行真泄漏。正确的区分不是帧名，而是「采样那一刻它还在不在飞行」：把**可等待**补上，采样就只可能看见
**没人认领的工作**。所以验收断言的是**差值**（`before` → `after`），不是「阈值以下」。

## 验收与证据

| 面 | 命令 | 结果 |
|---|---|---|
| 红（本 run 的 tip，pristine） | `go test -count=1 -timeout 200s -run 'TestWorkerSelfExitsAfterIdleRounds' ./cmd/agent/` | `parked repository goroutines: 1`（scratch 飞行帧），`FAIL … EXIT=1` |
| 红（watcher 面，pristine，×5） | 同上 | parked 1（watcherFrames=0 —— 见下「诚实」节） |
| 绿（同一命令） | 同上 | `ok … EXIT=0`，无 sweep 行 |
| 绿（另两条被点名的测试） | `-run 'TestWorkerLoopDoesNotWaitOnTheResidentSweep'` / `'TestWorkerBusyRoundResetsIdleStreak'` | 各自 `ok`、无 sweep 行、EXIT=0 |
| 绿（矩阵 ×5） | 上述三条 + 三条新回归 + `TestResidentSweepFlightIsJoinable`，跑 5 次 | 全绿、每次 `parked 0` |
| 整包（唯一验收面） | `make test-budget PKGS='./cmd/agent'` | 见 task-report 的整包一节 |

红/绿/整包的逐字输出在 session 目录（Rule 37）：
`~/.gopi/sessions/01a133e5-6013-7b39-97a7-dbe3a9b7a9eb/artifacts/` ——
`red-half-pre.stream`（红）、`green-*.stream`（绿）、`armC-*.stream` / `armD-*.stream`（变异）。

## 变异（red proof：拆掉任一半，回归测试必须变红）

| 手臂 | 变异 | 期望 | 实测 |
|---|---|---|---|
| C | 调用点退回 `defer workerCtx.installFileWatcher()`（少一个括号） | `TestWorkerLoopLeavesNoWatcherPollBehind` 红 | 见 `armC-caller-drops-teardown.stream` |
| D | `uninstall` 去掉 `<-exited`（保留 `sync.Once`），即回到 SIGNAL | `TestInstallFileWatcherIsUninstallable` / `…LeavesNoWatcherPollBehind` 红 | 见 `armD-signal-only-uninstall.stream` |

两臂各自打了**定点**的洞（一次只拆一样），并且都用 `cmp` 与 keep 副本对照证明**还原**（Rule 5 的 restore 判据）；
`git status --porcelain` 在收尾时为空（Rule 30）。

## 诚实（Not Done / 边界）

- **另一个 run（`01a1343d-…`）的 commit 也修 watcher，且它的 `-run` 集合当时没红。** 本 run 在 pristine 基线
  （`git checkout` 还原本 run 的补丁）上跑同一命令连续 5 次，**没有**采到 `installFileWatcher` 帧
  （`watcherFrames=0`），每次都只采到 scratch 飞行帧。原因未定论（该 run 的 `-timeout 600s` 比本 run 的 200s
  长，sweep 的 12–18s 成本与采样时机都受影响；**这是推断，不是判据**）。因此本 run **不把 watcher 的红 proof
  建立在自己的 tip 上**：watcher 面用**变异**证明（手臂 D 把 join 拆掉后回归测试变红），并在条目的验收里逐字记下
  「pristine 上未复现」。
- **`eabca844d` 当时没有任何 ref。** 实测 `git branch -a --contains eabca844d` 空、
  `git show-ref | grep eabca844d` 空、`remotes/gitpushcheck/master` = `b888461`（本 ground 的切点，不含它）——
  它的 commit 对象在共享对象库里，但没有任何 ref 指向它（`git log --all` 找不到）。本 run 按 sha `cherry-pick -n`
  取入。这不是「合并别人的分支」：它没有分支可合并。
- **另一个 run 的 `2b102c979` 未完成的一条**：它的 commit message 写「Callers are unchanged: both faces already
  hold the install return」——那描述的是它的**测试**只覆盖 uninstall 本身；本 run 因此补了
  `TestWorkerLoopLeavesNoWatcherPollBehind`（驱动真实 `runTaskWorker`，断言运行期有 poll + 返回后无 poll），
  因为只测 uninstall 的话，「调用点停止装/停止卸」两个方向都不会被测到。
- 本 run 未改 `installFileWatcher` 的观察点（`consumeDrainRequests` 的语义、ticker 间隔、锁）——不在验收面内。

census-evidence-ok: 本文所有数字都是**一次 run 的自身记录**（命令 + 逐字输出 + 路径），不含全项目统计量。
