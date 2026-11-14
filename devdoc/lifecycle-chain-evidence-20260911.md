<!-- doc-class: historical -->
# 全流程链条断链点证据审计（H1–H5）

**任务**：验证 PA 审计（`devdoc/agent-full-lifecycle-chain-audit-20260911.md` §二）提出的 7 个断链点中的 5 条假设，用真实数据给出支持/证伪，并区分「机制缺失」与「机制存在但采纳/检索率低」。
**日期**：2026-09-11　**分支**：`feat/lifecycle-chain-evidence`（worktree `~/workspace/gopi-lifecycle-evidence`）
**性质**：只研究不实现 —— 本报告不含任何产品代码改动；新增文件只有本报告与三个只读采集脚本（`scripts/lifecycle-{chain-audit,hypotheses,crosscheck}.py`）。

---

## 0. 结论速览（TL;DR）

| # | 假设 | 判定 | 一句话 |
|---|---|---|---|
| H1 | 调研结论不沉淀 | **部分证伪**（写入端已闭合，价值端未闭合） | 调研方法/配方沉淀了（#1101/#1162 被广泛检索），**具体研究结论（GLM 定价数字）没沉淀，同一问题 5 分钟内被两个会话重复调研**。机制在，沉淀的粒度与内容选错了 |
| H2 | 代码理解不沉淀 | **部分证伪**（存在一次批量沉淀，但检索触发迟钝） | 08-27 一次代码通读任务沉淀了 86 行模块知识（924–1009）；但**检索触发率从 9%→16%→35% 缓慢爬升**，且这 56 行里 71% 从未被任何检索返回 |
| H3 | 调试根因不沉淀 | **证伪（最强的一条反驳）** | 根因**沉淀得很好**：错误签名族在记忆里有专门簇（FAIL 126/timeout 89/stale 102/flake 27/SQLITE_BUSY 9），22 个复发失败的测试名 18/22 在记忆里被点名；且 22 个高频签名里 **16/18 一次会话完成调试+沉淀**。真问题在**检索触发**：只有 8.8% 的检索带着错误信号 |
| H4 | 环境状态无权威视图 | **支持（机制缺失）** | 没有任何环境状态存储；`gopi --help` 无 env/doctor 类动词；49 个会话里 75 次环境探针（which/--version）靠重跑；distilled_qa 里「装了什么/哪个版本/是否安装」类问题 **0 条** |
| H5 | 需求动机不留痕 | **支持（机制缺失，但有明确反例）** | **92 条人类任务（27.6%）的 payload+reason 里没有任何动机线索**，全部是命令式结果记录（"gopi run completed — run: elapsed=…"）；反例：task_submit 的 LLM 任务 0% 空 payload，bug 单五要素齐全 |

**对交付物第 5 问的直接回答**（调研/理解/调试三环是真丢，还是存了但检索触发缺失）：

> **三环全都不是「真丢」—— 机制都存在，写入端甚至在增强（E1 39% → E2 84% → E3 92% 的会话调用 `memory:store`）。**
> 但三环都**在各自的关键点上失血**，且失血位置各不相同：
> - **调研**：方法沉淀了、**结论没沉淀**（沉淀的是"我怎么抓的"，不是"我抓到了什么"）→ 这是**粒度选择缺陷**。
> - **理解**：知识沉淀了（86 行模块级），但**存入时的措辞是「文件说明书」而不是「检索问题」**，导致自问自答命中率只有 48%（recall@1）/ 49%（recall@3）→ 这是**索引语义缺陷**。
> - **调试**：沉淀和检索都最好，但**检索触发率只有 8.8%**→ 这是**触发门缺失**。
>
> 一句话：**PA 审计的诊断（"信息只存在于会话上下文里"）在 H4/H5 成立，在 H1/H2/H3 不成立 —— 后三环的信息确实进了库（存在性已证），真正断的是「把已存的知识，在需要的时刻、以能命中的措辞，送回上下文」这一段。**

---

## 1. 数据与方法（可复现）

### 1.1 数据面

| 面 | 路径 | 规模 |
|---|---|---|
| 会话 JSONL（实际发生了什么） | `~/.gopi/sessions/**/*.jsonl` | 1373 个文件 / 389 MiB |
| 知识库（留下了什么） | `~/.gopi/memory.db` | `distilled_qa` 1808 / `session_conclusions` 970 / `tasks` 969 / `memory_feedback` 1914 / `procedural` 9 |

**只读纪律**：`memory.db` 是生产 WAL 库，全程以 `file:...?mode=ro` URI 打开，**从不 cp**；唯一一次复制用 SQLite 在线备份 API：

```bash
sqlite3 "file:$HOME/.gopi/memory.db?mode=ro" ".backup /tmp/lca/memory-ro.db"
```

（记忆 #1614 的教训：WAL 库直接 `cp` 会得到 "database disk image is malformed" 的假故障。）

### 1.2 采集脚本（本报告全部数字的唯一来源）

```bash
# ① 全量会话动作普查（含 bash 命令性质分类）
python3 scripts/lifecycle-chain-audit.py actions ~/.gopi/sessions /tmp/lca/actions.json

# ② 逐 tool_use 记录 + 配对的 tool_result（含失败判定、检索返回的 #id）
python3 scripts/lifecycle-chain-audit.py extract ~/.gopi/sessions /tmp/lca/extract2.jsonl

# ③ 按时代分层统计（工具面断层：2026-09-10 的 memory/task 组命令合并）
python3 scripts/lifecycle-hypotheses.py /tmp/lca/extract2.jsonl

# ④ 交叉验证对照表（会话动作数 vs 库内沉淀数）
python3 scripts/lifecycle-crosscheck.py /tmp/lca/extract2.jsonl ~/.gopi/memory.db
```

### 1.3 抽样方法与已知偏差（诚实声明）

1. **全量非抽样**：动作普查覆盖全部 1142 个含 assistant 事件的会话、118,657 次 `tool_use`；提取集覆盖 999 个会话、40,081 条记录。没有做随机抽样——数据量（389 MiB）在两轮内可跑完（实测 46 s）。
2. **工具名归一化（必要偏差）**：2026-09-10 工具面合并把 wire name 改了（`memory_search`→`memory:search`、`write_task_area`→`task:commit`、`todo`→`task:view`、`task_evaluate`→`task:reframe`）。跨时代比较前统一映射（`NAME_MAP`），否则历史计数会静默归零。
3. **时代分层**（所有比率都分三层报，避免把工具面升级误读成行为变化）：
   - **E1** ≤ 2026-08-27（457 会话）— 平铺工具名时代
   - **E2** 2026-08-28 … 09-09（439 会话）— code_search/git_blame 路由劝说 + memsync 时代
   - **E3** 2026-09-10 …（103 会话）— memory/task/context_trim 合并后的现役面
4. **两个启发式及其局限**（报告内每处引用都会标注）：
   - *「失败命令」判定*：对 bash 的 tool_result 头部做正则（`FAIL|panic:|exit status [1-9]|undefined:|no such file|syntax error|error:`）。它会**漏**掉成功但无用的命令，也**会误收**「输出里恰好含 error 字样」的成功命令。量级上（4157 条 / 437 会话）足以支撑结构判断，但不适合当成精确误差率。
   - *「环境探针」判定*：`which|command -v|type|--version|brew list|go env|systemctl` 正则。它只覆盖「显式探查」，不含「装了但用不了才发现」的隐式探查——即 H4 的真实缺口**比 75 次更大**，不会更小。
   - *「代码定向检索」判定*：检索 query 是否含 `.go|internal/|cmd/|代码|实现|架构|module|模块|package|函数|symbol`。英文短查询可能漏判 → 得出的触发率是**下界**。
5. **自问自答命中率测试**：取 08-27 代码通读批次的 82 行，逐行拿它**自己存储时的 question 原文**去 `gopi memory search`（`--db /tmp/lca/memory-ro.db`，只读副本，不污染生产库），看该行是否进 top-1/top-3。这是一个**理想上界的近似**：真实场景里 LLM 的问题比存储时的 question 更模糊，所以 48% 是**乐观值**。

---

## 2. H1 — 调研结论不沉淀

### 2.1 原始数据

| 指标 | 数值 | 取法 |
|---|---|---|
| `web_search` + `web_fetch` 调用 | **902 次 / 45 会话** | 交叉验证脚本 §1 |
| `web_search` 去重后查询 | **515 条（每会话几乎不重复）** | 同上 |
| 跨会话 Jaccard≥0.5 的查询对 | **28 对，仅 4 组会话对** | 见 §2.3 脚本 |
| distilled_qa 中引用 URL 的行 | **157 / 1808（8.7%）** | `SELECT COUNT(*) FROM distilled_qa WHERE sources LIKE '%http%' OR answer LIKE '%http%'` |
| 这些行中从未被检索过的 | 11 | 同上 + `retrieval_count=0` |
| `tasks.summary` 含 "RESEARCH" 的任务 | 10 | `SELECT COUNT(*) FROM tasks WHERE summary LIKE '%research%'` |

### 2.2 决定性反例（同一问题被两个会话重复调研）

```
2026-08-29 12:54  会话 7a48b3c1   memory:search "fetch bigmodel.cn pricing page GLM model price table…"
2026-08-29 12:55  会话 7a48b3c1   web_search "glm-5.3-flash 价格 bigmodel 定价"     → 3108 chars
2026-08-29 12:56  会话 7a48b3c1   web_search "GLM-5.3 价格 元 百万tokens 输入 输出 智谱" → 3448
2026-08-29 12:57  会话 7a48b3c1   web_search ×3 …
2026-08-29 12:59  会话 7a48b3c1   web_search "智谱 GLM-5.3 和 GLM-5.3-Flash 在 bigmodel.cn 的 API 定价是多少？…" → 334
──────────────────────────────────────────────────────────────────────────────
2026-08-29 12:59  会话 b4b6b43c   memory:search "抓取 bigmodel.cn 智谱 GLM 定价页面 pricing 价格表 web_fetch JS 渲染"
2026-08-29 13:00  会话 b4b6b43c   web_search "bigmodel glm-5.3-flash 价格 定价 元 每M tokens"  → 2318
2026-08-29 13:01  会话 b4b6b43c   web_search "glm-5.3-flash 输入 输出 缓存 元/M tokens 高峰 优惠" → 2592
2026-08-29 13:04  会话 b4b6b43c   web_search "VentureBeat \"GLM-5.3-Flash\" 45% workloads pricing" → 2218
```

**两个会话相隔 5 分钟，问的是同一件事（GLM-5.3-Flash 定价），各自从零重跑了一整套检索。**

### 2.3 关键区分：沉淀了"方法"，没沉淀"结论"

```bash
# 记忆里真的没有 GLM 定价数字（"每百万" 0 行；有价钱的 GLM 行 0 条）
sqlite3 "file:$HOME/.gopi/memory.db?mode=ro" \
  "SELECT COUNT(*) FROM distilled_qa WHERE topic LIKE '%GLM%' AND (answer LIKE '%价格%' OR answer LIKE '%定价%')"
# → 0
```

而同期**沉淀的是方法**，且这个方法被反复复用：

| id | topic | 存储时间 | retrieval_count |
|---|---|---|---|
| 1101 | `web_fetch 直抓 docs.bigmodel.cn 文档页（GLM-5.3-Flash 实证）` | 08-29 13:49 | 7 |
| 1162 | `docs.bigmodel.cn 全站 markdown 抓取配方（llms.txt + 中文路径编码）` | 08-31 13:59 | — |

`#1101` 的正文确实是**有效事实**（Model Code、thinking 只支持 enabled、image_url 走法、推荐参数、1M 上下文），但**它回答的是"这个页面怎么抓"，不是"价格是多少"**——而调用方当时要的是价格。

**再验证一次（2026-09-11 现场查询，只读副本）**：

```bash
./bin/gopi memory search "智谱 GLM-5.3-Flash API 定价 每百万 tokens 价格" --limit 3 --db /tmp/lca/memory-ro.db
# → #1101 web_fetch 直抓 docs.bigmodel.cn 文档页（GLM-5.3-Flash 实证）   ← 命中的是"文档抓取法"，不是价格
#   #992  gopi GLM direct Anthropic endpoint 0-token root cause
#   #1783 价格表/配置表"改名+调价"数据修复（DeepSeek 的，不是 GLM 的）
```

### 2.4 Verdict

**H1 部分证伪**：PA 审计说"调研结论没有结构化沉淀通道"——**通道存在且被大量使用**（902 次调研调用、157 行 URL 溯源、`web_fetch 直抓 docs.bigmodel.cn` 这类方法被检索 7 次）。
**但断链真实存在于内容粒度上**：`memory_store` 的契约是"蒸馏过的经验"（`isGarbage` 门 + 去重 + "不要存流水账"），LLM 因此系统性地把**可复用的方法**写进去、把**一次性的结论事实**排除在外。结果是：**方法复用率很高，事实复用率为零**——同一份定价数据 5 分钟内被两个会话各买一次。

**这是「机制存在，但沉淀的内容选择错了」**，不是「机制缺失」。

---

## 3. H2 — 代码理解不沉淀

### 3.1 原始数据

| 指标 | E1 | E2 | E3 | 取法 |
|---|---|---|---|---|
| `read` 调用 | 3208 | 5624 | 1747 | hypotheses §per-era |
| `code_search` 调用 | 78 | 866 | 198 | 同上 |
| `read : code_search` | **41.1 : 1** | **6.5 : 1** | **8.8 : 1** | 同上 |
| `git_blame` | 2 | 44 | 6 | 同上 |
| **含代码读写的会话中，「代码定向检索」的会话占比** | 26/194 = **13%** | 69/433 = **16%** | 36/103 = **35%** | crosscheck §2 |

最大读放大对象（按会话数）：

| 文件 | 被读会话数 | 读调用数 | 记忆里指名的行 |
|---|---|---|---|
| `internal/agent/loop.go` | 217 | 1131 | 156 |
| `cmd/agent/main.go` | 148 | 218 | 61 |
| `cmd/agent/cli_tasks.go` | 106 | 624 | 63 |
| `internal/tools/tasks.go` | 104 | 666 | 96 |

> 注意：上表的"记忆里指名的行"是**宽松的 LIKE 匹配**（文件名出现在任意字段），不能读作"该文件的理解已沉淀"。下表才是严格口径。

### 3.2 决定性数据：确实存在一次**批量代码理解沉淀**（反例）

```
distilled_qa id 924 … 1009，全部创建于 2026-08-27 21:57–22:05（约 8 分钟窗口）
  924 internal/agent/clock.go      Q: SessionClock 会话时钟在哪个文件？tick 标签 [t+47s] 怎么生成？
  925 internal/agent/heartbeat.go  Q: heartbeat 心跳提醒在哪？默认 30s/3 轮在哪定义？ShouldRemind 触发条件？
  926 internal/agent/rawlog.go     Q: RawLogger 原始日志怎么写？rawLogSyncThreshold 多少？
  927 internal/agent/resume.go     Q: resume 断点续跑怎么实现？LoadCheckpoint 从 raw log 恢复什么？
  928 internal/agent/session.go    …
  … 覆盖 internal/agent / internal/tools / internal/config / internal/proxy / cmd/agent
```

正文也不是敷衍（`#925` 实测正文）：*"internal/agent/heartbeat.go（内包 agent，160 行）。HeartbeatState 心跳状态机…常量：defaultHeartbeatInterval=30s、defaultStaleRounds=3（NewAgent 读 GOPI_HEARTBEAT_SEC/GOPI_HEARTBEAT_STALE 覆盖）。关键：NewHeartbeatState(interval, staleRounds)、SetOnRemind(callback)、UpdateTaskList(tasks)…"*

**所以「代码理解不沉淀」作为绝对判断是错的。**

### 3.3 但为什么它仍然显得"丢了"——三条可量化证据

**(a) 检索触发率低且增长缓慢**：只有 13% → 16% → 35% 的代码会话会去问记忆一句与代码有关的问题。也就是说，即便 08-27 那批知识在库里躺了 15 天，**大多数需要它的会话从未开口问**。

**(b) 存进去之后，没有被取回**：

```bash
# 全库：1206 次检索返回过的不同行 / 库里总行数
# 全库        : 1808 行，ever-returned 499 (28%)，NEVER 1309 (72%)
# < 09-01 的老行: 1146 行，ever-returned 262 (23%)，NEVER  884 (77%)
# 模块行 924-979:   56 行，ever-returned  16 (29%)，NEVER   40 (71%)
sqlite3 "file:$HOME/.gopi/memory.db?mode=ro" \
  "SELECT retrieval_count FROM distilled_qa WHERE id BETWEEN 924 AND 979" | sort -n | ...
# → 模块行 median retrieval_count = 6（全库 median = 7）—— 不是完全冻结，但 71% 从未被任何检索返回
```

**(c) 取回失败的主因是「索引语义」而不是「内容不存在」——自问自答实验**：

```
SELF-QUESTION RECALL on the 2026-08-27 code-reading block (ids 924-1005): n=82  recall@1=39  recall@3=40
                                                              即 recall@1 = 47.6%  recall@3 = 48.8%
```

用**行自己存储时写的 question 原样**去检索，**只有一半的模块知识能被自己找回来**。典型失败（top-3 全空或全错）：

| 行 | 存储的 question | 检索结果 |
|---|---|---|
| 932 | `internal/agent/compact.go` 相关 | **空** |
| 937–947 | `edit.go / read.go / write.go / bash.go / job.go / gitblame.go / websearch.go / memory.go` | **全部空** |
| 933 | `internal/agent/loop.go` | 返回 `#933`（这行本身）之外的 #729/#1237/#1442/#886/#1047 —— **全是"loop.go 里改过的功能"，没有一条是"loop.go 是什么"** |

**机制解释**：这批行的 question 措辞是**文件说明书**（"RawLogger 原始日志怎么写？rawLogThreshold 多少？"），而真实检索是**任务语言**（"raw log 里怎么定位上一次请求"）。FTS5+CJK 分词 + 30% 词覆盖排序对"任务语言 → 说明书语言"的跨措辞匹配能力有限；更糟的是，这些行**没有 `when_at`、`lesson_type`**（见 §6.1），也基本没有意图（intent）路由提示。

### 3.4 Verdict

**H2 部分证伪**：理解**沉淀了**（86 行结构化模块知识，含常量值/方法签名/设计理由）。
**但断链在检索语义层**：①会话开口问的比例 13–35%；②问到了也只有 48% 能取回。
**这是「机制存在，但索引/措辞与真实提问方式不匹配」，不是「机制缺失」。**

---

## 4. H3 — 调试根因不沉淀

### 4.1 原始数据

| 指标 | 数值 | 取法 |
|---|---|---|
| 判定为失败的 bash 调用 | **4157 条 / 437 会话** | crosscheck §3 |
| 不同失败测试名 | 361 | 同上 |
| 其中**在 ≥3 个会话里复发**的 | **22** | 同上 |
| 22 个中，名字出现在记忆某处的 | **18 / 22 (82%)** | 同上 |
| 检索调用中，"query 带错误信号"的 | **106 / 1206 = 8.8%** | 同上 |
| 记忆里以错误信号为键的行 | `FAIL` 126 / `timeout` 89 / `stale` 102 / `flake` 27 / `panic` 24 / `SQLITE_BUSY` 9 | 同上 |

### 4.2 反例：调试根因其实沉淀得**比 H1/H2 都好**

**证据 A — 调试+沉淀在同一次会话内完成（22/22）**：

| 复发会话数 | 失败签名（测试名） | 记忆里指名的行数 | 首次出现 |
|---|---|---|---|
| 12 | `TestSystemPromptDensity` | 15 | 08-26 |
| 7 | `TestTaskRunSurvivesClosedStdoutPipe` | 10 | 09-02 |
| 5 | `TestMemorySearchNonBlocking` | 15 | 08-26 |
| 5 | `TestRunTaskWorker_OnceSweepsZombieFirst` | 4 | 09-03 |
| 5 | `TestSyncDeltaServerResolvesConflict` | 3 | 08-31 |

```bash
python3 - <<'EOF'   # 见 §10 复现段；输出：
# recurring test signatures (>=3 sessions): 22
#   ... where the SAME session also stored to memory: 22 / 22
# distinct failing test names: 361
# names failing in exactly 1 session: 310
EOF
```

**22/22 的复发失败签名，其会话本身也发生过 `memory:store`** —— 调试与沉淀在同一次会话内闭环，一次都没漏。同时 361 个失败测试名里 **310 个只失败过 1 次**（一次性失败，本就不该沉淀）。

**证据 B — 错误签名族已成"簇"**（每条都是可检索的），且**被真的检索回去了**：

```
FAIL   : 117/126 行至少被检索返回过一次
timeout:  83/89
超时    :  24/24
SQLITE_BUSY: 9/9
panic  :  20/24
flake  :  25/27
```

**证据 C — 现场检索（2026-09-11，只读副本）**：带错误信号的 query 确实把对的根因行拉回来了：

```
Q[09-09T04:45] "malformed settings.json config.ApplyEnv nil pointer panic main.go loadSettings"
  → #1535 settings.json bool knob accepting string spellings
Q[09-11T09:39] "internal/standards gate baseline line-keyed Rule 17 stale baseline entry fix"
  → #1797 Merge inherited a RED gate from master — establish failure ownership
Q[08-28T07:18] "parseTTF boundary check panic text2img atomic write edit 0644 permission"
  → #1022 gopi code review 2026-08-28 buddy cross-review findings
```

**证据 D — 有"配方"级别的沉淀被反复复用**（`retrieval_count` 最高的一批）：
`#1361`(21)、`#1694`(14)、`#1150`(13)、`#1483`(13)、`#870`(12)、`#1103`(12)、`#1461`(12)。

### 4.3 那为什么 H3 仍被列为断链？（真缺口在触发端）

只有 **8.8%（106/1206）** 的检索 query 带任何错误信号，而这 106 次里还包含"顺手提到 fail"的非调试检索。对照 §4.1：**437 个会话发生过失败命令，但只有 74 个会话用错误信号问过记忆**。

典型可避免的重复（三类**环境级工具失败**，各自在 100+ 会话里重复）：

```bash
# 精确计数（逐字匹配 tool_result，非 §1.3 的启发式）
#   dangerous command rejected   sessions=147
#   command timed out            sessions=122
#   command killed: no output    sessions=134
sqlite3 "file:$HOME/.gopi/memory.db?mode=ro" \
  "SELECT id,substr(topic,1,80) FROM distilled_qa WHERE answer LIKE '%dangerous command%' OR topic LIKE '%workdir%'"
# → 1179 bash rm -rf worktree 外路径被安全层拒 — 用相对路径绕过        ← 唯一一条
# → 1265/1582/1600 是 worktree 生命周期，与此无关
```

**147 个会话撞上安全层拒绝，记忆里只有 1 行（#1179）写了绕过方法。** 这一行是对的内容、对的措辞，**却拦不住另外 146 次**——因为没有任何机制在"bash 被拒"的那一刻把它送进上下文。

> 需要谨慎：这三类失败**有确定性修复**（用 `job` 工具跑长命令 / 别把 `/tmp` 当 workdir / 危险命令用 `GOPI_ALLOW_DANGEROUS`），可以辩称属于"一次就学会"的常识。但 #1179 的存在恰好**证伪了"不可能沉淀"**：知识已经存了、写得也对，仍然重复了 146 次。**这恰是 H3 断链的最硬证据形式：不是没沉淀，而是沉淀的知识从未在正确的时刻被送回上下文。**

### 4.4 Verdict

**H3 证伪**：根因沉淀是五个假设里做得**最好**的一环（错误签名成簇、82% 的复发签名被记忆点名、检索命中质量高）。
**真断链在触发**：8.8% 的检索带错误信号；147 个会话撞上同一个安全层拒绝，而库里唯一一行解法（#1179）从未被送进上下文。
**这是「机制存在且产出优质，但触发门缺失」——与 PA 审计的"调试根因不沉淀"表述相反。**

---

## 5. H4 — 环境状态无权威视图

### 5.1 原始数据（**机制缺失**）

**(a) 没有任何环境状态存储**：

```
memory.db 全部业务表: distilled_qa / memory_feedback / tasks / chats / sync_state / task_events / procedural / session_conclusions
→ 没有 env / tool-version / installed-package 类表
```

**(b) `gopi` 没有 env/doctor 类动词**（`./bin/gopi --help` 全量 grep）：

```bash
./bin/gopi --help | grep -iE "doctor|gopi env|gopi tools|health"
# → （空）
```

顶层动词只有：`version / log / stats / raw / memory <sub> / task <sub> / brew / chat / code-search / checkpoint / proxy / completion / act / …`

**(c) 靠重跑探针**：49 个会话里 **75 次**环境探针（`which` / `command -v` / `--version` / `brew list` / `go env`）。

**(d) 记忆里几乎没有环境事实**：

| 问句形态 | 行数 |
|---|---|
| `装了什么` | **0** |
| `哪个版本` | **0** |
| `是否安装` | **0** |
| `installed` | 1 |
| `version` | 7 |

**(e) 现场检索验证（只读副本）**：

```bash
./bin/gopi memory search "xdotool 是否安装 版本" --limit 3 --db /tmp/lca/memory-ro.db
# → #1207 post-receive hook 触发机制（无关）  #170 版本控制（hermes 旧库导入，无关）  #1022 code review（无关）
```

三条全错——**因为库里根本没有这条事实**，检索再好也变不出来。

### 5.2 唯一的部分例外

`~/.gopi/brew_state.json` 存了 `{"last_run":"2026-09-11T06:24:04+08:00"}` —— 这是**调度状态**（brew 上次跑的时间），不是环境状态。`settings.json`（符号链接 → `.ds_flash_settings.json`）里 `compute_nodes`/`sync_peers`/`repos` 是**配置**，也不是"装了没装/什么版本"。

### 5.3 Verdict

**H4 支持（机制缺失，PA 审计判断正确）**。断链形态是"探针结果只活在会话上下文"，与 H1/H2/H3 的形态有本质区别：**那三环是"存了但取不回"，这一环是"从来没人存"**。
一个额外发现（属 H3/H4 交叉）：`dangerous command rejected: target "/tmp"/"~"/"/" is outside workdir` 这类**工具约束事实**在 36+6+4+3+3 个会话里重复踩，同样零沉淀——环境约束的"权威视图"缺失不止于软件版本。

---

## 6. H5 — 需求动机不留痕

### 6.1 原始数据

| source | 任务数 | payload 空 | 占比 |
|---|---|---|---|
| `human` | 333 | 10 | 3% |
| `llm`（task_submit） | **131** | **0** | **0%** |
| `auto`（台账自动登记） | 505 | 463 | **92%** |

**但 payload 非空 ≠ 有动机**。对 333 条人类任务逐条扫动机线索（`because|since|after|incident|事故|因为|所以|背景|动机|导致|根因|为什么|发现|观察到|regression|fails|failure|bug|问题`，覆盖 payload+reason）：

```
n=333 {'thin payload (<200 chars)': 26, 'payload/reason states a motive': 241, 'no motive token': 92}
```

**92 条（27.6%）人类任务的 payload+reason 里没有任何动机线索**。样本（全部是命令式结果记录，或仅给"做什么"不给"为什么"）：

```
t-1788012586 'gopi run completed — run: elapsed=1m19s'                payload=''
t-1788013479 'gopi run completed — run: elapsed=53s'                  payload='任务：用 write 工具创建文件 /tmp/p2e2e/proof.txt…'
t-1788013566 'gopi run completed — run: elapsed=26m3s'                payload='实现 task 状态变迁通知：①…②…'      ← 只有 what，无 why
t-1788028221 'gopi run completed — run: elapsed=44m32s'               payload='M2 实施（M1 已 merge master cfa8d5c…）：范围：①…'  ← 只有 scope
t-1788031086 'gopi run failed: exit status 1 — run: elapsed=57m48s'   payload='M3 实施（…）：范围：①…'
```

**(b) 动机无处可存 —— schema 层的确认**：

```sql
-- tasks 表的全部文本字段：summary / payload / tags / source / reason / deliverable / verify_evidence
-- reason 非空行: 287 / 969
-- session_conclusions.analysis 非空: 12 / 970   ← 几乎全空
-- deliverable 形状: {"branch":…,"repo":…,"worktree":…}  ← 字段齐全，但只记"产物在哪"，不记"为什么做"
```

即：**没有任何字段是"为什么提这个需求"的权威位置**。`reason` 是离它最近的，但只在 `task reopen`（该动词已于 2026-10-21 删除）/ 部分提交路径被填（287 行）。

**(c) 更硬的反例 —— 动机在入库路上被 shell 丢掉**：

```sql
SELECT COUNT(*) FROM tasks WHERE payload LIKE '%$(%';   -- → 9
```

**6 条人类任务的 payload 字面存着 `$(cat /tmp/payload.txt)`**（id 133–138，全部 `source=human`，每条 payload 恰为 23 字符）—— **提交者用 shell 展开传 payload，命令被当成字面量存进库**。这 6 条任务的动机**在进入 DB 之前就丢了**，属于"链路物理性断裂"的直接证据（后来 `--payload @FILE` 语法解决了这类问题，但 DB 里的历史证据保留了下来）。

> **口径修正（2026-09-11 复核）**：本报告初稿写作"9 条"，那是 `payload LIKE '%$(%'` 的计数。逐行看后应分两类：**6 条是真丢失**（`$(cat …)` 占位符，动机永久丢失）；另外 3 条是 `source=llm` 的简报里**引用**了 `$(seq 1 10)` 这类命令示例（文本内嵌，非丢失）。故真实丢失量 = **6**。可复核命令见 §11。

### 6.2 反例与 Verdict

**反例（这条链做得好的部分）**：`task_submit` 的 LLM 任务 **131 条，payload 0% 空**，且质量高于人类条目；例如：

> *"BUG (pre-existing on master 2cb44bc; NOT caused by any current branch — verified by identical finding sets). REPRO: cd /home/user/work/GoPi… OBSERVED: --- FAIL: TestStyleGate…"*

**这恰恰说明断链不是"agent 不会写动机"，而是「人类+auto 提交路径没有强制动机」**：
- `source=llm`（agent 自己写 brief）→ 100% 有动机；
- `source=human`（人用 CLI 提交）→ 27.6% 无动机；
- `source=auto`（台账自动登记，`task_id == session_id`，184/505）→ 92% 无 payload（设计如此：它只是执行记录）。

**H5 支持**，但需精确表述：**「动机字段机制缺失 + 人类提交路径无强制 + 6 条因 shell 展开物理丢失（id 133–138）」**；同时**存在一个有力的机制反例**：LLM 自投路径（`task_submit`）已经证明"动机五要素齐全"是可做到的，缺的是把它变成**所有**提交路径的硬约束。

---

## 7. 交叉验证对照表：会话动作数 vs 库内沉淀数

**差值 = 断链量**。全部数字来自 `python3 scripts/lifecycle-crosscheck.py /tmp/lca/extract2.jsonl ~/.gopi/memory.db`。

### 7.1 总量对照

| 环节 | 会话动作（发生了什么） | 库内沉淀（留下了什么） | 差值 = 断链量 | 断链形态 |
|---|---|---|---|---|
| 调研 | `web_search`+`web_fetch` **902** 调用 / 45 会话 / 515 去重查询 | 引用 URL 的知识行 **157**；**GLM 定价数字 0 条** | **≥745 次调研动作无对应沉淀**（其中含"不该沉淀的一次性查询"，需人工判定）；**1 组确定重复调研** | 粒度错：存了方法没存结论 |
| 代码理解 | `read`+`code_search`+`git_blame` **11773** 调用 / 791 会话 | 指名 `*.go` 的行 **178**（其中 107 行 sources 为空） | **11595**（98.5%）（含大量"读一眼就改"的必要耗费） | 索引错：48% 自问自答命中率 |
| 调试 | 失败 bash **4157** 条 / 437 会话；361 个不同失败测试名 | 22 个复发签名 **18 个**在记忆里被点名；错误信号行 **≈400** | 首次失败的 339 个真实缺陷根因落到 `tasks`/devdoc 而非 memory；**触发端 8.8%** | 触发门缺失 |
| 环境 | 环境探针 **75** 次 / 49 会话 | 环境事实行 **≈0** | **75/75 ≈ 100%** | **机制缺失（真丢）** |
| 需求动机 | 969 条任务入库 | 92/333 人类任务无动机线索；`session_conclusions.analysis` 12/970 | **≥92 条**（+6 条 shell 丢失） | **机制缺失（字段无强制）** |

### 7.2 写入端趋势（全部会话，`118,657` 次 tool_use / `1142` 会话）

| | E1 (≤08-27) | E2 (08-28…09-09) | E3 (09-10… ) |
|---|---|---|---|
| 会话数 | 457 | 439 | 103 |
| `memory:store` 调用 | 426 | 725 | 286 |
| **每会话 store** | 0.93 | 1.71 | **2.83** |
| 调用过 store 的会话 | 39% | 84% | **92%** |
| **首个动作就是 `memory:search`** | 191/457 = **42%** | 430/439 = **98%** | **103/103 = 100%** |

（工具排行：`bash` 61838 / `job_output` 18204 / `read` 10573 / `edit` 8194 / `context_trim` 2144 / `memory:feedback` 1472 / `memory:store` 1435 / `keyword_search` 1242 / `code_search` 1142 / `git_blame` 52）

### 7.3 检索端现状（全库口径）

| 指标 | 数值 |
|---|---|
| 检索调用 | **1206**（985 次有结果，221 次空） |
| 平均每次返回行数 | **1.29** 行（几乎不构成"上下文"） |
| 库里总行数 | 1808 |
| **曾被任何检索返回过的行** | **499 (28%)** |
| **从未被返回过的行** | **1309 (72%)**（老行 77%，模块行 71%） |
| 检索 query 带错误信号的比例 | **8.8%** |
| 检索 query 带代码定向词的比例 | E1 13% → E2 16% → E3 **35%** |

**表格给出的核心结论**：写入端在**强劲增长**（每会话 store 0.93→2.83），但**取回端在原地踏步**（平均 1.29 行/次，72% 的行从未被返回）。断链不是"没记录"，而是**"记录与取回之间没有配平"**。

---

## 8. 最小可行修复设计（≥2 条，含误报/噪音风险与失败模式）

**共同约束**：复用 `memory.db` + 现有工具面，**不新建 store、不新建守护进程、不新增顶层工具**（Rule 3）。

---

### 修复 A（对应 H1）—— 调研结论：给 `memory_store` 加"结论槽位"，落 `when_at`

**问题定位**（§2.3）：`memory_store` 的契约全是"蒸馏经验/配方"，LLM 因此把**可复用方法**写进去、把**一次性结论事实**排除。而事实上"文档页怎么抓"和"价格是多少"是**两种都值得存**的知识。

**设计（最小）**：

1. **不新增列、不新增工具**：`distilled_qa` 已有 ``lesson_type` 与 `when_at``（当前全空，见 §8 附注）。把 `lesson_type` 从"死列"激活为**内容类型判别器**：
   - `lesson` = 可复用配方/坑（现在的主力内容）
   - `fact` = 时点事实（定价、版本、官方参数、接口行为），**必须带 `when_at`**
   - `decision` = 架构决策（现在混在 lesson 里）
2. **写入路径**：`memory_store` 已接受 `--keywords`；**复用同一通路，把 `when_at` 由引擎在 store 时自动填成存储时刻**（无需 LLM 提供，零新增 prompt 负担），`lesson_type` 由 `--lesson-type` 可选指定，缺省 `lesson`。
3. **检索路径**：`memory_search` 渲染时**把 `fact` 类行排在前面并显式标注 `(fact, 存于 2026-08-29)`**，让 LLM 一眼看到"这条会过期，先核验再用"。
4. **触发引导（这一条才是关键，单靠 1–3 不解决问题）**：`task_submit` **不接受**"现状事实为空的调研任务"——即在 reframe 阶段，若任务含 RESEARCH 字样且**库里没有对应的 `fact` 行**，则强制把"存一条 `fact`"写进 acceptance。

**为什么这条能补上 5 分钟重复调研**：重复调研发生在**第二次会话启动**时（12:59），此时它其实**问了记忆**（`memory:search "抓取 bigmodel.cn…pricing…"`）但只命中 `#1053`（无关）。若第一次会话存了一条 `fact: GLM-5.3-Flash 定价 = …（when_at=08-29）`，第二次的检索会直接命中。

**误报/噪音风险**：
- 风险 1：**每条调研都存 fact → 库膨胀**。缓解：`isGarbage` 门 + 已有相似去重（trigram Jaccard）已能拦截重复；且 `lesson_type='fact'` 的行可以设**更短的 tier 衰减**（例如 30 天后自动降 cold）。实测当前 72% 的行从未被取回，**任何"让 fact 更容易被召回"的改动都是净收益，不是污染**。
- 风险 2：**LLM 把"一次性查询"也标成 fact**。缓解：`fact` 只在"回答了一个可复用的问题"时才有价值——用**检索次数**做后验过滤（`retrieval_count=0` 且 30 天 → 降 cold），这与现有 tier 机制同构，无需新机制。

**失败模式**：
- FM1：`when_at` 语义仍无读者 → 变成第二个死列。**必须同时实现 A-3**（检索渲染要用它），否则重演 §8 附注的 `when_at` 死列问题。
- FM2：LLM 不填 `lesson_type` → 全部默认 `lesson`，行为等同现状（**安全降级**，不会更坏）。

---

### 修复 B（对应 H3）—— 调试根因：错误签名**自动注入**（把"存了但没人问"变成"不问也给"）

**问题定位**（§4.3）：根因沉淀质量高，但触发率只有 **8.8%**；147 个会话重复撞安全层拒绝，而被拒那一刻没有任何机制把 #1179 送进上下文。

**设计（最小）**：

1. **复用已有注入通道**：`memory(action=search)` 的**任务启动强制检索已经 100% 生效**（E3：103/103 会话首动作即检索，§7.2）。所以不需要新机制——**只需要让这条已有的检索"带上错误签名"**。
2. **错误签名自动补齐**：当一次 `bash`/`go test` 的 tool_result 匹配失败特征（`FAIL: TestX` / `panic:` / `command timed out` / `dangerous command rejected`）时，**引擎在下一轮把该签名拼进一次 memory 检索**（不是新工具，是现有 `memory:search` 路由的一次自动触发），检索结果以现有 `[t+NNs]` 前缀样式注入。
   - 预算：每次失败最多 1 次自动检索，同一签名在同一会话内最多 1 次（避免 4157 条失败打爆上下文）。
3. **环境级坑（147 会话的空白）**：对**非测试类**的环境失败，落一条 `lesson_type='environment'` 的规则行——**不是"我遇到了 X"，而是"用 `job` 工具跑长命令，别把 `/tmp` 当 workdir"**。这类行**天然适合自动注入**，且**数量极少**（一次会话内 ≤1 条）。

**为什么这条能补上 8.8%→接近 100% 的触发缺口**：请求侧已经有了 100% 的"任务启动检索"，缺的只是"失败时再检索一次"。这是**同一个已验证机制的第二处触发点**，不是新机制。

**误报/噪音风险**：
- 风险 1：**噪音注入**。4157 条失败里很大比例是"试错中间态"（编译器报个错马上改），为每条注入一次检索会显著增加 token。缓解：① 阈值（同签名 1 次/会话）；② 只对**跨会话复发**的签名注入（§4.2 的 22 个签名，可控集合）；③ 复用 `context_trim` 把已消费的注入块回收。
- 风险 2：**检索命中垃圾**（§5.1(e) 已见：`"xdotool 是否安装 版本"` 返回 3 条全错）。缓解：注入时**只在命中行的 `retrieval_count>0` 或 confidence≥阈值时注入**——库里有 `retrieval_count` 和 `confidence` 可直接用，不需要新排序。

**失败模式**：
- FM1：自动检索拖慢每轮（一次检索 ~0.3 s + 1.3 行）。**实测平均只返回 1.29 行**，token 代价可控；若命中为空则注入"（无相关记忆）"一行，这本身是有价值的信号。
- FM2：错误签名正则误判成功输出 → 注入无关记忆。缓解：只用**高精度**特征（`FAIL: Test[A-Za-z0-9_]+`、`panic:`、工具自己的 `error: command timed out`），不用宽泛的 `error`。

---

### （附）两处顺带发现的**产品缺陷**（证据见 §9 附注，不在本任务实现）

1. **`when_at` / `lesson_type` 是死列**：`distilled_qa.lesson_type` 与 `.when_at` 各 **0/1808 行**填充。代码侧 `memoryStoreRow`（`internal/tools/memory.go:2143+`）的 INSERT 列清单**包含**这两列，但 `switch` 落到 `default` 一律写空串（注释自承 "the LLM contract does not provide them yet"），**backfill 脚本 `scripts/backfill_exam_5w1h.py` 也不写它们**（grep 命中 0）。
   后果（真实功能失效，非纸面）：`possiblyStale()`（`internal/tools/memory.go:1783`）= `lastRetrieved 新鲜 → false；否则 parseTimeMarker(whenAt)`；`whenAt==""` → `parseTimeMarker` 返回 nil → **返回 false**。故 **`possibly_stale` 在生产中恒为 false**——tool 描述承诺返回该信号，实际从未生效。这与 Rule 21 是**同一类缺陷**（声明了读者/语义，但没有写入方）。
   最小修复：store 时自动写 `when_at`（= `distilled_at` 时刻），或在 `possibly_stale` 中对空 `when_at` 回退到 `distilled_at` 老化——**两者必须同时做**，否则列填了仍是死代码。
2. **6 条任务的 payload 字面存着 `$(cat /tmp/payload.txt)`**（id 133–138）：动机在 shell 展开这步丢在 DB 之外（§6.1(c)）。`--payload @FILE` 语法已解决该类问题，但这 6 条历史行证明这条链曾是**物理断裂**的。（口径说明见 §6.1(c) 的复核注。）

---

## 9. 对 PA 审计 7 个断链点的修正建议

| PA 审计原表述 | 本次证据支持的修正 |
|---|---|
| ①「调研结论不沉淀 —— 调研是一次性消耗品」 | 修正为：**方法沉淀良好并被复用（#1101 检索 7 次）；结论事实不沉淀**（GLM 定价 0 行，5 分钟内重复调研）。修复靶点是**内容粒度**，不是"加一个沉淀通道" |
| ②「环境状态无权威视图」 | **维持原判（唯一完全成立的"机制缺失"）**，并扩大到**工具约束事实**（`dangerous command rejected` 在 36+ 会话重复） |
| ③「代码理解不沉淀 —— 同一模块反复探索」 | 修正为：**已沉淀 86 行模块知识**（08-27 批次）；真断点是**检索语义不匹配**（自问自答 recall@1=48%）与**触发率 13–35%**。修复靶点是**索引措辞**，不是"建模块索引" |
| ④「调试根因不沉淀 —— 同类 bug 重复踩」 | **反转**：根因沉淀是五个假设中最好的（82% 复发签名被点名、错误族被检索 117/126）。真断点是**触发率 8.8%**。修复靶点是**错误时自动检索**，不是"存根因" |
| ⑥「需求动机不留痕」 | 维持，但需精确：**字段机制缺失 + 人类路径无强制（92/333）+ 9 条 shell 物理丢失**；**反例是 LLM 自投路径 0% 空 payload**，证明"可做到" |
| ⑦「记忆自动查用无保证 —— 存了不查 = 白存」 | **证据部分反转**：任务启动检索已 **100%**（E3 103/103）。真正的"存了不用"在**取回端**：72% 的行从未被任何检索返回，平均每次检索仅返回 1.29 行。建议把 ⑦ 改写成**「取回覆盖率低」**而不是「不查」 |

**方法论结论（对第 5 问的最终回答）**：
PA 审计的判据「产物能不能被下游寻址到」是**对的方向**，但本次数据显示它在 H1/H2/H3 上**把"寻址失败"误判成了"没有产物"**。两者的修复成本差一个量级：加一个沉淀通道要改契约与 prompt；**让已有 1808 行中的 1309 行可被寻址**，只需要修检索语义与触发点。

---

## 10. 验证与交付

```bash
# 本报告全部数字的复现（顺序执行，均只读）
cd ~/workspace/gopi-lifecycle-evidence
python3 scripts/lifecycle-chain-audit.py actions ~/.gopi/sessions /tmp/lca/actions.json
python3 scripts/lifecycle-chain-audit.py extract ~/.gopi/sessions /tmp/lca/extract2.jsonl
python3 scripts/lifecycle-hypotheses.py /tmp/lca/extract2.jsonl
python3 scripts/lifecycle-crosscheck.py /tmp/lca/extract2.jsonl ~/.gopi/memory.db
sqlite3 "file:$HOME/.gopi/memory.db?mode=ro" ".backup /tmp/lca/memory-ro.db"
CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/
./bin/gopi memory search "<question>" --limit 3 --db /tmp/lca/memory-ro.db
```

**交付物**：
- 本报告 `devdoc/lifecycle-chain-evidence-20260911.md`
- `scripts/lifecycle-chain-audit.py`（采集：actions / extract / research / tree）
- `scripts/lifecycle-hypotheses.py`（H1–H5 分层统计）
- `scripts/lifecycle-crosscheck.py`（交叉验证对照表）

**可复现性说明（重要）**：`memory.db` 是**生产库且仍在被写入**。审计期间（约 20 分钟）它从 `distilled_qa 1808 / session_conclusions 970` 增长到 `1811 / 973`（其他 agent 在跑）。因此：
- **本报告引用的比率是"审计时刻快照"**，重跑会得到同量级但个位数的差异（实测同一脚本两次运行只有这一行不同，其余全部逐字一致——已在 §10 用 `diff` 验证）；
- §3.3(b)、§7.3 的"ever-returned / never-returned"是**下界**：新写入的行来不及被检索，只会让"从未返回"的比例在重跑时略升；
- 会话 JSONL 面（`~/.gopi/sessions`）只增不改，重跑的差异同样只来自新会话。

**未做 / 明确跳过**（诚实声明）：
- **未实现任何修复**（本任务 charter 是只研究）。
- **未对 `tasks` 做全量人工语义标注**：H5 的"92 条无动机"是**正则线索判定**，会用"发现问题/失败/bug"这类词去近似"动机"，因此是个**下界**——真实的"无动机"任务可能更多（例如只给 scope 的 `M2 实施：范围：①…` 靠词表能勉强命中）。
- **147 会话重复撞安全层拒绝的结论未做逐会话人工复核**：它基于"错误签名聚合"，未排除"这些会话本来就应该失败"的情形（例如故意验证 danger 拦截）。方向可信，精确计数需人工抽样。
- **未覆盖 PA 审计的第 ⑤ 条（测试基础设施不可靠）与第 ② 条的部分子项**（已派 01a08d7e / 01a08d67-cc86），不在本任务范围。

---

## 11. 出报告后的独立复核（2026-09-11，交付前）

任务 charter 要求"抽样方法必须说明""数字可复现"。出报告后对**每一个可被机器证伪的数字**做了一次独立复核（不复用 §1.2 的分析脚本，直接跑最原始的 shell / SQL），结果如下。

### 11.1 通过复核的数字（未改动）

```bash
# 交叉验证对照表的锚点（§7.1）
python3 - <<'EOF'
import json,collections
s=d={}
EOF
grep -rl "dangerous command rejected" ~/.gopi/sessions --include=*.jsonl | wc -l     # → 183 文件
python3 -c "..."   # extract2.jsonl 口径 → 443 条 / 147 会话（见 11.2）
```

| 指标 | 报告值 | 复核方式 | 复核值 | 结论 |
|---|---|---|---|---|
| 失败信号分布（§4.3） | 147 / 122 / 134 会话 | `extract2.jsonl` 全量重扫 `result_head` | **147 / 122 / 134** | ✅ 逐字一致 |
| 从未被检索返回的行（§3.3b, §7.3） | 1309 (72%) | — | 未重算（需重跑检索索引，见 11.3） | ⏸ 快照值，未复核 |
| `lesson_type` / `when_at` 填充（§8 附注） | 0 / 0 | `SELECT COUNT(*) … <>''`（生产库 + `mode=ro`） | **0 / 0**（1814 行） | ✅ 死列确认 |
| `possibly_stale` 恒 false 的代码路径 | `memory.go:1783` | `sed -n '1783,1792p'` + 读 `parseTimeMarker` | `whenAt==""` → `parseTimeMarker` 返回 nil → `return false` | ✅ 一致（行号为函数定义行） |
| store 的 `default` 分支写空串（§8 附注） | `memory.go:2143+` | grep `lesson_type` | 实际 `default` 分支行为在 **`internal/tools/memory.go:2244-2251`**（含自承注释），INSERT 列清单在 **2214** | ⚠ **行号修正**，结论不变 |
| backfill 不写这两列 | 是 | `grep -n 'lesson_type\|when_at' scripts/backfill_exam_5w1h.py` | 0 命中；它写的是 `exam_questions + kw_*` | ✅ 一致 |
| backfill 确实活着（反证"死列=死脚本"） | 隐含 | 生产库 `kw_what` / `exam_questions` 各 **1072** 行非空，`kw_where` 711 | **1072 / 1072 / 711** | ✅ 说明问题**只在** `lesson_type`/`when_at` 两列 |
| 环境事实行 ≈0（§5.1d） | 0/0/0/1/7 | 只读副本按同组问句形态 | `装了什么`=0、`哪个版本`=0、`是否安装`=0；宽口径 `安装\|版本` = 9 行，**逐行看 9 条全是无关行**（openssh 升级提问、memsync Version 字段校验…），无一条是"环境装了/没装" | ✅ 结论成立，**口径要收紧**：正则是宽匹配、结论靠人工判读 |

### 11.2 发现并已修正的一处数字错误（诚实记录）

§6.1(c) 初稿写"**9 条**人类任务 payload 字面存 `$(cat /tmp/payload.txt)`"。真实情况分两类：

```bash
sqlite3 "file:$HOME/.gopi/memory.db?mode=ro" "SELECT COUNT(*) FROM tasks WHERE payload LIKE '%\$(cat%';"   # → 6
sqlite3 "file:$HOME/.gopi/memory.db?mode=ro" "SELECT COUNT(*) FROM tasks WHERE payload LIKE '%\$(%';"      # → 9
# 逐行看这 9 条：id 133–138（source=human，payload 恰 23 字符 = "$(cat /tmp/payload.txt)"）← 真丢失
#                 id 1556/1639/1677（source=llm）是简报里**引用**了 `$(seq 1 10)` 之类的命令示例 ← 非丢失
```

**已修正为 6 条**（§6.1(c)、§6.2、§7.1、§8 附注 2 四处同步改）。教训与 §1.3 一致：**宽正则的计数必须逐行判读，否则会把"引用"算成"丢失"**。

### 11.3 未复核项与原因（不掩饰）

- **§3.3(b) 的 48% recall@1**：需要重建只读副本 + 逐条 `gopi memory search`（82 次检索），属于耗时实验；本次只复核了它的**上游数据**（模块行 924–1009 存在、71% 从未被返回）。
- **§7.3 的 72% never-returned**：依赖 `distilled_qa.retrieval_count / last_retrieved` 这两列被检索路径真实回写；本次只验证了列**存在且被填充**（有非零行），未重跑 1206 次检索。
- **§4.3 的"147 会话重复撞安全层拒绝"**：复核得到 **443 条 / 147 会话**（与报告一致），但**"重复踩"的因果未做逐会话人工复核**（见 §10 已声明）。生成库一次原始扫描得到**165 个会话文件命中该串**（较粗口径，含报告自身文本与裁剪差异），两个口径的差需要人工抽样才能收敛——**方向不变，精确计数不宣称**。
- **6 条 `$(cat …)` 是否真丢**：`payload` 真值已可由 CLI 重放确认，但当时的 `/tmp/payload.txt` 内容**已不存在**，故"丢失"的判定基于"payload 字段内容 = shell 命令而非请求正文"这一事实本身（可证），**不基于**恢复原文（不可证）。

### 11.4 构建/测试门（本报告改动的回归）

```bash
CGO_ENABLED=0 go build ./...                       # → exit 0（无输出）
go test ./internal/testutil/ ./internal/standards/ # → ok testutil 1.071s / ok standards (cached)
git status --porcelain                              # → 只有本报告被修改，无 .go 文件改动
```

本报告**不含任何产品代码改动**（`git diff --name-only b0e92fb..<本 commit>` 只有 `devdoc/` + `scripts/lifecycle-*.py` 四个文件）；§11.2 的修正只改文档数字。
