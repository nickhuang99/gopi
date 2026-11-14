<!-- doc-class: normative -->
# DS vs GLM 成本博弈：真实 session 数据核算（cache 二次增长 / trim 边界 / 输出大头）

日期 2026-09-08。触发：nick 指出成本精读报告未转行动 + 强调 DS/GLM 竞争的关键是
cache-hit-rate 维持能力。用 gopi 真实 session 数据核算。

## 数据源
- session 88299d17（cost-reading Bond，101 requests，DS，4 trim）
- session 15f8ff7a（bug-fix Bond，199 requests，DS，5 trim）
- 每 request 有 cache_hit/cache_miss/completion_tokens/prompt_tokens

## 核心发现 1：命中二次增长实锤
- 88299d17: 命中/新增 = 22.4x（3.16M 命中 vs 141K miss）
- 15f8ff7a: 命中/新增 = 22.9x（7.47M vs 326K）
- 接近理论 n/2（101 轮≈50x 上界被 trim 打断）——agent 反复小请求全命中，
  命中量是输入的 n(n-1)/2 二次增长。nick 理解正确。

## 核心发现 2：trim 经济——9 次中 7 次净赚（出人意料）
| session | trim 轮 | 省前缀/轮 | 额外 miss/轮 | DS 净 |
|---|---|---|---|---|
| 88299d17 | 30 | 37K | +561 | 赚 0.0010 元/轮 |
| | 38 | 21K | -2428 | 赚 |
| | 54 | 33K | -21 | 赚 |
| | 98 | 32K | +973 | 赚 0.0001 |
| 15f8ff7a | 48 | 66K | +856 | 赚 |
| | 87 | 106K | -1170 | 赚 |
| | 111 | 34K | -6901 | 赚 |
| | 138 | 23K | +1090 | **亏** 0.0005 |
| | 155 | 17K | +1401 | **亏** 0.0013 |

**为什么大多净赚**：write_task_area 结论注入让 trim 后的前缀**稳定**（结论不再变）
→ 后续轮次立即命中新前缀 → 无"trim 后全部重发 miss"灾难。省的是每轮重发的
2-10 万前缀命中；额外 miss 仅 +0.5K-1.4K/轮。

**边界 = trim 时机**：结论稳定后的 trim 净赚；结论动荡期（mid-调试）的 trim 净亏
（15f8ff7a 轮 138/155）。结论链稳定是 trim 收益的前提。

## 核心发现 3：两模型成本结构根本不同（同一 session）
| | DS 定价 | GLM 定价 |
|---|---|---|
| 88299d17 占比 | 命中 23%/miss 31%/**输出 46%** | **命中 70%**/miss 11%/输出 19% |
| 15f8ff7a 占比 | 命中 21%/miss 28%/**输出 51%** | **命中 68%**/10%/22% |
| 总价 (同 session) | 0.68 / 1.76 元 | 0.52 / 1.27 元 |

- **DS 大头 = 输出**（46-51%）——深思考 thinking 的 4.5-9 元/M 是大头。省 DS
  钱的第一杠杆 = 控制 thinking 输出（thinking-trim 已做，剥离 reasoning 省 99.8%）
- **GLM 大头 = 命中**（68-70%）——cache 单价 0.115 贵。GLM 跑长会话的省钱 =
  最大化命中率（少 trim / 保持前缀稳定）
- **同一个 session，两模型的省钱策略相反**——印证 nick 的"不同模型玩不同游戏"

## 对策略的含义
1. **compaction vs cache 竞争被化解大半**：write_task_area 稳定结论块 = trim 后
   新前缀可命中——memory-aware T1 设计的意外红利（非预期的 cache 友好性）。
2. **DS 命门是输出不是 cache**：继续 thinking-trim / 控制 reasoning——比纠结
   trim 频率更有效。
3. **GLM 命门是 cache**：GLM 会话避免频繁 trim；若跑长任务，让前缀尽量稳定。
4. **9/1 GLM 184 元峰值**（16.08 亿命中）确认：agent 工作模式的命中二次增长对
   GLM（高 cache 单价）是灾难性的——GLM 会话要短或前缀极稳定。

## 待做（成本核算落地）
- gopi session_end 加 cost 核算（DS/GLM 双定价 × hit/miss/output）——让每个
  session 完成时自动报成本结构（现在只有 token 无元）
- 或 CLI: gopi stats 加 --cost 输出元（用 settings 的 provider 定价）
- devdoc 已存档；成本核算工具 /tmp/cost_analysis.py 可复用
