<!-- doc-class: normative -->
# 竞争分析：gopi vs codex — 为什么 codex 更强

> 首次一手源码对比（2026-08-29）。gopi 仓库 `~/work/GoPi` + codex 仓库 `~/work/codex`（codex-rs，Rust，核心 `core/src` ~33 万行）。
> 本报告聚焦 4 个维度：工具调用设计、工作流、系统提示词、其他代码逻辑/设计文档。所有结论均有源码依据 + memory 存证。

---

## 0. 摘要（TL;DR）

codex 在工具系统架构、多智能体能力、记忆自动沉淀、系统提示词设计四个层面系统性优于 gopi。根因不是"gopi 哪里写错了"，而是**两者定位不同**：

| | gopi | codex |
|---|---|---|
| 定位 | 单 agent 自驱效率引擎（闭环强制记账） | 开放协作平台（多 agent + 计划 + 审批 + 沙箱） |
| 工具模型 | 20 个扁平 `ClaudeToolDef` 全量塞 context | 多态 `ToolSpec` + namespace + `tool_search` 按需加载 |
| 扩展性 | 加工具 = 改 135KB loop.go switch + toolRouting prompt | `ToolHandler` 注册进 `ToolRegistry`，`defer_loading` 自动走工具搜索 |
| 记忆 | LLM 主动 `memory_store`（prompt 说服驱动） | 两阶段自动后台管线 + consolidation sub-agent + git baseline |
| 多 agent | ❌ 无委派（只有 job_start 后台 shell） | ✅ `spawn_agent`/`send_input`/`wait`/`close_agent` 完整委派生命周期 |
| 文件编辑 | 单文件 `edit`（一个 oldStr 匹配） | `apply_patch`（多文件统一 diff + 校验先行 + 原子执行） |
| 安全模型 | 危险命令黑名单 + `GOPI_ALLOW_DANGEROUS` | 审批策略(Never/OnRequest/Granular) + 沙箱三模式 + exec rules |
| 系统提示词 | 效率规则补丁集（9 个 `promptSections`） | 行为/沟通规范（personality + preamble + plan 示例 + answer 格式） |
| 输出截断 | 简单 cap | `TruncationPolicy`（中间截断 + 按内容项预算 + token 估算） |

**一句话**：codex 赢在**架构级差异**——把"工具规模""多智能体""记忆沉淀""安全自治"做成系统机制，gopi 把这些做成 prompt 提示词 + 硬编码 switch。codex 的工具是**插件式可扩展**的，gopi 的工具是**写死可枚举**的。

---

## 1. 工具调用设计对比（核心差距）

### 1.1 工具模型：扁平枚举 vs 多态 + 命名空间

**gopi**（`internal/agent/loop.go`）：
- 20 个工具定义在 `claudeToolDefs []deepseek.ClaudeToolDef`（平铺 slice），一次性通过 `BuildClaudeRequest(..., claudeToolDefs, ...)` 全量发给模型。
- 分派是硬编码 `executeToolDefault(tc)` 里一个巨大的 `switch tc.Name`（~450 行，case read/write/edit/bash/code_search/...）。
- 加一个新工具要动 3 处：`claudeToolDefs` + `executeToolDefault` switch + `toolRoutingRules` prompt。

**codex**（`codex-rs/tools/src/tool_spec.rs`）：
- 工具是**多态枚举** `ToolSpec`：`Function(ResponsesApiTool)` / `Namespace(ResponsesApiNamespace)` / `ToolSearch{execution,description,parameters}` / `WebSearch{...}` / `Freeform(FreeformTool)`。
- 一个 `Namespace` 把一组工具 + 一条 namespace 描述聚合在一起——模型按域看到一把工具 + 一段话，而非每工具一条描述（省 prompt token、按域路由）。
- 加一个新工具 = 实现一个 `ToolExecutor<ToolInvocation>` trait（`tool_name`/`spec`/`handle`），注册进 `ToolRegistry`（`register_trusted_*`），hook 层自动处理——**零侵入**。

**为什么 codex 赢**：工具数量可以到几百（MCP 工具、动态工具、插件工具），而 gopi 塞 20 个工具规格进 system prompt 已经是极限。codex 的 namespace 让你挂几十个 MCP server 而不撑爆 context。

**借鉴**：gopi 可把 `claudeToolDefs` + `executeToolDefault` switch 重构为"注册表 + 每工具 Handler"。不需要全量 Rust 那套，Go 的 map[ToolName]Handler 就行。至少按域给 namespace 聚合描述。

### 1.2 动态工具发现：`tool_search` / BM25（最大的可扩展性差距）

**gopi**：所有工具规格全量常驻 context，无按需加载。

**codex**（`codex-rs/core/src/tools/handlers/tool_search.rs`）：
- 被标记 `defer_loading`/`deferred`（`ToolExposure::Deferred`）的工具**不进模型初始 context**。
- 模型自己调用 `tool_search(query, limit)`——一个由 `bm25::SearchEngine`（Language::English）跑的工具查找工具，把工具 name/description/schema 拼成 `search_text` 建索引。
- 返回按 BM25 相关性排序的工具子集，`coalesce_loadable_tool_specs` 把同 namespace 的工具聚合成一个 namespace 一次性加载（`LoadableToolSpec::Namespace`）。
- 缓存 `ToolSearchHandlerCache` 用 Weak ptr 比较 source 复用 handler，registry 变化才重建。

**为什么 codex 赢**：工具发现是**模型主动的可检索能力**。一个 codex 实例可以注册几百个 MCP/动态工具，初始 context 只背核心小工具；模型需要某个领域工具时 `tool_search` 按需取回。这是 gopi 的"工具列表长度受 context 限制"与 codex 的"工具规模只受磁盘限制"的本质差别。

### 1.3 编辑能力：单文件 `edit` vs 多文件 `apply_patch`

**gopi**：`edit(path, oldStr, newStr)`——一次一个文件一个字符串匹配。改多个文件要多次调用。
**codex**（`apply_patch.rs`）：统一 diff 格式 `*** Update File: path`，**一个 patch 可改多个文件**；`StreamingPatchParser` 流式解析 → 对文件系统**验证**（不直接改）→ 审批/沙箱 → 原子执行。错误返回 `RespondToModel` 让模型修正 patch。行尾按 feature 保留。

**借鉴**：gopi 的 `edit` 已是"独特串替换"（surgical，单文件），可扩展为支持"一个调用内多个 edit/多文件"以减少轮次。

### 1.4 工具输出稳健性：性能截断

**gopi**：输出简单 cap，截断后可能丢尾/丢头。
**codex**（`output-truncation/src/lib.rs`）：`TruncationPolicy::Bytes|Tokens` + **中间截断**（保头保尾）+ `approx_token_count` 估算 + 多内容项（text/image/audio/encrypted）各自按预算扣费 + 明确 `Warning: truncated output (original token count: ...)`。音频用 `estimate_audio_token_count`。

---

## 2. 工作流对比

### 2.1 主循环：单 agent ReAct vs turn + 可委派多智能体

**gopi**（`internal/agent/loop.go runEngine`）：
- 标准 ReAct：`[EPIC GOAL]` 包装首轮 → 强制 `[TASK_LIST]` → 循环 {chatOnce → 执行工具 → 重复}。
- **闭环强制记账**：`maybeInjectTaskStartSearch`（强制先 memory_search）、`maybeEnforceCompletionStore`（结束时强制 memory_store）、end-turn 检测（`maxConsecutiveEndTurnRounds`）、thinking budget 强制（abort + 重发 + 思想禁用 latch）、job 完成 notice 注入为 user message。
- 哲学："GoPi does NOT dictate"——task list 只观测不强制，退出靠模型自己连续 end_turn。
- 单 agent：无委派，并行只有 `job_start`（后台 shell 进程）。

**codex**（`client.rs` + `session/turn.rs` + `multi_agents/`）：
- turn 循环 + `Session`/`SessionIo` 分离（`tokio::spawn(submission_loop)`），控制走 bounded channel。
- 传输层：websocket + HTTP fallback，OpenAI Responses API，sticky routing（`x-codex-turn-state` header）。
- **多智能体委派**（`tools/handlers/multi_agents/spawn.rs`）：`spawn_agent` 创建独立子 thread（自己的 thread_id/model/role/config），`agent_max_depth` 防无限递归委派（超了返回 "Agent depth limit reached. Solve the task yourself."），`fork_context` 支持复制父线程历史，`send_input`/`wait`/`close_agent`/`resume_agent` 完整生命周期，生命周期可见（collab tool call item 暴露给 UI）。
- 记忆后台管线在 root session 启动时异步跑。

**为什么 codex 赢**：codex 能"派子 agent 并行探索独立子任务/独立角色"，gopi 只能一个 agent 串行或起 shell job。对付大型复杂任务，子 agent 并行是量级级的吞吐差。**gopi 的最大缺失能力**。

### 2.2 上下文压缩：日记式 vs Memento 策略

**gopi**（`compact.go`）：压缩摘要（具体见 `devdoc/memory-compaction.md` 与 memory #1022 的分析：boundSummaryInputMax 等）。
**codex**（`compact.rs`）：`build_compacted_history` = [initial_context(中程)/最近 user 消息（从新到旧保留至 ~20K tokens）+ 末尾 summary]。**保留最近原文 + 摘要旧内容**的双轨比纯摘要抗信息丢失。压缩摘要由模型在对话自然产出（`SUMMARIZATION_PROMPT` 可被 config.compact_prompt 覆盖），并带完整 analytics（before/after tokens、trigger/reason、保留图片数）。
**codex**（`context_manager/history.rs`）：`Arc<Vec<...>>` + `Arc::make_mut` 写时复制（只读消费者零拷贝）；**粗字节估算即可触发压缩**（4 字节/token，图片 ~1844 tokens/图），不必 tokenizer 精确。

**借鉴**：gopi 的压缩是独立的 summarize 调用；codex 的"保留最近 20K + 摘要旧内容" + "粗 token 估算触发"更稳健且省 tokenizer 成本。

### 2.3 恢复/检查点（resume）

**gopi**：有 resume 能力（`internal/agent/resume.go` + `cli_resume.go`，见 memory #858/#858：raw-log byte-exact 恢复）。
**codex**：`resume_agent` 工具 + 记忆管线把"压缩摘要 → 记忆 → 恢复上下文"串起来，压缩的 handoff summary 就是给"下个 LLM"的恢复输入。codex 的恢复是**与记忆/压缩管线协同**的系统级能力，而 gopi 的 resume 是独立的 raw-log 复活。

---

## 3. 系统提示词对比（意识层差异）

### 3.1 gopi：效率规则补丁集

`assembleSystemPrompt()` 组装 9 个 `promptSections`：
- header ("You are GoPi. 20 tools: ...") / toolRouting / efficiency / taskManagement / bashEfficiency / job / engineering / conciseThinking / tickClock。
- 全是**针对竞赛暴露的缺陷不断添加的规则块**（BASH EFFICIENCY 是 gopi-vs-claude race 后加的、THINKING RULES 是 thinking-budget-fix 后加的）。**没有 personality、没有协作沟通层、没有 plan 质量示例、没有 answer 格式规范**。
- 密度目标 <4.5K 字符。

### 3.2 codex：行为/沟通/方法论完整规范

`models-manager/prompt.md`（"You are a coding agent running in the Codex CLI"）是一份**完整的行为规范**：
- **Personality**：concise/direct/friendly 人格定调。
- **Preamble messages**：每次工具调用前发 1-2 句前言（8-12 词、逻辑分组、给寒暄示例）——**协作沟通层**，gopi 完全没有。
- **Planning**：`update_plan` 工具 + 高质量/低质量 plan 对比示例（gopi 的 tasklist 只有格式约束没有质量示例）。
- **AGENTS.md spec**：整段讲 AGENTS.md 作用域/优先级（越深越优先）/system>AGENTS.md 覆盖。
- **Ambition vs precision**：新任务要 ambitious、存量代码库要 surgical。
- **Final answer 结构规范**：section headers（`**Title Case**`）、bullets（`- `）、monospace 规则、file path 引用格式。
- **验证哲学**：从 specific 到 broad、按 approval mode 决定是否 proactively 跑测试。

### 3.3 上下文组装：单块拼 vs 模块化 context fragment

**codex**（`context/mod.rs`）：把模型输入拆成**几十个可插拔 context fragment**注入 developer message——`PermissionsInstructions`、`TokenBudgetContext`、`ModelSwitchInstructions`、`MultiAgentModeInstructions`、`PersonalitySpecInstructions`、`PluginInstructions`、`RolloutBudgetContext`、`CurrentTimeReminder`、`EnvironmentContext` 等。加一种情境 = 加一个 fragment 模块，可独立开关。这是**模块化的 prompt 组装系统**（对应 codex-rs/prompts/templates/* 每个情境一个模板）。
**gopi**：单块 `assembleSystemPrompt()` 字符串拼接，无模块化注入、无情境开关。

**一句话**：gopi 的 system prompt 是"给效率机器打补丁"，codex 的是"定义一个有性格会沟通的工程师"。gopi 借鉴方向：加 personality 定调、加 preamble/沟通层、加 plan 质量示例、加 answer 格式规范。

---

## 4. 记忆系统对比（gopi 的竞争壁垒 vs codex 的自动管线）

### 4.1 gopi：LLM 主动调用（prompt 说服驱动）
- 记忆 = 跨任务竞争力，由 system prompt 的 toolRoutingRules 强引导 + `maybeInjectTaskStartSearch`/`maybeEnforceCompletionStore` **代码强制**保证每次任务先搜后存。
- 五个工具：memory_search / memory_store / memory_feedback / memory_update / (CLI memory_delete)。LLM 是唯一用户 = 唯一质量裁判。
- 已证实的机制：memory_feedback 投票降级错误记忆、memory_update 纠正、hop 检索、5W1H 面条包。

**gopi 的独有强项**：memory_feedback / memory_update 的**质量裁判闭环**是 codex 记忆系统没有的——codex 的 memory 没有用户级 feedback 投票来降级/纠正记忆条目（这点 gopi 反而是优势）。

### 4.2 codex：两阶段自动后台管线
- **Phase 1（per-thread rollout extraction）**：root session 启动异步触发，从 state DB 按 claim 规则选 eligible rollouts（年龄窗口/idle 足够久/未占用/有界），并行发给模型提取 `raw_memory` + `rollout_summary`，secrets 脱敏，存 DB stage-1。lease 防重复、失败带 retry backoff。
- **Phase 2（global consolidation）**：加全局锁，从 stage-1 选 top-N（按 usage_count 再 last_usage/generated_at），**spawn 一个 consolidation sub-agent**（no approvals/no network/local write，disables collab 防递归委派）根据 `phase2_workspace_diff.md`（git diff from baseline）更新 MEMORY.md/skills/，然后 reset git baseline。
- 记忆工作区 `~/.codex/memories/` 是 git-baseline 目录。

**为什么 codex 赢在自动化**：gopi 靠 LLM 每任务主动 store（一次一个记录，无整合）；codex 的管线自动把大量 rollouts 提炼成结构化记忆，再用 sub-agent 跨任务**整合去重**成全局记忆（raw_memories.md/MEMORY.md/skills）。**整合级 consolidation** 是 gopi 弱项。gopi 可借鉴：后台自动从 session 日志提炼记忆 + 周期 consolidation agent。

---

## 5. 安全/自治模型对比

**gopi**：bash 工具 = 危险命令黑名单 + `GOPI_ALLOW_DANGEROUS` 环境逃逸。无审批分层、无沙箱、无命令分类授权。
**codex**（`exec_policy.rs` + `prompts/templates/permissions/*`）：
- 审批策略 `AskForApproval::Never/OnRequest/UnlessTrusted/Granular`。
- 沙箱三模式 `read_only/workspace_write/danger_full_access`（对应模板文件）。
- exec policy rules（Python/Rust 或 TOML）：前缀规则 `PrefixRuleMatch` + 启发式 `HeuristicsRuleMatch`；命令如 git 允许、网络需审批。
- `child_uses_parent_exec_policy` 让子智能体共享父策略；`prompt_is_rejected_by_policy` 区分 rules vs sandbox approval。

**借鉴**：这让 codex 能在交互（on-request 每个危险命令给用户）和非交互（never 全自动）间切换，用 granular 规则让常用命令自动放行。gopi 的 bash 无法表达"git fetch 免审但 curl 外网需审"。

---

## 6. 其他值得学习的设计逻辑

1. **工具 hook 体系贯穿生命周期**（`registry.rs`）：pre-hook 可重写入参、post-hook 可拦截/替换模型可见结果。gopi 无通用 hook。
2. **agent 角色系统**（`agent/role.rs`）：default/explorer/worker 内置角色 = 有界 config override（能力单调递减，只能禁不能开）。
3. **确定顺序工具注册**（`IndexMap<ToolName, RegisteredTool>`，重复 error_or_panic）。
4. **ARC 写时复制历史**（只读消费者零拷贝）。
5. **输出截断中间保留**（前面第 1.4 节）。
6. **加密函数参数**：`encrypted_function_args` 脱敏，direct/plaintext source 分开打日志。
7. **工具/记忆输出统一 ToolOutput trait**（log_output / success_for_logging / to_response_item / code_mode_result）——可见性/telemetry 一致。

---

## 7. 结论与 gopi 借鉴优先级（Top 行动项）

按撬动力排序：

1. **【高】工具系统重构为注册表 + 多态 payload**：把 `claudeToolDefs` + `executeToolDefault` 巨大 switch 改成 `map[name]Handler` + 每工具一个 Go 文件。这是让 gopi 能从"写死的 20 工具"变"可扩展工具平台"的第一步。
2. **【高】`tool_search` 按需工具发现**：核心小工具全量常驻 + 领域/MCP 工具走 BM25 工具搜索按需加载。gopi 挂了 MCP/插件就能不撑爆 context。
3. **【高】多智能体委派**：`spawn_agent`/`send_input`/`wait`——至少先做一个并行子 agent（有独立 thread 和深度限制）。这是对付大型任务的吞吐量级提升。
4. **【中】记忆后台自动沉淀**：加一个后台管线从 session 日志/raw-log 自动提炼记忆 + 跨任务 consolidation（借鉴 codex 两阶段），保留 gopi 的 memory_feedback 质量裁判作为差异化优势。
5. **【中】apply_patch 多文件编辑 + 校验先行**：把 edit 升级为"一次调用多文件、验证后原子执行"。
6. **【中】审批/沙箱分层**：Never/OnRequest/Granular + read_only 沙箱，让 gopi 能在自主模式下安全运行危险度分级命令。
7. **【低】系统提示词补行为层**：personality 定调、preamble 沟通层、plan 质量示例、answer 格式规范——把"效率补丁集"升级为"有性格会沟通的工程师定义"。

---

## 8. 方法学备注（本报告如何得到）

- 全部结论为源码第一手阅读，非二手描述。
- 已 memory_store 的蒸馏记录：`codex tool_spec 多态工具模型`(1040)、`tool_search BM25`(1041)、`codex system prompt 行为规范`(1042)、`SKILL.md 技能系统`(1043)、`两阶段记忆管线`(1044)、`spawn_agent 多智能体`(1045)、`exec policy 审批沙箱`(1046)、`gopi vs codex 工作流`(1047)。
- 交叉验证：codex 仓库自带的 `COMPACT_REPORT.md`（同样是从 gopi 视角读 codex 的旧报告，蒸馏出来的 session/turn、compact Memento、apply_patch、agent role 结论与本报告一致）。
- 诚实清单：codex 的记忆系统没有 gopi 的 memory_feedback/memory_update 质量裁判（gopi 此点反而是优势）；gopi 的 resume（raw-log byte-exact）、闭环强制记账、thinking budget 强制是 codex 不具备的独有机制，值得保留。
