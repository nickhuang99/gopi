<!-- doc-class: historical -->
# Task Report: gopi stats 成本核算 v2 — 真实成本 + 业界研究

日期 2026-09-08。分支 `feat/stats-cost-v2`（worktree `/home/user/workspace/gopi-stats-cost-v2`）。
Epic: 业界研究(1) → 官方价格表验证(2) → `gopi stats --cost` 设计实现(3) → per-tool 成本(4) → 验证+交付(5)。

## Completed

### 1. 业界研究（research → 借鉴维度清单）
研究 Claude Code / Codex / LangSmith / Langfuse / Helicone / OpenAI usage API / llm-cost 库的成本实践，
提炼可借鉴维度（详见本报告 §A 借鉴清单）。没有照抄——每个入选维度都按 gopi 的单二进制 /
session JSONL / raw log 数据源适配。

### 2. 官方价格表验证（devdoc 三重互证数字复算吻合）
用真实 session 复算 devdoc/cost-game-ds-glm-real-session-analysis-20260908.md 的数字，全部吻合：
- DS session 88299d17（2026-09-07 22:30 闲时，101 req）：**total 0.6793 元**，
  hit .1579(23%) / miss .2117(31%) / out .3098(46%)，token n² amp 22.4x
- GLM session 002c43f2（2026-08-30 11:29，五折内，65 req）：**total 0.372 元**，
  hit .2489(**67%**) / miss .0448 / out .0783 —— GLM 大头=命中 67% ✓
- fixture fc426fed（DS 20:40 闲时，9 req）：0.0170 元
DS 高峰判定按 session 起始小时（9-12 / 14-18 北京时间 ×2）；GLM 无分时。
9/8 今日 < 9/10 → GLM 五折仍生效。模型族 `deepseek-*` / `glm-*` 前缀推断。

### 3. `gopi stats --cost` / `--bill`（核心交付）
- **internal/pricing**（新包，零依赖）：声明式价格表 + 日期/时间感知。
  内置表（GOPI.md Rule 1 — 常量声明、无魔法数字内联）：
  | provider | regime | input | cache-hit | output |
  |---|---|---|---|---|
  | GLM-5.3-flash 五折（→2026-09-09 24:00 +08）| 0.4 | 0.115 | 1.4 |
  | GLM-5.3-flash 原价（9/10 起）| 0.8 | 0.23 | 2.8 |
  | DS v4-flash 平价 | 1.5 | 0.05 | 4.5 |
  | DS v4-flash 高峰×2（北京 9-12/14-18）| 3.0 | 0.10 | 9.0 |
  - 时间窗数据驱动（Until / Peak 字段），tier 解析按 priced-at 时刻
  - `ProviderOfModel` 前缀推断；未知模型 → ok=false → stats 报"不可定价"而非瞎算
  - settings.json 可选 `"pricing"` 段 override（operator 实际费率逃生舱，REPLACE 不 merge）
  - 每个价格带 Reference 溯源（devdoc/glm-5.3-pricing.md 等）——钱的事可追溯
- **`gopi stats <id> --cost`**：COST box —— total 元 + 三元(hit/miss/out)各自元+tokens+% /
  regime / **n² money amp**（命中/新增 money 比，与 token n² 对比：DS 22x token → 0.7x money）/
  priced at（session 起始时刻）/ price ref
- **`gopi stats <id> --bill`**：--cost + PER-ROUND 元曲线（RND HIT/MISS/OUT/ROUND/CUM，
  CUM 用未舍入累加 → 终点==box total）+ PER-TOOL 边际元排名
- provider 从 model 推断；`--all` 上 reject（混模型/日期无法定价）；未知模型不含糊报错
- --json 默认零破坏（仅 --cost/--bill 时填 Cost 字段）

### 4. per-tool 边际元排名（LangSmith per-trace 聚合维度 → gopi 轮级等分）
每轮 元 在轮内 distinct tools 间等分（无工具轮 → "(no tool call)" residual，总和恒 == box total）。
fixture fc426fed: write_task_area 3 调 0.0064(52.8%) / memory_feedback 0.0046 / memory_search 0.0011。

### 5. 验证 + golden + 文档
- golden: stats-cost.txt / stats-bill.txt（含 per-tool 块）已重录
- 测试: pricing_test（devdoc 三重互证表 + 真实 session 复算 + 峰值×2 + override roundtrip +
  settings override）、cli_stats_test（--cost/--bill parse + fixture 0.0120 + 未知模型 + --all reject）、
  TestToolCostsSumBack（sum-back 单测）、TestStatsCostOverFixture
- 真实 session 复核: 88299d17 0.6793 元 / GLM 002c43f2 0.372 元 —— EXACT 匹配 devdoc

## Not Done
- thinking vs content 输出拆分（OpenAI reasoning_tokens 维度）：gopi 的 raw usage 无 reasoning
  token 字段，数据源不支持 → 未做（记录为研究发现的"有数据再接入"维度）
- 每轮成本曲线图（ASCII sparkline）：--bill 表格已覆盖，曲线图留待用户反馈
- settings 文档化示例：README 未加 "pricing" 段示例（docstring 里有）

## Verification
- Build: `go build ./...` OK
- Tests: `go test ./...` 全绿（见 commit 时完整输出）
- Golden: stats-cost.txt / stats-bill.txt 重录，fixture 复核通过
- Commit: 见 `git log`（本任务分支 feat/stats-cost-v2）

---

## §A 业界研究借鉴清单（交付物 1）

| 来源 | 实践 | 借鉴维度 | 入选 gopi? |
|---|---|---|---|
| Helicone / LiteLLM | 定价 = JSON 表（model_prices.json）声明式 per-1M rates，拆 cache_creation / cache_read / output | 声明式价格表（非硬编码 if-else）| ✅ 入选（Default() 声明式 Tier 表 + settings override）|
| Anthropic | cache_read ~90% off / cache_write +25% | 三明治缓存计费认知：命中/新增分开算 | ✅ 已有（三元 split）|
| OpenAI | cached ~50% off / reasoning_tokens 单列 | thinking vs content 拆分 | ⏳ 数据源有则接（gopi usage 无 reasoning 字段）|
| Claude Code | /cost 面板 + 状态栏累计 cost | 实时累计成本 UX（用户严肃对待）| ✅ 会话成本 --cost box 即累计 |
| Codex / OpenCode | session / daily / monthly cost meters | 周期聚合 | ⏳ --all 混模型被拒——按日聚合留作未来 |
| LangSmith / Langfuse | trace 级聚合：cost by model / by trace / by session，input/output token 拆分，每模型定价映射 | per-tool / per-session 成本归因 | ✅ per-session（三元）+ per-tool 边际排名 |
| OpenAI usage API | cached_tokens + completion_tokens_details 明细 | 账单结构（per-request）| ✅ 同构（raw 已有 cache_hit/miss/completion）|
| 业界 | savings / cache savings 对比（原价 vs 实付）| 显示省钱额 | ⏳ GLM 五折到期后自然消失，未做原价对比行 |
| llm-cost 类库 | tokens/1M × rate（纯计算，tiktoken 只 count 不 cost）| 成本=usage×rate 最小公式 | ✅ CostOf() 即此式 |

选入原则：gopi 单二进制/session JSONL/raw log 数据源下**能真实算出来**的维度才做；
纯观测平台图表/计费后台的炫技维度不做。
