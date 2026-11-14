<!-- doc-class: normative -->
# The answer band's cut mark is a CONTINUATION mark (U+21B5 `↵`), not an ellipsis

Status: LANDED (task `01a1a325-0fd6-75de-ae26-800953e214c4`).

## The misreading this page exists for (the operator's own investigation)

The answer band's right-edge cut used to spend a row's last cell on `…` — the same
glyph the status row uses when a field's tail was **dropped** (`fitField`, the
request row, every editor the operator uses). The two facts those marks stand for
are opposite:

| surface | mark | what it means |
|---|---|---|
| status row (`fitField`) | `…` | the tail is **GONE** — one screen row, overflow discarded |
| answer band (cut label) | `…` (before) | the tail is on the **ROW BELOW**, one character displaced onto its first column. **Nothing is missing.** |

Measured sighting: a band row ended `only w…` and the row below opened
`here the TEMPLATE`; another ended `at its B…` above `PE price`. The operator
compared the screen against `raw.log`, found no difference at all, and filed it as
*"the renderer altered the model's own words"* — and they were right to: a glyph
whose meaning is "content was omitted here" was standing where nothing had been
omitted, on rows where the band's whole job is to prove it lost no cell.

Two consequences of the old spelling, both measured rather than argued:

- **It fabricated an investigation.** The comparison ran position by position over
  a difference that does not exist (`only w…` / `here the TEMPLATE`; `at its B…` /
  `PE price`), because the mark asserted an omission the wire text contradicts.
- **It could not survive a CJK environment as one cell.** `…` carries East Asian
  Width **Ambiguous (A)**, so under `internal/config`'s ambiguous-width knob it may
  spend two cells — on a row whose width arithmetic is cell arithmetic.

## The glyph, and the three properties that chose it

`↵` (**U+21B5 DOWNWARDS ARROW WITH CORNER LEFTWARDS** — the carriage-return /
enter sign) says *"the line continues below"*, which is exactly what the band did,
and says nothing about omission.

1. **One display cell in every environment.** East Asian Width is **N** (neutral),
   not A, so `runeCells` answers 1 regardless of the ambiguous-width knob. The band's
   width arithmetic is cell arithmetic; a label whose width moved with the locale
   would break every row it is drawn on.
2. **Not the status row's elision idiom.** The two vocabularies stop sharing a
   spelling whose meanings are opposite — the misread, closed at the source.
3. **Present in the monospace fonts this product's terminals use** (measured on this
   host: DejaVu Sans Mono and FreeMono both carry U+21B5).

It is also deliberately **not** `NoteContinuation` (`↳ `, two cells, a note row's
gutter — a different path with a different contract), and it is not
`statusEllipsis` / `NoteCutMarker` / `StatusEllipsis`, which keep their meaning: a
field whose tail was **dropped**, and the note/status legend's reference to it.

## The single source, and what did NOT change

`internal/lineedit/answer_cut_label_glyph.go` holds the one definition:

```go
const answerCutLabel = "↵"      // the drawn glyph
const AnswerCutLabel = answerCutLabel  // the same value for callers outside the package
```

`writeRow`'s cut branch in `internal/lineedit/answer.go` names the constant:

```go
if tail != "" && runeCount(tail) > 1 && row.allowCutLabel && r.cutLabelFor(text, len(head)) {
	cutLabel = answerCutLabel
	displayed = cutLabelCell(head)
	tail = text[len(displayed):]
}
```

**What did NOT change, on purpose:**

- **The record's semantics.** `writeCutLabel` still calls `r.cutLabels.add(r.turnIndex,
  r.liveRow)` at the one place the glyph is drawn, so `CutLabels()` still reports
  every label drawn (turn + row + total, same cap, same counting).
  `cmd/agent/chat_answer_ellipsis.go` therefore keeps its whole job: the band's
  drawn copy can differ from the wire copy, and the record is what attributes that
  difference to the DISPLAY layer instead of to the model.
- **The label is never in the source bytes.** It is drawn into the cell, `liveText`
  stays the answer's own characters, `liveCol` counts the label's cell. That is what
  keeps the emphasis span walk (`row.skip`) and the committed-row comparisons on
  their byte arithmetic.
- **`statusEllipsis` and the status/request/header cut path** (`fitField`,
  `TurnHeader`, `truncateCols`) — out of this change's scope, untouched.
- **Note rows' vocabulary** — untouched (`NoteCutMarker` still *is* `statusEllipsis`).

## chat_answer_ellipsis.go: the attribution is unchanged, one sentence corrected

The three outcomes of `attributeChatAnswerElision` are untouched (`renderer`,
`answer`, `unattributable`), and so is every `CutLabels > 0` arm. One **sentence**
had to move, because it had become false: the report used to say the drawn cells
"are U+2026 the model's output does NOT contain". It now names the glyph from the
one place that draws it:

```
; display layer: the renderer drew N cut label(s) on this turn's rows — cells
carrying ↵ that the model's output does NOT contain (the band's own continuation
mark, not an elision)
```

A report that still said `U+2026` would send a reader looking for a glyph the
renderer no longer draws. Covered by
`TestChatRenderedEllipsis_ModelWroteNoneIsADisplayLayerFact_RED` and
`TestChatRenderedEllipsis_HitBesideCutLabelsIsUnattributable_PER_CHARACTER`.

**One asymmetry worth stating, because it is what the glyph change buys:** the
renderer's mark is no longer one of the characters this product watches for. A `↵`
on a row can only be the layout's; a `…` on a row can only be the model's (or the
status row's, above the band). The seam's "which of these is the model's?" question
therefore no longer has an ambiguous answer at the glyph level — and the record
still answers it for any future mark that shares a spelling with model output.

## The repro, and its red proof

`internal/lineedit/answer_cut_label_glyph_test.go` —
`TestAnswerCutLabel_NoLetterIsElidedOnScreen`. The minimal sample is one repeated
lower-case word long enough to overflow the band (7 cells × 20 = 140 > 117), drawn
through the **real renderer** on the cut family's existing rig (`cutBandRows`). It
asserts the misreading as a shape, and keeps the label's own contract:

1. **no band row ends `letter…`** (the form a reader parses as "text was dropped");
2. **the cut is still marked** — this is not "delete the label" dressed as a fix;
3. **the displaced character is the next row's FIRST cell**, and walking the body
   consumes the answer exactly.

`TestAnswerCutLabel_GlyphIsOneCellAndNotTheElision` pins the three properties above
(one cell; not `statusEllipsis`; not `NoteContinuation`; exported spelling equal to
the drawn one).

**Red proof** (measured, on this ground): with `writeRow` reverted to
`cutLabel = statusEllipsis` and nothing else changed, the test fails with

```
answer_cut_label_glyph_test.go:81: the band ends a row `letter…` — the reader takes U+2026
for an ELISION, and nothing was elided (the character continues on the row below).
```

— and passes with the constant in place. Verification of the restore was an explicit
revision comparison (`git diff HEAD -- internal/lineedit/answer.go`), not an
index-relative one.

## What a reader of the grid must now fold

Every reader that compares the band against the answer's own text folds the layout's
glyphs out; the band's mark moved from `statusEllipsis` to `AnswerCutLabel` at these
places (each reads the exported constant, never a re-spelled literal — Rule 14):

- `internal/lineedit`: `cutReadingFold`, `cutBody`, `turnEndStrippedMarks`,
  `spanGridDrawn`, `cutLabelRuneForTest`.
- `cmd/agent`: `turnDumpCutLabel`, `reconDense`, `vrrDense`, the emphasis span e2e
  walk, and the fixture-content guards that assert the fixture carries no layout
  glyph.
- `testsuite`: `chat_cut_pty_test.go`'s `cutLabel`.

The status-row and note-row folds keep `StatusEllipsis` (that is still the glyph
they are about).

## Measured (this ground, branch `task/01a1a3250fd675deae26800953e214c4`, base `9624ace`)

- `go build ./...` → clean.
- `go test ./internal/lineedit/` → `ok … 48.675s`.
- `go test ./cmd/agent/ -run 'TestChatAnswerEllipsis|TestChatRenderedEllipsis|TestChatTurnDumpGrid|TestChatTurnDumpCellReconcile|TestEmphasisSpanGridE2E|TestChatTurnDumpVariableRead|TestChatTurnSpillHeader'` → `ok`.
- Real pty, `testsuite`: `go test ./testsuite/ -run TestChatPTYCut_MidWordCutIsMarkedOnARealTerminal -v`
  → `PASS (19.89s)`, and the case's own logged grid shows the new vocabulary:

```
band row 0|fix/pushback-rule31-evid:alpha-beta_gamma.deltafix/pushback-rule31-evid:alpha-beta_gamma.deltafix/pushback-rule31-↵
band row 1|evid:alpha-beta_gamma.deltafix/pushback-rule31-evid:alpha-beta_gamma.deltafix/pushback-rule31-evid:alpha-beta_gamma
band row 2|.deltafix/pushback-rule31-evid:alpha-beta_gamma.deltafix/pushback-rule31-evid:alpha-beta_gamma.deltafix/pushback-r↵
band row 3|ule31-evid:alpha-beta_gamma.deltafix/pushback-rule31-evid
```

Row 0 ends `…rule31-↵` and row 1 opens `evid:` — the `e` the mark took the cell of
is row 1's first character. Row 2 ends `…pushback-r↵` and row 3 opens `ule31-evid`
— the `u`. **No row ends `letter…`**, and the band's rows still concatenate to the
answer exactly, once.

Related pages: `devdoc/answer-row-cut-vocabulary.md` (the cut's mechanics and the
byte/cell reconciliation), `devdoc/chat-answer-ellipsis-attribution.md` (what the
report may claim), `devdoc/note-row-cut-is-lossless.md` (the other vocabulary).
