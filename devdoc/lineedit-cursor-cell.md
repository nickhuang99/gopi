<!-- doc-class: historical -->
# Task Report: U3 — the cursor cell layoutText reports is inside the row it names

## Completed

- **Diagnosed the defect from the briefing by measurement, not by assertion**
  (Rule 10.4): re-measured the study's table through the scripted editor
  (`newScriptedEditor` + `editingScreen`) at widths 1,2,39,40,41,79,80,81,199,200
  with 100 unbroken runes.
- **Corrected the briefing's premise, with the data.** The study's `cols=200 →
  (0,104)` reproduces on `~/work/GoPi` @ `e3195ca6` **only when the column is
  printed rather than the screen cell**: at cols=200 the buffer does not wrap
  (5 prompt cells + 100 runes = 105 < 200), so row 0 with col 104 is the CORRECT
  pair and nothing is under-reported. `layoutText` was already wrapping correctly
  (100 chars at 40 cols → 3 rows (2,25); at 80 → 2 rows (1,25)).
- **Found the real break in the invariant the briefing DID name** (`0 <= cursorCol
  < width`): a row that fills every column was reported at `cursorCol == width`.
  Twenty-four such positions in a 1440-case sweep (widths 2..200 × buffer lengths
  1..120 × cursor at the end), e.g.
  - `layoutText("you> ", 35×"a", 35, 40)` → `(row=0, col=40)` on a 40-cell row;
  - `layoutText("you> ", 100×"a", 100, 40)` → `(row=2, col=39)` on a 39-cell row;
  - width 1/2 with CJK: every position.
- **Fixed it at the source** (`internal/lineedit/render.go`): `layoutText` is now
  the composition of
  - `layoutRows(prompt, text, cursor, width)` — the wrap, unchanged, which
    additionally reports the cells that precede the cursor **on its own row**
    (the loop already knows them; no substring is re-measured); and
  - `cursorCell(row, prefix, width)` — the cursor's cell in that row: the prefix
    width mid-row, and **the row's last cell when the row is full** — the cell
    that holds the terminal's pending wrap.
- **Fixed the same wrap-arming in `Redraw`'s clipped-cursor path**: the cursor
  kept on the last drawn row of a clipped editing line was addressed with
  `displayWidth(row)`, which is `width` exactly for a full row. It now goes
  through the same `cursorCell` rule (one definition, Rule 14).
- **Gates (Rule 10.2 render truth, not the return value alone)**, new files:
  - `internal/lineedit/layout_cursor_test.go` — table-driven over widths
    {1,2,39,40,41,79,80,81,199,200} × cursor positions {0,1,width-1,width,width+1,
    2·width±1,len(text)} asserting `0 <= cursorRow < len(rows)` and
    `0 <= cursorCol < width`, each reported cell replayed through `Screen`
    (`CUP → row text → CR → CUF col` must land on exactly that cell); the CJK
    variant over 40 wide runes (no row may exceed the width, no wide rune may be
    split); the minimal regression `TestLayoutTextFullRowCursorHoldsThePendingWrapCell`;
    the cursor-ROW boundary table; an exhaustive `cursorCol < width` sweep; and the
    newline-resets-the-column case.
  - `internal/lineedit/cursor_screen_test.go` — the same table through a **real
    editor**: a scripted redraw's `Screen.CurRow/CurCol` must equal
    `layoutText`'s `(cursorRow, cursorCol)`, the screen cursor must never sit
    outside its row, and `TestEditorScreenRepaintsStayOnTheirRows` pins three
    consecutive repaints at a full row (the screen's state, not a callback count).

## Not Done

- **The "behavioural" half of my first diagnosis is not claimed.** I first wrote
  that the armed wrap made repaints walk the editing line down a row. Measured on
  master, it does NOT: `TestEditorScreenRepaintsStayOnTheirRows` (typed = width−5,
  then two more keystrokes) **passes** on master, because the next repaint's
  absolute `CUP` re-anchors every row (only the region path prints the full-body
  CUP; the full-screen path's first `CSI row;1H` also re-anchors). The armed-wrap
  column is therefore fixed as the **latent** correctness bug the briefing named
  (the invariant), and the walk is documented as latent — one bare `LF` from it,
  not as a symptom observed in this harness. The render-truth assertion
  (`Screen.CurCol`) is what makes the report honest here: it disagreed with
  `layoutText` on master and agrees now.
- **U1 (`editorRowsCap` derived from terminal rows) is NOT in this branch.**
  It lives on `feat/u1-editor-rows-derived` (tip `882cb1ec`, not on master) and
  touches `region.go`/`editor.go` only — **no diff in `render.go`** (verified:
  `git diff master...feat/u1-editor-rows-derived -- internal/lineedit/render.go`
  is empty), so this fix is orthogonal to U1 rather than superseded by it. The
  clipping interaction is covered from this side: `Redraw`'s clip path now uses
  `cursorCell`, so a U1 rollout inherits the pending-wrap rule through the shared
  helper.
- **Not fixed, deliberately**: at `width == 1` a 2-cell rune cannot fit, so a CJK
  row is 1 cell wider than the terminal at that width. Splitting the rune would be
  the worse outcome, so the test pins only the cursor invariant at width < 2 and
  documents the bound.
- The pre-existing `cursorCol == width` case for `width <= 0` (the
  `defaultEditorCols` fallback, hit by a 1-column terminal) is a consequence of
  `max(width-1, 0)` on a 1-cell screen: the only addressable cell is 0, which the
  brief's strict `cursorCol < width` excludes. It is left as is — the region
  layout refuses a sub-minimum terminal loudly (`TestA5B_TooSmallTerminalRefusesTheLayout`),
  so no live path draws an editing line at width 1.

## Verification

- Build: `CGO_ENABLED=0 go build ./...` → exit 0; `go vet ./internal/lineedit/` → exit 0;
  `gofmt -l internal/lineedit/` → no output.
- Tests:
  - `go test ./internal/lineedit/` → `ok github.com/nick/gopi/internal/lineedit 15.123s`
    (whole package, no skip).
  - `go test ./internal/lineedit/ -run 'Layout|Cursor|Render|Screen' -v` → all PASS
    (including the pre-existing `TestLineEditorRenderWraps`,
    `TestLineEditorRenderMultiline`, `TestLineEditorRenderUnicodeCursorWidth`,
    `TestA5B_TooSmallTerminalRefusesTheLayout`).
  - `go test ./cmd/agent/ -run 'LineEdit|ChatStream|EditingLine|Render'` →
    `ok github.com/nick/gopi/cmd/agent 17.017s`.
- Red-before-fix (Rule 5), master's `render.go` restored over the fix, new tests only:
  ```
  $ grep -E '^--- (FAIL|PASS)' <red run>
  --- FAIL: TestLayoutTextCursorStaysInsideItsRow (0.00s)
  --- FAIL: TestLayoutTextCursorStaysInsideItsRowCJK (0.00s)
  --- FAIL: TestLayoutTextFullRowCursorHoldsThePendingWrapCell (0.00s)
  --- FAIL: TestLayoutTextCursorRowMatchesTheRowTheCursorTextIsOn (0.00s)
  --- FAIL: TestLayoutTextNoCursorReportsTheWrapArmedCell (0.00s)
  --- PASS: TestLayoutTextNewlineResetsTheCursorColumn (0.00s)
  --- FAIL: TestEditorScreenCursorAgreesWithLayoutText (0.01s)
  --- PASS: TestEditorScreenRepaintsStayOnTheirRows (0.00s)
  --- PASS: TestEditorScreenCursorNeverSitsPastTheRow (0.00s)
  ```
  The load-bearing red line, quoted verbatim:
  `layout_cursor_test.go:35: width=1 cursor=0: cursorCol 1 outside [0,1) — reported (row=4, col=1) over 105 rows`
  `cursor_screen_test.go:75: width=40 typed=35: screen cursor=(0,39), layoutText says (row=0,col=40) over 1 rows`
  and with the fix in place the same selectors print `ok` / all PASS.
- Commit: on branch `fix/lineedit-wide-cursor-row` (worktree
  `/home/user/workspace/gopi-u3-cursor`, based on master `92d14684`).

## Evidence (self-filed)

- `go test ./internal/lineedit/` → `ok github.com/nick/gopi/internal/lineedit 15.123s` (exit 0).
- `CGO_ENABLED=0 go build ./...` → exit 0 (no output).
- `gofmt -l internal/lineedit/` → empty output, exit 0.
- `go test ./cmd/agent/ -run 'LineEdit|ChatStream|EditingLine|Render'` → `ok github.com/nick/gopi/cmd/agent 17.017s`.
- Red run (master `render.go` + new tests): `exit=1`, the five FAILs above; the
  quoted `cursorCol 1 outside [0,1)` / `(row=0,col=40)` lines are that run's output.
- Layout probe over the table (fixed tree, `TestZZProbeU3*` scratch probe, deleted
  after use): `width=40 cur=40 -> rows=3 cursor=(1,5)`; `width=2 cur=1 ->
  rows=53 cursor=(2,0)`; `width=200 cur=100 -> rows=1 cursor=(0,105)` — no position
  reports `col == width`.
- 1440-case editor sweep (scratch probe, deleted after use): `sweep: 1440 cases, 0
  mismatches`, and `0 positions report cursorCol == width` at every width 2..200.
