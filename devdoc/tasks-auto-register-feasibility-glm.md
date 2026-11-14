<!-- doc-class: normative -->
# tasks 台账自动登记机制 — 可行性研究（设计报告）

日期: 2026-08-28
分支: feat/auto-register-feasibility-glm（worktree: ~/workspace/gopi-auto-register-glm）
范围: 只做研究，不写实现。产出 = 本报告。
上游: tasks 台账实现报告 `devdoc/tasks-ledger-impl-report.md`；设计约束简报 `~/work/hermes-agent/devdoc/memory-catchup-brief.md`（A3/D/E/F 段）

---

## 结论（先行）

**条件可行。** tasks 台账可以、也应该自动登记运行时任务，但必须做成**纯 harness 侧行为**：在 session 末尾（defer，覆盖全部退出路径）读取 todoState 最终快照（数据源 b），经现有引擎函数（TaskNew/TaskUpdate）一次性写入 tasks 表。**不新增 LLM 工具、不注入新 prompt、不修改 LLM 行为、不触碰 loop.go 运行时任务状态**。

三个关键判断：

1. **手动登记是死路**：gopi 是无人值守自动化 agent，跑完任务的不是人，没人会敲 `gopi task new`。Rule 11 实证（memory_update 曾 1/21435，LLM 不会自觉用低使用率工具；task 连工具都不是，LLM 根本无调用面）→ **自动登记是台账有数据的唯一现实路径**，也是唯一能兑现 nick "记录永久留存用于追溯" 铁则的路径。
2. **登记 ≠ 控制，Stateless 可保住**：tasks.go 服务定位声明（tasks.go:8-14）= "AUDIT/TRAIL layer, not runtime control"。自动登记只是把 **LLM 自己声明过的** tasklist JSON（每轮 [TASK_LIST_BEGIN]...END）持久化镜像，写入方向是 LLM→harness→DB 单向，不存在 harness→LLM 的状态反馈回路。"LLM 拥有状态"指运行时控制权（每轮携带全量表），不受影响。
3. **简单方案 > 复杂方案**：不做每轮变化驱动 upsert（演化史诱惑大，但 task_id 每轮漂移使合并成为最复杂易错的组件，见 §5）；session 末尾一次性登记最终快照，天然规避幂等难题，热路径零成本。

---

## Q1. 时机：task-start / task-end / 每轮变化 / 仅完成时？

| 时机 | 优劣 | 判定 |
|------|------|------|
| task-start（首轮） | 与 taskStartSearchMessage（loop.go:144）对称，但**首轮 LLM 尚未输出 [TASK_LIST]**（tasklist 是 LLM 规划产物，loop.go:1093 在首轮才注入 search checkpoint），harness 无任务数据可登记；且一个 session 多任务，无从拆解 | 排除 |
| 每轮 tasklist 变化 | 演化史完整（任务创建/完成精确时间）；触发点现成（loop.go:1812 `a.TaskState.update(tasks)` 每轮已执行）；但每轮写库 + task_id 漂移合并难题（§5） | Phase 2 可选，本期不做 |
| task-end（R3 完成出口） | 与 completionCheckpointMessage（loop.go:132）对称；数据完整（最终全量表）；但**只覆盖正常完成出口**——max-iterations（loop.go:2083）/timeout（loop.go:1678）直接 return error，不经过任何完成出口；TASKEND-STORE-BYPASS 教训（简报 E：强制点必须覆盖所有退出路径或明确声明不覆盖） | 部分覆盖，不够 |
| 仅完成时 | = task-end 弱化版，同样漏异常退出 | 排除 |
| **session 末尾（defer）统一登记** | **覆盖全部退出路径**（正常 R3 / bug022 确认轮 / consecutiveEndTurn / max-iterations / timeout / error）；数据源= todoState 快照（任意时刻可读，不依赖退出路径变量）；热路径零成本（每轮无写）；任务创建时间≈session 开始（近似，中间演化史可从 session JSONL 回放，`gopi log --todo` 已有 balanced-prefix recovery） | **推荐** |

完成出口的细节佐证"末尾统一登记"的必要性：R3 出口有两个分支（substantive ≥40 chars 信任退出 loop.go:1894-1815；terse 走 bug022 确认轮 loop.go:1906-1827），另有 consecutiveEndTurn 退出（loop.go:1919）——若挂"完成出口"，每一处都要接线且异常路径天然漏；defer 一处接线全路径覆盖。

## Q2. 数据来源：(a) 解析 LLM 输出 / (b) todoState / (c) CLI 手动？

- **(a) 解析 LLM 输出 [TASK_LIST] JSON**：每轮解析已在 loop.go:1790 `a.parseTaskList(content)` 完成（含 balanced-prefix 恢复 tasklist.go:175，Unparseable 时 tasks=nil 不产垃圾）。但解析结果是**局部变量**，退出路径上不可靠（max-iterations 的 return 之前解析过也可能被覆盖/从未解析）；且任务数据已经进 todoState，重复解析纯浪费。
- **(b) harness 内存态 todoState（推荐）**：loop.go:358-348，每轮由解析结果更新（loop.go:1812-1724，`tasks != nil` 时才更新、nil 不清空），**任何时刻（含异常退出）快照可读**。它是"框架侧只读镜像"，读它持久化 = 张力最小。两处代码引用：`hasList` 区分"LLM 从未输出过 tasklist"；`tasks []TaskItem` 含 alt 字段已解析（id→task_id, content→summary，tasklist.go:84-94）。
- **(c) CLI 手动（现状）**：保留为互补渠道，不废除。关键互补点：tasklist 只有两态 progressing/completed（tasklist.go:11-14），而 tasks 表四态 CHECK（tasks.go:53-54）——**failed/cancelled 只能 CLI 手动产生，自动登记只能产生 in_progress/completed**。这个缺口本身就是"自动登记不越界"的证据（框架不可能替 LLM 声明失败）。

## Q3. Stateless 哲学张力

核心问题：自动写入 = 框架侧获得任务状态写入权，是否推翻 "LLM 拥有状态、框架只读兜底"？

- **不推翻**，前提是区分"登记"与"控制"两个概念：
  - 控制 = 决定任务状态是什么（新建/改状态/删任务）→ 仍 100% 属于 LLM（每轮携带全量表，ValidateTaskList 禁删 tasklist.go:257，heartbeat 只兜底提醒）。
  - 登记 = 把 LLM 已声明的状态持久化到审计表 → 是**单向镜像**（LLM→harness→DB），与 todo 工具读镜像（harness→LLM）方向相反，互不成回路。类比：session JSONL 记录 assistant 响应 = 框架记录 LLM 说过的话，没人认为这推翻 LLM 的话语权。
- **不越界的四条设计红线**（推荐方案全部满足）：
  1. **只登记不修改**：写入的所有 status/summary 值逐字来自 LLM 声明的 TaskItem，框架不产生新值、不改写摘要。
  2. **LLM 不可见**：不是工具、不注入 prompt（对比 memory checkpoint 是 prompt 注入；task 登记不需要——数据已在 todoState，无需说服 LLM 做什么）。LLM 行为零变化。
  3. **不改 task_id**：upsert 键用 LLM 的 task_id；冲突时优先 UPDATE 而非改写键（§5）。
  4. **不碰运行时状态**：todoState/心跳/nudge 保持只读，登记钩子只读快照，与 tasks.go 声明的 "does not touch loop.go's tasklist/todoState/heartbeat/nudge machinery" 一致。
- **LLM 显式 tool_use 才登记**（更保守的替代方案）：排除。违反 Rule 3（18 工具封顶）+ 登记率≈0（memory_update 1/21435 实证：LLM 不会自觉调用低使用率工具）→ 显式方案 = 台账永远空。

## Q4. Rule 3 工具最小化

- **不需要新 LLM 工具**。自动登记是纯 harness 侧代码（Agent.Run 内一个调用），不算工具。先例：taskStartSearchMessage/completionCheckpointMessage 是 harness 侧强制行为（loop.go:132-150）不是工具；todoState 是 harness 内部状态（loop.go:358）不是工具；heartbeat 提醒不是工具。
- **零新引擎函数也可以**：登记逻辑 = 现有 `TaskNew(summary, taskID, sessionID)`（tasks.go:202）+ `TaskUpdate(taskID, status, summary)`（tasks.go:355）组合；tasks.go 注释已预留 harness 复用（tasks.go:14 "CLI 与未来 harness 共用"、tasks.go:199-201 "sessionID... the harness can fill it later"）。为聚合语义建议加一个薄封装（如 `HarnessRegisterTasks(sessionID, tasks)`），但引擎复用，不是新工具。
- CLI 子命令不算新工具先例（gopi log/checkpoint，简报 B）——`gopi task` 已存在，无新增。

## Q5. 幂等 / 去重（task_id 稳定性）

事实链：task_id 是 LLM 自定义字符串（TaskItem.TaskID 为 FlexibleString 兼容数字 id，tasklist.go:68-78）；ValidateTaskList 只查**本轮内**无重复/禁删（tasklist.go:257-281），不保证**跨轮**稳定；tasks 表 task_id 全局 UNIQUE（tasks.go:51）。

- **每轮 upsert 方案的致命点**：LLM 每轮重发全量表，task_id 可能改写（"1"→"t-1"、"check"→"check-2"）→ 同一任务分裂为多条记录；要合并就得模糊匹配（同 session 内 summary 相似度），这是本设计中最复杂易错的组件，且永远有误合并风险。
- **末尾一次性登记天然规避**：只写最终版本，无多轮合并问题，task_id 漂移不再有害（中间态丢失可接受——审计关注"做了什么、完成没有"，中间态可从 session JSONL 回放）。
- **残留的两个冲突面及对策**：
  1. 同 session 内同名 task_id 重复出现（tasklist 全量表本身禁重复，但跨轮登记只发生一次）→ 不存在。
  2. **跨 session 同名 task_id**（不同 LLM 会话都可能用 "task-1"）：task_id 全局 UNIQUE → 第二次 INSERT 报错。对策：登记时先 `taskByID`（tasks.go:165）查重——不存在 → TaskNew；存在 → 视为同任务 TaskUpdate（status/summary 覆盖）。**但跨 session 同名语义上不是同一任务**，覆盖会污染历史记录 → 更稳妥：查重命中且 task_id 非本 session 创建时，重生成带 session 前缀的稳定键（如 `t-<session前8>-<n>`，沿用 t-<unix> 碰撞后缀机制 tasks.go:216-234），原始 task_id 不进表。此决策留实现期验证，报告中标记为开放项。
- 登记入口自身幂等：defer 只执行一次（单次 Run 生命周期），无双写。

## Q6. 失败模式

| 失败场景 | 行为 | 依据 |
|---------|------|------|
| 解析失败（BEGIN-without-END Unparseable → tasks=nil） | todoState **不更新**（loop.go:1812 仅非 nil 更新），保留上一轮快照 → 末尾登记用最后已知状态，**不丢** | tasklist.go:35、loop.go:1812 |
| LLM 从未输出 tasklist（hasList=false） | **不登记**，不伪造记录 | ErrTaskListUnparseable 教训（绝不产垃圾 list），tasklist.go:35；todoState.hasList loop.go:361 |
| 异常退出（max-iterations/timeout/crash） | defer 兜底覆盖——快照在内存随时可读 | loop.go:1606 defer / 1588 / 1932 |
| DB 写失败 | **log warning，不阻塞退出**（审计层降级不拖垮运行时） | memory bump 失败不降级先例（简报 A2） |
| 逐条失败 | 一次登记多条，逐条独立，单条失败不影响其余 | — |
| resume / compact | resume 新 session 自有 session_id，登记自己的记录，原 session 记录不动 → 无重复 | 简报 A3：resume 语义=续跑，对称豁免两端 checkpoint 先例 |
| 登记时机 vs SessionLog.Close | 登记钩子放 defer 内 **LogSessionEnd/Close（loop.go:1633-1544）之前**，保证能取到 session_id | loop.go:1606-1551 |

## Q7. 收益 vs 成本

**收益**：
- 手动登记的现状 = 登记率趋近零（无人值守 agent 无"人"）；自动登记是台账有数据的唯一路径 → **直接兑现 nick 铁则"所有任务记录永久留存用于追溯"**（简报 F7，gopi_big_view）。
- 结构化查询面：`gopi task list --status` 走 idx_tasks_status（tasks.go:62 value-signal）；跨会话聚合（"这周跑过哪些任务、哪些 failed"）CLI 手动时代不可能有数据。
- **feedback.task_id join 锚点**：memory_feedback 表已有 task_id 按任务去重先例（简报 A1），自动登记让 feedback 的任务级聚合有可 join 的实体键。
- 与 `gopi log --todo`（session JSONL 回放）互补：回放是"每 session 的文本视图"，台账是"跨 session 的结构化查询面"，两者服务不同检索需求。

**成本**：
- 实现 ≈ 一个 defer 钩子 + 一个聚合封装函数 + 映射/查重逻辑，~100 行 + 单测 + E2E（对照 tasks-ledger 11+8 测试规模）。
- 热路径成本 = 0（不在每轮写；defer 一次写 N 行，WAL + SetMaxOpenConns(1) 下毫秒级）。
- 风险：summary 质量由 LLM 决定（与 CLI 手动同源，不更差）；task_id 跨 session 冲突需 §5 对策。

**判定**：自动登记与 nick 目标（永久留存追溯）的匹配度**显著高于** CLI 手动。CLI 保留为四态补全渠道。

---

## 推荐设计（接口层，不写实现）

```
触发点    Agent.Run 的 defer（SessionLog.Close 之前，loop.go:1543 前）
数据源    a.TaskState（todoState 快照；hasList && len>0 才登记）
接口      internal/tools 新增薄封装 HarnessRegisterTasks(sessionID string, tasks []TaskItem) (int, error)
          —— 内部复用 openTasksDB（外库保护原样生效）+ taskByID + TaskNew + TaskUpdate
状态映射  progressing → in_progress；completed → completed
         （failed/cancelled 无来源，CLI 手动补 —— 文档明示）
查重      逐条 taskByID(task_id)：不存在 → TaskNew(summary, task_id, session_id)
          存在 → TaskUpdate(status 或 summary)；跨 session 同名 → 重生成带 session 前缀的键（开放项，见 §5）
失败      逐条 log warning，不中断；整体失败不影响 Run 退出
开关      GOPI_AUTO_REGISTER_TASKS（默认开；NoEnforceMemory 开关先例 loop.go:1954，
          供 A/B 隔离 / 出问题一键关）
衔接      session_id 列（tasks.go:56）——CLI 不暴露但引擎已接受，正是 harness 预留位
```

Phase 2（本期明确不做）：每轮变化驱动 upsert（记录任务演化时间线）——需先解决 task_id 漂移合并（summary 模糊匹配），复杂度收益比不划算；中间态已有 `gopi log --todo` 回放覆盖。

## 风险与边界（对照硬边界复述）

1. **Stateless 张力**：登记=单向镜像（§3），红线四条（只登记不修改 / LLM 不可见 / 不改 task_id / 不碰运行时状态）必须写进实现注释。
2. **Rule 3**：无新工具（§4），引擎复用，薄封装不算工具。
3. **幂等**：末尾一次性登记规避每轮漂移（§5）；跨 session 同名冲突是对策重点，开放项。
4. **不碰 loop.go 运行时逻辑**：钩子只读快照、只写 DB，todoState/心跳/nudge 全部不动（与 tasks.go:8-14 声明一致）。
5. **外库保护**：harness 登记走 openTasksDB（tasks.go:120），hermes 生产库仍拒绝（distilled_qa 无 memory_feedback）。
6. **Rule 11 适用性**：自动登记非工具，无 LLM 调用面，Rule 11 不适用；替代验证 = E2E 测试（临时 GOPI_MEMORY_DB 跑一个真实/模拟 session，断言 tasks 表行）——沿 tasks-ledger 手工 E2E 惯例（impl-report 第 44-68 行）。
7. **实现验收前置**：登记数据质量抽查（真实 session 跑 5-10 个，看 summary/task_id/status 分布），确认无垃圾行（对照 memory-lowquality 教训：value-signal 列不 inert——status 是登记方产生的唯一可过滤面，必须来自 LLM 声明值）。

## 一句话结论

**可行**：session 末尾 defer 一次性把 todoState 最终快照写入 tasks 表（纯 harness 侧、引擎复用、不新增工具、不注入 prompt），是让审计台账有数据、兑现"记录永久留存"的唯一现实路径，且不推翻 Stateless 哲学——登记是 LLM 状态的单向镜像，不是框架控制权。
