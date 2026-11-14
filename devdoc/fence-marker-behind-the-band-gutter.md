<!-- doc-class: normative -->
# A fence marker behind the answer band's own gutter

**Standing claim (this file's gate spot-check):**
`internal/lineedit/fence_gutter_test.go:242` — `TestGutterPrefixedFenceIsCodeOnTheGrid`
holds this document's claim on master: the block a gutter-prefixed marker
delimits is drawn as code, and the prose AFTER the closing marker carries no code
cells (the assertion an unclosed block fails).
`TestComposerSeesTheBandGutterOnTheProductionPath` is the second half — the
shipped renderer hands the composer the guttered line VERBATIM, so the defect is
the classifier's to fix and not a caller's.

**Date:** 2026-09-29 · **Branch:** `task/01a0eaf109f97aa3835aea80afad37c4` ·
**Commit:** `821976b6` · **Symptom:** the chat answer's code emphasis painted
from a fixed point on the screen and ran across paragraph boundaries to the end
of the answer.

## The report, verbatim

The operator, watching a live `gopi chat` run:

> 固定的地方开始，比如"CAMPUS"这个部分 highlight … 注意到从上一行的" [0]）"延续到
> 下一行全部变色。

and his minimal artifact, `/tmp/hippo.txt` (172 bytes, verbatim, `cat -A`):

```
```go$
▌ // internal/agent/hippocampus_config.go:22$
▌ // hippocampusEnvEnabled is the master switch. Default OFF$
▌ hippocampusEnvEnabled = "GOPI_HIPPOCAMPUS"$
▌ ```$
```

A fenced block whose every line past the opening marker — **the CLOSING marker
included** — carries the answer band's own row prefix `▌ `.

## Root cause (5-why, and the answer measured rather than reasoned)

1. **Symptom** — the code colour crossed the answer's paragraph boundary.
2. **Why** — the composer's fence state was never toggled back to prose: a block
   opened and never closed.
3. **Why** — `asFenceMarker` (`internal/lineedit/compose.go`) reads a marker from
   the line's FIRST non-blank character, counting only `fenceIndentChars`
   (`" \t"`) as blank (§4). `▌` is `U+258C`, not one of them, so
   `▌ ````  → `rest[0]='▌' ≠ '`'` → **not a marker at all**.
4. **Why** — the band draws `▌ ` (TurnMarker) at column 0 of every marked row,
   and the answer's TEXT carries it whenever the answer quotes a screen of the
   product: `read` returns the band's rows (`152|▌ if len(tail) > …`), the
   operator pastes them, the model fences them. The classifier and the renderer
   disagreed about whose prefix `▌ ` is.
5. **Root** — the classifier had no notion that the product's OWN glyph can
   precede a line it is asked to classify.

**Which layer, answered by measurement, not by reading the call graph.** A
recording `LineComposer` installed into the shipped `AnswerRenderer` logged
exactly what `StyleFor` was handed:

```
0 style=1 asFenceMarker={indent: char:96 run:3} inFence_after=true | "```go"
1 style=1 asFenceMarker=NO  inFence_after=true | "▌ // internal/agent/…:22"
4 style=1 asFenceMarker=NO  inFence_after=true | "▌ ```"
final inFence=true
```

The guttered lines reach the composer **with their gutter intact**. (The renderer
does not concatenate the gutter into the string it hands over; it PASSES a
gutter-prefixed line only when the answer's own text carries one — which is
exactly the quoted-screen shape.) So the fix belongs in the **classifier**, and
the "strip at the call site" alternative would have been wrong: the composer is
the ONE component that answers *"is this line a fence"*, and the same stripping
at every caller would be one rule written N times (Rule 1b/14), reintroducible by
any new caller.

## The fix

`compose.go`, ~10 lines:

```go
var answerGutters = [...]string{TurnMarker, TurnMarkerSplit, ThinkingGutter}

func trimAnswerGutter(line string) string { /* one leading prefix, or the line */ }

func (c *LineComposer) StyleFor(line string) LineStyle {
	body := trimAnswerGutter(line)
	if m, ok := asFenceMarker(body); ok { … }
```

`StyleFor` steps over ONE leading band gutter before deciding anything — the same
tolerance a line's own indentation already gets. Notes:

- **The line is not rewritten.** Nothing here removes a character from what is
  drawn; only the CLASSIFIER stops pretending the product's own glyph is content
  it must parse a marker from. Every cell keeps its column.
- **Indentation still belongs to the marker.** Whatever whitespace stands between
  the gutter and the marker is still the marker's own indentation, so the
  2026-09-26 symmetry rule (a marker that is not this block's own delimiter is
  CONTENT) composes with this fix rather than being replaced by it.
- **Rule 1b/13/14** — the gutter list is not spelled here. It *is* the band's
  three constants (`TurnMarker`, `TurnMarkerSplit`, `ThinkingGutter`), whose one
  definition site is `answer.go`. `AnswerGutters()` exports the read-only face;
  `internal/uiclient/compare.go` — which had **mirrored all three literals
  inline** — now imports it. Before this change a fourth gutter added to
  `answer.go` would have been invisible to `CompareGutters`.

## Not done / honest limits

- `internal/lineedit` carries **five pre-existing whole-package failures**
  unrelated to this change, and they are unrelated in a way a reviewer can check
  rather than take on faith: the SAME five fail with the fix reverted and with the
  new test file moved aside (evidence `baseline-package.txt`). They are
  `TestEmphasisSpanAfterAWrapSpaceKeepsItsOwnCells`,
  `TestEmphasisWrapSpaceLedgerHoldsAtEveryWrapCount`,
  `TestScreenReconcilesAfterAnOverlongHold`,
  `TestScreenReconcilesAcrossAReMeasure`,
  `TestTurnHeaderLines_BlockRowsAreCommittedOnePerRow`. This run does not fix
  them and does not claim to; they deserve their own task.
- The classifier fix is **shape-based, not provenance-based**: it steps over a
  leading `▌ ` wherever it finds one. A fenced block whose FIRST content character
  is genuinely `▌` at line start (a Markdown quote inside a fence) could now be
  read as a marker if the remainder of the line is exactly a fence. This is
  accepted deliberately — the alternative (provenance) would require the composer
  to know whether a line came from the band or from the model's text, which is
  precisely the layout knowledge the composer's contract excludes.

## Verification recipe (reusable)

The two-layer proof for any composer/classifier defect:

1. **Sequence** — drive `LineComposer.StyleFor` line by line and assert the
   STYLE of the lines after the marker, never the marker's own answer alone
   (a classifier can "see" a marker and still not toggle).
2. **Cells** — assert the palette's style on a CELL, not on the classifier's
   return value: `lineedit.Screen.StyledLine` in-package, or the chat dump
   artifact's `AttrRuns` (`run.Style` carries `fg=cyan`) at the surface layer.
   The assertion that actually catches this family of defect is the NEGATIVE one:
   the prose row AFTER the closing marker must carry NO code cells — an unclosed
   block leaks, and only that direction observes the leak.
