<!-- doc-class: historical -->
# The marker column is measured in CELLS — and a wrap consumes the space it breaks at

## Symptom

The operator's chat dump (`/tmp/error.txt`: 127 rows, 117 cells wide) showed rows
inside a committed turn starting at a column their neighbours do not:

```
▌ 另外提醒一件无关但有风险的事：fetch 时 git 警告 `There are too many unreachable loose objects` 且 `.git/gc.log` 存
▌ 在 —— 自动 gc 已被禁用，仓库会持续膨胀。要我跑一次 `自动 gc 已被禁用**，仓库会持续膨胀。要我跑一次 `git prune` / gc
   吗？
```

`▌ x` / `▌ y` / `   吗？` — the turn's left edge is a staircase, and the
staircase is one cell deep: 3 against the block's 2. The same dump shows
continuation rows at cell 1 (` '*rule31*' '*r31*'; echo "--- packed refs …`)
and cell 0 (`tatus ==="; git status -sb | head -20` under a `▌ cd ~/work/GoPi; …`
row). Three different offsets on one block edge.

## Root cause

**A row that follows a wrap re-opens the marker column, then draws its text from
that column.** `writeRow` writes the gutter with `writeGutter(g, false)` — which
for a continuation is `strings.Repeat(" ", displayWidth(TurnMarker))` — and then
writes the text starting at the column the gutter ended at. If the text begins
with the SPACE the wrap broke at, that space is drawn *inside* the column, so
the row's first visible character lands one cell further in:

```
cell: 0 1 2 3
      ▌   s p a c e …        <- the row that ended, the space belongs HERE
        │ └─ the column is 2 cells (▌ is 2 cells wide)
        └── the continuation row's text starts at 2, so a leading space
            pushes the first real character to cell 3
```

Measured on master, 2026-09-27, at 56 and 60 cells and at the operator's own
117:

```
marker_column_narrow_test.go:289: cols=60 burst=7: body row 18 opens at cell 3,
    want the block's column 2 — the left edge is ragged
marker_column_narrow_test.go:267: operator geometry: body row 19 opens at cell 3,
    want the block's column 2 — the left edge is ragged
```

The space is not "extra": it is the break character, and a wrap has always been
allowed to consume it. Nothing consumed it. The `(head, tail)` pair from
`cutCols` puts it on the tail, `writeRow` hands the tail straight back to
`writeLive`, and `writeLive` writes it at the fresh column.

## Which symptoms are actually one bug

| the dump's symptom | root cause | verdict |
|---|---|---|
| `▌ x` / `   y` (indent 3) ragged edge | a wrap leaves its break space on the row that follows | **the defect fixed here** — same arithmetic at every width, ASCII and CJK alike |
| a row opening at cell 0 (a `⚙` note row's tail) | a note row's WRAP: `lineGutter()` returns `""` while `r.noting`, so the tail of a wrapped note row was drawn with a fresh row's blank indent and was indistinguishable from a new `⚙ row` | **a REAL defect — but not this one, and it must not be "fixed" here.** It was already dispatched and has since LANDED on master (`1bfd07eb`, merged into this branch by the delivery-point sync `c2ff5a4a`): `writeNote`/`cutNoteRow` now open a note's continuation with `NoteContinuation = "↳ "`, and `note_row_wrap_test.go` asserts it. `TestNoteRowCarriesNoTurnGutter` is the rule that still holds and is NOT in conflict: a note row carries no TURN gutter (`▌`), whose column belongs to the assistant's block; the `↳` is the NOTE's own column, and the two are different columns. This doc's fix does not apply to that path (`writeNote` calls `writeRow` with `head` already `↳ `-marked and never re-enters the continuation branch for it), so the two fixes are independent and both stand |
| a fenced block carrying `▌ ` | a fenced block inside a turn IS part of the turn — its lines keep the glyph, and the rows they wrap onto keep the blank column | **not a defect**; the discipline is asserted, and it holds |

So of the three symptoms reported, **one is the bug this doc fixes** — the ragged
left edge. The other two were NOT cosmetic either; each had a different owner and
a different mechanism, and master has since landed both:

- the note-row continuation at cell 0 — fixed by `1bfd07eb` (`NoteContinuation`),
  merged into this branch during close-out. **Superseded, not skipped**: this doc
  originally classified it "not a defect", which was wrong on the mechanism (a
  `⚙` tail at cell 0 was a defective wrap, not the by-design absence of a turn
  gutter), and that word is corrected above.
- the fenced block carrying `▌ ` — **not a defect**, and it stays not a defect:
  it is asserted here and holds after the merge.

## The fix

One function, one call site, `internal/lineedit/answer.go`:

```go
// trimWrapSpace drops the run of wrap spaces at the head of a row's text ...
func trimWrapSpace(text string) string {
	if text == "" || !isWrapSpace(rune(text[0])) {
		return text
	}
	return strings.TrimLeftFunc(text, isWrapSpace)
}
```

called from `writeRow`, ONLY on the path that has just re-opened the column:

```go
		r.finalizeLiveRow()
		if g := r.lineGutter(); g != "" {
			r.writeGutter(g, false)
			// The continuation row has just re-opened the block's column, so a
			// wrap space at the head of the text would be drawn INSIDE it ...
			tail = trimWrapSpace(tail)
		}
```

Three properties this shape was chosen for:

1. **The column is the marker's own cell width, never a second number.** The
   rule is stated once, by `displayWidth(TurnMarker)`; the trim removes exactly
   the cells a reader would otherwise see inside that column.
2. **A row that OPENS a line is untouched.** Only the continuation branch — the
   one that just wrote a BLANK gutter — trims. Leading spaces at the start of a
   line are the author's indentation, and the band still draws them.
3. **No text moves between rows except a space.** `isWrapSpace` is a named
   predicate (`r == ' '`), so what a wrap may consume has one definition
   (Rule 1). Tabs and ideographic spaces are content and are not touched.

The first attempt trimmed the row's TAIL in `writeRow` (moving the space onto
the tail text) and did not fix it: the live row's text is painted by
`writeLive`, not by the row-commit path, so the space was re-emitted at the
fresh column anyway. The continuation branch is where the column is re-opened,
and therefore where the trim belongs.

## Verification

- `internal/lineedit/marker_column_narrow_test.go` — four cases on the cell
  grid: a wrapped CJK paragraph at 40 and 56 cells, a fenced block at 56, the
  operator's own sentence at 117, and a sweep of widths × burst sizes. Each
  asserts the continuation row opens at exactly `displayWidth(TurnMarker)`
  cells AND that the answer round-trips through the grid, so a row cannot be at
  the right column and have lost characters.
- RED on pristine master (`8765609f`, `git archive master`), two failures
  (`evidence/RED_master_failures.txt`); GREEN after.
- `cmd/agent/pty_marker_column_dump_test.go` — a real pty at 56×48 through the
  real `ScreenDumper`, read back with `uiclient.ReadDump`:
  `8 body rows, 3 continuation rows, every one at the block's column`.

## A trap worth recording

The pty dump first read back EMPTY except the closing rule. The cause was not
the renderer: `uiclient.NewScreenDumper` falls back to its own `80×24` default
when the caller states no geometry (`defaultDumpCols/Rows`), while the editor
paints the pty's `56×48`. Every band write then landed in row 23 of a grid of
the wrong shape. A harness that drives a real pty and dumps it **must state the
geometry it measured** (`Cols: 56, Rows: 48, Geometry: GeometrySourceTerminal`)
or it reads a screen that is not the one it painted. The raw paint stream
(captured with a tee writer during the diagnosis) showed the answer rendering
correctly all along.
