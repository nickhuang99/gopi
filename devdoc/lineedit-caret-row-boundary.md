<!-- doc-class: historical -->
# Task Report: the editing line's caret row at the row-budget boundary

Epic `01a11811-265f-7800-84f6-69f8f6d51d95` · branch `task/01a11811265f780084f669f8f6d51d95`

## Completed

**The defect, and it was TWO mechanisms under one symptom.** When the input outgrew
the editing line's row budget, the terminal's caret was drawn on the wrong screen
row. It reproduced on the REAL binary (a pty at 40×24, a real bracketed paste) and
it needed BOTH fixes below — the first alone left the reported symptom intact.

1. **`internal/lineedit/window.go` — a DRAWN index published as a TEXT index.**
   `renderWindow` inserts the lead marker (`… N lines above`) at drawn row 0 and
   then computed the caret's drawn row as `cur - top`, a TEXT-row index. With a
   marker on screen those two numbers differ by one, so the caret was placed one
   row too HIGH — on the buffer row above the user's own last row — and the column
   was then measured off that wrong row as well (`cursorCell(drawn[row], …)`). The
   release-test's own `if row == 0 && leadMarker { row = 1 }` shows the author saw
   the off-by-one and patched ONE value of `row` instead of correcting the index.
   Fix: `row := cur - top; if leadMarker { row++ }`.

2. **`internal/lineedit/render.go` — the erase loop ran past the layout's span.**
   `total = max(len(rows), r.drawnRows)` exists so a row a previously LONGER line
   left behind is erased. In the full-screen path those rows are the renderer's own
   (nobody else draws there); under the reserved layout they are NOT — the editing
   line owns exactly `[EditorTop, EditorTop+EditorRows)` and the rows it GAVE UP
   are already cleared by `region.layout`'s own band-expansion carry
   (`clearRowsSeq`), which is where that contract lives (one owner per row).
   Running past the span placed the cursor below the screen too; placements clamp,
   so every withdrawn row landed on the LAST screen row and the walk-up back to the
   caret then started from a row it never reached. Measured: 15 rows → 13 (windowed,
   12 drawn) → 6 left the caret on the editing line's FIRST row (grid row 17, 22
   due). Fix: clamp `total` to `reg.EditingRows()` when a region is attached.

**New regression test** — `internal/lineedit/window_caret_row_boundary_test.go`, four
cases, all reading render truth from the vt100 cell grid (never `Renderer.cursorRows`,
which is the number the renderer believes it wrote):

- `TestWindowCaretRow_AtTheRowBudgetBoundary` — 12/13/14/15 rows of input, the last
  height at which the whole line fits and the first three at which the window draws
  a marked row (the operator's boundary).
- `TestWindowCaretRow_ShrinkBackAcrossTheBoundary` — 15 → 13 (windowed, the window
  MOVES) then 13 → 6 (the literal `drawnRows > len(rows)`: the window is gone and
  the six withdrawn rows must be ERASED, not left painted).
- `TestWindowCaretRow_UnderARealBracketedPaste` — the same assertion driven through
  the editor's OWN read loop (`NewChatEditor` + `EnableRegion`, a `scriptedSource`
  with its height set), so the input arrives as decoded paste bytes rather than a
  direct `SetText`.

**The epic's hypothesis is FALSIFIED and that is a deliverable, not a footnote.** The
briefing suspected `total = max(len(rows), r.drawnRows)` inflating the drawn-row
count in `render.go`. Measured: the defect reproduces with `drawnRows == 0` (a fresh
renderer: `total == len(rows)`, no erase phase involved). `render.go`'s formula IS a
real bug — mechanism 2 above — but it is the SECOND one; fixing it alone leaves the
reported symptom, which is what the red run before mechanism 1 showed.

## Not Done

- Nothing here for the **full-screen** path (`reg == nil`): it has no layout to
  stay inside, and `total = max(...)` remains its correct arithmetic. No test was
  added for it (there is no reported defect there and I did not invent one).
- The **`↑`/`↓` one-row keys at the boundary** were not driven. The epic mentions the
  key row appearing, not a defect in the keys; the caret's row after a scroll was
  left to `window_test.go`'s existing scroll coverage.
- `row == 0 && leadMarker` appeared only in the branch I replaced, so no other call
  site needed the same correction — but I checked by reading `window.go` in full
  rather than assuming: `cursorRow` is set in exactly two places (the `reg != nil`
  path and the full-screen path at line ~128), and only the first carries a lead
  marker.

## Verification

- **Build**: `go build ./...` → exit 0.
- **Tests**: `go test ./internal/lineedit/ -count=1` → `ok github.com/nick/gopi/internal/lineedit 42.462s`
  (whole package, no skips). The new file alone:
  `go test ./internal/lineedit/ -run 'TestWindowCaretRow' -count=1 -v` → all 7
  subtests PASS (4 + 1 + 1 `AtTheRowBudgetBoundary` subtests, plus the shrink and
  paste cases) in 0.196s.
- **Ran** (the product surface, Rule 31 — a pty, the REAL binary, a real bracketed
  paste of 16/20 rows, the row read back out of the dump the run itself writes):

  ```
  CGO_ENABLED=0 go build -o /tmp/gopi-caret ./cmd/agent/          # branch = fixed
  cd /home/user/workspace/gopi-caretbase-head && CGO_ENABLED=0 go build -o /tmp/gopi-caret-base ./cmd/agent/   # HEAD = pre-fix
  python3 <session>/artifacts/caret_pty_probe.py /tmp/gopi-caret-base /tmp/cb.dump 16 6
  python3 <session>/artifacts/caret_pty_probe.py /tmp/gopi-caret      /tmp/f16.dump 16 6
  ```

- **Result** — the sweep across the boundary, cursor row vs the row carrying the
  pasted last line, both binaries on the same 40×24 pty:

  | pasted rows | pre-fix (HEAD) | fixed (branch) |
  |---|---|---|
  | 12–17 | cursor IS on the last input row | cursor IS on the last input row |
  | **18** | **NOT** — caret row 20, input's last row 21 | **IS** — caret row 21 |
  | **19** | **NOT** — caret row 20, input's last row 21 | **IS** — caret row 21 |
  | **20** | **NOT** — caret row 20, input's last row 21 | **IS** — caret row 21 |
  | 25 | (not sampled pre-fix) | **IS** — caret row 21 |

  The fixed run at n=20, read off its own dump — the marked row
  (`… 5 lines above`), the input, the key row and the caret all in their places:

  ```
  11| … 5 lines above|
  12|L11|  …  20|L19|
  > 21|L20|
  22| … PgUp/PgDn scroll · Alt+↑/↓ one row|
  23|deepseek-flash ·  iter0    0.1s · ready|
  ```

  (Pre-fix, the same case put the caret on row 20 — L19 — while L20 sat on row 21,
  with the key row below it. That is the operator's 输入光标跳一行, reproduced and
  then closed.)

- **Commit**: see `git log -1` on the branch (the commit carrying this document).

## Evidence (self-filed)

- `go test ./internal/lineedit/ -run 'TestWindowCaretRow' -count=1 -v` **before** the
  fix: `--- FAIL` on the boundary case with
  `the cursor is on grid row 18, which carries "L11" — the caret's own row is "L13"
  (buffer row 12 of 13)` plus the full 24-row grid dump; and on the paste case with
  `the cursor is on grid row 22 while the editing line's last row of input is grid
  row 23`. Both are the reported one-row jump, read off the grid.
- Falsification of the epic's hypothesis: a scratch probe drove one redraw on a fresh
  harness and printed `drawnRows before the redraw = 0 … total = max(12,0)` — i.e.
  the defect's shape is present with no erase phase at all. (Scratch file deleted;
  the result is in this document.)
- Mechanism 2's observation: with only the `window.go` fix applied, the shrink case
  failed with `after shrinking 13 rows of input to 6 (drawn 6 rows, was 12) the
  cursor is on grid row 17, want the last row of the new drawing 22` — the caret on
  the editing line's FIRST row, which is what sent me to `render.go`'s `total`.
- Real-binary verdicts are the `## Verification` table; the probe's trajectory line
  (`cursor row → 22, 22, 22, …`) is the same value sampled 20+ times over the settle
  window, so each verdict is a state rather than a timing accident.
- `git status --porcelain` on the branch: only the two fixed files and the new test
  are listed (checked before the commit; after it, empty).
