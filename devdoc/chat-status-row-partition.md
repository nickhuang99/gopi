<!-- doc-class: normative -->
# Devdoc: the `gopi chat` status row's column partition

**Task:** 01a0db449a4e7bb38ff8117b7ca2b150 (branch `fix/chat-status-row-column-budget`)
**Files:** `cmd/agent/chat_stream.go` (the geometry + composition),
`cmd/agent/chat_status_width_test.go` (new width/degradation test).
**Read-only reference:** `internal/lineedit/answer.go` (the row's composer — untouched by this run).

## 1. What the row's width IS

The aligned status row is composed by `lineedit.statusLineColumns`
(`internal/lineedit/answer.go`), which knows the row's width as:

```
row = sum(column widths) + (n-1) * StatusColumnSepCells + StatusLineGapCells + LiveWidth
```

with `StatusColumnSepCells = 4` (StatusSep `" · "` = 3 cells + 1 cell of air) and
`StatusLineGapCells = 2`. Both are defined once, in `internal/lineedit/answer.go`
(Rule 14), and read here — the separator is measured in **cells**, never with
`len()` (`StatusSep` carries a multi-byte `·`).

The budget is `statusLayoutBudgetCols = 100` and it is not a knob: the guard

```go
const _ = uint(statusLayoutBudgetCols - statusColumnsAtCols)
```

fails the **build** when the geometry does not fit (a negative constant converted
to `uint` does not compile). This run keeps that guard and moves the pin to
**95** — headroom **5** cells.

## 2. The reservation table (this run's partition)

| # | field | col const | cell | line | content | widest form |
|---|---|---|---|---|---|---|
| 1 | model | `statusModelCol = 16` | 0 | chat_stream.go:215 | `modelShort(model)` | cut with `…` inside the column; contract floor 14 (`deepseek-flash`) |
| 2 | iter | `statusIterCol = 7` | 20 | chat_stream.go:218 | `iterLabel(iter, live)` | `iter 9999` (5 digits) + slack |
| 3 | mode | `statusModeCol = 5` | 31 | chat_stream.go:220 | the mode hint (`tools`) | fixed word |
| 4 | tool | `statusToolCol = 17` | 40 | chat_stream.go:231 | `indentStatusToolField` | `⚙ ` (2) + a 14-cell name = 16; contract floor 16, is cut inside the column |
| 5 | tokens | `statusTokensCol = 20` | 61 | chat_stream.go:237 | `formatCount(tokens) + " tokens"` + heavy suffix | `12.3k tokens (large)` = 20 (measured); a six-digit count's heavy form (22) is cut inside the column |
| — | live | `statusLiveCol = 12` | right edge | chat_stream.go:246 | the hint (+ the queued marker) | `responding` = 10 + one animation frame = 12 (display contract); the queued marker is cut first by the block |

`statusColumnsAtCols` (chat_stream.go:262) is the **sum of the table plus the
separators and the gap**, one definition:

```
16 + 7 + 5 + 17 + 20 = 65            sum(columns)
4 separators × 4     = 16
StatusLineGapCells   =  2
statusLiveCol        = 12
                       ---
statusColumnsAtCols  = 95   (≤ 100, headroom 5)
```

Start cells (verified by the new test, derived from the same constants, never
copied): model 0 · iter 20 · mode 31 · tool 40 · tokens 61 · live 95−12 = 83.

### Why these widths, and not the WIP's

The WIP commit `c561c97e` (branch `task/01a0db1fcf9677e98c7525b6142db371`, read
for reference, **not** continued on — Rule 23) fitted seven columns by reserving
`statusModelCol = 10` and `statusToolCol = 6`. That is what its two red tests
say: `"deepseek-flash"` is 14 cells and `"keyword_search"` is 14, so a 10-cell
model column and an 8-cell tool field (`⚙ keyword…`) cannot carry them.
`TestChatStatus_ColumnsHoldStillWhileValuesChange` and
`TestChatStatus_ToolRowRegression` are **display contracts and are not editable**
(this run edits neither), so the two fields' floors are the partition's fixed
points — the rest of the row is what may move.

## 3. The degradation table (what a narrow terminal shows)

The aligned form is chosen from the **terminal width alone**
(`Status.alignedFits`): below the pin the row falls back to the historic
unaligned composition, which **keeps the whole fields that fit, drops the rest
from the right, and keeps the live hint** — the live hint is the row's last
yield. Measured this run (long model `glm-5.3-flash-long-model-name`, tool
`read internal/lineedit/answer.go`, `iter 3`, 12345 tokens):

| cols | row |
|---|---|
| **95 (pin)** | `glm-5.3-flash-l… ·  iter 3  ·  tools ·  ⚙ read internal/l… ·  12.3k tokens          ready` |
| 80 | `glm-5.3-flash-long-model · iter 3 · tools  ready` |
| 60 | `glm-5.3-flash-long-model · iter 3 · tools  ready` |
| 40 | `glm-5.3-flash-long-model · iter 3  ready` |
| 30 | `glm-5.3-flash-long-mod…  ready` |
| 20 | `glm-5.3-flas…  ready` |
| 12 | `glm-…  ready` |

The ladder is monotone (a field dropped at width *w* is never back at a narrower
one) and the row is **exactly one screen row at every width** — both asserted by
the new table-driven test over every second width from the pin down to 10.
Master's measured rows are identical at 80/60/40/30/20 (the aligned form is
unreachable on master too there, because `statusColumnsAtCols` was 99); the
pinned row differs only in the cell positions, which is what the repartition
changes.

## 4. Field → counter → provenance

| field | what it prints | counter | source |
|---|---|---|---|
| model | the provider/model name | `Agent.Model` | `cmd/agent/cli_chat.go:2302` (`newChatStatusSource(ag.Model, …)`) |
| iter | the in-flight turn, else the completed count | `iterCount` (REPL loop) / `live` | `cmd/agent/cli_chat.go:616`, `:753`, `:796` (`publishChatStatus`) |
| mode | `tools` when tool mode is on | `sess.toolsEnabled` | `cmd/agent/cli_chat.go:579` |
| tool | the tool running, or the last one that ran | `status.setTool/Done/clearTool` | `cmd/agent/cli_chat.go:743/745/795/863` (the agent's tool events) |
| tokens | the session's running token total | `ag.Cost.TotalPromptTokens + TotalCompletionTokens` | `cmd/agent/cli_chat.go:788`, `:861` |
| live | `thinking…` in flight, else `ready` | `live > 0` → `Status.Thinking` | `cmd/agent/cli_chat.go:616/753/796/864` |

Every number on the row is one the REPL already maintains: **this run adds no
new counter and no new state**, which is why the row could be re-partitioned
without touching the turn loop.

## 5. The honest omissions

The epic asked for the Hermes operational read-outs. They are **deliberately not
on this row**, and the reason is arithmetic, not preference: at five columns the
geometry has 5 cells of headroom and a sixth column costs 15 at the very least
(11 for the narrowest candidate read-out plus 4 for its own separator), so making
room for one means cutting a display contract — `statusModelCol` below 14, the
tool column below 16, or `statusLiveCol` below 12, i.e. exactly the red state
this run exists to clear.

| omitted field | counter that exists | why it is not a column |
|---|---|---|
| ctx (`ctx 12.3k/200k 6%`) | `Agent.Ctx.EstimatedTokens()` (`internal/agent/context.go:374`) + `Agent.ContextWindow` (`internal/agent/loop.go:2273`) | 16 cells at its widest; the only column it could replace is the token column, whose start cell **and count** are pinned by the unedited column contract (`tokensNow` = 999/1000 must appear in the row). |
| cache (`↑h ↓m`) | `ag.Cost.TotalCacheHitTokens / TotalCacheMissTokens` (`internal/deepseek/claude_client.go:305-306`) | needs a labelled pair (two counters, two glyphs) ≥ 12 cells, and the glyphs are ambiguous without a legend the row has no room for. |
| cost (money) | `CostTracker` totals × the price card | 11+ cells with a currency marker; the amount is an ESTIMATE on every transport gopi speaks today (they report tokens, never money), so the row would have to carry a hedge character to stay honest (`c561c97e`'s `statusCostExact` is that observation) — 12+ cells. |
| elapsed (`9m 12s`) | the session clock (`sessionStart`) | 8 cells at its longest honest form, and it is the least load-bearing read-out: the session log's `session_end` records the same wall time. |
| compressions | `Agent.CompactCount` (added by `c561c97e`) | Hermes shows it only when non-zero; as a column it would be empty on almost every repaint while still reserving ≥ 9 cells. |

**The token counter is deliberately NOT used as a context read-out.** The number
the REPL publishes (`TotalPromptTokens + TotalCompletionTokens`) is the session's
**accumulated** total: it grows monotonically and is unbounded by any window, so
`tokens / ContextWindow` would be a fabricated percentage (it exceeds 100% after
enough turns). The context's own pair is the honest one, and it is the pair the
ctx column would have shown — which the geometry cannot host.

**One inherited limitation, recorded rather than fixed here.** The live block is
sized for the hint itself (`responding` + one animation frame), so its own cut
takes anything appended to it: the queued-turn marker renders `ready · 1 …`, and
master's phase words (`sent` / `thinking` / `responding`, composed by
`liveHintText` — a commit that landed on master while this run was in flight and
was integrated by the delivery-point sync) render their bare word with the
animation tail cut (`thinking…`). Master's own live block was 12 cells, so the
cut predates this re-partition; widening the block to hold a marker needs 4 cells
the five-column geometry does not have, and taking them from a display contract
is the trade this run refuses to make. It is reported in the task report as
**Not Done**.

## 6. Verification (Rule 10.2 — the rendered row, not an OCR of a screenshot)

- `go build ./...` — clean; the budget guard compiles with headroom 5.
- `go test ./cmd/agent/...` — green, including the two unedited contracts
  (`TestChatStatus_ColumnsHoldStillWhileValuesChange`,
  `TestChatStatus_ToolRowRegression`), `chat_panel_rows_test.go`,
  `chat_flow_rows_test.go`, `chat_turn_layout_test.go`, `lineedit_height_test.go`
  and the new `chat_status_width_test.go`.
- `go test ./internal/lineedit/` — green, `palette_status_salience_test.go`
  byte-identical to master (`git diff master -- internal/lineedit/palette_status_salience_test.go` empty).
- The rows in §3 are the **actual** `Status.Line(cols)` output at each width,
  captured with an in-tree probe test (a `t.Logf` of the real row, then removed).

## 7. Anchor repair, re-keyed rather than permitted (task 01a101fb)

§4's model row names the symbol `newChatStatusSource(ag.Model, …)`, and this doc
is a **standing claim about master** (not a landed record), so the drifted
accelerator line was **re-keyed by symbol** to the line that call now occupies —
never permitted as drift: a permit records a landed record's dated measurement,
and re-keying one would falsify what it measured.
