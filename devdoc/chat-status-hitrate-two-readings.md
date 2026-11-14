<!-- doc-class: historical -->
# The status row's hit rate: two readings, two denominators


Task: `01a10f4e73607b20acc722a2b820fae3` (epic: 累计口径 + 本轮差分读数).

## What the row draws now

```
ctx 92k/1M≡ ·↑12k↓80kΣ13.4%Δ13%
```

Two readings, side by side, told apart by their markers, each carrying the
denominator it is a percentage OF:

| reading | marker | denominator |
|---|---|---|
| session-cumulative | `Σ` | **every prompt token THIS SESSION has sent**: `served/(served+written)`, over the session's own counters — the very pair drawn to its left |
| this turn | `Δ` | **this turn's prompt tokens**: the same formula over the **DIFFERENCE** of those counters since the turn's request was issued |

Both denominators are stated in `gopi chat -h`, THE STATUS ROW
(`chatStatusRowLegend`, `cmd/agent/cli_chat.go`) — the reader meets `Σ`/`Δ` on
the screen and looks them up in the same section that defines `ctx`, `↑`, `↓`.

## The geometry did not move

`statusPinCols == statusLayoutBudgetCols == 100`, `statusPinSlackCells == 0` —
unchanged. The second reading is funded by the **word that used to name the
first one**: `hit` + its gap were 4 cells, and `Δ` + a three-digit whole percent
are exactly 4 (`statusHitRateDeltaDigitsCells = 3`, derived from the existing
`statusPctMax` clamp, never sampled from a render). The measured pair:

```
statusHitRateCumulativeGlyphCells = 1     ("Σ")
statusHitRateDeltaDigitsCells     = 3     ("100" — the clamp's own ceiling)
```

which lands the two-reading form on EXACTLY the 10 cells
`statusCacheHitRateReadingCells` already reserved. The only seam that could move
— `statusMetricsSep` (2 cells) — did not.

### Why `Δ` prints whole percent and `Σ` keeps its fraction

Not a style choice: `Δ`'s percentage at the session's resolution would cost
`1 + 6 + 1 + 6 = 14` cells against the 10 the row has. And it is the honest
resolution — a turn is ONE fresh sample of a few thousand tokens, where a tenth
of a percent is noise, while `Σ` is an average over everything.

## The ladder (what a narrow row sheds)

`statusCacheHitRateReadings(ops, cells)` tries its forms widest-first, each
dropped **WHOLE** — never cut into digits, because a cut number is the one thing
this row must not print:

```
"Σ98.8%Δ10"   both readings — the pinned form
"Σ98.8%"      the cumulative reading alone (no turn baseline, or a narrow row)
""            neither fits — the caller still has the pair
```

A row showing `Σ` alone is therefore a **narrow row**, not a session without a
turn. The prefix order is what makes that true: `Σ…` is a prefix of both, so a
geometry that hosts the pair form always hosts the reading alone.

## Where the turn's baseline comes from

`statusOps` carries three new fields (`cmd/agent/chat_stream.go`):

```
cacheReadAtTurn, cacheWriteAtTurn   the counters when the turn's request was issued
cacheTurnKnown                      the honesty flag (false until a snapshot exists)
```

`statusSourceOps` gained `cacheTurnRead` / `cacheTurnWrite` / `cacheTurnKnown`,
and the baseline is taken in `beginTurn` → `snapshotTurnCountersLocked`. The two
places that adopt live counters (`refreshOpsLocked`, `setOpsFn`) merge through
**one** function, so there is exactly one place a snapshot can be taken — Rule 14
in miniature.

`statusCacheTurnHitRate` **diffs the counters BEFORE taking the ratio**. The
alternative (difference of two session-level ratios) answers a different
question — how far the session average moved — and is diluted by every earlier
turn; the diff-first form is a turn-level measurement.

Empty is the honest answer in three states, all meaning the same thing to a
reader: no baseline yet, no traffic this turn, or nothing changed since the
snapshot. A negative is clamped, not reported — the counters are monotonic on
this transport, so a negative would mean the snapshot belongs to a different run,
and a row printing it would state a number the operator cannot act on.

## Two spans, two tiers — `internal/lineedit`

`StatusColumn` gained `EmphasisSpans []StatusEmphasisSpan`. The **number of
measurements a field carries is the producer's fact**, and a struct with two
span fields would need widening again for a third — while the walk is the same
either way (find the span in the remaining text, wrap it, re-state the enclosing
field). `emphasisSpans()` resolves the field's effective list, so the single-span
fields are the one-span spelling of it and the two spellings cannot draw
differently.

Each reading is graded by the **same** floors (`statusHitRateTierStyle` — the one
place the two floors are compared), so the two readings cannot grade the same
number differently. The question the hue answers — "is this rate healthy?" — does
not change with the window the rate is taken over; the readings are told apart by
their **markers**, never by their colour, which is what keeps the distinction
legible on a monochrome terminal.

Each span is checked with `strings.Contains` against the column's OWN text before
it is handed over: the composer drops readings whole, so a span the column does
not carry must wear nothing. Emphasising a span that is not in the text is
exactly the silent non-effect this channel exists to remove (Rule 15).

## The artifact leg: the row as `gopi ui_state` reads it

The row is drawn for two readers, and the epic's acceptance names both. The
terminal's reader sees the **stream**; the agent's reader sees the **artifact** —
`chat-screen.dump`, read back through `uiclient.ReadDump` as an interpreted CELL
GRID with a per-row `attr_runs` list, which is exactly what
`gopi ui_state --target chat` returns. `TestChatPTY_TwoReadingsCarryTheirOwnColourInTheDump`
is that second reader's leg: on a real pty, from the shipped binary, it polls the
surface's OWN dump until both readings are in the grid, then resolves the reading's
run through `ptyCtxRunCovering` and compares its style against the **tier the
palette entry yields when run back through the product's own screen parser**
(`ptyCtxStyleName`) — the floors coming from a read of
`chat_status_hitrate_emphasis.go`'s declarations. Nothing about the colour is
typed in the test, so a palette entry that moves, or a floor that moves, moves the
expectation with it (Rule 14).

A dump-based leg is not decoration on top of the stream-based one: the two fail
differently. A sequence that never reached the grid, and a run a reader cannot
attribute to a reading, are both invisible to a `strings.Contains` on the stream.

## One convention, two halves (Rule 14)

The composer spells the readings once (`statusCacheHitRateReadings`) and the
emphasis channel reads the **same constants back**
(`statusHitRateCumulativeSpan` / `statusHitRateDeltaSpan`) — a second spelling on
the emphasis side would let the coloured span and the drawn text be different
strings. `statusHitRateReading` — the parser that grades them — now strips the
marker the same way it strips the suffix, because the marker is part of the form
the row draws; an unprefixed percentage (the pre-epic labelled path) still parses
unchanged.

## Guard-test premises that had to move (not silently)

These were pinned to the pre-epic shape and each was **changed as a premise**,
never weakened:

| test | old premise | new premise |
|---|---|---|
| `TestStatusHitRateEmphasis_SpansTheMeasuredText` | span == bare `statusCacheHitRate` | span == `statusHitRateCumulativeSpan` (a composer, still) |
| `TestStatusHitRateIsSilentWhenNothingWasMeasured` | `" hit 74.5%"` | the composer's own span |
| `TestStatusRowKeepsBothThePairAndTheRate` | `statusHitRateLabel + " " + rate` | `statusHitRateCumulativeSpan` |
| `TestStatusHitRateFormYieldsTheNameOnlyWhenItMust` | `"Σ 74.5%"` (gap) | `"Σ74.5%"` (the marker is 1 cell — the gap was a cell the Δ reading needs) |
| `TestStatusRowLegendEntryOrderMatchesTheRow` | legend entry opens with `hit` | opens with `Σ` |
| `TestChatPTY_ContextDenominatorIsQualifiedOnScreen` (testsuite) | rate is `hit <pct>` | two readings, `Σ<pct>` then `Δ<whole pct>`, adjacent and in that order |

## Verification

```
Build:  CGO_ENABLED=0 go build ./...                      → OK
Tests:  go test ./cmd/agent/ -run 'TestStatusHitRate|TestStatusRow…'  → green
        go test ./testsuite/ -run 'TestChatPTY_ContextDenominatorIsQualifiedOnScreen|
            TestChatPTY_TwoReadingsDivergeOnScreen|
            TestChatPTY_TwoReadingsCarryTheirOwnColourInTheDump'  → 3/3 PASS
        go test ./testsuite/ -run 'TestChatEmphasisColourAlignment_OnRealPTY' -count=1
                                                             → branch ok 59.127s ·
                                                               master ok 60.896s (isolated)
        go test ./internal/standards/ -run 'TestCommandsDocGate|TestCensusGate|
            TestRepoHygiene' -count=1                        → ok, 5.726s
Ran:    ./bin/gopi chat -h        → THE STATUS ROW now defines Σ74.5%/Δ10%
Evidence (testsuite pty, shipped binary, scripted provider):
  leg 1 — ONE turn, the settled STREAM, readings adjacent and ordered:
          "ctx 92k/1M≡ ·↑12k↓80kΣ13.4%Δ13%"
  leg 2 — TWO turns, DIFFERENT cache traffic, the settled STREAM:
          "Σ53.0%" and "Δ98%" simultaneously on the row, cumulative first,
          each inside its own SGR run (a row repeating the cumulative value
          under Δ fails this case by name)
  leg 3 — the SAME row read back as the ARTIFACT `gopi ui_state --target chat`
          reads (uiclient.ReadDump → the interpreted cell grid + attr_runs),
          palette pinned to palette256 / TERM=xterm-256color:
          row 29 = "Σ13.4%" at col 73, "Δ13%" at col 83, the cells of BOTH
          inside the run cols 73..82 = fg=colour-210 bold — i.e. the tier's
          own colour (StatusFair in this table), resolved from the palette ENTRY
          and the floors READ FROM THE SOURCE, never typed here
Not Done: leg 3's row is the single-turn state, where both readings share the
          fair tier and the painter therefore emits ONE run over both. The
          two-run shape is NOT asserted by any pty case; what leg 2 asserts at
          that state is two SGR runs on the stream. See "Not Done" below.
```

## Not Done (honest)

- **The two-run shape is not witnessed on a pty.** The dump leg reads the single-turn
  state, where both readings land in the SAME tier (small sample, 13.4% and 13% are
  both "fair"), and the painter quite properly emits ONE continuous run over both. A
  case asserting a run boundary between them at that state would be asserting a
  boundary the tier model does not draw. What the pty legs DO witness is: both
  readings on the row, at different columns, ordered cumulative-first (leg 2 on the
  stream, leg 3 on the artifact), and the artifact colouring their cells with the
  tier's own hue. **A second scripted turn in the DUMP leg** — the one state where the
  readings would occupy different tiers and a boundary could be measured — was not
  built: the dump leg would need the two-turn script wired to it, and this run's budget
  was exhausted. The unit-level emphasis tests
  (`TestStatusHitRateEmphasis_SpansTheMeasuredText`) do cover a boundary between two
  spans in one field, so the two-run shape is not unmeasured — it is unmeasured ON A
  PTY.
- **The `./testsuite` pty family is load-sensitive and fails on master too.** This
  worktree's `make test-budget PKGS='./testsuite'` reported 8 failures
  (`TestChatEmphasisColourAlignment_OnRealPTY/delta=1`, `TestChatInfo_OnRealPTY_…`,
  `TestChatLayerFormat_OnRealPTY`, `TestChatNoticeTurn_*`, `TestChatPTY_LongPaste…`,
  `TestChatPTY_CtrlCDuringATurn…`, `TestTask_BarePrompt_SIGTERM_…`) while all four
  legs this epic changed passed. **Measured on `/home/user/work/GoPi` at master
  `86cdb3162`, same tip, none of my diff present**: `TestChatLayerFormat_OnRealPTY`
  FAIL, and `TestChatInfo_OnRealPTY_…` / `TestChatNoticeTurn_WakesAParkedConsole` /
  `TestChatPTY_LongPasteIsSpilled…` FAIL — the same names. They are timing-coupled
  pty cases under a load average of 3–9 on a 4-core box.
- **One of the eight was checked twice, because it could have been this branch's.**
  `TestChatEmphasisColourAlignment_OnRealPTY` failed in the budget run but not on
  master, and the file it lives in is one this epic edited — so isolation was run
  BOTH sides: branch `ok 59.127s`, master `ok 60.896s`, each with the case alone.
  Two greens in isolation after a red under load is the load verdict; the failure is
  not attributable to this diff. It is recorded rather than dismissed because the
  coincidence of file and failure is exactly what a regression looks like.
- The pre-existing red family in `./cmd/agent` (`TestChatStatus_LiveBlockKeepsTheStateWord`,
  the `TestG4…`/`TestG5…` help-inventory family, `TestAnnotateVerb…`,
  `TestSandboxDirFlagDecidesTheRoot`, `TestToolGroup…`) is **not** this epic's:
  it is the already-attributed baseline recorded in
  `devdoc/verb-retirement-complete.md` and `devdoc/memory-prefetch-default-on.md`.
  It fails identically on the base commit of this branch, which is the check that
  distinguishes it from a regression introduced here.
