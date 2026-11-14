<!-- doc-class: historical -->
# Task Report: KEYWORD-CLEANUP — stopword 汤 keywords 批量清理

> 目标库：`~/.gopi/memory.db`（672→674 行，FTS 674 行同步）
> 备份：`~/.gopi/memory.db.bak-keyword-cleanup`（任务开始前已存在，08:39）+ 本次自建
> `~/.gopi/memory.db.bak-keyword-cleanup2`（md5 与清理前一致）
> 维护 CLI：`~/work/GoPi/bin/gopi memory_update <id> --keywords=...`（--keywords 同时重写
> search_kw 列 + answer 尾部 `keywords:` 行，stripKeywordsTail 防重复——验证过无残留/无重复）

## Completed

### 1. 全量识别（扩展检测，非仅 7 模式）

- **模式检测**：answer 尾部 `keywords:` 开头为纯 stopword（about/above/this/any/are/being/
  based/that/which/with/from/and）→ 命中 **64 条**（远超 epic 的 44 条估计）。
- **比率检测**（扩展）：jieba + stopword 词典把每个 search_kw 分词，计算
  `soup_ratio = 1 - 实义词/总词数`：
  - `ratio ≥ 0.5`（纯汤，无实义）→ **37 条 replace**
  - `0.3 ≤ ratio < 0.5`（汤+好词混合）→ **60 条 keep_good**
  - 两波合计 **97 条**；再补查 12 模式 LIKE 剩余行（#174/175/176 等逗号簇被比率阈值漏掉——
    中文碎片被误判为"好词"）→ 追加 **31 条**。
- **清理总数：128 条**（id 完整清单见 `/tmp/keyword_cleanup/execute.py` FINAL 字典，可复现）。

### 2. 清理策略（每条约 2-5 个真实关键词，中英混合，仿 5W1H 面包屑）

- **纯汤（replace，37）**：从 topic/question/answer 提取真实关键词。提取 = jieba POS 名词过滤
  （n/nz/vn/eng）+ 英文 stopword/碎片过滤 + topic 优先；对低质会话行（topic="研究调研" 等）逐条
  手工改写（如 #570→"gopi,researcher,报告,调研"）。
- **混合（keep_good + 追加，91）**：优先保留原好词部分（过滤 stopword + 前缀碎片如 archi←architecture、
  短碎片如 aug/cla）；好词不足 3 个或全为碎片 → 用 topic+question 提取替代。全部逐条人工复核。
- **关键修正**：① 2-3 字母规则会误杀真词（ops/cto/act），只对 1 字母与非缩写 2 字母判 stop；
  ② 5W1H 手写行（#105 "what,why,how,when"）是故意风格 → 只删 stopword 词保留其余；
  ③ `--keywords "--chat,..."` 会被 Go flag 解析成 flag → 用 `--keywords=值` 形式。

### 3. 批量执行

- 先在 `/tmp/keyword_cleanup/test.db`（`--db` 复制库）全流程验证 **128/128** 成功，再对生产库执行。
- 生产库 `memory_update` **128/128 成功**（2 个 FAIL 均为测试阶段暴露并修复：见上③）。
- 验证：`memory_get` 抽查 #524/172/333/174/105 keywords 正确、无汤、无重复 tail；FTS 行数 674=674 同步。

### 4. 验证

- **原垃圾命中** `memory_search "about actual"`：不再因汤 keywords 命中——#524 仅因正文真实含
  "actual default model" 以 tokens=2 命中（内容命中，非关键词污染），keywords 已变为
  `model, default, deepseek-v4-pro, 配置, 默认值`。
- **回归** `memory_search "deepseek v4 flash price adjustment"`：top1 = #715（本查询的修复经验，
  tokens=5 全覆盖，相关且正确）；#524 不再出现（cold + 无汤关键词）。
- **可检索性**：新关键词可检索（"fire-and-forget"→#550/549/548；"evennia pipeline scanner"→#333）。
- **抽样 5 条质量**：
  - #172 `调度器, 设计, 蒸馏, 记忆, 权重`
  - #174 `blueprint, 知识检索, 工具, 手册, llm`
  - #333 `evennia, pipeline, scanner, builder, runner`
  - #524 `model, default, deepseek-v4-pro, 配置, 默认值`
  - #550 `fire-and-forget, 异步, 任务, 修复, 验证`
- **清理后全库扫描**：12 模式 LIKE 命中 **0 条**；soup_ratio≥0.4 仅剩 1 条 #634
  （`failed,count,对比,before,after`——before/after 为有意的对比词，非汤）。

## Not Done

- 未改 topic/question/answer/tier/confidence（仅 #524 之前已 cold，未动）。
- 未删除低质会话行本身（"研究调研"/"执行结果"类 topic 的行 keywords 已尽力提取，行本身
  是否删除属另一任务）；#185/#689 未被本次改动波及（回归 top1 是 tokens=5 全覆盖的 #715，
  更相关）。
- 未合并/提交代码（本任务无代码改动，仅 DB 数据 + 本报告）。

## Verification

- Build: 不涉及（无代码改动）
- Tests: 不涉及
- 清理执行: 128/128 memory_update 成功
- 模式残留: 0 条（清理前 64 条）
- Commit: 无（数据任务；脚本留存 `/tmp/keyword_cleanup/`：analyze.py / plan.py / execute.py /
  final 映射，可复现）

## 复现方法

```bash
python3 /tmp/keyword_cleanup/plan.py      # 重新分类（依赖 ~/.gopi/memory.db + jieba）
python3 /tmp/keyword_cleanup/execute.py "--db=/tmp/xxx.db"   # 先复制库试跑
python3 /tmp/keyword_cleanup/execute.py   # 对生产库执行（128 条 FINAL 映射内置）
```
