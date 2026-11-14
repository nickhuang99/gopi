<!-- doc-class: historical -->

## Salvage measurement — what of this branch survived (task `01a0e95d`)

This branch (`task/01a0e9226b6b73cf993c5b8ed13bddd9`) was deleted by the merge
lane after `gopi merge` judged it a SEMANTIC DUPLICATE: master re-landed the same
feature independently at `ecfb41f3` (same subject, **different patch-id** —
`a305b579…` for this branch's diff vs `35d520f8…` for master's commit, so the two
are independent implementations, not a rebase). The rework task `01a0e95d`
measured, against master `590d5f33`, exactly what of the branch's delta still
stands. The answer, and the method:

**Method.** The branch ref is gone, so its tip was reconstructed from the archived
diff: `git archive <merge-base cb89fff6> | tar -x` into a scratch root, then the
diff applied with `patch -p1`. The reconstruction is PROVEN, not assumed — every
one of the 11 files' post-image blob hash in the diff's `index …..<sha>` header
matches `git hash-object` on the reconstructed file (**11/11, 0 mismatches**).

**Result — the CODE delta is fully landed; this document is the only survivor.**

| branch-added code/test file | significant added lines | still absent from master `590d5f33` |
|---|---|---|
| `cmd/agent/chat_stream.go` | 50 | 0 |
| `cmd/agent/chat_status_hitrate_test.go` | 146 | 0 |
| `cmd/agent/chat_status_ctx_label_test.go` | 37 | 0 |
| `cmd/agent/chat_status_metrics_test.go` | 36 | 0 |
| `cmd/agent/chat_status_row_unit_rule_test.go` | 11 | 0 |
| `cmd/agent/chat_status_ladder_test.go` | 8 | 0 |
| `cmd/agent/chat_status_ctx_provenance_test.go` | 8 | 0 |
| `cmd/agent/chat_status_degradation_test.go` | 4 | 0 |
| `cmd/agent/cli_chat.go` | 22 | 0 (the 6 not byte-identical are prose drift: "a few cells" → "two cells", "while" → "when") |

A second, independent check: every symbol the branch's diff DEFINES
(`statusCacheHitRateField`, `statusHitRateLabel`, `statusHitRateLabelCells`,
`statusHitRateLabelCellsPinned`, `statusPctMax`, and the five test functions) is
declared **exactly once** on master — **0 of the branch's defined symbols are
missing**, and `cmd/agent/chat_stream.go` carries ONE
`func statusCacheHitRateField` (line 2355), ONE `func statusCtxField` (2167) —
there is no duplicate implementation.

Uniquely absent from master: **`devdoc/chat-status-cache-hitrate-implementation.md`**
(only file under `Only in tree-branch`). It is restored on this branch as the
record of task `01a0e922-…`, whose branch no longer exists in any ref.

**Two claims below were re-verified against master `590d5f33`, and both hold:**
the over-wide-pair arm is present (`cmd/agent/chat_stream.go:2208`, "THE PAIR MAY
NOT YIELD WHERE THE HIT-RATE FORM CANNOT REPLACE IT … `if statusCacheHitRate(ops)
== "" { return "" }`"), and the geometry constants are unmoved
(`statusCtxCol = statusCtxLabelCells + statusCtxBarGapCells + statusCtxPairCells`,
`statusLayoutBudgetCols = 100`, `statusPinSlackCells = … − …`). One difference
from master's own record of the same feature: master's
`devdoc/chat-status-hitrate-emphasis.md` does not cite this document, so it is
restored here as an independent record rather than as a cited authority.

**Method note for the next reader**: `git apply` was NOT used on master (the base
moved; a stale diff reapplies the conflicts it was archived for) — the diff was
read as a SPEC, and the surviving delta was decided by measurement, not by
replaying it.

# The chat status row's metrics sub-cell: the prompt cache's hit rate

**Task**: `01a0e922-6b6b-73cf-993c-5b8ed13bddd9` — a REWORK of
`01a0e8c7-eaf3-7c7d-a4d1-4eb0bdb174bd`, whose branch ref was deleted by the
merge lane (its diff was archived as an attachment and used as a SPEC, never
applied). This document is the implementation counterpart of the reconnaissance
note `devdoc/chat-status-cache-hitrate.md`, which is master's own record of the
scouting round and is left untouched.

**Files**: `cmd/agent/chat_stream.go` (the composer), `cmd/agent/cli_chat.go`
(the legend), `cmd/agent/chat_status_*.go` (the pinned tests).

## What changed, and what it replaced

The right-hand group of the row under the `gopi chat` prompt led with the
CONVERSATION's context pair:

```
… ·  ctx 1k/1M≡ ·↑2k↓558          24s · ready
```

The pair measured gopi's own TRIM BUDGET against itself, and on a 1M-window model
it printed `1k/1M` — three cells of qualifier (`ctx`, `≡`) whose denominator
carried no information about the model, and a numerator that says only how big the
prompt is. It is replaced by the PROMPT CACHE's hit rate, a fact about the
transport rather than a constant:

```
… ·  hit 79% ·↑2k↓534             24s · ready
```

`cacheRead`/`cacheWrite` were already in `statusOps` (`internal/deepseek` fills
them from the provider's own usage report, `cmd/agent/cli_chat.go` copies the
cost tracker's totals in), so the change is text production only — no new data
path, no new dependency.

## The three decisions, and why

**1. The absolute pair is NOT repeated in the sub-cell.** The obvious form was
`hit 68% 2k/1k`, and it was measured on a real screen before it was dropped: the
same two counters are already drawn a few cells to the right in the SAME group
(the cache sub-cell `↑<read>↓<write>`), so the row would read

```
hit 74% 12k/4k ·↑12k↓4k
```

— the same counts twice, a few cells apart. That is the "zero information, spends
cells" defect the operator filed against the money column (task `01a0dcd1`), not a
second measurement. The percentage is the part this sub-cell ADDS (the ratio no
other cell states); the pair beside it is what the percentage is OF, so a reader
who wants the breakdown already has it. The scouting note delegated this choice to
the implementation by on-screen readability.

**2. Unmeasured is not zero (Rule 15b).** Before the cache has answered
(`cacheRead <= 0 && cacheWrite <= 0`, i.e. a session whose first prompt was
entirely new) the composer returns `""` and the sub-cell FALLS BACK to the context
pair — which IS measured at that state. A `0%` there would be the row claiming a
measurement of a cache nobody exercised, and no session's first prompt is evidence
of a cache that always misses.

**3. The ratio is taken in float64, and that is an overflow decision.** Both
counters are `int`s a session accumulates; `read*100` wraps for magnitudes this
field must still answer for (the archived draft returned `-2%`, then `""`, for
`read=200000`). In float64 the sum cannot wrap, and the printed value is a whole
percent, so the representation error is far below the printed digit.
`TestStatusHitRateIsArithmeticOverTheCounters` pins the rungs, and
`TestStatusHitRateFormIsBoundedByConstruction` walks `math.MaxInt` on both sides.

## The geometry did not move — and that is the whole shape of the change

The sub-cell's reservation (`statusCtxCol`, 17 cells) is untouched: the hit-rate
form is a RE-COMPOSITION inside it, so `statusLayoutBudgetCols` (100),
`statusPinSlackCells == 0` and the compile-time equality guard are all unchanged,
and no geometry constant was added or edited. `statusCtxField` composes
`statusCacheHitRateField(ops, statusCtxCol)` first and falls through to the
context pair only when that answers empty.

The composer is built from named constants (`statusHitRateLabel`,
`statusHitRatePctSep`, `statusHitRateLabelCells`/`…Pinned`, `statusPctMax` — the
ONE ceiling both of the row's percentages clamp to, Rule 14) whose cells are
declared and MEASURED against the strings by the arithmetic tests (the T5 lesson:
a reservation sampled from a sample string is not a reservation).

The composer takes its cell budget as a PARAMETER (`statusCacheHitRateField(ops,
cells)`) rather than reading the constant, so the yield branches are arithmetic a
test can drive at a narrower geometry instead of prose about a shape it cannot
reach (Rule 31: the surface under test must be the one that runs).

One branch is new beside the archive's spec: `statusCtxField`'s over-wide-pair
arm (a 1M window, `999.9k/1000.0k`, 14 cells against a 17-cell sub-cell that must
also hold the label and the qualifier) now returns NOTHING when the cache is
unmeasured, rather than yielding the label and printing the pair. The archive's
draft printed the pair unlabelled there, which would put a bare pair beside a bare
cache pair — the ambiguity the label was added to close — in exactly the state
where the fallback is the sub-cell's only read-out.

## Evidence

Real pty, 100x26, `gopi chat` driven through the reserved layout, two turns
(`ptychat.py`), the binary built from this branch:

| row 025 | metrics column |
|---|---|
| `deepseek-flash ·  iter 2  ·  tools off                 ·  hit 79% ·↑2k↓534             24s · ready` | `{col: 57, cells: 30}` |
| a fresh session before any usage report: `… ·  no metrics                   0.1s · ready` | `{col: 57, cells: 30}` |

The metrics column's cell range is `{col: 57, cells: 30}` in BOTH — identical to
the pre-change baseline recorded in the scouting note (same `col`, same `cells`)
— so the change moved the TEXT and not the GEOMETRY. The frames also carry the
row at two different rates (`hit 69%` → `hit 79%`), which is the property that
makes the read-out worth the cells: a constant would not move.

Dumps: `~/.gopi/sessions/01a0e922-6b6b-73cf-993c-5b8ed13bddd9/ev/{after,fallback}.dump.json`.

## The scoped suites' reds are PRE-EXISTING — measured against master, not asserted

The epic's acceptance names `go test ./cmd/agent/ -run 'Status|Chat'` and
`go test ./internal/lineedit/ ./internal/standards/`. Neither is fully green on
this branch, and **every red reproduces identically on unmodified
`master@cb89fff6`** — measured in a detached probe worktree at that rev, the same
suite, the same run shape:

| suite | failing tests on this branch | failing tests on master | same failure line |
|---|---|---|---|
| `./cmd/agent/ -run 'Status\|Chat'` | `TestChatDumpHelp_TheNamedAuditCommandIsTheAutomaticOne`, `TestChatDump_GridFollowsTheLayoutGeometryOnARealPty`, `TestChatStream_RepaintsStayLinearInTheAnswer`, `TestChatTurnHeader_PaintsAMultiRowCountedHeaderOnARealPty` | the same four | yes — e.g. `chat_dump_geometry_pty_test.go:132: the band refused the row: no reserved layout is active on the editor`, `chat_stream_test.go:360: committed lines = 39 for 40 finalized lines, want exactly 40 (one commit each)` |
| `./cmd/agent/` (two more, run separately) | `TestChatStatus_StaysOneScreenRowWithALongToolSummary`, `TestUIStateAudit_RowDisplayingTheWholeLineIsNeverPaintLoss` | the same two | yes (`0.00s` / `13.08s`) |
| `./internal/lineedit/` | `TestEmphasisSpanAfterAWrapSpaceKeepsItsOwnCells`, `TestEmphasisWrapSpaceLedgerHoldsAtEveryWrapCount`, `TestScreenReconcilesAcrossAReMeasure`, `TestScreenReconcilesAfterAnOverlongHold`, `TestTurnHeaderLines_BlockRowsAreCommittedOnePerRow` | the same five | yes |

The **status-row family alone** is green on this branch:
`go test ./cmd/agent/ -run 'Status' -count=1` → `ok github.com/nick/gopi/cmd/agent 5.226s`,
and `go test ./internal/standards/ -count=1` → `ok … 48.934s`. The reds live in
pty/repaint geometry tests this change does not touch (it composes a string inside
an existing cell reservation), and none of them names the status row's metrics
sub-cell.

**The probe worktree is REMOVED** (`git worktree remove --force` → exit 0, `git
worktree list` no longer lists it): a detached checkout at the same rev left in
place would be litter the sweep would have to collect.
