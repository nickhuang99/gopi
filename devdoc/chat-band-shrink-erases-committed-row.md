<!-- doc-class: historical -->
# Devdoc: the band's shrink erased a committed answer row

**Measured 2026-09-26, session `01a0dd28` (defect) / `01a0dd54` (fix).**

## The symptom

A `gopi chat` run at 117×26 streamed an answer (`自观测闭环：中文与 latin 混排的一行，
dump 必须逐字符还原。`). The settled `chat-screen.dump` did **not** hold it, while a
mid-delta frame did. `gopi ui-state compare` then reported, on the SAME input and
the SAME binary, either `prefix=32` or `prefix=0` depending on the run:

```
mismatch grid_row=-1 in_grid=false in_transcript=true geometry=layout cols=117 rows=26
  prefix=0 reason="the transcript has this line but the grid does not (longest verbatim
  prefix on the grid: 0 runes): the paint lost it, re-wrapped it, or it has scrolled out
  of the visible grid"
```

`prefix=0` on a line that WAS painted is a **fabricated corruption report**: an agent
reading the artifact cannot tell "never painted" from "painted and then overwritten".

## The cause (measured, not reasoned)

The pty stream was captured (`settled/pty-raw.bin`) and the artifacts frozen per
frame (`settled/frames/*.txt`). The transition is exact, at region-sequence byte
offset 5166:

| frame | scroll region | answer row | `you>` row |
|---|---|---|---|
| 001–005 | `top=0 bottom=25` | 23 | 24 |
| 006 | `top=0 bottom=22` | **—** | 23 |

The bytes: `ESC[1;24r` (byte 365) → `ESC[1;23r` (byte 5166) → `ESC[24;1H ESC[2K
you>`.

Mechanism, in production code:

1. The answer is painted on the band's **live row** (= the region's bottom) and
   promoted out of the region by one LF (`region.writeBandLine` /
   `AnswerRenderer.finalizeLiveRow`). Committed row = 23.
2. The user's submitted message **wraps**, so `Editor.submit()` calls
   `region.SetEditorRows(2)` (`editor.go:1342`), and `region.layout()`
   re-installs the scroll region **one row shorter** — `[1;23r`.
3. The band gives the row back from **below**: `BandBottom` 23 → 22, so the live
   row is now 22 and the row 23 the answer was committed on is **outside the
   band**.
4. The editing line draws on the band's bottom row — `you>` is painted at row 23
   with `ESC[2K` first. That erase lands on the committed answer, which no longer
   has a row reserved for it.
5. The settled dump (read at the end) therefore has no answer; a frame frozen
   before step 2 still does.

The renderer caches its live row (`AnswerRenderer.liveRow`, set at
`finalizeLiveRow`), so it too keeps writing to the stale row after the shrink.

## The fix

`region.layout()` now **carries the band's rows** when the band shrinks, before
the new (shorter) scroll region is installed — `internal/lineedit/region.go`:

```go
if lost := was.BandBottom - p.BandBottom; lost > 0 {
        r.place(was.BandBottom, 0)
        for i := 0; i < lost; i++ {
                r.emit(escNewline)
        }
}
r.emitTo(sgr.ScrollRegionSeq(p.BandTop+1, p.BandBottom+1))
```

The carry uses the terminal's OWN promotion path: one LF at the band's live row
scrolls the whole band up by one row, so every committed row keeps its text —
one row higher — and the oldest leaves through the **top** into scrollback
(preserved, never discarded). The rows the band gives up are left blank for the
editing line. This is deliberately the same mechanism the layout already uses for
every committed answer line, not a new protocol.

**Why not "make the artifact say it was overwritten"?** The epic allowed an
artifact-level honesty change as the fallback. It is not needed: the row was
being destroyed, not legitimately re-folded, so the honest fix is to stop
destroying it. `Lines`-vs-`Grid`, `refused` and `AttrsRecorded` guards are
untouched.

## The regression

`internal/lineedit/answer_live_row_shrink_test.go` —
`TestAnswer_ShrinkingTheBandDoesNotEraseACommittedRow`. Deterministic, no pty, no
timing: one turn (Begin → Delta → Finish) commits the answer, `SetEditorRows(2)`
performs the production shrink, the editing line repaints, and the answer must
still be on the screen. It asserts the invariant (the row survives and the live
row is inside the band), **not** the row number — the carry legitimately moves
the text one row up.

RED with the fix reverted (`git show HEAD:internal/lineedit/region.go` restored
in place), GREEN with it:

```
--- FAIL: TestAnswer_ShrinkingTheBandDoesNotEraseACommittedRow (0.00s)
    the committed answer row is GONE from the settled screen: it was on row 22 …
ok  	github.com/nick/gopi/internal/lineedit	1.888s
```

## Reproducing the real surface

`run_loop.py` (in session `01a0dd28`'s evidence dir) drives a real `gopi chat` on
a pty of a known size, freezes per-frame dumps, and runs the real
`gopi ui-state compare`. With the fixed binary:

```
#gopi-ui-state-compare surface=chat state=exited … match=true
  match grid_row=16 in_grid=true in_transcript=true geometry=layout cols=117 rows=26 prefix=0
```

The unfixed binary on the same harness gives `match=false` with the `prefix=0`
reason quoted above.

## Field note for future agents

`prefix=0` from `ui-state compare` is reported as "the paint lost it, re-wrapped
it, or it has scrolled out of the visible grid" — three very different causes
under one message. Before believing the "lost it" reading, **freeze per-frame
dumps** and find the frame boundary where the row disappears; a row that is
present in one frame and absent in the next was overwritten, and the geometry
(not the wrapping) is usually the suspect. The dump sink's `Screen` is a
faithful, chunk-invariant interpreter (verified: byte-at-a-time, 64-, 512-,
4096-byte and single-chunk feeds agree), so the artifact is not where rows go
missing — the paint stream is.
