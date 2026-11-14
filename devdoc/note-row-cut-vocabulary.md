<!-- doc-class: historical -->
# Note-Row Cut Vocabulary — a break the reader has, or a marker that says so

**Task** `01a0e4c7-41a3-7567-aca8-204be202a5df` · branch
`task/01a0e4c741a37567aca8204be202a5df` · master HEAD `2044ae98` · terminal 117×26

## The defect, re-measured on master

The chat's `⚙ call` row is a NOTE row (`internal/lineedit`, `AnswerRenderer.Note`
→ `writeNote` → `cutNoteRow`/`cutAtBoundary`). Its cut vocabulary was already
supposed to be: *break at a boundary the text offers, mark a break that is not
one*. On master it did neither, on the row the operator filed.

The reproducer is the EPIC's own fixture — `internal/agent`'s `ToolCallSummary`
caps the bash command at 160 bytes and appends its own `…`, `cmd/agent` renders
`⚙ call bash ` + that, and `writeNote`/`cutNoteRow` cut it against `r.cols`
(**117** — the band's width, which on a 26-row pty is the terminal's own 117; the
geometry line of the dump is the source: `terminal=117x26 model=117x26 grid=117x26`).

Master's grid (session `01a0e4c7`'s `evidence/grid-before.txt`, verbatim):

```
000|⚙ call bash cd ~/work/GoPi ; echo "=== branch ===" ; echo "=== remotes ===" ; echo "=== status ===" ; echo "===
001|↳ worktrees (the + prefix) ===" ; echo "=== does the branch ex…
```

Root cause, established by arithmetic rather than by eye (`evidence/probe5.py`
tries each candidate rule and reports which one reproduces the row exactly):

| candidate cut | cells | == observed row 0? |
|---|---|---|
| `cutCols(text, 117)` | 117 | no |
| `cutCols(text, 115)` | 115 | no |
| **`cutAtBoundary(text, 117)`** | **111** | **YES** — byte-for-byte; the row ends `… ; echo "===` |
| `cutAtBoundary(text, 115)` | 111 | yes (same head) |

Row 0 IS a boundary cut at `r.cols`. The boundary it chose was the last **space**
inside its budget — and that space is *inside a quoted shell argument*:
`echo "=== worktrees (the + prefix) ==="`. So the row ended mid-argument, and
because the break was a boundary it printed **no marker**: the reader sees a row
that looks whole and is not.

Row 1 then took the tail, was cut at its own boundary, and used **61 of the 104
text cells it had — 38 cells left unused** while it pushed `=== does the branch
exist anywhere locally?` onto a run the reader never sees.

**Two properties were violated at once**, and they are one defect:
1. a row ended at a "boundary" that is not a boundary of the text's structure
   (a space inside a quoted word);
2. therefore the cut carried no marker — the row looked whole when it was not.

`evidence/probe5.py` also disproves the two readings the epic left open: the
head is NOT writeRow's `cutCols` re-wrapped as a fresh unmarked note (that would
be 117 cells of the text), and the missing `…` is not internal/agent's digest marker (that
one sits at byte 160 of a 222-byte command, **61 bytes past the row's cells** —
the marker was never in reach, and the row's own cut owed the reader a marker the
digest's marker could not supply).

## The fix (`internal/lineedit/answer.go`)

A note row now breaks only where the text itself offers a break **outside every
quoted run**:

* `lastUnquoted(s, limit, cls)` — one forward pass that tracks `"` and `'`
  (a backslash-escaped byte is text, both by the shell's rule and by this scan),
  returning the last member of the class that lies outside quotes. The space arm
  and the separator arm both go through it, so `"a/b"`, `"=== x ==="` and a quoted
  URL no longer break at their `/` or `=`.
* `noteQuotedAt(s, i)` — the same predicate at one offset. It gates the
  **rune-boundary exemption** in `cutAtBoundary`: a break sitting between a token
  and the punctuation its quoted argument continues with
  (`… locally? =` | `===" ; echo …`) is not a boundary the reader has, so it must
  be marked like any other mid-token cut.

Everything else is unchanged, deliberately: an unquoted space is still consumed by
the break; a separator is still kept in the head; a fit that fills the row is still
preferred over a word cut; a text with no quotes is scanned exactly as before.

**Documented glyphs.** `NoteContinuation` (`↳ `) is now a row of
`NoteRowLegend`, the activity block's own GLYPH TABLE — a `const` whose glyphs ARE
the renderer's constants (`ToolGlyph`, `NoteContinuation`, `NoteCutMarker`), so
the table cannot drift from the screen. `NoteCutMarker` is the status row's
`statusEllipsis`, referenced rather than re-spelled: the product keeps ONE word for
"cells were dropped". That answers the operator's "undefined symbol" directly —
`↳` is no longer a glyph the renderer invented at the call site.

## What it produces now (real pty, same fixture, same 117×26)

```
000|⚙ call bash cd ~/work/GoPi ; echo "=== branch ===" ; echo "=== remotes ===" ; echo "=== status ===" ; echo
001|↳ "=== worktrees (the + prefix) ===" ; echo "=== does the branch ex…
```

Row 0 now ends at `; echo` — an **unquoted space**, the last break the reader
actually has, one cell short of the quoted argument's opening quote. Row 1 opens
with the documented continuation gutter and carries the whole argument. The
`⚙ call` row is now copy-pasteable as two rows, and neither is a fragment of a
shell word.

## Files

* `internal/lineedit/answer.go` — `lastUnquoted`, `noteQuotedAt`, `noteQuoteChars`,
  the two arms of `cutAtBoundary`, the rune-exemption gate, `NoteRowLegend`,
  `NoteCutMarker`.
* `internal/lineedit/note_row_cut_test.go` — the regression tests (below).
* `devdoc/note-row-cut-vocabulary.md` — this file.


