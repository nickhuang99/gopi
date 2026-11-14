<!-- doc-class: normative -->
# U1 — the editing line's row budget is the terminal's, not a constant 4

**Run:** task U1, branch `feat/u1-editor-rows-derived`, base `92d14684` (master).
**Source of the finding:** `devdoc/cli-ui-feasibility-study.md` §1.1 / §5.1 (U1).
**Operator report it closes:** *"my typed text is wrapped without displaying in
multi-line… I can't read my own typing."*

## The defect

`internal/lineedit`'s repaint path clipped the editing line to an **absolute**
constant:

```go
// internal/lineedit/region.go (before)
editorRowsCap = 4

// internal/lineedit/editor.go:745-752 (before)
rows := e.val.RowsFor(e.render.prompt, e.width())
if rows > editorRowsCap { rows = editorRowsCap }
e.region.SetEditorRows(rows)
```

The buffer held the whole line and the cursor advanced past the clip; only the
first 4 wrapped rows were ever painted. The cap was lifted only on **submit**
(`editor.go:976`, whose own comment acknowledged *"the display cap above clips
long input"*) — so a multi-line message became readable only after it was sent.

The study's measured giveaway: the painted row count did **not vary with the
terminal height** (`cols=40` → 3 nonblank rows and cursor `(2,24)` at 6, 10 and
24 rows). An absolute cap is not a layout budget.

## The fix

The row budget is **derived from the terminal height** and defined **once**:

```go
// internal/lineedit/region.go
// editorRowsBudget — THE definition of the editing line's row budget:
// every row above the status line, less the band's own floor, clamped to the
// cosmetic ceiling (editorRowsCap, now a ceiling rather than the budget).
func editorRowsBudget(rows int) int {
    if rows < rowsMinUsable { return minEditorRows }
    budget := rows - statusRows - regionRowsMin
    if budget > editorRowsCap { budget = editorRowsCap }
    if budget < minEditorRows { budget = minEditorRows }
    return budget
}
```

Three call sites, all resolving to that one function:

1. **`computePlan`** (region.go) bounds `editingRows` by `editorRowsBudget(rows)`
   instead of the inline `free - regionRowsMin`, and still resolves the **menu**
   against that number and gives the **band the residual** — so the yield order
   landed by `fix/chat-editing-line-reserved` (band yields first, editing line is
   never sacrificed) is unchanged.
2. **`(*region).EditorRowsCap()`** publishes the number over the region's own
   height, so a caller never re-derives it (Rule 14).
3. **`editor.go`'s repaint clip site** asks the region: `min(RowsFor, region.EditorRowsCap())`.
   The editor therefore publishes the buffer's REAL height (never fewer rows than
   the truth) and lets the plan clip.

`editorRowsCap` is **still a constant** — but its role changed from *the budget*
to a *cosmetic ceiling* (`editorRowsCap = 12`): the terminal's own arithmetic is
what bounds the editor now, and the ceiling only stops a very tall terminal from
handing the whole screen to the editing line. Its doc comment states that.

### What was deliberately NOT changed

The renderer's clip window (`render.go` `Redraw` → `EditingRows()`) keeps the
**head** of the buffer and pins the cursor to the last drawn row. I implemented a
cursor-following tail window and **reverted it**: it broke the deliberate
contract pinned by `TestRegionYield_AWrappedEditingLineCannotOverwriteTheStatusRow`
(the editing line's head — the prompt — stays visible; that call was made by the
previous task and its reasons still hold). The reachable defect was the *budget*,
not the window direction: with a height-derived budget the window only clips on
terminals that genuinely cannot host the input.

## Verification

* **Build:** `CGO_ENABLED=0 go build ./...` → OK; `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → OK; `go vet ./internal/lineedit/ ./cmd/agent/` → clean; `gofmt -l internal/lineedit/` → empty.
* **Tests (new):** `CGO_ENABLED=0 go test -count=1 ./internal/lineedit/ -run TestEditorRows -v` → 4/4 PASS.
* **Tests (whole package):** `CGO_ENABLED=0 go test -count=1 ./internal/lineedit/` → ok (6.5s).
* **Acceptance #3 (13 render tests):** `go test -count=1 ./internal/lineedit/ -run 'Render|Layout|Screen' -v` → 13/13 `--- PASS`, 0 FAIL.
* **cmd/agent render guards:** `CGO_ENABLED=0 go test -count=1 ./cmd/agent/ -run 'TestChat|TestLineedit|TestHeight|TestChatEditingLine|TestCompletion'` → ok (2.4s) — includes `TestChatEditingLine_AWrappedInputAtTheLayoutFloorKeepsTheStatusRow`.
* **RED PROOF (Rule 5):** with the production change reverted (`git checkout -- region.go editor.go`, `editorRowsCap = 4` restored) and the new tests present, three of them fail on **assertions** (not a build error), naming the defect exactly:
  - `editor_rows_test.go:73: the layout gave the editing line 4 rows on a 24-row terminal, want the 5 rows the buffer occupies`
  - `editor_rows_test.go:144: a 40-row terminal drew only 4 editing rows — the old absolute cap of 4 is still in force (heights=[4 6 8 10 12 24 40] rows=[1 3 4 4 4 4 4])`
  - `editor_rows_test.go:245: computePlan(8, wish=14) reserved 5 editing rows, want the derived budget 4`
  Restoring the fix → all green.
* **Not caused by this diff:** `./internal/standards/` fails `TestDocClassBannerMatchesManifest` on `devdoc/gopi-statistics-20260920.md` — reproduced on a clean `~/work/GoPi` master tree at `92d14684` (commit `8ce29508`), a pre-existing master defect unrelated to lineedit.

## Acceptance item by item

| # | Acceptance | Result |
|---|---|---|
| 1 | 40 cols × 24 rows, all rows of the wrapped input present, `screen.Line(r)` equality | `TestEditorRows_ATallTerminalShowsEveryRowOfTheWrappedInput` — per-row equality against `layoutText`'s own rows, plus a named assertion on the last row (the one the old cap dropped) |
| 2 | 40 cols × 6 rows: graceful degradation, band keeps `regionRowsMin` | `TestEditorRows_AShortTerminalDegradesGracefullyAndKeepsTheBandFloor` — band rows == `regionRowsMin`, band anchored at row 0, editing span strictly above the status row, cursor inside that span |
| 3 | 13 existing render tests green | 13/13 PASS (command above) |
| 4 | Repro test fails before, passes after | Red proof above (3 failing assertions on the old tree) |

Plus two tests the acceptance does not require but the property needs:
`TestEditorRows_TheBudgetGrowsWithTheTerminalHeight` (monotonic in the terminal
height, and the extremes differ — the exact property the study measured as
absent) and `TestEditorRows_TheBudgetIsTheTerminalsAndNotAConstant` (the
arithmetic against the ONE definition, plus `computePlan` reserving exactly that
number and always leaving the band its floor).

## Files

| File | Change |
|---|---|
| `internal/lineedit/region.go` | `editorRowsBudget` (new, the one definition); `(*region).EditorRowsCap()` (new); `computePlan` bounds `editingRows` by it; `editorRowsCap` 4 → 12 and re-documented as a ceiling; `computePlan`'s doc comment updated |
| `internal/lineedit/editor.go` | the repaint clip site asks `e.region.EditorRowsCap()` instead of the constant |
| `internal/lineedit/editor_rows_test.go` | NEW — the repro + budget-property tests (screen-truth) |
| `internal/lineedit/answer_test.go` | `TestA5B_RegionClipKeepsTheEditingLineVisible` now asserts against `editorRowsBudget(a5Rows)` instead of the constant |
| `internal/lineedit/region_yield_test.go` | the two assertions that keyed "clipped" off the absolute cap now key off the floor terminal's derived budget |

## Not done (honest)

* **The cursor-following tail window** was written, tested and reverted (see
  above) — on a terminal below the budget the window keeps the buffer's HEAD, so
  a cursor parked at the end of a very long line on a 6-row terminal is not
  drawn. That is the pre-existing contract, deliberately preserved; changing it is
  a separate decision with its own tests (it trades prompt visibility for cursor
  visibility).
* **`editorRowsCap = 12` is a judgement, not a measurement.** It only matters on
  terminals taller than ~15 rows; the sensible value may be "no ceiling at all"
  (the band's floor is already reserved, so the editing line can never starve
  anything). Left as a named constant with its rationale, easy to re-decide.
* **No live pty run** against the built binary. The defect and the fix are both
  reproducible in-process at the screen seam (the harness replays the editor's own
  bytes into the vt100 grid), and memory #4053 records that this layout family has
  a live-only failure mode the in-process harness cannot see — so the in-process
  green is not offered as proof about a real terminal.
