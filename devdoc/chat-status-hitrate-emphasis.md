<!-- doc-class: historical -->
# The chat status row's metrics sub-cell: the prompt cache's hit rate

> Landing record of task `01a0e8c7-eaf3-7c7d-a4d1-4eb0bdb174bd` (branch
> `task/01a0e8c807ee7b579a0ee5241b5e0f95`, re-implemented on a fresh branch off
> master `cb89fff6`). The RECON/design half of the same work landed separately at
> `devdoc/chat-status-cache-hitrate.md` and is untouched by this record: this file
> is the report of what was implemented, that one is the design it was built from
> (its §9 is the geometry correction that made the change possible).

**Task**: `01a0e8c7-eaf3-7c7d-a4d1-4eb0bdb174bd`
**Files**: `cmd/agent/chat_stream.go` (the composer), `cmd/agent/cli_chat.go` (the
legend), `cmd/agent/chat_status_*.go` tests.

## What changed, and what it replaced

The right-hand group of the row under the `gopi chat` prompt used to lead with the
CONVERSATION's context pair:

```
… ·  ctx 1k/1M≡ ·↑2k↓558          24s · ready
```

The pair measured gopi's own TRIM BUDGET against itself, and on a 1M-window model
it printed `1k/1M` — three cells of qualifier (`ctx`, `≡`) whose denominator
carried no information about the model, and a numerator that says only how big the
prompt is. It is replaced by the PROMPT CACHE's hit rate, which is a fact about the
transport rather than a constant:

```
… ·  hit 76% ·↑2k↓618             24s · ready
```

`cacheRead`/`cacheWrite` were already in `statusOps` (`internal/deepseek` fills
them from the provider's own usage report), so the change is text production only.

## The three decisions, and why

**1. The absolute pair is NOT repeated in the sub-cell.** The obvious form was
`hit 68% 2k/1k`, and it was measured on a real screen before it was dropped: the
same two counters are already drawn two cells to the right in the SAME group (the
cache sub-cell `↑<read>↓<write>`), so the row read

```
hit 74% 12k/4k ·↑12k↓4k
```

— the same counts twice, two cells apart. That is the "zero information, spends
cells" defect the operator filed against the money column (task `01a0dcd1`), not a
second measurement. The percentage is the part this sub-cell ADDS (the ratio no
other cell states); the pair beside it is what the percentage is OF, so a reader
who wants the breakdown already has it. The epic delegated this choice to the
implementation by on-screen readability.

**2. Unmeasured is not zero (Rule 15b).** Before the cache has answered
(`cacheRead == cacheWrite == 0`, i.e. a session whose first prompt was entirely
new) the composer returns `""` and the sub-cell FALLS BACK to the context pair —
which IS measured at that state. A `0%` there would be the row claiming a
measurement of a cache nobody exercised, and no session's first prompt is evidence
of a cache that always misses.

**3. The ratio is taken in float64, and that is an overflow decision.** Both
counters are `int`s a session accumulates; `read*100` wraps for magnitudes this
field must still answer for (the first draft returned `-2%`, then `""`, for
`read=200000`). In float64 the sum cannot wrap, and the printed value is a whole
percent, so the representation error is far below the printed digit.

## The geometry did not move — and that is the whole shape of the change

The sub-cell's reservation (`statusCtxCol`, 17 cells) is untouched: the hit-rate
form is a RE-COMPOSITION inside it, so `statusLayoutBudgetCols` (100),
`statusPinSlackCells == 0` and the compile-time equality guard are all unchanged,
and no geometry constant was added or edited. The composer is built from named
constants (`statusHitRateLabel`, `statusHitRatePctSep`) whose cells are declared
and MEASURED against the strings by the arithmetic test (the T5 lesson: a
reservation sampled from a sample string is not a reservation).

The composer takes its cell budget as a PARAMETER (`statusCacheHitRateField(ops,
cells)`) rather than reading the constant, so the yield branches are arithmetic a
test can drive at a narrower geometry instead of prose about a shape it cannot
reach (Rule 31: the surface under test must be the one that runs).

## Evidence

Real pty, 100x26, `gopi chat` driven through the reserved layout, after each turn:

| turn | row 025 |
|---|---|
| before (`ctx` pair) | `… ·  ctx 1k/1M≡ ·↑2k↓558          24s · ready` |
| 1 | `… ·  hit 67% ·↑896↓426            3s · ready` |
| 2 | `… ·  hit 77% ·↑2k↓606             24s · ready` |

The metrics column's cell range is `{col: 57, cells: 30}` in every one of those
dumps — identical to the pre-change baseline — so the change moved the TEXT and
not the GEOMETRY.

At 88 columns both the pre-change and post-change binaries shed the metrics group
entirely (`… ·  tools off      3s · ready`), so the degradation point did not move
either.

Dumps: `~/.gopi/sessions/01a0e8c7-eaf3-7c7d-a4d1-4eb0bdb174bd/{before,after,turns.dump.turn{1,2},narrow88*.dump}.json`.

## Two tests are red on this branch AND on master

`TestChatStatus_StaysOneScreenRowWithALongToolSummary` and
`TestUIStateAudit_RowDisplayingTheWholeLineIsNeverPaintLoss` fail identically on
`master@d1209fe0` (measured in a detached worktree at that rev), so they are not
this change's and are not touched here.
