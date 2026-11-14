<!-- doc-class: historical -->
# The band's wrap path promoted whole rows BLANK — the cell loss, its root cause and the fix

Measured 2026-09-27/28 on `task/01a0e34d02d77065b6d62d5cd78804ca`, fixing the defect the
operator's own dump showed as "the answer band loses cells on the wrap path"
(`tasks/01a0e2f5-report.md` records the sighting: `已核实：… 9c6b8da8 自认的 Not-…` — the tail of
the sentence gone, the next line's text in its place).

## What was wrong

`AnswerRenderer.commitLines` commits one buffered line at a time. For the FIRST line of the
commit it does not write the decided row directly: the live row may already show a HEAD of
that line (a provisional paint the wrap path cut), so it asks `fitCommitRow` whether the
decided row still fits BESIDE the head.

- `ok == true` → the head is left standing and the row is written after it.
- `ok == false` → **for the first line of a commit, the row could not be drawn at all** —
  the head alone fills the row. This is not a rare corner: it happens whenever a wrap lands
  on the commit seam, i.e. on the ordinary streaming cadence of a long answer.

That branch fell through to the stale-cell erase with an EMPTY row:

```go
row, ok := fitCommitRow(row, h, line, r.cols)
if ok {
    r.writeRow(row, row.text)          // ok == false → nothing is written
}
if i == 0 {
    if w := displayWidth(rawHead); w > displayWidth(row.text) {
        r.placeAndWritePrefixed(escEraseToEnd, r.liveRow, r.liveCol, "")
    }
}
r.finalizeLiveRow()
```

With `ok == false`, `row.text` is empty, so `displayWidth(rawHead) > 0` is always true: the
erase fires from `liveCol` to the end of the row — and everything from `liveCol` on IS the
head, the only place those cells exist. `finalizeLiveRow` then promotes that row, now blank,
and the band scrolls it away holding nothing. Two effects, one cause:

1. **The row is promoted blank** — a whole line of the answer, destroyed at the exact
   moment it should have been committed. At a 1–3 cell cadence this is the WHOLE answer:
   every commit of a single line hits it, so the committed reading is empty.
2. **The head is consumed, not carried** — the cells that did not fit were cut and the
   carry-forward was the erase's victim.

## The measurement (Rule 10.2: the painted cell grid, not a string assertion)

The rig is the previous run's (`answer_wrap_scroll_dup_test.go`): the renderer's real bytes
replayed into the package's own vt100 `Screen`. What that rig needed was one more piece —
**the scrollback**. A committed answer line LEAVES the screen: the band's whole mechanism is
one scroll of the reserved region, so a reading of "the visible band rows" is a window on the
output, not the output, and a line that has scrolled away reads as *missing* when it is in
fact *delivered*. The committed reading is `Screen.Scrollback() + band rows`, in that order.

Streaming the transcript with the band's own `dupTranscript()` (3255 dense cells), committed
cells carried:

| delta | 1 | 2 | 3 | 5 | 7 | 9 | 11 | 13 | 64 |
|---|---|---|---|---|---|---|---|---|---|
| before | **0** | 3030 | **0** | 3030 | 2626 | 2245 | 2952 | 3053 | 3255 |
| after | 3255 | 3255 | 3255 | 3255 | 3255 | 3255 | 3255 | 3255 | 3255 |

(`delta=64` carried everything even before the fix — at that size no wrap lands on the commit
seam. It is the case's CONTROL: red there would mean the instrument is broken, not the
renderer.) At the rig's default cadence the pre-fix tree also shows it at FRAME level: 31
frames whose committed reading shrinks, e.g. write #65 going 136 cells → 0.

## The fix

`internal/lineedit/answer.go`, `commitLines`: when the first line of a commit does not fit
beside the live row's head, the head IS that row's content — so promote the row as it stands
and let the line continue on the fresh row below:

```go
if !ok {
    r.finalizeLiveRow()     // the head is the row: commit it as it stands
    r.writeLive(line)       // the rest of the line goes on below
    continue
}
```

No stale-cell erase is needed on this path: the erase exists for cells a provisional paint
left PAST the committed row's width, and a row promoted as-is has no such tail.

## The regression guard

`internal/lineedit/answer_wrap_loss_test.go`:

- `TestAnswerBand_WrappedLineKeepsEveryCommittedCell` — the settled grid: the committed
  reading is the transcript's own cells, once, in order, at every cadence of the rig's sweep
  (deltas 1..40 plus the delta=64 control).
- `TestAnswerBand_WrappedLineLosesNothingInAnyFrame` — the frame level: a settled grid can be
  right while a frame the human watched was wrong (the whole point of this defect). The
  invariant is monotonicity of the committed cells — the band only ever grows an answer — plus
  the same contiguity check per frame.

RED/GREEN observed on this worktree: pristine `answer.go` → 48 of 82 subtests fail
(34 pass, all of them the control delta and the frames before the first seam); with the fix →
all pass. `go test ./internal/lineedit/ -count=1` green over the whole package.

## What the guard does NOT cover (honest)

- **The live row's own frames** are read only through `dupBandRows`, i.e. the band rows
  *below* the committed ones. The defect was in the commit path, which this reads exactly; a
  loss that stayed on the live row and was never committed would not be caught.
- **The 67 number in the previous report is explained only qualitatively.** That run's rig
  measured the visible band only, so a line that had scrolled away counted as missing — a
  reading that is itself wrong. The *defect* behind it is proven (0 of 3255 cells at delta=1,
  the same transcript and the same rig), but the exact 67 was produced by an instrument this
  worktree no longer has and is not reproduced here.
- **One cadence class is untested**: a delta that lands on the seam AND is large enough to
  carry several lines at once is exercised, but a burst whose FIRST line does not fit while a
  LATER line does is only covered by the sweep, not by a dedicated case.
