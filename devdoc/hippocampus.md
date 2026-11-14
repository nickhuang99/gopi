<!-- doc-class: normative -->
# 海马体 (Hippocampus) 在 GoPi 的落地映射 — SPEC

> 状态: **AUTHORED SPEC** (2026-09-10)。本文是 hippocampus epic 的**实现接口契约**，
> 与 `devdoc/agent-five-organs-ontology.md`（本体论与权责）配套使用。
>
> 锚点基准: 分支 `feat/hippocampus-spec`，commit `2516e09`。所有 `文件:行号` 均在该
> commit 上**逐条核对过行内容**。行号会随后续改动漂移 —— 校验锚点的正确方式是**看符号名**
> （`grep -n "func MemorySearch" internal/tools/memory.go`），行号只是定位加速器。
>
> 术语: **海马体** = 检索+研判器官（`internal/tools/memory*.go` 的记忆面 + 新的检索循环）；
> **前额叶/调度器** = `Agent.runEngine` 的轮循环（`internal/agent/loop.go:3210` 定义 / `:3547` 轮循环体）。

---

## 0. 一句话目标与不可谈判的验收口径

把今天"**被动**的一发式提示"（harness 强制模型在首轮调一次 `memory_search`）升级为
"**主动**的检索子 Agent"（自己解析意图 → 自己决定检索几轮 → 自己筛 → 产出记忆简报）。

**不可谈判的口径（三个子任务共同遵守）**：

| # | 口径 | 为什么 |
|---|---|---|
| 1 | **默认关闭**：`GOPI_HIPPOCAMPUS` 未设置时，现有通路行为**逐字节不变**（golden 会话日志比对） | 回归门；新器官不许悄悄改变今天的确定性行为 |
| 2 | **不新增 LLM 工具**（Rule 3）：海马体是**进程内 Go 代码**，直接调 `tools.MemorySearch` 等导出函数 | `internal/tools/memory.go` 已是 Go 函数；工具往返是纯浪费 |
| 3 | **海马体不做情境判断**：只产出 `confidence` + `memory_weight` + 简报文本；**递送等级由调度器算** | 本体论 §2.2 的权责红线（the operator 原文：打断权不下放） |
| 4 | **不吞错**（Rule 15）：任何 recall/LLM/DB 失败都要么记录要么上抛；失败路径必须"降级为 Lv-0 或什么都不做"，不许静默卡住 | 静默失败会让"没搜到"和"没搜"不可区分 |

---

## 1. 复用点（REUSE）— 已经存在，直接调，不要重写

### 1.1 记忆面：`internal/tools/memory.go`（全部已是导出 Go 函数）

| 能力 | 符号 | 锚点 |
|---|---|---|
| 检索（FTS5 MATCH，返回**渲染后文本**） | `MemorySearch(query string, limit int, intent string) (string, error)` | `internal/tools/memory.go:1499` |
| 检索的默认条数 | `memoryDefaultLimit = 5` | `:55` |
| 单条读取 / 更新 / 删除 | `MemoryGet` / `MemoryUpdate` / `MemoryDelete` | `:2728` / `:2780` / `:2886` |
| 更新字段结构 | `type MemoryUpdateFields` | `:2766` |
| 固化（写入一条 QA + 面包屑） | `MemoryStore(topic, question, answer, source string, keywords []string) (string, error)` | `:2344` |
| 反馈投票（useful/useless） | `MemoryFeedback(id int, rating, reason, taskID string) (string, error)` | `:3022` |
| 常驻热层摘要（任务开始注入） | `MemoryHotResident() string` | `:3404` |
| 最近 N 条 | `MemoryListRecent(n int) (string, error)` | `:3473` |
| **命中结构体（结构化！）** | `type MemoryHit`（`Tier`/`Confidence`/`LessonType`/`WhenAt`/`PossiblyStale`/`IntentHits`/`Tokens`/`FbUseful`/`FbUseless`） | `:467` |
| 渲染器（`[]MemoryHit` JSON → 文本） | `renderMemorySearchResult(out string) (string, error)` | `:3344` |
| 行扫描器（rows → `MemoryHit`） | `scanSearchHit(rows, cols, now, intent)` | `:1786`，转调 `scanHit` `:1790` |
| SQL 选择器（按 schema 探列拼接） | `searchSelect` / `searchExtraSelect` | `:1588` / `:1599` |
| 检索计数回写（best-effort） | `bumpRetrievalCounts(ctx, path, ids)` | `:1559` |
| DB 路径解析 | `memoryDBPath()` | `:627` |
| 打开检索连接 | `openSearchDB(ctx, path)` | `:1249` |
| 热层注入配额（可作 Lv-0/Lv-1 条数上限的先例） | `hotResidentInjectLimit = 3` / `hotResidentMaxChars = 600` | `:478` / `:479` |
| 热层读超时 | `hotResidentTimeout` | `:3463` |

**结构性事实**：`MemorySearch` 在 `:2158` 内部构造 `hits := make([]MemoryHit, …)`，在
`:2201` `json.Marshal(hits)` 后交给 `renderMemorySearchResult`。**海马体要的是检索后的
结构化命中**，而今天唯一的出口是"渲染后的字符串"。→ **子任务 2 应当新增一个导出入口**
（例如 `MemorySearchHits(query string, limit int, intent string) ([]MemoryHit, error)`），
把 `:2158`–`:2201` 之间的逻辑抽出来共享，**不要靠解析渲染文本反推结构**（脆弱且违反 Rule 1b）。

### 1.2 分级 LLM 子循环的**形状先例**：`internal/tools/memory_brew*.go`

这是本 epic 最重要的一块"照着抄"的代码 —— 一个**进程内的、分级的、带 JSON 决策的 LLM 子循环**，
已经在生产里跑（session 退出时蒸馏记忆）。

| 形态 | 符号 | 锚点 |
|---|---|---|
| **最小 LLM 面（接口）** | `type brewClient interface { Chat(ctx, system, user string) (string, error) }` | `internal/tools/memory_brew_llm.go:112` |
| Flash 实现（包 `*deepseek.Client`） | `type brewFlashClient` | `:117` |
| 单次调用：thinking 关闭 + endpoint 自适应 | `func (f *brewFlashClient) Chat(...)` | `:129` |
| 真实构造函数（CLI 用） | `newBrewFlashClient()` / `NewBrewEngine()` | `:199` / `:212` |
| **一次调用 → 一个 JSON 对象 → struct** | `callBrewJSON(ctx, bc, system, user, out)` | `:233` |
| 客户端构造来源 | `deepseek.NewClaudeClient()`（定义 `internal/deepseek/claude_client.go:679`） | — |
| Flash 层模型常量 | `BrewModel = "deepseek-v4-flash"` | `internal/tools/memory_brew.go:33` |
| thinking 模式（**必须 disabled**，GLM 例外走 effort=low） | `brewThinkingMode = thinkmode.Disabled` | `:48` |
| 单次 LLM 超时 | `brewClientTimeout = 180 * time.Second` | `:107` |
| effort 模式 token 预算 | `brewMaxTokensEffortMode = 8192` | `:123` |
| prompt 配额常量（切块） | `brewMaxChunkChars = 12000` / `brewChunkPromptReserve = 2500` | `:53` / `:61` |
| 跑批入口 + 选项 | `RunBrew(ctx, bc, RunBrewOptions)` / `type RunBrewOptions` | `internal/tools/memory_brew_run.go:99` / `:80` |
| 统计结构 | `type BrewStats` | `internal/tools/memory_brew.go:127` |
| 会话事件渲染（JSONL → 文本） | `brewRenderEvent(ev *sessionEvent)` | `:197` |
| 写入来源标签（反自噬） | `BrewSourceTag = "brew"` | `:37` |
| 会话退出触发（唯一 knob 定义点） | `BrewIntervalEnv = "GOPI_BREW_INTERVAL_SEC"` / `BrewDefaultInterval = 24h` | `:90` / `:91` |
| CLI 状态文件 / 用法 | `brewStateFile = "brew_state.json"` / `brewUsage` | `cmd/agent/cli_brew.go:35` / `:22` |

**要抄的四件事**（子任务 2 的验收核心）：① `brewClient` 这样的**注入式最小接口**（测试塞 stub，
不打真 API）；② `callBrewJSON` 的"取回复里第一个 JSON 对象再反序列化"（模型爱在 JSON 外包散文）；
③ Flash **必须** thinking-disabled（GLM always-thinking 会 400，需 effort 回退，见 `:126` 注释里的
memory #185/#915/#920 教训）；④ 每调用一个 timeout 常量，**不散落魔法数**（Rule 1）。

### 1.3 宿主循环：`internal/agent/loop.go`

| 宿主点 | 符号 | 锚点 |
|---|---|---|
| Agent 结构体 | `type Agent struct` | `:1393` |
| 入口（Run → runEngine） | `Run` / `Resume` / `Chat` / `ChatOps` | `internal/agent/loop.go:3090` / `internal/agent/loop.go:3101` / `internal/agent/loop.go:4100` / `internal/agent/chat_ops.go:207` |
| **轮循环体**（多步 ReAct 的状态机） | `func (a *Agent) runEngine(...)` 内的 `for i := 0; ...` | `internal/agent/loop.go:3223` / `internal/agent/loop.go:3609` |
| 轮边界 ctx 检查（墙钟/超时） | `case <-ctx.Done():` at the round boundary | `internal/agent/loop.go:3008` |
| **轮边界注入的既有先例（必须照抄的位置）** | `for _, n := range tools.TakeJobNotices() { a.Ctx.AddUserMessage(...) }` | `internal/agent/loop.go:3043`（chat 通路同款 `internal/agent/chat_ops.go:220`） |
| 任务开始一发式搜索（**本 epic 要取代的被动通路**） | `maybeInjectTaskStartSearch()` | 调用点 `internal/agent/hippocampus.go:876`，定义 `internal/agent/loop.go:4460` |
| 任务结束固化检查点（对称强制） | `maybeEnforceCompletionStore()` | 定义 `internal/agent/loop.go:3819`，调用点 `internal/agent/loop.go:3405` / `internal/agent/loop.go:3409` |
| 热层注入前缀 | `hotResidentMessagePrefix` | `internal/agent/loop.go:5072`（拼接于 `internal/agent/loop.go:5049`） |
| 强制开关（A/B 隔离） | `Agent.NoEnforceMemory` / `GOPI_NO_ENFORCE_MEMORY` | `:2159` / `:3278` |
| 同款 gate（子任务应复用的形态） | `boolEnvTrue(name)` | `internal/agent/loop.go:2143` |
| env 解析（int/float + 校验 + 默认） | `envInt` / `envFloat` | `internal/agent/envparse.go:20` / `:45` |
| 上下文与注入 API | `type Context` / `AddUserMessage(content)` | `internal/agent/context.go:171` / `:171` |
| 会话时钟（tick） | `NewSessionClock` / `Elapsed` / `TickLabel` | `internal/agent/clock.go:20` / `:27` / `:37` |
| 工具分派中的记忆段（tool 名 → 函数） | `case toolnames.ToolMemoryActionSearch:` → `tools.MemorySearch(...)` | `internal/agent/loop.go:5369` → `:5378`；store 分支 `:5434` |
| 记忆工具定义（描述文本） | `Name: toolnames.ToolMemory` | `internal/agent/loop.go:1141` |
| 工具名常量（**禁止裸字面量**） | `ToolMemory` / `ToolMemoryActionSearch|Store|Update|Feedback` | `internal/toolnames/names.go:85` / `:42`–`:45` |

**轮边界是本 epic 的物理关键点**：`Lv-1`（后置追加）与 `Lv-2`（元事件插队）在 gopi 里
**落到同一个位置** —— `:3609` 那一小段（请求构造之前）。见 §4。

### 1.4 中断面（**必须区分清楚，别复用错**）

| 能力 | 符号 | 锚点 | 语义 |
|---|---|---|---|
| 轮边界 / 流内取消的 cancel 根 | `interruptBase` / `interruptCancel` | `:2315` / `:2316`（构造 `:3134`，装配 `:3170`） | 用户级**硬**停止的根 |
| 硬停止（信号处理器调用） | `Agent.Interrupt()` | `:3045` | 幂等，`ctx` 取消 + 闩锁 |
| 判定是否由信号打断 | `interruptedBySignal()` | `:3062` | 仅 ctx Done 后有义 |
| cancel 根访问器 | `interruptRoot()` | `:3072` | 运行期 ctx 都从它派生 |
| 信号闩锁字段 | `interrupted bool` | `:2026` | — |

**裁决（写进子任务 3 的验收）**：`Lv-3` **禁止**调用 `Agent.Interrupt()`。理由有两条硬事实：
① 它是**无条件最高优先级**（用户中断），而海马体的紧急标记只是**申请**；
② 它 cancel 运行 ctx ⇒ `runEngine` 走 `:2876` 展开收尾，**本轮结果被丢弃**（这正是
"粗暴硬切断，相当于直接断电"，the operator 原文明确否掉了对海马体用这一手）。
Lv-3 的正确实现形态是：**轮边界的调度器裁决** —— 不取消 ctx，而是把简报作为最高优先级
user 消息注入并继续下一轮（"优雅终止当前任务流 + 以简报为首要上下文开新回合"的可行近似），
且带配额（§5）。

### 1.5 节律与通知面（Lv-0 归档 / 跨会话投递）

| 能力 | 符号 | 锚点 |
|---|---|---|
| 会话事件写入（唯一入口，自动补 ts，缓冲+阈值刷盘） | `(*SessionLogger).log(event map[string]interface{})` | `internal/agent/session.go:238` |
| 用户消息事件（审计用） | `LogUser(promptID, content)` | `:282` |
| 其它既有事件类型 | `LogAssistantText` / `LogAssistantThinking` / `LogAssistantToolUse` / `LogToolResult` / `LogRequest` / `LogSessionEnd` | `internal/agent/session.go:488` / `:384` / `:397` / `:412` / `:428` / `:443`（按符号重定位：该表原本的裸续行已漂到空白行） |
| 刷盘 | `Sync()` | `:262`（轮边界调用） |
| 会话目录名常量（**引用，勿裸写**） | `config.SessionsDirName` | `internal/config/gopihome.go:29` |
| 会话 JSONL 消费方 1（耗时轨迹） | `trajectory.go` 的 `switch ev.Type` | `internal/agent/trajectory.go:468` |
| 会话 JSONL 消费方 2（brew 蒸馏，只认 `role` = user/assistant） | `brewRenderEvent` | `internal/tools/memory_brew.go:197` |
| 合成事件外送（唯一出口） | `NotifySyntheticEvent(event, sessionID, model, summary)` | `internal/tools/taskheartbeat.go:40` |
| 既有事件名先例 | `TaskHeartbeatNotifyEvent = "stale"` `internal/tools/taskheartbeat.go:22`；`notifyEventLoopStuck = "loop-stuck"` `internal/agent/loop.go:268`；`notifyEventLoopStuckToolful` `internal/agent/loop.go:303`（调用点 `internal/agent/loop.go:2068` / `internal/agent/loop.go:2259`） | — |
| 事件过滤器（默认值常量） | `taskNotifyEventsDefault = "claimed,in_progress,completed,failed"` | `internal/tools/tasknotify.go:63` |
| 通知配置加载 / 载荷 / 转移触发 | `loadTaskNotifyConfig` / `taskNotifyPayload` / `notifyTaskTransition` | `internal/tools/tasknotify.go:127` / `:180` / `:280` |
| **真实在跑的心跳**（不是 `agent/heartbeat.go`） | `taskHeartbeatThrottleMin=30s` `internal/agent/taskheartbeat.go:34`、`taskHeartbeatStaleThreshold=30m` `:38`、`var taskHeartbeatState` `:56` | env 名单一来源 `config.TaskStallSecEnv`（`internal/config/config.go:930`）；旧拼写 `internal/config/config.go:964` |
| 心跳 DB 句柄 / 打开 | `type TaskHeartbeatConn` `:26` / `OpenTaskHeartbeatConn` `:66` | `internal/tools/taskheartbeat.go` |
| 停滞检测（查询侧） | `DetectStaleTasks(thresholdSec)` | `:142` |
| 停滞看护（agent 侧） | `maybeNudgeStaleTaskList` | `internal/agent/loop.go:5189` |
| 旧心跳状态机 | **已于 2026-09-29 整文件删除** | 2026-09-24 复查确认它零生产调用者（`NewAgent` 只写字段、从不读）；2026-09-29 复核确认连被写的字段也无任何读者，遂连同它的两个 env 旋钮一并移除 — `devdoc/heartbeat-knobs-removed.md`（本行原写"已死/零读者"为假、后经两次测量纠正，特此记录） |

> **警告（必须在实现前知道）**：那套 30s/3 轮的状态机（**该文件已于 2026-09-29 删除**）自
> `f81bf48`（*revert to simple ReAct loop — remove task-list enforcement*）起**没有任何生产调用者**。
> 任何人（包括 `devdoc/hippocampus-epic-reframe.md` 的早期判断）把它当作 "Lv-0/Lv-1 的天然宿主"
> 都会把新功能接到死代码上。Lv-0/Lv-1 的真实宿主是 **`:2729` 轮边界注入点** + **`SessionLogger.log`**。

### 1.6 配置与模型面

| 能力 | 符号 | 锚点 |
|---|---|---|
| `memory_db` 设置项 | `Config.MemoryDB` | `internal/config/config.go:252` |
| `no_enforce_memory` 设置项 | `Config.NoEnforceMemory` | `internal/config/config.go:239` |
| env 绑定 | `setStr("GOPI_MEMORY_DB", …)` / `setBool("GOPI_NO_ENFORCE_MEMORY", …)` | `internal/config/env.go:195` / `:196` |
| 模型解析（env > settings > 默认） | `deepseek.ResolveModel(cfg)` | `internal/deepseek/config.go:84` |
| 客户端构造 | `NewClaudeClient()` | `internal/deepseek/claude_client.go:679` |
| 请求构造（含 thinking/effort） | `BuildClaudeRequest(...)` | `:467` |
| 非流式 Chat（brew 用的那个） | `(*Client).Chat` 经 `brewFlashClient.Chat` | `internal/tools/memory_brew_llm.go:113` |

---

## 2. 新代码（NEW）— 没有的东西，必须写

| # | 新东西 | 建议落点 | 说明 |
|---|---|---|---|
| N1 | ** hippocampus 包**（检索循环 + 简报类型） | `internal/hippocampus/`（新包，只依赖 `internal/tools` + `internal/deepseek`） | 叶子包，**不被 `internal/tools` 依赖**，避免环 |
| N2 | 结构化检索出口 | `internal/tools/memory.go` 新增导出 `MemorySearchHits(query, limit, intent) ([]MemoryHit, error)`，`MemorySearch` 改为它的渲染包装 | 见 §1.1 的结构性事实 |
| N3 | Flash 发散检索 / Pro 决策的两级调用 | `internal/hippocampus/engine.go` | 抄 `brewClient` 形状（§1.2） |
| N4 | 简报（Brief）类型 + JSON 决策解析 | `internal/hippocampus/brief.go` | 抄 `callBrewJSON`（`internal/tools/memory_brew_llm.go:233`） |
| N5 | **递送等级裁决器**（调度器侧） | `internal/agent/hippocampus_host.go` | Lv-0..Lv-3 在 §4 的落点；带配额 |
| N6 | 预算控制器（频次配额 + 队列/丢弃） | `internal/hippocampus/budget.go` | 原文：超限强制回落 Flash；会话转题可终止在途检索 |
| N7 | 会话日志新事件类型（Lv-0 归档） | `internal/agent/session.go` 加 `LogHippocampusBrief(...)`（走 `log()` `:855`） | 新 `type` 值必须是**常量**，且写明两个消费方（trajectory/brew）忽略它 |
| N8 | 开关与旋钮 | §5 | 单一 env 读取点（Rule 2） |
| N9 | 默认关闭的 golden 回归测试 | `internal/agent/hippocampus_off_test.go` | 口径 1 的机器证明 |

**明确不写**：新工具（Rule 3）、新 prompt section（简报只能作为 user/system 消息进入；
见 §3）、对 `agent/heartbeat.go` 的任何"复活"（§1.5 警告）。

---

## 3. 简报进入上下文的**唯一合法方式**

系统提示词由有序的 `promptSections` 渲染（`internal/agent/loop.go:1073` 变量、`:823` `assembleSystemPrompt`），
它是**框架级、非用户可配置**的静态列表。因此：

- **简报只能作为 user（或 system 角色的独立消息）进入 `Ctx`**，通过 `Context.AddUserMessage`
  （`AddUserMessage`，`context.go:171`）；
- **不许**给 `promptSections` 加动态 section（那会把每轮变化的文本塞进静态前缀，破坏
  缓存命中与"密度 <4.5K"的设计约束）；
- **必须**同时写会话日志（否则审计时"线上有、日志无"不可区分，`maybeInjectTaskStartSearch`
  在 `:3449`–`:3455` 已经立了这个先例，照抄）。

---

## 4. 四级通路在 gopi 的落点（Lv-0 … Lv-3）

原文语义见本体论文档 §6。本表是**gopi 的物理落点**，子任务 3 按此实现。

| 级别 | the operator 原文语义 | **gopi 宿主（锚点）** | 实现要点 | 风险/约束 |
|---|---|---|---|---|
| **Lv-0** 日志归档 | 只写会话事件日志，**不注入上下文** | `(*SessionLogger).log()` `internal/agent/session.go:238`（已有 `LogHippocampusBrief` `internal/agent/session.go:855`） | 新 `type` 常量（如 `"hippocampus_brief"`）写进 JSONL |
| **Lv-1** 后置追加提示 | 不打断当前流；**本轮结束后**追加一条独立提示 | **`internal/agent/loop.go:3043` 轮边界注入点**（`TakeJobNotices` 循环旁边），消息体带 `【海马体备忘】` 前缀 | 注入后**写会话日志**（`LogUser`，`internal/agent/session.go:283`） |
| **Lv-2** 上下文内元事件 | 不终止执行流，插一条**高优先级元事件**；多步循环在**子步骤间隙轮询**读到 | **同一个 `internal/agent/loop.go:3043` 注入点** + 一个 run 级事件队列（抄 `TakeJobNotices` 的"drain 一次/轮"形态，`internal/tools/job.go:297`） | 与 Lv-1 的差别**只是框定 + 配额**：Lv-1 = 轮末备忘（被动参考），Lv-2 = 要求**重新评估后续步骤**的元事件；两者共用一条物理通道 |
| **Lv-3** 系统软中断 | 向调度器发**申请**；校验置信度 + 配额通过后**优雅终止**任务流，以简报为首要上下文**开新回合** | 调度器侧 = `runEngine` 轮边界（`internal/agent/loop.go:3008` 附近做裁决，`internal/agent/loop.go:3043` 注入简报） | 唯一现存通道是 `Interrupt()`（`internal/agent/loop.go:2640`）——**Lv-3 禁止调用它**（见 §1.4） |

### 4.1 SSE 单向性的现实约束（原文 §三，gopi 同样适用）

SSE 是单向协议，**已发出的 token 撤不回**，服务端不能"打断自己正在生成的内容"。
⇒ **Lv-2/Lv-3 只在"多步 + 带循环状态机的长任务流"里有现实意义**（两个 step 之间有轮询间隙）。
gopi 的 `runEngine` 正是这种（`:3125` 定义 + `:3609` 既有轮边界注入先例）⇒ **形态具备**；
而 `Chat`（`:3260`）/`ChatOps`（`chat_ops.go:159`）的短问答路径**不具备** ⇒ 在那些路径上
`MaxLv` 应被夹到 **1**（把"不具备"变成显式规则，而不是运行期惊喜）。

### 4.2 与今天被动通路的关系（替换契约）

| 场景 | 今天（`GOPI_HIPPOCAMPUS` 关） | 开启后 |
|---|---|---|
| 任务开始 | `maybeInjectTaskStartSearch`（调用点 `internal/agent/hippocampus.go:876`，定义 `internal/agent/loop.go:4460`）注入 `taskStartSearchMessage` + 热层摘要；`NoEnforceMemory` 可关 | **由海马体的 recall 取代**：harness 不再注入那条强制搜索消息（**禁止双注入**）；`MemoryHotResident`（`:4471`）可作为 Lv-0 的输入之一（它是**提示**不是搜索替代，`internal/agent/loop.go:4468` 注释已明确） |
| 任务结束 | `maybeEnforceCompletionStore`（`:4427`，调用点 `:3994`/`:4013`） | 保留；海马体的固化侧复用 `MemoryStore`（`:2094`）+ `BrewSourceTag`（`:35`）的反自噬思路（用自己的 source tag，例如 `"hippocampus"`） |
| 关闭时 | — | **逐字节不变**（口径 1，N9 的 golden 测试） |

---

## 5. 配置旋钮（KNOWNS）— 单一读取点，常量先行（Rule 1/2）

**新增**（全部默认关闭/保守，定义在新包顶部常量区，env 只在**一个**函数里读）：

| env | 常量（建议） | 默认 | 语义 |
|---|---|---|---|
| `GOPI_HIPPOCAMPUS` | `hippocampusEnv` | **off** | 总开关；off ⇒ 逐字节不变（口径 1） |
| `GOPI_HIPPOCAMPUS_MAX_LV` | `hippocampusMaxLvDefault` = `1` | `1` | 递送等级上限（原文第一阶段只做 Lv-0/Lv-1）；无工具循环路径强制夹到 1（§4.1） |
| `GOPI_HIPPOCAMPUS_MAX_RECALL_PER_RUN` | `hippocampusMaxRecallPerRun` = `8` | `8` | 单 run 检索次数上限（预算控制器） |
| `GOPI_HIPPOCAMPUS_TIMEOUT_SEC` | `hippocampusRecallTimeout` = `30s` | `30s` | 单次 recall（含 Flash 调用）超时 |
| `GOPI_HIPPOCAMPUS_MODEL` | 复用 `tools.BrewModel`（`memory_brew.go:33`）作默认 | flash 层 | 发散检索用的模型 |
| `GOPI_HIPPOCAMPUS_DECIDE_MODEL` | — | 空 = 用 Agent 模型 | 决策/研判层（Pro）模型 |
| `GOPI_HIPPOCAMPUS_QUEUE` | `hippocampusQueueDepth` = `4` | `4` | 异步检索队列深度；溢出策略 = **丢弃最旧的低优先级**（原文：会话转题即终止在途检索） |
| `GOPI_HIPPOCAMPUS_LV3_MIN_INTERVAL_SEC` | `hippocampalLv3QuotaWindow` = `15 * time.Minute` | `900` | Lv-3 频次下限（原文示例 1 次/15 分钟） |
| `GOPI_HIPPOCAMPUS_LV3_MAX_PER_SESSION` | `hippocampalLv3MaxPerSession` = `1` | `1` | 单会话 Lv-3 上限 |

**复用**（不要另起一套）：

| env / 设置 | 位置 |
|---|---|
| `GOPI_MEMORY_DB` / `memory_db` | `internal/config/config.go:956` / `internal/config/config.go:252` |
| `GOPI_NO_ENFORCE_MEMORY` / `no_enforce_memory` | `internal/config/env.go:205` / `internal/config/config.go:239` |
| `GOPI_BREW_INTERVAL_SEC` | `internal/tools/memory_brew.go:90` |
| `GOPI_TASK_NOTIFY_EVENTS`（默认常量 `tasknotify.go:63`） | `internal/tools/tasknotify.go` |
| `GOPI_TASK_STALE_SEC`（唯一读取点 `config.ResolveTaskGracePeriods`，Rule 2） | `internal/agent/taskheartbeat.go:159` |
| `GOPI_HOME` / `config.SessionsDirName` | `internal/config/gopihome.go:29` |

---

## 6. **接口草图（AGREED INTERFACE）— 子任务按此实现**

原则：① 海马体的输出类型**携带原始指标**（`Confidence`/`MemoryWeight`），**不含等级**；
② 等级由 `internal/agent` 侧的调度器算；③ 所有跨包面是**接口**，便于测试注入 stub。

```go
// ===== package hippocampus (new, internal/hippocampus) =====

// RecallRequest is one retrieval ask. The hippocampus never asks "what is the
// current task state" — that is the scheduler's information (ontology §2.2).
type RecallRequest struct {
    Query     string // 意图解析后的检索式
    Intent    string // 路由提示（进阶 tools.MemorySearch 的 intent 参数）
    SessionID string
    TaskID    string
    Round     int
}

// Brief is the ONLY thing the hippocampus may emit: raw objective signals +
// a briefing text. NO answer-like text (职责漂移防线), NO delivery level.
type Brief struct {
    Confidence   float64           // 关联置信度（海马体自评）
    MemoryWeight float64           // 记忆固有权重
    Hits         []tools.MemoryHit // 结构化命中（N2 的新出口）
    Summary      string            // 简报正文（陈述"检索到什么"，不回答任务）
    Query        string
    DurationMS   int
}

// Client mirrors brewClient (internal/tools/memory_brew_llm.go:112) — inject a
// stub in tests; the real one is deepseek.NewClaudeClient() (claude_client.go:670).
type Client interface {
    Chat(ctx context.Context, system, user string) (string, error)
}

// Engine = 检索 + 研判（Flash 发散 / Pro 决策）。
type Engine interface {
    Recall(ctx context.Context, req RecallRequest) (*Brief, error)
}

// Budget = 算力预算控制器（原文强制）。Allow* 只做判定，Observe 记账。
type Budget interface {
    AllowRecall(now time.Time) bool
    AllowDelivery(level Level, now time.Time) bool
    Observe(level Level, now time.Time)
}

// Level is the delivery channel, computed by the SCHEDULER, never by the
// hippocampus (ontology §2.2). Values mirror devdoc/agent-five-organs-ontology.md §6.
type Level int

const (
    LevelArchive       Level = iota // Lv-0 session log only
    LevelAppend                     // Lv-1 post-round reminder
    LevelMetaEvent                  // Lv-2 in-context meta event
    LevelSoftInterrupt              // Lv-3 scheduler-adjudicated soft interrupt
)
```

```go
// ===== package agent (host side: internal/agent/hippocampus_host.go) =====

// HippocampusHost is the scheduler's contract toward the organ.
// Implementations must be NO-OPS when GOPI_HIPPOCAMPUS is off (口径 1).
type HippocampusHost interface {
    // StartOfRun replaces maybeInjectTaskStartSearch (loop.go:3486) when on.
    StartOfRun(ctx context.Context, a *Agent)
    // AtRoundBoundary drains pending briefs, computes the delivery level
    // (scheduler's call) and returns what to inject at loop.go:2729's site.
    AtRoundBoundary(ctx context.Context, a *Agent) []Injection
    // EndOfRun flushes/archives; called from runEngine's defer.
    EndOfRun(ctx context.Context, a *Agent)
}

// Injection is one wire-level message the loop will AddUserMessage + log.
type Injection struct {
    Level Level
    Text  string // already framed ([system] / 【海马体备忘】)
}
```

**接线清单（三个子任务的边界）**：

| 接线点 | 位置 | 谁做 |
|---|---|---|
| 开关判定 + 常量 + env 读取 | `internal/hippocampus/config.go`（新） | 子任务 2 |
| `MemorySearchHits` 结构化出口（N2） | `internal/tools/memory.go` | 子任务 2 |
| recall 循环（Flash 发散 → 筛 → Pro 研判） | `internal/hippocampus/engine.go` | 子任务 2 |
| Lv-0 归档事件（N7） | `internal/agent/session.go` | 子任务 3 |
| Lv-1/Lv-2 注入（`internal/agent/loop.go:3043` 旁）+ 日志 | `internal/agent/loop.go` | 子任务 3 |
| Lv-3 轮边界裁决 + 配额（**不碰 `Interrupt`**） | `internal/agent/loop.go` `:2857` 附近 | 子任务 3 |
| 任务开始替换 `maybeInjectTaskStartSearch`（调用点 `internal/agent/hippocampus.go:876`，定义 `internal/agent/loop.go:4460`） | `internal/agent/loop.go` | 子任务 3 |
| 默认关闭 golden 测试 + 真实会话采纳证据 | `*_test.go` + `~/workspace/.../devdoc/hippocampus-adoption.md` | 子任务 3 / 4（Rule 11：真实会话 `tool_use` 证据） |

---

## 7. 验收视角（机器可判定）

| 口径 | 怎么验（确定性检查） |
|---|---|
| 默认关闭 ⇒ 行为不变 | golden：同一输入跑两次（开/关），会话 JSONL 去掉时间戳后**逐字节相等**（N9） |
| 不新增工具（Rule 3） | `TestGOPIRule3_MatchesClaudeToolDefs` 保持绿；工具数仍为 16 |
| 无魔法数 / 无裸字面量（Rule 1/1b/13） | 新常量全部在文件顶部；`"hippocampus_brief"` 之类的 type 值只在**常量定义处**出现一次；新增字面量门测试 |
| Flash 必须 thinking-disabled | 单测用 stub `Client` 断言请求形态；GLM 400 → effort 回退路径有测试（抄 `memory_brew_llm.go:118` 的教训） |
| 不吞错（Rule 15） | recall/LLM/DB 三类失败各有测试，断言"降级为 Lv-0 或 no-op + 有记录" |
| Lv-3 配额与一票否决 | 单测：15 分钟内第二次申请被拒；置信度低于阈值被拒；**且断言 `interrupted` 仍为 false**（证明确实没用 `Interrupt`） |
| 真实采纳（Rule 11） | `echo "<task>" \| ./bin/gopi`，`~/.gopi/sessions/*.jsonl` 里有 recall/简报注入记录 —— 子任务 4 |

---

## 8. 未决问题（诚实清单）

1. **异步 vs 同步检索**：原文提到"每条用户消息一个后台协程"，但 gopi 的 loop 是同步的；
   后台协程只有 Lv-2/Lv-3 才需要（要有轮边界来收割）。**建议第一阶段只做同步**（Lv-0/Lv-1），
   队列/限流等到确实需要 Lv-2 时再上 —— 否则就是给一个没人用的通道建基础设施。
2. **`memory_weight` 的算法**：原文只给了名字。建议第一版 = 命中的 `Confidence` × tier 权重
   （hot/warm/cold）× `Tokens` 归一化；**先写文档再写代码**（避免这个字段变成第二个
   `possibly_stale` 式的"过度承诺"信号，见 `memory.go` 里 Fix 1 的历史教训）。
3. **跨会话投递**：本 run 结束后才产出的 Lv-1 简报，是否要落到"下一次任务开始"的注入点
   （`:4387`/`:3256`）？倾向**是**，但那会让 `maybeInjectTaskStartSearch` 的角色变成
   "海马体简报的第二个出口" —— 需要子任务 3 明确它和 N5 的优先级（同一位置不能双注入）。
4. **`internal/hippocampus` 的依赖方向**：它要 import `internal/tools`（拿 `MemoryHit`）与
   `internal/deepseek`（拿 `*Client`）。`internal/agent` 再 import 它 —— 单向，无环；
   但若 `internal/tools` 将来需要引用 hippocampus 的类型（例如新工具），环就出现了。
   **结论：`internal/tools` 永远不许 import `internal/hippocampus`**（写进 N1 的注释）。
