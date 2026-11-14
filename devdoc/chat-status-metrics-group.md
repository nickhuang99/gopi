<!-- doc-class: historical -->
# Task Report: the chat status row's metrics GROUP (ctx · cache · cost as ONE column)

Task `01a0db58fda671248c3e5a2a82401a48`, branch **`feat/chat-status-metrics-group`**
(worktree `/home/user/workspace/gopi-01a0db58fda671248c3e5a2a82401a48`).

## The problem, stated once

`gopi chat`'s aligned status row is a fixed-width layout over a 100-column budget
(`lineedit.BudgetWidth`). The epic wants the Hermes operational read-outs on it:
context used/max (+ percent, + fill bar), prompt-cache read/write, session spend,
elapsed time, the compaction count.

Measured, EIGHT top-level columns do not fit — and the reason is the separator,
not the content: `lineedit.StatusColumnSepCells` is **4**, spent once per column
boundary by the aligned composition, so every extra top-level field costs its
content **plus 4**. Three more columns cost 12 cells the row does not have.

## The solution: one column, three fields, a light inner join

The three operational read-outs are **one column** (`statusMetricsCol`) whose
sub-cells are joined internally by `lineedit.StatusSepGroup` (`" ·"`, **2** cells).
A top-level boundary stays `StatusColumnSep` (`" ·  "`, **4** cells): that pair of
separators IS the row's grammar — a column boundary vs a boundary between fields
inside one column.

Measured geometry (every number a named constant in `cmd/agent/chat_stream.go`):

| column | constant | cells | carries |
|---|---|---|---|
| model | `statusModelCol` | 14 | `modelShort` — "deepseek-flash" fits UNCUT (the measured worst case on this host) |
| iter | `statusIterCol` | 7 | `iterLabel` ("iter 12") |
| mode | `statusModeCol` | 5 | `statusToolsHint` ("tools") |
| tool | `statusToolCol` | 16 | `indentStatusToolField` (glyph + " " + 14-cell name: "keyword_search") |
| **metrics** | `statusMetricsCol` | **29** | the group: ctx **10** · cache **7** · cost **7** + 2 inner joins × 2 |
| separators | `statusColumnSepTotalN` × `StatusColumnSepCells` | 16 | 4 top-level boundaries |
| gap | `StatusLineGapCells` | 2 | fields block ↔ live block |
| live | `statusLiveCol` | 12 | state word / animation frame / compaction count |
| **total** | `statusColumnsAtCols` | **100** | = `lineedit.BudgetWidth` (exactly) |

`statusMetricsCol` is DERIVED
(`statusCtxCol + statusCacheCol + statusCostCol + (statusMetricsSubCells-1)*statusMetricsSepCells`),
so a sub-cell edit cannot leave the geometry summing a stale number, and the
compile-time guard `const _ = uint(statusLayoutBudgetCols - statusColumnsAtCols)`
fails the build if a column is added without a budget for it.

### Field → counter → source (provenance)

| field | rendered by | counter | read from |
|---|---|---|---|
| ctx percent + bar (+ `!` warn) | `statusCtxField`, `statusCtxPct`, `statusBar` (█/░, `statusBarCells`=4), `statusCtxHeavyMarker` | `Agent.Ctx.EstimatedTokens()` / `Agent.ContextWindow` (+ `PromptTokens` = the measured flag) | `statusOpsFor`, `cmd/agent/cli_chat.go` |
| ctx token volume (no window known) | `statusCtxBase` (`statusTokensUnit`) | the same tokens | `statusOpsFor` |
| cache read/write | `statusCacheField` | `Agent.Cost.TotalCacheHitTokens` / `TotalCacheMissTokens` (`internal/deepseek/types.go`) | `statusOpsFor` |
| cost | `statusCostField` (`~` = estimated) | `pricing.CostOf(model, hit, miss, out, start)` → CNY `res.Total` | `statusOpsFor` |
| elapsed | `statusElapsedLabel` | session start → now | `statusOpsFor` |
| compactions | `statusCompactionLabel` (`⧉`) | `Agent.CompactCount` (incremented at the one success return of `compactContext`, `internal/agent/compact.go`) | `statusOpsFor` |

Every one has an UNMEASURED branch: a counter nobody measured renders **nothing**,
never a zero (`statusOps.ctxMeasured` / `costOK` / `costExact` / `elapsedKnown` /
`compressionsKnown` exist so the row can say nothing instead of something false).

### Degradation, measured (the row's own steps)

| band | form | rendered (quoted from the test run) |
|---|---|---|
| 100 | five columns | `deepseek-flash ·  iter 12 ·  tools ·  ⚙ keyword_search ·  65% ██░░ ·↑12k↓4k ·~0.12元    ⧉2 · ready` |
| 99 | metrics group shed (as ONE step, ctx with it) | `deepseek-flash ·  iter 12 ·  tools ·  ⚙ keyword_search  ⧉2 · ready` |
| 67 | tool shed | `deepseek-flash ·  iter 12 ·  tools  ⧉2 · ready` |
| below that | model · iter · mode, then the historic unaligned composition | `deepseek-flash · iter 12  ⧉2 · ready` → `deepseek-flash  ⧉2 · ready` → `⧉2 · ready` |

Inside the group the order is **ctx · cache · cost** and the row's tail cut reaches
the rightmost first, so a group too narrow for its content loses cost, then cache,
and keeps the context read-out (the number that predicts a compaction) longest —
the epic's documented order (cache → cost → ctx) expressed as geometry rather than
as a second list.

## Completed

- The metrics group: `statusMetricsCol` + the three sub-cells + the light inner
  join; `statusMetricsField` is left-anchored (a leading unmeasured read-out costs
  no seam) and its seams between rendered sub-cells are HELD (a value appearing
  later cannot slide the one before it).
- Five-column geometry; model 14 (name uncut), tool 16, ctx 10, cache 7, cost 7,
  live 12, metrics 29 — **exactly 100** reserved, rendered 98 (the live block's
  trailing padding is trimmed, as the composition always did).
- The live column stays at 12: master pins the responding state + its animation
  frame at exactly 12 cells (`TestStatusLiveWordsFitTheirColumn`), so the clock
  the block can no longer hold beside the state is the trade the metrics group
  costs — and the reason `statusLiveField` gives the compaction count priority
  over it when idle.
- The WARN form: a context at `statusTokensHeavy` marks its sub-cell with
  `statusCtxHeavyMarker` ("!") — a GLYPH, not a second colour, because the row is
  GOLD (`palette.go` statusFg) and a warn that exists only as a colour is one the
  row cannot show with colour off. The mark rides inside the sub-cell's cells, so
  it moves nothing.
- The ladder reduced to the row's real steps (metrics → tool → be-spine), named
  `statusColMetrics` instead of the two separate ctx/cost columns.
- `formatCountCompact` (12345 → "12k") for the cache sub-cell: the one-decimal
  pair is 10 cells where the sub-cell is 7.
- Context field: the percent + bar is the sub-cell's form; the used/max pair
  (10 cells + bar = 15) cannot share it, so the pair survives as the
  no-window form and in the unaligned composition. Which form a VALUE gets is a
  function of the value's width, never of the terminal's — a field that changed
  shape on resize would be the jumping-row defect at a different seam.
- Live block priority: the clock yields to the state word while a turn is in
  flight (the in-flight words are 9/11 cells of a 12-cell block); when idle the
  compaction count outranks the clock (both cannot ride the block: "0:42 · ready"
  is the whole reservation).
- Elapsed time and the compaction count are now RENDERED (the counters existed
  unrendered before).
- `lineedit.StatusSepGroup` + `StatusSepGroupCells` (derived from `StatusSepCells`,
  so the two spellings cannot disagree) and `lineedit.BudgetWidth` — the pin's ONE
  definition, replacing the drifted `const pinned = 99` copy in
  `internal/lineedit/turn_layout_test.go`.
- `lineedit.Status`'s phase machine wiring restored and kept compatible: the live
  field still composes from the source's own phase words
  (`statusLiveThinkingWord = statusLiveWordThinking`, Rule 1b — spelled from the
  constant, not retyped).
- `cmd/agent/chat_status_metrics_test.go`: the group's arithmetic, the row rendered
  at the pin and at EVERY narrower width, the ladder's measured steps, the jump
  regressions (long model / six-digit count / many-digit cost / huge context /
  unmeasured everything / long tool summary), and the group's honesty branches.

## Not Done (honest)

- **Task `01a0db44` (branch `fix/chat-status-row-column-budget`) is rewriting the
  same file with a DIFFERENT design** — it reduces the aligned row to four columns
  and deliberately omits ctx/cache/cost. Whichever of the two lands second must
  merge; this branch is a self-contained grouped geometry on master's phase
  machine. Nothing here was committed on `task/01a0db1fcf9677e98c7525b6142db371`:
  that branch was read with `git show` for its formatters/ladder and its design
  was re-partitioned, not adopted.
- The ctx used/max PAIR is not on the pinned row (only the percent + bar). Reason:
  the pair plus a bar is 15 cells; the pair is the wider form, kept for the case
  where no window is known (the volume alone) and for the unaligned label path.
  A real reduction of what the epic listed, not a rounding detail.
- The context sub-cell is 10, which is NOT wide enough for the historic token
  spelling "1.0k tokens" (11): the volume form is bare ("42.1k"), and the unit
  survives only in the unaligned (`Status.Label`) composition. Every cell of the
  partition is spoken for and the live column is master's own 12-cell contract.
- Cache values are rendered compact ("↑12k↓4k") — the decimal is traded for the
  pair fitting the sub-cell.
- The row was NOT driven through a real PTY session in this run: its geometry and
  its band behaviour are executed by the test suite at every width from 100 down
  to 1, and the REPL wiring (`statusOpsFor` + `setOpsFn` + `setAvailCols`) is
  exercised by `go build`/`go vet` only. A live `gopi chat` render is the one
  piece of Rule 31 evidence this branch does not carry.

## Verification

Build: `go build ./...` → OK (no output, exit 0)
Tests: `go test ./cmd/agent/ -run 'TestStatus|TestChat|TestChatStatus|TestChatTurn|TestChatPanel|TestChatFlow|TestChatLive|TestLineeditHeight|TestBareRun' -count=1`
       → `ok github.com/nick/gopi/cmd/agent 10.544s`
       `go test ./internal/lineedit/ -count=1` → `ok ... 38.453s`
       `go test ./internal/standards/ -run 'Gate|Mirror|NamedLimit|Heavy|Rule' -count=1`
       → `ok github.com/nick/gopi/internal/standards 45.985s`
Ran: the row is RENDERED by the suite (Rule 31) at the pin and at every narrower
width; the exact rows are quoted in the degradation table above. Verbatim:

```
go test ./cmd/agent/ -run 'TestStatusRow|TestStatusMetrics|TestStatusLong|TestStatusContextWarn' -v -count=1
  pin row (98 cells): "deepseek-flash ·  iter 12 ·  tools ·  ⚙ keyword_search ·  65% ██░░ ·↑12k↓4k ·~0.12元    ⧉2 · ready"
  cols=100 cells=98, cols=96/88/80/72 cells=66, cols=60 cells=46, cols=40 cells=36, cols=26 cells=26, cols=12 cells=10
  step full                     cols=100
  step metrics group shed first cols=99
  step tool next                cols=67
  warn:  29% █░░░ (no mark) → 30% █░░░! (mark), same cells, mode column unmoved
```

Result: 6/6 of the new geometry tests PASS; the pin row is 98 of the 100-cell
band (the live block's trailing pad is trimmed by the composition, as it always
was); every band from 100 down to 1 composes inside itself; each fixed column
keeps its start cell across all six value-jump cases; the heavy-context mark
appears without moving the row.

### Evidence (self-filed)

- `go build ./...` → exit 0, no output.
- `go test ./cmd/agent/ -run 'TestStatusRow|…|TestBareRun' -count=1` → `ok` (10.5s),
  which includes master's own contracts on this row: `TestStatusLiveWordsFitTheirColumn`
  (the 12-cell live column), `TestChatStatus_ColumnsHoldStillWhileValuesChange`,
  `TestChatLiveHintWalksTheTurnStates`, `TestChatFlowRows`, `TestChatPanelRows`,
  `TestLineeditHeight`, `TestBareRunKeepsAllItsArtifactsInOneSessionDir` (the
  built-binary freshness check).
- `go test ./internal/lineedit/ -count=1` → `ok` (38.5s): the separator cell
  widths, the aliveness/salience palette test (UNCHANGED — no theme-resolved SGR
  was introduced), and `TestStatusAlignedLayoutFitsARealTerminalWidth` against
  `lineedit.BudgetWidth`.
- `go test ./internal/standards/ …` → `ok` (46.0s): Rules 16–21 gates + the
  named-limits and heavy-inventory gates.
- The compile-time budget guard is in the tree and green:
  `const _ = uint(statusLayoutBudgetCols - statusColumnsAtCols)`, both sides 100
  (`statusLayoutBudgetCols = lineedit.BudgetWidth`).
- **Not observed in this run**: a live `gopi chat` session on a real terminal
  (the `## Not Done` item above), and the pre-existing failures of the unrelated
  worker/merge/cwd-release tests in this environment (ssh to the peer host is
  refused; an unrelated Rule 22 device-id fixture) — they do not touch this diff
  and were not re-run for this report.
- `git status --porcelain` in the worktree → empty (Rule 30).

## Rework: landed on the moved master (2026-09-26, run 01a0dbe2)

The branch's ref was pruned while master moved on independently (33 commits over
this same chat status row plus a further pair retiring the top-level cache column
and adding the geometry guard). The tip (`59580ebe`) was preserved as
`rework-backup/feat/chat-status-metrics-group` and master was merged INTO it, so
the branch's provenance survives rather than the diff being replayed.

**The union, in one sentence**: the row carries FIVE top-level columns — model,
iter, mode, tool and the metrics GROUP — and the group joins ctx, cache and money
on light field separators inside it, which is what pays for master's 13-cell live
block while the whole row is still exactly `statusLayoutBudgetCols` (100).

Where the two sides disagreed, and why:

- **The group replaces master's ctx + cost columns as a TOP-LEVEL column.** Not
  "the cache column came back": master's RETIRED note (the arithmetic shows a
  seventh column of 10 cells plus its separator cannot fit the zero-slack budget)
  is about a top-level column, and the group is the geometry that CAN hold the
  read-out — it spends a field separator instead of a column boundary. The cache
  renderer and its two direction glyphs were reintroduced as the group's SUB-cell
  renderer, and master's retired-column guard still holds: `statusRowColumns()`
  carries the group (one column), so no name in the ladder lacks a column behind
  it.
- **`statusColumnSepTotalN` is 4, not master's 5** — five top-level columns, one
  of which (the group) pays a single separator for three read-outs.
- **The ladder is 3 steps (100 / 69 / 49)**, the group being ONE step. Master's
  cost -> tool -> ctx priority expressed a priority between two top-level columns;
  with one operational column the group sheds atomically (the branch's documented
  order), and the internal priority survives as the group's own left-to-right
  policy: a tail cut takes cache first, then money, and keeps the context read-out
  longest. Master's `statusShedOrder`/`statusColumnNames` now name `metrics`; the
  phantom-step guard is unchanged and still proves itself red.
- **`statusLiveField` is master's 3-argument signature** (master's own
  `TestStatusLiveWordsFitTheirColumn` calls it), extended with the branch's
  priority: a session that has COMPACTED shows its compaction count where a
  session with no compaction shows the clock — a strict extension, since the
  fall-through reproduces master's behaviour exactly.
- **The context sub-cell shows the ratio** (percentage + bar), not the used/max
  pair: the pair is 15 cells with its bar and does not fit a sub-cell of a
  100-cell row. The pair survives as the field's no-window form and as the
  UNALIGNED composition's text.
- **`LayoutCols` keeps master's meaning** (the width the row is composed FOR), and
  the form is chosen against it — the branch's `statusFormWidth` value was
  dropped, since reporting the form's own width would make the ladder
  unobservable at narrow widths.
- **Merge artifacts removed**: master's side contributed its whole ladder/fields
  region a second time at the conflict (keeping its one unique symbol,
  `statusFieldCells`), and the earlier merge had left the `availCols` struct field
  declared twice.

Adapted for the grouped row (each noted in place): master's ladder table and its
`composeLadderRow` ctx marker, which is now DERIVED from the production formatter
rather than the retired column's text.

**Defect found and fixed by this rework's own probe**: an unmeasured sub-cell in
the MIDDLE of the group (the cache read-out is the one a session reaches last)
spent a seam, so the row drew `6% ░░░░ · ·~1.23元` — two separators with nothing
between them. `statusMetricsField` now joins only the read-outs it can show
(the left anchor's rule applied to a hole), with a regression case in
`TestStatusMetricsFieldsAreHonest`.

Measured row at the pin (real composition, `composeLadderRow`):

```
deepseek-flash ·  iter 1  ·        ·  ⚙ keyword_search ·  6% ░░░░ ·~1.23元             4m00s · ready
```

and at 95 / 80 / 70 the group is shed as one step while the tool read-out stays,
then tool goes at 68 — the 3-step ladder above.
