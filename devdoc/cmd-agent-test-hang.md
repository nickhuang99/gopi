<!-- doc-class: historical -->
# devdoc: `go test ./cmd/agent/` 的 600 s 收尾 —— 不是 hang，是预算；以及 watchdog 的固定预算误报

任务 `01a113b1-d668-7b95-922f-b7e31379b0fe`，2026-10-07，peerbox。
完整证据链与逐条 `Ran:`：`~/.gopi/sessions/01a113b1-d668-7b95-922f-b7e31379b0fe/task-report.md`。

## 1. 症状与其真实性质

```
$ go test ./cmd/agent/ ./internal/standards/ -count=1     # 简报原命令，不带 -timeout
FAIL	github.com/nick/gopi/cmd/agent	600.668s
```

600.668 s 是 Go 的默认 `-test.timeout`（10 m）。**"整数级 600 秒"是预算耗尽的
形状，不是挂起的证据**——挂起与跑不完在这条命令上不可区分，这正是本任务要
分开的两件事。

## 2. 为什么不是 hang（四条独立证据）

1. **四次运行各自指名不同的测试**。`panic: test timed out` 的 `running tests:`
   行每次给出另一个名字（`TestNotifyDeliverWakesAnIdleChatSessionWithNoInput`、
   `TestNotifyDeliverReachesARunningChatSession`、
   `TestBoundCloseoutWaitStaysAliveWhileTheRunKeepsProgressing`、
   `TestVersionReportDoesNotBlameACheckoutTheBuildStoodIn`）。凶手不会换人。
2. **被指名的测试单独跑是绿的**：`TestNotifyDeliverWakesAnIdle…` 在整包运行里
   `--- PASS (32.04s)`，单独跑 35.68 s PASS。它是"最后一个启动的"，不是停住的。
3. **时间账**：同一次 10 m 运行里测试自身耗时合计 **580.23 s / 600.42 s**
   （96 %），差值 ≈25 s 是构建+套件初始化。没有"什么都不做"的空窗。
4. **没有外来 pipe 持有者**：逐 `/proc` 全量扫描只列出测试二进制自己（fd 1/2）
   与 `go test` 父进程（fd 7）。`os/exec.(*Cmd).writerDescriptor.func1()` 那一帧
   属于 `versionFixture.build()` 里一个**正在正常构建**的子进程——旁观者。

## 3. 真正的原因：包预算是调用者的选择，而这条命令没选

本仓库对这件事早有定论，写在唯一声明的入口里
（`scripts/test-budget.sh` 头部）：

```
# `go test` ... Go's default is 10 minutes. On this host that default is not a
# hang detector, it is a load detector: ... takes >600 s when other agents'
# suites are running (measured: 1123.8 s at load 42-63)
test_budget="25m"
```

⇒ 裸 `go test`（不带 `-timeout`）在重的机器上**必然**以 600 s 收尾。这是
Rule 15 说的那种失败：FAIL 文本读起来像断言失败，实际什么都没证明。

## 4. 顺带修掉的真实缺陷：stall watchdog 的固定预算

`cmd/agent/suite_stall_watchdog_test.go` 的 watchdog（2026-09-16 加，动机与本次
完全一致）用常量 `suiteStallBudget = 8 * time.Minute` 做预算。**固定值只对一个
调用者成立**，于是两边都错：

- **长预算 → 误报**：`GOPI_SUITE_STALL_BUDGET_SEC=240` 的健康运行（PASS 计数
  当时仍在增长）打出 `suite stall: … running 4m0s (watchdog budget 4m0s)`；
  25 m 默认下同样在 8 m 打出。
- **短预算 → 沉默**：`-timeout 60s` 时 8 m 永远到不了，verdict 一句不打。

**修法**：预算与 tick 都从调用者的 `-test.timeout` 派生。

```go
suiteStallBudgetFlagPercent = 90      // 预算 = -test.timeout 的 90%
suiteStallTickDivisor       = 10      // tick = 预算 / 10（含下限）
suiteStallBudgetFrom(flagTimeout)     // 0（超时被禁用）→ 回退常量
suiteStallTickFor(budget)
```

**为什么是"比例"而不是"另一个常量"**：watchdog 唯一的存在理由是它的结论必须
**先于** Go 的 `panic: test timed out after <budget>`（那个栈不指名成因）。
固定值只在某一个预算上"刚好提前"；比例在**任何**预算上都是"刚好提前"。

**tick 也必须派生**——本轮自己踩出来的：只派生预算时，`-timeout 60s` 导出的
54 s 预算要等到固定 15 s tick 的**下一个倍数** 60 s 才被检查，
`panic` 先到，verdict 缺席。加 `suiteStallTickFor` 后同一条命令：

```
174: suite stall: this test binary has been running 54s (watchdog budget 54s) …
231: panic: test timed out after 1m0s
```

## 5. 否证掉的两个候选（简报里点名的）

- **"子进程/孙子进程持有 stdout 写端，io.Copy 等不到 EOF"**：否证。见 §2.4。
- **"故意的 fd 200 继承"**：否证。`daemon lifetime: armed — read end fd 200 is
  inherited …` 是**读端**，写端由测试二进制本身独占（进程自杀则全家死）；
  `/proc/<testbin>/fd/200` 是 pipe 读端，不可能是"孙子持有写端"的来源。

## 6. 未做 / 待决

- 未给包加 `TestMain` 级默认超时来让"裸 `go test`"也安全：那是更宽的改动，
  且会与 `-test.timeout` 语义打架。本仓库的答案是走 `scripts/test-budget.sh`。
- `TestVersionReportDoesNotBlameACheckoutTheBuildStoodIn`（要构建一个版本
  二进制）是否该列入 heavy（`heavytest.Require`）：未决，见任务报告的
  `## Not Done`。
- `internal/standards` 的 3 条红门归 `01a1139d`，本任务未动；基线里的
  CLI 重组相关 FAIL 归命令行重组任务。
