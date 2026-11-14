<!-- doc-class: normative -->
# The one-candidate completion menu — a one-item list that says so

**Standing claim about master**, decided 2026-09-27 (task `01a0e2e6`, worktree
`task/01a0e2e6503474dcb5a2bf14a23c2fa0`). Code: `internal/lineedit/menu.go`
(`Menu.Rows`, `Menu.noteRows`, `Menu.noteLine`, `Menu.MoveSelection`,
`Menu.MenuLines`), `internal/lineedit/menu_test.go` (the a7 harness's stub
vocabulary), `internal/lineedit/menu_single_candidate_test.go` (the regression),
`cmd/agent/slash_menu_test.go` (the REPL-seam acceptance test over the real
completer).

## The report and the root cause

The operator typed `/q` in `gopi chat`. The slash completer matched exactly one
command (`/quit`), the menu drew that sole row already marked active, and
Arrow-Down changed **nothing** on screen. `Menu.MoveSelection(1)` computed
`(Index+delta)%n` with `n == 1` — `0` — so the selection moved to the row it was
already on, the repaint drew the identical cells, and an inert key read as a
broken shortcut: *"/q is useless"*.

The defect shipped with a green suite because **every** menu test drove a
three-candidate stub (`menuStubResult`), and no prefix of that vocabulary reaches
`n == 1` on a settled line. The single-candidate case was structurally
unreachable from the harness.

## The decision, both halves

1. **A one-candidate menu is a one-ITEM list.** Up/Down do not move the selection
   — it *clamps*. `MoveSelection` now returns early when
   `n == completionSingleCandidate`; the arithmetic was already the identity, so
   this is the decision made explicit in the one place the behaviour is decided,
   not a behaviour change.
2. **The menu states the reason on screen.** A menu whose whole list is one
   candidate reserves and draws one extra row:

   ```
   you> /q
   > /quit (alias of /exit)
     nothing to move to — the only match
   ```

   The arrow press stays a no-op **by design**; it is no longer a *silent* one.

The condition is a property of the **LIST** (`len(Candidates) == 1`), never of
which key was pressed last: the row follows what is on screen, so it is gone the
moment the menu has 2+ candidates and gone entirely once the menu closes (Escape
erases it with the rest — the region erases the rows it recorded, which now
include the note row).

## Why the extra row lives inside `Menu.Rows()`

`cmd/agent` (and the editor's layout arithmetic) read the menu's reserved height
from `Menu.Rows()` — one definition, so the rows the layout reserves and the rows
painted cannot disagree. The note row is therefore counted by `Rows()`
(`n + m.noteRows()`) and emitted by `MenuLines()`, both in `menu.go`, from the
same `noteRows()` predicate. A note appended only in the paint path would have
been drawn on rows the layout never reserved: the erase-after-close would then
reach into the row below the menu (the status row), and a menu too tall for the
terminal would have been admitted one row over.

The explicit page budget (`WithPage`, used when the layout can host fewer rows
than the menu wants) is a **total** row budget, so a page of 1 draws the note
alone — the reason rather than half a row.

## Wording

`noteLine` puts the **reason first**: `nothing to move to — the only match`. On a
narrow terminal `truncateCols` eats the tail, and the tail is the reassurance, not
the answer to the key press. `menuNoteMoveInert` and `menuNoteWhy` are named
constants (Rule 1), and `lineedit.CompletionMenuNote()` /
`lineedit.CompletionNoteRow()` export the reason and the row count so
`cmd/agent`'s acceptance test asserts the drawn row by constant instead of a
copy of the wording (Rules 13/14).

The note row carries **no selection marker**: it can never be mistaken for a
second offer, and Tab/Enter can never complete to it.

## Verification (rendered cells, not state)

- `internal/lineedit/menu_single_candidate_test.go` — the a7 harness with a
  **one-entry** stub vocabulary: the sole row keeps the marker across Down and
  Up, the note row is present before and after each press, `Menu.Rows()` equals
  candidates + note, the note leads with the reason, the row is truncated at the
  terminal width, a 3-candidate menu draws no note and still **wraps** (Up from
  row 0 → the last row), narrowing back to one candidate brings the note back,
  and Escape erases it.
- `cmd/agent/slash_menu_test.go::TestSlashMenuOneCandidateStatesWhyTheArrowCannotMove`
  — the REAL `NewSlashCompleter()` with `"/q"` (`len(cands) == 1` asserted, so a
  table change cannot silently turn it into another case), driven through the real
  chat input layer, asserted on the replayed cell grid.
- Real PTY run (`devdoc/evidence/sgr-grid/termgrid.py`, 100×30) is recorded in the
  task report with the grid rows quoted.

## Where this decision is cited

`internal/lineedit/menu.go` (`Menu.noteRows`) names this document as the record of
the one-candidate decision, so the ruling and the code that implements it point at
each other.
