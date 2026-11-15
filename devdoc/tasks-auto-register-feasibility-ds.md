<!-- doc-class: normative -->
# tasks 台账自动登记机制 — 可行性研究

日期: 2026-08-28
分支: feat/auto-register-feasibility（worktree: ~/workspace/gopi-auto-register）
性质: **设计报告，只做研究不写实现**（硬边界：不碰 loop.go 运行时逻辑）
参考: internal/tools/tasks.go、internal/agent/loop.go、internal/agent/tasklist.go、
      ~/work/hermes-agent/devdoc/memory-catchup-brief.md（A3/D/E/F 段）、
      devdoc/tasks-ledger-impl-report.md

---

## 0. 结论先行（中文）

**条件可行，推荐实现。** 研究问题"harness 能否/应否在 task-start/task-end 自动写入 tasks 表"——答案是 **应，且技术上完全可行**，但只推荐在 **task-end（R3 完成出口）** 这一个时机登记，不推荐 task-start 或每轮变化登记。

推荐设计一句话：**在 loop.go 的 all-completed 完成出口（与 `maybeEnforceCompletionStore` 同点），把当前轮已解析的 `[]TaskItem` 逐条以"已存在则 UPDATE 到 completed、不存在则 INSERT"的方式写入 tasks 表（复用 `tools.TaskNew`/`TaskUpdate` 引擎函数，`session_id` 填 SessionLog 的 SessionID），一次性 latch 防重复，写库失败仅 log 不中断主流程。**

三条铁律约束下均不越界：
1. **Stateless 哲学不推翻**——自动登记是"任务结束后把 LLM 已声明的完成状态抄写进审计表"，是后置记录动作，不参与任何运行时决策（不读 todoState 做判断、不注入 reminder、不改 exit 逻辑）；运行时任务状态仍是 LLM 每轮携带，框架只读兜底。
2. **Rule 3 工具最小化不违反**——纯 harness 侧行为，不算新 LLM 工具（log/checkpoint/CLI 子命令先例）；不需要任何新工具面。
3. **append-only / 无 delete 铁则不破**——只 INSERT + 状态推进到 completed，不做 summary 修改、不做回退、绝无删除。

主要风险（均有对策）：LLM 的 task_id 是自定义字符串可能跨轮漂移（对策：只在完成出口单次登记 + 冲突走 UPDATE）；异常退出/compact/resume 路径不覆盖（对策：与 completion checkpoint 对称，明确声明边界，CLI 手动补录）；LLM 的 summary 质量不可控（对策：审计记录重事实轻摘要，CLI `gopi task update --summary` 可后续修正）。

---

## 1. 时机：task-start / task-end / 每轮 tasklist 变化 / 仅完成时

| 时机 | 数据可得性 | 写库频率 | 幂等风险 | 结论 |
|------|-----------|---------|---------|------|
| task-start（首轮前） | 差——此时 LLM 尚未规划出任务列表，无 summary 可登记（只能登记 session 级占位或 EPIC GOAL，TaskNew 要求 summary 非空，占位 summary 无追溯价值） | 1 次/run | 低 | **不推荐**。与 taskStartSearchMessage 同点但无任务数据可写，登记空壳=台账噪音 |
| **task-end（R3 all-completed 出口）** | **好——当前轮 tasks 变量已全 completed，summary 齐备**（loop.go:1889 `if tasks != nil && AllCompleted(tasks)`） | 1 次/run | 低（单次批量） | **推荐（本方案唯一登记点）** |
| 每轮 tasklist 变化 | 好——每轮 parseTaskList 都有结果 | N 次/run | **高**——LLM 的 task_id 每轮可能变（FlexibleString 接受任意字符串），同任务多轮产生多行 UNIQUE 冲突 | **不推荐**。且中间轮 LLM 有省略 tasklist 的 token 节省行为（loop.go:41-46 bug010 注释："DeepSeek's learned token-saving behavior of omitting the task list in intermediate rounds"），变化检测不可靠 |
| 仅完成时 | = task-end 的子集 | 1 次/run | 低 | 并入 task-end 方案，不单列 |

**论据**：
- task-end 出口是现有 harness 唯一"任务真正结束"的语义点——`AllCompleted(tasks)` 判定 + `maybeEnforceCompletionStore` 已在此挂载（loop.go:1889-1809），复用同点零新增循环逻辑。
- memory-catchup-brief.md A3 段明示："**这是 gopi 唯一的 'harness 强制行为' 模式——一次性 latch + 完成出口注入，tasks 台账若要自动登记可复用同款机制**"——即设计简报本身就预见了 task-end 出口是登记挂载点。
- 每轮变化登记会撞上 TASKEND-STORE-BYPASS 教训（brief E 段）：强制点必须覆盖所有退出路径或明确声明不覆盖——每轮登记看似覆盖更全，实则把幂等复杂度推给每轮，且 LLM 省略 tasklist 行为使"变化"信号本身不可靠。

## 2. 数据来源：解析 LLM 输出 vs harness 内存态 vs CLI 手动

| 来源 | 机制 | 优点 | 缺点 |
|------|------|------|------|
| (a) 解析 LLM 输出 [TASK_LIST] JSON | `ParseTaskList`（tasklist.go:120）：LastIndex 找 BEGIN，JSON unmarshal；END 缺失时 `ParseTaskListBalancedPrefix` 平衡前缀恢复 | 权威（LLM 声明的原始状态）；恢复机制实测 23/23=100% 恢复率（tasklist.go:156 注释）；Unparseable 时返回 nil 绝不产生垃圾列表（ErrTaskListUnparseable） | 依赖 marker 纪律；中间轮可能省略（bug010）；恢复路径有 warn |
| (b) harness 内存态 todoState | `todoState{mu, tasks, hasList}`（loop.go:358），每轮 `TaskState.update(tasks)`（loop.go:1812）同步，只读镜像 | 已是解析结果的缓存，零重复解析；nil 不清空已有状态 | **不是独立数据源**——它就是 (a) 的结果缓存；且 `hasList` 语义与"本轮是否有声明"相关，直接读它做登记判断会引入陈旧列表风险 |
| (c) CLI 手动（现状） | `gopi task new/update`（cli_tasks.go） | 质量最高（人工摘要）；可登记 failed/cancelled | **覆盖率趋近 0**——memory_update 1/21435 的教训（brief F/工具没人用=白写）：LLM/人不会自觉执行 CLI 登记；台账追溯目标落空 |

**推荐**：数据源 = **R3 出口处当前轮的 `tasks` 变量**（即 (a) 的当轮解析结果，已在 loop.go:1889 作用域内，无需额外解析、无需读 todoState）。理由：
- 零新增解析成本——`parseTaskList` 每轮已跑（loop.go:1790），R3 分支直接复用其输出。
- 避免 todoState 的陈旧性——todoState 累积的是历史列表（nil 不清空），而登记只应反映"本轮声明的完成态"；直接用当轮 `tasks` 语义最干净。
- 解析失败自然不登记：Unparseable → tasks=nil → R3 分支不进入 → 不产生错误记录（失败模式见 §6）。
- session_id 填充：`a.SessionLog.SessionID()`（loop.go:2152 sessionLabel 同源），TaskNew 的 sessionID 参数自实现起就预留此用途（tasks.go:201："the harness can fill it later"；tasks-ledger-impl-report.md 决策 5："session_id 列预留……harness 未来自动登记可用"）。

## 3. 与 Stateless 哲学的张力：会不会推翻"LLM 拥有状态、框架只读兜底"

**不推翻，条件是"只登记不参与"**。逐层拆解：

1. **Stateless 的实际含义**（brief E 段引 the operator 原话）："每次 response 携带全量表，scheduler 纯兜底，浪费一点 token 换零同步复杂度"——它约束的是**运行时任务状态的控制权**：谁决定任务进行到哪、谁驱动进度。LLM 每轮携带 tasklist，框架只读（todoState）+ 兜底（heartbeat/nudge）。
2. **tasks 表的定位**（tasks.go:8-9 注释原文）："the ledger is an AUDIT/TRAIL layer, not runtime control. It does not touch loop.go's tasklist/todoState/heartbeat/nudge machinery"——台账是**跨会话审计记录**，与运行时控制分层。tasks-ledger-impl-report.md 决策 1 同样明确。
3. **自动登记做了什么**：在任务已经结束（LLM 声明全部 completed、R3 出口已判定）之后，把**既成事实**（task_id/summary/状态=completed）抄写入审计表。它：
   - 不读 todoState 做任何运行时判断（只消费当轮 tasks 变量）✓
   - 不注入新 prompt / 不改 exit 逻辑 / 不影响 heartbeat / nudge ✓
   - 不修改任务状态本身——LLM 说 completed 才登记 completed，框架从不自作主张 ✓
4. **越界设计是反面教材**：若自动登记在任务中途读取 todoState 并"纠正"或"同步"状态，或把台账写回 tasklist 影响下一轮上下文——那就确实推翻 Stateless。本方案明确不这样做。
5. **登记 vs 控制正交性**：Stateless 管"状态怎么活"，台账管"事实怎么留"。登记是单向抄写（LLM → 表），控制是双向协商（LLM ↔ 框架）——单向抄写不产生控制权。brief F-7 引 the operator："LLM 仅能新增、修改摘要、修改状态；任何场景不能删除；调度器只读过滤，记录永久留存用于追溯"——**"记录永久留存"本身就是 the operator 拍板的 Stateless 配套要求**，自动登记是它的执行手段，而非对立面。

**附加约束（设计上写死）**：登记只做 INSERT + 状态推进到 completed；不自动改 summary（LLM 摘要质量不可控，但审计重事实；人工可用 `gopi task update --summary` 修正）；不做 completed→in_progress 回退（CLI 手动才允许）；绝不删除。

## 4. Rule 3 工具最小化：需要新 LLM 工具吗

**不需要，纯 harness 侧行为，零新工具面。**

- 登记逻辑 = loop.go 内的一个私有方法（如 `registerTasksToLedger(sessionID string, tasks []TaskItem)`），内部遍历调用现有 `tools.TaskNew` / `tools.TaskUpdate` 引擎函数——tools 包**不新增任何导出**，agent 包不新增任何 LLM 可见工具。
- Rule 3 先例链完整：`gopi log` / `checkpoint` / `gopi task` CLI 子命令均不算新工具（brief B 段："CLI 子命令不算新工具"）；harness 内部调用引擎函数比 CLI 更"隐形"。
- 对比反面方案：若做成"LLM 显式 tool_use 才登记"（如 task_register 工具），则直接违反 Rule 3（18 工具封顶），且 memory_update 1/21435 的教训表明 LLM 自觉调用率趋近零——**显式 tool_use 方案既违反 Rule 3 又达不到覆盖目标**，双重否决。
- system prompt 密度门（≤4500 chars，TestSystemPromptDensity 守护）零影响：无任何 prompt 变化。

## 5. 幂等 / 去重：同一任务多轮更新如何合并

**核心风险**：LLM 的 task_id 是自定义字符串（tasklist.go:68 `TaskID FlexibleString`，接受任意字符串/数字），跨轮可能漂移。tasks 表 task_id UNIQUE（tasks.go:51），TaskNew 对重复显式 ID 直接报错（tasks.go:240-242："duplicate is an error, never silently mangled"）。

**对策组合**：
1. **单次登记（结构性去重）**：只在 task-end 出口登记一次，每 run 最多写 len(tasks) 行——没有"多轮更新"场景，漂移只在跨轮中间态存在，最终列表以完成出口那轮为准。这是选 task-end 而非每轮登记的根本原因（§1）。
2. **冲突走 UPDATE**：对每个 task_id 先 `TaskUpdate(taskID, "completed", "")` 探测（不存在会报错，tasks.go:407-409），报错则回退 `TaskNew(summary, taskID, sessionID)`；成功则已是 completed（幂等收尾）。或更简单：先 `TaskNew`，ErrDuplicate 时改走 `TaskUpdate`。两种顺序皆可，实现时选一种，报告不锁定。
3. **跨 session 同名 task_id**（如 resume 续跑、多次运行同任务）：保留首次记录（审计完整性，the operator "记录永久留存"），冲突仅 log 提示；resume 语义本身跳过登记（§6），所以正常 resume 不会撞。
4. **task_id 为空**：TaskNew 缺省自动生成 `t-<unix>`（tasks.go:216-217），不违反 UNIQUE。
5. **latch 防重复**：登记用一个 per-run 布尔（如 `tasksRegistered`），与 `completionStorePrompted`（loop.go:2120）同款——R3 出口可能因 `maybeEnforceCompletionStore` 返回 true 而 continue 重入，latch 保证登记只发生一次。

## 6. 失败模式：解析失败 / 异常退出 / compact 时如何保证不丢不错

**设计原则：与 completion checkpoint 完全对称（A3 段模式 + TASKEND-STORE-BYPASS 教训）。**

| 场景 | 行为 | 理由 |
|------|------|------|
| 解析失败（BEGIN 无 END 且 balanced-prefix 失败） | tasks=nil → R3 分支不进入 → **不登记**，无错误记录 | ErrTaskListUnparseable 语义（tasklist.go:35）：绝不产生垃圾列表；登记层同享"no garbage"保证 |
| 恢复解析（ErrTaskListRecovered） | 当轮 tasks 有效 → 正常登记（recovered 列表是"一等公民"，tasklist.go:113-118 下游全路径共享） | 与既有下游一致，登记不区分 recovered/normal |
| 异常退出（max-iterations / timeout / content_filter / 用户中断） | **不登记**——R3 出口不可达 | 与 `maybeEnforceCompletionStore` 一致（A3："异常退出与 resume 路径刻意不强制"）；session JSONL 的 session_end 事件已带 exit_code/iterations/tokens（brief C 段），审计面仍在；补录靠 CLI 手动 |
| compact / resume | **跳过登记**（resume 语义=续跑，对称跳过两端 checkpoint） | A3 段既有决策；resume 后若最终正常完成，新的 R3 出口会登记（session_id 为新 session，task_id 冲突走 §5.2 UPDATE 收尾） |
| 写库失败（DB 锁/只读/外库保护拒绝） | **登记失败仅 log，不中断主循环** | 审计层失败降级：登记是增值动作，绝不让台账故障杀死任务执行（与 memory_search 的 best-effort bump 同哲学，brief A2："失败不降级"的对称面是"失败不致命"）；外库保护（tasks.go:153-155）天然防误写 |
| LLM 状态缺失 | tasklist 只有 progressing/completed 两态（tasklist.go:12-14）→ **failed/cancelled 无自动来源**，只能 CLI 手动登记 | 边界声明：自动登记覆盖 completed；failed/cancelled 是 CLI 专属通道（四态 CHECK 约束保留完整语义） |

## 7. 收益 vs 成本：真的比 CLI 手动更有价值吗

**收益（显著 > 成本）**：
1. **覆盖目标是核心驱动**：the operator 铁则"记录永久留存用于追溯"（brief F-7）在纯 CLI 手动下**实际不可达**——memory_update 1/21435 实证 LLM 自觉调用率≈0（brief F 段），人也不会记得每任务跑 `gopi task new`。自动登记是唯一能让"每次运行的全部 completed 任务" 100% 入表的机制。
2. **跨会话追溯从"可能"变"必然"**：tasks.go 开篇定位"later sessions can trace what was done"——自动登记让 tasks 表真正成为可依赖的审计源，而不是靠运气的空表。
3. **与 feedback.task_id 聚合打通**：memory_feedback 表已有 task_id 列（每 record 每 task 一票去重，brief A1 段）——自动登记后，台账 task_id 与 feedback 的 task_id 同源（都取 LLM 声明值），未来"按任务看反馈/看执行"的 join 成为可能（tasks-ledger-impl-report.md Not Done 段明确"memory_feedback 的 task_id 聚合展示留未来"——自动登记是激活该未来的前提）。
4. **成本极低**：无新工具、无 schema 变更、无 prompt 变化、无新依赖；改动面 = loop.go R3 出口一个私有方法 + 对应测试；引擎函数全部复用。

**诚实对比 CLI 手动的优势**：CLI 摘要质量更高（人工/LLM 主动写）、可登记 failed/cancelled、可改状态回退。**结论：两者互补而非替代**——自动登记保证基线覆盖（completed 100% 入表），CLI 保留为增强通道（failed/cancelled、人工修正摘要/状态）。这正是"自动兜底 + 手动精修"的分层，与调度器"纯兜底"哲学同构。

**结论**：自动登记比纯 CLI 手动**更匹配 the operator 的追溯目标**——后者让台账沦为仪式性空表，前者让"永久留存"落到实处。

---

## 8. 推荐设计（若实现）

```
┌─────────────────────────────────────────────────────────────────┐
│ LLM 每轮输出 [TASK_LIST_BEGIN]...[TASK_LIST_END]                 │
│   → parseTaskList() 每轮解析（loop.go:1700，已有）                │
│   → R3 出口判定（loop.go:1799: tasks != nil && AllCompleted）     │
│        │                                                         │
│        ├─ maybeEnforceCompletionStore()（已有，memory_store 强制） │
│        └─ registerTasksToLedger()（新增，本次研究推荐）            │
│              │  latch: tasksRegistered 布尔（per-run）            │
│              ▼                                                   │
│        对每个 task:                                               │
│          TaskNew(summary, task_id, sessionID)  ← sessionID 用     │
│          │   SessionLog.SessionID()                               │
│          └─ 冲突(UNIQUE) → TaskUpdate(task_id, "completed", "")   │
│         写库失败 → log 警告，不中断循环                            │
└─────────────────────────────────────────────────────────────────┘
```

**接口**（零新导出）：
- `agent` 包私有方法 `func (a *Agent) registerTasksToLedger(sessionID string, tasks []TaskItem)`（放 loop.go 或新文件 taskledger.go），内部遍历调 `tools.TaskNew` / `tools.TaskUpdate`——tools 包**零改动**（引擎函数已齐备，TaskNew 的 sessionID 参数自实现起预留）。
- 触发点：loop.go R3 出口（all-completed + substantive 分支，`maybeEnforceCompletionStore` 同点）。
- 数据流：当轮 `tasks` 变量（解析结果，非 todoState）→ 逐条登记 → 失败降级 log。
- 与 tasks.go 衔接：完全复用 openTasksDB 的外库保护/append-only/schema 自愈；session_id 从"预留空列"变为"harness 真实填充"；无 delete、无回退、无 summary 改写。

**测试建议**（实现阶段）：TaskNew 冲突→UPDATE 幂等路径；同 session 双 run 不重复；Unparseable 不登记；resume 跳过；写库失败不 panic；latch 重入只登记一次。

## 9. 风险与边界（汇总）

1. **Stateless 张力**：已化解——单向抄写（LLM→表）、后置动作、不参与运行时决策；若未来有人把台账读回 tasklist 或中间轮同步状态，即越界（本设计明确禁止）。
2. **Rule 3**：零新工具；反面方案（LLM tool_use 登记）双重否决。
3. **幂等**：task_id 漂移靠"单次登记 + 冲突 UPDATE"兜住；跨 session 同名保留首次。
4. **覆盖边界（声明式）**：异常退出 / compact / resume 不自动登记（与 completion checkpoint 对称，session_end 统计留审计面，CLI 补录）；failed/cancelled 无自动来源（tasklist 两态协议限制，CLI 专属）。
5. **summary 质量**：LLM 摘要可能含糊——审计重事实（时间/session/状态）轻摘要，CLI 可修正；不自动改写（防"框架改 LLM 内容"争议）。
6. **外库保护**：登记永远只落 gopi 自己的 memory.db（openTasksDB 检查 memory_feedback 表存在性，tasks.go:148-156），hermes 生产库不受影响。

---

### 附：关键代码引用索引
- tasks 表 schema / 引擎函数：`internal/tools/tasks.go:296-306`（schema）、`:202`（TaskNew，sessionID 预留）、`:355`（TaskUpdate，无 delete）、`:120-162`（外库保护）
- tasklist 解析与恢复：`internal/agent/tasklist.go:120`（ParseTaskList）、`:175`（BalancedPrefix，100% 恢复率）、`:31-35`（哨兵错误）、`:68-78`（FlexibleString / TaskItem）
- harness 强制点模式：`internal/agent/loop.go:132`（completionCheckpointMessage）、`:189`（taskStartSearchMessage）、`:4561`（maybeEnforceCompletionStore + latch）、`:1799-1809`（R3 出口）、`:1700`（parseTaskList 每轮）、`:333-348`（todoState）
- 设计约束：`~/work/hermes-agent/devdoc/memory-catchup-brief.md` A3（强制点模式+自动登记预判）、D（Rule 3）、E（store 绕过教训 / Stateless 哲学）、F-7（记录永久留存铁则）
- 实现报告：`devdoc/tasks-ledger-impl-report.md`（session_id 预留 / loop.go 集成刻意不做=未来可补）
