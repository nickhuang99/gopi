<!-- doc-class: normative -->
> **Retired mechanism (document-pipeline teardown — slices 1/5 and 3/5).**
> `gopi tool doccheck` and `tools/docclass/` were DELETED (`e6bfa10`,
> `29882b3`) and this page is no exception: the command quoted below is the
> RECORD of what a past run executed — it cannot be re-run, and no document may
> be classified, verified or frozen by running it.
# gopi chat 缓存命中率归因：92% vs hermes 99.87%（2026-09-30）

> **溯源。** `TestDocAnchorsResolveInNormativeDocs` 已于 2026-10-22 随 `internal/docclass` 在 `5a35f61` 中删除：本页里的 `file.go:NNN` 引用是落地时的记录，没有任何在跑的门禁解析它。行号对人依然有用，但它不再被测试守护。下文引用的原句都按当时的写法保留 —— 若某句读起来像现在时（"FAIL"、"resolve"、"门禁"），那是那一次运行的状态，不是本树上还活着的门禁。

**结论（一句话）**：「中途注入破坏前缀」这个假设**不成立**——会话里注入的每一类 harness 消息
（store checkpoint / end-turn continuation / rounds warning / job notice）在实际 wire 上
**都是尾部追加**，紧邻它的下一次请求 miss 只有几百 token。真正吃掉 60.9% miss 的机制是
**trimContext 的滑动窗口**：因为一段固定的 per-turn 前缀开销把上下文常年顶在触发线之上，
`TrimToFit` 每一轮都从**最新头部**删掉几条消息，下一轮的追加又把它顶回线上——头部每轮前移，
于是整段已被缓存的前缀被反复重写、反复重新 prefill。

---

## 1. 口径先对齐：92% 与 91.5% / 91.96% 是同一个数

`session.jsonl` 的 `request` 事件里，`cache_hit` / `cache_miss` / `prompt_tokens` **是累计值**
（`internal/agent/chat_ops.go:649-657` 用的是 `a.Cost.Total*`，不是增量），
而 `internal/agent/loop.go:4290-4300`（任务循环）写的是**增量**。
两种读法给出的数：

| 读法 | 计算 | 结果 |
|---|---|---|
| 末次累计 | 85,105,280 / 93,042,446 | **91.47%** |
| 累计值直接求和（`trajectory.go` / `gopilog` 的口径，即 UI 上的 92%） | 27,951,657,088 / 30,395,161,260 | **91.96%** |
| **per-request 增量**（唯一正确口径，见 §2 自洽性检验） | 85,104,256 / 93,011,362 | **91.50%** |

三者一致 ⇒ 先前的「92% vs 99.87%」是真差距，**不是口径错觉**。hermes 的 99.87% 是它自己的
per-request 口径，量纲相同，可以比。

**证据与脚本**：`~/.gopi/sessions/01a0f206-9ed0-7735-8348-0fa44b9b4000/cachehit/attrib.py`
（调用：`python3 attrib.py`）。

## 2. 方法：per-request 归因 + wire 真值 join

**第一层（自洽性）**：相邻两次 request 的增量满足 `dp == dh + dm`（±2 token），684/684 全成立。
这证明增量口径正确（累计读法下不可能恒等）。

**第二层（wire 真值）**：`<sid>.raw.log` 是 JSONL，每个 `{"model":…}` 行就是一次**真实请求
payload**（含 system / messages / tools）。用「最后一条 tool_result 的内容归一化」做锚点，
与 `session.jsonl` 的 request 顺序 join，**684/685 命中**（1 条无 tool_result 无法锚定，已排除）。
脚本：`wire2.py` → `snaps2.pkl` → `join.py`（输出 `joined_rows.json`）。

**第三层（分类）**：`join.py` 用「消息条数」（`n` 变多/变少/不变）分类，得到
SHRANK 41 次 / 4,810,942 miss（60.9%）。**但这个判据后来被证伪，见 §2.4 的修正。**

**判据必须是字节级 LCP，不是条数。** 条数变少 ≠ 头部被删（尾部被截同样会让条数变少），
条数变多也不排除头部同时被删。`align3.py` 改用「逐条 (role, len, hash) 的最长公共前缀」：

| 字节级形状（`lcp` = 逐条相同的最长前缀条数） | 次数 | miss | 占总 miss |
|---|---|---|---|
| **`lcp == 0`（头部被删/被改写，缓存前缀归零）** | 78 | 1,895,442 | **24.0%** |
| `lcp > 0`（头部逐条相同） | 606 | 6,011,664 | 76.0% |

miss 高度集中：**65/684 个请求（9.5%）贡献 96.4% 的 miss**（7,620,961 / 7,907,106），
其中 `lcp == 0` 的 78 次只占 23.9%——**剩下的 76.0% 是另一个机制，见 §2.4**。

### 2.3 第二机制：`dh` 恰好卡在 system 块的大小（76.0%）——**本会话未坐实，给出指纹与候选**

用**完整**消息哈希（含 `tool_result` 内容与 tool_calls 参数；早先的 `join.py` 只比
角色+文本长度，**漏掉了 tool_result 内容**，那个判据是错的）重算后：

| 大 miss（≥30K，65 个）的形状 | 次数 |
|---|---|
| `lcp == 0`（头部被删，缓存前缀归零） | 22 |
| `lcp == len(prev)`（**纯尾部追加，头部一条未动**） | 41 |

**41 个大 miss 请求的头部逐条相同、消息列表只是被追加，却仍然 miss 掉几乎整段对话**——
它们在我们这一侧**没有任何可变的东西**。而它们的 `dh` 中位数 = **30,848**，
恰好等于 **system 块的 token 数**（system 字符中位 76,756 ÷ 2.49 ≈ 30,825）：
命中的**只有 system 那一段**，第一条消息之后的缓存全部不见了。

**候选解释（按证据强度排序，均未坐实）**：

1. **provider 侧缓存条目过期/被逐出**（最符合）。system 前缀（system + tools ≈ 31K）
   在整个会话里逐请求复用、且被同一 prompt 的其它调用共享，所以它一直热；
   消息前缀是本会话独有的，在长停顿之后冷掉。证据：这些请求的 `dm` 中位 ≈ 119K
   与「整段重新 prefill」一致，而 `dh` 精确停在 system 边界。
   注意这类请求之前的请求**也**是大 miss（49 个大 append-only miss 里有 43 个的前一请求
   也是 append-only），符合「一段冷区里连续几轮都冷」的形态。
2. **stub 化改写头部边界**：`snapOldToolResults` 把旧 tool_result 换成占位文本
   （长度和内容都变，且 `folded` 不计入 `EstimatedTokens()`）。但 stub 化会改变
   消息内容 ⇒ 应表现为 `lcp` 落在中段，而实测这 41 条是 `lcp == len(prev)`（纯追加）。
   **所以这一候选与证据不符，降级。**
3. **token 级边界错位**（消息合并/拆分）：与 `lcp == len(prev)` 同样不符。

**诚实结论**：76.0% 的 miss 落在我们这一侧看不到任何变更的请求上，
**本会话只能给出「`dh` 精确等于 system 块大小」这个指纹**，不能断定是 provider 的
缓存过期策略（`CONFIG.md` 里没有任何 TTL/keepalive 旋钮；本会话也没有测 TTL）。
**要坐实它必须做一次 TTL 实验**（同一 payload 在 0/10/30/60 分钟后各发一次，看 `dh` 何时掉到
30,848），那是一个独立任务，见 §7.5。

### 2.4 对旧结论的修正（诚实记录）

`join.py` 最初按**消息条数**分类，得出「SHRANK 41 次 = 60.9% of miss」，
并据此写进了 commit `6dbe5a43` 的信息。**那个判据是错的**（条数变少可能只是尾部被截），
改用字节级 `lcp` 判据后头部删除降到 **24.0%**。**代码与测试不受影响**
（摊销逐出消除的正是 78 次 `lcp==0`，在任何判据下都是对的），
但**份额数字以本节为准**：`lcp==0` = 23.9%，其余 76.0% 见 §2.3（未坐实）。

### 2.1 每一类注入的 miss 贡献（epic 要求的「带数字」）

| 注入类别（harness user 消息） | 是否在 wire 上出现 | 紧随其后请求的 miss | 判定 |
|---|---|---|---|
| `CHAT TURN CHECKPOINT`（memory_store 检查点） | **0** 次（该会话 `GOPI_HIPPOCAMPUS` 未开，`chat_ops.go:104-112` 的 nil-host 直接返回） | — | 无贡献 |
| `Continue: you have tools available…`（end-turn continuation） | 0 次 | — | 无贡献 |
| `Round budget almost spent`（轮次预算警告） | 0 次 | — | 无贡献 |
| job 完成通知（`jobNoticeInjectionMessage`） | 0 次 | — | 无贡献 |
| 其它 `[system] …` 注入（task-start search / hot-tier 常驻） | 97 条 user 消息中，其余全为操作员原文 | 中位 miss ≈ 293–325 | **≈3.6%**（619 个小 miss 请求的合计） |

**注入假设的证据**：注入本身是**尾部追加**，`dh`（被缓存的部分）无一次跌落到 31K 基准；
31K 基准只在 65 个大 miss 请求上出现。**注入破坏前缀 = 0 miss 直接归因。**

### 2.2 真正的指纹：31K

大 miss 请求的 `dh` 中位数 = **30,848**（min 30,848 / max 165,632），
而普通请求的 `dh` 中位数 = **140,800**。
31K ≈ **系统提示（83KB）+ 工具 schema（43KB）**——它们在两次请求间**逐字节相同**
（`wire_lcp.py`：712 个请求里只有 52 次 system 块变化，且全部来自独立的不带 tools 的调用）。
所以「只有前 31K 命中」**不可能是 system 变化造成的**，只能是**消息列表本身被重写**。

## 3. 机制（5-why）

1. 为什么只有 31K 命中？→ 消息列表与上一轮不再逐字节相同。
2. 为什么不同？→ 有消息被删/被改。
3. 被谁删？→ `trimContext` 每一轮都触发。
4. 为什么每轮都触发？→ 该会话的上下文被**固定的 per-turn 开销**常年顶在触发线上
   （console.log 里 28 个调用块，全是同一形态：`compaction skipped` → `folded N` → `trimmed N oldest message(s)`；
   其中 1 块连续 trim 13 次）。
5. 为什么这是「每轮」而不是「偶尔」？→ `TrimToFit(limit)` 只删**超出 limit 的那部分**；
   下一轮的追加立刻把它顶回线上，于是**再删一次**，而头部已经前移。
   **头部每轮前移几条 ⇒ 缓存前缀每轮失效一次。**（对比：一次性删到 `CompactTargetRatio` 之下
   才会留下「好几个请求都不触发」的搭便车窗口。）

**代价**：console.log 里 `est 150022 → 70760` 说明一次真删达到 10–14K token，
而大 trim 请求的 miss 在 10K–14K 区间。该会话独有的证据：**473s 的单轮耗时**（每次请求
冷 prefill 数秒）。注意 `folded` 的 stub 并不计入 `EstimatedTokens()`（估算器按 Content 重建），
所以 console 上「folded N → est 不变」是**自洽的**，估值下降只来自真删消息。

## 4. 落地改善 + 回归测试

**改动**：`internal/agent/context.go` 新增 `trimToFitStickyHead(maxTokens, targetTokens)`；
`internal/agent/loop.go` 的 `trimContext` 改用它（`TrimToFit` 原样保留）。

**唯一一条规则（数据只支持这一条）**：删头消息时必须**一次性删到目标水位**，
而不是每轮只削掉超出触发线的那一点。

- 每轮削 → **每一轮**都付一次冷前缀（这就是实测缺陷：8 轮里 6 轮头部变动）；
- 一次摊销 → 只有**第一轮**付（实测 8 轮里 1 轮），之后上下文停在限线之下，
  连续多轮根本不触发 trim ⇒ 这些轮次全部吃缓存。

**为什么不做「先删尾部」**：我最初实现了「尾部优先」，测试立刻把它否掉了——
尾部删除会**先吃掉模型刚生成的最新回复**（`TestTrimContext_DisabledCompactionTrimsOldestOverThreshold`
里最新一条 `assistant-19` 被删掉），而那一轮正是模型正在用的上下文。
缓存省下的钱买不回被删掉的当下轮次，所以最终只保留「摊销」这一条。
**这一条修正来自测试，不是来自设计**（诚实记录）。

**回归测试**（`internal/agent/context_prefix_cache_test.go`，5 个测试全绿）：

- `TestCacheEvictionIsAmortizedNotNibbled` —— **同场景 A/B**：同一份「满上下文 + 每轮增长约一条消息」
  的序列，分别用旧 `TrimToFit` 与新的 `trimToFitStickyHead` 跑 8 轮，
  统计**头部发生变动的轮数**（每变动一次 = 整段对话重新 prefill 一次）。
  实测：`sliding-window=6, cache-aware=1`。断言 `amortized <= 1` 且**严格小于**旧策略。
- `TestCacheEvictionReachesTheTargetWaterline` —— 一次调用必须摊销到 target。
- `TestTrimKeepsTheContextBounded` —— 25 轮后每轮必须回到 limit 之下（不许用无界增长换缓存稳定）。
- `TestTrimKeepsRecencyAndToolPairs` —— 最新一条消息永不被删；不许留孤儿 tool_result。
- `TestTrimContext_DisabledCompactionTrimsOldestOverThreshold`（既有测试，已按新契约更新断言）——
  context window 关闭 compaction 时仍必须把上下文压回限线之下。

**运行**：
```
CGO_ENABLED=0 go test ./internal/agent/ -count=1 -run 'Trim|Cache|Prefix|Compact|Estimate|Context|Measured'
ok  github.com/the operator/gopi/internal/agent
```
**红→绿实测**：把调用点换回 `TrimToFit(limit)` 后
`TestCacheEvictionIsAmortizedNotNibbled` FAIL（`amortized=6`），换回即绿。

## 4b. 未采用的一条改法（诚实记录）

**「注入改写为追加」不成立**：会话全量检查显示 `CHAT TURN CHECKPOINT` /
end-turn continuation / 轮次警告 / job 通知在该会话中**一次都没出现**
（`GOPI_HIPPOCAMPUS` 未开，注入路径本身被 gate 掉）。
而 `attrib.py` 显示注入类消息在 wire 上都是尾部追加，紧随其后请求的 miss 中位数 ≈ 293–325。
所以「中途注入破坏前缀」这个假设**没有可归因的 miss**——不需要为它写补救代码。

## 5. 改前 / 改后真实会话对比

### 5.1 结论

**改后的真实会话命中率本会话未能与改前分离**（诚实）。原因是两个既有缺陷叠加，
把「越线→trim」的窗口挤到几乎不存在，逐条证据见 §5.2。

### 5.2 本会话真实跑过的 A/B（9 组，逐组 per-request 读数见 §5.2b，verbatim 命令见 §9）

| 组 | 窗口 | 上下文 | 结果 |
|---|---|---|---|
| `ab/` | 默认 200000 | 5 轮 echo，~80K token | **未越线**（0 次 trim）⇒ 两二进制同路径，75.39% vs 75.45%（噪声） |
| `ab2_*` | 4000 | 5 轮 | 两个二进制**同时** HTTP 422 |
| `ab3_*` | 20000 | 3 轮 head GOPI.md | 两个二进制**同时** HTTP 422（各 4 次） |
| `real_ab/` | 100000 | 5 轮 × 120KB | 未越线（0 次 trim）⇒ 81.61% vs 81.69% |
| `real_ab2` | 60000 | 5 轮 × 120KB | 未越线（0 次 trim） |
| `real_ab4` | 100000 | 8 轮 | 未越线；86.13% vs 86.12% |
| `real_ab6` | 默认 200000 | 10 轮 | 未越线；两二进制同为 11 请求 |
| `real_ab7` | 60000 | 12 轮 | **fixed 越线 2 次 / base 0 次** —— 见 §5.3 |
| `real_ab8` | 60000 | 260KB payload | **不可比**：同 payload 下 base 拒答埋入指令只跑 4 轮、fixed 跑 6 轮（§5.2b） |
| `real_ab3` / `real_ab5` | 50000 / 100000 | 4 / 9 轮 | base 侧 422 / 同路径（§5.2b） |

### 5.2b 九组 A/B 的 per-request 读数（脚本 `cachehit/ab_summary2.py`）

**为什么必须用 per-request 口径**：`session.jsonl` 的 `cache_hit/cache_miss` 是**累计值**（§6），
末次读数 = 会话平均；把它当「本轮命中率」会让第一轮整段冷 prefill（~29K miss）被后面所有轮稀释
（`real_ab` 就是典型：末次 81.6%，而被稀释掉的 per-request 是 94.0%）。下表第三列是
**逐请求增量**，才是「每轮缓存是否守住」的真值。

| 组 | 窗口 | base per-req | fixed per-req | 判定 |
|---|---|---|---|---|
| `ab` | 200000 | 98.54% | 98.60% | 同路径（未越线） |
| `real_ab` | 100000 | 93.93% | 94.01% | 同路径 |
| `real_ab2` | 60000 | 93.99% | 93.98% | 同路径 |
| `real_ab3` | 50000 | — | 91.10% | base 侧 HTTP 422（§5.4-2），不可比 |
| `real_ab4` | 100000 | 92.91% | 92.90% | 同路径 |
| `real_ab5` | 100000 | 92.92% | 92.88% | 同路径 |
| `real_ab6` | 200000 | 94.85% | 94.95% | 同路径 |
| `real_ab7` | 60000 | 94.15% | **89.80%** | **两边状态分岔**（§5.3），不可比 |
| `real_ab8` | 60000 | 98.23% | 98.12% | **不可比**：见下 |

**`real_ab8` 为什么也不可比（诚实记录，也是本次新发现的一个坑）**：两组拿到的是**同一个**
260KB payload（`prompt8.txt`，其正文末尾埋着一条「跑 12 轮 echo」的祈使句）。
base 侧**拒绝执行**那条埋入指令、只跑了 4 轮就答复；fixed 侧**照做了**、跑了 6 轮。
同一个 prompt 上两边**请求数都不同**（4 vs 6），任何命中率比较都没有意义——
这既是「两边不可比」，也说明**拿一个含祈使句的 payload 做 A/B 本身就会引入行为分岔**。
（该 payload 的构造意图正是「注入假设」的探针，结果它先证明了另一件事：**A/B 的 payload
必须是数据、不能是可执行指令**。）

**结论**：**没有一组**能让两个二进制同时进入「被修的那条路径」（唯一越线的 `real_ab7`
在越线那一刻两边轨迹分岔）。这不是测法不够好，是 §5.4 那两个既有缺陷把可观测窗口堵住了。

### 5.3 越线时能看到什么（`real_ab7`，唯一一组真正触发 trim 的）

```
fixed: 上下文在 46466 token 越过 45000 触发线 → compacted to 26222
       miss deltas: [29K, 2K, 2K, 2K, 2K, 2K, 2K, 14K, 2K, 2K, 14K, 2K, 2K]
base : 从未越线
       miss deltas: [29K, 2K, 2K, 3K, 3K, 3K, 3K, 3K, 3K, 1K, 2K, 0K, 0K]
命中率 fixed 84.91% vs base 88.06% —— **fixed 更低**
```

**这一组的读数是真实的，但它证明的不是「改后更差」**：两个二进制跑的不是同一场景
（fixed 越线 2 次 ⇒ 它带着更长的上下文继续、总 prompt 也多 83K；
base 未越线 ⇒ 它直到结束都没进过被修的那条路径）。**越过触发线之后两边的状态不可比**，
所以这一组不能用作改前/改后对比，只能证明「触发线正好卡在能触发的边缘时，
两边的上下文轨迹会立刻分岔」。

### 5.4 为什么「越线」这么难（根因，两个既有缺陷）

1. **触发线以下没有可达的稳定工作点**。`trimContext` 只有在 `est > limit` 时才动手，
   而 **`est ≤ limit` 的区间里没有任何东西阻止上下文继续涨**——`folded`（
   `snapOldToolResults`）**不计入 `EstimatedTokens()`**（估算器按 Content 重建），
   所以它压不住估算值；`compaction` 又要 `CompactionEnabled` 才跑。
   ⇒ 上下文要么在触发线以下一路涨到越线，要么越线后被大幅削回。
2. **越线之后立刻撞既有 422**。实测：一旦 trim 把消息削到很少（`GOPI_CONTEXT_WINDOW`
   小、或系统提示本身占满窗口），请求体里 `messages` 变成 **`null`**
   （`…/dumps/20260930-194502.dump.json` 的 `original_request.messages = null`，
   API 回 `invalid type: null, expected a sequence`）。baseline 与 fixed **同样**失败
   ⇒ **与本次改动无关的既有缺陷**，但它**恰好堵在「能观测到 trim 效果」的窗口上**。
   §9 的 `GOPI_CONTEXT_WINDOW` 一栏可以看到：4000/20000 触发 422，50000 时 base 422、
   fixed 通过，60000 以上两边都通过——**这个「能跑」的窗口就是 §5.2 里那些未越线的组**。

### 5.6 改后能消除多少 miss —— 真实会话上的**账目上界**（不是模拟出来的命中率）

不做「未来重放」（试过，见下），只做**实测事件的账目**：这次改动能消除的是
**§2.2 里那 78 次 `lcp == 0` 事件中的「段内重复」部分**。

```
真实会话 a09b91ae：685 请求，累计 prompt 93,042,446 token，实测 miss 7,907,106（91.50%）
  lcp == 0 事件              78 次   1,895,442 miss  (24.0%)
  这些事件聚成 37 个连续段    段长 [9,7,4,3,2,2,2,2,2,2,2,2,2,2,2,2,2,2,2,2,2,2,2,1×15]
  每段首个事件               37 次     936,011 miss   ← 摊销后仍需付的"冷前缀"
  段内重复的事件             41 次     959,431 miss   ← 本次改动消除的部分 = 12.1% 的总 miss
  ⇒ 命中率账目  91.50% → 92.52%   (+1.02pp)
  余下 76.0% 是 §2.3 的第二机制（provider 侧，本次改动不触及）
```

**这是账目，不是测得值**：它假设「段内后续 `lcp==0` 事件在新的摊销策略下不再发生」，
而这个前提**只由 §4 的单元 A/B（6/8 → 1/8）支持，未由真实会话支持**（§5.2 那些组没能越线）。
**真实改后命中率仍是未测项**（§7.1）。

**试过但放弃的路线（诚实记录）**：我写过两个轨迹重放脚本
（`replay_ab2.py` / `predict_check.py`：把 wire 快照的逐条消息长度当作 token 账本，
预测 `dh`，再用同一模型重放两种策略）。**它失败了**：预测的 `dh` 与实测相差 31650%
（`predict_check.py` 输出），根因是 `session.jsonl` 的 `prompt_tokens` 是**累计值**，
拿它的差分当「本轮上下文大小」把 chars/token 标定成了 0.009。**标定不成立 ⇒ 重放的数字全部作废**，
两个脚本的产物（`replay_ab.json` / `predict_check.json`）保留在证据目录里，**不作为结论**。
这正说明了为什么 §5.6 只给账目：账目只用**实测到的事件与 miss 数**，不引入任何模型。

## 6. 9000 万级上下文的成因

**结论：是「累计口径」的假象，不是单次请求真有 9000 万 token。**

- 单次请求最大 `prompt_tokens` = **93,042,446** —— 这确实是 9000 万级，但那是**累计值**
  （`a.Cost.Total*`），等于该会话 685 次请求的 `prompt_tokens` 之和。
- 单次请求的**真实**大小：raw.log 里每行请求 payload 的最大长度 = 551,809 字符 ≈ **~138K token**，
  中位 511,927 字符 ≈ ~128K token —— 与 `context_window 200000 × trigger 0.75 = 150000` 触发线吻合。
- 所以「上下文涨到 9000 万」= `session.jsonl` / UI 把累计值当成了单次值（同一 bug family 见 §1）。
  **无天花板增长问题**：估算器 + 触发线确实把它压在 150K 以下。

## 7. Not Done（诚实）

1. **改前/改后真实会话命中率对比未取得**（详见 §5）。9 组真实 `gopi chat --once --tools` A/B
   全部跑过（verbatim 在 §9）：7 组因**未越触发线**而两边同路径（差异是噪声），
   1 组（`real_ab7`）真正触发了 trim，但**两边状态随即分岔**（fixed 越线 2 次、base 0 次），
   读数不可比；1 组（`real_ab8`）因 payload 内含祈使句导致两边**行为分岔**（4 轮 vs 6 轮）。
   逐组 per-request 读数见 §5.2b。**根因是两个既有缺陷**：① 触发线以下没有可达的稳定工作点；
   ② 一旦越线就容易撞上 `messages: null` 的既有 422。两个二进制已留档
   （`…/cachehit/gopi.baseline` = `3b419f8f`、`gopi.fixed`），A/B 脚本与 payload 全部在
   `…/cachehit/real_ab*/`。
2. **`tool_result` 计费假设未验证**：本文的「31K ≈ system 块」由算术吻合得出
   （system 字符中位 76,756 ÷ 2.49 ≈ 30,825 ≈ 实测 `dh` 30,848）；确定性的验证应当用
   **离线 tokenizer** 对同一 payload 计费，本会话未做。
3. **注入路径未跑通**：该会话 `GOPI_HIPPOCAMPUS` 关闭，所以 `CHAT TURN CHECKPOINT` / 轮次警告
   等注入在本会话中**一次都没出现**——「它们不破坏前缀」这一条是**基于事件缺失**的结论，
   不是基于它们真实出现过。需要另开一个开 hippo 的会话才能实测这几类注入。
4. **新发现的既有缺陷（建议另开任务）**：`gopi chat --tools` 在上下文被削到很小时发出
   `messages: null` 的 422（`…/dumps/20260930-194502.dump.json` 的
   `original_request.messages == null`）。它**掩盖**了本次改善的验证，优先级高于本次改动的验证。
5. **§2.3 的第二机制（76.0% 的 miss）未坐实**：`dh` 精确停在 system 块大小（30,848），
   我给出的候选是 **provider 侧缓存过期**，但本会话**没有测 TTL**（`CONFIG.md` 里也没有
   TTL/keepalive 旋钮）。要坐实它需要一次 TTL 实验：**同一 payload 在 0/10/30/60 分钟各发一次，
   看 `dh` 何时从 ~140K 掉到 30,848** —— 这是一个独立任务。
6. **§5.4 的「触发线以下无稳定工作点」的修法未做**：`folded` 不计入估算值、
   触发线以下无节流，是本次改动**没有触及**的相邻缺陷。

## 8. 证据清单

`/home/user/.gopi/sessions/01a0f206-9ed0-7735-8348-0fa44b9b4000/cachehit/`
- `attrib.py`：per-request 增量归因（§1/§2.1）
- `align3.py` → `rows3.pkl`：**字节级 lcp 判据**的 wire 分类（§2.2 现行判据）
- `join.py` → `joined_rows.json`：条数判据的旧分类（§2.4 记录其被证伪）
- `diff2.py`：**完整消息哈希**（含 tool_result 内容）的逐条差异定位（§2.3）
- `wire2.py` → `snaps2.pkl`、`wire_lcp.py`：wire 快照与 system 稳定性统计
- `gopi.baseline` / `gopi.fixed`：改前/改后二进制（`3b419f8f` / 本分支）
- `ab/`、`ab2_*`、`ab3_*`、`real_ab/`、`real_ab2/`、`real_ab3/`、`real_ab4/`、`real_ab5/`、
  `real_ab6/`、`real_ab7/`、`real_ab8/`：11 次真实 `gopi chat --once --tools` 的
  stdout/stderr/GOPI_HOME（含 422 复现与唯一一次越线），§5.2 的全部读数来自这些目录
- `prompt3.txt` … `prompt8.txt`：A/B 用的任务提示词
- `ab_summary2.py`：9 组 A/B 的三口径对照（§5.2b）
- `replay_ab.json` / `predict_check.json`：策略重放的中间结果（**未采用**，见下）

---

## 9. Rule 31 — Ran（verbatim，本会话实际执行过的命令）

```bash
# 归因（每类注入的 miss 数字 + wire 真值 join）
python3 /home/user/.gopi/sessions/01a0f206-9ed0-7735-8348-0fa44b9b4000/cachehit/attrib.py
python3 /home/user/.gopi/sessions/01a0f206-9ed0-7735-8348-0fa44b9b4000/cachehit/join.py
# join.py（条数判据，已被证伪）: SHRANK n=41 miss=4,810,942 = 60.9%（§2.4 修正为 24.0%）
# align3.py（字节级 lcp 判据，现行）:
python3 /home/user/.gopi/sessions/01a0f206-9ed0-7735-8348-0fa44b9b4000/cachehit/align3.py
#   输出: HEAD(lcp==0) n=78 miss=1,895,442 = 24.0% ; append-only n=605 ; mid-list n=1
#   总计 miss 7,907,106 / 684 增量；65 个请求(9.5%)贡献 96.4% 的 miss
python3 /home/user/.gopi/sessions/01a0f206-9ed0-7735-8348-0fa44b9b4000/cachehit/diff2.py   # 完整哈希(含 tool_result) 的逐条差异定位
python3 /home/user/.gopi/sessions/01a0f206-9ed0-7735-8348-0fa44b9b4000/cachehit/ab_summary2.py
#   9 组 A/B 的三口径对照（per-request 增量 / 累计和 / 末值）→ §5.2b
#   ab 98.54/98.60 ; real_ab 93.93/94.01 ; real_ab4 92.91/92.90 ; real_ab7 94.15/89.80 ;
#   real_ab8 98.23/98.12（base 4 reqs vs fixed 6 reqs — 行为分岔，不可比）

# 改前/改后真实 chat 会话（7 组，两个 GOPI_HOME 隔离，全部落在 session evidence 目录下）
EV=/home/user/.gopi/sessions/01a0f206-9ed0-7735-8348-0fa44b9b4000/cachehit
GOPI_HOME=$EV/real_ab/base  GOPI_CONTEXT_WINDOW=100000 $EV/gopi.baseline chat --once --tools -p "$(cat $EV/prompt3.txt)"
GOPI_HOME=$EV/real_ab/fixed GOPI_CONTEXT_WINDOW=100000 $EV/gopi.fixed    chat --once --tools -p "$(cat $EV/prompt3.txt)"
# base 6 reqs miss 40,665 / fixed 6 reqs miss 40,522 —— 两组 0 次 trim（未越线，噪声）
GOPI_HOME=$EV/real_ab7/base  GOPI_CONTEXT_WINDOW=60000 $EV/gopi.baseline chat --once --tools -p "$(cat $EV/prompt7.txt)"
GOPI_HOME=$EV/real_ab7/fixed GOPI_CONTEXT_WINDOW=60000 $EV/gopi.fixed    chat --once --tools -p "$(cat $EV/prompt7.txt)"
# base 13 reqs 0 次 trim，88.06% ; fixed 13 reqs 越线 2 次(46466>45000 → compacted 26222)，84.91%
#   —— fixed 的 miss deltas [29,2,2,2,2,2,2,14,2,2,14,2,2]K 可见摊销的代价与收益
GOPI_HOME=$EV/real_ab8/base  GOPI_CONTEXT_WINDOW=60000 $EV/gopi.baseline chat --once --tools <$EV/prompt8.txt
# 长 prompt 必须走 stdin（argv 上限：`/usr/bin/timeout: Argument list too long`）

# 回归测试 + 红→绿
CGO_ENABLED=0 go build ./...                       # BUILD_OK
CGO_ENABLED=0 go build -o $EV/gopi.fixed ./cmd/agent/   # BUILD_FIXED_OK
CGO_ENABLED=0 go test ./internal/agent/ -count=1 -run 'Trim|Cache|Prefix|Compact|Estimate|Context|Measured'
# ok github.com/the operator/gopi/internal/agent 2.070s
# TestCacheEvictionIsAmortizedNotNibbled 日志：sliding-window TrimToFit=6, cache-aware trimToFitStickyHead=1
# 把 loop.go 调用点换回 a.Ctx.TrimToFit(limit) 后同一测试 FAIL(amortized=6) —— 红→绿已实测
CGO_ENABLED=0 go test ./internal/agent/ -count=1
# 仅 2 个既有失败：TestDocAnchorsResolveInNormativeDocs / TestScreenCaptureDeclarationsStayBelowDocAnchors
# —— 已用 pristine master 文件复测，两者同样失败 ⇒ 与本改动无关

./bin/gopi doccheck     # devdoc corpus consistent
git status --porcelain  # 空
```

## 10. 交付物与位置

| 交付物 | 位置 |
|---|---|
| 归因报告（per-request + wire join） | 本文 §1–§3；脚本见 §8 |
| 改善代码 | `internal/agent/context.go`（`trimToFitStickyHead`）、`internal/agent/loop.go`（调用点） |
| 回归测试 | `internal/agent/context_prefix_cache_test.go`（5 例） |
| 既有测试的契约更新 | `internal/agent/trim_threshold_test.go` |
| 9000 万成因结论 | 本文 §6 |
| devdoc 报告 | 本文件（已登记 `devdoc/DOC-INDEX.tsv`，`gopi doccheck` 通过） |
