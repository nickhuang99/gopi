<!-- doc-class: normative -->
# GoPi Todo List — Feature Design & Implementation Task

> 来源: ~/work/claude/the operator/to_go_list_design_talk.txt
> 类型: GoPi 内置工具扩展（第 5 个工具，本地操作）
> 优先级: P0（解决 LLM "只分析不执行"的核心卡壳问题）

## 问题

当前 GoPi 的 ReAct 循环有一个致命缺陷：LLM 可能只输出文字方案而不调用工具，框架只能被动等待，流程永久卡死。上下文窗口即使有 1M token，也无法解决"LLM 主动停滞"的问题。

## 方案：无状态任务列表 + 心跳提醒

完全遵循 GoPi 极简哲学——**调度器不维护任务状态，LLM 拥有全部决策权**。

### 五条核心约束

1. **完全无状态**：Scheduler/Timer 不持久存储任务列表。LLM 每轮响应必须携带完整任务清单
2. **首轮特殊**：框架只在第一轮传入 Epic 总目标；LLM 基于此生成首版任务列表
3. **禁止删除**：所有历史 task_id 必须保留在列表内，只能通过修改状态体现进展
4. **两状态**：`progressing` / `completed`
5. **自主排序**：LLM 可自由选择执行顺序，调度器不干预

### 任务格式

LLM 每轮响应末尾必须包裹：

```
[TASK_LIST_BEGIN]
[
  {"task_id":"t001","summary":"读取配置文件","create_at":"1780000000","status":"completed"},
  {"task_id":"t002","summary":"修改数据库schema","create_at":"1780000000","status":"progressing"}
]
[TASK_LIST_END]
```

### 心跳机制

固定周期（30s 默认，可配）检查：
1. 当前是否有工具在执行？→ 跳过
2. 最后 N 轮任务列表无变更且无工具调用？→ 推送提醒

提醒文案：
```
HEARTBEAT REMIND：当前仍存在 progressing 未完成任务，请更新任务状态或调用工具继续执行。
当前任务列表：【复述上一轮完整 task_list】
```

### 父任务拆解规则

拆分子任务 → 父任务标记 `completed`。

```json
// 拆解前
[{"task_id":"t001","summary":"实现认证模块","status":"progressing"}]

// 拆解后
[{"task_id":"t001","summary":"实现认证模块","status":"completed"},
 {"task_id":"t002","summary":"Auth: 数据库表设计","status":"progressing"},
 {"task_id":"t003","summary":"Auth: JWT中间件","status":"progressing"}]
```

规则优势：
- 队列始终扁平一维结构，无任务树
- BFS 天然行为（子任务追加到队尾）
- 可扩展 DFS：子任务 prepend 到队首

### 全局终止条件

所有任务 `status == "completed"` → 工作结束。

## 实现任务

### 0. 核心逻辑变更：loop.go 终止条件重写（最重要）

**当前终止条件**（loop.go）：
```go
// 旧: LLM 停止输出 tool_call → 判定任务完成
if len(toolCalls) == 0 {
    return content, nil  // 任务结束
}
```

**新终止条件**：
```go
// 新: 解析 task_list → 全部 completed 才退出
taskList := parseTaskList(content)     // 提取 [TASK_LIST_BEGIN]...[TASK_LIST_END]
progressing := countProgressing(taskList)

if progressing == 0 {
    return content, nil                // 全部完成 → 退出
}

// 有 progressing 任务 + 本轮无工具调用 → 卡壳
if len(toolCalls) == 0 {
    staleCount++
    if staleCount >= staleThreshold {  // 连续N轮卡壳
        content = injectHeartbeatReminder(content, taskList)  // 注入提醒
        staleCount = 0
        continue                       // 重新推给 LLM
    }
    continue                           // 给 LLM 更多机会
}

staleCount = 0  // 有工具调用 → 重置计数器
```

**完整循环流程**（Run() 主循环）：
```
for each iteration:
    1. LLM 回复 → 解析 content
    2. 提取 tool_calls + task_list
    3. 执行工具调用（如有）
    4. 检查 task_list 中 progressing 数量:
       - 0 → return（全部完成）
       - >0 + 有工具调用 → 继续下一轮
       - >0 + 无工具调用 → 累加 staleCount
         - staleCount < 阈值 → 继续
         - staleCount >= 阈值 → 注入心跳提醒，重置计数器
    5. 心跳 goroutine 独立运行（不阻塞主循环，仅监视）
```

### 1. 新增 `Todo` 系统提示词

在 GoPi 的 kernel prompt 中追加任务列表规则（system prompt 不可变部分+规则）：

```
1. 首轮基于传入的 Epic 目标生成首版 task_list；Epic 本身不属于正式任务。
2. 每轮回复末尾必须附带完整 task_list；不可删除任何已有 task_id，仅可新增/修改。
3. 任务状态仅支持 progressing、completed；所有任务均为 completed 时工作结束。
4. 复杂任务可拆分子任务：父任务设为 completed，子任务新增并设为 progressing。
5. 存在 progressing 任务时必须持续推进；长时间无进度会收到心跳提醒。
```

### 2. 心跳 Timer（框架侧）

**（2026-09-29 状态：此设计的心跳计时器从未接线，已删除。）** 实际在跑的节律是轮级判定的 `maybeNudgeStaleTaskList`
（`internal/agent/loop.go`，阈值 `maxRoundsWithoutTaskList`，带反骚扰上限 + prompt 契约兜底）——见 `devdoc/heartbeat-knobs-removed.md`。
原设计如下，保留为记录：
- 固定周期协程（默认 30s，`GOPI_HEARTBEAT_SEC` 可配）——**该旋钮已随死代码移除**
- 仅在无工具执行时触发提醒
- 提醒内容：复述上一轮完整任务列表
- 连续多轮无变更时才触发（避免频繁打扰）

### 3. 任务列表解析器

`internal/agent/tasklist.go`:
- 从 LLM 响应中提取 `[TASK_LIST_BEGIN]...[TASK_LIST_END]`
- 校验：所有旧 task_id 保留、状态合法性、无删除
- 统计 progressing 数量 → 判断是否终止

### 4. 集成到 Agent 循环

`internal/agent/loop.go`:
- 首轮：传入 Epic 作为第一条用户消息
- 每轮：解析 task_list，校验，统计状态
- 心跳：goroutine 检查是否卡壳
- 终止：全部 `completed` → 退出循环

## 非目标

- 不做抢占式中断
- 不做优先级调度（LLM 自主选择）
- 不做外部任务持久化（无状态设计）
- 不做任务依赖树

## 测试

```go
// heartbeat_test.go
TestHeartbeat_TriggersWhenStuck — 连续 3 轮无变更 → 提醒触发
TestHeartbeat_SkipsWhenBusy — 工具执行中 → 不触发
TestHeartbeat_StopsWhenAllCompleted — 全部完成 → 停止检查

// tasklist_test.go
TestTaskList_Parse — 解析 TASK_LIST_BEGIN/END
TestTaskList_RejectDeletion — 删除旧 task_id → 报错
TestTaskList_AllCompleted — 全部 completed → 返回 true
TestTaskList_ParentDecomposed — 父 completed + 新增子 progressing → 合法
```

## System Prompt 注入

追加到 GoPi 硬编码 kernel prompt 之后（不可变部分）：

```
[TASK MANAGEMENT RULES]
1. First turn: generate initial task_list from the Epic goal. Epic is NOT a task.
2. Every response MUST end with [TASK_LIST_BEGIN]...[TASK_LIST_END].
3. Only 2 task statuses: progressing, completed. Never delete old task_ids.
4. Complex task → decompose: set parent to completed, add children as progressing.
5. While any task is progressing, you MUST keep working. 
6. All tasks completed = work DONE. Exit.
```
