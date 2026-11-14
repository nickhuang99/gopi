<!-- doc-class: historical -->
# The Editing Line Is Never Sacrificed — compositor yield order + a one-row status

Task: `gopi chat --tools` could push the `you>` editing line off the screen at a
small terminal height, and the status row (longer since the tool row landed) could
reach past the terminal's width and wrap onto a second screen row, taking another
editing row with it — and, in the reachable case, painting over the status row.

Branch: `fix/chat-editing-line-reserved` (worktree `/home/user/workspace/gopi-chat-editline`).

## Completed

### 1. `computePlan` — an explicit YIELD ORDER (`internal/lineedit/region.go`)

The old arithmetic placed the editing line at a FIXED row (`regionRowsMin`) and
then let the band/status arithmetic claim rows the line needed; the editing line's
row count was a leftover of that subtraction. It is replaced by a four-step
one-pass computation whose order IS the policy:

1. **Floor** — `regionRowsMin` (band) + `minEditorRows` (editing line). A new
   named constant `minEditorRows = 1`; `rowsMinUsable` is now defined as
   `regionRowsMin + minEditorRows + statusRows`, so the floor and the smallest
   hosting terminal cannot drift apart.
2. **Editing line** — takes the rows it asked for (the caller's policy; the
   repaint path passes at most `editorRowsCap`), never below its minimum, never
   so many that the band's minimum goes.
3. **Menu** — takes what is left once the editing line's NUMBER and the band's
   minimum are served (capped by `menuRowsCap`, which the region publishes).
4. **Band** — the RESIDUAL, bounded by `regionRowsMin`/`regionRowsMax`.

The band therefore yields first at every height: extra rows go to the band, a
shortage is absorbed by the band, and an active layout always hosts ≥1 editing row
**and** the full minimum band.

Below `rowsMinUsable` the plan is a degenerate placeholder that is never installed
(no scroll region is emitted); its editing line is pinned to the screen's first row
so a caller drawing through it cannot be pushed off-screen by a band that is not
there.

### 2. The renderer honours the layout's row bound (`internal/lineedit/render.go`)

`Renderer.Redraw` drew `len(layoutText(...))` rows from the editing line's first
row downwards — the layout's own published `EditorRows` was **never consulted**,
even though `region.EditingRows()` exists and its doc comment claimed the
renderer asked for it. That disagreement was the reachable half of the defect: at
the smallest hosting terminal the layout gives the editing line ONE row while the
buffer wants four, so the surplus three painted the menu row and the status row.

`Redraw` now clips `rows` to `reg.EditingRows()` and keeps the cursor on a row it
actually drew (the last drawn row), so the terminal's cursor can no longer be
placed on the menu's row or the status row.

### 3. Shrinking under the floor takes the layout DOWN (`internal/lineedit/region.go`, `editor.go`)

`region.Resize` used to leave the region ACTIVE with the old geometry when the
terminal shrank below `rowsMinUsable` — the editing line stayed placed at a row
the shrunken terminal no longer has. It now disables the layout (resetting the
scroll region) and the editor announces the degradation naming the height the
composer needs:

```
[gopi] editor: terminal is now too short for the chat layout (needs 4 rows) — full screen output
```

`RowsMinUsable()` is the single source of that number (Rule 1/13: the value is
named, exported, and *referenced* — it is what the existing startup message
already prints too). The turn renderer asks `Editor.Answer()` per turn, so the
REPL falls back to the cooked buffered renderer with no further wiring.

### 4. The status row is hard-bounded to ONE screen row (`internal/lineedit/answer.go`)

`statusLine` already dropped whole fields from the right when the row was too
narrow, so it was already total in `cols` — but the LAST resort (`fitStatusFields`
→ `truncateCols` on the first field) cut a field with **no marker**, which is the
"looks like corrupted data" failure. Now:

- every cut field ends with a new named constant `statusEllipsis = "…"`
  (`fitField`), which both branches of the last resort go through;
- the marker is reserved BEFORE the cut (`truncateCols(s, room-1) + "…"`), so
  marking never pushes the field one cell over its room — the row's
  one-screen-row guarantee is arithmetic on cell widths, not a hope.

A rendered status row therefore occupies exactly one screen row at every width,
whatever the model name and the tool digest contain.

### 5. Tests

`internal/lineedit/region_yield_test.go` (new):

- `TestRegionYield_EditingLineSurvivesEveryHeightAboveTheFloor` — sweeps EVERY
  (height, editing-row wish, menu wish) shape the layout accepts and asserts the
  invariant that matters: the editing line keeps ≥1 row entirely on screen, the
  band is above it with ≥ its minimum, and the chrome's last drawn row never
  passes the status row.
- `TestRegionYield_BandGivesRowsToTheChromeFirst` — the yield direction as a
  value: at `rowsMinUsable` the band sits at exactly its minimum; every extra row
  of a taller terminal goes to the band until its own cap.
- `TestRegionYield_MenuYieldsToTheEditingLineAndTheBand`.
- `TestRegionYield_AWrappedEditingLineCannotOverwriteTheStatusRow` — the rendered
  defect: at `rowsMinUsable` a four-row buffer must not paint the status row.
- `TestRegionYield_ShrinkBelowTheFloorTakesTheLayoutDown`.
- `TestStatusRow_IsExactlyOneScreenRowWithALongToolSummary` — long model name +
  long tool digest, measured by WRAPPING the row at the terminal width (a
  display-cell count is not a row count; a byte count is neither) at widths
  20/34/40/100 plus the degenerate width 1.

`cmd/agent/lineedit_height_test.go` (new), on the rendered screen through the
existing A5/REPL harness:

- `TestChatEditingLine_SurvivesALargeToolOutputAtSmallHeights` — heights 4/6/8/10,
  a 30-row tool output streamed through the band, a long tool digest on the status
  row: the `you> hello` line is asserted PRESENT on the rendered grid, the band is
  above it with its minimum, and the status row is the last row.
- `TestChatEditingLine_AWrappedInputAtTheLayoutFloorKeepsTheStatusRow` — the
  REACHABLE half, at the screen seam: a 4-row terminal, input wider than the
  terminal, the prompt + head of the text survive on the editing row, the status
  row still carries the counters, and the editor's cursor is on a row the layout
  gave the editing line. **Red-proved** (see Evidence).
- `TestChatStatus_StaysOneScreenRowWithALongToolSummary` — the status row's own
  half, same heights: the editing row keeps the user's text and the status row
  occupies exactly one screen row (measured by feeding the row into
  `lineedit.Screen` and counting the wrap).
- `TestChatStatus_ToolRowRegression` — the tool row (`⚙ keyword_search`) reaches
  the status row together with the counters on a 100-column terminal, and clears
  when the turn ends.
- `TestChatStatus_LongLeadingFieldIsCutWithTheEllipsis` — a 30-column terminal,
  long model name: the rendered row is `"glm-5.3-flash-long-mod…  ready"` — the
  marker at the screen seam, exactly one screen row.

## Not Done

- **The defect was NOT reproducible in the in-process harness.** I first attacked
  the startup path (a too-small terminal *at launch*) and measured that it already
  behaved: probing at heights 4/6/8/10 with the layout enabled showed the correct
  layout and a visible `you>` in every case, and diffing the old vs new
  `computePlan` over heights 1..20 × wishes 1..8 × menus 0..5 produced ZERO bad
  plans for either formula. **Qualifier (memory #4043):** the defect WAS seen live
  in a fixed-geometry pty harness driving the real `gopi chat` binary — a gap
  between my harness and the real binary that this task did not close. So the
  reproduced mechanism here is the RESIZE path below the floor plus the renderer's
  unbounded row count (both fixed), and the layout rewrite is justified by design +
  the invariant sweep rather than by a red I could drive.
- **Red proof is for the renderer bound, not for the layout arithmetic.** The
  red→green transition observed is the `EditingRows` clip: with it disabled,
  `TestRegionYield_AWrappedEditingLineCannotOverwriteTheStatusRow` fails
  (`status row 3 = "wwwww" — the editing line's surplus rows overwrote it
  (EditorRows=1)`) and `TestChatEditingLine_AWrappedInputAtTheLayoutFloorKeepsTheStatusRow`
  fails at the screen seam (`status row 3 = "dabcd…" — the wrapped editing line
  overwrote it`); both pass with it restored.
- A fix for the `cmd/agent` wide test-suite wall clock is out of scope.

## Verification

- Build: `go build ./...` → OK.
- Tests, acceptance-scoped:
  `go test ./internal/lineedit/ ./cmd/agent/ -run 'Chat|Region|Plan|Status' -count=1`
  → `ok github.com/nick/gopi/internal/lineedit`, `ok github.com/nick/gopi/cmd/agent`.
- Full packages: `go test ./internal/lineedit/ -count=1` → ok (incl. the
  pre-existing A5 tests: `RegionInstallsScrollableBand`,
  `RegionClipKeepsTheEditingLineVisible`, `NarrowStatusDropsWholeFields`, …);
  `go test ./cmd/agent/ -count=1` → ok (incl. `ReservedLayoutCommitsEachLineOnce`,
  `RepaintsStayLinearInTheAnswer`, `CookedPathKeepsTheBufferedRenderer`).
- gofmt: `gofmt -l internal/lineedit cmd/agent` → empty for the touched files.
- `git status --porcelain` in the worktree: only this change's files, all
  committed.
- Evidence per gate claim is in `tasks/<id>-report.md` (`## Evidence
  (self-filed)`); the red/green transition and the measured screens are quoted
  there.

## Files

- `internal/lineedit/region.go` — yield-order `computePlan`, `minEditorRows`,
  `Resize` drops the layout below the floor.
- `internal/lineedit/render.go` — `Redraw` honours `EditingRows()`.
- `internal/lineedit/answer.go` — `statusEllipsis`, `fitField`, total `statusLine`.
- `internal/lineedit/editor.go` — resize degradation names `RowsMinUsable()`;
  `Answer()` documented as live (per-turn).
- `internal/lineedit/region_yield_test.go`, `cmd/agent/lineedit_height_test.go` — new tests.
