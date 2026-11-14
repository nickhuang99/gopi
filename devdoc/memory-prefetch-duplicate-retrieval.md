<!-- doc-class: historical -->
# Task Report: memory-prefetch — 一行输入被两条路径各检索一次（task 01a11601）

**Branch**: `task/01a11601388471118bdffdf64de72447`
**Verdict**: **缺陷（defect）**，不是有意双保险。来源行与设计出处见下。

## Completed

### 1. 判定结论：是缺陷，且「双检索」不是设计意图

设计来源有据可查（不是推测）：

- **`internal/memoryprefetch/session.go:7-10`**（打字路径头注释）明说两台机器的分工：
  > "It answers a DIFFERENT question from the sibling machine in state.go, which is
  > why it is a second type rather than more fields on State: **Step decides whether a
  > TURN is worth a retrieval** … while **Session decides whether the OPERATOR has
  > paused** at all and whether the query already in hand still describes the line
  > being sealed".
- **`internal/memoryprefetch/turn.go:8-24`** 明说轮边界存在的理由是**打字路径结构上看不见的四类输入**：
  notify 投递、整块粘贴（词在 spill 文件里）、`/queue` 排队提示、模型自输行。原文：
  > "the four source classes that have NO line editor to sample got nothing at all".
- **commit `655ec406a`** 的提交信息（轮边界那一笔）标题就是
  **"one memory-prefetch retrieval per sealed TURN"**。
- **`devdoc/memory-prefetch-turn-boundary.md:57-59`** 写着 **"ONE section per turn"**，
  并说封印时若已有轮边界段，打字段被 **superseded** 丢弃。

**结论**：设计意图是「一轮一次检索、双路径互斥」。但**只对轮边界那一侧做了互斥**——
`cli_chat.go:3354-3363` 的执行是：

```go
if turnPrefetched { prefetch.discardTypingSection(line) }   // 轮边界赢了 → 丢弃打字段
else              { chatPrefetchAdopt(out, prefetch, line) } // 否则用打字段
```

这段代码假设「两者不会针对**同一个问题**检索」。对 notify / paste / queue / 模型自输行这四类
成立（它们的关键词来源不同，键天然不同）；但对**操作者亲手敲的普通行**不成立：打字采样器
从 `Editor.Value().Text()` 建键，轮边界从 `sealedMessage` 里同一批词建键 —— **同一个键**。
于是两条路径各跑一次同样的查询，而 `superseded` 分支永远救不到它（它只在**轮边界已经跑完**
之后才判定，第二笔钱已经花掉了）。record 里的实测形态是两行 `outcome=hits`、query 逐字相同。

这不是「双保险」：保险的价值在于覆盖不同故障域，而这里两笔查询的输入、DSN、预算、查询语句
全部相同 —— 第二笔只增加一次 ledger 往返与一次渲染，任何一侧失败时所覆盖的东西另一侧也覆盖不到。
（对照：`superseded` 这个机制本身是**有意**的，本次未改动它，见 `Not Done`。）

### 2. 最小样本复现（Rule 10.4）——先红

`evidence/pty_dup_probe.py`：真 `bin/gopi chat` 挂在 pty 上，**逐字打一行**（间隔 90ms，不是粘贴爆发），
**停顿 1.6s**（>600ms 去抖），**一次 `\r`**。改动前（base `bb6ee97f9` 编出的二进制）：

```
LABEL=BEFORE-BURST  record_lines=5  outcome=hits lines=2
    {"query":"worktree base tip master fetch","hits":1,"ms":3,"outcome":"hits","adopted":"yes"}
    {"query":"worktree base tip master fetch","hits":1,"ms":1,"outcome":"hits","adopted":"no","source":"human"}
    {"query":"worktree base tip master fetch","hits":0,"ms":0,"outcome":"adopted","adopted":"yes","source":"human","section_bytes":182}
    {"query":"worktree base tip master fetch","hits":0,"ms":0,"outcome":"adopted","adopted":"yes","source":"human","section_bytes":182}
    {"query":"","hits":0,"ms":0,"outcome":"superseded","adopted":"no"}
```

**一行输入 = 2 条 `outcome=hits`（同一个 query）+ 2 条 adopted**。红证已归档：
`evidence/probe-BEFORE-BURST.jsonl`、`evidence/probe-BEFORE.jsonl`。

### 3. 最小回归测试（Rule 10.5）——先红后绿

`cmd/agent/chat_prefetch_dup_test.go`：

- `TestOneTypedLineIsRetrievedOnceAtTheTurnBoundary` —— 打字路径 `Claim`+`fetch` 之后再走轮边界，
  断言全 record 里 `outcome=hits` 恰好 **1** 条，且 section 仍进 `Message`（不碰 `Display`），
  且采纳事件仍带 `source`。
- `TestTheTurnBoundaryStillRetrievesAKeyTheTypingPathNeverBuilt` —— **负控制**：粘留言（键来自
  spill 载荷、打字路径从未建过）仍必须**检索**。没有它，修法就退化成「把轮边界调安静」，
  即删掉它存在的理由。

**红**（改动前，同一测试文件）：
```
--- FAIL: TestOneTypedLineIsRetrievedOnceAtTheTurnBoundary
    chat_prefetch_dup_test.go:143: one line typed + one Enter recorded 2 outcome=hits lookup line(s), want 1
        [0] query=notify turn boundary payload retrieval hits=1 outcome=hits adopted=yes
        [1] query=notify turn boundary payload retrieval hits=1 outcome=hits adopted=no source=human
        [2] query=notify turn boundary payload retrieval hits=0 outcome=adopted adopted=yes source=human
```
**绿**（改动后）：见 `## Verification`。

### 4. 修法（最小改动）

让**轮边界先问打字路径有没有现成的同一问题的段**，有就共用，没有才检索：

- `internal/memoryprefetch/turn.go` 新增 **`TurnQuery`**：返回这一轮**将会**用的键与 delta，
  **不推进状态机**（不花 throttle 步、不标记 satisfied）。键由 state.go 自己的 delta 规则算出，
  调用方不自己拼（Rule 13/14）。
- `cmd/agent/chat_prefetch.go` 新增 **`chatPrefetch.takeShared`**：仅当
  `session.PendingQuery() == 本轮的键`（逐字相等）时，把打字路径暂存的段交接出去；
  记录为**新 outcome `shared`**（不记 `hits` —— 读者审计的是「跑了几次查询」，共用的这一轮
  一次都没跑）。交接后 `Session.Take` 自然清空暂存段，所以后续轮次不会重复收到同一段。
- `cmd/agent/chat_prefetch_turn.go`：在 `TurnStep` **之前**做这次比对（放在之后会把这一轮
  记进 turn 状态机，令同一键在后续轮次看起来是「新词」）。

**为什么用「键逐字相等」而不是「行相同」或「来源相同」**：四类盲区输入的行本身就不同
（引用行 vs 载荷），来源也不同；而「这一轮的问题已经被答过」的诚实判据，恰好就是**两个键相同**——
包括打字路径其实 fetch 的是它自己 clip 过的键（`DefaultMaxQueryRunes`），所以比对的是
`Session.PendingQuery()`（真正 fetch 过的那个键），不是一个新拼的键。
**保守方向是明说的**：只共享逐字相等的键；仅仅**重叠**的轮次仍花自己的一次检索 ——
按重叠跳过会压制一个真不同的问题（本功能要防的正是这个），而漏共享只多花一次查询。

**没有新增持久依赖**（Rule 35）：无新文件、无新配置键、无新常驻进程；`shared` 只是既有
record 文件里的一个新 outcome 词（Rule 1 常量 `prefetchOutcomeShared`）。
**没有加时间窗去抖**（禁令遵守）：判据是**键相等**，与时间无关。

### 5. Rule 31 真跑证据（改动前后 memory-prefetch.jsonl 对比）

同一个探针、同一条输入、同一台机，只换二进制：

| 二进制 | record 行数 | `outcome=hits` | 形态 |
|---|---|---|---|
| base `bb6ee97f9` | 5 | **2** | 两条同 query 的 hits + 两条 adopted + 一条 superseded |
| 本分支 HEAD | 2 | **1** | 一条 hits + 一条 `outcome=shared`（adopted=yes, source=human） |

改动后完整 record（`evidence/probe-AFTER-BURST.jsonl`）：
```
{"query":"worktree base tip master fetch","hits":1,"ms":2,"outcome":"hits","adopted":"yes","candidates":"ladder"}
{"query":"worktree base tip master fetch","hits":0,"ms":0,"outcome":"shared","adopted":"yes","source":"human","section_bytes":182}
```

## Not Done

- **`superseded` 分支未改动**：轮边界**已经检索完**之后才走到的那条路（`discardTypingSection`）
  是**有意设计**（`devdoc/memory-prefetch-turn-boundary.md:57-59`），本次只堵住它前面那次重复，
  没有删它 —— 删掉会让「轮边界赢、打字段作废」这件事重新变成静默丢弃（Rule 15）。
- **打字采样器「打字中途就命中」的那类重复未处理**：本次探针在宿主负载高时观察到打字 poller
  在**行还没打完**时就 fetch 了一个前缀键（如 `worktree base tip master`），随后轮边界又按完整
  键 fetch 一次。那是**另一个缺陷**（前缀键 vs 完整键，去抖与 poll 时钟的竞争），既不是同一个键、
  也不是本次 acceptance 所指的形态；按 Rule 26 应另行派单，未在本分支内改动。
- **`gopi chat --tools` 面未接线**（沿用既有 gap，见 `devdoc/memory-prefetch-default-on.md:173`），
  本次未扩大范围。
- **`chat_prefetch_segments_test.go:481` 的 flaky 在本次 `-run Prefetch` 跑动中复现**，
  但它在 base 上同样红、且已有兄弟任务 `01a11604` 在取证；本报告不声称它是绿的。

## Verification

- Build: `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → `BUILD_OK`；`go build ./...` 无输出（通过）
- Tests: `go test ./cmd/agent/ ./internal/memoryprefetch/ -run 'TestOneTypedLineIsRetrievedOnceAtTheTurnBoundary|TestTheTurnBoundaryStillRetrievesAKeyTheTypingPathNeverBuilt|Turn' -count=1`
  → `ok github.com/nick/gopi/cmd/agent 6.184s` / `ok github.com/nick/gopi/internal/memoryprefetch 0.007s`
- Ran: `python3 evidence/pty_dup_probe.py BEFORE-BURST /dev/shm/gopi-base burst`（base 二进制，pty，真 chat）
  → `outcome=hits lines=2`
- Ran: `python3 evidence/pty_dup_probe.py AFTER-BURST bin/gopi burst`（本分支二进制，同一条输入）
  → `outcome=hits lines=1`，另一条 `outcome=shared`
- Result: 一行输入 + 一次 Enter 的 `outcome=hits` 由 **2 → 1**；section 仍进入 `Message`，
  `Display` 未变，采纳事件带 `source=human`。

## Evidence (self-filed)

- 红证（真跑，base 二进制）：`evidence/probe-BEFORE-BURST.jsonl` 首两行 query 逐字相同、均 `outcome=hits`。
- 红证（单测）：`chat_prefetch_dup_test.go:143` 输出 `recorded 2 outcome=hits lookup line(s), want 1`（见上文引用）。
- 绿证（真跑，本分支二进制）：`evidence/probe-AFTER-BURST.jsonl` 一行 `hits` + 一行 `shared`。
- 来源行：`internal/memoryprefetch/session.go:7-10`、`internal/memoryprefetch/turn.go:8-24`、
  `devdoc/memory-prefetch-turn-boundary.md:57-59`、commit `655ec406a` 提交信息。
- 探针脚本：`evidence/pty_dup_probe.py`（含 base 二进制构建：`/dev/shm/base-01a11601` @ `bb6ee97f9`）。
