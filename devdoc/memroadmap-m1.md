<!-- doc-class: normative -->
# 方案：Memory 路线图 M1 — exam_questions + 5W1H 并行检索面（feat/memroadmap-m1）

> 参照 devdoc/task-notify.md 风格：先方案后实现。本文件是 M1 的实施蓝图，
> 实现完成后的验证结果追加在「⑥ 验收」节。

## 背景与目标

hermes 反向 RAG 已验证：每条 memory 预生成 2-3 种"考试问法"（exam_questions），
查询命中问法≈命中记忆本体，填充率 97%。GoPi 的 memory_search 目前检索面 =
topic/question/answer/sources/search_kw/jieba_token 六列，问题是**问法多样性
不足**——用户问"怎么批量改关键词"时，存的是"keywords 批量清理"，token 覆盖率
低就排不上来。M1 做四件事：

1. schema 扩展：`exam_questions` 列 + 5W1H 独立列（what/why/when/where/who），
   **只加不删**（现有列全部保留，external-content FTS 索引追加新列）。
2. backfill：存量 ~1069 条全量补 exam_questions + 5W1H（LLM 批量生成，
   hermes `batch_exam_gen.py` 模式：DeepSeek flash + thinking:disabled 防
   reasoning 吃 max_tokens）。
3. memory_search 三级路由验证：FTS5 exact-token（≥3 字 CJK 走 trigram）+
   LIKE（1-2 字）+ **新列参与检索与排序**。
4. 验收：12 项抽测（中英混合/同义变体/5W1H 维度各 2×2），before/after 命中率
   对比；devdoc + 蒸馏。

## hermes 参考摘要（读过什么、取什么）

| 参考 | 位置 | 取用 |
|---|---|---|
| `batch_exam_gen.py` | ~/work/glow-moon/bin/ | 批量生成模式：每批 5 条拼 prompt → 单次 LLM 调用 → JSON `{"0":["Q1","Q2"]}` 按 id 回填；NO_JSON/PARTIAL/STALL(10 批零进度熔断) 三类防御 |
| `agent/fts5_query.py` | hermes-agent | 自适应 BFS（AND→子集→OR）+ IDF 降噪思路——GoPi 已有等价物（Route1→1.5 OR→trigram→LIKE），不照搬 BFS，仅验证三级路由后决定是否需要 |
| 5W1H 标注规范 | hermes compare_5w1h_report.txt | 标签理想 2-6 字、每维 ≤3 个、去冗余——DeepSeek 综合扣分 73 vs Ollama 234，**选 DeepSeek** |
| distilled.db schema | ~/.hermes/distilled_knowledge.db | exam_questions 为 JSON 数组字符串列（非 FTS 列）——GoPi 改为**参与 FTS 索引**（问法必须可被 MATCH） |

## ① Schema 扩展（只加不删）

### 新列（5 个 + 1）

```sql
ALTER TABLE distilled_qa ADD COLUMN exam_questions TEXT DEFAULT '';  -- JSON ["问法1","问法2"]
ALTER TABLE distilled_qa ADD COLUMN kw_what  TEXT DEFAULT '';
ALTER TABLE distilled_qa ADD COLUMN kw_why   TEXT DEFAULT '';
ALTER TABLE distilled_qa ADD COLUMN kw_when  TEXT DEFAULT '';
ALTER TABLE distilled_qa ADD COLUMN kw_where TEXT DEFAULT '';
ALTER TABLE distilled_qa ADD COLUMN kw_who   TEXT DEFAULT '';
```

- **只加不删**：现有 18 列原样保留；ensureSchema 幂等，老库缺列自动补（与
  ensureFeedbackSchema 的 PRAGMA-probe + ALTER 动态列同模式）。
- 命名 `kw_*` 避免与 SQL/FTS5 关键字混淆，且明确是"检索关键词"不是正文。

### FTS 索引面变化（核心决策）

- `distilled_qa_fts`（exact-token，unicode61）**列集扩展到 12 列**：
  topic, question, answer, sources, search_kw, jieba_token, exam_questions,
  kw_what, kw_why, kw_when, kw_where, kw_who。
  - external-content 表 + triggers 同步全部更新（ai/au/ad 三触发器重写）。
  - **存量库迁移**：CREATE VIRTUAL TABLE IF NOT EXISTS 不会更新已有表的列集
    → 用 `ALTER TABLE ... ADD COLUMN`?（FTS5 不支持）→ 正确做法：**检测列集
    不齐时 drop + 重建 FTS 表 + INSERT 全量重灌**（external-content 表重建
    成本 = O(n)，1069 行毫秒级），triggers 建完后 `INSERT INTO
    distilled_qa_fts(distilled_qa_fts) VALUES('rebuild')`。
- `distilled_qa_tri`（trigram）：列集保持 question/answer/topic 不动——
  问法是**整句 CJK**，trigram 子串召回天然受益于 question 列里没出现的措辞
  （问法本身进 exam_questions 列，trigram 索引它会让"问法→记忆"的子串命中
  成为可能，但 trigram 对 unicode61 的长 CJK run 已够用；**M1 先不动 tri**，
  避免重建面扩大，验收数据说话）。
- LIKE 路由（2 字 CJK）：查询子句从 `question/topic` 扩到
  `exam_questions/kw_what/kw_why`（1-2 字问法词最常见的是 what 维度短词）。
  where/who/when 列保持 LIKE 兜底但低频，不加。

### scanSearchHit / searchExtraSelect 适配

- 新列**不进** value-signal 的 searchExtraSelect（它们是检索面不是值信号）；
  排序变化走 queryTokenCoverage 的 haystack 扩展（见 ③）。

## ② backfill（scripts/backfill_exam_5w1h.py）

hermes batch_exam_gen.py 逐行同款 + GoPi 适配：

- 输入：`~/.gopi/memory.db`（先复制到 /tmp 试跑，验证后上生产——#723 套路）。
- 模型：`deepseek-v4-flash`（真实 DeepSeek key 来自 ~/.hermes/.env；
  DEEPSEEK_API_KEY 环境里那个是 GLM 代理 key，模型名对不上，不用），
  `thinking: {"type":"disabled"}` 防 reasoning 吃 max_tokens。
- 一次请求一批 5 条（question[:200]+answer[:300]），prompt 要求：
  - 每条 2-3 个考试问法（不同角度，≤30 字，中英混合场景保留原术语）；
  - 5W1H 五维标签（每维 0-2 个、2-6 字，no 该维→空数组）；
  - 严格 JSON：`{"0":{"eq":["Q1","Q2"],"w":["…"],"y":["…"],"t":["…"],"e":["…"],"o":["…"]},…}`。
- 回填列：exam_questions=JSON 数组；kw_* = 逗号串。
- 防御：NO_JSON / PARTIAL / STALL（连续 10 批零进度熔断）/ 429 退避。
- 断点续跑：WHERE exam_questions IS NULL OR ''，天然幂等。
- 完成后跑 `INSERT INTO distilled_qa_fts(distilled_qa_fts) VALUES('rebuild')`
  同步 FTS 索引（新列内容进索引）。

## ③ memory_search 三级路由验证（代码改动）

1. **exact-token 路由**：escapeQuery 不动（注入安全已验证）；FTS 列集扩展后
   exam_questions/kw_* 自动参与 MATCH。
2. **排序扩展**：queryTokenCoverage 的 haystack 从 `topic+question+answer`
   扩为 `+exam_questions+kw_what..who`——同义变体问法（用户措辞≈问法而非
   正文）直接吃 token coverage 分。
3. **LIKE 路由**：2 字 CJK 子句加 exam_questions/kw_what/kw_why 列。
4. **store 侧**：MemoryStore 写入新列（空默认；LLM 可传 5W1H keywords 时
   分维写入——prompt 引导后续 M2 再说，M1 保证 schema+检索通路）。
5. **验证**：三级路由各打新列的用例进 memory_test.go（FTS hit on
   exam_questions / LIKE hit on kw_what / coverage 排序提升）。

## ④ 验收（12 项抽测）

抽测集 = hermes 蒸馏抽测同款：**中英混合 2 + 同义变体 2 + 5W1H 维度 8**
（what/why/when/where/who 各取 2 项——直接用 backfill 后库里真实记录的
kw_* 标签构造查询，问法与正文措辞错开）。指标：

- **before**（master 分支二进制 + 未 backfill 库副本）：12 项 top1/top3 命中率。
- **after**（本分支二进制 + backfill 后库副本）：同 12 项重跑。
- 命中判定：top1 或 top3 内出现目标记录 id（人工核对期望 id，脚本输出对照表）。

## ⑤ 交付物

- `internal/tools/memory.go`：ensureSchema 扩展 + FTS 重建迁移 + 路由/排序适配 + store 写新列。
- `internal/tools/memory_test.go`：三级路由新列用例。
- `scripts/backfill_exam_5w1h.py`：存量 backfill 脚本（可复跑、可试跑）。
- `devdoc/memroadmap-m1.md`（本文件）：方案 + 验收结果。
- 蒸馏：memory_store M1 套路。

## ⑥ 验收结果（2026-08-30 实测）

### 回填填充率

| 库 | 总行数 | exam_questions 填充 | 填充率 |
|---|---|---|---|
| /tmp/m1-after.db（试跑） | 1070 | 1070 | **100%**（hermes 97%）|
| ~/.gopi/memory.db（生产） | 1072 | 1072 | **100%** |

生产库备份：`~/.gopi/memory.db.bak-pre-m1`。索引同步：fts=tri=base=1072。

### 12+2 项抽测 before/after（scripts/m1_acceptance.py）

before = master 二进制 + 无 M1 列库副本；after = 本分支二进制 + 回填库。
查询集：中英混合 2 + 同义变体 2 + 5W1H 维度各 2（选取器自动挑每行
区分度最高（DF 最低）的标签做查询词——泛化词不算检索钩子）。

| 指标 | before | after | 提升 |
|---|---|---|---|
| top1 命中 | 5/14 | **7/14** | +2 |
| top3 命中 | 6/14 | **9/14** | +3 |

top1 命中率 36%→50%，top3 43%→64%。逐例：

- **M1 直接受益**：'迭代时'(when) before=[133,298,699] → after=[773,…]（top1 翻正）；
  '解决分歧'(why) before=[1018,194,232] → after=[848,…]（top1 翻正）；
  'hermes 命令行模块有哪些' top1 前进了 10 位进入 top3。
- **无回退**：14 项中 0 项 after 命中位次劣化超过噪声；'内网' after
  top3 从 [283,293,282] → [283,#1,293]（#1 进入 top3）。

### 测试

- `go test ./internal/tools/ -run 'TestM1'`：5/5 PASS
  （fresh 布局 / legacy heal / trigram 问法召回 / LIKE 标签召回 / coverage 排序）。
- `go test ./internal/tools/ -run 'TestMemory'`：全绿（48+ 项回归）。
- 已知无关失败：master 上 `internal/agent`
  TestAutoTaskRegisterFailedStatus / TestChatTaskRegistration_WrapperFailedStatus
  预存失败（已提 bug 单 t-1788021020）。

### backfill 运行数据

- GLM paas/v4 glm-4.5-flash，batch=10、并发=2：≈30 行/分；
  batch=10、并发=8：429 限流；**服务端实际串行化，并发>2 无增益**。
- 三轮防御实录：429 backoff 生效（多轮 5-20s 退避后成功）、
  NO_JSON/PARTIAL 记录 stderr、断点续跑三轮补漏（pass2 90 行、
  pass3 重灌 423 行 pass-1 截断污染行、pass4 20 行超时漏网）。
- pass-1 教训：ASCII 标签截断成死面包屑（memory_searc）+ 污染 FTS
  索引 → clamp 保留 ASCII 词完整 + 重灌受损行。
