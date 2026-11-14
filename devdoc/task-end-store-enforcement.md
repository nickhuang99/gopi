<!-- doc-class: historical -->
# devdoc: task-end memory_store 对称强制

## Why
REPEAT-TEST 发现 task-end `memory_store` 只靠 soft prompt 提示，Run 1 首次构造空 input 失败
靠 LLM 自发重试。本 Epic 把它加固为强制——与 task-start `memory_search` 对称。

## What changed (loop.go)
1. **prompt**: `toolRoutingRules` 记忆行 + `taskManagementRules` 规则 6 现强制 store-at-completion。
2. **harness**: `Agent.memoryStored` / `completionStorePrompted` + `maybeEnforceCompletionStore()`。
   在 `Run` 的 R3 完成出口（all-completed + substantive answer、即将 return）调用：若本 run
   从未真实落库，注入一次性 `completionCheckpointMessage` 用户消息并 `continue`，强制模型
   先 store 再最终回复。只在该点、只一次——绝不每轮泛滥。

## Design decisions (未来 agent 必读)
- **完成出口在返回前注入**（而非用独立工具强制）：保持 ReAct 循环单线程、不动模型自主判断，
  只是保证「完成即存」。存储失败/校验拒绝不置 `memoryStored`，checkpoint 仍可兜底；
  真实成功落库后 latch 置位，永不再注入（不会双存）。
- **不 per-round**：只在 all-completed 完成检测点触发一次。这正是「任务完成时」，满足
  "不要过度强制/避免 store 泛滥"。
- **密度**：system prompt 从 ~4457 起，加强制后须仍 <4500。用减法压缩现有 wording（如
  efficiency rule 4 去掉示例括号）腾出空间，最终 4477。
- **R3 完成出口测试被改动**：原 `TestAgent_Run_ExitsOnFirstCompletedEndTurn` 断言 1 API call；
  现在 completed+never-stored 会多 1 个强制 store 轮（不是 bug022 确认轮）。测试已更新说明。
- **工具描述 vs prompt 双写**：5W1H 类别清单（knowledge/skill/facts/lessons/pitfalls/success/
  setback/summary）放在 `completionCheckpointMessage`（运行期 user 消息，不进 system prompt 密度）
  和 memory_store 工具描述里；system prompt 只留简短对称指针，保住 4500 预算。

## Verify
- `go test ./internal/agent/` — checkpoint one-shot / skip-when-stored / 真实 store latch /
  error-not-latch / 消息类别全覆盖。
- `TestAgent_Run_ExitsOnFirstCompletedEndTurn` 现断言 completed 未存 → 强制 1 store 轮。
- 真机重跑 REPEAT-TEST 任务 2 次（隔离 GOPI_MEMORY_DB）：均任务完成自动 memory_store。
