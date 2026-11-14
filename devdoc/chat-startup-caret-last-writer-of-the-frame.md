<!-- doc-class: normative -->
# The caret is the LAST writer of the frame — a band drain must hand it back

**Defect class**: a frame with more than one writer, where the one that runs last
owns the cursor. `Renderer.Redraw` placed the caret; the region's band write then
parked the cursor somewhere else, *after* it.

Measured 2026-10-08 by the operator, then reproduced here: a `gopi chat` startup on
a 100×30 pty, **no keystroke sent**, left the hardware block on **row 27 col 0**
while the editing line (`you> `) is **row 28**. The block therefore sat one row
ABOVE the input and read as "the prompt is not focused yet"; it stayed there until
the next keystroke repainted. The last bytes of the frame were `ESC[28;1H LF` —
the band's own write, not the renderer's.

## The two roads, both of which end with the band

`region.writeBandLine` finishes every row it writes by parking the cursor on the
band's **live** row. That runs after `Redraw` on both layouts:

1. **the repaint road** — `Editor.repaint()` → `Redraw` (places the caret) →
   `e.drainBandFrames()` (moves it away again);
2. **the direct-driver road** — `Editor.SettleBand()` drains with **no repaint
   after it at all**, so a missing placement there is permanent rather than
   corrected by the next frame.

Startup is exactly road 1: the banner and the side panel are committed through the
band, so the first screen a user sees is the one with the block off the input.

## The fix

- `Renderer.PlaceCaret()` (`internal/lineedit/render.go`) — ONE absolute placement
  (`cursorTo`) at `EditorTop()+cursorRows`, `cursorCol`. It returns without writing
  when `out` or `region` is nil, the region is not `Active()`, or `drawnRows <= 0`:
  the historic full-screen renderer moves RELATIVELY and has no absolute row to
  place to, so its bytes are untouched.
- `Editor.repaint()` calls it after `e.drainBandFrames()`.
- `Editor.SettleBand()` calls it when the drain wrote anything.

This completes the hand-back `internal/lineedit/menu.go` already performs after
drawing its own absolute rows — the drain was the writer that did not.

**The off-by-one the first attempt hit.** `sgr.CursorSeq` takes **1-based**
arguments (the region's own `cursorTo` does `row+1, col+1`). The first version
passed 0-based values and landed one row BELOW the editing line (measured
`row 28 col 5` → `row 29 col 5`). `PlaceCaret` routes through the package's
existing `cursorTo`, so the conversion has one definition per package.

## Verification shape, and why it is a cell grid

Both the package regression (`internal/lineedit/caret_placement_test.go`, four
cases) and the real-binary acceptance case
(`testsuite/chat_startup_cursor_pty_test.go`) judge the **rendered cell grid's**
`CurRow`/`CurCol` replayed from the editor's own paint stream — never the
renderer's belief (`cursorRows`) — and the editing line's row is **found** in the
grid, not assumed, so neither can pass by agreeing with a hard-coded layout.

- Post-fix, replayed from the captured stream: `CURSOR = row 28 col 5` (the caret
  cell right after `you> `), stable over four runs — a FIXED position, not the
  random one first reported.
- RED proof on the pre-fix binary (`f445bf7ac^`, built to a separate path so the
  worktree was never left modified): `after the startup frame the cursor is on row
  27, but the editing line is row 28 ("you>")`.

## What this fix does NOT cover

`gopi chat --dump-screen` still renders an all-blank grid with a `cursor row=0
col=0` header on a live run whose byte stream shows a fully painted screen. That is
a **separate defect on a different seam** (the dump's own feeding of its screen
model, and whether it tracks a cursor at all — `cmd/agent/chat_dump.go` tees the
paint stream through `chatDumpWriter`); it is *not* fixed by this change and was
deliberately not asserted on here, because the acceptance case would then have been
asserting on a surface that is itself broken. Filed as its own bug row
(`01a119e6-32f7-7879-b9a2-7267d9fdb4c5`).
