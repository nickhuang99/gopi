<!-- doc-class: historical -->
# Task Report: the editing line's scrolling window — the rows it hides are stated

Task: `01a0eaca-9ad8-7255-baea-ef7f66aa62da` · branch `task/01a0eaca9ad87255baeaef7f66aa62da`

## Completed

**1. The window follows the caret (`internal/lineedit/window.go`, new).** The
renderer used to draw the input's FIRST `EditorRows` rows. Past the budget that
put the user's own caret off the window — typing moved out of sight — and the
rows below were dropped with nothing said. `renderWindow` now resolves a window
of the drawn rows: the caret's neighbourhood once the text outgrows the budget,
the head while it fits (the historic behaviour, every ordinary message).

**2. What the window does not reach is STATED on the editing line.** In the
product's own elision idiom (`cutField`'s `…`, as the status row and the request
row already cut): `… 20 lines above` above the text, `… N lines below · PgUp/PgDn
scroll · Alt+↑/↓ one row` below it. Marked rows are cut to the terminal width,
never wrapped (a wrapped marker would take a row the layout did not give it), and
they carry no buffer emphasis (`editingRowSegs` returns nil for a chrome row).

**3. A scroll mechanism, and a line that names it.** New key codes
`KeyPageUp/KeyPageDown` (CSI `5~`/`6~`) and `KeyScrollUp/KeyScrollDown` (Alt+↑/↓,
and Ctrl+P/Ctrl+N — folded onto the scroll key in the decoder and translated
there, so the history motion in `handleKey` is untouched). PageUp/PageDown page
by the window's text height less one; the arrows move one row. Up/Down also move
the window while it has hidden rows (so a keyboard with no PageUp still reaches
the text) and fall back to history recall when the whole line is on screen.

**4. The chrome is RESERVED, not borrowed (`internal/lineedit/region.go`).** The
marker row and the key row cost the layout `editorChromeRows` (2) → the editing
line's floor is `EditorRowsMin = minEditorRows + editorChromeRows` (3), and
`rowsMinUsable` derives from `EditorRowsMin` so every terminal that hosts the
layout hosts the mechanism. Before this, both rows were drawn only when a spare
row happened to be free — i.e. on a tall terminal, never when the user needed
them. The budget arithmetic itself is unchanged (`rows - statusRows -
regionRowsMin`, clamped at `editorRowsCap`); only its floor moved.

**5. The submitted message is whole.** `submit` unpins and unclips the window
before the request row is laid out, so the turn's own echo of the message is
never windowed.

**6. Tests on the rendered cell grid** (`internal/lineedit/window_test.go`, new;
plus updated `editor_rows_test.go` / `region_yield_test.go`): visible + above +
below == the input's row count; 1 line (no marker at all), 30 lines, 300 lines;
the floor terminal still shows text + marker + keys; a narrow terminal cuts
markers instead of wrapping them; the scroll keys driven through the REAL decoder
(scripted paste → PageUp → PageDown → Alt+Up) with the motion measured on the
grid. Every surface runs the real editor and reads the real grid — no test reads
the window's own opinion of what it drew.

## Not Done

- **`editorRowsCap = 12` stays.** The ceiling is legitimate (the editing line must
  not take the whole screen for itself, and the reserved layout is what the band
  and the status row live in). What was wrong was the SILENCE below it, and that
  is what this change closes. The epic's "grow with the terminal" reading is
  honoured where it is real: the derived budget (`rows - statusRows -
  regionRowsMin`) is applied, so a 40-row terminal shows 12 rows of input instead
  of a constant 4 — that part landed earlier (`devdoc/editor-rows-derived.md`).
- **The 300-line paste on a real pty is captured once only.** The pty captures of
  30 lines are clean and settled (`frame-*`, `v2-*`, `final300-paste`); the
  300-line frame settles on some runs and not others (`grab` returning at
  `dumps=1`, the boot frame), a harness timing property rather than a layout one —
  the same input is asserted at that scale in the cell-grid test. Not claimed as a
  settled real-terminal observation.
- **Five tests in `internal/lineedit` and three in `cmd/agent` fail on this
  branch — all pre-existing.** Measured on a detached worktree at this branch's own
  merge base (Rule 29's read-only comparison), identical set, twice:
  `TestEmphasisSpanAfterAWrapSpaceKeepsItsOwnCells`,
  `TestEmphasisWrapSpaceLedgerHoldsAtEveryWrapCount`,
  `TestScreenReconcilesAfterAnOverlongHold`,
  `TestScreenReconcilesAcrossAReMeasure`,
  `TestTurnHeaderLines_BlockRowsAreCommittedOnePerRow`,
  `TestChatFlow_CutNeverLandsInsideAnEscapeSequence`,
  `TestChatStream_RepaintsStayLinearInTheAnswer`,
  `TestChatTurnHeader_PaintsAMultiRowCountedHeaderOnARealPty`,
  plus `TestChatFlow_AgentOutputTakesWholeBandRows` (flaky on BOTH trees: 0/0/0
  branch vs 1/1/0 base across three runs each). Left alone deliberately — they are
  another task's defect, not this branch's.

## Verification

- **Build:** `go build ./...` → OK (exit 0); `go vet ./internal/lineedit/` → clean.
- **Tests:** `go test ./internal/lineedit/ -run TestWindow_ -count=1` → **ok** (new
  contract, 8 tests). `go test ./internal/lineedit/` → FAIL with exactly the five
  pre-existing names above. `go test ./cmd/agent/ -run 'Chat|Dump'` → FAIL with
  exactly the pre-existing set above (base tree re-run for comparison). **Red
  proof:** the same assertion written against the unfixed tree
  (`gopi-baseline-lineedit/internal/lineedit/redproof_window_test.go`, a detached
  worktree at the merge base `14836a97`) →
  `--- FAIL: TestRedProof_APastedInputStatesItsHiddenRows … the dump states
  NOTHING about the input rows the editing line does not show (silent cut)`,
  printing the old screen as `you> pasted line 1 … pasted line 9` with lines
  10–30 absent and unmentioned.
- **Ran (the real surface, a real pty of 100×24, the real binary):**
  `CGO_ENABLED=0 go build -o <evidence>/gopi ./cmd/agent/`, then
  `pty.fork()` + `ioctl(TIOCSWINSZ, 100×24)` + `execv(gopi, [gopi, "chat",
  "--dump-screen=<path>"])`, then bytes written to the pty master:
  `ESC[200~` + 30 lines joined by CR + `ESC[201~`; `ESC[5~`; `ESC[6~`; `CR`.
- **Result (read out of the dump's own cell grid):**
  - after the paste — `011|… 20 lines above`, `012|pasted line 21` … `021|pasted
    line 30`, `022|… PgUp/PgDn scroll · Alt+↑/↓ one row`; `cursor row=21 col=15`
    sits on `pasted line 30`. 20 hidden + 10 shown = 30, the exact accounting.
  - after PageUp (`ESC[5~`) — `011|… 11 lines above` … `022|… 9 lines below ·
    PgUp/PgDn scroll · Alt+↑/↓ one row`; both counts stated at once.
  - after PageDown — back to `… 20 lines above` / `pasted line 21…30`, the caret's
    own window. A key with no movement repaints nothing.
  - after Enter — the answer band repaints the message, and the session transcript
    for the 300-line paste (`~/.gopi/sessions/cab4226d-f437-4ae9-b6a8-1f93367dbf6a/
    session.jsonl`) carries all 300 rows including `row 1`, i.e. the window never
    shortened the submission.
  - before/after on the OLD binary (same pty, same bytes): `you> pasted line 1 …
    pasted line 9`, nothing stated — the defect, reproduced on the real surface.
  Evidence directory: `~/.gopi/sessions/task-01a0eaca-evidence/`
  (`frame-paste.txt`, `frame-pgup.txt`, `frame-pgdn.txt`, `frame-enter.txt`,
  `v2-*.txt`, `final-enter300.txt`, `drive*.py`).
- **Commit:** the branch commit for this work (see `git log -1` on
  `task/01a0eaca9ad87255baeaef7f66aa62da`).

## Evidence (self-filed)

- `go build ./...` → `exit=0`.
- `go test ./internal/lineedit/ -run TestWindow_ -count=1` → `ok
  github.com/nick/gopi/internal/lineedit`.
- `go test ./internal/lineedit/` (branch) and the same command in
  `~/workspace/gopi-baseline-lineedit` (base `14836a97`) → the same five FAIL
  names; red proof FAILs only on base.
- `gopi chat --dump-screen` on a 100×24 pty → the dumps quoted under Result
  (frame files in this run's session directory).
- `git status --porcelain` in the worktree → empty after the final commit.
