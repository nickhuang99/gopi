<!-- doc-class: normative -->
# devdoc: `session stats` 的 exchange 命名 —— 同一改名的第三个表面

> **ORTHODOXY RESTORED 2026-10-12 — this document's naming decision is REVOKED.**
> The operator's ruling: **round ≡ iteration ≡ one HTTP request/response (one JSONL
> `request` row)**, counting notify messages and human input alike. That is what the
> word ROUND names on every surface again; the operator-row quantity (what this
> document calls the human round) is the neighbouring quantity **TURNS**
> (`sessionevent.CounterUnitTurn`, `gopi session log <id> --turns`), and the
> `request`-row unit's own word went back from `exchange` to `round`
> (`sessionevent.CounterUnitRound`, `--rounds`). Read
> `devdoc/round-definition-restored.md` before implementing anything from the text
> below — the reasoning in this file is the HISTORY of the split, kept as a record
> of the path walked, not a description of master.


**Date**: 2026-10-11 · **Task**: `01a12b8a-7203-752f-a9a4-1b2945063b86`
· **Branch**: `task/01a12b8a7203752fa9a41b2945063b86` · **Base**: `a49d1616d`

## 缺陷

同一份 session 日志上三个表面数**同一个量**（一条 JSONL `request` 行 = 一次 HTTP
请求/响应），却有两个名字：

| 表面 | 改前印的字 | 数的量 |
|---|---|---|
| `session log <id> --rounds` | `SESSION EXCHANGES` / 列 `EXCHANGE` | exchange（已修正）|
| `session stats <id> --rounds` | `PER-ROUND` / 列 `RND` / 句 `canonical round, 1-based` | exchange（**本 bug**）|
| `session replay <id> --rounds` | `rounds=` | **human round**（operator 行）|

于是 `round` 一个词指两个量 —— 正是本次改名战役要消灭的东西。第三行是**另一个量**，
它才配用 round；stats 借用了那个词，就使 `stats` 与 `replay` 不可对读。

## 根因（实测读码）

`internal/gopilog/stats.go`：

- `roundBasis()` 对 jsonl 源返回**字面量** `"canonical round, 1-based (…)"`；
- `roundNoun()` **恒返回 `"round"`**，注释还写着 "Both spellings say \"round\" now" ——
  那是旧口径时代（"canonical round" 是唯一 round）的决定，在 `round` 被重新定义为
  人类轮之后就成了错的；
- 三处表头字面量 `PER-ROUND` / `PER-ROUND COST` / `BODY TRAFFIC`，三处列头字面量 `"RND"`。

**注意**：缺陷在**量的名字**，不在 `--rounds` 这个 flag。改 flag 会打断所有既有脚本与文档，
而 flag 拼写不是"量叫什么"的证据（`--rounds` 是 CLI 语法，Rule 14：选择器 ≠ 量的名字）。

## 修法（最小面积，Rule 4）

1. **值从常量包取**（Rule 1/13/14）：`sessionevent.CounterUnitExchange = "exchanges"`
   是 exchange 这个量的唯一拼写（由前置分支
   `task/01a12b69367c73e589d83dcf40cf2733` 定义，本 run 先 merge 它 —— 本 base 上
   该常量尚不存在，只有旧名 `CounterUnitRequestEvent`）。
2. `internal/gopilog/stats.go` 新增一处常量组 `statsExchangeColumnWord` /
   `statsExchangeMoneyHeader` / `statsExchangeWords` / `statsRoundWords`，以及
   `exchangeNoun()` / `exchangesNoun()`；**每一次印字都引用它们**，函数体里不再有
   `"EXCHANGE"` 字面量（Rule 13：定义而不引用 = 缺陷）。
3. `roundBasis()` / `roundNoun()` 改为按源返回常量：jsonl 源 → exchange，
   raw 源 → `CounterUnitRawRound`（raw 日志的 `→ REQUEST` 是**另一个语料**，
   它还会数 JSONL 从未记录的发送）。
4. 列宽 `%5s` → `%8s`：`EXCHANGE` 是 8 字符，宽度不改则列头与数据行错开。

## 陷阱：`round` 一词只允许出现在两处

`statsRoundWords = "round"` 是**唯一**允许拼出 round 的地方，而它只用于
`--rounds` 这个选择器的拼写（`(元 per exchange at the session's rate card; * = trim
cliff in --rounds)`）。任何**量的名字**都用 exchange（或 raw round）。
`internal/gopilog/counter_units_test.go` 新增的反向断言就钉这一点：
stats 三张表里出现 `PER-ROUND` 或 `RND` → 红。

## 红绿证明（不是"我说它有用"）

只把 stats.go 的表头还原成旧拼写（其它不动）：
`counter_units_test.go:104: the stats tables still head the exchange table with the
old spelling (PER-ROUND/RND)` → **FAIL**；还原本次改动 → `ok`。

## 为什么不改 `--json` 的键名

`"rounds"` / `"round_stats"` / `"trim_cliff_rounds"` 是机器可读 schema，
外部脚本与 `stats-json.json` golden 依赖它。改键名是**改 API**，不是改打印面 ——
本战役的既定做法是"改列名不改 API"（见 `devdoc/round-vs-exchange-naming.md`）。

## 为什么不重命名 `RoundStat` / `st.Rounds` / `BuildStatsFromRounds`

理由同上（内部标识符），另加一条实测教训：**未合并的兄弟分支正在同一文件同区域改动**，
全符号重命名必冲突。先 `git diff base...sibling` 看清它动了哪些文件哪些行，把改动限制在
它没碰的区域，是这次能一次合干净的原因。

## 验证

- `go build ./...` OK；`gofmt -l` 干净（`internal/gopilog/types.go` 在合并前就 gofmt-dirty，
  非本次引入）。
- 6 个 golden 重录（任务点名 4 个；`stats-overview.txt` / `stats-overview-jsonl.txt`
  也印同一段，一并重录），`git diff` 逐行确认**只有单位词/列名/列宽漂移，没有一个数字变化**。
- 同一份真实日志 `aa48a7eb`（169）：`stats --rounds/--bill/--bytes` 全印 EXCHANGE，
  `stats` 输出中 `round` **0 次**；`log` 印 `SESSION EXCHANGES`；
  `replay` 印 `rounds=29 exchanges=169`（human round 只有 replay 用 round）。
- 原始输出存档：`~/.gopi/sessions/01a12b8a-7203-752f-a9a4-1b2945063b86/artifacts/`
  （`baseline-stats-naming.txt` 改前、`final-three-surfaces.txt` 改后、`word-audit.txt` 词表）。
