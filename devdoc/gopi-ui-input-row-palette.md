<!-- doc-class: historical -->
# `gopi ui`'s input row carries the palette — prompt and buffer, applied after the cut

**Task** `01a0e44a-3369-7d6f-a653-d6125a9f4944`. C1's sibling for the second
surface: `gopi chat`'s editing row was put under the palette in task `01a0e439`
(`Palette.Buffer` over the typed buffer), and `gopi ui` — a separate surface with
its own renderer — was still drawing its persistent input row as ONE unstyled
string. The operator's typing row sat in whatever colour the terminal declares
for plain text, next to chrome that carried the palette.

## The defect, measured before the change

The row is drawn by one function, `inputLine()` (`internal/uiclient/model.go`) —
`inputPrompt + before + caret + after`, then `truncateToWidth`. Nothing in it
read `m.pal`. A probe compiled against the **pre-change tree** (detached worktree
at `d6b596a1`, `zz_inputrow_probe_test.go`, capture in this task's session dir as
`/tmp/probe-base.txt`) rendered six shapes × five tiers:

```
empty · short · mid-caret · truncated-w12 · cjk · wrapped-long-w20
× mono · basic · 256 · hermes-dark · hermes-light
```

All 30 lines were **byte-identical**. That is the measurement the fix is
answerable to: the row ignored the palette entirely, not "the palette was wrong".

## The change

```go
func (m model) inputLine() string {
	before := string(m.input[:m.cursor])
	at := promptCursor
	after := string(m.input[m.cursor:])
	line := truncateToWidth(inputPrompt+before+at+after, m.width)
	n := inputPromptCells
	if n > lineedit.DisplayWidth(line) {
		n = lineedit.DisplayWidth(line)
	}
	prompt, buffer := splitCells(line, n)
	return m.pal.Prompt.Wrap(prompt) + m.pal.Buffer.Wrap(buffer)
}
```

Three decisions, each one load-bearing:

1. **The SGR goes on AFTER the cut, never before it.** `truncateToWidth` measures
   DISPLAY CELLS; an escape byte is not a cell, and letting one into that
   measurement is the row-shear this repo has already paid for more than once.
   The layout is decided on plain text, and only the surviving halves are painted.
2. **The halves are the palette's existing fields — no new field was invented.**
   The prompt half takes `Palette.Prompt` (the same field the chat surface's
   prompt uses) and the buffer half, including the inline caret cell, takes
   `Palette.Buffer`. Two fields, one table, one source of truth.
3. **The boundary is DERIVED, not spelled.** `inputPromptCells` is
   `lineedit.DisplayWidth(inputPrompt)` — so the split cannot drift from the
   prompt string it splits. `splitCells` walks runes and stops BEFORE a glyph
   that would cross the budget (the same rule `truncateToWidth` uses), so a wide
   CJK glyph that straddles the boundary stays whole in the second half: half a
   glyph is not a cell this row can draw. A window that cuts INSIDE the prompt
   therefore emits prompt bytes only — the buffer half is empty and costs nothing.

Mono is untouched **by construction and by test**: `MonoPalette`'s style fields
are the zero value, and `Style.Wrap` returns its argument unchanged when the
field is empty.

## Verification — the three claims

**1. The palette reaches the LIVE CELL GRID** (not "we wrote some SGR bytes").
The real surface, run under a pty with `--dump-screen`, typing `hi 宽`:

```
$ gopi ui --dump-screen        # TERM=xterm-256color, 24×60 pty, typed "hi 宽"
$ gopi ui_state --pid <pid>                → row 022
022|you> hi 宽 ▏
022|0:5=fg=colour-186 bold 5:6=fg=colour-220 bold 11:49=default
```

`TERM=xterm` (the basic tier) on the same drive:

```
022|0:5=fg=green bold 5:6=fg=yellow bold 11:49=default
```

Cells 0–4 are the prompt under the prompt field; cells 5–10 are `hi 宽` + the
caret under the buffer field; the rest of the row is untouched background. The
two halves are DIFFERENT, which is the point: one field, two emphasis roles.


**3. The pre-change bytes are the colour-off bytes.** The six golden strings were
captured on the pre-change tree and are asserted by
`TestInputRowMonoBytesAreUnchanged` against both `MonoPalette` and the
zero-value `lineedit.Palette{}` — a future field that is not zero fails loudly
instead of quietly putting SGR on a monochrome terminal.

## Tests

`internal/uiclient/input_row_palette_test.go` (new):

| test | claim |
|---|---|
| `TestInputRowPaletteReachesTheCellGrid` | the model's own View, pushed through `ScreenDumper`, read back per cell: the prompt cells carry the table's Prompt style, the buffer cells its Buffer style, and the two differ. Styles are read back from the TABLE's own sequence (`declaredCellStyle` feeds the field through lineedit's parser), never typed into the test. |
| `TestInputRowCarriesThePalettePerTier` | basic / 256 / hermes-dark / hermes-light × all six shapes: both halves painted, and the stripped row is the same TEXT in the same cells as the unstyled row (the SGR was applied after the cut, not counted as cells). |
| `TestInputRowMonoBytesAreUnchanged` | the six measured pre-change byte strings, mono + zero-value. |
| `TestInputRowCutLandsOnRuneBoundaries` | a 3-cell window draws exactly `Prompt.Wrap("you")`; `splitCells("ab宽cd", 3)` leaves the wide glyph whole in the tail. |

`internal/uiclient/client_test.go` — **re-derived, not deleted**:
`TestViewLayoutIsStable` identified the input row with
`strings.HasPrefix(lines[len-2], inputPrompt)`, which holds only while the row
carries no SGR before the prompt — i.e. it was an UNSTYLED-ROW assertion standing
in for a layout assertion. It now strips SGR before the prefix check, and
`TestViewInputRowIsTheSecondToLastLineWithColourOn` asserts the property the old
check used to carry (with colour on, the row opens with the palette's own
prompt). Two tests, two capabilities — no claim was weakened, the colour
capability got its own.

## Related, and NOT fixed here (`## Not Done`, honest)

- **`gopi ui_state --compare` is inert on a non-chat surface.** On this live
  surface it answers `outcome=could-not-run`, `reason="no transcript lines were
  available"` (`transcript_lines=0`) — the comparison's authoritative side is a
  transcript, and `gopi ui` writes no session transcript the reader can find. The
  cell-grid read above is done from the DUMP instead. Making `--compare`
  meaningful for this surface is its own task.
- **No color-scheme judgement.** The change proves the palette REACHES the row; a
  terminal theme where colour-186 (a red-ish 256-cube index) reads badly against
  the buffer's colour-220 is a palette-table question for the table, not for
  `inputLine`.
- **The transcript's own rows are untouched.** This task is the INPUT row only.
