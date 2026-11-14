<!-- doc-class: historical -->
# Task Report: task-end memory_store bypass on terse completed answers

> 分支：feat/taskend-store-bypass · 日期：2026-08-26 · 根因定位 + 修复 + 复现测试 + 活体验证

## 背景（Epic）

用户观察到 `gopi log --last 3 --memory` 只显示任务开始的 `memory_search`，
任务结束的 `memory_store` 不出现。怀疑是 memory_feedback 与 task-start
memory_search 的新组合改动（feat/search-remove, 82cb805）绕过了 task-end
强制 store。

## 根因（不是 memory_feedback 直接绕过的，而是它把缺口暴露出来）

**结构缺陷：`maybeEnforceCompletionStore()` 只在 R3 完成分支里可达，而该分支
要求「任务列表全 completed **且** 答案 ≥ 40 字符（minFinalAnswerChars）」。**

loop.go 的完成出口（Run 循环内）有两条正常退出路径：

1. **R3 信任出口**（`AllCompleted(tasks) && contentWithoutTaskList >= 40`）：
   调 `maybeEnforceCompletionStore()` —— 唯一会强制 task-end store 的路径。
2. **bug022 确认出口**（`consecutiveEndTurn >= maxConsecutiveEndTurnRounds`）：
   **从不检查 `memoryStored`，直接返回。**

当模型完成任务但最终答案很简短（冒烟任务的 "SMOKE-REMOVE-OK" 14 字符、
"3" 字节数、一行确认）时：
- 第 1 轮 end_turn 带 completed task list，但 `contentWithoutTaskList < 40`
  → R3 分支不成立 → 落入 bug022 的确认等待；
- 第 2 轮 end_turn（确认轮）→ `consecutiveEndTurn >= 2` → **走路径 2 退出，
  从不注入 completion checkpoint，从不强制 store。**

这正是用户看到的现象：`gopi log --last 3 --memory` 只有开始的 memory_search。
memory_feedback/search-remove 改动（82cb805）把模型引导成
「search → feedback(useless) → 简短确认」的工作流，让这个**既有**结构性缺口
（13e6019 起就存在）高频暴露。

### 证据（历史会话扫描 ~/.gopi/sessions）

- `f15da5e3 / 2c8e30cd / a67d4e1d`（search-remove 冒烟）：
  memory_search + memory_feedback + 简短 "SMOKE-REMOVE-OK" 完成 → **无 store、
  无 checkpoint**。其中 f15da5e3 的 task list 明确 all-completed。
- `522c0fb2`（clock-tick 冒烟，Aug 25 19:25）：
  memory_search + bash + 答案 "6 [TASK_LIST completed]"（剥离后 1 字符）→
  无 store、raw log 里 0 个 "COMPLETION CHECKPOINT"。
- 对照 `5109f034`（正常长任务）：中途自发 store 过 → memoryStored latch 置位
  → 完成出口直接放行，**正确**（设计如此）。

## 修复（internal/agent/loop.go Run 完成出口）

把 task-end store 强制从「R3 子分支」提升到「**任何 all-completed 声明**」：

```go
if tasks, _ := ParseTaskList(content); tasks != nil && AllCompleted(tasks) {
    if len(contentWithoutTaskList(content)) >= minFinalAnswerChars {
        // R3 信任出口：substantive 答案 → 强制 store（若有需要）→ 直接返回
        if a.maybeEnforceCompletionStore() { continue }
        ...return pendingFinalContent/content
    }
    // 简短完成答案：仍保留 bug022 确认轮（不信任第一轮 end_turn），
    // 但 task-end store 不再被绕过 —— 现在就注入 completion checkpoint
    //（它替代 end-turn nudge：任务已声明完成），强制模型先 memory_store。
    if a.maybeEnforceCompletionStore() { continue }
}
```

语义变化：
- R3 信任出口行为不变（store → 直接返回）。
- 简短完成答案：checkpoint 注入后 `continue` → 模型多一轮，被要求先
  memory_store 再给最终答案；下一轮 checkpoint 已闩锁
  （completionStorePrompted）→ 落入 bug022 确认出口正常退出。
- 中途已自发 store（memoryStored=true）→ 两种路径都直接放行，不重复注入。
- 未完成/无 task list 的会话（如 "hello" 测试）不触发 —— 保持原语义。

## 测试

新增 `internal/agent/taskendstore_bypass_test.go`：
- `TestAgent_Run_TerseCompletedAnswer_StillEnforcesStore`：mock 恒定返回
  简短 completed 答案 → 断言 checkpoint 注入且只注入一次（修复前 FAIL）。
- `TestAgent_Run_TerseCompletedAnswer_ToolRoundThenExit`：完整复刻 f15da5e3
  会话模式（search + feedback 轮 → 简短 completed 轮 → 确认轮）→ 断言
  checkpoint 注入（修复前 FAIL）。

增强既有 `TestAgent_Run_KeepsConfirmationForTerseCompletedList`：
- 追加断言：简短 completed 答案必须注入 completion checkpoint（bug022
  确认轮保留 与 task-end store 强制 不再互斥）。

## Verification

- Build: `go build ./...` OK（worktree，feat/taskend-store-bypass）。
- Tests: `go test ./...` 全绿（cmd/agent, internal/agent, config, deepseek,
  gopilog, tools）。
- Live E2E（修复后二进制 /tmp/gopi-taskend-fix）：
  `echo hi > file && wc -c file` 任务，答案只回 "3" 并标记 completed：
  ```
  gopi log --last 1 --memory →
  Session: 205caa4a...
  ── TOOL 1/2: memory_search @ 08:40:27.023 ──
  ── TOOL 2/2: memory_store  @ 08:40:31.740 ──
  ```
  raw log 确认 "COMPLETION CHECKPOINT" 用户消息被注入（3 处），session JSONL
  确认 memory_store 落在最终回答之前（iter 11 store → iter 14 final）。
  修复前同场景（522c0fb2）0 个 checkpoint、无 store。

## Not Done

- max-iterations / timeout / content-filter 等**异常退出**路径未加 store 强制
  （非正常完成，设计上不强制；且 timeout 路径拿不到完整上下文）。
- resume 路径（Agent.Resume）不强制 task-end store（resume 会话的语义是
  「续跑」，跳过 task-start search 对称地也跳过端侧强制——保持现状）。
- memory_feedback 的上下文瘦身（RemoveToolPair）本身无 bug：只动消息数组，
  不触碰 memorySearched/memoryStored 闩锁，与本次绕过无因果关系。
