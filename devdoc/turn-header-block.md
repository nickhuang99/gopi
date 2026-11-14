<!-- doc-class: historical -->
# The turn header as a multi-line block — a fixed row budget, and a counted ending

**Scope**: `internal/lineedit` (the header's composition) + one call site
(`AnswerRenderer.beginTurnNow`). Callers keep the same types: `TurnInfo` is
unchanged and `TurnHeader` keeps its signature and its meaning.

## The defect this closes

`TurnHeader` was ONE row by construction: `cutField` shortened the label
(`turn N · <prompt>`) until it fit beside the rules, and the row ended with `…`.
Measured on a real turn (the operator's own dump), a paragraph of input read

```
──── turn 1 · explain-the-failing-step-and-why explain-the-failing-ste… ────────
```

which is honest about "something was cut" and silent about **how much**. A pasted
paragraph, and a spilled paste's reference `[paste 51200: /abs/paste.txt]`, became
the same kind of row: a prefix. Nothing in the band said whether the question was
one line longer or forty.

## The shape

Three functions, in the order they compose:

1. **`turnHeaderLine(label, cols) string`** — the row `TurnHeader` always drew
   (lead rule, label, closing rule), lifted out so the block and the single-row
   entry point share ONE composition (Rule 14). `TurnHeader(info, cols)` is now
   `turnHeaderLine(turnLabel(info), cols)`; every existing degradation
   (`turnLabelHalfCells` → one lead rule; `turnHeaderMinCells` → bare label) is
   byte-identical to before.
2. **`TurnHeaderLines(info, cols) []string`** — the block: row 0 is
   `TurnHeader`'s own string, then up to `turnHeaderContinuationLines` (3, or
   `turnHeaderPasteContinuationLines` = 4 when the prompt carries a spilled-paste
   reference) continuation rows indented to the block's label column, then — when
   text is left over — one final row spelling
   `... (+N more line(s))` (`TurnHeaderMoreMarker`, exported so the pty cases and
   the callers read the state from one spelling).
3. **`beginTurnNow`** writes the block ROW BY ROW through the existing
   `writeLive` + `finalizeLiveRow` pair — one writer, one finalize per row, so the
   band's committed-row ledger counts the header exactly as the block drew it.
   No second writer, no new write path. Measured on the real 80x26 pty: the
   typed fixture occupies 5 band rows (header + 3 continuations + the count row)
   and the paste fixture 6, and `Committed()` equals the block's row count.

### What a caller sees

`TurnHeaderLines` is derived FROM `TurnInfo.Prompt` — no field was added, so
every existing `TurnInfo` literal (and the zero value) keeps its exact meaning.
A caller that wants the historical single-row header keeps calling `TurnHeader`
(unchanged signature); a caller that wants the block calls `TurnHeaderLines`.

### The states a reader can see

| state | rows | last row |
|---|---|---|
| the label fits beside the rules | 1 | `TurnHeader`'s row, no marker |
| the label does not fit, remainder spans ≥2 continuation rows | 1 + k + 1 | `... (+N more line(s))` |
| the label does not fit, remainder is one row's worth | 1 | `TurnHeader`'s row, ending in the cut `…` (the block declines — `turnHeaderBlockMinHiddenRows`) |
| `cols < turnLabelHalfCells` | 1 | the header's own narrow chain (half rule, then bare label) |

There is no fifth state: every row of a block either carries a prefix of the
label, or is the count row. That is the property the package test asserts
directly (drawn rows reconstruct to a prefix of the label; the count row's N
equals the rows the remainder still needs at a continuation row's width).

The count row itself degrades in WORDS when the width is tight: the whole
sentence, then without `)`, then without the plural `s`, then bare `... (+N` —
the marker head and the COUNT survive every step, because the count is the one
fact separating this row from a silent cut. `cutField` (the package's marked cut)
is the last resort and unreachable from `TurnHeaderLines`, which declines to
compose below `turnLabelHalfCells`.

### Constants (Rule 1)

| constant | value | why |
|---|---|---|
| `turnHeaderContinuationLines` | 3 | a header is chrome, not the answer: a pathological prompt must not push the answer out of the band (measured: 5 band rows for the 80-column fixture) |
| `turnHeaderPasteContinuationLines` | 4 | a paste reference's PATH is the operand of the reader's next action (measured: 6 band rows, one more than the typed leg) |
| `turnHeaderContinuationLeadCells` | 2 | the continuation's extra indent, so the block reads as one label |
| `turnHeaderMinContinuationCells` | 8 | below this a continuation row holds a word and costs a band row for nothing |
| `turnHeaderBlockMinHiddenRows` | 2 | a block worth composing must hide at least this many rows (otherwise the single-row marker says it already) |
| `TurnHeaderMoreMarker` | `"... (+"` | the count row's fixed head; three dots in WORDS, never the cut vocabulary's `…` |

### Two decisions worth recording

- **The count is stated in words, not with `…`.** `statusEllipsis` is this
  package's "this ROW was shortened" mark, and a row legitimately carrying `…` as
  a character (a path, a hash, typed) would then read as a mark. The block says
  its state in a sentence, and the three dots are part of that sentence.
- **The block's row cutter is `turnHeaderRowCut`, not `cutAtBoundary`.** The note
  block's cutter may replace the middle of an unbreakable token with `…`; a
  header may not — the header's rows ARE the record of what was asked, so the cut
  must be lossless (`head` + `rest` is the whole input). Measured while building
  the paste leg: the marked word cut turned
  `…/tmp/pastes/paste-1.txt] explain-the-failing…` into
  `…/tmp/pastes/paste-1.txt…`, silently dropping question text and counting it
  nowhere. `turnHeaderRowCut` reads the same boundaries (unquoted space that uses
  the row, then unquoted separator) and always falls back to a rune boundary.

## Verification

`Ran:` — the surfaces below were executed for real; the artifact paths are in the
task's session evidence directory
(`~/.gopi/sessions/01a0e81b-fcdb-79d0-b272-8b82960f731b/evidence/`).

- Package suite: `go test ./internal/lineedit/ -count=1`
- Chat surface suite: `go test ./cmd/agent/ -count=1`
- pty screen truth (Rule 31): `go test ./cmd/agent/ -run 'TestChatTurnHeader' -count=1 -v`
  → log `evidence/pty-header-block-v1.log`, which carries the painted rows
  verbatim, e.g. at 80 columns:

```
──── turn 1 · explain-the-failing-step-and-why explain-the-failing-ste… ────────
       p-and-why explain-the-failing-step-and-why
       explain-the-failing-step-and-why explain-the-failing-step-and-why
       explain-the-failing-step-and-why explain-the-failing-step-and-why
... (+3 more lines)
```

  and the paste leg at the same width (one more continuation row, reference
  whole):

```
──── turn 1 · [paste 51200: /tmp/pastes/paste-1.txt] explain-the-faili… ────────
       ng-step-and-why explain-the-failing-step-and-why
       explain-the-failing-step-and-why explain-the-failing-step-and-why
       explain-the-failing-step-and-why explain-the-failing-step-and-why
       explain-the-failing-step-and-why explain-the-failing-step-and-why
... (+2 more lines)
```

  and 40 columns (no row over the width, no wrapped row):

```
──── turn 1 · explain-the-fail… ────────
       ing-step-and-why explain-the-
       failing-step-and-why
       explain-the-failing-step-and-why
... (+9 more lines)
```
