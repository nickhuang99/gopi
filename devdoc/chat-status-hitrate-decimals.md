<!-- doc-class: historical -->
# The chat status row's hit rate carries one decimal place (task 01a0f658)

**Task**: `01a0f658-ad87-78ed-9119-73dbb3f03e07`
**Files**: `cmd/agent/chat_stream.go` (the ONE composer), `cmd/agent/chat_status_hitrate_emphasis.go`
(the tier's reader), `cmd/agent/cli_chat.go` (the legend), `cmd/agent/chat_status_hitrate{,_emphasis}_test.go`.

## What changed

The prompt-cache hit rate printed at WHOLE-PERCENT resolution ("hit 98%"). It now
prints `statusHitRateDecimals` (1) fraction digits:

```
deepseek-flash ·  iter 2  ·  tools off   ·  hit 75.0% ·↑2k↓726    24s · ready
```

Only this one field. No geometry constant moved: `statusCtxCol` (17),
`statusMetricsCol` (27), `statusLayoutBudgetCols`, `statusPinSlackCells == 0` and the
compile-time equality guard are untouched, and the widest labelled form the composer
can produce — `"hit 100.0%"` — is 9 cells of the 17 the sub-cell reserves.

## The two halves of ONE format (Rule 14)

The reason this is not "a `%.1f` in the composer plus a `strconv.Atoi` left where it
was": the tier that colours the reading is decided by PARSING that reading back. A
composer writing `98.8%` and a parser reading whole numbers is exactly the
two-conventions defect — the parse fails, `statusHitRateEmphasis` returns the
no-tier state, and the emphasis channel goes silently inert while the row still
prints (Rule 15).

So the format lives in one place, `cmd/agent/chat_stream.go`, as three named terms:

| term | role |
|---|---|
| `statusHitRateDecimals` | the fraction-digit COUNT — the epic's actual request |
| `statusHitRatePctSuffix` | `"%"`, appended by the writer and trimmed by the reader |
| `statusHitRatePctPointCells` | the `.` the formatter emits, so the geometry's widest-width statement is derived rather than typed |

and two functions that are each other's inverse:

| function | half |
|---|---|
| `statusHitRatePct(ratio float64) string` | WRITER — scale to percent, clamp to `statusPctMax`, format at `statusHitRateDecimals`, append the suffix |
| `statusHitRateReading(reading string) (float64, bool)` | READER — trim the suffix (the one constant), `strconv.ParseFloat` |

`statusCacheHitRate` is the counters→reading composer (unchanged contract: `""` when
`cacheRead <= 0 && cacheWrite <= 0`, Rule 15b) and it now routes through
`statusHitRatePct`. `statusHitRateEmphasis` calls `statusHitRateReading` instead of
`strconv.Atoi`, so it judges the number the reader sees — to the printed digit.

The ratio stays `float64` (`read / total`, then `* 100`), which is now both an
overflow decision and a precision one: `read*100` wraps the int type for sessions
this field must still answer for, and an integer expression could not state the
fraction at all.

`statusHitRatePctCellsWidest` (`statusCtxPctCellsWidest + statusHitRatePctPointCells
+ statusHitRateDecimals` = 6) is a MEASUREMENT of the composer's widest output, not a
term in any reservation — the read-out is a re-composition inside `statusCtxCol`.

## The tiers are unchanged, and the boundary moved by a tenth

The floors (`statusHitRateGoodFloorPct` 80 / `statusHitRateFairFloorPct` 50) are the
producer's judgement and stay whole percents; the reading they are compared against
now carries a fraction, so `79.9%` is `fair` and `80.0%` is `good`. The exhaustive
range test walks every tenth of a percent the composer can print (1001 readings), not
every whole percent.

## Verification (the numbers, and how they were taken)

The 98.75 boundary is `395/400` — pinned as the STRING the formatter produces, so a
formatter and a truncation cannot silently disagree:

```
read=395 write=5   → "98.8%"     (exact ratio 0.9875, one decimal, half-up at the printed digit)
read=111104 write=1369 → "98.8%" (the operator's own session: 111104/112473 prompt tokens)
read=12300 write=4200  → "74.5%"
```

Real pty dump (private sandbox profile, `gopi chat` with the reserved layout
installed, 117x26) — the row at two consecutive turns, copied between turns:

```
turn 1: deepseek-flash ·  iter 1  ·  tools off   ·  hit 62.2% ·↑896↓544      2s · ready
turn 2: deepseek-flash ·  iter 2  ·  tools off   ·  hit 75.0% ·↑2k↓726     24s · ready
```

`62.2%` is `896/1440` and `75.0%` is `2176/2902` — the cumulative counters the
sandbox's own session record carries, and the rate MOVES with the conversation: the
fraction is what lets it move in steps smaller than a percent.

The same two dumps carry the geometry claim as a MEASUREMENT rather than an
argument, and the measurement is the metrics column's hit-rate span — the run of
cells the emphasis colours — at the SAME column and the SAME width at both turns:

```
turn 1: attr run col=58 cells=9  fg=rgb:ffa726 bold   'hit 62.2%'
turn 2: attr run col=58 cells=9  fg=rgb:ffa726 bold   'hit 75.0%'
```

Nine cells at both turns, which is exactly `statusHitRateLabel` (3) +
`statusHitRatePctSep` (1) + the rate (5): the label did NOT have to yield for the
fraction, and the column the grid drew that span at did not move. (The trailing
`·↑2k↓726` run does shift its padding by one cell as the counter itself gains a
digit — that is the cache sub-cell's own bounded formatter, untouched here.)

At 88 columns the whole metrics group is shed, as it was before this change: the
ladder decides on the RESERVED column widths (`statusMetricsCol`, fixed at 27), not
on the rendered text, so content of any length cannot move the shed point —
`chat_status_ladder_test.go` pins those points and passes unchanged.

## Not done

- The 88-column run sheds the whole group, so it exercises the ladder rather than the
  field's own narrow-cell yield; the yield branch is covered by
  `TestStatusHitRateFormYieldsTheNameOnlyWhenItMust`, which drives
  `statusCacheHitRateField(ops, cells)` one cell at a time.
- The `docs/archive/demo/` showcase material (frames shot by `docs/archive/demo/replay/shoot-frames.py`)
  still shows `hit 91%`: those are captured IMAGES of an older binary, recording what
  that run displayed. They are evidence, not assertions, and re-shooting them is its
  own task.
