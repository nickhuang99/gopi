<!-- doc-class: historical -->
# The chat status row's width ladder, re-derived from the shipped binary

**Task** `01a0faa7-575f-74e6-83e4-6f792b350f17` (the `[3/4]` child of the
ctx-pair-leads re-partition epic). **Base** `9316f3dd`. Every row below was read off
a binary built from that tree, driven on a real pty one terminal width at a time.

## Completed

The ladder's table — `cmd/agent/chat_status_ladder_test.go`, the composer's
statement of which columns survive at which width — is now checked against what the
**binary paints**, and the painted rows are recorded verbatim.

- **The instrument** — `testsuite/chat_status_row_ladder_probe_test.go` (new). It
  starts the shipped binary on a pty, resizes it width by width, drives ONE real turn
  at each rung, and writes the row the terminal received, with its cell width, to
  `$GOPI_LADDER_PROBE_OUT`. Two arms, one scripted turn each: `usage` (a turn whose
  usage reports the context occupancy and the cache transport — the arm that measures
  the metrics group) and `tools` (a turn that really CALLS a tool — the arm that
  measures the tool column, which no settled usage-arm row ever carries).
- **Classifier** — the walk is `heavytest.Require(t, CriterionDeviceExec)` and its
  call site is in `testdata/heavy-tests.tsv`, so the smoke path skips it. The arm's own
  switch (`GOPI_LADDER_PROBE_ARM`) keeps even a heavy run from driving 26 turns nobody
  asked for.
- **The rows** — `~/.gopi/sessions/01a0faa7-575f-74e6-83e4-6f792b350f17/ladder-rows-usage.txt`
  (26 rungs) and `…/ladder-rows-tools.txt` (9 rungs), reproduced below.

### The usage arm (metrics group, the row's own counters)

```
cols= 31 cells= 29 row=deepseek-v4-flash  1s · ready
cols= 32 cells= 29 row=deepseek-v4-flash  3s · ready
cols= 33 cells= 29 row=deepseek-v4-flash  5s · ready
cols= 34 cells= 29 row=deepseek-v4-flash  7s · ready
cols= 35 cells= 29 row=deepseek-v4-flash  9s · ready
cols= 36 cells= 30 row=deepseek-v4-flash  11s · ready
cols= 37 cells= 30 row=deepseek-v4-flash  13s · ready
cols= 38 cells= 30 row=deepseek-v4-flash  15s · ready
cols= 39 cells= 39 row=deepseek-v4-flash · iter 9  17s · ready
cols= 40 cells= 38 row=deepseek-v4-f… ·  iter 10  19s · ready
cols= 48 cells= 46 row=deepseek-v4-f… ·  iter 11          21s · ready
cols= 49 cells= 47 row=deepseek-v4-f… ·  iter 12           23s · ready
cols= 50 cells= 48 row=deepseek-v4-f… ·  iter 13            25s · ready
cols= 60 cells= 58 row=deepseek-v4-f… ·  iter 14                      27s · ready
cols= 62 cells= 60 row=deepseek-v4-f… ·  iter 15                        29s · ready
cols= 63 cells= 61 row=deepseek-v4-f… ·  iter 16 ·  tools off            31s · ready
cols= 64 cells= 62 row=deepseek-v4-f… ·  iter 17 ·  tools off             33s · ready
cols= 68 cells= 66 row=deepseek-v4-f… ·  iter 18 ·  tools off                 35s · ready
cols= 69 cells= 67 row=deepseek-v4-f… ·  iter 19 ·  tools off                  37s · ready
cols= 70 cells= 68 row=deepseek-v4-f… ·  iter 20 ·  tools off                   39s · ready
cols= 80 cells= 78 row=deepseek-v4-f… ·  iter 21 ·  tools off                             41s · ready
cols= 95 cells= 93 row=deepseek-v4-f… ·  iter 22 ·  tools off                                            43s · ready
cols= 98 cells= 96 row=deepseek-v4-f… ·  iter 23 ·  tools off                                               45s · ready
cols= 99 cells= 97 row=deepseek-v4-f… ·  iter 24 ·  tools off                                                47s · ready
cols=100 cells= 98 row=deepseek-v4-f… ·  iter 25 ·  tools off           ·  ctx 92k/1M≡ ·↑1M↓2M hit 13.4%      49s · ready
cols=117 cells= 98 row=deepseek-v4-f… ·  iter 26 ·  tools off           ·  ctx 92k/1M≡ ·↑1M↓2M hit 13.4%      51s · ready
```

### The tool arm (a turn that really calls a tool)

```
cols= 40 cells= 37 row=deepseek-v4-f… ·  iter 1   1s · ready
cols= 50 cells= 47 row=deepseek-v4-f… ·  iter 2             3s · ready
cols= 62 cells= 59 row=deepseek-v4-f… ·  iter 3                         5s · ready
cols= 63 cells= 60 row=deepseek-v4-f… ·  iter 4  ·  no tool              7s · ready
cols= 68 cells= 65 row=deepseek-v4-f… ·  iter 5  ·  no tool                   9s · ready
cols= 70 cells= 68 row=deepseek-v4-f… ·  iter 6  ·  no tool                     11s · ready
cols= 95 cells= 93 row=deepseek-v4-f… ·  iter 7  ·  no tool                                              13s · ready
cols=100 cells= 98 row=deepseek-v4-f… ·  iter 8  ·  no tool             ·  ctx 19/1M≡ ·↑0↓90 hit 0.0%         15s · ready
cols=117 cells= 98 row=deepseek-v4-f… ·  iter 9  ·  no tool             ·  ctx 19/1M≡ ·↑0↓100 hit 0.0%        17s · ready
```

Plus, on the one turn whose in-flight paint changed (cols=40 — a row is repainted
only when its composed TEXT changes, so the digest is painted once, not at every
rung), the tool column's OWN content:

```
	in-flight cells= 26 row=⚙ call bash head -3 go.mod
```

## The three widths the re-partition tuned

`95`, `70`, `68` — the widths the epic's siblings name — each produce this row (the
usage arm's row at 95/70/68, verbatim from the file above):

```
cols= 95 cells= 93 row=deepseek-v4-f… ·  iter 22 ·  tools off                                            43s · ready
cols= 70 cells= 68 row=deepseek-v4-f… ·  iter 20 ·  tools off                   39s · ready
cols= 68 cells= 66 row=deepseek-v4-f… ·  iter 18 ·  tools off                 35s · ready
```

**The finding, stated plainly: the three "tuned" widths are three rungs of ONE row.**
They differ in nothing but trailing air — same columns, same separator rhythm, same
shed form — because the ladder's steps are the FORM widths, and the form the row is
in has been 63 cells wide since well above 70 (see the table below). The narrower
reading, which the earlier report's own row (`… `cols 70` keeps the tool (69-cell
form fits), `cols 68` drops it`) carries, is the composer's pre-re-partition
arithmetic: the form that fits at 70 is 63 cells, not 69, and 68 does not drop the
tool column. What the numbers 68/70/95 are is the widths the re-partition TUNED
(the pin's own slack, the boundary of the form, the operator's neighbouring width),
not three widths that produce three different rows.

## The ladder's own arithmetic (the composer, for comparison)

From `statusDegradationOrder` resolved at the base commit (`statusFormFor` +
`statusFormWidth`, column widths 14 / 7 / 19 / 33):

| form | columns kept | reserved width | the width at which it is selected |
|---|---|---|---|
| 0 | model, iter, tool, metrics(group) | `100` | `cols >= 100` |
| 1 | model, iter, tool | `63` | `63 <= cols < 100` |
| 2 | model, iter | `40` | `cols < 63` |

`statusColumnsAtCols = statusLayoutBudgetCols = 100`, `statusPinSlackCells = 0`.
So the composer's switch points are **100 → 63 → 40**, and the painted rows above
agree with them exactly (the first row carrying `tools off` is cols=63; the first row
carrying the metrics group is cols=100; `39` is the lowest width at which the ALIGNED
row is still painted — at 38 and below the row falls back to the 30-cell unaligned
composition, which is the row the composer refuses to align for a terminal of that
width).

## Cross-check against `chat_status_ladder_test.go`

The table's six cases, against the painted row at the same width (`tool` = the tool
column is on the row — its own content or its label; `ctx` = the metrics group is;
`cost` = a money field is):

| case width | table says (tool / ctx / cost) | the painted row |
|---|---|---|
| `100` (the pin) | true / true / false | ✅ `tools off` + `ctx 92k/1M≡ ·↑1M↓2M hit 13.4%`, no money field |
| `95` | true / false / false | ✅ `tools off`, no metrics |
| `80` | true / false / false | ✅ same |
| `70` | true / false / false | ✅ same |
| `60` | false / false / false | ✅ no third field |
| `50` | false / false / false | ✅ same |

All six agree. `cost` is the absence proof the table calls it: no money field
appears at any of the 26 usage rungs or the 9 tool rungs.

## What the screen showed that the table cannot

1. **The row's own floor is a measurement, not the form table.** The composer's
   narrowest form is 40 cells, but the ALIGNED row is painted down to a 39-cell
   terminal (cols=39 → a 39-cell row) and the unaligned fallback appears at 38. The
   table's narrowest case (`cols: 50`) does not reach that boundary, so nothing in it
   states where the aligned composition actually ends.
2. **`"tools off"` is a substring of `"no tool"` — do not read the row by grep.**
   Automated greps for "tools off" over the accumulated stream match rows in the 40-
   to 62-cell band that plainly do not carry the column; the row has to be read as a
   row (the probe's own reader is what does this).
3. **A descending walk measures stale forms.** The region repaints on the width the
   terminal reports *at the paint it decides to run*, and it only paints when the
   row's composed text changed. Walking 117 → 24 reported the shed form at 99/98/95
   even though the composer keeps the full form down to 100. The instrument's walk is
   ASCENDING for this reason, and the reason is written at `ladderProbeWidths`.
4. **The tool column's own content exists only in flight.** Settled, the column reads
   `no tool` (tools enabled) or `tools off` (tools disabled) — never the glyph. The
   glyph and the call's digest are painted while the call runs, and the instrument
   reads them off the turn's own byte stream.

## Not Done

- **The tool arm's in-flight row is captured only where the paint was NEW.** The
  recorded runs show it at cols=40 alone, because the row is deduplicated on its
  composed text and the digests at the other rungs differed only in cells that were
  elided identically. Driving a *different* tool call at each rung would capture all
  of them; it was not done, and no claim in this report rests on the in-flight row
  beyond the one rung that shows it.
- **The siblings' own subjects are not measured here.** `01a0faa7-55b5` (the ctx
  pair leads, at the pin) and `01a0faa7-58f9` (the tool column's way-marker breadth)
  land their own rows; this task's rows are the form ladder, and the two branches are
  expected to move the widths above if they change the pin.
- **The composer-side probe is not kept.** The form table above was read with a
  throwaway `cmd/agent` test, deleted before the commit: the durable statement of the
  same arithmetic is `chat_status_ladder_test.go`'s own table and its guard, and a
  second copy of the numbers in this tree would be a stale copy.

## Verification

- Build: `CGO_ENABLED=0 go build -o <session>/gopi ./cmd/agent/` — OK (the binary
  every row above was read from, built from base `9316f3dd` + this branch).
- Tests: `go test ./cmd/agent/ -run TestStatusLadder -v -count=1` — PASS
  (`TestStatusLadder_FormPerWidth`, `_LiveBlockIsReserved`,
  `_ReachableOnScreenBelowThePin`, `_EveryNameIsAColumnOfTheRow`,
  `_GuardFailsOnAPhantomColumn`, `_FormsNarrowStrictly`).
  `go test ./internal/standards/ -run Heavy -count=1` — ok;
  `go test ./internal/standards/ -run TestCensusGate -count=1` — ok;
  `go test ./internal/testutil/ -run 'Rule27|Rule35' -count=1` — ok.
  `go vet ./testsuite/` — clean.
- Ran: `GOPI_BIN=/home/user/.gopi/sessions/01a0faa7-575f-74e6-83e4-6f792b350f17/gopi
  GOPI_HEAVY_TESTS=1 GOPI_HUMAN_TERMINAL=1 GOPI_LADDER_PROBE_ARM=usage
  GOPI_LADDER_PROBE_OUT=…/ladder-rows-usage.txt go test ./testsuite/
  -run TestChatStatusRowWidthLadderProbe -v -count=1 -timeout 900s`
  (and the same with `GOPI_LADDER_PROBE_ARM=tools`).
- Result: `--- PASS: TestChatStatusRowWidthLadderProbe (58.97s)` for the usage arm
  (26 rungs) and `(25.10s)` for the tool arm (9 rungs); the rows written to the two
  files are the ones reproduced above. The unarmed run reports the refusal
  (`HEAVY TESTS REFUSED: this process is a WORK context …`) — the classification
  working, not a failure.

## Evidence (self-filed)

- `go test ./internal/standards/ -run Heavy -count=1` → `ok
  github.com/nick/gopi/internal/standards 1.257s` (the new call site is in
  `testdata/heavy-tests.tsv`; the inventory is bidirectional, so the gate proves both
  halves).
- `go test ./cmd/agent/ -run TestStatusLadder -v -count=1` → `--- PASS:
  TestStatusLadder_FormPerWidth (0.00s) … ok github.com/nick/gopi/cmd/agent 1.044s`.
- The composer's form table above was printed by a scratch test run as
  `form 0 width=100 colwidths=[14,7,19,33] / form 1 width=63 colwidths=[14,7,19] /
  form 2 width=40 colwidths=[14,7]`, with `pin=100`; that file was deleted
  (`git status --porcelain` lists neither it nor any other untracked path before this
  commit).
- The rows themselves live in this run's session directory,
  `/home/user/.gopi/sessions/01a0faa7-575f-74e6-83e4-6f792b350f17/`:
  `ladder-rows-usage.txt` `8dc30c94b2d2b921012944e704f4509a2b0aea8d8ba89b00a7e17cb1319486a2`,
  `ladder-rows-tools.txt` `c971028550d10fac94f7669f947f1322b761b85f0e4cceb26a8c5bead846244c`.
- `make test-budget PKGS='./testsuite ./internal/standards'` → `exit=0 outcome=pass`
  (stream `run-1790917944-32663.json`), including
  `TestHeavyTestInventoryMatchesItsCallSites` on the new row.
