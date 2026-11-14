<!-- doc-class: historical -->
# 取消句柄发布窗口：round 在飞却无可取消

2026-10-26，任务 `01a175c9`。修 `internal/tools` memory-sync-on-store 家族 4 个稳定失败的测试。

## 症状与它的两次不同读数

bug 报告给出 4 条失败，其中两条不同：

```
--- FAIL: TestStoreTriggeredRoundDiesWhenCancelled (0.03s)   ← 根本没等
    the round spawned no observable child — the cancel would prove nothing
--- FAIL: TestStoreTriggeredRoundDiesWhenCancelled (5.03s)   ← 等满了 5 秒
```

0.03s 与 5.03s 是**两个不同的红**，不是同一个红的不同写法：前者在子进程出现前就读 `/proc`
（读一次、立刻拿到 0），后者是 `storeSyncWaitFor` 的 5 秒轮询打满。安静主机上它是绿的，
负载下必红——这就是"环境依赖的红"的典型形状。

## 根因

`memoryStoreSyncDecide` 在锁里设置 `memoryStoreSyncInFlight = true`，然后解锁、`go` 起 round；
而取消句柄 `memoryStoreSyncRoundCancel` 由 **round 自己的 goroutine**
（`memoryStoreSyncRunChild`）发布。所以：

```
t0  decider: inFlight=true, 解锁, go round   ← 从这里开始,"有 round 在飞"
t1  (窗口)
t2  round goroutine: ctx, cancel := …; 发布句柄
```

**窗口内 `CancelInFlightMemoryStoreSync()` 返回 false。**

实测（n=4，/proc 采样，无负载，Rule 27）：

```
handle at 0.178ms – 1.160ms  ← 句柄就位
child  at 32.6ms – 46.4ms    ← 子进程可见
窗口 32.6 – 46.4 ms
```

## 为什么这不是"测试前提过时"

生产里的同一窗口意味着：`gopi chat` 的 closeout 调 `CancelInFlightMemoryStoreSync()`，
落进窗口 → 返回"没有 round 在飞" → round 继续，带着已结束的 run 跑到自己 30 分钟的界。
**那正是 `7f1e9d7` 要修的孤儿进程，只是被挪早了几毫秒。**

测试里的形态是它的放大版：取消落空 → round 逃出本测试 → **稍后**才发布句柄
（进程级状态，全包共用一个进程）→ 下一个测试的 `TestCancelInFlightMemoryStoreSyncIsANoopWhenNothingRuns`
看到别人留下的句柄，报"reported a cancellation with no round in flight"。

## 修复

1. **句柄由 decider 在锁内发布**，同一段里设 `inFlight`、建 ctx、发布句柄、递增代次，再解锁、
   再 `go`。**round 存在 ⟺ 有东西可取消**，两者不再分叉。窗口被**关闭**而不是缩短。
2. **ctx 穿进去**（`memoryStoreSyncRun` / `memoryStoreSyncRunChild` / `SetMemoryStoreSyncRunner`
   都带 `ctx`）。窗口内到达的取消，落到后来才 spawn 的子进程上，而不是被丢掉。
3. **排队期间被取消的 round 不 spawn**：`memoryStoreSyncRunChild` 开头查 `ctx.Err()`，
   返回 `the round was cancelled before its child started`——不制造"出生即孤儿"的子进程。
4. **代次防误清**（`memoryStoreSyncRoundGen`）：round 收尾只在 `gen` 仍是自己的时候清句柄，
   否则会清掉**下一轮**刚发布的活句柄。
5. **Cancel 按 `inFlight` 判定**，不只看句柄：在飞但无句柄（只能由 seam 造出）报"取消了"。

## 测试侧的对应修正

> **退役批注，2026-10-27 追加（本条以下正文不改）。** 本节点名的
> `memory_store_trigger_cancel_window_test.go`（以及 `internal/tools` 里整个
> memory-sync-on-store 家族）已在 `5e3ce2e` 随
> `internal/tools/memory_store_trigger.go` 一起删除，本节描述的窗口与代次机制在
> HEAD 上不存在。本页是 2026-10-26 那次修复的记录，正文按原样保留（Rule 36）；
> 可通用复用的部分——"发布太晚"那一族并发 bug 的判定方法——与代码是否还存在无关，
> 见文末「复用要点」。当前状态：`devdoc/memory-store-sync-decouple.md`。

- `waitForRoundCancelHandle` **不再顺带等到子进程**（它以前是"等 round 跑到 exec"的代理，
  修复后立刻返回）。新增 `waitForLifetimeChild`：句柄就绪 ⇏ 子进程存在，**测试需要两个事实**。
- `TestStoreTriggeredRoundChildCarriesTheChildWaitBound` 原来断言
  `processAlive(lifetimeChildPID(...))`：重新扫描在 round 结束后会返回 0，而
  `processAlive(0)==false` → **断言偶然通过**（空洞）。改成对 `waitForLifetimeChild` 的 pid 断言。
- `resetMemoryStoreSyncState` 也清句柄并递增代次——脚手架拒绝继承上一测试的状态。
- 新增 `memory_store_trigger_cancel_window_test.go`：**start 返回后立刻取消、不轮询不 sleep**，
  必须 true；在飞但无句柄必须报"取消了"；陈旧句柄必须被脚手架丢弃。
  突变回退产品修复后，它立刻变红。

## 复用要点

- **"发布太晚"是并发 bug 的一个固定家族**：`X 已成立` 与 `X 的句柄/资源已就位` 是两个事实，
  中间那段就是窗口。判定方法不是读代码，是**在 start 返回后立刻取那个资源**——留任何
  轮询/sleep 都会把窗口藏起来而不是关掉。
- 修复方向永远是**把发布移到判定同一段锁里**，不是"加等待"。
- 测试里"等句柄"曾被当成"等 round 跑到 spawn"，这种**代理等待**在修复后立刻失效，
  而且它的失效方式是**确定性的红**（0.02s），不是偶发——所以修复一出，前提错误就暴露了。
