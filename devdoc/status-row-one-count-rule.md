<!-- doc-class: normative -->
# The Status Row's Counts Are Drawn By ONE Rule

**Task** `01a0e4c7-9846-72d7-bfb0-9f1d595718d6` · branch `feat/status-row-one-count-rule`
**Epic** — one status row mixed two unit systems for the same quantity family.

## The defect, measured

The row's two count families were drawn by two formatters:

- the **cache pair** through `formatCountBounded` — the bounded k/M/G ladder
  (`statusCountUnits`), 3 significant digits, no trailing `.0`;
- the **context pair** through `formatCount` (`chat_stream.go:2548`) — a **k-only**
  ladder with a forced one-decimal tail (`"%.1fk"`).

On a real pty at 117x26 (scripted model, no traffic generated — Rule 27), one
session driven to two magnitudes, the SAME field on the SAME row:

```
ctx 19.5k/200.0k≡ ·↑0↓20k     1s · ready     ← the count in k
19500.1k/200.0k≡ ·↑0↓20M      1s · ready     ← the count in M, and the label is GONE
```

Three defects ride in those two strings, and they are one defect:

| # | defect | evidence |
|---|---|---|
| 1 | **k where M applies** — 19 500 128 tokens drawn as `19500.1k`, while the cache pair two columns right says `20M` | `19500.1k` |
| 2 | **a forced tail** — `"%.1fk"` prints `.0` for a whole number of thousands; the tenth is the format's constant, not a measurement | `19.5k`, `200.0k` |
| 3 | **unbounded form** — `formatCount` grows with its input (5 cells at `12.3k`, **7** at `1000.0k`), so the sub-cell's own cut took the field's LABEL off the front | row 2 above has no `ctx` |

A reader could not apply one rule to the row; the scale had to be re-derived per
field. That is the whole complaint.

## The fix — one ladder, one digit policy, one width bound

`formatCount` is **deleted** (`cmd/agent/chat_stream.go`), not deprecated: its only
production caller was `statusCtxPair`, and leaving a second formatter alive leaves a
second rule for a future field to pick up. Every count on the row now goes through
`formatCountBounded` / `boundedCountRect` — the constants in
`cmd/agent/chat_status_cache_bound.go`:

| the rule | the ONE definition | the value |
|---|---|---|
| unit ladder | `statusCountUnits` | `"" , k, M, G` (1000x rungs) |
| digit policy | `statusCountFractionDigits` / `statusCountWholeDigitsWidest` | 0 decimals, 2 whole digits |
| a side's ceiling | `statusCountSideTextWidest` | **3** cells (MaxInt → `92G`) |
| totality | `boundedCountRect`'s rung walk + `statusCountSmallestMantissa` | every `int` renders inside the bound |

**No second ladder and no bare literal.** The context sub-cell's per-side ceiling was
a bare `6` — a measurement of *the retired formatter's* worst case (`200.0k`,
`1000.0k`), i.e. a reservation that silently named the formatter it was built for.
It is now spelled as the `max` of its two DERIVED terms:

```go
statusCtxPairSideCellsWidest = max(statusCountSideTextWidest, statusCtxPairSideCellsPinned)
statusCtxPairSideCellsPinned = 6
```

`statusCtxPairSideCellsPinned` is not a formatter's width but a **column's** width —
what the budget re-partitioned for this pair when the label and the qualifier were
paid for (`statusCtxCol`'s arithmetic). It is kept deliberately: shrinking the term to
the ladder's 3 was TRIED and is wrong, because it moves `statusPinCols` 100 → 94 and
`statusPinSlackCells` 0 → 6, leaving every top-level column reserving cells the drawn
row no longer occupies — the retired-column defect at the row's own scale. The pin
`statusPinCols == statusLayoutBudgetCols` with slack 0 is a hard contract
(`TestStatusRowAtThePinIsExactlyTheBudget`,
`TestStatusCountTotalityCostsNoCell`), and this change does not reopen it.

The composition rule is therefore: **the ladder's bound is what the form may never
exceed; the column's pin is what the budget has already reserved.**

## After-state, same pty, same script

```
ctx 20k/1M≡ ·↑0↓20k          1s · ready     ← k magnitude
ctx 20M/1M≡ ·↑0↓20M          1s · ready     ← M magnitude
```

Same ladder (never `k` where `M` applies), same decimal policy (no `.0`), and the
label now survives at BOTH magnitudes — because the form's worst case (3 cells) is
inside the reservation the yield path used to be needed for.

## Guards landed

`cmd/agent/chat_status_row_unit_rule_test.go`:

- `TestStatusRowCountsShareOneLadder` — the production row at four magnitudes (k, M,
  G, `MaxInt`): unit is a rung, the rung is the one the row's ladder states, no
  trailing `.0`, the side fits the bound, both pairs whole, row inside the pin.
- `TestStatusRowUnitRuleIsRedOnTheRetiredFormatter` — **the red proof**: the
  predicate is handed the retired formatter's own output (spelled locally as the
  literal it was, since the production spelling is gone) and must report the ladder
  defect and the `.0` defect. It also asserts the live row carries **no** retired
  spelling — so re-adding a k-only count to this row fails a test.
- `TestStatusCtxPairReservationCoversTheLadder` — walks rung boundaries and the int
  extremes against the per-side ceiling, and pins that the deployed-window sub-cell
  keeps its **label** (the yield path must not fire at a magnitude the ladder covers).

`statusRowUnitRuleProblems` is written as a predicate over two **rendered sides**
rather than over counts, precisely so the retired formatter's output can be fed to
the same predicate — a red proof that re-implemented the check would only prove the
copy agrees with itself.

## Not Done (honest)

- **The predicate's rung test is `boundedCountRect`'s own answer, not
  `countScaleRung`'s.** These two disagree at a real boundary and the disagreement is
  the shipped behaviour, not an inconsistency this change introduced: measured,
  `countScaleRung(200000)` → rung `k` while `boundedCountRect(200000)` → `"1M"`,
  because the ladder rounds once and ADVANCES the rung when the whole part (200)
  passes `statusCountWholeDigitsWidest` (2), then states
  `statusCountSmallestMantissa` on the rung it landed on. The predicate therefore asks
  the EPIC's question — "does this spelling belong to the row's unit system?" — via the
  ONE function that turns a count into a unit. **A separate defect is named here and
  NOT fixed**: `countScaleRung` and `boundedCountRect` give different units for the
  same value, and any other caller that trusts `countScaleRung` for the *displayed*
  unit would be wrong at 200000. It has no such caller today (the geometry helper is
  the only one, and it reads the ceiling rather than the rung), so it is logged, not
  repaired — the honest thing is that a reader of this doc knows it.
- The **pinned geometry was not re-partitioned** to the ladder's 3-cell bound. Doing so
  is arithmetically possible (pin 94, slack 6) but requires spending 6 cells in some
  top-level column that draws them; the candidates (`statusModelCol` 14, `statusIterCol`
  7, `statusToolCol` 16, `statusLiveCol` 13) are all documented as exactly their widest
  drawn value, and re-partitioning them is a separate epic's decision. The reservation
  is therefore left conservative — the property the epic asked for (one rule) does not
  depend on it.
- The full `cmd/agent` package suite **stalled once** in an unrelated test
  (`TestTmpDirSettingsValueAmbientFallbackLeaks`, `tmpdir_settings_value_test.go:435`,
  a child blocked in `RunGopi`) during the first background run; that test passes in
  isolation on this tree, and the re-run's verdict is reported in the task row. It does
  not touch the status row's code.

## Verification

- Build: `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → exit 0.
- Tests: `go test ./cmd/agent/ -run 'TestStatus|TestCtx|TestMetrics|TestCache' -count=1`
  → `ok github.com/the operator/gopi/cmd/agent 4.323s`; the three new tests
  (`TestStatusRowCountsShareOneLadder`, `TestStatusRowUnitRuleIsRedOnTheRetiredFormatter`,
  `TestStatusCtxPairReservationCoversTheLadder`) → `ok … 0.230s`, all PASS.
- Ran: two real pty sessions per binary at 117x26, scripted SSE stub, no traffic:
  ```
  ./driver.sh ./bin-gopi-before before 19500     k 8931
  ./driver.sh ./bin-gopi-before before 19500128  M 8932
  ./driver.sh ./bin-gopi-after  after  19500     k 8933
  ./driver.sh ./bin-gopi-after  after  19500128  M 8934
  # inside driver.sh:
  PTY_SCRIPT=one.txt GOPI_HOME=$RUN/home GOPI_CHAT_DUMP=$E/$TAG-$NAME.dump GOPI_MODEL=fake-model \
    ANTHROPIC_BASE_URL=http://127.0.0.1:$PORT ANTHROPIC_AUTH_TOKEN=sk-fake ANTHROPIC_MODEL=fake-model \
    timeout 90 python3 pty_multi.py $E/raw-$TAG-$NAME.log 117 26 "$BIN" chat --tools
  ```
- Result: rows quoted above; dump header `terminal=117x26 geometry=layout mode=ansi
  frames=79`; stub log `req 0 input_tokens=19500` / `req 0 input_tokens=19500128`
  (the two magnitudes, reached through the composition path).
- Commit: `81c29f95` — `fix(status): one count rule for the whole row — the context pair
  joins the ladder` — the branch tip in `feat/status-row-one-count-rule`; it carries the
  code, the guards and this doc in ONE commit. (`ff0c5cf5` was an earlier revision of the
  same message and is **not** an ancestor of `HEAD` — `git merge-base --is-ancestor
  ff0c5cf5 HEAD` → non-zero; it is superseded, never cite it.)

## Evidence (self-filed)

- Before-k row `ctx 19.5k/200.0k≡ ·↑0↓20k` and before-M row `19500.1k/200.0k≡ ·↑0↓20M`
  — read from `evidence/before-k.dump:32` and `evidence/before-M.dump:32`
  (each dump prints the same row twice, at lines 32 and 60; `grep -n '≡' <dump> | head -1`).
- After-k row `ctx 20k/1M≡ ·↑0↓20k` and after-M row `ctx 20M/1M≡ ·↑0↓20M` —
  `evidence/after-k.dump:32`, `evidence/after-M.dump:32`.
- The two magnitudes came from the scripted stub's own usage report, NOT from generated
  traffic: `grep -h input_tokens evidence/fake-*.log | sort -u` →
  `req 0 input_tokens=19500` / `req 0 input_tokens=19500128`.
- Dump provenance line, `evidence/after-k.dump:1`:
  `#gopi-chat-dump pid=… terminal=117x26 model=117x26 grid=117x26 geometry=layout mode=ansi frames=79`.
- Scoped suites: `ok github.com/the operator/gopi/cmd/agent 4.323s` (observed).
- **Evidence integrity**: `sha256sum` of a fresh build of the committed tree, of
  `bin/gopi`, and of the binary the after-state pty ran, are all
  `c400ce0c0665fbfc…` — the row quoted above is provably from the shipped code, not
  from an intermediate tree (observed).
- New tests: `--- PASS: TestStatusRowCountsShareOneLadder`, `--- PASS:
  TestStatusRowUnitRuleIsRedOnTheRetiredFormatter`, `--- PASS:
  TestStatusCtxPairReservationCoversTheLadder` (observed).
- **Re-observed on the branch tip `81c29f95`** (this revision): `CGO_ENABLED=0 go build
  ./cmd/agent/` → exit 0; the three new tests → `ok … 2.084s`, each `--- PASS`; the doc's
  stated suite → `ok github.com/the operator/gopi/cmd/agent 9.922s`; the dump rows re-read at the
  corrected line numbers.
- `gofmt -l` on the touched files: only `chat_stream.go`, whose single hunk is at line
  796 (pre-existing prose drift, proven by `git show HEAD:cmd/agent/chat_stream.go |
  gofmt -l /dev/stdin` → `/dev/stdin`); my edited regions are at 500 / 2081 / 2574 and
  add no drift.
