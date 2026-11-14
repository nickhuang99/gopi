<!-- doc-class: normative -->
# 方案：Memory 路线图 M2 — tier 语义闭环 + confidence 动态衰减（feat/memroadmap-m2）

> 参照 devdoc/memroadmap-m1.md 风格：先方案后实现，验证结果追加在「⑥ 验收」节。
> 基于 master cfa8d5c（M1 已 merge：exam_questions+5W1H 全列 100% 填充，top1 36%→50%）。

## 背景与目标

M1 解决了"检索面"（问法多样性）。M2 解决"可信度生命周期"：一条记忆存进去后
confidence 是静态的——2024 年存的 0.85 和昨天存的 0.85 在排序里同样响亮，尽管
前者从未被重新验证过。hermes tier 哲学：**过时≠错误，只是可信度降**；无永久 hot。
M2 做五件事：

1. **tier 列语义落地**（列已存在，补行为）：
   - hot=常驻注入候选：任务开始时 harness 把 hot 记录作为"常驻候选"注入上下文
     （检索兜底——再冷的查询也能看见 hot 教训的存在）。
   - hot 可过期：hot + 超过 hotStaleDays 未重新验证 → meta 行提示 hot_stale
     （无永久 hot 的可观测信号；降级仍走手动 memory_update——only-up never auto-down）。
2. **confidence 动态衰减（惰性计算）**：查询时按 e^(−Δt/τ) 衰减 + 重新验证奖励。
3. **feedback 闭环强化**：useful → 存储置信升 / useless → 降 + 连续 useless 冷却提示。
4. **触发器同步 FTS 验证**：M2 新增的写路径（feedback 置信回写）必须保持 FTS 同步。
5. **验收**：反馈注入前后 top 命中对比 + tier/conf 分布检查（数据驱动）。

## 设计决策（写代码前定死）

### ② 衰减选惰性计算，不建 decay job

- `openSearchDB` 带 `PRAGMA query_only`（search 永不写——这是 foreign hermes
  production DB 只读兼容的根基）。惰性计算是纯读侧数学：`eff = f(base, timestamps,
  counts)`，不回写。decay job 则需要写权限 + 调度器 + 与共享 DB 的写锁竞争。
- 衰减是确定性函数（base、distilled_at、last_retrieved、retrieval_count、
  fb counts 全在行上），任何时刻算结果一致——物化它没有正确性收益，只有写放大。
- **结论：查询时惰性计算**。scanHit 里算出 eff conf，同时进排序（rescore 的
  conf 项）与渲染（meta 行）——一处计算，两处消费，无第二真值源。

### 衰减公式（单一时间常数，复用 fbDecaySeconds）

```
eff = clamp( base × decay(Δt) + reward + floor_offset , confDecayFloor, 1.0 )
  Δt    = now − max(distilled_at, last_retrieved)     # 重新验证即刷新
  decay = e^(−Δt / fbDecaySeconds)                     # τ=90d，与 feedback 衰减同源
  reward = confReverifyCap × min(1, retrieval_count / confReverifySaturate)
```

- 常量（全部命名，数据依据见 ⑥）：
  - `confDecayTauSeconds = fbDecaySeconds`（90 天——confidence 与 feedback 共享
    一个遗忘时间常数：两条曲线说的是同一件事"多久没被验证"，分开就是两套 magic number）
  - `confDecayFloor = 0.05`（scanHit 现有 0.05 字面量提升为命名常量，同一处语义）
  - `confReverifyReward 用 confReverifyCap=0.15`：封顶奖励——10 次（
    `confReverifySaturate=10`）检索即视为充分重新验证，此后不再涨（防热门记录
    confidence 无界爬升挤掉新知识）
- `possiblyStale`（−1.5 排序惩罚）保持不变：它是布尔"可能过时"信号，衰减是连续
  可信度——两者正交，meta 行都能看到。

### ③ feedback → confidence 回写（机器唯一写置信的地方）

在 MemoryFeedback 既有事务里（append-only 事件 + EMA 物化之后）追加：

```
useful  → confidence += confVoteStepUseful   (0.05)
useless → confidence += confVoteStepUseless  (−0.10，2× 步长)
clamp [confDecayFloor, 1.0]；unused 不动
```

- **为什么不对称**：bug-report 文化——被矛盾的记忆若被信任是主动伤害（快速降），
  被采纳的记忆是增量确认（慢速升）。步长小：单票撬不动排序（conf 项权重 1.0，
  一个 token coverage=2.0），4 票 useless = −0.4 才与一个 tier 权重同量级。
- **冷却**：decay 后 useless 质量 ≥ fbColdSuggestThreshold（3.0，复用）且
  useful=0 → MemoryFeedback 的返回文本直接携带 cold 建议（`memory_update <id>
  --tier cold`）——建议式，执行仍手动（FeedbackSuggestCold 同款契约）。
- FeedbackRecompute **不碰 confidence**（它只重建 fb 物化列——置信回写是一次性
  状态迁移不是事件重放，重放会二次衰减；devdoc 记录此边界）。

### ① hot 常驻注入 + hot_stale

- `tools.MemoryHotResident(limit)`：tier='hot' 查询，按 last_retrieved DESC、
  confidence DESC，渲染单行摘要（#id tier conf Q[:120]）。
- agent 侧：任务开始 latch 处（taskStartSearchMessage 注入点），有 hot 记录时
  追加 compact 块（`hotResidentInjectLimit=3`、`hotResidentMaxChars=600`），
  best-effort——DB 打不开就跳过，绝不阻断任务开始。检索兜底语义：常驻 ≠ 全文
  注入，只是"存在性提示"（LLM 需要细节会自己去 memory_search）。
- `hotStaleDays = 7`：hot 且未重新验证超过 7 天 → meta 行 `hot_stale` 提示。
  无永久 hot 的机器可读信号；自动降级仍然不做（only-up）。

### ④ 触发器同步 FTS

M1 的 ai/au/ad 触发器已按探测列集动态渲染（m1FTSTriggers）。M2 的全部新写路径
（feedback 置信回写 = UPDATE）天然落进 au 触发器 —— **验证性任务**：
- 测试断言 MemoryFeedback UPDATE 后 FTS MATCH 仍能命中该行（trigger 同步未断）。
- tier/confidence 是值信号不进 FTS 列集——确认无需重建。
- ensureFeedbackSchema 的 heal 路径补 tier/confidence 列探测（古董库缺失时
  ALTER ADD，幂等）。

## 实施清单

| # | 文件 | 内容 |
|---|---|---|
| 1 | internal/tools/memory.go | M2 常量块 + m2EffectiveConfidence + scanHit 接线 + MemoryFeedback 置信回写/冷却提示 + MemoryHotResident + hotStale + searchExtraSelect/scanHit 增 retrieval_count |
| 2 | internal/agent/loop.go | 任务开始 hot 常驻注入（best-effort，latch 同点） |
| 3 | internal/tools/memory_m2_test.go | 衰减/回写/冷却/hot_stale/常驻注入/FTS 同步 单测 |
| 4 | scripts/m2_acceptance.py | 反馈前后 top 命中对比 + tier/conf 分布（prod DB 副本） |
| 5 | devdoc/memroadmap-m2.md | 本文件 + 验收结果 |
| 6 | Rule 11 验证 | 2 个真实场景跑 gopi，session JSONL 证据 |

## ⑥ 验收结果（2026-08-31 实测）

### 单元测试（internal/tools）

- `go test ./internal/tools/ -run 'TestM2' -count=1`：**8/8 PASS**
  （衰减表驱动 6 case / 衰减重排序 / 投票双向步长 / 冷却建议 / feedback
  UPDATE 后 FTS 仍命中（au 触发器同步）/ hot_stale 双向 / hot 常驻摘要+空库
  幂等 / legacy 库五列 heal）。
- 全量 `go test ./...`：internal/tools ok（229s）；internal/agent 2 项
  预存失败（TestAutoTaskRegisterFailedStatus /
  TestChatTaskRegistration_WrapperFailedStatus）——master 上同样失败，
  bug 单 t-1788021020，与本分支无关。

### 数据驱动验收（scripts/m2_acceptance.py，生产库副本）

- **反馈前后 top 命中**：12 项抽测，对每项的 top1 投 useful 后重跑——
  top1 **12/12 → 12/12**，top3 **12/12 → 12/12**，回归 **0**（门槛：useful
  票不得丢 top 命中）。排序位移仅见同分近邻互换（946/1062 的 BM25 rank
  tie 抖动为预存现象：未投票重跑也会互换）。
- **tier 分布**：{cold: 122, warm: 951, hot: 0}（总 1073）——cold 占比
  11.4%（历史低质降级），hot 未填充（M2 提供机制，hot 化由使用积累）。
- **confidence 分布**：mean=0.85 sd=0.007 min=0.65 max=0.90，
  越界 [0,1] **0**。min=0.65 即 M2 机器步长的痕迹（0.85 − 2×0.10）——
  分布形状开始分化，不再全库单值。
- hot_stale 份额：hot 行为 0，份额检查空跑通过（逻辑由单测覆盖）。

### Rule 11 —— LLM 真实调用验证（3 个真实 gopi 会话）

方法：#1014 配方（HOME 隔离 + scratch DB + 预种矛盾 + 自然任务文本 +
session JSONL tool_use 解析 + DB 复核）。会话：

- **场景 A**（session `6335c7ee`，scratch-a.db，预种 DefaultCompactTargetRatio=0.5 vs 实际 0.35）：
  tool_use 链 `memory_search → read → context_trim → memory_feedback(useless)
  → memory_update(修正 0.35) → memory_store`。**M2 效果 DB 复核**：id=1
  confidence **0.85 → 0.75**（机器 −0.10 步长真实落库）✅
- **场景 B2**（session tmp-m2/home-b2，hot 记录正确时）：任务开始注入
  **hot 常驻摘要**（JSONL 含 "Hot-tier resident candidates"）→ LLM 查证
  无矛盾 → 投 useful → confidence +0.05 ✅（补充验证：正确记录奖励通路）
- **场景 B3**（session tmp-m2/home-b3，scratch-b3.db，预种 hot 记录
  80% 错值 + 2.9 decayed useless）：tool_use 链 `memory_search → read →
  memory_feedback(useless) → memory_update → memory_store`。
  useless 票越过冷却门槛（2.9→3.9 ≥ fbColdSuggestThreshold）→ 投票返回
  文本携带 **COOLDOWN 建议**（JSONL 含 "COOLDOWN: ≥3 decayed useless…"）
  → LLM 随后的 memory_update 修正了记录内容（keywords 写入 50%/64% 正值，
  标记 80% 为 stale wrong）而非盲目降级——建议被消费且判断更优（记录可
  救活时修内容优先于降级）。**建议式契约按设计工作** ✅

反证：B3 若无 M2，投票返回只有 fb_score 行，无 COOLDOWN 提示、confidence
不动（master 行为）；JSONL 中 COOLDOWN 文本 + DB conf=0.75 都是本分支新产生。

### 设计修正记录（实现中发现）

- 无日期记录（distilled_at 缺失）**不衰减**：无证据不惩罚（宁缺勿滥，
  与 possiblyStale 对 unparseable when_at 的处理一致）——scenario A 单测
  首版按"落到地板"设计被测试纠正后定稿。
- hot 常驻摘要 / task-start 检查点消息补写 session JSONL 审计（此前只进
  wire 不进日志——Rule 11 检查无法从日志区分"没注入"与"注入未记录"）。

## ⑦ 交付物

- `internal/tools/memory.go`：M2 常量块、m2EffectiveConfidence、
  scanHit/searchExtraSelect 接线（eff_conf 进排序+渲染）、MemoryFeedback
  置信步长+冷却建议、MemoryHotResident、hotRecordStale、heal 扩展。
- `internal/agent/loop.go`：任务开始 hot 常驻注入 + 会话日志审计。
- `internal/tools/memory_m2_test.go`：8 项回归测试。
- `scripts/m2_acceptance.py`：数据驱动验收（可复跑）。
- `devdoc/memroadmap-m2.md`：本文件。
