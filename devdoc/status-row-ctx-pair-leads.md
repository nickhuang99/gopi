<!-- doc-class: historical -->
# Task Report: the chat status row leads with the context pair (re-partition), the hit rate rides the cache sub-cell

## Completed

**The behavioural defect the task names is fixed**: on a session where the cache
counters are measured, the row no longer leads with a hit-rate percentage while the
context pair is missing. The pair now LEADS the metrics group at every state where a
window is measured.

Pinned row, measured (100 cells, `statusColumnsAtCols`):

```
deepseek-flash ·  iter 9  ·  ⚙ keyword_search i… ·  ctx 13k/1M≡ ·↑12k↓4k hit 74.5%     4m00s · ready
```

Before this change the same state painted (master baseline, measured on the running
binary in `testsuite/chat_pty_ctx_qualifier_test.go`):

```
… · hit 13.4% ·↑12k↓80k · …
```

— the rate leading, the pair gone.

- **`statusCtxField`** (`cmd/agent/chat_stream.go`) always returns the context pair
  (label + pair + qualifier); its hit-rate arm is removed. The pair never prints CUT:
  where the widest pair a budget of this size can host does not fit beside the label
  (`999.9k/1000.0k`), the LABEL yields and the pair is shown whole.
- **`statusCacheField`** composes the pair (`statusCachePairExpr`, split out so a
  caller can ask the pair's own width) plus the labelled hit rate riding it, via
  **`statusCacheHitRateAppend`** — the rate's home is the sub-cell whose two counters
  it is computed FROM, so the number and its operands are one read-out.
- **The rate is shed LAST, by construction**: the width ladder drops a WHOLE sub-cell
  at a time, so a rate that is part of the cache value leaves the row exactly when the
  pair leaves it, never one cell sooner.
- **`statusCacheHitRateField`** (the form) keeps its two arms — labelled, and the
  honest empty. Its UNNAMED arm is now REFUSED on the row: a percentage beside a
  two-sided pair is the ambiguity the name exists to close (the "6%" beside
  "13.1k/200.0k" report), so a composition that cannot name the number prints none.
- **geometry re-partition, no cell invented, none given up**: `statusCtxCol` 17→12
  (label 3 + gap 1 + pair 7 + qualifier 1). The pair's per-side pin dropped 6→3,
  which is the retired `formatCount`'s "200.0k" side against the ladder the row
  actually draws (`statusCountSideTextWidest` = 3, "1M"/"92G") — six cells the
  sub-cell held and nothing could draw. `statusCacheHitRateCells` (10) is that
  headroom, spent on the cache sub-cell's own value. `statusToolColWayBreadth` 4→0 and
  `statusToolColOwn` 16→14 (the `kWay` marker rides the tool cell) complete it, so
  `statusPinCols == statusColumnsAtCols == statusLayoutBudgetCols == 100`,
  `statusPinSlackCells = 0`, and the compile-time guard
  (`const _ = uint(statusLayoutBudgetCols - statusColumnsAtCols)`) still asserts
  equality.
- **`statusMetricsFormCol`** is the group's FULL-FORM width (reservations + the
  rate's cells). It is what the group's column is declared as (`statusRowColumns`) —
  a column declared narrower than its own content is what the width ladder reads —
  and what the pin spends. `statusCacheHitRateSpanBudget` is the same number for the
  emphasis channel, so the span the resolver answers is the form the row DREW.

## Not Done

- **The tool column's `statusToolColWayBreadth` is 0**, so a tool name longer than the
  cell is elided with a plain ellipsis and no way-marker. The cells it held paid for
  the rate. This is stated rather than hidden: the alternative was a longer row than
  the terminal, which the compile-time guard refuses.
- The re-partition assumes the row's counters fit the LIVE ladder
  (`statusCountSideTextWidest`). A counter whose unit is not on
  `statusCountUnits` widens the pair past its reservation — that is pre-existing
  (the retire of `formatCount` is why the pin dropped) and `statusCacheField` already
  bounds the cache pair by construction; the context pair is NOT bounded to a ladder
  cell, and a `statusCtxPair` wider than 3 a side still yields its label first. Not
  addressed here.

## Verification

- Build: `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/`: OK
- Tests:  `go test ./cmd/agent/ -run TestStatus -count=1`: PASS (all `TestStatus*`
          guards, including the 8 the re-partition invalidated — each updated with its
          old premise stated and the new one asserted, never silenced)
- Tests:  `go test ./testsuite/ -run TestChatPTY_ContextDenominatorIsQualifiedOnScreen
          -count=1`: **PASS** — the guard that was RED at HEAD.
- Ran:    `go test ./testsuite/ -run TestChatPTY_ContextDenominatorIsQualifiedOnScreen
          -count=1 -v` on the master baseline (before the change):
          FAIL in 71.8s, `pty row paints "· hit 13.4% ·↑12k↓80k" — rate leads, no ctx pair`.
- Ran:    same test on this branch: PASS in 13.4s,
          `pty stream carried "92k/1M≡"`.
- Ran:    `go test ./cmd/agent/ -run TestZZPinProbe` (a throwaway probe, deleted after
          use) printing the composed row: `WIDTH|100|` and the pinned row quoted above.
- Result: the pinned row is exactly 100 cells with the pair leading and the rate in
          the cache sub-cell; the end-to-end PTY guard flipped RED→GREEN.

## Evidence (self-filed)

- `make test-budget PKGS='./cmd/agent ./internal/lineedit'` — see the run stream
  appended below this report by the close-out; the two `go test -run TestStatus` /
  `testsuite` invocations above are the branch-scoped evidence for this diff.
- `git status --porcelain` at close-out: the tracked files of this diff, committed;
  the `devdoc/` report of this run is classified in the same commit (Rule 36).
