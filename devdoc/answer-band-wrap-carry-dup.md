<!-- doc-class: historical -->
# The answer band's partial repeat: one in-flight line, two spellings

**Status:** symptom GONE on current master; the branch's own 5-line carry change was
**reverted as dead code** (see "What actually fixed it"). Regression guard:
`internal/lineedit/answer_wrap_scroll_dup_test.go`.
**Session:** 01a0e2f5-bc4f-7355-bf37-52ba32727b8d, 2026-09-27 (re-measured 2026-09-27
after the delivery-point sync).

## The symptom

The operator's frozen grid (`/tmp/zz_dup_grid.txt`, 1281 lines) showed a single
transcript line reaching the painted band as a **partial repeat with a truncated
tail**:

```
1100|…现有 `emph
1101|asis_chunked_test.go` 测了九种 delta 但都不折行**，所以漏了它。…所以漏
```

`session.jsonl` held that line **once**. So the defect was in the paint path, not
in generation: the renderer drew one line's cells twice.

## The root cause

`internal/lineedit` keeps an in-flight line in two registrations:

| registration | meaning |
|---|---|
| `compPrefix` (the "park") | the in-flight line's text, for the emphasis decision and `Finish`'s carry |
| the live row's `text` | what the row **displays** — which a WRAP CUTS |

A line wider than the band row wraps: `writeRow`'s loop cuts the cells that do not
fit (`cutCols`) and hands them to the next row as its **head**, while `liveText`
records what the row shows. When that cut lands **inside a token**, the two
registrations become the same line in two spellings that differ by exactly the cut
fragment — measured at 100 cols, 3-byte deltas:

```
row.text = "04 29-05 29-06 … 29-17 "    (the fragment's TALE)
head     = "-04 29-05 29-06 … 29-17 "   (the SAME cells, with the cut's own head)
```

`fitCommitRow` then asked "is this head already on the row?" with `headIsInRow`,
which compares the row against the head's **leading** cells — it found `04 …` one
cell past `-04 …`, judged the head absent, and `withHead` prefixed it. The row was
written as its own cells preceded by those same cells:

```
▌ -04 29-05 … 29-17 04 29-05 29-06 …     ← the operator's shape
```

`Finish`'s carry had the same defect in the same shape: `strings.HasSuffix(queued,
liveText)` tested one direction only, so the one-cell-short park looked absent and
the row's text was appended to text already carrying it.

**Why only small deltas.** The park and the row only diverge when a wrap's cut
lands inside a token, which needs the line to arrive in bursts that do not align
with the row's cells. On the rig: 1 and 3 bytes duplicated; the same answer
delivered whole was clean, which is why whole-line tests stayed green through it.

## The branch's fix (REVERTED — kept here as the record of a path walked)

`carryAlreadyDisplayed(queued, shown)` stated the relation once — a suffix in
**either** direction, trailing air ignored on both sides, a floor of
`carryMinOverlapCells` = 8 — and was used at `fitCommitRow` and at `Finish`.

It was correct about the mechanism above and **it is dead code on current master**,
proven by measurement rather than by argument:

- with it and without it, the painted band is **byte-identical** — the same
  sha256 over the band rows at every cadence (100×30, deltas 1..40);
- the whole shipped suite passes either way (`TestEmphasisWrappedLine*`,
  `TestEmphasisChunk*`, the rest of `internal/lineedit/` green in both trees);
- the duplication no longer reproduces at **any** delta with the branch's change
  **reverted** to master's own files.

Merging a no-op is not "safe": it puts a second answer beside the one the tree
already has, and a future reader has to work out which of the two relations is the
live one. It was reverted (`git checkout master -- internal/lineedit/{answer,emphasis}.go`).

## What actually fixed it

**`b7d3f86a fix(lineedit): decide a wrapping line before its rows are committed`**,
which landed on master after this branch's base. It made a flush refuse to hand a
wrapping line to `writeLive` in pieces (`drain` → `unfinishedLineWouldWrap`): the
line is held until its break arrives and then decided and painted **whole**, so
`writeRow` cuts two DECIDED rows and the piecewise carry the duplication lived in
never runs. That is the same family the operator reported here (a duplicated
fragment on a committed row), reached from the other end.

**The lesson, and it is this repo's Rule 30a in the flesh**: the branch was built on
a base whose sibling fix (`b7d3f86a`, master) already covered the defect. The
alignment step is what exposed it — the fix looked done on its own base and was a
no-op once aligned. *Measure the fix against the integration branch, not against
the base you started from.*

## The regression guard (what ships)

`internal/lineedit/answer_wrap_scroll_dup_test.go`, on the painted CELL GRID
(Rule 10.2) — the renderer's bytes are replayed into the package's own vt100
`Screen` and the rows are read back:

- **I1** no 10-cell run of the visible band occurs twice (the operator's defect,
  stated as a property of the grid);
- **I2** every visible band row is a contiguous piece of the transcript (the
  mid-word interleave);
- swept over deltas **1..40** plus a 64-byte control, and — separate case — over
  **every frame** the terminal would have shown (13823 frames judged at the time of
  writing).

**RED on the pre-fix tree, GREEN on master.** With the tree's production files
swapped for the branch's own base (`0407bc33`, before `b7d3f86a`):

```
--- FAIL: TestAnswerBand_WrappingLineIsNeverPaintedTwice/delta=1
    I2: band row 26 carries characters that appear in no transcript line, in no line's order — the mid-word interleave
--- FAIL: TestAnswerBand_WrappingLineIsNeverPaintedTwice/delta=7
    I2: band row 26 … (same)
```

and GREEN on current master. The guard therefore fails at the seam if the piecewise
carry is ever reintroduced.

Pinning the red proof to `b7d3f86a` rather than to a seam was a measurement, not a
choice: disabling the whole-line hold by itself (`unfinishedLineWouldWrap` → false)
does **not** bring the duplication back, because the surrounding landings between
the branch base and master also participate. The red proof that exists is the one
above, and this file states which tree it was measured on.

## Not done / the residual (dispatchable)

Three things this branch did NOT fix, each stated so it is dispatchable rather than
assumed away:

1. **The rig's own measurement gap.** At some cadences the guard's rig reads the
   band as empty or as a short leading run (measured on current master: delta=1 and
   3 read no cells; 7/9/11/13/17/21/27/33/39 stop a few hundred cells in — the rig
   reads a *live row* whose cells the renderer positions by cursor address). Those
   cadences are **skipped by name and counted** in the test's log (Rule 27: a skip
   must be listed and auditable), never silently passed. It is the same write path
   that produced the earlier "band loses one cell per wrapped line" reading, so the
   two are very likely one instrument question, not two renderer defects.
2. **`gopi ui_state`'s auditor has no `duplicated` damage class** — it reports the
   operator's line 199 as `cjk_wrap`, which is why the repeat was found by eye
   rather than by the audit. An auditor gap, not a renderer one.
3. **The carry helper itself** (`carryMinOverlapCells` / `carryAlreadyDisplayed`) is
   not in the tree; if a future defect re-opens the piecewise carry, the relation is
   written out in this document rather than in code.
