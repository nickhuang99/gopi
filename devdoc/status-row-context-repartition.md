<!-- doc-class: historical -->
> **Retired mechanism (document-pipeline teardown, slice 3/5).** `tools/docclass/`
> was deleted: a doc's truth class now lives in its own line-1
> `<!-- doc-class: … -->` header, written by the write path
> (`internal/tools/header_hook.go`) and by no generator. The
> `python3 tools/docclass/…` commands quoted below are the RECORD of what a past
> run executed — they cannot be re-run, and no document may be classified by
> running them.
> **§3's geometry is SUPERSEDED by Addendum 2 (run `01a0ddab`): the ctx sub-cell is
> 17 (label + pair), the group 26, the pin 99, the named slack 1, and the window
> percentage is off the aligned row.** The numbers below are the `01a0dcd1` state
> and are kept as that run's record.

# Task Report: the status row's re-partition — money sub-cell retired, context read-out made absolute

Task `01a0dcd126f17830af19420a6bf6710a` · branch `task/01a0dcd126f17830af19420a6bf6710a`

## Completed

### 1. The money sub-cell is gone from the shared status row

`cmd/agent/chat_stream.go` no longer carries a money column, a money field
formatter, or the constants they were spelled with (`statusCostCol`,
`statusCostField`, `statusCostEstimatedMarker`, `statusCostCurrency`), and
`statusOps` no longer carries the three counters that fed them (`costYuan`,
`costOK`, `costExact`). `cmd/agent/cli_chat.go`'s `statusOpsFor` no longer calls
`pricing.CostOf` — its only remaining cost-adjacent read-out is the cache pair,
which the TRANSPORT reports about itself.

A **RETIRED note** sits where `statusCostCol` was, stating what was removed and
why, so a later reader does not re-add it:

- the amount was never MEASURED — the transports gopi speaks report token
  volumes, never a price, so the figure was derived from (price card generation ×
  Beijing civil time × a scraped price page), which is why `pricing.CostOf` takes
  a timestamp and why `dsV4FlashOld`/`dsV4Pro` are already retired generations;
- what reached the screen was `·~0.00元`: a hedged, sub-precision figure spending
  seven cells. The operator's verdict is quoted as the design rule the row now
  follows ("You cannot just display a '·~0.00元'. this is a fooling, and user is
  not a fool!");
- **`internal/pricing` is NOT deleted** and its counters are not dropped: it has
  live callers outside this row (`cli_chat.go` priced the session for the row;
  `cli_stats.go`'s `gopi stats --cost` is the surface where a bill belongs,
  computed once, off the repaint path). What is retired is the ROW's money CELL,
  not money. This is about the row.
- the freed cells are spent, not reserved-but-undrawn (the retired-cache-column
  defect): they are the context pair's own width.

### 2. The context read-out is absolute (Hermes shape)

`statusCtxPair` is the ONE definition of the pair (`"42.1k/64.0k"`, or the bare
count when no window is known, or empty when nothing is measured), and
`statusCtxField` composes the sub-cell in Hermes' order:

```
"42.1k/64.0k ▓▓▓▓░░░░░░ 65%"   the widest form (pair + bar + percentage)
"42.1k/64.0k 65%"              the pinned form (pair + derived percentage)
"42.1k"                        tokens known, no window → no denominator, no %
""                             nothing measured; the column holds its cells
```

The percentage is **derived from the pair** (`statusCtxPct(used, max)`), never
from a stored ratio, so the two cannot disagree. `statusCtxBase` (pair + `%`, no
bar) is the unaligned label's form — so the absolute pair reaches the screen
below the ladder's floor as well, and the historic bare token total is replaced
everywhere by the same number measured against its window.

### 3. The arithmetic, re-derived (budget 100 / pin 98)

| quantity | before | after |
|---|---|---|
| `statusLayoutBudgetCols` | 100 | 100 (unchanged — it is the ceiling) |
| `statusPinCols` (the drawn/widest aligned form) | 100 | **98** |
| `statusColumnsAtCols` | 100 | **98** (`= statusPinCols`, asserted at compile time) |
| `statusMetricsSubCells` | 3 | **2** |
| group sub-cells | ctx 9 + cache 7 + cost 7 | **ctx 16 + cache 7** |
| `statusMetricsCol` | 27 | **25** (16 + one 2-cell field seam + 7) |
| `statusPinSlackCells` | — | **2** (the named headroom) |
| ctx sub-cell | `9` (pct + bar) | `16` = pair 10 + pct 4 + 2 gaps 1 + **bar 0** |

The ctx sub-cell is derived, not hand-summed:
`statusCtxBarCells = statusCtxCol - statusCtxPairCells - statusCtxPctCells -
2*statusCtxBarGapCells`. It derives to **zero** in this geometry, which is a
MEASURED fact rather than a stylistic choice: the absolute pair plus the
percentage plus a bar beside them does not fit the 100-column row once the model,
tool and live-block contracts are paid. `statusCtxField` therefore draws the pair
and the percentage (the epic's acceptance) and turns the bar ON by arithmetic
alone if a future geometry affords it — the cell is never reserved for a bar it
cannot draw.

Compile-time gates (all in the build, not in a test):

```go
const _ = uint(statusLayoutBudgetCols - statusColumnsAtCols)  // geometry ≤ budget
const statusContextColumnCells = statusColumnsAtCols          // occupancy == pin
const statusPinSlackCells = statusLayoutBudgetCols - statusColumnsAtCols
```

`statusMetricsSubCellWidths()` is the ONE list the group draws from, and
`TestStatusMetricsGroupArithmetic` ties its length to `statusMetricsSubCells` and
its derived width to `statusMetricsCol` — so a read-out added or removed cannot
leave the row summing a number the drawn group no longer matches.

### 4. Evidence of the real row (the log this task's acceptance asks for)

`go test -run TestChatStatus_WidthDegradation -v ./cmd/agent/` prints the composed
row at every width in the ladder — that log IS the render evidence, because the
test is the invocation. Measured on the branch (probe state: deepseek-flash, iter
12, a live `keyword_search`, a 42.1k/64.0k context, a cache pair, a 4m00s clock):

```
cols=100 width= 98 |deepseek-flash ·  iter 12 ·  tools ·  ⚙ keyword_search ·  42.1k/64.0k 65% ·↑12k↓4k   4m00s · ready|
cols= 98 width= 98 |deepseek-flash ·  iter 12 ·  tools ·  ⚙ keyword_search ·  42.1k/64.0k 65% ·↑12k↓4k   4m00s · ready|
cols= 97 width= 97 |deepseek-flash ·  iter 12 ·  tools ·  ⚙ keyword_search                              4m00s · ready|
cols= 69 width= 69 |deepseek-flash ·  iter 12 ·  tools ·  ⚙ keyword_search  4m00s · ready|
cols= 49 width= 49 |deepseek-flash ·  iter 12 ·  tools  4m00s · ready|
```

No money field at any width; the absolute pair and its derived percentage on the
row wherever the metrics group survives.

### 5. Tests added or re-derived

| test | what it now proves |
|---|---|
| `TestStatusRowCarriesNoMoneyAtAnyWidth` (new) | every width from the budget down to 10, plus the unaligned label, spells no price — the glyphs are literals because the production constants were deleted |
| `TestStatusRowCtxReadOutIsAbsolute` (new) | the pair is on the row and LEADS the percentage; a **different window** (1M) reaches the row, so the percentage is visibly derived from the pair it travels with |
| `TestStatusMetricsFieldsAreDrawnWithoutMovement` (new) | the pair starts on the group's reserved cell across every width the group survives (cell-grid measurement) |
| `TestUIStatusRowCarriesNoMoney`, `TestUIStatusRowIsMeasuredInCells` (new, `internal/uiclient`) | the SECOND surface's own status row spells no price and is cell-measured |
| `TestStatusMetricsGroupArithmetic` | derived count + width + sub-cell list agree; the ctx composition is self-consistent; `statusCtxBarGap` measures what the arithmetic counts |
| `TestStatusLadder_FormsNarrowStrictly` | the ladder starts at the PIN and the pin + named slack == the budget |
| `TestStatusLadder_FormPerWidth` | `cost` is false at every width (the retirement, not a shed), and the cache pair stays INSIDE the group's cells |
| `TestStatusRowRendersAtThePin`, `TestStatusRowDegradesInTheDocumentedOrder`, honesty/band tests | re-derived for the two-sub-cell group |
| `TestChatStatus_FieldsStartOnTheirReservedCells`, `_ValuesCannotWidenTheRow`, `_HeavyContextWarnForm`, `_WidthLadderAndProvenance` | the money probes became cache-pair probes; the ctx assertion is INVERTED (the pair must now be on the aligned row); the "huge value" case is the widest cache pair the sub-cell can carry |

The ladder's form widths (98 / 69 / 49) are asserted against the production
constants, not restated.

## Files

| file | change |
|---|---|
| `cmd/agent/chat_stream.go` | money sub-cell retired (RETIRED note); ctx = absolute pair + derived percentage; geometry 3→2 sub-cells, group 27→25, pin 98 + named slack 2; derived `statusCtxBarCells` |
| `cmd/agent/cli_chat.go` | `statusOpsFor` no longer prices the session for the row |
| `cmd/agent/chat_status_ctx_provenance_test.go` | NEW: no-money sweep, absolute-pair provenance, cell-grid stability |
| `cmd/agent/chat_status_metrics_test.go` | arithmetic derives from the sub-cell list; pin render; band/honesty cases re-shaped; log line corrected |
| `cmd/agent/chat_status_degradation_test.go` | probes → cache pair / ctx pair / money glyph; degrade state; adversarial widest cache pair |
| `cmd/agent/chat_status_ladder_test.go` | `cost=false` at every width; cache pair asserted as a CELL range; pin == geometry sum, sum + slack == budget |
| `cmd/agent/chat_status_width_test.go` | start cells from `statusMetricsCol`; omitted list carries the currency glyphs |
| `internal/uiclient/ui_status_surface_contract_test.go` | the second surface's absence + cell-measured row contract |
| `devdoc/status-row-context-repartition.md` | the report (design + measured rows + addendum) |
| `devdoc/DOC-INDEX.tsv` | this report classified; the duplicate row dropped |

## Not Done (honest)

- **"Both surfaces" is NOT one shared row, and the briefing's premise needs
  correcting.** `gopi chat` composes the grouped status row
  (`cmd/agent/chat_stream.go`) through `internal/lineedit.Status`. `gopi ui`
  (bubbletea, `internal/uiclient`) is a different surface with its own
  `statusLine()` — `state · turns N · tool events N`, truncated to the window —
  and **shares no cell geometry with that row**. So "the shared status row" is a
  claim about the PRODUCER, not about both screens: `gopi ui` never drew the
  money sub-cell and never drew a context read-out, because it does not use
  `chat_stream.go` at all. What this task delivered for the second surface is its
  own absence proof (`internal/uiclient/ui_status_surface_contract_test.go`), and
  the `gopi ui` row captured from a real run (below) is reported as measured
  rather than as a claim invented for the occasion. Giving `gopi ui` the
  absolute context read-out would be a second design task (its status line has no
  column reservation to draw one in) — dispatched separately if wanted, not
  silently claimed here.
- The **bar** is not drawn in the pinned geometry (`statusCtxBarCells = 0`). The
  derivation is armed and tested (the moment the sub-cell can afford it, the bar
  composes), but a reader who expects Hermes' `[████░░░░░░]` on today's row will
  not see one. Buying the bar costs 8 cells and requires shedding a field —
  a design decision this task does not make on the operator's behalf.
- The bar-free form means the epic's phrase "percentage/bar derived beside it" is
  only half realisable at 100 columns; the pair and the percentage are both on
  the row.

## Verification

- **Build**: `go build ./...` — OK (exit 0); `go vet ./cmd/agent/` — clean;
  `CGO_ENABLED=0 go build -o /tmp/gopi-ctx ./cmd/agent/` — OK.
- **Tests**:
  - `go test -run 'Status|Ladder|ChatStatus' ./cmd/agent/` — PASS (11.0s), the
    whole status-row family including the new provenance tests.
  - `go test ./internal/uiclient/ -run 'TestUIStatusRow'` — PASS.
  - `go test ./cmd/agent/ ./internal/lineedit/ ./internal/uiclient/ ./internal/standards/`
    — see the commit's environment note; run in a background job (transcript in
    the session directory).
- **Ran** (the surfaces, not `-h`):
  - `stty cols 100 rows 30; /tmp/gopi-ctx ui` under `script -qec`, on a real
    terminal-shaped pty. Its status row measured through that transcript:
    `idle · turns 0 · tool events 0` — no price, no percentage, and the identity
    handshake line above it (`gopi ui: identity http://127.0.0.1:8090/api/identity
    (messages=0 turns=0 tokens=0 …)`).
  - `/tmp/gopi-ctx chat -h` → the usage page (the `--once` and interactive faces).
  - The grouped row itself at 100/98/97/69/49 columns — the table in §4, produced
    by `TestChatStatus_WidthDegradation` and reproduced with a temporary probe
    harness (since deleted) that drove the real `chatStatusSource`.
- **Result**: the pinned row carries `42.1k/64.0k 65%` and `↑12k↓4k`, occupies
  exactly 98 cells inside the 100-cell budget, and no row at any width carries a
  currency glyph.
- **Commit**: see the branch tip (`git log --oneline -1`).

## Evidence (self-filed)

- `go build ./cmd/agent/` → `exit=0`; the first attempt printed
  `initialization cycle for statusPinCols`, and `statusPinCols` was rewritten as
  the geometry's own sum (a const may not call the derivation helper) — the
  compile-time guard then compiled and the drawn-width assertion became a const.
- `go test -run 'TestStatusMetricsGroupArithmetic|TestStatusLadder_FormsNarrowStrictly' -v ./cmd/agent/`
  before the change → PASS with the log
  `group: ctx 9 + cache 7 + cost 7, separators 2 x2 = 27 cells; row 100/100`.
- `go test -run 'Status|Ladder|ChatStatus' ./cmd/agent/` after the change → PASS.
- The measured pin: `pin=98 budget=100 metricsCol=25 ctxCol=16 barCells=0`,
  `ctxField="42.1k/64.0k 65%"`, `ctxBase="42.1k/64.0k 65%"`.
- `gopi ui` transcript (`script -qec … /tmp/gopi-ctx ui /tmp/ui.log`): the status
  row line read from the captured bytes is `idle · turns 0 · tool events 0`.

## Addendum — the close-out gate, re-measured on a real terminal (2026-09-26, run 01a0dcd1)

Three findings from re-running the branch's own gates AFTER the commit, and what
each one changed. All three are in this branch's later commits (`e171f83a`,
`33df2190`).

### 1. The standards gate was RED on this branch — the report had no manifest row

`go test ./internal/standards/ -run TestDocManifestCoversEveryDevdocFile`:

```
docclass_gate_test.go:91:  devdoc/DOC-INDEX.tsv:762: duplicate row for devdoc/docindex-write-path-row-ownership.md
docclass_gate_test.go:104: devdoc/status-row-context-repartition.md: no row in devdoc/DOC-INDEX.tsv —
                           every file under devdoc/ must be classified
```

The second line is this run's own defect: the report was written into the corpus
(§"Not Done (honest)" of the first half never said so) and left unclassified, so
the gate that says "every file under devdoc/ has a row" was red. Fixed the way the
corpus's own generator asks for it — `python3 tools/docclass/classify.py .
--only devdoc/status-row-context-repartition.md` — plus the optional banner
(`<!-- doc-class: historical -->`, which is what the write TOOL puts on a new
devdoc file; a report written by `write` gets one, one written by the shell does
not, and the gate does not require it).

The FIRST line was not this run's: the duplicate row is byte-identical at lines 10
and 762 on **master** (`git show 79807932:devdoc/DOC-INDEX.tsv` shows both), so the
gate had been red on master since `108ca498`. The later copy is dropped — the
manifest's own header says one row per path, and a duplicate is an insert the
generator never performs.

### 2. The e2e red was a stale-binary artifact, not a defect

`--- FAIL: TestEditToolAnnotatesTeachingDocsInAuditedTrees` reported that the
SUITE's build of the binary (mtime 17:03:22) was older than
`cmd/agent/chat_stream.go` (17:06:03) — the test refuses to verify code that is
not in the binary it drives. Re-run alone after the build: PASS (1.9s). Nothing in
this branch's diff touches that path; the suite's shared fixture binary simply
predated the last edit of the run that produced it.

### 3. The row, on a real pty (Rule 31)

```
gopi chat   (cols 100, `script -qec "stty cols 100 rows 30; ./gopi chat"`)
  painted row: "deepseek-flash ·  iter 0  ·        ·                   ·                             0s · ready"
  currency glyphs in the whole transcript: 0
gopi ui     (`script -qec "stty cols 100 rows 30; ./gopi ui"`)
  painted row: "idle · turns 0 · tool events 0"
  currency glyphs in the whole transcript: 0
```

Both tables (the row at the pin, and the ladder from 100 to 24) are measured in
§4 by the suite, which is the surface that composes the row at every width; a live
`gopi chat` idle session shows the unmeasured form, and a real turn was driven to
give the row counters (`gopi chat --once --tools`: `read go.mod` → `bash` →
answer, three tool events, no currency glyph anywhere in the transcript).

## Addendum 2 — the context read-out re-partitioned again (2026-09-26, run 01a0ddab)

The geometry in §3 above is **superseded**. This addendum states what that task
changed and why; the numbers in §3 (ctx sub-cell 16, pin 98, slack 2) are the
`01a0dcd1` state and are kept as that run's record, not as the current row.

### 1. What the operator reported

Reading his own live row: *"the percentage is still there and I don't know what
does that help. also '13.1k/200.0k'? what is this? cache-hit/cache-miss? '6%'? I
think this is most meaningless percentage."* Two defects, not one: the pair had no
name while a second pair of the same unit sat beside it, and the percentage was
read as a cache hit rate.

### 2. The two decisions

- **The pair is named**: `ctx 13.1k/200.0k` — 3 cells, a WORD rather than a glyph
  or a colour, because the row's distinctions must survive with the palette off
  and because the disambiguation is "what is this number", which no symbol answers.
- **The percentage is folded off the ALIGNED row.** Both operands are already
  drawn there (the pair states used AND max), so it restated the pair's own first
  number over its own second at lower precision. Re-labelling (`6% ctx`) lost on
  cells — 16 for a confirmation against 13 for a name. The **unaligned fallback
  keeps its ratio**, deliberately: it reserves nothing, cuts a label from the
  right, and often carries no pair, so the ratio can be the only complete read-out
  that survives the cut. The new test asserts BOTH sides.

### 3. The arithmetic, re-derived (budget 100 / pin 99)

| | `01a0dcd1` (superseded) | `01a0ddab` (current) |
|---|---|---|
| ctx sub-cell | 16 = pair 10 + pct 4 + 2 gaps | **17** = label 3 + gap 1 + pair 13 |
| group | 25 | **26** |
| pin | 98 | **99** |
| named slack | 2 | **1** |
| `statusCtxBarCells` | 0 | 0 |

**The freed cells went to a reservation bug, and that is the substantive find.**
`statusCtxPairCells` claimed 10 as `5 + 1 + 4`, where the 4 was a PERCENTAGE's
width rather than the second side of a pair. `formatCount` really prints `"200.0k"`
— six cells — for the production window (`DefaultContextWindow`, 200000), so the
reservation was **cutting the operator's own pair**: `ctx 13.1k/200.0k` is 16 cells
against the 14-cell sub-cell that 10 bought. The mis-measure survived because every
fixture in §5's test list uses a 64k window (`"64.0k"`, 5 cells) — the deployed
window was never an input. The honest ceiling is now derived
(`2 * statusCtxPairSideCellsWidest + statusCtxPairSepCells`) and asserted against
`formatCount`'s real output for `200000`.

The sub-cell also now equals `statusMetricsCol` exactly, so the group's content no
longer overflows its own reservation — the "one cell the group's content can spend
over" note in §3 is gone, and pin and pin-sum are one number.

### 4. The row (Rule 31)

Ladder, real renderer, printed by `status_row_render_test.go`:

```
cols=117 width= 99 deepseek-flash ·  iter 9  ·  tools ·  ⚙ keyword_search ·  ctx 13.1k/200.0k ·↑12k↓4k   4m00s · ready
cols=100 width= 99 deepseek-flash ·  iter 9  ·  tools ·  ⚙ keyword_search ·  ctx 4.2k/64.0k ·↑12k↓4k     4m00s · ready
cols= 80 width= 80 deepseek-flash ·  iter 9  ·  tools ·  ⚙ keyword_search             4m00s · ready
cols= 40 width= 38 deepseek-flash · iter 9  4m00s · ready
cols= 24 width= 24 deepseek…  4m00s · ready
```

`200.0k` reaches the screen whole at 117 and at 100/99 — the number the old
reservation cut. At 24 the metrics group is shed and nothing is pushed off the row
(the state word survives).

### 5. A pair wider than the reservation yields the LABEL, not a cut number

Above a million tokens a pair side is 7 cells (`"1000.0k"`), one past the
reservation. The form drops the label and draws the pair whole, because a
truncated denominator is the one thing this sub-cell must not print and because
the compile-time guard forbids a wider row. Asserted at the boundary (a 1M window)
in the ctx-provenance test.

### 6. Files

`cmd/agent/chat_stream.go` (reservation re-derived, `statusCtxLabel` added, the
percentage folded off the aligned field, the label-yields narrow form) plus
`chat_status_ctx_label_test.go` (NEW), `status_row_render_test.go` (NEW, the Rule
31 instrument), and the four status test files whose anchors moved with the
sub-cell. Commit `5497a039`, branch `task/01a0ddab-2a5d-7bc3-8029-e4e1a489043c`.

### 7. Not done (honest)

- No live model turn populated the sub-cell: the pty runs reach the row at
  `iter 0` with empty context, so §4's rows are the shipped composer driven by the
  operator's counters, not a photograph of a turn.
- The whole-package `go test ./cmd/agent/ -count=1` run reported FAIL at 421s and
  its failing test names were lost to a `tail -30`; the scoped status/chat suites,
  `internal/standards/` (all six gates) and `internal/lineedit/` are green.


`chat_status_metrics_test.go`'s arithmetic log still printed the three-sub-cell
sum's slot list and the pin-render doc comment still named `statusCostField`
(deleted) as its anchor. Both re-worded; `33df2190`.
