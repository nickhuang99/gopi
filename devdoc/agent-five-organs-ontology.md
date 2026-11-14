<!-- doc-class: normative -->
# 智能体五器官本体论 (Agent Five-Organs Ontology)

> 状态: **AUTHORED SPEC** (2026-09-10) — 本文档在写之前**不存在**于仓库、git 历史或 nick 的
> 设计目录中（见 §0 溯源）。它是一个**提案**，供 hippocampus epic 的三个子任务实现时对齐；
> 有争议的命名请以"哪一部分是 nick 原文 / 哪一部分是本文演绎"的标注为准。
>
> 锚点基准: 分支 `feat/hippocampus-spec`，基线 commit `2516e09`。
> 所有 `文件:行号` 均在 2516e09 上**实际验证过**（行内容与所述符号一致）。

---

## 0. 溯源（先讲清楚"哪些是原文、哪些是本文写的"）

| 材料 | 位置 | 性质 |
|---|---|---|
| 海马体设计（523 行） | `/home/user/work/claude/nick/create_hippocampus-agent.txt` | **nick 原文**。全篇 "海马体" 出现 211 次，"前额叶" 6 次，"记忆巩固" 2 次，"器官" **0 次** |
| 四要素本体论（70 行） | `/home/user/work/claude/nick/AI智能体的本体论与计算模型.md` | **nick 原文**：脑(推理)/眼(感知)/手(执行)/外置持久记忆(记忆) 四要素 + 自动机谱系 + 冯·诺依曼映射 |
| 语境 / 虚拟内存类比 | `/home/user/work/claude/nick/what_is_harness.txt`、`/home/user/work/claude/nick/builtin_memory_search_tool_call.txt` | nick 原文：harness 四职责、海马体 Memory-Agent 架构、pre-LLM hook 与主动 Agent 的区别 |
| **五器官命名与编号** | 本文 §2、§3 | **本文演绎**（nick 原文只有四要素 + 海马体/前额叶两个脑区隐喻，没有"五器官"清单） |

**诚实声明**：epic briefing 引用的 `devdoc/agent-five-organs-ontology.md` 是**幻影引用**——
仓库无此文件，git 历史无此文件（`devdoc/hippocampus-epic-reframe.md:47` 独立核实同一结论）。
因此本文不是"补全一个已有文档"，而是**按原文事实新写一份**。凡 nick 未说过的推论，均以
**[推论]** 标注；凡 gopi 代码事实，均给 `文件:行号`。

---

## 1. 为什么需要本体论：模型 ≠ 智能体

nick 原文（`AI智能体的本体论与计算模型.md` §0）的核心论点：

> 将具备对话能力的 LLM 直接等同于自主智能体，等同于将离体存活的人脑视作完整的人类个体。
> …… 每一次对话重置，都会丢失所有过程信息，**不具备时间维度的智能连续性**。

其自动机论证（§2）给出了硬上界：**DFA(仅上下文窗口) → PDA(栈式短期) → 图灵机(可随机寻址的
外存)**，进化"从未升级计算单元，只升级存储结构与寻址机制"。§4 结论：参数规模只决定**单轮**
质量；**分层存储 + 随机寻址 + 持久记忆**决定**任务上限与运行时长**。

这与 gopi 的实际处境完全吻合：gopi 的对话能力已经够用，缺的是**跨任务的连续性**——
"这次的路子上次已经走过"这件事，靠 LLM 的上下文窗口永远得不到。

**本体论的作用**：把"智能体"拆成**职责互斥、权责可分、可独立调度**的器官，让每个器官的
失败模式、预算、接口都能被单独设计和验收。这正是 hippocampus 能作为**独立子 Agent**
（而非 system prompt 里的一句话）的前提。

---

## 2. 五器官：定义与职责边界

四要素（脑/眼/手/记忆）+ gopi 已有的第五个**节律**器官。第五器官不是凑数：peerbox 四要素
只回答"智能体由什么构成"，不回答"**长任务跑到一半卡住了怎么办、谁来维持生命体征**"——
gopi 已经在生产里实现了它（心跳 + 通知管道，§4.5），只是从未被本体化命名。

| # | 器官 | 一句话职责 | 契约（**可以/禁止**） | gopi 落地包 |
|---|---|---|---|---|
| 1 | **海马体** Memory / Hippocampus | 把**往事**捞出来：解析意图 → 多轮检索 → 粗筛 → 产出记忆简报 | 可以：检索、筛选、标记显著性、存储固化<br>**禁止：情境判断**（"这条旧方案现在还能不能用"） | `internal/tools/memory*.go` |
| 2 | **前额叶** Decision / Prefrontal | 拿到素材后做**判断与决策**：旧方案适不适用、刻舟求剑否、要不要推翻 | 可以：最终决策、对外输出、调度裁决（含是否打断）<br>禁止：把记忆检索当次要工作塞进自己的 prompt | `internal/agent/loop.go` (`runEngine`) |
| 3 | **眼** Perception | 把外部世界变成可推理的输入 | 只读 | `internal/tools/{read,search,symbol,websearch,vision}.go` |
| 4 | **手** Execution | 把决策变成改变世界的操作 | 有副作用、需可审计 | `internal/tools/{bash,write,edit,job,text2img}.go` |
| 5 | **心跳/节律** Rhythm | 维持生命体征、发现停滞、向上报警 | 可以：观测、计时、触发提醒/告警<br>禁止：改变任务内容 | `internal/tools/taskheartbeat.go` + `tasknotify.go` + `internal/agent/taskheartbeat.go` |

### 2.1 为什么"记忆=核心器官"

epic 指定 memory=hippocampus 是核心器官，原文给了两条独立理由：

1. **能力上界由它决定**（`AI智能体的本体论与计算模型.md` §2/§4）：存储结构决定智能等级，
   感知/推理/执行只支撑单轮瞬时响应。
2. **它是唯一被 LLM 的"惰性"反复击穿的器官**（`create_hippocampus-agent.txt` 第 3 段 nick 的
   亲身案例）："我们之前已经实现了一个流程……他完全不知道，然后我说了半天，他又去重新去把它
   实现了"。被动 pre-LLM hook 的失配点在于——**它不思考该搜什么**，只执行硬编码检索；
   而"该搜什么"本身是智能（原文："记忆检索不是一个工具调用动作，它本身就是一种思考"）。

### 2.2 权责红线（本文最重要的一条裁决）

原文反复强调，海马体**只拥有情报研判权，没有任务调度的最高决策权**
（`create_hippocampus-agent.txt` 第 458/463/503 行区段）：

- 海马体只能产出 **`confidence`（关联置信度）** 与 **`memory_weight`（记忆固有权重）** 两个
  **原始客观指标**；
- **递送等级（Lv-0..Lv-3）由中央调度器结合"当前主任务状态"计算**，不由海马体判定
  （"海马体没有全局视角判断'现在是不是一个不能打扰的时刻'"）；
- 理由是反例极强：把打断权下放，"一段看似相关、实则无关的记忆就可能随意腰斩长任务流，
  系统行为变得不可控，就像随便一个职员就可以冲进会议室打断谈判"。

**[推论]** gopi 的中央调度器在这个 epic 里的落点有两个候选：`Agent.runEngine` 的轮边界
（同步通路，`internal/agent/loop.go:3230` 起的 for 循环 — 该行随 `runScopedRoot` 插入而移动；
锚点以符号为准：`repairToolUsePairs` 调用所在的 for 循环）与任务调度器的 serve tick
（异步通路）。Lv-0/Lv-1 不需要调度器；Lv-2/Lv-3 才需要，且必须带配额（§5）。

---

## 3. 器官协作：两条通路

nick 原文区分两条通路，二者**不是同一件事的快慢版**，而是用途不同：

```
输入
 ├─[通路 A 阻塞/同步前置] 高价值话题（项目、方案、bug、流程定义）
 │   输入 → 海马体(Flash 为主，可升档) 完成检索 → 记忆简报 → 前额叶(Pro) 再推理
 │   "我们不能允许先行动再后悔，记忆唤起必须前置，避免从头重复造轮子"
 │
 └─[通路 B 异步/并行非阻塞] 闲聊、模糊对话、低风险交互
     输入 → 分两路：前额叶立刻作答（不等）；海马体后台扫描（天然滞后）
     简报返回后做后校验：无关 → 什么都不做；高度相关 → 注入"迟来的记忆提醒"
     "亡羊补牢，未为晚矣" —— 不阻止本次已发出的回答，但阻止错误趋势继续演化
```

**关键设计选择**（原文明确）：海马体与前额叶是**两个独立的推理循环**，不是把"你要自己检索记忆"
塞进前额叶的 system prompt。两个顽疾：①主 Agent 优先处理用户任务，记忆检索变次要工作，容易被
忽略；②记忆思索逻辑与业务逻辑耦合，**检索的推理资源会和解决问题的推理资源互相抢占**。
拆成独立子 Agent = 把记忆唤起变成**第一道不可跳过的流水线关卡**。

**[推论] 在 gopi 的实现映射**：gopi 现在的被动通路是
`maybeInjectTaskStartSearch`（`internal/agent/loop.go:4521`）——它在**首轮之前**注入一条
`taskStartSearchMessage`（`internal/agent/loop.go:189`）用户消息，**强制**模型先调一次 `memory_search`，
外加 hot-tier 摘要（`hotResidentMessagePrefix`，`internal/agent/loop.go:4557`）。
这正是原文批判的"确定性程序 + 硬编码检索"：它强制了**动作**（调工具），但没有、也无法
强制**思考**（该搜什么维度、要不要二次检索、命中了什么历史方案）。
本 epic 的通路 A 就是它的**替代者**，且必须**默认关闭**（§6）。

---

## 4. gopi 解剖表（器官 ↔ 包 ↔ 已验证锚点）

### 4.1 海马体 Memory

| 能力 | 符号 | 锚点 |
|---|---|---|
| 检索（多路召回：FTS5 exact + trigram + LIKE 2-char） | `MemorySearch(query, limit, intent)` | `internal/tools/memory.go:1499` |
| 命中结构（含 `Tokens` 覆盖率、`EffConfidence`、`PossiblyStale`、`FbUseful/FbUseless`） | `MemoryHit` | `internal/tools/memory.go:467` |
| 命中扫描 | `scanSearchHit` / `scanHit` | `internal/tools/memory.go:1786` / `:1790` |
| **原始命中切片（渲染前的 []MemoryHit）** | `hits := make([]MemoryHit, …)` + `json.Marshal(hits)` | `internal/tools/memory.go:1835` / `:1878` |
| 存储 | `MemoryStore(topic, question, answer, source, keywords)` | `internal/tools/memory.go:2344` |
| 单条读取 / 更新 / 删除 | `MemoryGet` / `MemoryUpdate` / `MemoryDelete` | `:2728` / `:2780` / `:2886` |
| 反馈（useful/useless 投票，驱动冷热） | `MemoryFeedback(id, rating, reason, taskID)` | `:2718` |
| 反馈重算 / 历史 / 建议降冷 | `FeedbackRecompute` / `FeedbackHistory` / `FeedbackSuggestCold` | `:2855` / `:3277` / `:2979` |
| 结果渲染（LLM 可读文本） | `renderMemorySearchResult` | `:4392` |
| 热层常驻摘要 | `MemoryHotResident()` | `:3100` |
| 最近列表 | `MemoryListRecent(n)` | `:3169` |
| 工作流层检索（并入 MemorySearch 引擎） | `ProceduralSearch(query, limit)` | `internal/tools/memory_procedural.go:375` |
| 库路径（单一来源） | `memoryDBEnv`=`GOPI_MEMORY_DB` / `memoryDBEnvLegacy`=`GOPI_HIPPO_DB` / `memoryDBPath()` | `internal/tools/memory.go:63` / `:68` / `:627` |
| 包自述 | 文件头注释：`Memory tools: memory_search / memory_store — gopi's hippocampus` | `internal/tools/memory.go:1` |

### 4.2 前额叶 Decision

| 能力 | 符号 | 锚点 |
|---|---|---|
| Agent 结构 | `type Agent struct` | `internal/agent/loop.go:1483` |
| 入口（新会话/续跑/共享引擎） | `Run` / `Resume` / `runEngine` | `:3090` / `:3101` / `:3125` |
| ReAct 轮循环（**轮边界 = 事件注入点**） | `for i := 0; a.MaxIterations == 0 \|\| i < a.MaxIterations; i++` | `:2818` |
| 轮边界注入（既有先例：后台 job 完成通知） | `tools.TakeJobNotices()` 循环注入 user 消息 | `:2897` |
| system prompt 组装（有序区块注册表，密度目标 <4.5K） | `promptSections` / `PromptSection` / `assembleSystemPrompt` | `:1052` / `:1034` / `:1075` |
| 工具定义（Rule 3 唯一来源） | `claudeToolDefs` | `:1148` |
| 被动记忆通路（本 epic 的替代对象） | `maybeInjectTaskStartSearch` / `hotResidentMessagePrefix` / `maybeEnforceCompletionStore` | `:4521` / `:4557` / `:4561` |
| 强制消息文本 | `taskStartSearchMessage` / `completionCheckpointMessage` / `resumeContinuationMessage` | `:210` / `:198` / `:186` |
| 关闭开关 | `Agent.NoEnforceMemory` / `GOPI_NO_ENFORCE_MEMORY` | `:2056` / `:2054` |
| memory 工具分发（in-process 调用点） | `case toolnames.ToolMemoryActionSearch:` → `tools.MemorySearch(...)` | `:5369` → `:5378` |
| 用户级硬中断（**唯一现存中断通道**） | `Interrupt()` / `interruptedBySignal()` / `interruptRoot()` | `:2448` / `:2465` / `:2475` |
| 停滞看护（真实在跑的那个） | `maybeNudgeStaleTaskList` | `:4501` |

### 4.3 眼 Perception / 4.4 手 Execution

`internal/tools/read.go:92 Read`、`search.go`、`symbol.go`、`websearch.go`、`vision.go:116 VisionAnalyze`；
`bash.go`、`write.go`、`edit.go`、`internal/tools/job.go`（`JobNotice` `:83`、`TakeJobNotices` `:297`、`jobManager` `:91`）、`text2img.go`。
职责分离的工程价值在于**预算与权限分级**：眼只读、手有副作用、心跳只观察——三者不该共享同一个预算池。

### 4.5 心跳/节律 Rhythm

**必须在实现前知道的代码事实（否则会照着已死代码设计）**：

| 层 | 符号 | 状态 | 锚点 |
|---|---|---|---|
| 旧的 `HeartbeatState` 状态机 | `defaultHeartbeatInterval`=30s / `defaultStaleRounds`=3 | **已删除**（2026-09-29：零生产调用者 → 状态机连同两个 env 旋钮一并移除，`devdoc/heartbeat-knobs-removed.md`） | — |
| | `NewHeartbeatState` / `SetOnRemind` / `ShouldRemind` / `ReminderCount` | **零生产调用者**（删除前实测；删除后符号不再存在） | — |
| | `Agent.HeartbeatInterval` / `HeartbeatStaleRounds`（写入但**从不读取**） | 死字段，已随旋钮删除 | — |
| 撤线原因 | commit `f81bf48` *refactor(agent): revert to simple ReAct loop — remove task-list enforcement* | 历史事实 | `git log` |
| **实际在跑的心跳** | `taskHeartbeatThrottleMin`=30s / `taskHeartbeatStaleThreshold`=30min | 生产活跃 | `internal/agent/taskheartbeat.go:34` / `:38` |
| | 两列原始事实：`last_toolcall_at` / `tasklist_issued_at`（每列独立时钟） | | `internal/agent/taskheartbeat.go:52` 起 |
| | `TaskHeartbeatConn` / `OpenTaskHeartbeatConn` | | `internal/tools/taskheartbeat.go:26` / `:66` |
| | `DetectStaleTasks(thresholdSec)`（**查询侧**检测） | | `internal/tools/taskheartbeat.go:142` |
| 通知管道 | `GOPI_TASK_NOTIFY_EVENTS`（默认 `claimed,in_progress,completed,failed`） | | `internal/tools/tasknotify.go:63` |
| | `loadTaskNotifyConfig` / `taskNotifyPayload` / `notifyTaskTransition` | | `internal/tools/tasknotify.go:127` / `:180` / `:280` |
| | `NotifySyntheticEvent(event, sessionID, model, summary)`（合成事件的唯一出口） | | `internal/tools/taskheartbeat.go:40` |
| 事件名先例 | `stale` / `loop-stuck` / `loop-stuck-toolful` / `session-wallclock-exceeded` | | `internal/tools/taskheartbeat.go:22` / `internal/agent/loop.go:292` / `:332` / `:304` |
<!-- merge-note (feat/cov-agent ⟷ master): both sides had re-keyed this row's
anchors and BOTH were stale in the merged tree. Re-keyed to the tree's
authoritative values, derived (not guessed) via
`grep -n 'NotifySyntheticEvent(notifyEvent' internal/agent/loop.go` → 1885 / 2076
and `grep -n 'notifyEventLoopStuck\|notifyEventWallclockExceeded' internal/agent/loop.go`
→ const defs 256 / 268 / 293 (re-measured on the task-report branch, whose
prompt-block edits shifted loop.go by a few lines — a stale literal here is the
drift this test exists to catch). Master's superset prose
(`session-wallclock-exceeded`) is preserved; neither side's content was dropped. -->

**[推论] 对 epic 的影响 —— 一个必须纠正的假设**：`devdoc/hippocampus-epic-reframe.md` 把
`internal/agent/heartbeat.go` 称作 "a natural Lv-0/Lv-1 host"。**代码事实不支持**：该状态机
自 `f81bf48` 起无人调用（该文件已于 2026-09-29 随两个空旋钮一并删除）。Lv-0/Lv-1 的宿主应当是**真实在跑**的那层：
`TaskHeartbeatConn`/`DetectStaleTasks`（每 tick 的存活探查）+ `NotifySyntheticEvent`（外送）
+ `runEngine` 的轮边界注入（`internal/agent/loop.go:3609` 的 job-notice 先例）。详见 `devdoc/hippocampus.md` §4。

---

## 5. 断裂面与失败模式（设计时必须前置纳入）

| 失败模式 | 原文论述 | 工程后果 | 缓解（本 epic 的裁决） |
|---|---|---|---|
| **假关联（记忆误唤起）** | 子 Agent 可能把无关历史强行绑定到当前话题 | 错误上下文注入，比"没搜到"更糟 | 简报必须带 `confidence`；海马体自校验一步；阈值以下只走 Lv-0 |
| **职责漂移** | 能力变强后开始介入解答/规划 | 定位模糊，权责失守 | 输出**只能**是结构化简报，**禁止**产出回答性文本；契约先行 |
| **预算失控** | 升档由海马体自决 → 极端情况每轮深度检索 | 成本回到高位 | 预算控制器：单位时间高阶检索配额 + 单会话深度检索上限，超限强制回落 Flash |
| **资源膨胀** | 每条消息一个后台协程 | 高频会话下并发检索爆炸 | 队列 + 限流 + 任务丢弃（会话已转新话题 → 终止未完成的检索） |
| **上下文撕裂** | 主 Agent 已输出一大段，后台才发现相反经验 | 补正消息造成对话逻辑断裂 | 迟来记忆**只能作为提示**，不能无条件推翻已发出的输出；两种策略可选：温和提醒 / 主动发起重新审视 |
| **漏召回（新增风险）** | `/home/user/work/claude/nick/builtin_memory_search_tool_call.txt:171`–`:176`（原文 "代价 3"）与静态大 prompt 的对比 | 静态 prompt 至少"信息都在，只是信号弱"；海马体判错则**信息直接消失** | 逃逸出口：前额叶可发"请调取 XX 历史记忆"的**反向请求**（双向交互，非单向流水线） |

---

## 6. 四级递送通路（Lv-0..Lv-3）

原文 §二（`create_hippocampus-agent.txt` 第 466–496 行区段，注：文件名为 `create_hippocampus-agent.txt`）：

| 级别 | 名称 | 何时用 | 行为 | 侵入性 |
|---|---|---|---|---|
| **Lv-0** | 日志归档 | 闲谈、无关弱关联 | 只写会话事件日志；**不注入上下文** | 无 |
| **Lv-1** | 后置追加提示 | 重要但不紧急（历史约定、踩过的坑） | **不打断**当前轮；本轮结束后追加独立提示（`【海马体备忘】…`） | 无 |
| **Lv-2** | 上下文内消息插队 | 情报足以改变论证方向，但不必停止当前动作 | 不终止执行流，向任务状态总线插一条**高优先级元事件**；多步循环在**子步骤间隙轮询**读到并调整后续 | 中 |
| **Lv-3** | 系统软中断 | 正在规划的是已明确踩过、代价极高的灾难路径 | 向调度器发**中断申请**，校验置信度 + 配额通过后优雅终止任务流，以简报为首要上下文开新回合 | 高 |

**保护锁（原文强制）**：单会话 Lv-3 频次上限（原文示例：**最多 1 次 / 15 分钟**）；
海马体的紧急标记是**申请**不是**指令**；调度器有**一票否决权**。

**SSE 的现实约束（原文 §三，gopi 同样适用）**：SSE 是单向流式输出协议，协议层无反向中断信号；
服务端不能在流运行中途"打断自己正在生成的内容"。已发出的 token 无法撤回。
⇒ **Lv-2/Lv-3 只在"多步、带循环状态机的长任务流"里才有现实意义**（两个 step 之间有轮询间隙），
对短的一次性问答几乎不存在优雅中断的方案。gopi 的 `runEngine` 是前者（`:2685` 的轮循环 +
`:2729` 的轮边界注入先例），所以 gopi **具备**实现 Lv-2 的形态基础。

**落地时序（原文 §五，与 epic 的 4 个子任务对齐）**：
1. **第一阶段（近期）**：只实现 Lv-0 + Lv-1。不动 SSE 内核、不动调度器，改动最小。
2. **第二阶段（架构预留）**：引入**会话事件总线**，所有内部事件（海马体情报、系统告警、状态变更）
   统一投递；主循环每完成一个子步骤轮询 → 可实现 Lv-2（仍不终止流）。
3. **第三阶段（远期）**：Lv-3 完整链路 + 限流 + 配额 + 多重校验，经大量测试后选择性启用。

---

## 7. 验收视角：本体论如何被"可检验"地使用

本文不是装饰性文档。三个子任务应当用它来回答：

1. **边界问题**（实现时最容易越界）：这个功能属于哪个器官？——例如"检索后发现旧方案不可复用"
   是**前额叶**的判断，海马体**禁止**做（§2 表 1 号 + §2.2）。
2. **接口问题**：跨器官调用只能走**已存在的导出面**（§4 的锚点表），新增导出面必须在此登记。
3. **预算问题**：每个器官独立预算，检索的算力不与决策的算力绑定（原文：把海马体剥离出来最大的
   收益之一就是"记忆检索的算力可以独立调度"）。
4. **验证问题**：记忆器官的效果必须能用**真实场景 benchmark** 复现原文的失败案例（"遗忘已有
   开发流程、重复实现"）——这是子任务 4（真实会话采纳验证）的验收锚。

跨器官接口与配置旋钮的**具体形态**见 `devdoc/hippocampus.md`。
