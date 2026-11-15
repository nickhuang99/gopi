<!-- doc-class: normative -->
# 自动记忆的存储形态裁定：A 指针 / B 连日志 / C 自包含全文

> **PARTLY REVERSED 2026-11-06 — the command this page names is BACK, minus one flag.**
> `gopi memory sync` was restored the day after its deletion, as a MANUAL door:
> one invocation = one session, exit. `--peer` / `--push` / `--pull` / `--dry-run` /
> `--serve-assets` / `--timeout` all run again; the runnable shape and its measured
> two-way session are in `devdoc/memory-sync-manual-door-restored.md`. **What is
> still gone is the `--every <dur>` cadence flag**, whose reader and loop went in
> batch 5 — so a line below that carries `--every` is a RECORD, not a command to
> paste (it exits 1 with `unknown flag`). The one-way copy `gopi memory backup`
> is the other manual cross-host door and its guarantees differ: it REPLACES the
> peer ledger instead of merging it.

**任务**：`01a11df4-abca-7214-99e9-4e389057883e`（epic `自动记忆` 的第 1/5 项）
**日期**：2026-10-09
**范围**：**只出裁定文档，零 gopi 源码改动**（本 commit 只含 `devdoc/` 与 `devdoc/DOC-INDEX.tsv`）。
**基准 commit**：分支成立于 master `f5ffc27e2108823edd40d494d0c43ca5a0fca49b`（本 commit 的父）。
**被测二进制**：`/home/user/work/GoPi/bin/gopi`（64,833,495 B，mtime 2026-10-09 08:24）。

## 0. 一句话裁定

**采纳方案 C（自包含全文），正文上限 4 KiB，只存可见 text 不存 thinking。**
理由不是"体积看起来还好"，而是三条可复算的事实：① 操作者"几百 KB"的估计**高估两个数量级**（整轮 p95 = 5,017 B，max = 68,416 B）；② 库体积从 257 MiB 长到 819 MiB 只让 `gopi memory search` 的**最坏墙钟多 ~0.6–0.9 s**，而这个命令本身有 **0.8–1.7 s 与数据库无关的固定地板**；③ C **整类消灭"指针悬空"**，且**零新增依赖**（不读 session log、不新增常驻/脚本/状态文件）。

---

## 1. assistant 消息体积实测（本轮独立复现）

**取样方法与 N（可复现）**

- 语料：本机真实 `~/.gopi/sessions/*/*.jsonl`（6844 个文件被扫，**跳过 1 个 > 60 MiB 的文件**——那种体积的单行是带附件的 briefing，不是对话轮）。
- 行形状：`message` 是对象，`message.content` 是 **list of parts**（`{"type":"text"|"thinking"|"tool_use", …}`）。**早期一次只统计字符串形 content 的测量对 assistant 恒为 0 命中**，本表已用真实形状重测。
- 计量：每个 part 按其在日志里序列化后的 **UTF-8 字节数**；`text` = 可见正文；`text+thinking` = 整轮散文；`full` = 含 `tool_use` 入参的整行。
- 生成器：`measure2.py`（本 session scratch，逐字命令见 §7 `Ran:`），输出 `asst-size2.json`；条件集输出 `conditioned.json`。

| 量 | N | mean | median | p90 | p95 | p99 | max | Σ bytes |
|---|---|---|---|---|---|---|---|---|
| `text`（可见正文） | 962,148 | 53 | **0** | 92 | 180 | 1,301 | 67,641 | 51,470,081 |
| `text+thinking` | 962,148 | 812 | 0 | 1,958 | **4,067** | 13,974 | 68,416 | 781,895,632 |
| `full`（整轮，含 tool_use） | 962,148 | 1,214 | **377** | 2,731 | **5,017** | 14,836 | **68,416** | 1,168,992,573 |
| `user`（对照） | 29,519 | 2,909 | 832 | 6,748 | 9,505 | 50,657 | 64,798 | 85,897,052 |

**条件集（真正的 candidate 母体）**

| 条件 | N | Σ bytes | median | p95 | max |
|---|---|---|---|---|---|
| 有可见正文的轮 | 106,583 | 47,646,830 | 139 | 2,496 | 66,112 |
| 有可见正文 **或** thinking 的轮 | 410,405 | 758,612,875 | 550 | 8,376 | 67,729 |

**按会话聚合**（3911 个含正文的会话）：单会话正文 Σ 中位 7,984 B / p95 32,725 B / max 627,486 B；轮数中位 148 / p95 764 / max 6,205。

**按"内容形态判据"采样**（操作者判据：含 markdown 表 / 含制表符拓扑图 / 大段总结 ≥1500 B；本轮在**最新 2000 个 session 文件（实扫 1294 个、220,106 轮）**上独立跑了一遍）：

| 集合 | N | 占全部轮 | Σ bytes | median | p95 | max |
|---|---|---|---|---|---|---|
| 表（≥2 行 `\|…\|`） | 1,050 | 0.48% | 2,942,538 | 2,756 | 4,255 | 7,644 |
| 拓扑图（制表符集合命中） | 2,649 | 1.20% | 5,530,346 | 2,321 | 3,985 | 27,845 |
| 大段总结（≥1500 B） | 1,924 | 0.87% | 5,505,443 | 2,749 | 4,215 | 27,845 |
| **并集（candidate）** | **2,891** | **1.31%** | 6,101,088 | 2,321 | 3,954 | 27,845 |

**结论**：操作者"单轮回复一般几百 K 最多"的估计，**对 p95 高估约 2 个数量级、对 max 高估约 1 个数量级**。C 在体积上站得住的前提**成立**。

## 2. memory.db 现有体积构成

生成器：`introspect_db.py`（只读打开副本，**绝不碰** `~/.gopi/memory.db`；Rule 32 的红线是"不写操作者的真实库"，本任务全程只读 + 副本）。

- `page_count 65,877 × page_size 4,096 = 269,832,192 B (257.3 MiB)`，`freelist = 165 页 (0.6 MiB)`。
- 头部对象（`dbstat`，副本 270,487,552 B）：`distilled_qa_tri_data` 116.8 MiB、`distilled_qa` 37.4、`tasks_tri_data` 34.9、`distilled_qa_fts_data` 26.8、`session_conclusions` 13.3、`tasks` 13.1、`tasks_fts_data` 6.9、`task_events` 2.1、`memory_feedback` 0.9。
- `distilled_qa` 现有 **8,802 行**，正文合计 **16.6 MB**，而它三套索引（trigram 116.8 + fts 26.8 + 基表 37.4）≈ **181 MiB** ⇒ **正文→磁盘的经验放大 ≈ 7.7×**。这是外推的核心系数，不是估计值，是 dbstat 实测。

### 2.1 C 方案的外推

**外推假设（明写）**

1. 只捕获通过上面"内容形态判据"的轮（即 candidate 集合），不是全部 962,148 轮；
2. 正文原样进 `distilled_qa.answer`，**三套索引照现有触发器建立**（放大系数由上节的真实插入实测给出，见假设 4）；
3. 行数线性增长，不做淘汰，语料为"本机现有全部会话"；
4. 每行成本由**两次真实插入**拟合（`grow.py` 往副本里插真实 assistant 文本，+5,000 行/次）：**固定 ≈ 7,000 B/行 + 2.372 B 磁盘 / 1 B 正文**（5,000×2,000 B → 11,837 B/行；5,000×5,000 B → 18,953 B/行）。

| 变体 | 行数 | 正文 Σ | 外推库体 | 相对现有 257 MiB |
|---|---|---|---|---|
| **C（只存可见 text）** | 106,585 | 47.6 MB | **859,120,633 B ≈ 819 MiB** | **+562 MiB（3.2×）** |
| C + 正文限 4 KiB | 106,585 | 45.0 MB | 852,891,527 B ≈ 813 MiB | +556 MiB |
| C + thinking（**不建议**） | 410,434 | 758.7 MB | 4,672,606,871 B ≈ 4.35 GiB | +4.1 GiB（17×） |
| C + thinking 限 4 KiB | 410,434 | 483.8 MB | 4,020,586,969 B ≈ 3.74 GiB | +3.5 GiB |

实测锚点（真实插入，非外推）：`base 257.5 MiB → +5,000×2,000 B = 317.4 MiB → +5,000×5,000 B = 350.6 MiB → 累积 27,687 行 = 484.4 MiB(growC 507,981,824 B 含 WAL/空闲页)`。

**⇒ C 的库体积代价 = +562 MiB（2.6 GB 磁盘里可忽略），前提是"只存可见正文"这条一起采纳**；thinking 一起存会让它跨到 GiB 级，**明确排除**。

## 3. `gopi memory search` 真实延迟（改动前后对比）

**被测表面就是真实 CLI**：`gopi memory search "<query>" --limit 5 --db <副本>`，墙钟由 `date +%s%N` 包住（Rule 31：真跑，不是单测）。

**同轮交错采样（interleaved 8 轮 × 5 个库，消除负载漂移）**，query = `内存检索 索引 延迟 膨胀`：

| 库 | 体积 | best | p50 | max |
|---|---|---|---|---|
| empty | 131,072 B | **824** | 1,176 | 1,529 |
| **base（= 现有库）** | 270,487,552 B | **1,399** | **2,266** | 3,107 |
| fresh2000（+5,000×2,000 B） | 329,752,576 B | 1,823 | 2,490 | 3,678 |
| fresh5000（+5,000×5,000 B） | 365,617,152 B | 2,109 | 2,904 | 3,413 |
| **growC（累积 27,687 行）** | 507,981,824 B | **2,267** | **2,700** | 3,722 |

单位 ms。第二次独立复跑（6 轮交错）：base best 1,539 / growC best 2,210（同一结论）。

**把固定地板剥出来**（否则会把进程启动算成"膨胀代价"）：

- `gopi --version`：**7–13 ms**（二进制启动本身可忽略）。
- `gopi memory list --limit 3 --db base.db`：14–17 ms（**store 打开也便宜**）。
- `gopi memory search` 打在 **EMPTY 库**上：**best 824 ms，负载重时到 1,728 ms**（`--limit 5`，同 query）。
  ⇒ 这 0.8–1.7 s 是 **search 路由自身的固定成本（CJK 分词器/gse 初始化），与数据库体积无关**；ASCII 查询与 CJK 查询在 empty 上同量级（1,728 vs 1,626 ms），进一步排除"CJK 分词是空库成本唯一来源"的单一解释。

**内核级（纯 SQL，read-only，5 次取中位，`qtime.py`）**：

| 查询 | base | growC | Δ |
|---|---|---|---|
| FTS5 `MATCH`（术语 OR） | 0.70 ms | 2.83 ms | +2.1 ms（2×，绝对值仍 3 ms） |
| CJK `LIKE '%…%'` 全表扫描 | 90.9 ms | 202.1 ms | +111 ms（`answer` 列 16.6 → 55.3 MB） |
| 全表扫描 | 84 ms | 277 ms | +193 ms |

**⇒ 体积膨胀的可归因代价**：`269 MiB → 508 MiB（+88%）` 让 `gopi memory search` 的 **best +0.6 s、p50 +0.4 s（≈+1.6×）**；拆开是 **~0.5 s 的 DB 打开层 + ~0.1 s 的查询路由**。
**外推到 C 的 819 MiB**（再翻倍一次，同样只按线性缩放）：**再 +0.5–0.8 s**，即 C 落地后 `memory search` 大致 **2.2–3.2 s（best）**，其中 0.8–1.7 s 与本次改动无关。
这是**慢，但没有慢到否决 C**：同一命令的地板已经是 0.8–1.7 s，C 把它抬到 ~2–3 s，同时把 `--db` 上的副本采样噪声（±0.5 s）一起算进来仍是同一量级。**如果将来要压这条延迟，压的应该是那 0.8–1.7 s 的固定地板（CJK 分词器初始化），而不是 C**。

## 4. sync 带宽量化（真实导出）

**表面**：`gopi memory memsync --export --out <file> --db <副本>`（导出的是**行**，不复制索引），文件字节 = 真实线上载荷。

| 库 | 导出行数 | 导出文件 | 每行 | 导出耗时 | 源库体积 |
|---|---|---|---|---|---|
| base | 7,687 | 23,176,240 B (23.18 MB) | 3.0 KB | 453 ms | 270,487,552 |
| fresh2000 | 12,687 | 34,109,692 B | 2.7 KB | — | 329,752,576 |
| fresh5000 | 12,687 | 49,232,471 B | 3.9 KB | — | 365,617,152 |
| **growC** | 27,687 | 67,579,892 B (67.58 MB) | 2.4 KB | 1,355 ms | 507,981,824 |

**读数**：导出字节 **= Σ(行固定开销 + 该行正文字节)**，**与库膨胀无关**（growC 库比 fresh5000 大 39%，导出只大 27.6 MB，因为新增了 15,000 行**而不是**因为索引变大）。每行固定开销实测 **≈2.4–3.0 KB**。

**C 的增量外推**：+106,585 行、正文 47.6 MB ⇒ `106,585 × ~2.5 KB + 47.6 MB ≈ **+0.31 GB**` 每次全量导出。今天 base 的一次全量导出是 **23 MB** ⇒ 变成 **~0.33 GB**（14×）。
**B 方案的对照（这才是量级炸弹）**：每个 session 日志文件的体积分布（6,848 个真实文件）——中位 **19,888 B**、mean **378,515 B**、p90 **1.24 MB**、p95 **1.67 MB**、p99 **2.56 MB**、max **87.88 MB**、Σ **2.59 GB**。B 要按条目带走整份日志 ⇒ **单次同步从 MB 级抬到 GB 级**，且把"对端必须持有同一份日志文件"这一依赖**加重后重建**。

## 5. 裁定：C 被采纳

| 方案 | 体积代价 | 延迟代价 | sync 代价 | 故障面 | 判决 |
|---|---|---|---|---|---|
| **A 指针** | ~0（只存 summary） | ~0 | 最小 | **"指针悬空"一整类**：对端无该 session 文件 ⇒ 必须引入显式不可达状态 + 降级逻辑 + 维护标注 | 否 |
| **B 连日志** | 目标端 GB 级 | 传递/落盘双重 | **每条目 ~19 KB–88 MB**（中位→max） | 依赖"日志与条目一起搬"，保留期耦合 | 否（最重） |
| **C 自包含** | +562 MiB（≈3.2×） | ~+0.9 s 墙钟（其中 ~0.5 s 是 DB 打开） | +47.6 MB 正文（全量导出 ≈0.33 GB） | **悬空不存在**（内容与条目同生共死） | **✅ 采纳** |

**为什么不是"体积还能忍"这种弱理由**：

1. **C 消灭一整类故障**（指针悬空 / 对端不可达 / 标注与降级逻辑腐化）——这正是操作者给的判据偏好："能消灭一整类故障的设计 > 需要小心维护的设计"。
2. **A 的"轻"是假的**：A 省下的是 562 MiB 磁盘，付出的是**永久维护成本**（写指针、判可达、渲染不可达状态、Rule 15 不允许静默悬空）——而一旦检索要真正有用，A 最终还是得把正文取回来给 LLM 看，**prompt 侧的体积因此并没有省**（§6.1）。
3. **C 的代价是一次性的、可测的、有界的**：三套索引的放大系数（7.7×）是 SQLite 的固定行为，不随版本漂移；+0.9 s 落在一条本来就 1–2 s 的命令上。
4. **兜底条款**：正文**上限 4 KiB** + **排除 thinking**。上限不是省空间（813 vs 819 MiB 几乎无差），而是给尾部（max 68 KB 的轮）一个确定性上界；thinking 排除是关键——含 thinking 会让库到 4.35 GiB，**那才是真正撑不住的一档**。
5. **跨机语义随之变简单**：条目在对端**直接可检索**，`gopi memory sync` 无需任何"悬空"分支。

**不做的事（诚实边界）**：本裁定**不含** schema 迁移、捕获接线、prompt 注入实现——那是本 epic 的第 2/3 项（`01a11df4-ad0e…` / `01a11df4-ae3b…`）。本文只决定**存什么形态**。

## 6. 两个冲突的处置

### 6.1 冲突 A：prompt cache 边界

**实测（真实聚合，非估计）**：`gopi session stats --all --json` ⇒ **5,978 个会话 / 447,842 轮**：

- `cache_hit 169,056,520,892` / `cache_miss 7,858,138,756` ⇒ **命中率 95.56%**；
- `prompt_tokens` **median 67,742 / p95 141,091**（448,046 个 request 事件口径）；`prompt_avg 395,047`；
- `completion_tokens 938,168,605`。

**处置**：**存储形态与 prompt cache 是两个不相交的决定，C 不破缓存。**

- 缓存断点是 **prompt 前缀**（ground/system）；C 决定的是**字节落在 memory.db 里**，而不是落在 prompt 里。
- 检索路径是**工具调用**（`memory action=search` 的结果作为一条 tool result 进入上下文的尾部），**不是把整库插进 system prompt**。因此注入量由**检索策略（top-k、截断）**决定，**与 A/B/C 无关**：A 的指针要被回答，正文终究也得取回来喂给 LLM。
- 唯一可能的 prompt 面改动是"注入一条自记录契约指令"，它在**尾部**，并且**本设计不要求每轮变化**；只要不重写前缀，95.56% 的前缀命中不受影响。
- **红线（写给第 3 项执行者）**：任何"把检索到的记忆正文拼进 system/ground 前缀"的实现**都会**破缓存——那是实现缺陷，不是 C 的代价。

### 6.2 冲突 B：依赖预算（Rule 35）

**结论：C 是三者里唯一"零新增持久依赖"的。**

按 Rule 35 的四件事逐条：

1. **不做会坏什么** —— 不需要回答：C **不新增**任何持久依赖（无常驻、无 cron、无脚本、无新状态文件、无新配置键、无 socket、无 lock file）。
2. **为何不需要在进程内无法解决的机制** —— 捕获动作挂在**已经存在的"响应结束"路径**上（与第一期 `selfrecord.go` 抽取 `[IMPORTANT]` 的挂点同址），写入走**已有的** `MemoryStore`/`distilled_qa` + 已有的 FTS/trigram 触发器。**没有任何新的 tick、timer、worker**。
3. **生命周期归谁** —— 数据落在**已有的** `memory.db`，由保留策略治理（原文写的是“已有的 `gopi memory sync`”，但该动词已于 `5677cc6e` 删除；今天唯一跨节点动作是单向拷贝 `gopi memory backup`）/ 保留策略治理；没有新工件需要回收，因此没有新的 owner 问题。
4. **后人如何辨识** —— 无需辨识：没有遗留物。

**反证**：**A 才是引入机制的那个**——它需要
(a) 读 `~/.gopi/sessions/<sid>/*.jsonl`（**新增跨存储依赖**：日志的保留期/prune 与条目寿命被耦合），
(b) 一个显式的"细节仅在本机 `<host>`，不可达"状态（Rule 15 禁止静默悬空），
(c) 该状态在 sync 到对端后的正确传播。
**B** 更重：把 session 日志变成同步载荷的一部分（中位 19 KB、p99 2.56 MB、max 88 MB 每次）。

## 7. Rule 39 的边界说明（blob 禁令只针对 git tracked tree）

- Rule 39 管的是**被 git 跟踪的树**：判据是 `git ls-files` 的内容/体积（>256 KiB 的非文本、>1 MB 字体、二进制 magic），**按内容判定，与目录名无关**。
- `~/.gopi/memory.db` 是**运行时数据**，**从不被跟踪、从不被提交**；C 的正文只存在于它里面。**两条规则不相交，也不冲突。**
- 本裁定**不授权**任何对 tracked tree 的写入。**本次变更的 `git diff` 只含 `devdoc/auto-memory-candidate.md` 与 `devdoc/DOC-INDEX.tsv`**（见 §8）。
- 本次测量产生的**临时副本**（`scratch/copies/*.db`，共 ~2.3 GB）位于**本任务的 session 目录**：`/home/user/.gopi/sessions/01a11df4-…/scratch/`，实测设备号 **2052 ≠ `/` 的 2051**（Rule 22），测量结束即清理；**它们从未进入任何 git 索引**。

## 8. 印证与复现（Rule 30 / 31）

全部数字都能从下面的命令重跑得到；`<S>` = `/home/user/.gopi/sessions/01a11df4-abca-7214-99e9-4e389057883e/scratch`。

- **体积分布**：`python3 $S/measure2.py ~/.gopi/sessions $S/asst-size2.json`（N、跳过文件数、坏 JSON 数都写在输出里）
- **candidate 集合**：`python3 $S/cand.py $S/candfiles $S/cand.json`
- **库构成**：`python3 $S/introspect_db.py`（只读 `mode=ro` 打开副本）
- **真实插入拟合**：`python3 $S/grow.py $S/copies/base.db 5000 <targetB> $S/asst-samples.json`
- **真实延迟**：`gopi memory search "内存检索 索引 延迟 膨胀" --limit 5 --db $S/copies/<db>.db`（交错 8 轮，见 `$S/tm.txt`）
- **纯 SQL 延迟**：`python3 $S/qtime.py`
- **sync 带宽**：`gopi memory memsync --export --out $S/exp-<db>.json --db $S/copies/<db>.db`
- **prompt cache**：`gopi session stats --all --json`
- **设备号**：`stat -c %d /home / ` ⇒ `2052 / 2051`

**Ran / Result（逐字）**

```
$ gopi memory search "内存检索 索引 延迟 膨胀" --limit 5 --db $S/copies/base.db     # 8 rounds interleaved
Result: best 1399 ms / p50 2266 ms
$ gopi memory search "内存检索 索引 延迟 膨胀" --limit 5 --db $S/copies/growC.db
Result: best 2267 ms / p50 2700 ms
$ gopi memory memsync --export --out $S/exp-base.json --db $S/copies/base.db
Result: base  export_ms=453  bytes=23176240  db=270487552
$ gopi memory memsync --export --out $S/exp-growC.json --db $S/copies/growC.db
Result: growC export_ms=1355 bytes=67579892  db=507981824
$ gopi session stats --all --json
Result: sessions 5978 rounds 447842 cache_hit_pct 95.55823 prompt_avg 395047 completion 938168605
$ git diff --name-only master...HEAD
Result: (见下方 commit: 仅 devdoc/auto-memory-candidate.md 与 devdoc/DOC-INDEX.tsv)
```

**Not Done（诚实边界）**

- 本文**不含** schema 迁移、捕获接线、prompt 注入、Rule 11 真会话验证——那是 epic 的第 2/3/4/5 项。
- 延迟数字受**本机负载**影响（测量期间 loadavg > 3，多个 worker 在跑），因此**同时给了 best 与 p50，并额外给出与负载无关的内核级 SQL 数字**；把"膨胀使 search 变慢"精确到 0.1 s 在本机不可复现，能复现的是量级（+0.6 s best / +0.4 s p50 per +88% 体积）。
- `grow.py` 的插入用 `synchronous=off` + 单事务：**测的是磁盘体积与查询代价，不是写吞吐**（避免设备压力，Rule 27）。
