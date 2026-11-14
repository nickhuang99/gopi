<!-- doc-class: historical -->
# Task Report: chat 状态行的两处缺陷 —— `⧉N` 被状态词挤掉、`ctx 1M/1M≡` 零信息

修复 operator 报告的两个 chat 状态行缺陷，全部取证在**生产组合路径**上（`setOpsFn` +
`setAvailCols`，即 REPL 真实安装的那条装配线），不是辅助函数的自洽演练。

## Completed

### 缺陷① —— 压缩计数 `⧉N` 被状态词的长度挤掉

- **现象**（operator 原话）："in idle, it shows `⧉30 · ready` … when request is sent, the
  `ready` becomes `responding`, it will hide the compaction number"。
- **根因**：`statusLiveField` 只有**一次**尝试 —— `counter + " · " + hint` 装得下
  `statusLiveCol`(13) 才显示。实测 `⧉30 · responding` = **15 格**，于是整个组合被放弃，
  连 `⧉N` 一起丢掉。
- **修法**：把"放弃谁"的优先级反转成 operator 说的那条 —— **计数 > 状态词的长度**。
  状态词多一个**短拼法**（`resp` / `think`，命名常量 `statusLiveRespondingShort` /
  `statusLiveThinkingShort`），块按 `statusLiveStateWords()` 给出的顺序逐个尝试：先全称，
  再短拼法；两者都不行，才轮到"丢掉计数"这条旧行为（保持可达）。
  `⧉30 · resp` = **11 格**，装得下。
- **短拼法是常量，不是截断**：`resp`/`think` 本身就是词，块从不 CUT 一个状态词，只选拼法。
  `sent`/`ready` **没有**短拼法 —— 它们和计数并排本来就装得下（9 / 11 格），给它们定义短形
  等于同一个字符串的第二份定义。
- **发现的第三例（operator 未报告）**：红证显示 `thinking` 同样把计数丢掉了（`thinking ` 5 格
  + 计数 5 格 + 分隔 4 格 = 14 > 13）。修复一并覆盖。
- 动画帧不被替换破坏：`statusLiveStateWords` 只在词的**前缀**上匹配、后缀原样带过，所以
  keepalive 的帧在两种拼法下是同样的格子。

### 缺陷② —— `ctx 1M/1M≡` 零信息

- **先取证的座标（原以为要从数学下手）**：`ctxMax` 是**部署值 200000**，不是 1M ——
  `internal/agent/context.go` 的 `DefaultContextWindow` = 200000，`CONFIG.md:186` 载明
  `context_window` 默认 `200000`，`cmd/agent/cli_chat.go:3591` 把它装进 `statusOps.ctxMax`。
  机器上**没有任何** `GOPI_CONTEXT_WINDOW` 覆盖（`~/.gopi/settings.json` 无该键、bashrc 无导出）。
- **所以 `1M/1M` 不是"窗口很大"，而是格式化器把 `150052/200000` 印成了两个 `1M`**：
  `formatCountBounded` 的每侧上限是 `statusCountWholeDigitsWidest`(2) 位整数，**两侧共用同一个
  上限**，于是 100000→200000 的每一对都塌成 `1M/1M` —— 双方各自**跨大范围**印同一个数，
  这是"数字看起来是一个测量、其实不是"这一类缺陷的直接形态。
- **修法（不改几何）**：`statusCtxPair` 先试粗形；**两侧塌成同一个图形**时，用**同一个阶梯
  函数、不同的位数预算**（`boundedCountRectDigits(n, statusCountCtxDigitsFine=3)`）重拼两侧，
  **且只在细形真的把两侧分开时**采用它。`150052/200000` → `150k/200k`。
- **为什么不用改几何**：`statusCtxField` 自己文档化的 FOLD —— 带标签装不下时**标签让位**、
  整对显示。细形 9 格 + 限定符 1 格 = **10 格 ⊆ 12 格子单元**，走的就是这条既有的 yield 路径：
  `statusCtxCol` / `statusMetricsCol` / `statusPinCols` / `statusPinSlackCells` **一格未动**
  （100 格 pin 每格都被占用且被绘制：`TestStatusReservationsAreFullyDrawn` 报
  `ctx 12/12, cache 19/19, group 33/33, tool 19/19, live 13/13`）。
- **细形分不开时保持粗形，并保住标签**：`199999/200000` 细形也是 `200k/200k`，此时粗形
  `1M/1M` 与细形说的是同一件事，于是**不花掉 `statusCtxLabel` 那格** —— 一个不多带信息的拼法
  不该是让"分母是什么"那格消失的那个。
- **有界性**：细形仍是**取整**，不是截断，对任意 `int` 有界
  （`1<<62-1` → `461G`，4 格 = `statusCtxPairSideCellsFine`）。为此把 `boundedCountRect`
  参数化为 `boundedCountRectDigits(n, maxDigits)`（单参形式变成一行调用，阶梯/进位/零尾数修补
  全是同一份 body，不可能漂移）。
- **未采用的两条备选**（记录理由，防止后人回头再试）：
  (a) **只把精度给"内存侧"** —— 需要给 pair 加一个 `ctxMemUsed` 字段并打通 loop 的调用链，
      超出本次会话预算；
  (b) **从命中率/way-marker 借格** —— 那会撤销 operator 明确要求过的 1 位小数命中率
      （`statusHitRateDecimals`）与 tool 列宽度，属于拿一个特权去换另一个特权。

### 测试

- `cmd/agent/chat_status_live_compaction_priority_test.go`：计数优先级 + 短拼法表 +
  短拼法几何，**含 fixture 自检**（先断言"该 fixture 真的还是那个缺陷"：`⧉30 · responding`
  = 15 > 13，fixture 失效则测试红）。
- `cmd/agent/chat_status_ctx_pair_zero_information_test.go`：pair 全程不为两个不同计数画同一图形、
  细形宽度上界、细形装得进子单元、分不开时保标签，外加**渲染行**断言
  （`TestStatusRowCtxPairOnTheRenderedRow`，`lineedit.DisplayWidth(row) <= 100`）。

## Not Done

- **缺陷②还有更深一层的根因，本次未修（已派发）**：`ctxUsed` 里**没有** API 实测的
  prompt token —— 它是 `EstimatedTokens()` 的字符启发式（约 4 char/token）。取证：
  `internal/agent/loop.go:5391` 与 `5601` 是 `SetPromptTokens` 的**唯一两个**调用点，
  分别服务非流式与流式路径；而 chat 会话里唯一带 usage 的 SSE 帧是 `message_delta`，
  它只有 `output_tokens`（`input_tokens` 只在 `message_start` 的 `message.usage` 里）。
  **实测**：`~/.gopi/sessions/*/console.log` 里 95 条压缩日志，`measured` **95/95 全是 0**；
  压缩日志自己说的是 `of 200000`（= 部署窗口），而本地**没有**任何窗口覆盖。即
  `SetPromptTokens` 在流式路径从不落地，压缩的触发与显示都建立在字符估算上
  （正是 operator 报告的"compaction 后 EstimatedTokens 虚高"那一层）。**这是独立的一个 bug，
  不是本次修法的一部分**，已作为后续任务派发。
- **未跑真实 PTY 端到端 chat**：状态行是纯函数，本次取证停在"生产装配线 + 渲染行"这一层；
  真实 chat 的端到端（arm + 真实 SSE + keepalive）未在 60 分钟预算内跑完 —— 首答前没有压缩，
  故端到端只能证明 keepalive 帧不崩，信息量低于上面那两层断言。

## Verification

- Build: `go build ./cmd/agent/` → OK（每个改动点后各跑一次；gofmt clean）
- Tests: `go test -run 'TestStatus|TestChatPTY|TestLiveField|TestLadder' ./cmd/agent/`
  → `ok github.com/nick/gopi/cmd/agent 13.328s`（改后）
- Ran: `go test ./cmd/agent/ -run 'TestStatusLiveCompactionCounterOutranksTheWidestStateWord|TestStatusLiveStateWordsAreNamedConstantsAndStatedAlternatives|TestStatusLiveShortWordsFitTheReservedColumn|TestStatusCtxPairNeverDrawsDistinctCountersAsOneFigure|TestStatusRowCtxPairOnTheRenderedRow' -v`
- Result: 5/5 PASS（改后）；把**旧逻辑**在内存里还原后同一组测试 → `FAIL`
  （`chat_status_live_compaction_priority_test.go:51` 逐个状态报
  `drops the compaction counter: "responding "` / `"thinking "`；
  `chat_status_ctx_pair_zero_information_test.go:45` 报
  `renders the pair "1M/1M": two DIFFERENT counters drawn as one figure … ("100k"/"200k")`），
  再还原修复 → PASS。**红→绿两向都实测过**。
- Commit: 见本文件所在提交（`git log -1 --format=%H`）

## Evidence (self-filed)

- 缺陷①宽度的实测（生产宽度函数 `statusFieldCells`）：
  `⧉30 · responding` = 15 格 > `statusLiveCol` 13；`⧉30 · resp` = 11 格（修复路径）。
- 缺陷②的实测（生产格式化器，直接调用，`go test -run TestZZProbe*` 临时探针，已删除）：
  `formatCountBounded(150052) = "150k"`、`formatCountBounded(200000) = "1M"` —— **两侧共用
  2 位上限**才是塌陷的原因；`statusCtxPair{used:150052,max:200000}` 改前 `"1M/1M"`、
  改后 `"150k/200k"`；修正后 `statusCtxField` = `"150k/200k≡"` = 10 格 ⊆ `statusCtxCol` 12。
- `statusCtxPairCellsFine` = 2×4 + `statusCtxPairSepCells`1 = 9；9 + `statusCtxQuantCells`1 = 10 ≤ 12。
- 几何不动的实测：`TestStatusReservationsAreFullyDrawn` 输出
  `air audit: ctx 12/12, cache 19/19, group 33/33, tool 19/19, live 13/13`（改后仍 PASS）。
- 窗口座标的实测：`ctxMax` 在 chat 里 = 200000（`DefaultContextWindow`，`CONFIG.md:186`），
  无 `GOPI_CONTEXT_WINDOW` 覆盖；压缩日志 40 条报 `of 200000`。
- `measured 0` 的实测：`grep -c 'measured 0' ~/.gopi/sessions/<id>/console.log` = 32（该会话），
  全部会话合计 95 条压缩日志中 95 条 `measured` 为 0。
