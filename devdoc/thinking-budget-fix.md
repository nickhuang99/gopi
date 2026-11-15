<!-- doc-class: normative -->
# Thinking Budget Fix: 让 thinking 预算真正生效 (2026-08-21)

## 问题

竞争 #3 side-by-side 分析（report-side-by-side-w2w4.md）发现 gopi 比 claude 慢
5.5x 的根因：thinking 失控。gopi 每轮模型等待均值 44.6s（max 262.8s），82% 时间
在等自己的 CoT；单轮 thinking 最高 23,876 字符（≈5,970 tokens），3 轮巨量 thinking
= 592s = 总耗时 46%。

源码诊断：`DefaultClaudeThinkingBudget = 2048`，请求发 `budget_tokens=2048`，但
max_tokens=384,000 无输出上限兜底 —— 若 API 不执行 budget，thinking 理论上可
写到 384K tokens。

## 实证复现（2026-08-21，真实 API）

同一提示词（遥测文件格式设计，刻意挑逗长思考）直接打 Claude-format 端点
（`/anthropic/messages`，deepseek-v4-flash）：

| 请求 | 返回 thinking | text | stop |
|---|---|---|---|
| budget=128 | 56,569 字符（~14K tok） | 0 | max_tokens |
| budget=512 | 62,377 字符（~15.6K tok） | 0 | max_tokens |
| budget=2048 | 61,358 字符（~15.3K tok） | 0 | max_tokens |
| budget=128 + reasoning_effort=minimal | 61,632 字符（~15.4K tok） | 0 | max_tokens |
| 简洁性 system prompt + budget=128 | 58,411 字符（~14.6K tok） | 0 | max_tokens |
| **thinking=disabled** | **0 字符** | 8,707 | end_turn |

**结论**：
1. **`budget_tokens` 被 API 完全忽略** —— 128/512/2048 结果无差别，模型把整个
   max_tokens 输出预算全花在 thinking 上，连回答都不产出（`stop=max_tokens`）。
2. `reasoning_effort` 在 Claude-format 端点同样被忽略（OpenAI-format 端点 honor，
   但 gopi 走 Claude-format 是为原生 tool_use）。
3. 简洁性 system prompt 单独无效（模型照样写 58K 字符 thinking）。
4. **`thinking: disabled` 干净生效**（0 thinking + 完整回答）—— 降级通道可行。

## 修复方案（B+C+D 组合）

### 1. harness 层硬截断 + 降级重试（核心，internal/agent/loop.go）

`chatOnce` 重构为 attempt 循环：流式累计 thinking 字符（`thinkingCharsPerToken=4`
估算 token），超过预算（默认 2048，`GOPI_THINKING_BUDGET` 可配）时：

- 中断当前流（per-attempt context cancel，不杀 run 级 context）；
- 该轮丢弃（不进入历史），用 `thinking: {type: "disabled"}` 重发同一请求；
- stderr 打警告 `⚠️ thinking exceeded N-token budget — re-issuing round with
  thinking disabled`；
- 最多 2 次 attempt（重试轮 disabled 不再计数）。

非流式 fallback 路径同样后验检查（超限响应丢弃重试）。

**效果**：单轮 thinking 硬上限 = 预算；正常短思考轮（<预算）零影响。max_tokens
不动（384K），write 大文件能力保持。

### 2. thinking-mode 线格式规则修复（internal/deepseek/claude_client.go）

修复过程中实测发现第二条 API 规则：**thinking enabled 的请求，历史中凡带
tool_use 的 assistant 消息必须携带 thinking block**，否则 HTTP 400 "The
`content[].thinking` in the thinking mode must be passed back to the API"。
降级重试（disabled 模式）产出的 tool 轮没有 thinking → 下一轮（enabled）必 400。

实测验证：空 thinking block（`"thinking": ""` + signature）即满足该规则。
`BuildClaudeRequest` 现在对「tool 轮 + 无 thinking 文本 + 当前请求 thinking
enabled」注入空 thinking block（`Thinking: " "` —— omitempty 会丢掉真空串，
wire 上必须存在该字段；`EmptyThinkingSignature` 常量签名）。此修复同时覆盖
bug024 潜在路径（API 流式空 thinking + tool_use 被丢弃的场景）。

### 3. 系统提示词简洁性引导（internal/agent/loop.go）

新增 `conciseThinkingRules` prompt 块（"think briefly then act; 不重述计划；
直接给 tool call"），仿 claude 的 concise-output-style —— 压低典型轮次的
thinking 长度（claude 同模型同端点中位 423 字符）。提示词单独无效（实测），
但作为叠加层压低中位数；harness 硬截断抓失控尾部。

### 4. 接通 GOPI_THINKING_BUDGET（internal/agent/loop.go + cmd/agent/main.go）

env var 文档中存在但从未被解析（预算恒为默认 2048）——现在解析进
`Thinking.BudgetTokens`，非法值告警回退默认。

## 实测验证（真实 API）

预算 128 跑 stats.py 任务（6 轮迭代，exit 0，输出正确）：

- stderr 证据：`⚠️ thinking exceeded 128-token budget — re-issuing round with
  thinking disabled`
- 全程 thinking 合计 814 字符 ≈ 203 tokens（竞争 #3 同量级任务为 69,373 字符）
- 全程 146.1s 完成，输出正确（mean 19.17 / median 3.5 / stddev 36.17）
- 预算 64 跑 409 行 design.md 写入任务：完成，无 max_tokens 截断

原始日志确认序列：REQ1(enabled, 超限 abort) → REQ2(disabled retry, tool_use) →
REQ3(enabled, 注入空 thinking block) → …全链路无 400。

## 取舍说明

- **为什么不收紧 max_tokens**：maxtokens-truncation.md 教训 —— write draft 需要
  大输出空间，收窄会截断长文件。解法全部在 thinking 侧。
- **为什么 abort 而非等完**：失控轮 thinking 7.5K tokens @28tok/s ≈ 4 分钟；
  在预算处中断 + disabled 重试 ≈ 预算时间 + 一轮 disabled，硬上限。
- **为什么降级为 disabled 而非提示重试**：disabled 实测零 thinking + 完整回答，
  确定性最好；提示重试模型可能再想一轮。
- **重试成本**：仅失控轮发生（竞争 #3 数据中 3/30 轮），正常轮零开销；
  disabled 重试轮质量与 enabled 短思考持平（thinking-sweep：disabled 与
  enabled+minimal 任务质量相同，仅轮次多）。
