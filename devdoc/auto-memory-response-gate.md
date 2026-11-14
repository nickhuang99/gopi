<!-- doc-class: historical -->
# auto-memory 正文形状闸门（response gate）— 实现报告

**日期**：2026-10-10 · **任务**：`01a1234b-b97c-785e-accd-8b5a8cd8b3fa`
**分支**：`task/01a1234bb97c785eaccd8b5a8cd8b3fa`

## 一句话

auto-memory 原来只看模型自己写的 `[IMPORTANT]` note（上限 500 字符，装不下表格），
现在**同一个形状闸门也看正文**——操作员那张火山账单表（正文里的四列 markdown 表格）
从「今天查不到」变成「自动入库」。

## Completed

### 1. 正文形状闸门（`internal/agent/selfnotecaptureresponse.go`）

与 note 闸门**同三臂**（拓扑 / 表格 / 结论），但是**另一套实测标定**——因为两个语料的
分布完全不同，note 的常数照搬过来会全错。每条常数的取值都在源码里写明了它是从哪个语料
的哪个分位数来的：

| 常数 | 值 | 为什么是这个值（实测） |
|---|---|---|
| `respCaptureMinRunes` | 600 | 正文语料 p26（268 条可见正文，p25=601 / p50=1279 / p90=2558）——与 note 的 150（110 条 note 的 p25）**同一分位位置**，不是同一个数 |
| `respCaptureConclusionRunes` | 1200 | 正文 p43，保持 note 的 2× 关系（150/300） |
| `respCaptureTopologyMin` | 2 | 实测 14 条含框线字符的正文，**每一条都 ≥3 个**，没有一条正好是 1 或 2 —— 2 是留余量的下限 |
| `respCaptureTableRowMin` | 1 | markdown **分隔行**（`\|---\|`），不是管道符计数 |
| 拓扑字形集 | 框线**不含箭头** | 见下 |

### 2. 三处与 note 闸门的关键差异，每一处都有实测依据

- **表格臂**：note 用 `|` 计数 ≥3。在正文上这是假阳性机器：268 条正文里 **5 条含 ≥3 个管道符却根本没有表格**（最长的一条 350 rune，是「用散文在讲一张表」）。改用「markdown 分隔行」后，长度门以上 **0 假阳性**。
- **拓扑臂**：note 把**任何**拓扑字形当图。在正文里 `→` 是普通标点——**268 条里 155 条含 `→`**，而含框线字符的只有 14 条。含箭头的版本会让拓扑臂吞掉 58% 的正文，并且因为臂是有序的，会把表格臂刚好吃掉。所以正文臂**只认框线**。
- **结论臂**：note 的 300 rune 落在正文语料的 p23（会捕获四分之一），1200 落在 p43，且**要求带因果/结论词**（词汇直接引用 note 的 `selfNoteSummaryMarkers`，不重打一遍）。

### 3. 两个闸门永远不会看到同一段文字

- `visibleResponseBody()` 先把 `[IMPORTANT]…[/IMPORTANT]` 块**剥掉**再判定——note 是给 harness 的协议文本，note 路径已经拥有它；
- 只剩 note 的正文按 `note-only` **具名拒绝**（不是静默跳过）；
- 未闭合的 `[IMPORTANT]` 视作**散文**（本轮这种回答很多，不能被吃掉）。

**精确说法**：这不是「一轮最多一条」。一轮里正文有形状 **且** note 也有形状时，会存两条——
它们的文字本来就不同（一条是 note 的总结，一条是正文的表格），不是重复。
保证的是**同一段文字只被一个闸门看到**，所以不存在「同一份内容存两遍」。

### 4. 接线（`loop.go` ×2 + `chat_ops.go` ×1）

任务循环的 response-end、纯 chat 路径、带工具的 chat 路径各一行 `a.captureResponseText(...)`，
紧挨原有的 `captureSelfNotes(a.recordSelfNotes(...))`。三条路径同源同行为。

### 5. 只读命中率面：`gopi memory capture-stats`（+ `internal/capturesurvey`）

一份**可复跑**的标定生成器（Rule 38：数字必须能重新导出，不能只有报告里的一句话）。
它读会话 JSONL、对每条可见正文跑**闸门自己的分类器**（经 `agent.ClassifyResponseText`，
所以不可能和存的那套不一致），打出逐臂计数、rune 分位、以及与 note 闸门实测 1.31% 的对照。
`--json` 给可 diff 的同数字；`--root` 换语料；默认读**本 profile 的会话目录**。

### 6. 回归样本（操作员那条真实轮次）

`internal/agent/testdata/volcengine-bill-response.txt` —— 从本机会话日志里**原样取出**的那一轮
（1153 rune，含四列价格表 + 它自己的 note）。测试同时用它的两半：
**绿**=正文表格被表格臂捕获；**红**=同一轮的那条 note 在 note 闸门下**不**被捕获——
这正是当初表格随轮次丢失的原因。

## 实测结果（`gopi memory capture-stats --root ~/.gopi/sessions`，真实输出）

```
corpus: 123 file(s), 1594 round(s), 270 visible response body(ies)

arm            bodies   % bodies
topology           14       5.2%
table             124      45.9%
conclusion         46      17.0%
CAPTURED          184      68.1%
refused:too-short  70      25.9%
refused:no-structure 16     5.9%
body runes: p0=34 p25=624 p50=1283 p75=1934 p90=2563 p100=4384
rate: 68.1% of bodies, 11.5% of rounds
note gate, same measure: 1.3% of rounds
=> the response gate keeps 8.81x what the note gate kept
```

**诚实读法**：这 8.81× **不是「更多记忆」的证明**。note 路径是「模型自己判定值得记」，
正文是「什么都往里放」，两者本来就不该同率。所以门槛做得比正文的平均水平高得多
（26% 的正文因为太短直接不合格；表格臂要求真正的分隔行而不是管道符；拓扑臂要 ≥2 个框线字符）。
真正该看的是：**`refused:no-structure` 只有 16 条（5.9%）**——长度够、却什么形状都没有的正文很少，
说明「长度门 + 形状臂」在正文上没有大面积误杀，也没有大面积误收。

## Not Done（诚实列举）

1. **note 闸门的常数一个没动**。原计划的「note 侧独立标定」经取证后判定**不需要**：
   note 语料（110 条、上限 500 字符）与正文语料不可比，两边各用各的实测即可，改 note 反而会动到已上线行为的门。
2. **`thinking` 块不扫**。它是另一个对象，本轮只做正文；要做得另开任务并各自标定。
3. **prefetch 查询构造没动**。本轮只解决「存不进去」，不解决「取不出来」——`below-floor` 那条线是另一个问题（任务 `01a0fa3d` 那轮的实测：19 条 query 里 16 条 below-floor）。
4. **`capture-stats` 的数只反映本机语料**（123 个 session、1590 轮）。换机器跑出来的数会不同，这是设计如此（生成器可复跑，数字随语料走）。

## Verification

**Build**：`CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → OK（`go build ./...` 亦 OK）

**Tests**（分支范围内，逐包）：
```
go test ./internal/agent/      -run 'TestCaptureResponse|TestResponse|TestVolcengine|TestVisibleResponse|TestExportClassify|TestRunCapturesTheResponseBody'  → ok
go test ./internal/capturesurvey/                                                                                                              → ok
go test ./cmd/agent/           -run 'TestG3|TestG4|TestMemoryHelp|TestMemoryVerb|TestMemoryGrouped|TestCLIContractSubcommandHelp|TestMemoryLegacy' → ok (17.2s)
go test ./internal/standards/  -run 'Rule|TestG'                                                                                                → ok
```

**Ran**（真跑，非 `-h`）：
```
$ ./bin/gopi memory capture-stats --root ~/.gopi/sessions
（输出见上，68.1% of bodies / 11.5% of rounds / 8.81x，exit 0）

$ scripts/g3-memory-help-surface.sh ./bin/gopi      # 帮助面重导出（Rule 38）
search db,intent,limit 3 … capture-stats json,root 2
14 verbs, 68 verb-flag pairs（总数由脚本算，非手数）
```

**Result**：正文表格真的入库——`TestCaptureResponseTextStoresARowWithItsPointer` 与
`TestRunCapturesTheResponseBodyAtTheRoundBoundary`（走 `Agent.Run` 真实轮次、mock 端点回账单表）
都从 `distilled_qa` 里**读回了那一行**，`sources='response(auto)'`、session/round 指针齐全；
同轮 note 在 note 闸门下仍被拒（红例）。

## Evidence (self-filed)

- `git status --porcelain`：干净（提交后复查；见交付声明）。
- 门禁命中：`go test ./internal/standards/ -run 'Rule|TestG'` → `ok github.com/nick/gopi/internal/standards 5.212s`（Rule13/14 常数门、Rule31/36/38 相关门全绿）。
- 帮助面一致性门：`TestMemoryHelpSurfaceMatchesInventory` / `TestMemoryHelpSurfaceInventoryTableMatchesHelp` / `TestMemoryHelpGroupPageAdvertisesEveryDispatchableSubcommand` 全绿——新子动词已进 `memorySubcommandNames`、help 页、inventory 表三处，任一处缺失即红。
- 样本可追溯：`testdata/volcengine-bill-response.txt` 取自 `~/.gopi/sessions/68bec4a8-…/session.jsonl`，原样（含其 note）。
- **`TestDocAnchorsResolveInNormativeDocs` 的红是 master 上既存的，不是本次引入的**（已用干净 HEAD 的对照 worktree 实测，Rule 29 只读检出）：
  ```
  对照（HEAD 66ed4d543，干净 worktree）:  doc anchors … 242 pre-existing drift entries permitted, 47 new, 13 stale permits
  本分支（同上口径）                  :  doc anchors … 242 pre-existing drift entries permitted, 47 new, 13 stale permits
  失败条目数 62 = 62；具名文件集合的差异只有 2 处，且都是 DOC-INDEX.tsv 行号位移
  （我新增一行使 :1031→:1030、:633→:634），不是新增漂移。
  ```
  与我三个文件（`selfnotecaptureresponse.go` / `capturesurvey` / `cli_memory_capture_stats.go` / 本报告）相关的失败条目数 = **0**。
- `gopi tool doccheck` → `the devdoc corpus is consistent — every file under devdoc/ has a well-formed row`（新报告已入 `DOC-INDEX.tsv`，class=historical）。
- **`make test-budget PKGS='./internal/agent ./internal/capturesurvey ./cmd/agent'`：exit=1，但失败集合与干净 HEAD 逐条相同**（同样用对照 worktree @ `66ed4d543` 实测，只跑失败名单）：
  ```
  本分支 : TestKeywordSearchRunPrintsHitsAndNoMatches, TestChatCookedFallback_NonTTY,
           TestRunPathPlainPromptIsVerbatim, TestWorkerDaemon_StartProofAndPidSurviveTheScopeChange,
           TestWorkerDaemon_RequireScopeOffSwitchIsADocumentedDegradation,
           TestDaemonScopeNoPrivilege_TheCorrectionIsEmitted, TestMergeUnionRowCheckOnTheRealManifest
  干净 HEAD: 同一组七条，一条不多一条不少
  ```
  根因两条，都与我无关：`TestSearchWrongGroundExitCodeOnTheRealBinary` / `TestCodeSearchBackendNoSymbol` 报
  `rg: executable file not found in $PATH`、`ctags: executable file not found in $PATH`（hosta 本机未装，本机已知事实）；
  其余为 daemon-scope / merge-union 类既存红。
  **本次改动新增的失败 = 0。**
- 我改的三条用例在两次运行里都**不在**失败名单：`TestMemoryVerbCaptureStatsRunsAsAdvertised`、`TestMemoryHelpSurface*`、`TestRunCapturesTheResponseBodyAtTheRoundBoundary`（含 `internal/agent`、`internal/capturesurvey` 两个包整体绿）。
