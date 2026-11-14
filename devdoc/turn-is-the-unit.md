<!-- doc-class: normative -->
# devdoc: the TURN is the unit — session replay / log / stats print the ask, not the exchange

**Date**: 2026-10-22 · **Ground**: `/home/user/workspace/01a16301-a660-7da0-8e6f-5f84176bd0ae` (branch `master`)

## The operator's ruling

> 用户只认为……一个请求，就是用户手动输入或者语音输入的一个文字的请求，然后呢？
> 这中间 agent Harness 去跟这个 LLM 做了多少次的这个 request response 或者说
> tool call……所以你不能把这两个东西混为一谈

Read as a design rule: **a session surface's top-level unit is the TURN — one user
input.** Everything the harness ran to answer it (HTTP request/response exchanges,
tool calls, tool results) is that turn's DETAIL, printed as an annotation, never
promoted to a headline beside it.

## What was wrong before this change

The 2026-10-12 ruling had made the ROUND (one JSONL `request` row = one HTTP
request/response) the unit every session surface *steps through* and *tabulates*,
with the TURN as a neighbouring count. Measured on one live session
(`5af45c3e-24b6-4578-9f32-f4da2c40cde7`):

| surface | printed unit | value |
|---|---|---|
| `session replay` status line | round | `446` steps |
| `session log --rounds` | round | 446 rows |
| `session stats` headline | round | `rounds 446` |

The person had asked **120** times. A playback whose step count is 446 for 120
asks is the "混为一谈" the ruling names: the exchange was the subject and the ask
was the footnote.

The face that was ALREADY right is `gopi chat --replay`'s notice
(`chatReplayUses`, `cmd/agent/chat_replay.go`): `(%d turn(s), %d scripted
response(s), %d recorded tool result(s))` — the turn outside, the inner
quantities as a parenthetical. That is the reference shape, and it is what the
session surfaces now copy.

## The shape, and where each piece lives

| layer | what it now does | file |
|---|---|---|
| vocabulary | `CounterUnitTurnStep` / `CounterUnitTurnStepOne` / `CounterUnitTurnStepLabelFmt` / `CounterUnitTurnStepNoDetail`; the detail half `CounterDetailRequestsFmt` / `CounterDetailToolCallsFmt` / `CounterDetailJoin` / `CounterDetailOpen` / `CounterDetailClose`; the demoted label `CounterUnitInnerRequest` (+ `…One`) | `internal/sessionevent/counters.go` |
| the ONE span derivation | `SessionTurnSpan` + `SessionTurnSpans` (turn ordinal, iteration span, requests inside it, tool calls inside it, prompt); `SessionTurnRows` now reads THROUGH it | `internal/gopilog/sessionround.go` |
| the ONE label renderer | `FormatTurnStepDetail` / `FormatTurnStepLabel` — `turn 3/8  (5 request(s), 12 tool call(s))` | `internal/gopilog/sessionround.go` |
| replay material | `Material.Turns` (one per turn) beside `Material.Rounds` (the per-exchange sequence) and `Material.DumpFrames` (the screen); **three quantities, three names**: `len(Turns)`, `RoundCount`/`len(Rounds)`, `DumpFrameCount()`. The playback's own length is `PlayableUnits()` — turns when the log recorded them, the dump's frames otherwise, and that "otherwise" exists in ONE place. `TurnAt(i)` reads the TURN sequence only; `StepCount()`/`StepAt()` and their three-way fallback are GONE | `internal/replayplay/material.go` |
| replay player | walks `Turns` (`TurnAt`); the status line's position reads `turn 3/8`; a dump-only material falls back to its screen frames at the VIEW layer, not inside a count | `internal/replayplay/loop.go`, `render.go` |
| `session replay` listings | `--turns` (one line per turn, ONE headline number) and `--turn N` (one turn in FULL — every tool call with its result); `--steps` RETIRED BY NAME (exit 1, names the replacements); `--rounds`/`--round N` demoted to INTERNAL/DEBUG, marked so in `-h` and in their own first output line | `cmd/agent/cli_session_replay.go` |
| `session log --rounds` | header leads with the turn count and names the round count with the round's word; every row carries a `TURN` column derived from `SessionTurnSpans` | `internal/gopilog/sessionround.go` |
| `session stats` | box headline is `turns`, the line under it is `requests` under the request's own word; aggregates are labelled `per-request` because that IS what they are folded over | `internal/gopilog/stats.go` |

### The `--rounds` flag name: kept, and why

`--rounds` keeps its meaning on all three verbs. The reason is a contract, not
tidiness: `--round N`, `gopi session log <id> N` and `gopi session replay --round
N` form ONE address space (the exchange), and a listing flag that quietly stopped
listing that space would silently renumber an address callers already pass. So the
turn listing was ADDED instead of re-meaning the round one, and both help pages
say which sequence each lists. It has since been renamed `--turns`, and the round
views are marked INTERNAL/DEBUG (below): the round flag was never re-meant, and
the turn listing never became the round one.

### `step` was a name, not a unit — and it is gone (2026-10-25)

`Material.StepCount()` answered three questions under one name (the turn count,
the round count, the dump's frame count) with a silent three-way fallback, and
the CLI spelled the turn listing `--steps`. Both are removed: a count is now
`len(Turns)`, `RoundCount` or `DumpFrameCount()`, the playback's length is
`PlayableUnits()`, and the two flags are `--turns` / `--turn N`.

What `--turn N` buys, measured on the real session `80c07a7d-…`: turn 11 is one
operator row (`你自己去执行去看看`) that the harness answered with 10 requests and
8 tool calls. `--turn 11` prints **277 lines / 12436 bytes**, i.e. the whole ask;
the internal `--round 11` prints one of those ten exchanges (**32 lines / 1602
bytes**). Reading a person's question through the transport's address space loses
about 7.8x of it — which is the whole argument for one user-facing unit.

The turn partition loses nothing either: the `request(s)` counts on the 11 turn
rows sum to **152**, equal to `session log <id> --rounds`' `152 rounds`, and the
`calls=` counts sum to **134**, equal to the JSONL's `tool_use` / `tool_result`
block count.

`session log --rounds` and `session stats --rounds` were NOT given a `--turns`
twin: `session log --turns` already exists and is the turn table, and the stats
`--rounds` table is the per-exchange breakdown whose rows now name their turn — a
second table would be the same numbers under a second name.

### What did NOT change

- `SessionRounds` still counts `request` rows; `SessionTurns` still counts operator
  rows; the JSONL `iteration` field still numbers sends.
- The round address space (`--round N`, `session log <id> N`, the raw pairing, the
  `--rounds` table's rows) is byte-identical in meaning.
- `gopi chat --replay`'s output is untouched — it was the reference shape.

## Test changes, one by one, with the reason

| test | old assertion | new assertion | why the old one had to move |
|---|---|---|---|
| `internal/replayplay/counter_units_test.go: TestDumpFramesAreASecondMaterialNotSteps` | `StepCount() == len(mat.Rounds)` | `StepCount() == mat.TurnCount`, and explicitly `!= len(mat.DumpFrames)` | it pinned the step unit to the ROUND. The fixture (2 request rows, 1 operator row) is exactly the sample where the two numbers differ, so the assertion can now tell them apart instead of passing by coincidence |
| `internal/gopilog/counter_units_test.go: TestCounterUnitsAreDeclaredOnEverySurface` | header contains `"(%d round(s))"` | header contains `"%d rounds"` (round word + round count) AND `"%d turn(s)"` | the phrasing moved to `"N turn(s), M rounds"`; the quantity defended is unchanged |
| `internal/gopilog/counter_word_collision_test.go: TestCounterWordsNeverCarryTwoNumbers` | same parenthetical spelling | same pairing property, spelled against the constants (`strconv.Itoa(rounds)+" "+CounterUnitRound`, and NOT `Itoa(turns)+" "+CounterUnitRound`) | the test's subject is the (word, number) PAIRING, not the surrounding punctuation; asserting the shape made it a golden in disguise |
| `tests/fixtures/log-stats-raw/golden/stats-overview.txt`, `stats-overview-jsonl.txt`, `stats-json.json` | `rounds 9` / `per-round` | `turns 1` / `requests 9` / `per-request`, `"turns": 1` in JSON | regenerated with `GOPI_GOLDEN_UPDATE=1`; the diff is the change itself (fixture session `fc426fed`: 1 operator row, 9 request rows) |

New regression tests (Rule 10.5 — every behaviour change ships the smallest repro):

- `internal/gopilog/turnstep_test.go` — `TestTurnStepLabelSpellsTurnOuterDetailInner`,
  `TestTurnSpansCountTheRequestsInsideOneTurn`,
  `TestStatsOverviewLeadsWithTheTurnAndNamesRequestsBesideIt`.
- `internal/replayplay/turnstep_test.go` — `TestOneStepIsOneTurnThreeRounds`,
  `TestStepLabelCountsAreTheTurnsOwn`, `TestRoundAddressSpaceIsUnchanged`,
  `TestStatusLineNamesTheTurnAsItsUnit`.

Each uses a session whose turn count (1) and request count (3) DIFFER — the
property a fixture where they coincide cannot test.
