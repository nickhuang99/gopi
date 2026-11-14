<!-- doc-class: historical -->
# Task Report: the status row's guards assert BOTH metrics facts and their order (pair leads, rate named)

## Completed

**The defect's own guard was one fact short, and this run closed that.** The pty case
`testsuite/TestChatPTY_ContextDenominatorIsQualifiedOnScreen` asserted the ctx pair
(`92k/1M≡`) and nothing else. The defect the re-partition was opened for is a row that
kept **one** of the two metrics facts — measured on the operator's binary:
`· hit 13.4% ·↑12k↓80k`, the pair **absent**, the rate leading. A guard that asserts only
the pair cannot see its own mirror image: **pair present, rate silently dropped** passed
green. That is what this run fixed, on both the real screen and at unit speed.

**1. The pty case reads the SETTLED stream, and asserts the rate and the order.**
`chat.waitFor` returns the instant its marker arrives — **mid-repaint**. Measured (probe
under the case): the bytes it hands back end exactly at `ctx 92k/1M≡`, and the rate is
written **one repaint later**. So the case now continues reading until the stream is
quiet (`ptyReadSettled`, `ptyCtxSettleQuiet` 700ms / `ptyCtxSettleLimit` 10s, both named)
and asserts on the settled row:

- (4) the named rate is on it — `ptyCtxRateReading()` derives the expected reading from
  **this file's own scripted counters** (read / (read + create + input)), never a typed
  magnitude (a second copy of the product's arithmetic is the stale-copy defect the ctx
  cases were previously repaired for);
- (5) the pair is written **BEFORE** the rate, and the cells between them carry the cache
  transport `↑` — the re-partition's invariant, measured in byte order rather than argued
  from the composers.

**2. `cmd/agent/TestStatusRowKeepsBothThePairAndTheRate` — the millisecond-scale twin.**
The pty run costs ~14s and reaches **one** state; the invariant is a property of the
composition. The new unit gate asserts the same four things on the composed row
(pair present, rate present, pair's cell before the rate's cell, cache transport in the
cells between) **and** the measured coexistence fact: across `statusProbeCacheRead`,
`statusCountCompactKilo` and `math.MaxInt` the pair and the rate are always
present-or-absent **together** (they are one read-out), so the rate can never be dropped
while its own counters are still printed.

**3. One fixture definition instead of two spellings.** The probe row's context occupancy
is now named (`statusRowProbeCtxUsed`/`statusRowProbeCtxMax`, 42100/64000) in
`chat_status_cache_bound_test.go`. It was a bare `42100` in the row's own composition
while the new gate derived its expected pair from a **different** constant
(`statusProbeCtxUsed = 4200`), so the assertion could not match the row it was reading —
the same hand-derivation class the ctx cases were repaired for. One definition, referenced
by both.

**4. The legend states the invariant its gates now prove.** `chatStatusRowLegend`'s rate
entry said the rate is the least permanent fact; it now also states what the rate never
does — leave **alone** — and names both proving gates (`chatStatusRowLegend` is unchanged
in kind: text only, no symbol added; the legend gates stay green).

## Not Done

- **No geometry change, by design.** `cmd/agent/chat_stream.go` is byte-identical to
  master (`git diff master -- cmd/agent/chat_stream.go` prints nothing): the three coupled
  edits the epic names (`statusColumnSepTotalN`, `statusMetricsFormCol`, the rate's funded
  home) were already implemented by task `01a0f8c9` (`9316f3dd`) and merged while this run
  worked. This run's deliverable is the **guard** for that re-partition, not the
  re-partition.
- **`statusLayoutBudgetCols` NOT widened** — verified by diff, not asserted: 100, and
  `statusPinSlackCells` is 0 (`statusPinCols == statusColumnsAtCols == 100`).
- **The 4 top-level separators the epic's clause (1) names are, measured, 3 drawn at
  top level.** A probe of the composed row found `·` at cells 15, 26, 49 and 65 — the
  fourth is the metrics group's **internal** seam (`statusMetricsSep = " ·"`), not a
  top-level boundary. `statusColumnSepTotalN = 3` therefore matches the row (four
  top-level columns), and no edit was made to it; `TestChatStatus_WidthDegradation` and
  `TestChatStatus_FieldsStartOnTheirReservedCells` are **green** on the unchanged
  geometry.
- **Not covered here**: whether `%`-bearing strings elsewhere in `gopi chat -h` (the
  unaligned composition's percentage) need the same treatment — outside this row.

## Verification

- Build: `CGO_ENABLED=0 go build ./cmd/agent/` → **OK**;
  `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → **OK** (the binary is rebuilt before
  every pty run — the pty case SKIPs with "gopi binary not found" otherwise).
- Tests: `go test ./cmd/agent/ -run 'TestChatStatus|TestStatus' -count=1` → **ok** (26.4s).
- Tests: `go test ./cmd/agent/ -run TestStatusRowKeepsBothThePairAndTheRate -count=1 -v`
  → **PASS**.
- Tests: `go test ./cmd/agent/ -run 'TestStatusRowLegend|TestStatusRowSymbols|TestStatusCompactionLegend' -count=1`
  → **ok** (legend gates green after the text change).
- **Ran (the pty surface, acceptance (c)):**
  `go test ./testsuite/ -run TestChatPTY_ContextDenominatorIsQualifiedOnScreen -count=1 -v`
  → **PASS (14.03s)**, its own log lines:
  `pty stream carried "92k/1M≡"; dump (…): frames=142 dumps=26` and
  `pty settled row: pair at 4102, rate "hit 13.4%" at 4127, transport between: "92k/1M≡ ·↑12k↓80k "`.
- **Ran (the legend's own text, Rule 31):** `./bin/gopi chat -h | grep -c
  TestStatusRowKeepsBothThePairAndTheRate` → **1** (the reader-visible help carries it).
- Result: on the real pty the settled row states the ctx pair, then the cache transport,
  then the named rate — pair leading — and both facts are asserted; the same invariant is
  asserted at unit speed; the geometry is untouched and the budget is still 100/100.
- Commit: `d9df7776` on `task/01a0fa8115587e3d99b9512d13c72c57` (a merge of master
  follows it, per Rule 30a: align last).

## Evidence (self-filed)

- **Red proof 1 — the mirror defect (rate dropped, pair kept).** Mutation:
  `statusCacheHitRateAppend` returns `pair` unconditionally; built, surface re-run:
  `go test ./cmd/agent/ -run TestStatusRowKeepsBothThePairAndTheRate` →
  `FAIL … the pinned row carries the ctx pair but NOT the named rate "hit 74.5%"`;
  `go test ./testsuite/ -run TestChatPTY_ContextDenominatorIsQualifiedOnScreen` →
  `FAIL … the settled row does not carry the hit rate "hit 13.4%"`. Mutation reverted
  (`cp` of the pristine file kept in the session dir); both green again.
- **Red proof 2 — the order reversed (rate before the pair).** Mutation: the metrics
  field order swapped; built, surface re-run → `FAIL … the ctx pair does not LEAD the hit
  rate on the settled row (pair at byte 4118, rate 4102)`. Reverted; green.
- **The separate top-level separator count, measured:** probe of the composed row —
  `middot at cell 15 ctx="flash ·  iter"`, `26 "er 12 ·  ⚙ ke"`, `49 "ch l… ·  ctx "`,
  `65 "/64k≡ ·↑12k↓4"` = 3 top-level `·` plus the group's 2-cell internal seam
  (`statusMetricsSep=" ·"`). `statusColumnSepTotalN=3` therefore already matches.
- **The budget is not widened, measured:** `grep -n 'statusLayoutBudgetCols = '
  cmd/agent/chat_stream.go` → `216: statusLayoutBudgetCols = 100`;
  `git diff master -- cmd/agent/chat_stream.go | grep -c .` → `0`.
- **The pin, measured:** probe — `pin=100 columnsAtCols=100 slack=0 budget=100`,
  `formWidth(statusRowColumns())=100 nColumns=4`.
- **The rate never yields at the pinned geometry, measured:** probe over widths 40..100
  → `pair/rate mismatches=0`; over magnitudes `12300 … math.MaxInt` the row is 97 cells
  with `hit 50.0%` present at every one. This is why acceptance (e)'s "the rate yields"
  branch is unreachable at 100 cells and the invariant asserted is coexistence.
- All probe files (`zz_probe*_test.go`) were **deleted before the commit**; the pristine
  `chat_stream.go` copy and the pre-fix/probe material live under
  `/home/user/.gopi/sessions/01a0fa81-1558-7e3d-99b9-512d13c72c57/`, never inside the
  worktree.
