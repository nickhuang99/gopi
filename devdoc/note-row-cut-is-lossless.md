<!-- doc-class: historical -->
# Task Report: the note row's mid-token cut is LOSSLESS and draws no marker

**Task**: `01a12669-19aa-7751-980b-fab5a4d1f443` · branch `task/01a1266919aa7751980bfab5a4d1f443`
**Devdoc class**: evidence

## Completed

- **`internal/lineedit/answer.go` — `cutAtBoundary`'s word-internal arm no longer
  draws `statusEllipsis`.** The arm used to return `text[:mark] + "…"` with the
  budget one cell short, so the glyph stood in the note's own content cells while
  standing for *nothing* (the tail was never dropped — it reopened the next row).
  It now returns `fit, end`: a plain prefix, and the remainder opens the next row,
  which carries `NoteContinuation` (`↳ `). The note path draws **no cut glyph at
  all**, and the rows are lossless by construction.
- **`noteQuotedAt` deleted** (it refused the rune-boundary exemption inside a
  quoted run so the row would fall through to the *marked* arm instead). With no
  marked arm the predicate has no consumer, and an unreferenced predicate is a
  constant bypassed by a literal (Rule 1/13) — the rune-boundary exemption now
  carries a quoted run's punctuation as well, which is exactly what the lossless
  contract wants.
- **`NoteCutMarker` / `NoteRowLegend` comments rewritten**: the marker is now
  documented as the **status row's** vocabulary — a status field is one screen row
  and *drops* its tail, so `…` there is a true statement; the legend's own row says
  a note row never wears it and why.
- **`internal/lineedit/note_row_cut_test.go`**: the "a row that does not end at a
  text-offered break MUST carry NoteCutMarker" contract is replaced by "**no row
  ever wears the marker, and no character is lost**". `checkNoteRowBreaks` no
  longer infers a break class; it asserts the absence of the marker and compares
  the joined rows to the source text (whitespace-insensitive, because a space
  break consumes the space it broke on — `noteTail`).
- **`internal/lineedit/note_row_wrap_test.go`**: `TestNoteRowElidesATokenWithNoBoundary`
  → `TestNoteRowTokenWithNoBoundaryIsLosslessAndUnmarked` — the same fixture
  (a 72-cell digest with no boundary), the opposite assertion: the cut row must
  **not** wear the marker, and the rows joined with the gutter stripped must equal
  the note **byte for byte** (not merely after normalization — the note has no
  spaces, so the byte comparison is meaningful here).
- **Report + evidence files** in the run's session directory.

**Not changed, deliberately**: `statusEllipsis` and the whole status-row path
(`fitField`, `cutFieldHead`, `writeRow`'s `cutLabelFor` right-edge label). Grid
geometry is untouched: same content width, same block left edge, same row count —
the rows show *different characters* in the same cells, no reflow.

## Not Done

- **`testsuite`'s two note-row guards still speak the OLD contract, and this branch
  does not touch them.** Measured, not assumed:

  ```
  go test ./testsuite/ -run 'TestChatPTYCut|TestChatRenderVerdicts_OnRealPTY' -count=1
  ok  	github.com/nick/gopi/testsuite	16.425s        (exit 0)
  ```

  — they are **green on this branch**, because they are *canned, self-consistent*
  tests: `chatRenderVerdictNote`'s `unmarked` leg defines the defect as "cut
  mid-token **without** an ellipsis" (master's definition) and the canned inputs are
  assembled from that same definition, so both halves are wrong together and neither
  reddens. The real, unfixed problem is that this harness would report the *fixed*
  shape as a **failure** — it currently protects the behaviour this task removed.

  An earlier attempt at a green claim here was **self-inflicted and is withdrawn**:
  the failure I first recorded (`band row 0 ends with the cut label "…"`, and
  `chatRenderNoteRebuild` reporting `unmarked`) came from MY OWN edits to those two
  files, which I reverted (`git checkout -- testsuite/chat_cut_pty_test.go
  testsuite/chat_render_verdict_pty_test.go`). No claim in this report rests on it.

  Filed as a follow-up task (`01a1267b-13b4-7351-ae53-04851160b8b5`, parented on
  this row so it is parked until this one is released) with the corrected premise
  and the honest evidence above.

## Verification

- Build: `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → exit 0 (`BUILD_OK`).
- Tests (branch-scoped):
  - `go test ./internal/lineedit/ -count=1` → `ok github.com/nick/gopi/internal/lineedit`
    (whole package).
  - The named cases, green: `TestNoteRowTokenWithNoBoundaryIsLosslessAndUnmarked`,
    `TestNoteRowCutIsNeverSilent`, `TestNoteRowBreaksOutsideQuotedArguments`,
    `TestNoteRowWiderThanTheBandKeepsItsTokensWhole`,
    `TestNoteRowFittingTheBandIsUnchanged`,
    `TestNoteRowCJKNeverSplitsAWideRune`, `TestNoteRowLegendNamesEveryGlyph`.
  - `acceptance.txt` (session artifacts) carries the verbatim runs.

### Ran: the real surface, at the epic's geometry (Rule 31)

A temporary probe test in `testsuite/` (deleted before the commit — it was a probe,
not a deliverable) started a real `gopi chat --tools` on a PTY set to
**112×30** with `TIOCSWINSZ`, typed the question, let the mock model call `bash`
with one long command, and took two artifacts while the child was still alive:

```
GOPI_PROBE_OUT=<session>/artifacts/{before,after} go test ./testsuite/ -run TestProbeNoteRowGrid -v
```

`before` = master's renderer (the fix reverted in the worktree, binary rebuilt);
`after` = this branch. `ui_state.txt` is `gopi ui_state --target chat`'s own
reading of the armed dump, taken by the probe as a second process against the same
profile.

#### Result — the cell grid, 112 columns

`before` (master):

```
020|⚙ call bash echo 0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcd…
021|↳ ef0123456789abcdef0123456789abcdefghij
```

join of the two rows (what a selection + paste yields), `contains "…"` = **True**:

```
⚙ call bash echo …89abcd…ef0123456789abcdef0123456789abcdefghij
```

`after` (this branch):

```
020|⚙ call bash echo 0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcde
021|↳ f0123456789abcdef0123456789abcdefghij
```

join, `contains "…"` = **False**, and **byte-identical to the command**:

```
⚙ call bash echo 0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdefghij
```

Both readings name the same file (`dump=<home>/.gopi/sessions/<id>/chat-screen.dump`),
both report `cols=112 rows=30`, and the join is taken the way a reader takes it:
select the two rows, drop the `↳ ` continuation gutter, concatenate. The `↳ ` is a
GUTTER (a two-cell marker in the note block's own column, documented in
`NoteRowLegend`), not content — this task's constraint is that content cells lose
their glyph, and it touches no gutter.

### Red → green

Red proof taken **on this branch** with master's marked arm restored in place
(`marked, _ := cutCols(text, width-displayWidth(statusEllipsis))` +
`text[:mark] + statusEllipsis` — the exact pre-fix body):

```
--- FAIL: TestNoteRowCutIsNeverSilent
    note_row_cut_test.go:171: row 0 wears the cut marker "…": "⚙ call bash echo \"quotedwordquotedwordquotedwor…"
          the note path is LOSSLESS — the tail continues on the next row, so the marker stands for
          nothing while sitting in a cell of the command the reader copies
--- FAIL: TestNoteRowTokenWithNoBoundaryIsLosslessAndUnmarked
    note_row_wrap_test.go:334: no row ended inside the digest (["⚙ call bash a9f4c2e7b1d8…a9f4c2e7b1d…" "↳ 8a9f4c2e7b1d8…"])
```

The same two cases with the arm removed (this branch): **PASS** (see the green run
above). The grid evidence is the second, independent red→green: the same probe run
against both binaries, `abcd…` → `abcde`.

**Commit**: <filled by the commit step> — the branch is the deliverable.

## Evidence (self-filed)

- `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → exit 0, no output.
- `go test ./internal/lineedit/ -count=1` → `ok github.com/nick/gopi/internal/lineedit 32.457s`
  (before the testsuite leg; the whole-package line is in `acceptance.txt`).
- Red run (master's arm restored, same worktree): `--- FAIL: TestNoteRowCutIsNeverSilent`
  and `--- FAIL: TestNoteRowTokenWithNoBoundaryIsLosslessAndUnmarked` quoted above.
- Green run, `-v`: seven named cases `--- PASS`, `ok github.com/nick/gopi/internal/lineedit 0.361s`.
- `git status --porcelain` after the probe was deleted: exactly the three source
  files modified (no untracked residue); after the commit it prints nothing.
- `acceptance.txt` (session artifacts) — the branch-scoped runs, verbatim:
  `go test ./testsuite/ -run 'TestChatPTYCut' -count=1` → `ok
  github.com/nick/gopi/testsuite 14.400s`; `go test ./internal/lineedit/ -count=1`
  → `ok github.com/nick/gopi/internal/lineedit 31.534s`; `go test ./cmd/agent/
  -run 'ChatTurnDump|ChatStatus|Cut|Note' -count=1` → `ok
  github.com/nick/gopi/cmd/agent 105.226s`; and `verdict-run.txt` →
  `ok github.com/nick/gopi/testsuite 16.425s` (exit 0, `pipefail`).
- `gopi tool doccheck` → `doccheck: the devdoc corpus is consistent — every file
  under devdoc/ has a well-formed row` (exit 0).

## Why the change is where it is (the design decision)

`statusEllipsis` means **"this field was shortened"**, and the status row's
one-screen-row invariant is what makes it true there — the tail really is gone
(`fitField`). The note path has the opposite property: the tail is on the next
row, marked `↳ `. So the glyph in a note row was a **false statement drawn in
copyable ink**. The project already ruled on this class once, for the `▌ ` gutter
of a fenced code block:

> **THE FIX IS THE GUTTER'S INK, NOT THE GUTTER'S COLUMN.**
> — `internal/lineedit/copied_text_test.go`

Same shape, second instance: the note block's *column* (the `↳ ` continuation
gutter) is what the reader needs, and the *ink* (`…`) inside the content cells is
what they must not get. Nothing on the grid reflows; the marker simply leaves.
