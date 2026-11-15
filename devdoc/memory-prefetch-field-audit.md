<!-- doc-class: normative -->
# memory_prefetch 实测审计：四个 brief 缺陷的真/假判定、记录正确性修复、缺陷 2/3 的归属

任务 `01a115ce-4209-73bf-a159f631a3a2cfaa`，分支 `task/01a115ce420973bfa159f631a3a2cfaa`，
工作树 `/home/user/workspace/gopi-01a115ce420973bfa159f631a3a2cfaa`（Rule 6a —— 本次运行自己的）。
本机 `hostb-Laptop`（client）。审计基准 rev `12d50350fca4b256e6c401d4b629ac507200f455`，
日期 `2026-10-07`（Asia/Shanghai）。

**baseline 澄清（必须先说）**：brief 自己的证据文件
`~/.gopi/sessions/275b5100-eee1-42fe-905f-35eeedf83342/memory-prefetch.jsonl`
**在本机不存在** —— 它是 peer1 上的会话。本机只有 3 个记录文件、共 164 条记录。
所以 brief 的每一个数字都是**另一台机器、另一份语料**上的测量；本报告的数字全部在本机重跑，
并且结论只建立在本机语料上。这正是 reframe 里写死的那条约束。

---

## 一、功能确认（本机重跑）

生成器（Rule 38：报告里的数字来自这个脚本，可重跑）：

```bash
python3 /home/user/.gopi/sessions/01a115ce-4209-73bf-a159-f631a3a2cfaa/artifacts/census.py
# -> artifacts/census.txt
```

Rerun 命令与输出（逐字）：

```
$ python3 artifacts/census.py
date: 2026-10-07T18:17:53+08:00
git rev: 12d50350fca4b256e6c401d4b629ac507200f455
record files: 3  records: 164
empty record files: 1
outcome: {'hits': 117, 'adopted': 35, 'superseded': 12}
adopted: {'yes': 129, 'no': 35}
source: {'(absent)': 128, 'human': 34, 'notify': 2}
lookup lines: 129  adoption lines: 35
lookup lines with hits==0: 12
lookup lines with hits>0: 117
lookup hit rate: 90.7%
lookup ms: avg 467 min 0 p50 421 p90 933 max 1162
lookup lines with ms==0: 12
  ^ of those, empty-query: 12  non-empty-query: 0
queries asked more than once: 32
  within those groups: typing-path lookups = 28  turn-boundary lookups = 15
typing-path lookups per distinct query: {1: 86, 2: 4, 12: 1}
```

`outcome × adopted × source` 交叉表（本机，164 条）：

| outcome | adopted | source | 条数 |
|---|---|---|---|
| hits | yes | (absent，打字路径) | 94 |
| hits | no | human（轮边界） | 22 |
| adopted | yes | (absent) | 22 |
| adopted | yes | human | 12 |
| superseded | no | (absent) | 12 |
| hits | no | notify | 1 |
| adopted | yes | notify | 1 |

**功能确实在跑**：129 条 lookup 行里 117 条有命中（90.7%），p50 421ms、p90 933ms、
max 1162ms（预算 1500ms，**没有一条 gave-up**）；35 次采纳真的把段塞进了 prompt
（`section_bytes` 334–348B）；两条 source=notify 的行证明轮边界那条路也在跑。

---

## 二、四个 brief 缺陷逐条判定

判定方法：**本机 164 条记录 + 代码行**。判定标准是「这个字段能不能回答它名字所问的问题、
消费者读到它会不会被误导」，不是「值看起来对不对」。

### 缺陷 1（brief 称"最严重"）：`hits=0` 时仍标 `adopted="yes"` —— **误读（真缺陷是另一个）**

brief 的测量：peer1 会话 13 个 hits=0 的轮次里 9 个 adopted=yes。
**本机同一命令**（brief 自己给的那段 python）跑出来是 47/35。

**判定：这个计数是 `outcome=adopted` 事件行**，不是"没有命中却报已采纳"。
证据（**这条判定我第一版写错了，这里给出改正后的逐字复核**）：本机 164 条里
`hits=0` 的行确实是 **47** 条，其中 `adopted=yes` 的 **35** 条 —— 但那 **35 条全部是
`outcome=adopted` 的采纳事件行**（`take()` 写的，`chat_prefetch.go:466`），
不是检索行；余下 12 条 `hits=0` 是 `outcome=superseded` 且 `adopted=no`。
**`outcome=hits` 的行里 `hits=0` 的条数是 0**（117 条 hits 行全部 hits>0）。

第一版那句"129 条 lookup 行 adopted 全 `no`"是错的，实测反例：`outcome=hits` 行里
`adopted=yes` 有 **94** 条。这**不**推翻判定，反而正是判定本身 ——
`fetch()` 在 `chat_prefetch.go:491` 调 `p.session.Use(query, section)` 拿到的返回值才是
`Adopted`，所以 lookup 行的 `adopted=yes` 的意思是"**这次检索的那一段真的进了 prompt**"，
它**只可能**出现在 `hits>0` 的行上（`Use` 拿到空段就返回 false）。

所以规则要按 (outcome, hits) 联合键说，不能按 `adopted` 一个字段说：

| 行的 kind | `adopted` 的含义 | 实测 |
|---|---|---|
| `outcome=hits`（检索行） | 这次检索的段有没有进 prompt | 94 yes / 23 no，**全部 hits>0** |
| `outcome=superseded`（检索行） | 恒 `no`（`Use` 拒绝了过期段） | 12 no |
| `outcome=adopted`（采纳事件行） | 恒 `yes`（这行就是采纳本身） | 35 yes |

两个 kind 是分开的行；**`adopted=yes 且 hits=0` 的组合在检索行上不存在**，
只出现在采纳事件行上 —— 而那一行的 `hits=0` 是零值被序列化，不是测量。
这正是缺陷 1' 的根因：**采纳行的 `hits=0` 让一个按字面数 hit 分布的普查把 35 条采纳
当成了 35 次空检索**。

代码行：`cmd/agent/chat_prefetch.go:466`（`take()` 写采纳行，`Adopted: prefetchRecordAdoptedYes`）
与 `:385`（`fetch()` 写 lookup 行，`Adopted: boolLabel(adopted)`），
轮边界对应 `cmd/agent/chat_prefetch_turn.go:284` 与 `:306`。

**brief 的误读在哪**：它把两种事件行合起来数 hit 分布。`take()` 写的行**根本没有测量 hits**，
`hits` 是结构体零值被序列化出来的 0。brief 作者自己在按字面读一个"没测过"的字段。

**但那个读误本身是一个真缺陷** —— 而且比 brief 说的更宽（见下）。
所以缺陷 1 的**结论**：brief 的机制描述错（不是"没有命中却报已采纳"），
brief 指向的**位置对**（`adopted`/`hits` 的语义确实坏了，坏在记录形态而不是采纳逻辑）。

### 缺陷 1'（brief 没提，本次实测发现的同一个根因）：未适用字段与真值 0 不可区分 —— **真缺陷，已修**

`prefetchRecord` 的 `hits`/`ms` 是裸 `int`，采纳行写它时留零值，于是**每一条采纳事件都伪装成
一次 hits=0/ms=0 的检索**。`query` 同理：12 条 superseded 行全部渲染成 `"query":""`，
读起来像"问了空字符串"。

这**就是 Rule 15a 点名的形态**（required semantic value lost at the zero value），
而且它的代价在本报告第一节已经量化：一个问"有多少次检索什么都没找到"的普查，
在这个字段上会得到 47（其中 35 条是采纳行），而真答案是 12。**一个字段让分析给出错答案。**

修法与验证见第四节。

### 缺陷 2：重复检索（brief 称 65%）—— **计数误读 + 计数为真、机理误读**

本机：32 组 query 被问过不止一次，涉及 78 条记录。

**先拆开这 78 条**（这是 brief 数的 26 条重复的同一现象）：

| 成分 | 条数 | 是不是重复检索 |
|---|---|---|
| 轮边界 lookup（`source=human/notify`） | 15 | **不是** —— 打字路径给了 1 条 hits，轮边界在同一条 query 上又检索了一次 |
| 打字路径 lookup | 28 | 见下 |
| 采纳事件行 | 35 | **不是** —— 采纳是另一个事件，不是第二次检索 |

打字路径那 28 条再拆：`{1 次: 86 个 query, 2 次: 4 个 query, 12 次: 1 个 query}`。
**那个"12 次"是 12 条 `query=""` 的 superseded 行**（同一条空 query 键），不是同一个 query 检索 12 次。
所以**真正的打字路径重检索只有 4 个 query × 各 1 次多余 = 4 次**。

**机理（代码行）**：重复不可能来自打字路径自己的去抖 ——
`internal/memoryprefetch/state.go:173` 的 `delta()` 只保留 `!Satisfied[t] && !Excluded[t]` 的词，
而查询字符串就是 delta（`state.go:165` `strings.Join(delta, deltaSeparator)`），
所以**同一个 (行, 词集) 不可能产生两次同样的 query**。
真正会重复的只有一条缝：**轮边界用的是另一个状态机**
（`cmd/agent/chat_prefetch_turn.go` 的 `p.turnState`，见 `chat_prefetch.go` 里
"a second State rather than the typing one" 的设计注释），它自己的 delta 独立演化，
于是同一条行上的词会被**打字路径检索一次、轮边界再检索一次** —— 这正是那 15 条。

**判定**：brief 的"65% 花在重复上"是**把三种不同事件合成一个计数**得出的；
按事件拆开后，真正的浪费是每条 prompt 一次（打字段被轮边界段取代，`discardTypingSection`）
—— 也就是上面"采纳事件 35 条 vs 非 superseded lookup 129 条"那个比。
**归因：不是去重缺陷，是"同一行在一次 seal 里被两个状态机各检索一次"的设计后果。**
修法需要动轮边界状态机的公共契约（系统层，Rule 28），超出本会话 → `## Not Done (honest)`。

### 缺陷 3：中文 query 被切成重叠碎片 —— **真缺陷，且根因归属明确：`internal/memoryprefetch` 的切分器，不是 `internal/cjkseg`**

本机实测（census.py 原文）：

> **Retired mechanism (2026-11-05).** The `gopi task sync` family is DELETED;
> the query strings quoted in this capture are kept verbatim as the record of
> what was typed when the mechanism existed, and they route to a removal
> sentence now. Work that must run on another host is FILED there with
> `gopi task submit`.

```
queries containing CJK: 108
    task sync 目前我们的 目前 前我 们的 是被禁止的 是被
    task sync peer1 bug claim race condition 目前我们的
```

`query` 是**检索键**（`state.go:165`），由 `internal/memoryprefetch/extract.go` 的
`candidateTerms` 产出：CJK run 整段作为一个候选**之外**，还对整段滑窗产出 2-rune 二元组。
这是 extract.go 里**自己写明的降级路径**，代码行：

- `internal/memoryprefetch/extract.go`：`KindCJKBigram = "cjk-bigram-fallback"`，
  注释写着 "It is a fallback, not a tokenizer: it is recorded as a degradation on the set"。
- 同一文件 `DegradationCJKTokenizerGap` 的 Detail 逐字承认：
  "no CJK segmenter in this project"。

**归属判定（brief 要求的最重要一步）——三问三答：**

1. **那个分词分支合并进 master 了吗？** 合并了。
   ```bash
   $ git merge-base --is-ancestor 3099ba919 HEAD && echo "IS ANCESTOR of HEAD"
   IS ANCESTOR of HEAD
   $ git log -1 --format='%h %ci %s' 3099ba919
   3099ba919 2026-10-07 11:17:35 +0800 feat(memory): jieba_token carries real segmentation (gse); distilled_qa_fts folds stems
   ```
   另有 `69ff2586a 2026-10-06 fix(chat): segment the memory prefetch, warm one handle per session, retry a locked ledger`
   **同样是 HEAD 的祖先** —— 但它的 `--stat` 只碰
   `cmd/agent/chat_prefetch.go`、`chat_prefetch_segments_test.go`、DOC-INDEX、一个 devdoc；
   commit message 里的 "segment the memory prefetch" 指的是**分段计时（segments）**，
   不是 CJK 分词。名字撞车，别被它骗。

2. **既然合并了，为什么 prefetch 的 query 还是碎的？** 因为**两套切分在两个不同的包，而且各管一段**：
   `3099ba919` 引入的 `internal/cjkseg`（gse + 内嵌词典）只被 `internal/tools` 用
   （`internal/tools/memory.go:31` import，用于 `jieba_token` 列 / 建 FTS 索引）；
   **`internal/memoryprefetch` 一个 cjkseg 符号都不 import**（实测 grep：0 命中）。
   `cjkseg.Words(s) ([]string, error)` 是现成的、已经是 master 的一部分、零新增依赖。

3. **所以是不是那个分支的下游？** **不是"未合并"，是"没接线"**。
   这是 Rule 8 里"机制存在但没接线"的第二个实例（第一个是 `memoryprefetch.Step` 自己）。
   真实缺口：给 `extract.go` 的 CJK 分支接上 `cjkseg.Words`，把 bigram 滑窗从默认路径降为
   **cjkseg 不可用时的降级**。

**判定：真缺陷。** 修法不是新写切分器（Rule 8 明令禁止），而是把既有 `internal/cjkseg`
接进 `internal/memoryprefetch`；改的是**检索键的形状**（会影响 `memoryprefetch` 的
全部候选/门控/差量测试），并可能动 `internal/config` 侧的开关 → 系统层（Rule 28）→
超出本会话 → `## Not Done (honest)` + 子任务。

### 缺陷 4：空 query 与 ms=0（未发查询）未被区分报告 —— **真缺陷，已修**

本机精确定位：`ms==0` 的 lookup 行 **12 条，全部是 `query` 缺失的 superseded 行**
（"empty-query: 12 / non-empty-query: 0"）。也就是说本机**不存在** brief 说的那种
"有 query 却 ms=0 的神秘跳过行" —— 在 peer1 语料上那 9 条很可能同样是 superseded
（brief 自己数的是"query 为空串的 2 条 + 其余 9 条有 query"，而空串与缺失在旧形态下
**无法区分**，这正是缺陷本身使它数不清的原因）。

**判定：真缺陷，而且 brief 的"跳过 vs 无命中不能共用一个记录形态"完全正确。**
形态上：`superseded` 的语义是"检索跑完了、结果被更新的行取代"，
而有 12 条 superseded 行是 `discardTypingSection` 写的 —— 那是一条**根本没发查询**的路径
（`chat_prefetch_turn.go:333`）。它和"检索被取代"共用一个 outcome，且带一对伪造的 `hits/ms`。

修法与验证见第四节。

---

## 三、缺陷总表

| # | brief 的说法 | 本机判定 | 依据 |
|---|---|---|---|
| 1 | `hits=0` 却 `adopted=yes` 是"没命中却报采纳" | **误读**（计数把两种事件行合并）+ 指向的真缺陷已修 | 本机 47 条 `hits=0` 里 35 条 adopted=yes **全是 `outcome=adopted` 采纳事件行**、另 12 条是 superseded；`outcome=hits` 且 `hits=0` 的检索行 **0 条**（117 条 hits 行全部 hits>0） |
| 1' | （brief 未提）未适用字段伪装成真值 0 | **真缺陷，已修** | 采纳行 35/35 带 `"hits":0,"ms":0`；Rule 15a |
| 2 | 65% 重复检索 | **计数误读**（78 条里 35 是采纳行、28 里 12 是空 query 键）+ 真机理另说 | `delta()` 保证同词集不重复；真重复是 typing + turn 两个状态机各检索一次（15 条） |
| 3 | 中文被切碎成重叠碎片 | **真缺陷** | `extract.go` 的 `cjk-bigram-fallback`；`internal/cjkseg` 已是 master 祖先但 memoryprefetch 未接线 |
| 4 | 空 query / ms=0 未区分 | **真缺陷，已修** | 12 条 superseded 全带伪造 `hits/ms` + `"query":""` |

---

## 四、记录正确性修复（Rule 15a）

### 改了什么

`cmd/agent/chat_prefetch.go` 的 `prefetchRecord`：

- `Query string` → `Query string \`json:"query,omitempty"\``
- `Hits int` → `Hits *int \`json:"hits,omitempty"\``
- `Ms int64` → `Ms *int64 \`json:"ms,omitempty"\``

**为什么是指针而不是"少写一个字段"**：`hits=0` 是真值（"这次检索没找到东西"，12 条），
`ms=0` 也是真值（快），所以**不能删字段**，只能让"没测量"变成**键不存在**。
`encoding/json` 的 `omitempty` 在指针上判的是 nil —— 这是"未适用"唯一能在 JSON 里说出口的拼法。
消费者若忽略键，读到 0，与旧行为一致，**不破坏任何既有读取方**。

**为什么是逐字段而不是整行分型**（我考虑过 `sql.NullString` 式整行变体，否决了）：
整行分型会顺带把 `query` 从 **lookup 行**里也删掉 —— 实测 4 个 lookup 生产者里 3 个设了 query
（`fetch()`、`startTurnLookup()`、以及它们各自的字段），而 `take()` 那条**按设计没有 query**。
逐字段 omitempty 让"哪一行该有什么"由生产者自己决定，正是我们想要的精度。

### 顺带修掉的一个假断言（本次实测发现的第二处真缺陷）

`cmd/agent/chat_prefetch_test.go:150` 原来这样断言采纳：

```go
if !strings.Contains(rec, prefetchRecordAdoptedYes) || !strings.Contains(rec, prefetchOutcomeHits) {
```

它找的是 `rec` 里有没有 `hits` 这个**字符串**。而采纳行上那对伪造的 `"hits":0` 让这个断言
**永远为真** —— 检索一次都没跑也能通过。修复后它立刻变红，这就是证据。
现在它走 `p.fetch(query)`（真正的 lookup 路径），钉的是"真的检索过"。

### 回归测试（红→绿可复现）

新文件 `cmd/agent/chat_prefetch_adoption_semantics_test.go`。判据刻意用**键集**而不是值
（值读不出"缺失"与"0"的区别 —— 那正是被修的歧义），并钉住**两半契约**：

- 采纳行（轮边界门 + 打字门两个入口各一条）**不得携带** `hits`/`ms`，
  但**必须**保留 `adopted` 与 `section_bytes`（删掉伪造不要连它自己的测量一起删）。
- **lookup 行必须保留写出来的 0** —— 防止修法蔓延成"值等于零就删字段"，
  那会把"什么都没找到"重新变回"没测量"，同一个缺陷换个符号。
- superseded 行不得再带 `"query":""`。

**红证（未修复的树上，逐字）**：

```
--- FAIL: TestTurnAdoptionLineCarriesNoFabricatedMeasurement
    chat_prefetch_adoption_semantics_test.go:167: turn-boundary adoption: the adoption line carries "ms":0 — an adoption event measured no lookup, so the field must be ABSENT (Rule 15a: ...)
    chat_prefetch_adoption_semantics_test.go:167: turn-boundary adoption: the adoption line carries "hits":0 — an adoption event found no hits itself, so the field must be ABSENT
--- FAIL: TestTypingAdoptionLineCarriesNoFabricatedMeasurement
    chat_prefetch_adoption_semantics_test.go:202: typing adoption: the adoption line carries "ms":0 ...
--- FAIL: TestQuerylessEventDoesNotReadAsAnEmptyQuery
    chat_prefetch_adoption_semantics_test.go:247: a query-less event still carries "query"="" — ...: {"query":"","hits":0,"ms":0,"outcome":"superseded","adopted":"no"}
```

**绿（修复后）**：`scripts/test-budget.sh ./cmd/agent/ -run 'Prefetch|Adoption|Turn'` —— 除一条
**预先存在的红**外全绿，见第五节。

---

## 五、Ran: / Result:（Rule 5/31）

规则要求的是**真跑被改动的面**。被改的面是 `gopi chat` 的 prefetch 记录，
所以用**真二进制 + 真 pty** 跑，并且做 A/B：同一行字、同一份 ledger 副本、两个二进制。

- A 臂 = 基线二进制（`12d50350f` 原样工作树构建）
- B 臂 = 本分支修复后

驱动脚本（不提交，Rule 37；落在本运行 session 目录的 artifacts/）：

```
Ran (A): export GOPI_MEMORY_DB=/home/user/workspace/gopi-test-scratch/prefetch-audit/ledger.db
         python3 artifacts/pty-drive-prefetch.py /home/user/workspace/gopi-base-01a115ce/bin/gopi-base \
                 /home/user/workspace/gopi-sandbox/sandbox-audit-prefetch-base-b1658592
Ran (B): python3 artifacts/pty-drive-prefetch.py .../bin/gopi-fixed \
                 /home/user/workspace/gopi-sandbox/sandbox-audit-prefetch-fixed-1c30c3fe
```

两次都是真 pty（`pty.fork()`），等 REPL 提示符出现后才逐块打字（10 字符/块、350ms 间隔，
让 100ms 采样器看到**稳定的行**），然后回车封印这一轮，再 `/exit`。
两个沙箱都是 `gopi tool sandbox create`（Rule 34）建的，`GOPI_MEMORY_DB` 指向
操作者 185MB ledger 的**副本**（`/home/user/workspace/gopi-test-scratch/prefetch-audit/ledger.db`，
device 66309 ≠ `/` 的 66307，Rule 22）—— 预取只读，但**不拿操作者的活库做实验**。

**证据文件的落盘更正（Rule 15：拷贝器也不能静默失败）**：一次 chat 运行会在沙箱里留下
**多个** session 目录，pty 驱动那一次就在一个目录留下 0 字节的 `memory-prefetch.jsonl`、
在另一个留下真记录；`run-ab.sh` 原本用
`find … -name memory-prefetch.jsonl -exec cp {} DST \;`，于是拷到的是**遍历最后命中的那一个**
—— 本次就是一个 0 字节文件，`arm-a-record.jsonl` / `arm-b-record.jsonl` 一度都是 0 字节。
已按显式路径从真记录恢复，并把 `run-ab.sh` 的选择条件改成 `-size +0c`：

```
arm-a (BASE) : …/sandbox-audit-prefetch-base-b1658592/.gopi/sessions/2c5fba78-…/memory-prefetch.jsonl  1020 bytes
arm-b (FIXED): …/sandbox-audit-prefetch-fixed-1c30c3fe/.gopi/sessions/2cd47368-…/memory-prefetch.jsonl   961 bytes
```

下面引用的两臂记录与这两个文件**逐字一致**（`diff` 为 0 行差异）。
这条更正本身就是本节 A/B 的一个副产品：**"没有证据"与"证据存在但被拷丢了"必须可区分**，
所以 artifacts/ 下另有 `record-file-note.txt` 记下这个坑。

**Result（A 臂 = 改动前，逐字）**：

```json
{"query":"gopi task ledger merge conflict auto rework feature","hits":1,"ms":28,"outcome":"hits","adopted":"yes","segments":{...,"handle":"session","journal_mode":"wal","query_only":1,"busy_timeout_ms":250,"attempts":1},"candidates":"ladder"}
{"query":"gopi task ledger merge conflict auto rework feature","hits":1,"ms":32,"outcome":"hits","adopted":"no","source":"human",...}
{"query":"gopi task ledger merge conflict auto rework feature","hits":0,"ms":0,"outcome":"adopted","adopted":"yes","source":"human","section_bytes":348}
{"query":"gopi task ledger merge conflict auto rework feature","hits":0,"ms":0,"outcome":"adopted","adopted":"yes","section_bytes":348}
{"query":"","hits":0,"ms":0,"outcome":"superseded","adopted":"no"}
```

**Result（B 臂 = 改动后，逐字）**：

```json
{"query":"gopi task ledger merge conflict auto rework feature","hits":1,"ms":44,"outcome":"hits","adopted":"yes",...}
{"query":"gopi task ledger merge conflict auto rework feature","hits":1,"ms":37,"outcome":"hits","adopted":"no","source":"human",...}
{"query":"gopi task ledger merge conflict auto rework feature","outcome":"adopted","adopted":"yes","source":"human","section_bytes":348}
{"query":"gopi task ledger merge conflict auto rework feature","outcome":"adopted","adopted":"yes","source":"human","section_bytes":348}
{"outcome":"superseded","adopted":"no"}
```

**形态差异（这就是修复的证据）**：

| 行 | 改动前 | 改动后 |
|---|---|---|
| 采纳行 | `"hits":0,"ms":0` | 两个键**不存在**，`section_bytes:348` 仍在 |
| superseded 行 | `"query":"","hits":0,"ms":0` | 三个键都不存在 |
| lookup 行 | `"hits":1,"ms":28` | **一字未变**（真值 0 照旧保留） |

**A/B 还证明了一件 brief 没料到的链路**：同一条行**确实被两个状态机各检索一次**
（一行 `source` 缺失 = 打字路径，一行 `source:"human"` = 轮边界），
随后 `discardTypingSection` 把打字段记成 superseded —— 这就是缺陷 2 的机理，真跑看到了。
另外两臂都只出现 `hits`/`adopted`/`superseded`，**没有 gave-up**（ms 28–44，远在 1500ms 预算内）。

### 5.1 交付二进制上的最终验证（从已提交的树重建后重跑）

A/B 用的是 `bin/gopi-fixed`（修复当时构建的）。为了验**交付物本身**，
在报告与修复都已提交之后，从干净树**重建**二进制并重跑一次（Rule 31：跑被改动的面，
且跑的是要交出去的那一个）：

```
Ran: CGO_ENABLED=0 go build -o bin/gopi-final ./cmd/agent/            # 64252579 bytes
     # 交付当时的 rev 记成了 e56b29f14 —— 该 hash 在本分支历史里已不可达（§9 改正）
     SB=$(gopi tool sandbox create --label prefetch-final --task-id 01a115ce420973bfa159f631a3a2cfaa | tail -1)
     export GOPI_MEMORY_DB=/home/user/workspace/gopi-test-scratch/prefetch-audit/ledger.db
     python3 artifacts/pty-drive-prefetch.py "$PWD/bin/gopi-final" "$SB"     # 真 pty, exit=0
     # sandbox: /home/user/workspace/gopi-sandbox/sandbox-prefetch-final-8dd9e496
```

**Result** —— 记录落在
`…/sandbox-prefetch-final-8dd9e496/.gopi/sessions/4d1ee605-…/memory-prefetch.jsonl`，
逐行 **键集**（缺键 = 该 kind 不测量它）：

| outcome | adopted | 该行的键 |
|---|---|---|
| `hits` | yes | `adopted candidates hits ms outcome query segments` |
| `hits` | no | `adopted hits ms outcome query segments source` |
| `adopted` | yes | `adopted outcome query section_bytes source` |
| `adopted` | yes | `adopted outcome query section_bytes` |
| `superseded` | no | `adopted outcome` |

**这就是修复要的形状**：检索行的测量（`hits`/`ms`/`segments`）**在**，
采纳行**没有** `hits`/`ms` 而保留它自己的 `section_bytes:348`，
而被丢弃的打字段只剩 `outcome`/`adopted` —— 三个键集的差别**由 KIND 决定，不由值决定**。
对照 A 臂（改动前）：采纳行是 `hits:0, ms:0`、superseded 行是 `query:"", hits:0, ms:0`。

**一次读错动词的教训（记下来，Rule 15）**：第一次跑这一节时写的是 `gopi sandbox create`，
它在当前二进制里**已退役**，只打印 `gopi: \`sandbox\` is RETIRED — use \`gopi tool sandbox\``
到 stderr 并以 exit 1 结束，于是 `SB` 为空、驱动脚本拿空目录跑、`driver exit=1`，
最终 `find` 什么都没找到。**正确动词是 `gopi tool sandbox create`。**
这一类失败的危险在于：如果只看"没找到记录"，会误判成"本次运行没有产生证据"。

---

## 六、构建与测试

```
Build: CGO_ENABLED=0 go build -o bin/gopi-fixed ./cmd/agent/   -> OK (64252579 bytes)
       CGO_ENABLED=0 go build -o bin/gopi-base  ./cmd/agent/   -> OK (64241387 bytes, 基线对照)
       go build ./...                                          -> OK (exit 0)
Vet:   go vet ./cmd/agent/                                      -> clean
Tests: make test-budget PKGS='./cmd/agent ./internal/memoryprefetch'   （仓库声明的入口，分支作用域）
       -> 2 条 FAIL，两条都在修复前的树上同样失败（见下）
```

**两条 FAIL 的归因（Rule 5 要求，且必须是观察而不是推断）**：
在一个**临时 detached 工作树**里 checkout 修复前的 `12d50350f`（`grep -c prefetchHitsLabel` = 0，
即完全没有本修复），跑同样两条：

```
$ git worktree add --detach /home/user/workspace/gopi-audit-check-$$ 12d50350f
$ GOPI_TMPDIR=… go test ./cmd/agent/ \
    -run 'TestCLINamingExceptionsAreStillDispatchable|TestPrefetchBusyIsRetriedWithinTheSameBudget' -count=1
--- FAIL: TestPrefetchBusyIsRetriedWithinTheSameBudget (1.62s)
    chat_prefetch_segments_test.go:481: session shape: the give-up is not a busy refusal: prefetch gave up after 1500ms (query): context deadline exceeded
--- FAIL: TestCLINamingExceptionsAreStillDispatchable (0.00s)
    cli_completion_exceptions_test.go:92: "web_search" has no contract-inventory entry — the exception would be unreachable from the contract suite
    cli_completion_exceptions_test.go:92: "web_fetch" has no contract-inventory entry — the exception would be unreachable from the contract suite
FAIL	github.com/the operator/gopi/cmd/agent	5.713s
```

**两条都在未修复的树上同样失败 → 预先存在，不是本分支引入的。** 各自的机理：

- `TestPrefetchBusyIsRetriedWithinTheSameBudget`：它要求一次 busy 拒绝而不是 deadline 到期，
  而本机 load 18–23（8 核），250ms 的 dial 预算常常直接超时 →
  `prefetch gave up after 1500ms (query)`。**属于"高负载尾巴"，不是规则违规** ——
  它没制造负载（Rule 27），是宿主本身忙。
- `TestCLINamingExceptionsAreStillDispatchable`：`web_search` / `web_fetch` 的
  contract-inventory 条目缺失。**与本 epic 完全无关**（我的改动只碰 prefetch 记录），
  也不在 `c41a94056` 的改动文件里 —— 列在这里是为了让下一个人知道
  "本分支的测试不是全绿"，而不是把它当成我的回归。

临时工作树用完即 `git worktree remove --force`（Rule 30：不在别人的地上留东西，
也不动 `/home/user/work/GoPi` 主检出）。

---

## 七、Not Done (honest)

### 缺陷 3：给 `internal/memoryprefetch` 接上 `internal/cjkseg` —— 未做

- **根因**（已查实，非推测）：`internal/memoryprefetch/extract.go` 对 CJK run 只做
  "整段 + 2-rune 滑窗二元组"，文件自己把后者标为
  `KindCJKBigram = "cjk-bigram-fallback"` 并写下 "no CJK segmenter in this project"。
  **那个前提已经过期**：`internal/cjkseg`（gse + 内嵌词典）由 `3099ba919` 引入，
  **已经是 HEAD 的祖先**（`git merge-base --is-ancestor 3099ba919 HEAD` → 真），
  现有 API `cjkseg.Words(s string) ([]string, error)`。
- **还差什么**：`extract.go` 的 CJK 分支改为优先 `cjkseg.Words`，把 bigram 滑窗降为
  cjkseg 不可用时的降级（并保留 `DegradationCJKTokenizerGap` 的诚实上报）。
  影响面 = `internal/memoryprefetch` 的候选/门控/差量/渲染测试 + 可能
  `internal/config` 的开关文案 + CONFIG.md 同步。
- **为什么不在本会话做**：这改的是**检索键的形状**（`state.go:165` 的 query 构造），
  牵动 `memoryprefetch` 全包测试与门控阈值；加上 Rule 28（`internal/config` + 检索侧归 peer1），
  一次 60 分钟会话做不稳。按 Rule 26 提子任务，不硬做、不虚报。
- **下一步**：见 `task_submit` 子任务（下节）。

### 缺陷 2：一次 seal 内打字段与轮边界段各检索一次 —— 未做

- **根因**：打字路径与轮边界各持一个 `memoryprefetch.State`（`chat_prefetch.go` 里
  "a second State rather than the typing one" 的设计注释），delta 各自演化，
  于是同一行的同一批词会被检索两次；随后 `discardTypingSection` 把先到的打字段记为 superseded。
  真跑 A/B 两臂都复现了这一对（`source` 缺失行 + `source:"human"` 行 + superseded 行）。
- **还差什么**：要谈的是**两个状态机之间要不要共享 satisfied 集**——
  但共享会把"这一轮的检索抑制操作者下一次停顿"（那条设计注释正是为防这个才分开的），
  所以这是个**设计取舍**，不是去重 bug。需要先在账本里定夺取舍，再改。
- **为什么不在本会话做**：改的是轮边界的公共契约（Rule 28 系统层），且需要先有设计裁定。
- **补充**：brief 的"65% 预算花在重复上"**不成立**（见第二节缺陷 2 的拆分）。
  真正的浪费是"每条 prompt 多跑一次打字段检索"，量级远小于 65%。

### 未做的小项（如实列出）

- 报告的普查用 `python3`（宿主自带）。仓库无 jq 依赖，brief 自己也给了 python 版本；
  未新增任何脚本到树内（Rule 35）—— 生成器落在本次运行的 session 目录 artifacts/ 下。

### 已提的子任务（Rule 26：不等待、不硬做、写清理由与还差什么）

两个缺陷都属**系统层**（Rule 28：`internal/config` + 检索侧归 peer1），
所以按 Rule 40 路由到 **peer1 的账本**（本机是 client；且本 epic 在 peer1 侧的行已
`cancelled`，说明工作已迁到 HP，两边不会重复认领）：

| 子任务 | 缺陷 | 内容 |
|---|---|---|
| `01a115e6-6862-79fe-8703-26a775ed93ec` | 缺陷 3 | 给 `internal/memoryprefetch` 接上既有 `internal/cjkseg` 真分词，把 bigram 滑窗降为显式降级；**不许新写切分器**（Rule 8） |
| `01a115e6-7772-7af1-ac67-5a30e57111d9` | 缺陷 2 | 打字路径与轮边界两个状态机在一次 seal 内的重复检索；**先裁定 satisfied 集是否共享**，再改 |

两条 briefing 全文落盘在
`/home/user/.gopi/sessions/01a115ce-4209-73bf-a159f631a3a2cfaa/artifacts/briefings/`
（`defect3-cjkseg-wiring.md`、`defect2-two-state-machines.md`），并已随提交上传到 peer1 的 `/tmp/`。
两条都写明：**不要重复做已在 HP 分支修掉的记录形态问题**（`c41a94056`），
以及"65% 重复"这个数字不成立、不要沿用。

### 一个必须一起读的补充：报告的第八节

本报告写完后做过一次**自复核**，改正了两处并补了一次独立验算 —— 见第八节。
其中第一处（缺陷 1 的判定句）是把一个**限制条件**写成了**全称否定**，
第二处是红证的构造方式不足以支撑结论。两处都不改变任何判定，
但读者若只看第二节而不看第八节，会得到"lookup 行的 adopted 恒为 no"这个**错的**印象。
**审计报告的错误与代码的错误同价**，所以更改的是报告本身，不是加一个脚注。

---

## Evidence (self-filed)

- 本机语料普查：`python3 artifacts/census.py` → `record files: 3 records: 164`，
  `lookup lines: 129 adoption lines: 35`，`lookup lines with hits==0: 12`，
  `ADOPTION lines that also carry a hits field: 35 (values {0: 35})`。
  **复核时在同一台机器上重跑过一遍，逐项数字一致**（`generated by:` 那两行与
  `git rev` 行随工作树而变，其余数字相同）。
- 交叉验算（缺陷 1 的判定，独立于报告的措辞）：
  `grep -h '"outcome":"hits"' $(find ~/.gopi/sessions -name memory-prefetch.jsonl) | grep -c '"hits":0'`
  → `0`；再叠加 `grep '"adopted":"yes"` 仍是 `0`。
  47 条 `hits=0` 行按 outcome 分组 → `35 adopted / 12 superseded`。
- 归属判定：`git merge-base --is-ancestor 3099ba919 HEAD` → exit 0（"IS ANCESTOR of HEAD"）；
  `grep -rn "internal/cjkseg" --include=*.go . | grep -v _test` → 只有 `internal/tools/memory.go:31`
  与 `cmd/agent/cli_memory_backfill_tokens.go:21`，`internal/memoryprefetch` **0 命中**。
- 红证（三次注入，树均为 `c41a94056`，可编译，测试未改动；每次注入后 `git checkout --` 还原）：
  ① `take()` 写回伪造对 → `--- FAIL: TestTypingAdoptionLineCarriesNoFabricatedMeasurement`；
  ② 轮边界内联写入点同样写回 → `--- FAIL: TestTurnAdoptionLineCarriesNoFabricatedMeasurement`；
  ③ `Query` 的 tag 还原成 `json:"query"` →
  `--- FAIL: TestQuerylessEventDoesNotReadAsAnEmptyQuery`，
  报文 `a query-less event still carries "query"="" … {"query":"","outcome":"superseded","adopted":"no"}`。
  阴性对照：①② 下 `TestLookupLineKeepsItsMeasuredZero` 与 `TestQuerylessEventDoesNotReadAsAnEmptyQuery`
  保持 **PASS**（说明两个字段问题各自独立）。
  另有一次**不成立的取证**（已记入第八节）：把新测试放进修复前的树只会 `[build failed]`。
- 绿证：`make test-budget PKGS='./cmd/agent ./internal/memoryprefetch'`（仓库声明的入口）→
  两条 FAIL，**两条都在修复前的 `12d50350f` 上同样失败**（`TestPrefetchBusyIsRetriedWithinTheSameBudget`、
  `TestCLINamingExceptionsAreStillDispatchable`，逐字见第六节）→ 预先存在，非本分支引入。
- 假断言证据：修复后 `chat_prefetch_test.go:152` 立刻变红
  （"the record does not report the adoption:"），证明它此前是靠伪造的 `"hits":0` 通过。
- A/B 真跑：两臂记录逐字贴在第五节；A 臂采纳行 `"hits":0,"ms":0`，B 臂同两行无这两个键。
  两臂记录的**权威副本**（artifacts/arm-a-record.jsonl 1020B、arm-b-record.jsonl 961B）
  与贴出的文本逐字一致；原先拷成 0 字节的事故与更正记在第五节与第八节。
- 交付二进制上的就地验证：`bin/gopi-final`（从交付树重建，rev 见 §9.1 的更正）+ 真 pty + `gopi tool sandbox create`
  的沙箱 → 采纳行无 `hits`/`ms`、保留 `section_bytes:348`；superseded 行只剩 `adopted,outcome`（第五.1 节）。
- 不变量复扫（对**修复前**的 164 条语料跑，两问各一次）：
  ① `outcome=hits ∧ hits==0 ∧ adopted==yes` → **0 条**；
  ② `outcome=adopted ∧ (有 hits ∨ 有 ms)` → **35 条**。
  即"该不变量在旧语料上的违规恰好就是采纳行的 35 条伪造值，检索行一条没有" ——
  与缺陷 1 的误读判定、缺陷 1' 的修复对象**同时吻合**。修复后的记录在同两问上均为 0（第五.1 节的键集可查）。
- 落盘位置：`/home/user/.gopi/sessions/01a115ce-4209-73bf-a159-f631a3a2cfaa/artifacts/`
  （`census.py`、`census.txt`、`pty-drive-prefetch.py`、`run-ab.sh`、`record-file-note.txt`、
  两臂的 record 与 pty-stream.log、`final-record.jsonl`、`final-out.txt`、`final-sandbox.txt`、
  `briefings/` 下两份子任务 briefing）。

## 八、审计后的自复核（改正两处，并补一次独立验算）

审计自己的产物也要按同一标准复核。本节记录复核发现的两处**我第一版写错/取证不完整**的地方，
以及一次针对缺陷 1 判定的独立交叉验算。**改的是报告的文字与取证，不改任何判定。**

### 8.1 缺陷 1 的判定句写错了（已在上文改正）

第一版写"129 条 lookup 行 adopted 全 `no`" —— **实测反例**：`outcome=hits` 行里
`adopted=yes` 有 94 条。判定本身（brief 把两种事件行合成一个计数）**不受影响**，
而且反例正是判定的机理：`Adopted` 来自 `p.session.Use(query, section)` 的返回值
（`cmd/agent/chat_prefetch.go:491`），而 `Use()` 要求 `query != "" && section != ""`
且 `query == s.fetched`（`internal/memoryprefetch/session.go`），
所以 `outcome=hits` 行上的 `adopted=yes` 只能意味着"**这次检索的那一段真的进了 prompt**"。

**独立验算（不看我自己那句判定，直接问语料）**：把问题写成它的字面形式 ——
"有没有哪条**检索行**报了没命中、却说自己被采纳了？"

```
$ grep -h '"outcome":"hits"' $(find ~/.gopi/sessions -name memory-prefetch.jsonl) \
  | grep -c '"hits":0'                                        → 0
$ grep -h '"outcome":"hits"' … | grep -c '"hits":0.*"adopted":"yes"'   → 0
```

**0 条。** 不是"我数错了"的 0，是结构上的 0：`Use()` 在 `section == ""` 时返回 false，
而 `hits == 0` 的检索给出的正是空段。**所以 brief 说的情况在本机不可能发生**，
它的 47/35 全部来自 `outcome=adopted` 的采纳行 —— brief 读的是那个伪造的 `hits:0`。

### 8.2 红证的取证方式（复核时发现第一版的"红证"不足，已重做并逐条注明构造）

正文第四节贴的红证文本来自修复前的运行，但**复核时发现取证方式经不起追问**，
所以重做了，并把每一次注入的**构造方式与观察到的结果**逐字列出。

**第一处错误（已改正）**：把新测试文件放进未修复的基线树
（`/home/user/workspace/gopi-base-01a115ce`，`12d50350f`）**产生不了断言级红** ——
测试用的 `prefetchHitsLabel` / `prefetchMsLabel` 是修复一手引入的，基线树上不存在，
结果是编译失败而不是断言失败：

```
cmd/agent/chat_prefetch_adoption_semantics_test.go:213:35: undefined: prefetchHitsLabel
FAIL	github.com/the operator/gopi/cmd/agent [build failed]
```

**一个编译失败证明不了语义门是有效的**（它只证明 helper 是新的）。

**第二处（更隐蔽，也是复核的价值）**：只注入打字路径的 `take()`，
**只有一条**测试变红：

```
--- FAIL: TestTypingAdoptionLineCarriesNoFabricatedMeasurement
```

原因查实：**两个采纳入口各写各的 `prefetchRecord`，不共用一个写入点** ——
打字路径走 `chatPrefetch.take()`（`cmd/agent/chat_prefetch.go`），
轮边界在 `chatPrefetchAdoptTurn()` 里**内联**写自己的采纳行
（`cmd/agent/chat_prefetch_turn.go` 的 `p.recordLine(prefetchRecord{Query: action.Query, Outcome: prefetchOutcomeAdopted, …})`）。
所以"注入一处就全红"的预期是错的；**两条测试的覆盖面正是这样被证出来的** ——
若当初只写了打字路径那一条，轮边界的门就是空的。

**最终采用的三次注入（树均为本分支 `c41a94056`，可编译，测试未改动）**：

| 注入 | 还原的缺陷 | 观察到的红 |
|---|---|---|
| ① `take()` 写回伪造对（指针形态保留） | 打字路径的采纳行伪造 `hits/ms` | `--- FAIL: TestTypingAdoptionLineCarriesNoFabricatedMeasurement` |
| ② 轮边界内联写入点同样写回伪造对 | 轮边界的采纳行伪造 `hits/ms` | `--- FAIL: TestTurnAdoptionLineCarriesNoFabricatedMeasurement` |
| ③ 只把 `Query` 的 tag 还原成 `json:"query"` | 无 query 的事件渲染成 `"query":""` | `--- FAIL: TestQuerylessEventDoesNotReadAsAnEmptyQuery` |

③ 的**完整观察**（逐字）：

```
=== RUN   TestQuerylessEventDoesNotReadAsAnEmptyQuery
    chat_prefetch_adoption_semantics_test.go:247: a query-less event still carries "query"="" — an event that asked nothing must not render as one that asked the empty string (Rule 15a): {"query":"","outcome":"superseded","adopted":"no"}
--- FAIL: TestQuerylessEventDoesNotReadAsAnEmptyQuery (0.00s)
FAIL	github.com/the operator/gopi/cmd/agent	7.382s
```

①② 合并注入的**完整观察**（`-v`，逐字）：

```
=== RUN   TestTurnAdoptionLineCarriesNoFabricatedMeasurement
--- FAIL: TestTurnAdoptionLineCarriesNoFabricatedMeasurement (0.08s)
=== RUN   TestTypingAdoptionLineCarriesNoFabricatedMeasurement
--- FAIL: TestTypingAdoptionLineCarriesNoFabricatedMeasurement (0.01s)
=== RUN   TestLookupLineKeepsItsMeasuredZero
--- PASS: TestLookupLineKeepsItsMeasuredZero (0.00s)
=== RUN   TestQuerylessEventDoesNotReadAsAnEmptyQuery
--- PASS: TestQuerylessEventDoesNotReadAsAnEmptyQuery (0.00s)
FAIL	github.com/the operator/gopi/cmd/agent	1.858s
```

断言文本（逐字）：

```
chat_prefetch_adoption_semantics_test.go:167: turn-boundary adoption: the adoption line carries "ms":0 — an adoption event measured no lookup, so the field must be ABSENT (Rule 15a: …)
chat_prefetch_adoption_semantics_test.go:167: turn-boundary adoption: the adoption line carries "hits":0 — an adoption event found no hits itself, so the field must be ABSENT
chat_prefetch_adoption_semantics_test.go:202: typing adoption: the adoption line carries "ms":0 — …
chat_prefetch_adoption_semantics_test.go:202: typing adoption: the adoption line carries "hits":0 — …
```

**其余两条在 ①② 下保持绿，是阴性对照**：`TestLookupLineKeepsItsMeasuredZero` 绿说明
注入没有破坏真值 0 的保留；`TestQuerylessEventDoesNotReadAsAnEmptyQuery` 绿说明
"空 query" 与 "伪造 hits/ms" 是**两个独立的字段问题**，不会被一次修法顺带覆盖
（这也是为什么 ③ 需要单独验）。

三次注入都已 `git checkout --` 还原，工作树回到 `c41a94056` 的干净状态。

### 8.3 一次交付事故：A/B 记录文件曾经拷成 0 字节（已修，见第五节）

`run-ab.sh` 用 `find … -exec cp {} DST \;` 选记录文件，而一次 chat 会在沙箱里留下
**多个** session 目录、其中一个是 0 字节的同名文件 → 拷到的是空文件，
`arm-a-record.jsonl` / `arm-b-record.jsonl` 一度都是 0 字节。
**这类事故的危险不是丢了几个字节，而是它会伪装成"这次运行没产生证据"**（Rule 15）。
已按显式路径恢复（1020B / 961B），并把选择条件改成 `-size +0c`。
正文第五节引用的两臂记录与恢复后的文件**逐字一致**。

---

## 九、收尾复核：本报告的 hash 引用与"交付物本身"的重跑

审计报告的错误与代码的错误同价（§8 的原则），所以收尾时把**报告里引用的每一个 hash**
当作断言复核了一遍。查出两处引用错误，都改的是报告，不改判定：

**9.1 `e56b29f14` —— 一个不可达的 rev（已改正）**

第五节 5.1 与 `## Evidence` 把"重建交付二进制"的那次构建记在
`# e56b29f14, 64252579 bytes` 上。实测：

```
$ git cat-file -t e56b29f14      -> commit        (对象还在，所以当初不会被立刻发现)
$ git merge-base --is-ancestor e56b29f14 HEAD    -> NO
$ git log --oneline --all -5 -- devdoc/memory-prefetch-field-audit.md
  2050bfeb0  docs(audit): memory_prefetch field audit — …   (HEAD)
```

**它不在本分支的历史里** —— 是中间一次 amend/改写留下的孤儿提交，对象在 `.git` 里，
但任何 `git checkout <rev>` / 报告读者顺着 rev 走都会落空。
**后果**：报告里最有说服力的那一段（"从已提交的树重建后重跑"）指向一个读者拿不到的状态。
已改为不写 rev，并在此处点名更正。

**9.2 `b304d3083`（提交表）—— 同一类错误的第二处（已改正）**

同一原因，`b304d3083` 也不可达；可达的 docs 提交是 `2050bfeb0`
（`git merge-base --is-ancestor b304d3083 HEAD` → NO；`2050bfeb0` 就是 HEAD）。
两张表里的 rev 已全部换成可达值。**两次都是"hash 看起来像真的"**，
所以这条复核不是洁癖：一个不可达的 rev 会让证据链在读者那一端静默断掉。

**9.3 交付物本身重跑一次（Rule 31：跑要交出去的那一个面）**

收尾时从**当前 HEAD（`2050bfeb0`）**重建二进制，用**同一个真 pty 驱动脚本**再跑一次：

```
Ran: CGO_ENABLED=0 go build -o bin/gopi-closer ./cmd/agent/     # 64252579 bytes
     SB=$(gopi tool sandbox create --label closer-verify --task-id 01a115ce420973bfa159f631a3a2cfaa | tail -1)
     export GOPI_MEMORY_DB=/home/user/workspace/gopi-test-scratch/prefetch-audit/ledger.db
     python3 artifacts/pty-drive-prefetch.py "$PWD/bin/gopi-closer" "$SB"    # 真 pty, driver exit=0
     # sandbox: /home/user/workspace/gopi-sandbox/sandbox-closer-verify-9db85a81
Result: 记录 .../sandbox-closer-verify-9db85a81/.gopi/sessions/f4f9277a-22a8-4fea-9d98-e4480a67f935/memory-prefetch.jsonl
        逐行键集（outcome | adopted | keys）：
          hits       | yes | adopted candidates hits ms outcome query segments
          hits       | no  | adopted hits ms outcome query segments source
          adopted    | yes | adopted outcome query section_bytes source
          adopted    | yes | adopted outcome query section_bytes
          superseded | no  | adopted outcome
```

与 5.1 节**逐行键集完全一致**（同一形状，两次独立运行），并额外覆盖了
"有 query 键的 superseded 行"（5.1 那次是 `discardTypingSection` 的无 query 形态）。
**这是本轮改动面的最终 `Ran:`**：交付 HEAD 上的二进制 + 真 pty，
产出记录里 `hits`/`ms` 只出现在检索行、采纳行只有 `section_bytes`。

---

## 提交

| commit | 内容 |
|---|---|
| `c41a94056` | `fix(chat): an adoption line stops reporting a lookup it never made`（4 文件改动 + 1 新测试文件） |
| `2050bfeb0` | `docs(audit): memory_prefetch field audit — 4 defects judged, 2 real and fixed, 2 routed`（本报告 + `DOC-INDEX.tsv` 分类行，同 commit，Rule 36）。**本行原写 `b304d3083` —— 那是一次改写历史留下的孤儿 hash，已按 §9 改成实际可达的 rev。** |

分支：`task/01a115ce420973bfa159f631a3a2cfaa`（工作树
`/home/user/workspace/gopi-01a115ce420973bfa159f631a3a2cfaa`）。


---

## 十、收尾复核（二）：一条被我写轻了的测试结论，与一次复现出来的 flaky

第九节复核的是 **hash 引用**；这一节复核的是**测试结论**，结论是：第五节与任务报告里
"包内定向 `-run` 全绿"**写得比事实强**，实测这个说法**不成立**。

### 10.1 复现方式（命令与数字都在这里）

```bash
cd /home/user/workspace/gopi-01a115ce420973bfa159f631a3a2cfaa      # rev 73492ff5d
for i in 1 2 3; do
  L=$(cut -d' ' -f1 /proc/loadavg)
  R=$(go test ./cmd/agent/ -run 'TestPrefetchBusyIsRetriedWithinTheSameBudget|TestPrefetchRecordCarriesSegmentTiming' -count=1 2>&1 | grep -cE '^--- FAIL')
  echo "iter$i load=$L failing_assertions=$R"
done
```

实测（本机 hostb-Laptop，8 core，load 平均 12–17）：

| 轮次 | load | 失败断言数 |
|---|---|---|
| 1 | 13.66 | 0 |
| 2 | 14.01 | **2** |  ← `TestPrefetchRecordCarriesSegmentTiming` + `TestPrefetchBusyIsRetriedWithinTheSameBudget`
| 3 | 12.95 | **1** |

更早一轮三连：`ok / ok / FAIL(2)`。合计 6 轮里 3 轮有失败 —— **约一半**。

### 10.2 两条失败各自是什么

```
chat_prefetch_segments_test.go:137: ms = 96 but seg_stat_ms+seg_open_ms+seg_query_ms = 94 (0, 0, 94):
    the segments do not account for the measured time
chat_prefetch_segments_test.go:481: session shape: the give-up is not a busy refusal:
    prefetch gave up after 1500ms (query): context deadline exceeded
```

- **第 137 行（分段 vs 墙钟）**：`prefetchSegUnitMs = 1`，即分段按 1ms 粒度记，而断言只
  容许 ±1ms。在 8 core、load>12 的宿主上，`time.Since` 与分段累加差 2–3ms 是**正常抖动**，
  不是"分段没测到东西"。断言把**测量精度**当成了**不变量**。
- **第 481 行（busy 拒绝 vs 超时）**：测试要求 `isBusyErr(err)`，但实测错误是
  `context deadline exceeded`（1500ms 的 `prefetchGoroutineTimeoutMs` 先到）。这更像一个**真问题**：
  宿主忙的时候，`BEGIN EXCLUSIVE` 拿锁那条路还没走到 `SQLITE_BUSY`（250ms busy_timeout）就先被
  1500ms 的协程上限掐断 —— 也就是"拿不到锁"和"太慢"在这条路径上**用错误类型区分不开**。
  这一条是否为缺陷，本会话**没有下结论**（要判它得先测同一宿主空载时的行为，那是另一件事）。

### 10.3 谁是被告：不是本轮的改动

`cmd/agent/chat_prefetch_segments_test.go` 在本分支上**一行没动**：

```bash
git diff --name-only master...HEAD -- cmd/agent/chat_prefetch_segments_test.go | wc -l   # → 0
```

本轮改的 `chat_prefetch.go` 确实把 `Ms` 从 `int64` 变成了 `*int64`，**但这个测试文件自己
定义了一个旁路解码结构**（同文件 41 行起 `type prefetchRecordLine struct`，带
`Hits int` / `Ms int64`），它**不经过** `prefetchRecord`，字段仍然是普通 `int`/`int64` ——
所以指针化对它**没有影响**（这也正是它当初写那个旁路结构的目的，见该文件 38–40 行的注释：
"through prefetchRecord itself would not notice a renamed or dropped field"）。

也就是说：**这两条失败在本轮改动之前就存在**，是本轮**发现**的，不是本轮**引入**的。

### 10.4 对被写轻的那句话的更正

| 位置 | 原话 | 更正 |
|---|---|---|
| 第五节 / 任务报告 `## Verification` | "改跑包内定向 `-run` 全绿" | 改为：**跑过若干轮，其中数轮全绿；但同一命令在宿主 load>12 时约一半轮次会在 `chat_prefetch_segments_test.go:137/481` 失败**。该文件不在本分支 diff 内。 |
| 任务报告 `## Not Done` | 只写了 `make test-budget` 超时 | 补：**分支作用域内还有两条 flaky 测试**（上表），已提子任务。 |

一条"我跑过一次是绿的"和一条"这条命令约一半轮次会红"是**两个不同的结论**，
只有后者能被下一个人拿去用。这正是 Rule 5 `## Evidence (self-filed)` 要挡的东西：
把没观测到的结论写成已观测到的。
