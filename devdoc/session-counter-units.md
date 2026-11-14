<!-- doc-class: historical -->
# Session counters: four numbers, four producers — naming each surface's unit

**Status:** delivered. **Measured on** the session named below, gopi @ `4937e4dc0`.

## The defect

One session answered *"how many rounds was this?"* with **four different numbers on
four surfaces**, each naming its unit without saying what that unit counted:

| surface | line it printed |
|---|---|
| `gopi session log <id> --chat` | `Iterations: 12` |
| `gopi session log <id> 12` | `SESSION … — ROUND 12 of 28` |
| `gopi session replay <id> --frames` | `frames=27` and `turn K/24` |

Not one number was wrong. Each counts a different real thing. **The defect was in
the words** — and it was expensive: the briefing this work answers *attributed all
four to the wrong producers* (it read `28` as the raw HTTP round count), which is
exactly the misreading a reader without a unit declaration can only make.

## The four producers — measured, not inferred

Session `1988d8a0-874b-4f2a-976b-2e8dd37f0079` (2026-10-04), `session.jsonl`
sha256 prefix `75d6c736145f9592`, its paired `*.raw.log` (120295 lines).

| # | counter | value | what it counts | producer (code) | when it differs from its neighbour |
|---|---|---|---|---|---|
| 1 | **loop iterations** | **12** | the agent loop's own count | `session_end`'s `total_iterations` — written by `LogSessionEnd` (`internal/agent/session.go:443`), fed by `iterCount++` (`cmd/agent/cli_chat.go:3679`) | it is **per REPL turn**, not per model exchange. For this chat it counted 12 turns against 31 exchanges |
| 2 | **request events** | **31** | every send to the model the JSONL recorded | one per `request` row — `LogRequest`/`session.go:428`; counted by `reqCount++` (`internal/gopilog/reader.go:215`) | always ≥ #3 (the reader drops empty ones) and ≤ #4 (the raw log numbers sends the JSONL never saw) |
| 3 | **session rounds** | **28** | a `request` row **that carried content** | `SessionRounds` = `len(s.Iterations)` (`internal/gopilog/sessionround.go:35`); the filter is `flushIter` (`reader.go:111`) | **= #2 − the empty exchanges.** The reader flushes at every `request` **and once at the end** — 32 flush points here, of which **4 carried nothing and were dropped** → 28 (measured; the 4 empty spans are `iter=8, iter=5, iter=0, iter=0`) |
| 4 | **raw HTTP requests** | **31** | every `→ REQUEST` marker in the raw log | `ParseHTTPRounds` (`internal/gopilog/rounds.go:149`); marker const `markerRequestPrefix` (`rounds.go:20`) | ≥ #2 — the raw log also numbers non-streaming meta calls and retries the JSONL never recorded (`k` ∈ 0..4 across the profile) |
| 5 | **turns** | **24** | one user/assistant **message** carrying text | `LoadJSONLAt` (`internal/replayplay/material.go:402`), `Turns: len(turns)` | a long turn **wraps**, so #6 ≥ #5 |
| 6 | **frames** | **27** | one frame **row-set** | `Material.Frames` — one per wrapped page, plus dump frames | ≥ #5: here 24 turns → 26 event frames + 1 `screen` frame = 27 |

## The two relations that are easy to get backwards

- **#1 ≠ #3, and neither is a bug.** They come from *different sources*: the loop
  counts turns it ran; the reader counts rows a log holds. A chat turn that makes
  several ReAct exchanges makes them differ (12 vs 28 here). A session where each
  turn is exactly one exchange makes them agree — **a coincidence, not a
  guarantee**, which is precisely why every surface must name its unit rather than
  let the numbers look interchangeable when they happen to match.
- **#3 ≤ #2 ≤ #4.** The reader can only *drop* rounds (empty content), so #3 ≤ #2;
  the raw log sees sends the JSONL never logged, so #2 ≤ #4. The raw offset is
  **not constant**, which is why the two are paired by **token identity**
  (`completion_tokens` ↔ `output_tokens`), never by an offset (memory #6017/#6021).

## What changed

Naming and self-declaration only — **no counting semantics were altered**. The
values 12 / 28 / 31 / 31 / 24 / 27 are byte-identical before and after.

1. **`internal/sessionevent/counters.go` (new)** — the ONE home for the unit names
   (`CounterUnitLoopIteration`, `CounterUnitRequestEvent`, `CounterUnitSessionRound`,
   `CounterUnitRawRequest`, `CounterUnitTurn`, `CounterUnitFrame`) and the provenance
   words. Rule 1/13/14: a leaf package with zero project imports, already shared by
   `internal/gopilog`, `cmd/agent` and `internal/tools` — so two surfaces cannot
   drift into calling one unit by two names.
2. **COST SUMMARY** (`FormatChat` / `FormatSession`) now reads
   `Iterations: 12  (loop iterations — session_end total_iterations)` and adds a
   `request events: 31` line. The provenance word is printed because the **same
   label counts a different quantity** on the live-session path: when no
   `session_end` exists the reader derives `TotalIterations` from request events
   (`reader.go:256`), so `CounterSourceDerived` must say so (Rule 15).
3. **`gopi session log <id> <N>` header** (`FormatSessionRoundWithRaw`) names its
   unit, its denominator, **and the two neighbours it differs from** — this is the
   line that would have prevented the briefing's misattribution.
4. **`--rounds` table header** — the exact birthplace of the misreading (a table
   whose last row read `28   28?` was taken for "28 raw rounds"): the ROUND and RAW
   columns now each name their unit, with the `?` marker explained.
5. **`gopi session replay --frames`** — now prints `turns=24 (…)` beside
   `frames=27 (…)`, each with its definition, instead of leaving `turn K/24` and
   `frames=27` to be read as two measurements of one thing.
6. **`Session.RequestEvents`** (new field) and **`Material.Turns`** (new field) carry
   the counts the surfaces needed; `RequestEvents` is set unconditionally
   (`reader.go:250`), not only on the fallback path.

## Files

- `internal/sessionevent/counters.go` — new: unit names + provenance words
- `internal/gopilog/types.go` — `Session.RequestEvents`
- `internal/gopilog/reader.go` — set `RequestEvents`; `iterationCountSource` input
- `internal/gopilog/formatter.go` — COST SUMMARY (both views) + `iterationCountSource`
- `internal/gopilog/sessionround.go` — round header + `--rounds` table header
- `cmd/agent/cli_log_round.go` — resolve the raw total for the header cross-reference
- `internal/replayplay/material.go` — `Material.Turns`
- `cmd/agent/cli_session_replay.go` — `--frames` unit declarations
- `internal/gopilog/counter_units_test.go`, `internal/replayplay/counter_units_test.go` — new
- `tests/fixtures/log-stats-raw/golden/log-chat.txt`, `log-session.txt` — re-recorded
  (the only output change: the two COST lines)

## The RAW column's unit word carried "round" — renamed (2026-10-11)

A follower-up bug on the same table (`gopi session log <id> --rounds`): the column
header still spelled its RAW unit **"raw HTTP rounds"**, so ONE SCREEN used the one
word for two quantities — beside the header's own line naming the *human* rounds
(operator rows). Measured on session `aa48a7eb-a5ab-4507-9c4e-b717bf3583cd`: 169
exchanges, and only 29 operator rows in the log.

- `CounterUnitRawRound = "raw HTTP rounds"` → **`CounterUnitRawRequest` =
  `"raw HTTP requests"`** — the identifier AND the value both drop the word
  (GOPI.md Rule 14: the word ROUND names the human round and nothing else).
- The constant's own doc now carries the measurement and says why the bare word has
  one meaning; `counters.go`'s MEASURED line reads "exchanges 31 … raw requests 31".
- 2 printed surfaces (`sessionround.go` — the single-exchange header and the
  `--rounds` table header) and 3 test references (`counter_units_test.go`) moved with
  it; both devdoc cross-references were updated. **The CLI flag `--rounds` was NOT
  touched** — the defect was in the column's unit word, never in the flag.
- Not renamed: the API family `RawRoundOrdinal` / `pairRawRounds` /
  `RawRoundRequest` — those number the RAW LOG's rounds, which is its own honest
  noun, and renaming them would collide with the unmerged sibling branch (below).

### Merge note — the unmerged sibling collides HERE

Sibling `task/01a12b69367c73e589d83dcf40cf2733` (task `01a12b69-367c`, still
`in_progress`, ref at `e1cfffa1e`, NOT yet merged) renames
`CounterUnitRequestEvent` → `CounterUnitExchange` in the SAME two regions. Measured
with `git merge-file` against base `a3a31da42`: **3 conflict hunks in
`counters.go`, 1 in `counter_units_test.go`, 0 in `sessionround.go`** — all of them
"both sides edited this block", none semantic. Resolve by taking the sibling's
exchange wording AND this change's `CounterUnitRawRequest` line, keeping both.

## Verification

- `go build ./...` — clean
- `go test ./internal/gopilog/ ./internal/replayplay/ ./internal/sessionevent/` — ok
- `go test ./cmd/agent/ -run 'TestGoldenCLILog|TestGopiLogRound|TestG4'` — ok (the
  help-surface inventory and the round-selector pins are untouched; no new help row
  was added, so the G4 inventory did not need a new entry)

The three real invocations and their output are in the task report.

## Not done / honest gaps

- **The fixture cannot exercise a session where #1 ≠ #3.** The committed fixture has
  `TotalIterations == RequestEvents == 9`. The disagreement is therefore pinned
  *structurally*, by a purpose-built log in
  `TestCounterRelationsCanDisagree` (three request rows, one empty, `total_iterations`
  = 1) — not by a real fixture session. Adding the measured session 1988d8a0 as a
  fixture would need the operator's profile copied into the repo, which Rule 39 and
  the fixture corpus's own rules exclude.
- **`--frames` for a dump-only material reports `turns: unknown`** rather than 0: a
  dump reconstructs no turns, so there is no number to state, and printing `turns=0`
  would assert an absence that was never measured (Rule 15).

---

## 2026-10-26 — 后续（task 01a17492）：显示层把两个单位相加

上面记录的是"四个量各自命名"。本条记录的是**同一个会话仍在被误读的第二个原因**：
显示层把两个不同单位的数**相加**了。

### 实测（会话 `41e5dade-71e1-4efc-a012-456d0532e762`）

```
$ gopi session log 41e5dade-… | grep -E "Turns|Passes"
  Turns:      38  (turns (operator rows) — …)
  Passes:     465  (loop passes — derived from JSONL  events — this session recorded no session_end)

$ gopi session replay 41e5dade-… --turns | head -1
session 41e5dade-…  source=<…>  geometry=<WxH>  turns=38

$ gopi session list --mode chat | grep 41e5dade
41e5dade-… [chat] … (running, model deepseek-flash, repl turns 73, …)
```

信息栏（`chat-screen.dump.json` 第 23 行原文）：

```
deepseek-flash ·  turn120 ·  no tool   ·  135k/200k≡ ·↑63M↓2MΣ97.3%Δ99%      ⧉11 · ready
```

四表面：`turn120` / `Turns 38` / `repl turns 73` / `replay turns=38`。

### 根因 A：`chatTurnDisplay` 加的是两个不同的量

```go
func (s *chatSession) chatTurnDisplay(local int) int { return s.turnBaseline + local }
```

- `turnBaseline` = `gopilog.SessionTurns(s)` —— **操作者行**。
- 调用点传的 `local` = `iterCount` —— REPL 每跑一趟 +1，**含 `notify` 投递趟与自我驱动趟**。

本会话分解（按 `session.jsonl` 统计）：

```
resume 前操作者行 = 0
本 run 操作者行   = 39
本 run notify 行  = 44
iterCount 口径    = 120   ← 信息栏印的数
操作者行口径应为  = 39
```

38 与 82 相加，得 120 —— 和不是其中任何一个量。`repl turns` 也**不是**这个和的两倍：
它是 `chats.turn_count`（每次 `RecordChatTurn` +1，只记模型完成的回合），是第五个量。

### 根因 B：不存在的 pass 数被伪造

`internal/gopilog/reader.go`（以及 `stats.go` 的同形代码）：

```go
if !sawEnd && reqCount > 0 {
    s.TotalIterations = reqCount   // request 行数冒充 loop pass 数
```

`TotalIterations` 是 **loop pass**（`session_end.total_iterations`），`reqCount` 是 **round**
（一个 `request` 行 = 一次 HTTP 交换；一个回合几十个 round）。二者只在"一回合一个 round"的
早期会话里碰巧相等 —— 这就是这行活了这么久的原因。于是 `session log` 印出
`Iterations: 468` 与 `Passes: 468`：**同一个数两个词，且两个都不是 pass 数**。

### 修法

1. **删除伪造回退**（`reader.go` / `stats.go`）：没记录就保持 0，由
   `Session.EndUnrecorded` / `SessionUsage.HasIterations` / `SessionSummary.EndUnrecorded` 说明。
2. **`iterationCountText`**（`internal/gopilog/formatter.go`）：记录的 pass 数印数，没记录印
   `sessionPassUnrecordedDash`（`—`）+ `sessionevent.CounterPassUnrecorded` 的完整说明；三处渲染点
   （COST 块、`--turns` 表头、`--rounds` 表头）全部改用它。summary 表用 `summaryPassText`，
   `summaryRowFmt` 的列说明符同步 `%d → %s`。
3. **显示计数器统一单位**：`chat` 主循环加 `operatorTurns`，只在
   `chatCountsAsOperatorTurn(turnFace)` 为真时 +1（`human`/`queue` 真；`notify`/`llm`/空 假 —— 与
   reader 的 `IsHumanRoundBoundary` 同一判据）。信息栏、正文标题、`turnStatus.begin` 全改用它；
   `iterCount` 回到它唯一的职责：写 `session_end` 的 pass 数。

### 验证

```
$ make build
CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/

$ ./bin/gopi session log 41e5dade-… | grep -E "Iterations|Turns|Passes"
  Iterations: 476  (rounds — one JSONL `request` row = one HTTP request/response = one iteration)
  Turns:      38  (turns (operator rows) — …)
  Passes:     —  (loop passes — not recorded — this session wrote no `session_end`, so the loop never
                  reported its pass count (the `request`-row count is the ROUNDS, a different quantity))

$ ./bin/gopi session log --all --summary | head -3
29cb5919-… deepseek-flash      25.1s      — PASSES 400,829 tok  OK  …
b4563c0f-… deepseek-flash     221.0s     40 PASSES 2,614,600 tok  OK  …
```

### 未做（诚实）

- **`repl turns` 未改**：它是 `chats.turn_count`，与 `Turns` 是不同量，本身不算错；但与 `Turns` 的差
  （本会话 78 vs 38）读者从任一表面都看不出原因。更彻底的做法是 `session list` 也印操作者行数。
- **信息栏新编号未在真实屏幕上取证**：`operatorTurns` 通过了 `go build` 与单测，但屏幕上的数要变，
  需要一个带 `resume` 的新 `gopi chat` 会话 —— 本 run 不能重启操作员正在用的那个（pid 331978，
  会话 41e5dade 仍 live）。

### 回归守卫

- `TestParseNoSessionEnd`（`internal/gopilog/reader_test.go`）改为断言 `TotalIterations == 0` 且
  `EndUnrecorded == true` —— 它原来把伪造值 1 钉成期望，等于把缺陷写成契约。
- `cmd/agent/chat_turn_display_unit_test.go`（新增）：`TestChatCountsAsOperatorTurn` 钉四种来源；
  `TestChatTurnDisplayAddsLikeUnits` 复现缺陷算术（38+82=120）并断言正确算术（38+39）；
  `TestSessionPassCountIsNotTheRoundCount` 钉"pass 词 ≠ round 词"且"未记录"有独立措辞。
