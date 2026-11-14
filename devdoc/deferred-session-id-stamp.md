<!-- doc-class: normative -->
# 延迟 session_id 回填：一句注释许诺了一个不存在的兜底

任务 `01a16e1d-e3ef-7c5a-88aa-e68235be8078`，2026-10-25。

## 现象

`gopi task show <id>` 对大量已认领（`claimed_by` 非空）的行显示 `session_id:` 为空，
而 `~/.gopi/sessions/<task-id>/` 下明明躺着那次运行的会话记录。后果不是显示难看：

- `task show` 的 run 行不渲染；
- `taskHeartbeat` 的 session 腿拿不到会话（心跳只能退到 pid 腿）；
- 模型/日志路径解析退化成猜测。

只读打开 `~/.gopi/memory.db`（`mode=ro&_pragma=query_only(true)`）实测，
8 天窗口内 **39 条**已认领行的 `session_id` 为空、且其 run 目录持有会话记录，
其中两条是两天前就已 `completed` 的终态行。全表口径：`claimed_by` 非空 1204 行，
其中 `session_id` 空 111 行；`run_method='worker'` 906 行，其中 59 行为空。

## 根因

两处写点，一个早、一个「延迟」，后者不存在。

1. **早戳**（`cmd/agent/cli_tasks.go` 的 `runTaskRunOnce` → `tools.TaskBackfillSession`）：
   子进程起来后**轮询**子会话的 JSONL，`taskSessionPollMax`（5s）等不到就**静默放弃**。
   `TaskBackfillSession` 自己的注释把这写成契约：「一次 miss 绝不阻塞运行 ——
   写回的延迟回填仍是兜底」。
2. **延迟回填**（`internal/tools/tasks.go` `TaskUpdateWithResult`）：
   那段注释原文是
   「The deferred session_id stamp for human rows: runTaskRun's poll is best-effort,
   so a missed poll is repaired here.」
   —— 它**上面紧挨着的那几行代码**只调 `taskBackfillModel`，**只写 model 列**。
   `TaskBackfillSession` 的生产调用点全仓只有一处，就是第 1 条那个早戳：

   ```
   $ grep -rn "TaskBackfillSession" --include=*.go internal/ cmd/ | grep -v _test
   internal/tools/tasks.go:5487:func TaskBackfillSession(taskID, sessionID string) error {
   cmd/agent/cli_tasks.go:3241:            if err := tools.TaskBackfillSession(target, sid); err != nil {
   ```

   **兜底不存在。** 早戳一 miss，这个列就永远是空的。

   时间不易断言：这行注释在本仓可见历史的**第一天就已经在了**
   （`964b240` "initial: source snapshot at 0592c9ae4, no history"），
   所以只能说它至少和这份快照一样老 —— 具体是谁在哪次提交里写下这句
   已经不可考（`git log -S` 只能追到快照边界）。

这类缺陷的形状值得单独记住：**注释描述了一个不存在的机制**，于是下一个读代码的人
（包括写测试的人）会把它当成已实现的事实。它不会报错，不会卡住任何流程，
只会让所有按 session 关联的读取静默退化 —— 属于「静默失败」（Rule 15）最贵的一档。

## 修法，以及为什么不在上层修

第一版把补丁装在 `TaskUpdateWithResult`（写回 wrapper）里，那一版通过了正向测试，
但**漏掉了真正的入口**：`gopi task update <id> --status …` 走的是
`TaskUpdateReason → taskUpdate`，**不经过** `TaskUpdateWithResult`；
merger 的执行器（`task_merge_executor.go`）同理。装在 wrapper 上，操作员自己的
CLI 与合并器的写回依旧是空列。

所以补丁落在 **`taskUpdate` 的共享状态门** —— 全仓唯一被每个门共享的那次写
（run 写回、`task update --status`、auto-register 结束钩子、merger 执行器、同步层）。
`TestDeferredSessionStampReachesEveryDoor` 就是这条结论的钉子：三个门各跑一遍，
把补丁从共享门移回 wrapper 时它会红。

三个实现约束：

1. **id 只从行自己的 run 目录读**，不从环境变量读。
   写回跑在 **wrapper** 里，`GOPI_EXECUTOR_SESSION_ID` 叫的是 wrapper 自己的会话；
   盖上去就是把一个错的 id 写进列 —— 比空列更坏，因为空列至少一眼看得出缺失。
   行自己的产物是 `sessions/<task-id>/<session-id>.jsonl`，按行 id 寻址，
   归属由构造决定。选文件用 `internal/sessionlayout` 的共享词汇
   （`IsJSONL` 判是日志、`IsSideFile` 排除 `signal-provenance.jsonl` 这类 side file），
   resumed 行一个目录里有多条时取**最新**的那条（列描述最近一次执行）。
2. **与状态转换同一次 UPDATE**（`sets` 里加一个 CASE 子句），不另开句柄、
   不留锁窗口 —— 这个门里已经有一个打开的 `*sql.DB`，第二次打开是自找麻烦。
3. **CASE 守卫**：`session_id IS NULL OR session_id = ''` 才填。已有值的列永不被覆盖
   （`TestDeferredSessionStampNeverOverwritesAStampedRow` 钉住）；
   目录不存在或没有记录时**静默跳过**，绝不因此拒一次写回
   （`TestDeferredSessionStampIsSilentWithoutARunRecord` 钉住 ——
   把修复变成新的卡行会比原缺陷更贵）。

## 红证

把 `taskUpdate` 里的 CASE 子句用 `false &&` 停掉，重跑：

```
--- FAIL: TestDeferredSessionStampFillsTheRowFromItsOwnRunDirectory
    session_id after the writeback = "", want "0f1e2d3c-…"
--- FAIL: TestDeferredSessionStampReachesEveryDoor/task_update_--status_(TaskUpdateReason)
    session_id = "" after the task update --status (TaskUpdateReason) door
--- FAIL: TestDeferredSessionStampReachesEveryDoor/status-only_(TaskUpdate)
--- FAIL: TestDeferredSessionStampReachesEveryDoor/verify_writeback_(TaskUpdateWithResult)
```

恢复后同一条命令绿。恢复的判据不是「我搬回去了」，而是内容同一性与 revision：

```
$ test "$(sha256sum internal/tools/tasks.go | cut -d' ' -f1)" = "$(sha256sum /tmp/tasks.go.fixed2 | cut -d' ' -f1)"
$ go test ./internal/tools/ -run TestDeferredSessionStamp   → ok
```

**CLI 层的红证**（单测之外，用真实二进制）：同一个 sandbox profile 里，
**修复前**的 `/usr/local/bin/gopi`（revision `bb77c62`）把行写成 `failed` 后列仍为空；
再用**修复后**的 `./bin/gopi` 对同一条行做一次写回，列立刻变成
`11112222-3333-4444-8555-666677778888`。同一行、同一份 run 记录，两个二进制，
两种结果 —— 这就是这条修复的可观测差异。

## 与 01a16f7c 的边界（不要把两件事混起来）

本任务提交时的 briefing 把根因写成「closeout 用 `claimed_by` 的 pid 存活判该谁写回，
而那个 pid 是长驻队列进程永远活着」。**那个判据已经在 2026-10-25 04:37 被净删除了**
（`f8e31a2`，HEAD `bb77c62` 的祖先；`devdoc/run-closeout-boundary.md` 记录边界）：
`internal/agent.closeOutRunRow`、`tools.TaskRunCloseoutStaleRun`、
`RunCloseoutOwnedBy`、`runCloseoutClaimEvidenceHolds` 全部不存在，
`internal/tools/tasks.go:8924` 现在是「不得重新引入」的墓碑注释。
现在的写回者是 wrapper 自己在 `cmd.Wait()` 之后的那次写回 —— 它**不问 pid**，
它就是子进程的父进程。也就是说，briefing 建议的目标形态（「以 Wait 已返回为判据」）
**已经就是现状**。

交付顺序那个独立缺陷（交付腿排在 `completed` 写回之后，而门量的正是交付腿指向的 ref）
也已由 `0754f0d` / `e2a8988` 修掉，实测三条 clone ground 的提交已落 master
且 `git rev-list --count origin/master..HEAD == 0`。

**所以本任务真正还活着的缺陷只有一个：这个 session_id 空列。** 判据是实测，
不是 briefing 的措辞 —— 提交时把已修的缺陷当成待修的记下来，代价是下一次有人
按同样的描述再修一遍不存在的东西。

## 没做的（诚实）

- **历史 111 条空列不回填。** 回填是一次批量写，`memory.db` 是本机唯一事实来源，
  而它们绝大多数是终态行：补 session_id 不改变任何已发生的事实，只改善可追溯性。
  该由操作员决定要不要跑一次（一条 `UPDATE … WHERE session_id IS NULL`-形状的
  迁移，需要单独 review），不是本任务的顺手副作用。
- **ground 生命周期是另一个缺陷。** 实测 `/home/user/workspace` 下 157 个任务
  ground 共约 28.9 GB 无人回收（`failed` 112 个 21.0 GB、`completed` 34 个 6.2 GB、
  `cancelled` 5 个、行已不存在的 5 个），`/` 分区 96% 已用。与本任务无关，已单独提行。
