<!-- doc-class: historical -->
# Task Report: the status row's cache column — remove the column that was never drawn, and make the shed order checkable

**Task** `01a0dbe28a0b7d48b7035e5975dbf8f5`
**Branch** `task/01a0dbe28a0b7d48b7035e5975dbf8f5` (worktree `/home/user/workspace/gopi-01a0dbe28a0b7d48b7035e5975dbf8f5`)
**Base** `17a5969a`; **master merged in twice** — `d963c467` (master's `779761f4`,
the sibling 01a0dbc7) and, at the delivery point, `f8611f80` (master's `6b36002e`)
**Commits** `f6925b4c` (the fix) → `d963c467` (merge master) → `43d4fa1d` (guard + red
path + this report) → `a37705f4` (pty re-verification). *(`2e363b5f` below was the
pre-rebase commit of the guard — same tree, rewritten by the delivery-point sync;
`git merge-base --is-ancestor 2e363b5f HEAD` → NO, `43d4fa1d` is its live form.)*

## The decision, and the arithmetic behind it

The epic offered two honest answers: **make the cache column actually drawn where
it fits**, or **remove the cache name/claim** so the documented order matches the
columns the row carries. This run chose **the second**, and not as the cheaper
option — the first is **unattainable at this budget**, for a reason that does not
depend on taste:

```
columns    model 14 + iter 7 + mode 5 + tool 16 + ctx 16 + cost 7 = 65
separators statusColumnSepTotalN 5 × StatusColumnSepCells 4      = 20
gap        StatusLineGapCells                                    =  2
live       statusLiveCol                                         = 13
                                                    total        = 100
statusLayoutBudgetCols                                           = 100   → slack 0
```

Zero slack is enforced at compile time (`const _ = uint(statusLayoutBudgetCols -
statusColumnsAtCols)`); a seventh column of `statusCacheCol` (10) plus its
separator (4) would be 114 and would not build.

**A wider terminal does not help**, and this is the part that decides it rather
than the pin: `chatStatusSource.status()` only ever *narrows* the composed width —

```go
layoutCols := statusColumnsAtCols
if availCols > 0 && availCols < layoutCols { layoutCols = availCols }   // chat_stream.go
```

— so a 200-column terminal still composes the 100-cell geometry, and the ladder can
only remove columns from it. There is therefore **no width at any terminal** at
which a cache column could be drawn: "cache is the FIRST thing shed as the terminal
narrows" was unobservable by construction, not by accident.

One more measurement settled it. The reservation did not even fit its own canonical
content: the epic's own example render `↑12.3k ↓4.2k` is **12 cells against a
10-cell column**, so `padStatusColumn` would have cut the very values the column
existed to show. The column could have been neither reached nor read.

### What was retired, and what was kept

`cmd/agent/chat_stream.go`: `statusCacheCol` (now a RETIRED note at its own site
carrying the arithmetic, where a future reader would try to add it back),
`statusCacheField`, `statusCacheReadGlyph` / `statusCacheWriteGlyph` (`↑`/`↓`) —
all three had **zero callers**. **Kept**: `statusOps.cacheRead` / `cacheWrite`,
which `statusOpsFor` feeds to `pricing.CostOf` — the cache counters are real, they
reach the screen through the **cost** column, and their doc comment now says that
instead of describing a column that does not exist. The ladder's doc comment (and
the `KNOWN GAP` paragraph master landed, which filed this exact defect) is replaced
by the closure note.

### A second defect the arithmetic surfaced

Summing the ladder's forms — the arithmetic the epic asked for — found a step that
**no terminal can ever select**:

| form | columns kept | cells |
|---|---|---|
| 0 | model iter mode tool ctx cost | 100 |
| 1 | model iter mode tool ctx | 89 |
| 2a | model iter mode **tool** | **69** |
| 2b | model iter mode **ctx** | **69** |
| 3 | model iter mode | 49 |

`statusColumnsForWidth` takes the **first** form that fits, so 2b was dead code —
and the consequence was behavioural, not cosmetic: **ctx shed BEFORE tool**, the
opposite of the doc comment's own argument that ctx "is the number that predicts a
compaction" and is therefore the last counter to go. The duplicate is removed, the
order is now `cost → tool → ctx` as documented, and
`TestStatusLadder_FormsNarrowStrictly` fails on a repeated or non-narrowing step.

## Completed

- `cmd/agent/chat_stream.go`
  - `statusCacheCol`, `statusCacheField`, `statusCacheReadGlyph`,
    `statusCacheWriteGlyph` **removed**; a RETIRED note at the `statusCacheCol`
    site carries the reasoning and the arithmetic.
  - The ladder doc comment no longer names `cache`; it records that the step was a
    no-op at every width, and replaces master's `KNOWN GAP` paragraph with the
    closure.
  - `statusDegradationOrder`: the duplicate 69-cell form removed — one reachable
    step per width, ctx last as documented.
  - **New** `statusRowColumns()` — which columns the row CARRIES, with their
    reserved widths, as ONE production function, so a test resolves the ladder's
    names against the geometry the row is actually composed from. `status()`
    copies it and fills the Texts.
  - **New** `statusColumnNames` (column key → name) and `statusShedOrder` (the
    documented order as a value). The order lived only in prose, which is exactly
    how the row came to document a column it never carried: **prose cannot fail a
    build**.
  - `statusOps.cacheRead/cacheWrite` doc corrected; the formatter provenance list
    now points at `statusCostField` for the cost read-out.
- `cmd/agent/chat_status_ladder_test.go`
  - `TestStatusLadder_EveryNameIsAColumnOfTheRow` — **the epic's guard**: every
    name in the shed order resolves against `statusRowColumns`; the name table and
    the geometry are the same size; no column is unnamed; and the documented order
    is cross-checked against the order **derived from the forms**
    (`ladderShedOrderFromForms`), so doc drift fails from either side.
  - `TestStatusLadder_GuardFailsOnAPhantomColumn` — **the guard's own red path**
    in-repo: a phantom column and a name table whose keys miss the geometry are
    handed to `ladderNameProblems` and must be rejected **by name**, with the
    pristine geometry as the control.
  - `TestStatusLadder_FormsNarrowStrictly` — strictly narrowing forms; widest ==
    `statusColumnsAtCols`; narrowest == the identity set; and the named sum +
    `statusFormWidth` both equal `statusLayoutBudgetCols` **exactly**.
  - `TestStatusLadder_FormPerWidth` — the per-width table re-pinned to the
    corrected ladder (100/95/80/70/68/60) and extended with the retired-glyph
    assertion; `TestStatusLadder_ReachableOnScreenBelowThePin` (master's) corrected
    to the real order at 80 cols (cost gone, tool gone, ctx — 12.6k — still there).
- `devdoc/chat-status-cache-column-decision.md` (+ its `DOC-INDEX.tsv` row).

## Not Done (honest)

- **The cache column is not drawn at any width.** That is the deliverable, not an
  omission — but it means the epic's either/or is answered *by proof that the
  first branch is impossible at this budget*, not by implementing it.
- Nothing else is deferred on this task's own scope. The one defect this run
  measured OUTSIDE that scope (below) was fixed by master's own merge while this
  run was in flight, and is re-verified here rather than left as a claim.

## Verification

- **Build**: `go build ./...` → **OK** (job-2 output: `go build: OK`).
- **Tests**: `go test ./cmd/agent/ -run TestStatusLadder -count=1` →
  `ok github.com/nick/gopi/cmd/agent 1.827s`; full branch-scoped smoke
  (`go test ./cmd/agent/ ./internal/lineedit/ -count=1`) — see `## Smoke`.
- **Ran** (Rule 31 — the changed surface, executed, not `-h`):

  ```
  CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/
  python3 /home/user/workspace/_pty01a0dbe2/pty_ladder_probe.py \
      <worktree>/bin/gopi /home/user/workspace/_pty01a0dbe2/out3 \
      /home/user/workspace/_pty01a0dbe2/home3 /home/user/workspace/_pty01a0dbe2/scratch3 \
      100 95 80 70 60 120 200                 # the FINAL-tree re-run quoted below
  ```

  A **real pty** (stdlib `pty.openpty` + `TIOCSWINSZ` per width, no external
  binary), the **shipped binary**, a **dead backend**
  (`ANTHROPIC_BASE_URL=http://127.0.0.1:9`, so no network call), and the chat's
  **own** `GOPI_CHAT_DUMP` read back as `screen.lines`.

- **Result** — the status row the terminal actually received, per width:

  | cols | cells | `·` seps | cost (`元`) | tool (`⚙`) | ctx | row |
  |---|---|---|---|---|---|---|
  | 120 | 97 | 6 | yes | no¹ | no¹ | `deepseek-v4-f… ·  iter 0  ·        ·                   ·                   ·  ~0.00元   0s · ready` |
  | 100 | 97 | 6 | yes | no¹ | no¹ | same |
  | 95 | 92 | 5 | **no** | no¹ | no¹ | `… ·        ·        ·                   ·                          0s · ready` |
  | 80 | 77 | 4 | no | **no** | yes² | `… ·        ·        ·                               0s · ready` |
  | 70 | 67 | 4 | no | no | yes² | `… ·        ·        ·                     0s · ready` |
  | 68 | 65 | 3 | no | no | **no** | `… ·        ·  ·                            0s · ready` |
  | 60 | 57 | 3 | no | no | no | same shape |

  ¹ no tool ran and no ctx had been measured at the prompt in this run — those
  columns are present but EMPTY (a valueless column is padded, never dropped), so
  their markers are absent at every width; the shed sequence is still legible from
  the **separator count** (6 → 5 → 4 → 3) and the **row width** (97 → 92 → 77 →
  67 → 65 → 57), which is exactly the ladder stepping.
  ² at 80/70 the ctx column is empty in this run, which is why `ctx` is shown as
  surviving *by position*; the unit fixture (`TestStatusLadder_FormPerWidth`) pins
  the marker-level truth with values in every column.

  **`grep -c "↑\|↓" out2/dump-*.txt` → 0 in all seven dumps**: the retired cache
  glyph is on **no** row at **any** width — the epic's acceptance criterion for the
  implemented choice, shown on a real pty.

  **Re-run on the FINAL tree (`62fe24fe`, after the doc/guard commits), `/home/user/workspace/_pty01a0dbe2/out3`
  — same numbers, and the wide controls now proven identical by checksum:**

  ```
  dump-100.txt: cells=97 seps=6 glyphs=0     dump-120.txt: cells=97 seps=6 glyphs=0
  dump-95.txt:  cells=92 seps=5 glyphs=0     dump-200.txt: cells=97 seps=6 glyphs=0
  dump-80.txt:  cells=77 seps=4 glyphs=0
  dump-70.txt:  cells=67 seps=4 glyphs=0     md5(status row) at 100 == 120 == 200
  dump-60.txt:  cells=57 seps=3 glyphs=0     = d03cc4e6e0c1c79804092271f3f7d059
  ```

  The row is `039|deepseek-v4-f… ·  iter 0  ·        ·                   ·                   ·  ~0.00元  0s · ready`
  (100 cols — note the model cell is `deepseek-v4-f…`, ELLIPSIZED at this
  worktree's configured model name; the numeric cell counts above are what matter,
  the ellipsis is `padStatusColumn` doing its job on a longer live value).

- **Commit**: `43d4fa1d` carries the guard + this report; `a37705f4` the pty
  re-verification (branch `task/01a0dbe28a0b7d48b7035e5975dbf8f5`).

## Evidence (self-filed)

- **The name guard FAILS on the exact defect it exists for** (red-proof, run on
  THIS tree; `git stash` is refused with linked worktrees, so the file is copied
  aside and restored): re-adding `statusColumnNameCache = "cache"` to
  `statusShedOrder` and the name table →
  ```
  --- FAIL: TestStatusLadder_EveryNameIsAColumnOfTheRow (0.00s)
            - the shed order names cache, which is no column of the row
  ```
  Restored → `ok github.com/nick/gopi/cmd/agent 1.827s`.
- **The duplicate-step guard FAILS on the ctx-before-tool defect**: restoring the
  fifth form (`{model, iter, mode, ctx}` after `{model, iter, mode, tool}`) →
  ```
  --- FAIL: TestStatusLadder_FormPerWidth/narrower:_tool_yields_next_(69_cells_survive)
        cols=80: column tool on the row = true, want false (marker "⚙")
        cols=80: column ctx on the row = false, want true (marker "200.0k")
  --- FAIL: TestStatusLadder_FormsNarrowStrictly
  ```
  Restored → green.
- **Arithmetic**: `65 + 20 + 2 + 13 = 100 = statusLayoutBudgetCols`, asserted at run
  time by `TestStatusLadder_FormsNarrowStrictly` in addition to the compile-time
  `const _ = uint(statusLayoutBudgetCols - statusColumnsAtCols)`.
- **The below-the-pin half was measured, and then fixed by master, and re-measured**
  (recorded because this run carried the claim in an earlier draft and the evidence
  changed under it):
  - On this branch's ORIGINAL base, the pty showed the aligned row at 100/120 and
    the **unaligned** fallback at 95/80/70/68/60 (38 cells,
    `deepseek-v4-flash · iter 0  0s · ready`), with root cause
    `grep -rn "publishChatStatus(" --include=*.go cmd/agent/ | grep -v _test.go`
    matching only its own definition — the live loop publishes through
    `chatTurnStatus`, so `setAvailCols` was never called.
  - Master's `779761f4` (the sibling task 01a0dbc7) landed the compose-time
    viewport hook that fixes exactly this. After `git merge master`, the SAME pty
    probe on the SAME binary pipeline shows the **aligned** row at every width:
    97/92/77/67/65/57 cells with 6/5/4/4/3/3 separators — the ladder live below the
    pin, which is what makes the shed order observable at all.

## Smoke

`job(action=start)` on `go test ./cmd/agent/ ./internal/lineedit/ -count=1` (the
branch-scoped smoke Rule 27 prescribes, on the merged tree); recorded result in the
job's own output, with `go build: OK` as its first assertion.

## Real-pty re-verification (Rule 31, second run of the same surface)

The evidence above was produced by one run; this task's close-out re-ran the SAME
surface independently — a shipped binary of the committed tree, a real pty, a dead
backend (`ANTHROPIC_BASE_URL=http://127.0.0.1:9`, so the turn fails and the status
row is the only thing moving on the grid), the dump taken by the row's own sink.

**The recipe is re-runnable and needs no tool from the repo** — a ~40-line
program (`/home/user/workspace/scratch-chat-pty`, scratch, deliberately NOT
committed: Rule 30 wants the worktree clean and Rule 3 wants no new tool):

1. `syscall.Open("/dev/ptmx")` → `TIOCGPTN` (pty number) → unlock `TIOCSPTLCK` →
   open `/dev/pts/<n>`: a pty pair in pure Go;
2. `TIOCSWINSZ{Col: N, Row: 30}` on the MASTER before `cmd.Start()` — this is the
   width the child reads, and it is the one thing `script -qec` cannot set;
3. `cmd.SysProcAttr = {Setsid, Setctty, Ctty: 0}`, stdin/stdout/stderr = the slave;
4. the master's reader ANSWERS the child's terminal probes exactly as a real
   terminal does — `\x1b]11;?` → an OSC 11 colour reply, `\x1b[6n` → `\x1b[1;1R` —
   because `termenv` blocks waiting for both (without them the REPL never paints
   and the dump stays at `frames=0`);
5. `GOPI_CHAT_DUMP=<path>` names the dump; the status row is the last grid line
   before the live block.

```bash
export ANTHROPIC_BASE_URL=http://127.0.0.1:9 DEEPSEEK_BASE_URL=http://127.0.0.1:9
export GOPI_CHAT_DUMP=$D/screen-<N>.dump
ptyrun <N> 30 $D -- <worktree>/bin/gopi chat        # feed "hello\n" after the first paint
```

Measured on the committed tree (branch tip), widths 100/95/80/70/60 plus the two
WIDE controls 120/200 (the epic asks for the wide case explicitly):

| terminal cols | status cells (wcwidth) | separators | form | `↑`/`↓` present |
|---|---|---|---|---|
| 100 (the pin) | 97 | 6 | full | **0** |
| 95 | 92 | 5 | cost shed | **0** |
| 80 | 77 | 4 | tool shed | **0** |
| 70 | 67 | 4 | tool+ctx shed | **0** |
| 60 | 57 | 3 | identity + live | **0** |
| 120 (wide) | 97 | 6 | full — IDENTICAL to 100 | **0** |
| 200 (wide) | 97 | 6 | full — IDENTICAL to 100 | **0** |

The wide controls are the decisive half of the claim: a terminal with 20 and 100
cells of head-room left over paints the SAME 97-cell row, because the composed
width is `min(avail, statusColumnsAtCols)` — there is no width at which a seventh
column could appear. `grep -c '↑|↓'` over all seven dumps returns 0.

Guard behaviour re-observed on this tree (Rule 31: the surface, not just the
suite): `go test ./cmd/agent/ -run 'TestStatusLadder_' -count=1 -v` →
`FormPerWidth`, `EveryNameIsAColumnOfTheRow`, `GuardFailsOnAPhantomColumn`,
`FormsNarrowStrictly` all PASS in 0.99s.
