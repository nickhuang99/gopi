<!-- doc-class: historical -->
# Task Report: 记忆检索演习场（Memory Arena）— 把 Skill Arena 三段式搬到记忆

**run**: `01a1268a-e2cb-7f12-8404-19565c70d6c6` · **日期**: 2026-10-11 · **主机**: hosta (client)
**性质**: 纯测量。**未改 memory 引擎、未改 `distilled_qa` 任何一条记录**（全程 `mode=ro`）。

---

## 0. 一句话结论

> 同一批 40 条记忆、同一条 `gopi memory search --limit 10` 通道：
> **抄原文关键词**的问法 **40/40 命中（34 perfect / 6 partial）**；
> **不抄词的自然问法** **0/120 命中（全部 fail）**。
> 这不是"测量失效"——对照组把测量通道证明了；这是被测系统的一个**确定性质**：
> **检索要求字面词面匹配。** 问法与记忆之间只要没有共享的实词，目标记忆**排在第 10 名之外**，
> 返回的 10 条命中里 **99% 的 `tokens=0`（零词面覆盖）**。

---

## 1. 设计（三段式，照 Skill Arena 的 Prepare→Exercise→Feedback）

| 阶段 | 做什么 | 产物 |
|---|---|---|
| **Prepare** | 固定种子抽样 + 每条用 LLM 生成 3 个"不抄原文词"的自然问法（并记录故意避开的原文词）+ 1 个"抄原文关键词"的对照问法 | `sample.json` `questions.jsonl` |
| **Exercise** | 逐条**串行**跑 `gopi memory search "<问法>" --limit 10`，记录目标是否出现/排名/命中条/`tokens=N` | `arena_log.jsonl`（增量、可断点续传） |
| **Feedback** | 判定 perfect/partial/fail → 把 partial+fail 诊断成可执行动作类 | `analysis.json`、本报告第 5 节 |

**与 Skill Arena 的对应关系**：技能演习场测"该调用的技能有没有被调用"；记忆演习场测
"**存进去的记忆能不能被自然地问回来**"。判定是确定性的（目标 id 是否出现在返回的前 10 里），
不是 LLM 主观打分——与 Skill Arena 的 `expected_action` 匹配同一思路。

**判定口径**（`arena.py:cmd_run`）：
- `perfect` = 目标条出现在返回列表**第 1 位**
- `partial` = 目标条出现在前 10 但**不是第 1 位**（"被压过"）
- `fail` = 前 10 里没有目标条

---

## 2. 实测结果

### 2.1 三联数字

| 组 | n | perfect | partial | **fail** | 命中率 | perfect 率 | 命中的平均排名 |
|---|---|---|---|---|---|---|---|
| **natural**（不抄词问法） | 120 | 0 | 0 | **120** | **0.0%** | 0.0% | — |
| **control**（抄词问法） | 40 | 34 | 6 | 0 | **100.0%** | 85.0% | **1.20** |

### 2.2 "存了但搜不到"的比例

按**记忆条**统计（40 条里任意一个自然问法能命中即算命中）：

| 口径 | 命中 | 未命中 | **未命中比例** |
|---|---|---|---|
| natural 组（3 个自然问法全落空才算 miss） | 0 / 40 | 40 / 40 | **100.0%** |
| control 组（1 个抄词问法） | 40 / 40 | 0 / 40 | 0.0% |

→ **"存了但用自然语言搜不到" = 40/40 = 100%**（在同一批语料上，"抄词"能搜到 = 40/40）。

### 2.3 测量失效检查（先证伪自己，再下结论）

全 120 个 natural 问法返回的都是**完整 10 条**（`n_hits` 分布 = `{10: 120}`），
而返回的命中行 **1189/1200 条 `tokens` 为 0**（引擎对 0 覆盖省略 `tokens=` 字段，实测确认非解析 bug）：

| 观测 | 值 |
|---|---|
| natural 组返回条数 | 恒为 10（`Counter({10: 120})`） |
| 命中行的 `tokens=0` 占比 | **99.1%**（1189/1200） |
| natural 组 `tokens>=1` 的命中行 | 11 条（散在 10 个查询里） |
| control 组**目标条**的 `tokens` 均值 | **5.28**（`k` 个抄来的词基本全中） |

**结论**：通道没坏（control 100% 命中、排名 1.2），是**检索只用词面**：
没有共享实词时它仍然返回 10 条，但返回的是**全局权重高的常客**，与被检索的记忆无关。

### 2.4 复现性：问法确实没抄词

| 指标 | natural | control |
|---|---|---|
| 问法与源记忆的**中文二元组重合率**（均值） | **0.030**（max 0.154） | **0.558** |
| 每条问法平均列出的"故意避开的原文词" | **12.9 个** | 0（允许照抄） |

→ 实验组确实做到了"不共享原文词"，对照组确实做到了"抄词"。**组间差异来自问法，不来自别的变量**。

### 2.5 对照组的失败也有信息（6 个 partial）

抄词也可能不是第 1 名——6 条 partial 的返回里，**近重复记忆**排在目标前面（见 5.6/5.7）：
`#3162` 被同为"say hello 类 epic goal"的 `#3146`/`#3158` 夹在中间（rank=2），
`#1703` 被 `#5093`（同讲 worktree UU/merge 冲突）压过（rank=2）。
这是**近义/近重复干扰**，与"抄不抄词"无关，属于独立的失败类。

### 2.6 剂量-反应：召回率是"共享实词数"的阶梯函数（本实验的归因核心）

为了把 `keyword-gap` 与 `too-general` 分开归因，又跑了**剂量-反应**实验：
取固定子集 **16 条**（同种子 `20261011` 的前 20 条中 `search_kw≥3` 者），
问法 = `自然问法 + 前 k 个抄来的 search_kw`（k=0,1,2,3），共 64 次搜索：

| k（补回的原文关键词数量） | n | 命中（进前10） | **rank=1** |
|---|---|---|---|
| **0**（纯自然问法） | 16 | 0（**0%**） | 0（0%） |
| **1** | 16 | 9（**56%**） | 0（**0%**） |
| **2** | 16 | 14（**88%**） | 0（**0%**） |
| **3** | 16 | 16（**100%**） | 0（**0%**） |

逐条看（`dose_response.json`）：`64`、`7185`、`19`、`3919`、`2186`、`1477` 等目标条
在 k=0 时**完全不可见**，补 1–2 个原文词后立刻进入前 2–5 名。

**两个结论，都很硬：**
1. **召回 = 词面桥的函数**。0 个共享实词 → 0% 召回；3 个 → 100% 召回。这不是"排序偏好"，
   而是**当无共享词时目标根本不在候选集里**（`keyword-gap` 是**主因**，不是措辞问题）。
2. **即使补满词面桥，目标也几乎不可能是第 1 名**（k=1..3 全部 rank=1 为 0%）。
   要靠抄词拿到 rank 1，需要**整串关键词**（对照组平均 `tokens=5.28` 才拿到 85% 的 perfect）。
   → **`too-general`（常客碾压）是独立的第二层缺陷**：`#1103`/`#1133`/`#1069` 这类高权重行
   在几乎每个查询里霸占前列。

---

## 2.7 根因（不是猜测：用 SQL 在真库上复现）

`internal/tools/memory.go:2085-2111` **Route 3（LIKE 子串扫描）**：

```go
// Route 3: LIKE substring scan on the query's SEGMENTED CJK words …
qargs := appendCJKLikes(terms, len(terms))
qargs = append(qargs, trigramRecallK)
//   SELECT … FROM distilled_qa d WHERE <likeCJKClauses> OR …
//   ORDER BY d.confidence DESC LIMIT ?        ← memory.go:2111
```

**机制（三步，全部实测）**：

1. 自然问法里的疑问词被分词器切成词（`怎么`/`什么`/`如何`/`该`…），这些词**在答案正文里到处都有**；
2. Route 3 命中**几千行**，取 `LIMIT N`，而排序依据**只有 `d.confidence`** —— 一个与查询毫无关系的信号；
3. 库里有 **70 行 `confidence=1.0`**（并列），SQLite 对并列行按 rowid 兜底 → 恒定的低 id 行霸占前列。

**在真库上用 Route 3 的原样 SQL 复现**（`mode=ro`）：

| 复现 SQL（去掉 LIMIT 前的相关性，逐字对应 memory.go:2107-2111 的 WHERE/ORDER） | 返回 top-10 |
|---|---|
| `… WHERE question LIKE '%怎么%' OR answer LIKE '%怎么%' OR topic LIKE '%怎么%' ORDER BY confidence DESC LIMIT 10` | `1069, 1133, 1136, 1138, 1139, 1140, 1146, 1157, 1168, 1174` |
| 本次演习 natural 组**跨 120 次查询**里出现频率最高的 id | `1103(118), 1133(118), 1069(116), 1140(112), 1146(110), 1139(101), 1118(90), 1168(68)` |
| 上面这批 id 的 `confidence` | **全部 = 1.0** |

→ **验尸结论**：natural 组返回的不是"和查询有点关系的记忆"，而是
**"confidence=1.0 且 id 最小的那批行"**——一个与查询无关的固定集合。
这解释了为什么 1189/1200 条命中 `tokens=0`、为什么 top-1 重复、为什么 120 个不同问法返回同一批 id。

**这是可用一句 SQL 验证的、确定的排序缺陷，不是"模型不懂语义"**：
Route 3 缺一个**相关性信号**（LIKE 命中词数 / 最佳命中位置），
在它补上之前，`confidence DESC` 把一条与查询无关的全局常量当成了排名依据。

---

## 3. 失败分类表（partial + fail → 可执行动作）

对 **126 条** partial+fail 逐条诊断（`analyze.py`，判据见下），类别可叠加：

| 类别 | 计数 | 判据（机器可判） | 可执行动作 |
|---|---|---|---|
| **keyword-gap 关键词缺口** | 63 | 问法与源记忆（question+search_kw）**零中文二元组重合** → 无词面桥 | 这不是"记忆写得差"，是**查询侧无桥**。动作：① 给高价值记忆**补 `search_kw` 同义/口语变体**（存的时候多写几个"别人会怎么问"）；② 检索侧引入**同义/向量召回**（另开任务）。 |
| **too-general 近义干扰/返回过泛** | 120 | 前 10 里 ≥5 条的 `tokens<=2`，且总返回 10 条 | 返回的常客（`#1103`/`#1133`/`#1069` 等）**在几乎每个查询里都出现** → 训练/存储侧的**全局高权重行淹没了召回**。动作：审查这些"万能命中"行是否 `tier` 过高或 `confidence` 过高；考虑对**零覆盖命中**不返回（宁可返回 3 条也不返回 10 条噪声）。 |
| **phrasing-rank 措辞导致排名丢失** | 57 | 有词面桥（共享二元组 >0）但目标未进前 10 | 措辞用了别的说法 → 动作同上①②；该类的存在说明**不是"完全无法关联"，而是关联信号太弱**。 |
| **near-dup 近重复记忆互相压过** | 6 | 目标未进前 10 或非第 1，而**第 1 名与目标的 question 二元组相似度 > 0.35** | 同一主题存了多条近重复行 → 动作：**合并/去重近重复记忆**（存储侧卫生），并给同族记忆加**互相区分的关键词**。 |
| **low-coverage 目标自身覆盖率低** | 1 | 目标 `tokens<=2` | 该记忆的 `search_kw` 太薄 → 补齐关键词。 |
| **unclassified 待人工判读** | 5 | 以上都不命中 | 人工抽样判读（见 `analysis.json:diagnoses`）。 |

> **类别之间的重要区分**：`keyword-gap` 是"**查询与记忆没有共同词汇**"（检索侧/存储侧关键词问题），
> `too-general` 是"**返回结果被常客占满**"（排序/权重问题），`near-dup` 是"**存储里有近重复**"（数据卫生问题）。
> 三者修复手段完全不同，不能合成一句"记忆搜不到"。

---

## 4. 可复跑配方（Rule 38）

```bash
# 环境（hosta）：sqlite3 CLI 未安装 → 用 python3 stdlib sqlite3，mode=ro
A=~/.gopi/sessions/01a1268a-e2cb-7f12-8404-19565c70d6c6/arena     # 本次 run 的 session 目录

# ① Prepare-抽样（固定种子，可复现；只读）
ARENA_OUT=$A python3 $A/arena.py sample --seed 20261011 -n 40
# ② Prepare-出题（40 次 LLM 调用；串行；thinking=disabled 见下）
ARENA_OUT=$A python3 $A/arena.py gen
# ③ Exercise（160 次 gopi memory search，串行，增量写 arena_log.jsonl，可断点续传）
ARENA_OUT=$A python3 $A/arena.py run
# ④ Feedback（诊断分类）
ARENA_OUT=$A python3 $A/analyze.py
```

**抽样 SQL（逐字）**：

```sql
SELECT id, question, answer, topic, search_kw, session_id, tier
FROM distilled_qa
WHERE length(answer) BETWEEN 200 AND 1600 AND question <> '' AND answer <> ''
ORDER BY id
-- 之后 random.Random(20261011).sample(rows, 40)   ← 随机种子 20261011
```

**本次实测量（可审计）**：

| 项 | 值 |
|---|---|
| 合格池 | **2867 条**（`length(answer) BETWEEN 200 AND 1600`） |
| 抽样 | **40 条**（种子 `20261011`） |
| **LLM 调用次数** | **40 次全部成功**（`llm_audit.jsonl`：`ok=40`） |
| **LLM 总耗时** | **62.9 s**（均值 1.6 s/次；`deepseek-flash`） |
| LLM 输入/输出规模 | 每条 prompt ≈1.5 K 字符；`max_tokens=2000`，`thinking=disabled` |
| **`memory search` 调用次数** | **160 次**（120 natural + 40 control），串行 |
| **`memory search` 总耗时** | **328.1 s**（均值 2.05 s/次） |
| 非零退出 | 0 |
| 总消息量/成本量级 | ≈40 × 1.5K 输入 + 少量输出；无需计费 API 之外的依赖 |

**LLM 调用坑（会毁掉整场实验，务必带 `thinking` 开关）**：
deepseek-flash 走 Anthropic 兼容端点时**默认开 thinking**，会把整个 `max_tokens` 花在思考上，
返回 `stop_reason=max_tokens` / `text=""`（实测 6000 tokens 预算跑满、耗时 2–6 分钟、正文为空）。
请求体必须带 `"thinking":{"type":"disabled"}` → 同一 prompt 0.6 s 返回、`stop_reason=end_turn`。
（已入库 memory id=8990。）

**语料B（指定 session）说明**：`62a7079b-3f59-4ce6-a0d5-ad5bef1a948e` 在
`~/.gopi/memory.db` 的 `distilled_qa` 里**一行都没有**（`session_id` / `sources` / `evidence` /
`question` / `answer` / `search_kw` 六个列 `like '%62a7079b%'` 全部 0 行，
`~/.gopi/sessions/62a7079b*` 目录也不存在——hosta 的 memory.db 是从别处同步来的）。
**故本次实验只用语料A（种子随机 40 条，来自 2867 条合格池）。**这是环境事实，不是省略。

---

## 5. ≥5 例逐条诊断（目标条 vs 实际返回条，含 `tokens=N`）

> `tokens=N` = 该命中覆盖了多少个**查询词**。引擎在 `N=0` 时**省略该字段**（实测确认），
> 下表 `tokens=0` 即原文无该字段。以下 `#id` 均为真实返回，命令均为逐字实跑。

### 5.1 `id=3484` [natural#0] grade=fail rank=0 hits=10
- **目标记忆** `#3484`｜`git blame internal/tools/task_finish_cleanup.go lines 740-752 — which commits last touched`
- **问法**：`这个源文件那十几行最近一次是谁改的？`
- **实测返回前 3**：
  - `#7562` tokens=**0**｜同步层（memsync）里想改"同一份工作"的身份判据…
  - `#783` tokens=**0**｜何时使用/如何调用 claude-multi-input-merge-pattern？…
  - `#747` tokens=**0**｜何时使用/如何调用 claude-21f634c5-quote-sourcing-dis…（被截断）
- **目标 tokens**：0
- **诊断**：`keyword-gap` + `too-general`。"blame / 那几行 / 谁改的" 与记忆里的 `git blame / lines / commits` 零中文二元组重合；返回的 10 条全部 `tokens=0`，是**与查询无关的常客**。

### 5.2 `id=3484` [natural#1] grade=fail rank=0 hits=10
- **问法**：`某段代码的修改记录该找哪几个人查？`
- **实际返回前 3**：`#7316` tokens=**0**（DeepSeek 服务降级卡死）｜`#1103` tokens=**0**（tasks 台账升级队列）｜`#1133` tokens=**0**（跨节点同步 brew lesson 行）
- **目标 tokens**：0
- **诊断**：同一个目标、换了个说法**依然 0 命中** —— 说明这不是单一措辞的偶然，**同一语义空间的不同入口都无桥**。

### 5.3 `id=8907` [natural#2] grade=fail rank=0 hits=10
- **目标记忆** `#8907`｜`要给一个只从外部进程读音频/数据的采集器加一个"从文件读"的测试/离线能力，怎么改最短、最不容易踩坑？`
- **问法**：`回放录音时下游按时间段切分的结果对不上，而且跑一半就报终止，问题出在哪？`
- **实际返回前 3**：`#7574` tokens=**0**（chat 重放真实录制会话）｜`#1103` tokens=**0**｜`#1075` tokens=**0**
- **目标 tokens**：0
- **诊断**：**语义能对上、词面对不上**的典型。`#7574`（重放录制会话）**主题高度相关**却仍 `tokens=0`——它被当作"没有词面匹配的常客"召回。这是本实验最有价值的一类：**召回理由与查询意图无关**。

### 5.4 `id=3826` [natural#0] grade=fail rank=0 hits=10
- **目标记忆** `#3826`｜`git blame internal/tools/task_staleness_guard.go lines 528-545 — which commits last touched`
- **问法**：`本地那个任务过期检查工具，五百多行那段代码最近是谁改的？`
- **实际返回前 3**：`#8701` tokens=**0**（HP 上跑 `git branch -a --contains <sha>`）｜`#1103` tokens=**0**｜`#1133` tokens=**0**
- **诊断**：`keyword-gap`。注意 `#3826` 记的是 **`git blame` + 文件路径 + 行号**，这是**工具使用型记忆**——它最该被"谁改的"问出来，实际 0 命中。**这类记忆在自然问法下等于不可检索。**

### 5.5 `id=6942` [natural#2] grade=fail rank=0 hits=10
- **目标记忆** `#6942`｜`要做多产品"实跑对照"（同探测文件喂多家 CLI agent，记录 verbatim 命令/原始输出/退出码/判定）该怎么做才不是编造？`
- **问法**：`一次会话里怎么安排几十次真机执行，才不会超时又不会被说作弊？`
- **实际返回前 3**：`#8112` tokens=**0**（chat_surface key 注入）｜`#1103` tokens=**0**｜`#1133` tokens=**0**
- **诊断**：`keyword-gap` + `phrasing-rank`。"作弊/真机执行" ↔ 原文"编造/实跑对照"无共享词面。

### 5.6 `id=3162` [control#0] grade=**partial** rank=2 hits=10
- **目标记忆** `#3162`｜`An epic goal is a literal non-engineering phrase like "say hello" — how should the agent handle it…`
- **问法（抄词）**：`epic goal 是 say hello 这类非工程短语，agent 怎么 handle 而不 fabricate deliverable？`
- **实际返回**：`#3146` tokens=**6**（rank 1）→ `#3162` tokens=**6**（rank 2）→ `#3158` tokens=**6**（rank 3）
- **诊断**：`near-dup`。**抄词命中、但被近重复记忆挤到第 2**：`#3146`/`#3158` 是同一主题的另两条。这是**存储侧近重复卫生**问题，与查询措辞无关。

### 5.7 `id=1703` [control#0] grade=**partial** rank=2 hits=10
- **目标记忆** `#1703`｜`How do I add tests to a package when a sibling unmerged branch adds a test file for the same…`
- **问法（抄词）**：`add/add merge conflict 时，sibling unmerged branch 加了同一个 test file，怎么用 worktree 做 throwaway merge 验证 duplicate helper names？`
- **实际返回**：`#5093` tokens=**10**（rank 1，UU entries 的 worktree）→ `#1703` tokens=**10**（rank 2）→ `#1704` tokens=**10**（rank 3）
- **诊断**：`near-dup`。目标覆盖率满分（`tokens=10`）仍非第 1，第一名 `#5093` 是"worktree 有 UU 条目"的近邻记忆。

---

## 6. 建议动作清单（按类分组，可直接转成任务）

### A. 存储侧：关键词缺口（对应 63 条 keyword-gap）
1. **给高价值记忆补 `search_kw` 口语变体**：存记忆时除了"主题词"，补 2–3 个"别人会怎么问这件事"的口语说法（"谁改的这段"↔`git blame`）。
2. **给工具使用型记忆加功能别名**：如 `git blame` → 追加 `谁改的, 修改历史, 哪次提交, 代码行历史`。

### B. 检索侧：无词面桥（结构性，需另开任务）
3. **同义/近义召回层**（词表或 embedding 二选一），或对 **0 覆盖命中**降权/不返回 —— 本次实测：natural 组返回的 1200 条命中里 1189 条 `tokens=0`，**约 99% 的返回是纯噪声**。
4. **返回长度自适应**：没有真实匹配时就返回 3 条而不是凑满 10 条（当前恒为 10 条）。

### C. 排序/权重（对应 120 条 too-general）——**根因已定位（第 2.7 节）**
5. **`internal/tools/memory.go:2111` Route 3 的 `ORDER BY d.confidence DESC` 缺相关性信号**：
   自然问法被分词后只命中 `怎么/什么/如何/该` 这类**全域高频词**，Route 3 于是返回
   "70 行 confidence=1.0 里 id 最小的若干行"——与查询无关的固定集合（已用原样 SQL 在真库复现）。
   **动作**：Route 3 排序改为按**相关性**（LIKE 命中词数 + 最早命中位置 / 覆盖率），
   把 `confidence` 降为并列时的次序；并考虑对 `tokens=0` 的命中**不返回**（当前恒返 10 条、99% 是噪声）。
   ⚠️ 这属于**记忆引擎改动**：本 run 只报告 + 单独提任务（见下），不在本分支改引擎。

### D. 数据卫生（对应 6 条 near-dup）
6. **近重复记忆合并**：question 二元组相似度 >0.35 的同族行合并/互相加区分关键词。

### E. 方法论（本次实验承认的边界）
7. 本实验**只测了 `question/answer/search_kw` 三列的词面**，未测 embedding；结论限定为"**在字面词面匹配的检索路径下**，不抄词 = 搜不到"。

---

## 7. Not Done（诚实）

- **语料B 无法做**：指定 session `62a7079b-…` 在本机 `memory.db` 里 **0 行**、sessions 目录不存在。已如实报告，未编造替代数据。
- **未跑更大样本**：受 60 min 预算限制，抽 40 条（120 natural + 40 control = 160 次搜索）。抽样池 2867 条，样本量足以定性质（0/120 vs 40/40 是极端分离），但**分类表的比例（63/120/57/6）受 40 条样本限制**，只应读作方向。
- **剂量-反应已补跑**（见 2.6，16 条 × 4 档 = 64 次搜索）：它把 `keyword-gap` 与 `too-general` 分开归因，**已不是缺口**。
- **未测 embedding/向量召回路径**：本实验只覆盖字面词面检索路径（见第 6 节 E.7）。
- **未修改任何引擎代码**：按约束（发现→报告+单独提任务）。**已单独提任务**：
  `gopi task 01a127d7-fdc3-7a07-a6c9-40589236fd8b`（tags=bug,memory,retrieval）——
  修 `memory.go:2111` Route 3 排序缺相关性信号，附复现 SQL + 修前基线 + 验收口径（重跑本演习的数字对照）。
  **报告本身不在本分支改引擎**，因此本分支 `git status` 干净的结论成立。
- 第 6 节其余三类（A 补 `search_kw` / B 同义召回或零覆盖不返回 / D 近重复合并）**未单独提任务**，
  以清单形式交付，可按需转成任务。

---

## 8. Verification

- **Build**: 未改 Go 代码，N/A（本任务不产出 Go 变更）。
- **Tests**: N/A（同上）；`git status --porcelain` 只含本报告。
- **Ran**（逐字，真实执行）：
  - `ARENA_OUT=$A python3 $A/arena.py sample --seed 20261011 -n 40`
    → `sampled 40 of 2867 eligible rows (seed=20261011) -> sample.json`
  - `ARENA_OUT=$A python3 $A/arena.py gen`
    → `gen done: 40 LLM calls, 62.9s`（`llm_audit.jsonl` 40 行全 `ok:true`）
  - `ARENA_OUT=$A python3 $A/arena.py run`
    → 160 行 `arena_log.jsonl`，0 非零退出，搜索总耗时 328.1 s
  - `ARENA_OUT=$A python3 $A/analyze.py`
    → natural `{n:120, perfect:0, partial:0, fail:120}` / control `{n:40, perfect:34, partial:6, fail:0, mean_rank_when_hit:1.2}`
  - 逐字抽验（人肉复跑单条）：`gopi memory search "回放录音时下游按时间段切分的结果对不上…" --limit 10`
    → 10 条命中，前面数条**无 `tokens=` 字段**（=0 覆盖），目标 `#8907` 不在其中
- **Result**: 见第 2 节三联数字；"存了搜不到" = 100%（40/40 条记忆），对照组命中率 100%（40/40）。
- **Commit**: 见第 10 节（本报告提交于 `task/01a1268ae2cb7f12840419565c70d6c6`）。

## 9. Evidence (self-filed)

| 声明 | 我实际看到的东西 |
|---|---|
| natural 组 0/120 | `arena_log.jsonl` 中 `arm=natural` 共 120 行，`grade` 全部 `fail`，`rank` 全部 `0` |
| control 组 40/40 | 同文件 `arm=control` 共 40 行：`perfect=34, partial=6, fail=0`；`rank` 分布 1 与 2 |
| 返回恒为 10 条 | `Counter(r["n_hits"] for r in natural) == Counter({10: 120})` |
| ~99% 命中 `tokens=0` | natural 组 1200 个命中行中 1189 个 `hits_tokens` 取值 0（引擎 0 覆盖时省略该字段；已在 /tmp 手跑单条命令确认返回行确无 `tokens=`） |
| 组间差异来自问法 | `questions.jsonl` 的 avoided 词平均 12.9 个/问法；natural 与源记忆的中文二元组重合率均值 0.030 vs control 0.558 |
| 未写 memory.db | 所有查询连接为 `file:...?mode=ro` + `PRAGMA query_only=ON`；全程无 `INSERT/UPDATE/DELETE`（`arena.py`/`analyze.py` 只有 SELECT） |
| 语料B 为空 | 6 个列 `like '%62a7079b%'` 各 0 行；`ls ~/.gopi/sessions/62a7079b*` → No such file or directory |

## 10. 文件清单

| 文件 | 位置 | 说明 |
|---|---|---|
| `devdoc/memory-arena.md` | **worktree（入库）** | 本报告 |
| `arena.py` | session 目录 `~/…01a1268a…/arena/` | 三段式可复跑脚本（Rule 39：可再生产物不入库） |
| `analyze.py` | 同上 | 诊断分类器 |
| `sample.json` / `questions.jsonl` / `arena_log.jsonl` / `analysis.json` / `llm_audit.jsonl` / `dose_response.json` / `diag_cases.md` | 同上 | 原始证据（种子、问法、逐条判定、LLM 审计） |
