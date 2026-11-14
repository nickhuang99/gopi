<!-- doc-class: historical -->
# 被 cap 拒绝的 verify closure：从一行 stderr 到可审计 + 可补发（2026-10-23, task 01a163fb）

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

上一行的 `devdoc/closure-cap-counts-in-flight-work.md` 修的是**计数口径**（cap 拿历史行数比较，
于是永远满额）。这一段修的是**另一边** —— cap 的口径当时是对的、饱和也是真的，但**拒绝本身
不留痕迹**：关闭照常完成、exit 0、账本上没有一个字，而没有任何 pass 会再想起这行。
一条在队列饱和期间关闭的 bug 行，就此**永久失去独立验证**（Rule 15：静默失败）。

## 缺陷形状（实测）

```go
if n, err := inFlightClosureCount(ctx, db); err == nil && n >= taskClosureMaxAutoDispatchedCompleted {
    fmt.Fprintf(os.Stderr, taskClosureCapMessageFormat, ...)
    return ""          // ← 到此为止
}
```

被拒的 judged 行此刻**已经是终态**（不能因为排队失败就回滚关闭 —— 关闭是真事实），
于是"欠一次验证"这个事实只活在一行 **stderr** 里：不落库、不可查询、没有工作项。

## 两半修法

**① 拒绝写入账本。** 在 judged 行上写一条 `task_events`：

| 字段 | 值 |
|---|---|
| event | `verify-closure-deferred` |
| detail | 原因常量 `in-flight-cap` + 当时 in-flight 计数 + cap 值 + judged id |

**只写一次，在关闭那一刻。** 补发扫描在 cap 仍满时会重复复检同一行；若每个 tick 都追加，
审计轨迹会退化成计数器、工作项会退化成不断增长的扫描。`closureEnqueueOnce(..., recordDeferral bool)`
承担这个区别：close 路径 `true`，sweep 路径 `false`。回归测试按事件计数断言这一条。

**② 补发 —— 不新增任何常驻物。** `SweepDeferredVerifyClosures()`：

- **工作项的来源就是那条 deferral 事件**：`task_events` 有 `verify-closure-deferred`、
  该行**没有 closure 子行**、**没有 `verify-closure-requeued`** 的行，最早优先。因此
  **不存在第二份状态**，路径天然自愈（Rule 35：没有新表、没有新脚本、没有 cron）。
- 对每行重跑 gate **自己的**谓词 —— 新提取的 `closureRowEligible`，close 触发点与补发
  扫描共用同一个（Rule 14：不镜像）。然后走同一个 `closureEnqueueOnce`。
- **dedup 原样保留**，这才是"补发恰好一次"成立的原因而不是指望；成功后写
  `verify-closure-requeued`（带 closure id + 等待秒数）关掉工作项。
- 已在他处被 verified / 重打标 / cancel 的行 → 不再欠验证，仍写 requeue 事件
  （closure id 位填常量 `taskClosureRequeueNoClosure`）关掉工作项，**不**插验证行。

**挂载点**：`cmd/agent/task_lifecycle_tick.go` 的 `taskLifecyclePass` 新增第六条腿，紧挨已有的
`failed-needs-reissue` 腿。该 pass 由 worker 循环（每轮）与 merger tick 两个**已有**常驻载体驱动，
fail-soft（读账本失败只报错、不中断 pass）。没有新进程。

## `exit=0` 为什么不改

`gopi task update <id> --status completed` 是**主动请求的关闭**。让它因为"验证任务没能排队"而失败，
等于把**后台排队容量问题**变成**前台关闭动作的失败**：关闭本身是真事实，cap 满只是"暂时没有
验证员名额"。两者绑在一起会让 `task update` 的正确性依赖队列水位 —— 那正是同族上一个 bug 的镜像。

可追溯性不需要退出码承担：① 已把"欠一次验证"写进账本，② 已把"cap 清零就补发"挂进已有常驻 tick。
stderr 那行**仍然打印**，但消息现在指向一条真实存在的修复路径。

**唯一剩余缺口**：**没有任何常驻载体在跑**（既无 worker 也无 merger tick）。此时 deferral 事件仍在
账本上、仍是完整审计记录，补发会在下一次有载体启动时发生 —— 这正是 Rule 35 想要的方向。
手动驱动一次：`gopi worker --once`（空队列即跑一轮 lifecycle pass）。

## 实测证据（sandbox 账本，未碰 live）

pre-fix 与 fixed 两个二进制、同一形状的账本（`gopi tool sandbox create` 的私有 profile）：

| 步骤 | pre-fix | fixed |
|---|---|---|
| cap 满时关闭 bug 行 | `Task updated … completed`，stderr 一行警告 | 同左，但消息末尾多一句 "the requeue sweep re-queues this row then" |
| `task_events` | 只有 `completed` | `completed` + `verify-closure-deferred` |
| cap 清零 + 一轮 tick | closure 子行 **0**（永久失去 verdict） | closure 子行 **1**（`[verify-closure-requeue] deferred examined=1 requeued=1 still-deferred=0`） |
| 再扫一次 | — | `no deferred closures`（工作项已被 consume，不重复） |

time trail 落在账本上、且是**同一条**：

```
verify-closure-deferred  closure DEFERRED (in-flight-cap): 100 … cap 100) — judged row t-capbug …
verify-closure-queued    closure row <id> queued for independent verification (judged run: …)
verify-closure-requeued  closure row <id> … by the requeue sweep (deferred 13 s at the in-flight cap)
```

`deferred 13 s` 就是这条关闭等 verdict 的真实时长 —— 修复前它不可观测。

## 一处踩过的坑（日志前缀）

第一版里 sweep 返回值自带 `[verify-closure-requeue]` 前缀，而载体侧又打自己的腿标签，
于是日志出现 `[verify-closure-requeue] [verify-closure-requeue] deferred examined=…`。
修法：sweep 只返回**body**，前缀常量 `VerifyClosureRequeuePrefix` 导出、由载体引用
（Rule 13/14 不写第二份字面量）。**一个 token 每行只出现一次**，靠的是职责划分而不是去重。

## 未做

- **存量回填**：本修复前被静默拒绝的历史关闭行**无法重建** —— stderr 行没落库、没有别的痕迹
  能指出是哪些行。追不回的证据不能靠推测补一条 deferral（Rule 41 三要件）。
- **未加"查看/手动触发补发"的新 CLI 动词**：`gopi worker --once` 已能驱动一轮，加动词是 Rule 3/35
  方向的倒退。
- **未订阅 notify**：deferral 只进 `task_events` + 常驻 tick 的日志行。要主动叫醒人属于操作员侧
  `notify.events` 配置决策。
