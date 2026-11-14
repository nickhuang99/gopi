<!-- doc-class: historical -->
# memory-prefetch 第二条腿：chat_notes（round 级 [IMPORTANT] 笔记索引）

任务：`01a131f5-37da-79ee-ada3-6406b5d1b116`
分支：`task/01a131f537da79eeada36406b5d1b116`

## 问题（一句话）

`memory_prefetch` 只查 `distilled_qa`/`memory` 一条腿。round 级的
`[IMPORTANT]` 笔记索引（表 `chat_notes` + `chat_notes_fts` + `chat_notes_tri`，
消歧字段 `session_id` 与上限 `maxSelfRecordNoteChars` 都已存在）从来没有被
prefetch 读过 —— 实测于 `0ab59bf72`。

## 实测根因（5-why，不是"看起来不对"）

第一版把第二条腿接上之后，**真实对话里它仍然交白卷**：

```
{"query": "auto memory important note response 捕获 对象 改为", "hits": 1,
 "outcome": "below-floor", "notes": {"outcome": "empty", "hits": 0, "ms": 12}}
```

同一时刻 `gopi session notes "auto memory important note response 捕获 对象 改为"`
也回 `No notes matching`，而**同一条查询的 memory 腿第一击就命中**。

- why：`ChatNoteSearchOn` 的三个 tier（fts/trigram/like）都把查询当成**一个短语**
  （`ftsQuote` 包成 `"..."`），这是给"人手输几个词"用的形状。
- why：prefetch 的查询是**整行对话**（`query:"auto memory important note response
  捕获 对象 改为"`），没有一条笔记包含这个句子。
- why：memory 腿之所以命中，是因为它走的是 **ladder**（把查询切成词元，AND 求子集），
  而不是短语匹配。
- 结论：**缺的不是"再查一张表"，是"用同一种召回形状查第二张表"**。修在源头。

## 做了什么

### 1. ladder 成为可复用的（`internal/tools/memory_ladder.go`）

ladder（子集下降、阈值、探测预算、ranking）**一份实现**，表名参数化：
`memsearchLadderFTS` 常量 + `ladderCountOn` / `ladderTermDFOn` / `ladderDroppedDFOn` /
`memsearchSubsetPickOn` / 导出 `LadderMatchOn`。原函数各自变成
`… → …On(ctx, db, memsearchLadderFTS, …)` 的一行转发 —— 内存腿的行为一个字节没变。

### 2. 第四个 tier：ladder（`internal/tools/chat_notes.go`）

`ChatNoteSearchOn` 在三短语 tier 全部空手之后才下探 ladder，返回 `ChatNoteTierLadder`。
**顺序就是优先级**：短语命中 ≫ 词元子集命中，所以能回答的查询答案不变；
它只接手"短语根本无法回答"的查询。

实测（同一台机器、同一个索引、同一条查询）：

```
$ ./bin/gopi session notes "auto memory important note response 捕获 对象 改为"
1 note(s) matching "..." via ladder:
  hosta  round 28  2026-10-10 13:09  23f88060-...
```

### 3. 第二条腿（`cmd/agent/chat_prefetch_notes.go` + `internal/memoryprefetch/notes.go`）

- `prefetchNotesSource`：会话级状态（handle + `tried` 闩 + `reused` + `sessionID`）。
- **句柄复用**：`tools.ChatNoteIndexPath()` 与 `config.LedgerPath()` 解析到**同一个文件**，
  所以腿走 memory 腿已经热好的那条连接（`handleFor(shared, path)` 比对
  `prefetchHandle.path`），不另开一条；两个 store 被拆开时（测试/诊断）才自己 dial，且**只 dial 一次**。
- **回声排除**：读者自己这个 run 写的笔记不是检索，是已经在上下文里的文本，按名丢弃
  （`note-self-echo` degradation，记录但不注入）。
- **每次退出都被记录**：`not-consulted / absent / empty / hits / failed / gave-up / latched`，
  一条腿答不上来绝不长得像"查了但没有"。

### 4. 一条 section，两条腿（`cmd/agent/chat_prefetch.go`）

`prefetchComposeSection` 是唯一的合成点：memory 腿的行在前、notes 腿的行在后，
交给**同一个** `memoryprefetch.InjectSection` 渲染（同一 rune 预算、同一
`MaxSegmentHits`、同一注入下限）。注入下限按**合并后的行数**判 —— 这正是本 epic 的存在
理由：memory 库一无所知、笔记索引拿着答案，1+1 才够到下限。

记录因此多了三个可观测：
`notes{outcome,tier,hits,ms,err}`、`memory_outcome`、以及腿自己的 degradations；
**单腿（一次性）形状的记录一个字节没变**（指针 + 空 verdict ⇒ 键不落盘）。

## 真实运行取证（Rule 31）

改了**对话表面**，所以跑的是真对话，不是 `-h`：

```
$ CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/
$ GOPI_MEMORY_PREFETCH=1 ./bin/gopi chat --once \
    -p "auto-memory 捕获对象从 IMPORTANT note 改为 response 正文，这条设计怎么来的"
```

`~/.gopi/sessions/29a77035-dde0-456f-9744-bb1467c1210b/memory-prefetch.jsonl`：

```json
{"query":"auto memory important note response 捕获 对象 改为","hits":2,"ms":209,
 "outcome":"hits","adopted":"no","segments":{"seg_stat_ms":0,"seg_open_ms":0,"seg_query_ms":138,...},
 "notes":{"outcome":"hits","tier":"ladder","hits":1,"ms":68},"memory_outcome":"hits"}
{"query":"...","outcome":"adopted","adopted":"yes","section_bytes":1055}
```

两条腿都命中，section 1055 字节并被采纳 —— 修复前后的记录对照见上。

## Not Done（如实）

- notes 腿没有独立子预算的**压力测试**：`prefetchNotesLegBudgetMs` 的超时路径只在
  已取消 ctx 的单测里走过，没有在真机上制造慢索引（Rule 27 禁止造负载）。
- `reused` 的收益只有**结构性证据**（句柄同一性断言），没有计时数字 —— 计时会同时
  测到机器的负载，不测代码。
- memory 腿的 `InjectFloor` 语义**未放宽**：单腿 1 行仍然被拒（有对照断言）。
