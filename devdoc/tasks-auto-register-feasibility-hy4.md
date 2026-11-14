<!-- doc-class: normative -->
# tasks 台账自动登记机制 — 可行性研究（设计报告）

日期: 2026-08-28
分支: feat/auto-register-feasibility-hy4（worktree: ~/workspace/gopi-auto-register-hy4）
上游: master @ dbba5b1（feat/tasks-ledger 已 merge 进 master）
性质: **可行性研究，只出设计报告，不写实现**。本文不包含任何 loop.go / tasks.go 代码改动。
结论部分为中文；代码引用行号基于 dbba5b1。

---

## 0. 结论先行

**条件可行（推荐方案：纯 harness 侧单向录音机 + session 命名空间 + session-end 终态清扫）。**

tasks 台账**应当**自动登记运行时任务，但必须满足四个设计约束才不越界：

1. **单向流**：runtime → ledger 只写不读。loop.go 任何路径都不得读 tasks 表来影响行为（Stateless 哲学不动摇——LLM 仍拥有任务状态，台账只是转录它的持久副本）。
2. **转录不虚构**：harness 只转录 LLM 已声明的 task_id/summary/status，绝不代编摘要或状态；唯一例外是 session-end 清扫（把该 session 残留的 in_progress 行按客观退出码盖终态章）——这是"兜底"角色的记录动作，不是对 LLM 状态的覆盖。
3. **append-only 不动摇**：无 delete（the operator 铁则），改名遗留的孤儿行是审计真相，永远不删不重写。
4. **可关停**：GOPI_NO_TASK_REGISTER 类开关（镜像 GOPI_NO_ENFORCE_MEMORY 先例），A/B 隔离与"框架默认零副作用"可证伪。

**否掉的方案**：(a) 依赖 LLM 显式 tool_use 登记——顶撞 Rule 3（18 工具封顶）且被经验证伪（todo 19 次调用零使用、memory_update 1/21435、feedback 软提示采纳率 9-15%：LLM 不会自觉做记账，自愿登记 = 台账永远是空的 = memory-lowquality 死列教训）；(b) 仅 task-end 完成时登记——max-iterations/timeout/abort 的会话（恰恰是审计最该记录的失败）永远进不了台账，与"记录永久留存用于追溯"目标直接相悖。

核心事实支撑：**harness 每轮已经在 loop.go:1700 解析 [TASK_LIST]（balanced-prefix 恢复 + 显式丢弃告警），数据零成本可得**；tasks.go 的 session_id 列和 TaskNew 的 sessionID 参数就是为 harness 预留的（impl report 决策 5："harness 未来自动登记可用"）。自动登记 ≈ 在现有解析点后加一个转录函数 + session-end 清扫，无新工具、无 schema 变更、无 prompt 密度成本。

---

## 1. 时机（Q1）：每轮 diff 为主，task-start/task-end 是它的特例

| 方案 | 优 | 劣 |
|---|---|---|
| (a) task-start 登记 | 记录"尝试过什么"，一次一写 | 单独用 = 永远 in_progress，无结果面，半死台账 |
| (b) task-end 完成时 | 记录结果 | 失败/中断会话（max-iterations 出口 loop.go:1932、timeout、error）永不触达——审计最需要的那一半丢了 |
| (c) 每轮 tasklist diff | 完整、忠实、自愈（漏一轮下轮补）；解析点已存在 | 需变更检测避免每轮落库 |
| (d) 仅完成时 | = (b) | 同 (b) |

**推荐 (c)**：在 loop.go:1700 `tasks, warn := a.parseTaskList(content)` 之后（TaskState.update 之前，1700-1724 区间）挂转录钩子——每轮把解析结果与 session 本地 seen-set 对比，仅在新 task_id 出现（=task-start 语义）或状态变化（=task-end/中途完成语义）时写库。两个既有强制点自然复用：
- **task-start 语义** = task_id 首次出现在该 session 的列表；
- **task-end 语义** = 每轮 diff 看到 progressing→completed（比只盯 all-completed 更细：中途完成的任务也能落账）；
- **异常出口兜底** = Run defer（loop.go:1516 写 session_end 的同一块）做 session-end 清扫：该 session 残留 in_progress 行按退出码盖终态（exit_code≠0 → failed；exit_code=0 但有未完成行 → cancelled）。

心跳/nudge 机制（**2026-09-29 更新：`GOPI_HEARTBEAT_SEC` / `GOPI_HEARTBEAT_STALE` 两个旋钮及其死状态机已删除** —— `devdoc/heartbeat-knobs-removed.md`；现行机制是轮级判定的 `maybeNudgeStaleTaskList`）与转录正交，**不碰**——转录吃的是每轮解析结果，不依赖心跳触发。

## 2. 数据来源（Q2）：吃解析结果，不重解析、不读 todoState

| 来源 | 判定 |
|---|---|
| (a) 解析 LLM [TASK_LIST] JSON | ✅ **采用（复用现成结果）**。裸解析确实脆弱（LLM 忘 END 需 balanced-prefix 恢复），但脆弱性**已经被现有代码吸收**：loop.go:1700 每轮解析，ErrTaskListRecovered → 任务照常流转（非二等公民），ErrTaskListUnparseable → nil + 显式告警（绝不产出垃圾列表）。自动登记必须钩**解析结果 []TaskItem**，绝不二次解析。可用字段：TaskID / Summary / Status（progressing/completed 两态）/ CreateAt（LLM 自报 unix 时间戳，**不可靠，弃用**，用 harness 墙钟） |
| (b) todoState（TaskState） | ❌ 不用作写源。它是 todo 工具的只读镜像（loop.go:333-367，update 只存最新列表），与写源混用会模糊"框架只读"契约；且只留最新态、丢失每轮 diff 上下文 |
| (c) CLI 手动 | 保留为策展通道（改摘要/改状态），不可能撑起规模登记（见 Q7） |

**状态映射**：tasklist 是 2 态（progressing/completed），tasks 表是 4 态（in_progress/completed/failed/cancelled）。映射：progressing→in_progress、completed→completed。**failed/cancelled 永远不可能来自 tasklist**——它们只来自 session-end 清扫（harness 兜底）或人工 CLI。这是台账 4 态设计的正确用法，不是缺陷。

## 3. Stateless 哲学张力（Q3）：不推翻，靠四条护栏

张力是真实的："自动写入 = 框架侧获得任务状态写入权"。但：

- 台账的定位已是**审计/追溯层**（tasks.go:8-14："the ledger is an AUDIT/TRAIL layer, not runtime control. It does not touch loop.go's tasklist/todoState/heartbeat/nudge machinery"）。自动登记只是让这个审计层从"人工喂"变成"自动喂"，定位未变。
- the operator 铁则原文（memory-catchup-brief F7）："LLM 仅能新增、修改摘要、修改状态；任何场景不能删除；**调度器只读过滤，记录永久留存用于追溯**"——台账是"记录永久留存"那一半的落地；自动登记让"永久留存"从口号变成事实。Stateless 保护的是**运行时**：LLM 每轮带全量表、scheduler 纯兜底——**只要 loop.go 不读 tasks 表，运行时语义零变化**。
- "只登记不修改"的方案太弱：状态必须能推进到 completed，否则台账烂在 in_progress（死列教训）。正确护栏是**转录 + 兜底**：harness 转述 LLM 声明的状态变迁（LLM 是状态的唯一宣言者），唯一例外是 session-end 清扫盖终态（客观事件：run 确实异常结束了）。
- 硬约束写进代码注释与本文档：**loop.go 禁止任何读 tasks 表路径**；转录钩子只写不读（除 openTasksDB 自身校验）。

## 4. Rule 3 工具最小化（Q4）：不新增 LLM 工具，纯 harness 侧行为

- 自动登记 = loop.go 解析点后的内部调用 + 一个新引擎函数（tools 包内），**零 tool_use 事件、零系统提示改动、零密度预算消耗**。不算新工具。
- 先例：taskStartSearchMessage / completionCheckpointMessage 就是 harness 侧强制行为（loop.go:132-150），没有为此加过任何工具；`gopi task` CLI 子命令已有 log/checkpoint 先例（CLI 子命令 ≠ LLM 工具）。
- Rule 11（新工具必须 LLM 真实调用证明）**不适用**——没有新工具。但价值仍需实证：E2E 验证（§10）证明台账真的被填满且状态正确。

## 5. 幂等/去重（Q5）：命名空间 + upsert + seen-set

- **DB 层**：task_id UNIQUE。自动登记需要 upsert 语义（存在则更新、缺则插入），**不能复用 TaskNew 的重复即报错契约**（tasks.go:235-243）——需要新的 harness 专用引擎函数（§9）。
- **task_id 稳定性（关键风险，实测确认）**：LLM 的 task_id 是自定义字符串，且**当前运行时没有任何强制手段保证它稳定**——bug010 纠错常量和 ValidateTaskList 定义了但**全仓库零调用点**（grep 验证：maxTaskListViolations/deletedTaskCorrection/malformedTaskListCorrection 仅 const 定义，ValidateTaskList 仅测试引用）。session 内稳定性目前**纯靠 prompt 纪律**（deletedTaskCorrection 文本说"never delete or rename them"但从未被注入）。跨 session（resume）稳定性更无从谈起。
- **对策：session 命名空间**。落库 task_id = `<session_id>:<sanitized llm_id>`（如 `a1b2c3:step-1`）。确定性、全局唯一（"1" 在不同 session 不再撞 UNIQUE）、跨 session 可追溯（CLI 能看到哪条属于哪个 session）。**绝不裸用 LLM id**。
- **改名孤儿**：LLM 若违反纪律改名，旧行永远 in_progress（append-only 不可删）→ session-end 清扫盖 cancelled（"不再被追踪"是客观事实，不是虚构）。孤儿行=审计真相，保留。
- **同 session 去重**：session 本地 seen-set（task_id → 上次落库状态），仅状态变化才写；无变化零 DB 命中（EqualTaskLists 已有同款比对逻辑可借鉴）。

## 6. 失败模式（Q6）：逐项对策

| 失败 | 对策 |
|---|---|
| 解析失败（BEGIN 无 END 且无 JSON 前缀） | 现有 ErrTaskListUnparseable → nil → 本轮跳过不写；每轮 diff 的自愈性保证下轮完整列表补账。已恢复列表（ErrTaskListRecovered）正常落账，零丢失 |
| max-iterations（loop.go:1932）/ timeout / 错误退出 | Run defer 的 session-end 清扫盖 failed（exit_code≠0）；不触达 R3 完成出口也无妨 |
| 用户中断（exit_code=0 但任务未完成） | 清扫盖 cancelled |
| resume | resume 刻意跳过两端 checkpoint（A3 设计决策）；新 session_id → 新命名空间 → 新行。每 session 一行审计 = 正确语义，不值得为复用原 session_id 加复杂度 |
| DB 不可用 / 外库 | openTasksDB 外库保护不变（tasks.go:153-155，拒绝 foreign DB）；harness 调用**必须 best-effort**：失败只打日志继续跑——台账永不能拖垮运行时（镜像 MemorySearch bump 的"失败不降级主路径"哲学的反面：这里主路径是任务执行，台账是旁路） |
| 进程被杀（无 defer） | 已提交的行持久（每轮 diff 写是独立事务）；未清扫的 in_progress 行留在台账里呈现为"aborted"，肉眼可辨。后续可加 `gopi task --reconcile-stale` 离线清扫（本期不做，诚实声明） |
| 输出截断（finish_reason=length） | 解析取 LAST 块（tasklist.go:121 LastIndex）；截断可能丢尾块 → 本轮 diff 不全 → 下轮自愈；已登记行不受影响 |
| summary 噪音/截断 | LLM 原始摘要质量低于人工——审计优先完整性，策展由 CLI 补（Q7） |

## 7. 收益 vs 成本（Q7）：自动登记更匹配 the operator 的目标

**收益**：
- 台账完备性 = "记录永久留存用于追溯"（the operator 铁则）的唯一可靠实现。经验数据：自愿记账行为采纳率 9-15%（feedback）/近乎为零（todo 19 调零用、memory_update 1/21435）——**CLI 手动 = 台账实际上永远填不满**。自动登记让每个声明过 tasklist 的 session 都留下持久记录（task_id/摘要/状态/时间戳/session_id），`gopi task list --status` 审计查询第一天就有意义。
- value-signal 教训直接兑现：status 索引、created_at/updated_at/ended_at 生命周期从第一天就是活数据，不做死列。
- 零边际成本：数据已解析、session_id 列已预留、schema 不动、无新工具、无 prompt 密度、无每轮 token 成本。

**成本**：约 1 个引擎函数 + 1 处 loop 钩子 + session-end 清扫 + 测试（估计 <150 行，本期不写）。唯一实质代价 = LLM 原始摘要噪音 vs 人工策展质量——但**审计目标优先完整性**，且 CLI 与自动登记共存（自动填、CLI 策展），冲突面定义清晰：CLI 显式 --task-id 撞自动登记行 → 报错（现有正确行为，tasks.go:235-243）。

**结论**：自动登记压倒性优于 CLI 手动，两者是互补关系（完整度 vs 策展），不是替代关系。

## 8. 推荐设计概览（若实现，接口 + 触发点 + 数据流）

```
LLM 每轮响应
   │
   ▼
parseTaskList (loop.go:1700, 现有, balanced-prefix 恢复)
   │ []TaskItem (TaskID/Summary/Status)
   ▼
【新增】转录钩子: a.registerTasks(sessionID, tasks)   ← 唯一新调用点
   │  per-task diff vs seen-set: 新 id → TaskRegister(create)
   │                            状态变化 → TaskRegister(update)
   ▼
tools.TaskRegister(sessionID, taskID, summary, status) (string, error)
   │  upsert by task_id；task_id = sessionID + ":" + llmID(sanitized)
   │  复用 openTasksDB（外库保护不动）；best-effort：错误仅日志
   ▼
tasks 表（schema 零改动）
   ▲
Run defer (loop.go:1516 现有块内) 【新增】tools.TaskSweepSession(sessionID, finalStatus)
   该 session 残留 in_progress 行 → failed/cancelled（按退出码）
```

- **接口**（引擎函数，tools 包，CLI 与 harness 共用零分叉——tasks.go 惯例）：
  - `TaskRegister(sessionID, taskID, summary, status string) (string, error)` —— upsert：task_id 含 session 命名空间；缺则 INSERT（status 默认 in_progress，创建即成功），有则 UPDATE（status/summary 变更 + updated_at；terminal 盖章 ended_at——复用 TaskUpdate 生命周期语义）。重复 upsert 幂等，不报错。
  - `TaskSweepSession(sessionID, finalStatus string) (int, error)` —— UPDATE 该 session 全部 in_progress → finalStatus（terminal，盖章 ended_at），返回受影响行数。仅用于 session-end 兜底。
- **触发点**：仅两处——(1) loop.go:1700 后转录钩子（每轮）；(2) Run defer（session_end 同一块）清扫。无第三处。
- **开关**：`GOPI_NO_TASK_REGISTER`（Rule 2：NewAgent 单点读 env，默认开）。与 NoEnforceMemory 平行。
- **与 tasks.go 衔接**：复用 openTasksDB/ensureTasksSchema/validTaskStatus/terminalTaskStatus/formatTaskTime；schema 常量、CHECK 约束、外库保护、append-only **全部不动**。状态映射只产出 4 合法值。
- **依赖方向注意**：internal/agent 已 import internal/tools（loop.go 调 tools.MemoryFeedback 等），tools 不能 import agent → TaskRegister 的参数用纯字符串/自结构体，agent 侧把 TaskItem 映射为裸字段传入，**不引 agent 类型进 tools**。

## 9. 风险与边界（汇总）

1. **Stateless 张力**：靠单向流护栏。文档 + 代码注释双重声明"loop.go 禁止读 tasks 表"；转录钩子只写不读。若未来有人试图用台账做运行时控制（如读表决定 exit），直接违反本报告结论，应被 code review 拦下。
2. **Rule 3**：无新 LLM 工具、无 prompt 改动——明确不触发。
3. **幂等**：命名空间 + upsert + seen-set 三层；孤儿行保留为审计真相。
4. **2 态 vs 4 态**：failed/cancelled 仅来自清扫/CLI，LLM 永不产生——这是台账 4 态设计的正确用法，文档化防误解。
5. **task_id 稳定性无运行时强制**（bug010 常量为死代码——已实测）：设计**必须**假设漂移存在，不能把稳定性赌在 prompt 纪律上。
6. **外库保护**：清扫与转录同样走 openTasksDB，foreign DB 场景下 harness 应静默跳过（日志）而非报错中断。
7. **不清扫 resume 原 session**：原 session 行保留 in_progress（或由原 session 自己的 defer 清扫——resume 前的进程若正常退出 defer 已跑；若被杀则残留，见 Q6 进程被杀行）。

## 10. 验证建议（实现阶段执行，本期不做）

1. **E2E 真实会话**：临时 GOPI_MEMORY_DB 跑一个真实 gopi 任务（3-5 轮，含中途完成任务），断言：任务首现即落账（in_progress）、完成轮状态转 completed + ended_at 盖章、无变化轮零 DB 写。
2. **异常出口**：max-iterations 强制触发（小 MaxIterations），断言清扫盖 failed。
3. **恢复路径**：构造 BEGIN-无-END 会话，断言 ErrTaskListRecovered 列表正常落账、Unparseable 轮无写。
4. **幂等**：同 task_id 多轮 diff，断言单行、updated_at 前进。
5. **外库**：GOPI_MEMORY_DB 指向 hermes 生产库形状（distilled_qa 无 memory_feedback），断言拒绝 + harness 不崩。
6. **开关**：GOPI_NO_TASK_REGISTER=1 断言零写入。

## 11. 交付物声明（Rule 6d）

- 分支名：`feat/auto-register-feasibility-hy4`
- 恢复命令：`git show feat/auto-register-feasibility-hy4:devdoc/tasks-auto-register-feasibility.md`
- 主仓库确认：worktree 提交即入主仓库 refs（~/work/GoPi），master 未动，未 merge（PA 验收后处理）
- 本报告为可行性研究，**零实现代码**，未触碰 loop.go / tasks.go
