<!-- doc-class: normative -->
# The fence marker's toggle rule: a marker that is CONTENT, not a delimiter

Task `01a0dcc4-7967-7cce-8412-78e86983ab4a` (branch
`task/01a0dcc479677cce841278e86983ab4a`, base master `79807932`).

## The defect, as the operator saw it

Reported live on `gopi chat --tools` (2026-09-26), two symptoms:

> "sometimes I do see you are displaying bold, or maybe highlight, but still
> 'title' is not, i.e. texts wrapped by double star ( ** )."
> "if you can see the color of font, you will notice this has a different color"

One cause. `LineComposer.StyleFor` toggled `inFence` on ANY line whose trimmed
text started with a fence marker:

```go
if isFence(trimmed) {          // HasPrefix(trimmed, "```") — four chars of state
    c.inFence = !c.inFence     // toggle, unconditional
    return LineCode
}
```

`isFence` read four characters and threw the rest of the line away. A line whose
text begins with ``` is not necessarily a block's DELIMITER — most often it is a
marker the answer is *quoting* — and one such marker left the classifier inside a
block for the rest of the answer. Every later prose line then came back
`LineCode`, and `AnswerRenderer.rowFor` decides the drawn text from that verdict:

```go
if ls != LinePlain || !hasBoldMarker(line) { return answerRow{text: line, st: st} }
```

so `**bold**` lost its emphasis (the span path was never entered) AND prose was
painted in the code style — the pair of symptoms above, from one wrong line.

## The trigger is not exotic: it is in this machine's own corpus

Measured over `/home/user/.gopi/sessions/*/*.jsonl` (65,414 assistant answers):
4 answers end with an odd number of fence-marked lines. The most recent
(session `a8553c96`, the same day as the report) is an answer quoting its own
screen:

```
**Verified end-to-end, ...** I watched it live:

```
you> run the bash tool with exactly: head -3 go.mod
gopi>
...
      ```                      <- the quoted capture's own marker, INDENTED
      go 1.24.0
      ```
idle · turns 1 · tool events 2
```

Every acceptance criterion met, **observed by me, not taken from the report**: ...
```

The quoted capture's first marker is glued to the end of the line `gopi> ``` ` —
not a marker at all. The capture's last marker is indented six cells inside the
block that quotes it: a marker of the block's own kind, but not its delimiter. On
master that line closed the quote early, the capture's own closing marker
RE-OPENED a block, and the answer's two closing paragraphs — both carrying `**` —
were painted as code with no emphasis.

## The rule chosen, and the alternatives MEASURED (not preferred)

A marker opens a block and a marker of **the block's own shape** closes it. The
shape is the line's indentation, its fence character, and how many of that
character it carries:

```go
type fenceMarker struct { indent string; char byte; run int }
switch {
case !c.inFence():          c.open = m      // a block opens on any marker, any indent
case c.open == m:           c.open = fenceMarker{}   // only its own shape closes it
}
```

Both the operator-visible defect and the whole session corpus were measured
against the alternatives before this rule was written (`internal/lineedit/compose.go`
carries the numbers where a future reader will meet them):

| rule | lines newly PROSE (cost) | lines newly CODE (gain) | odd-tail answers |
|---|---|---|---|
| master (`HasPrefix` + toggle) | — | — | 4 / 65,414 |
| **symmetric marker** (chosen) | 4 | 47 | 3 |
| symmetric + "the delimiter is the marker ALONE on its line" | 45 | 12,048 | 4 |
| symmetric + "an opening marker must be UNINDENTED" | 1,456 | 47 | 2 |

- The **"alone on its line"** clause was measured and rejected too: it asks a marker
  line to carry nothing but the marker, so the language tag every real block uses
  (` ```go `) stops opening one — 12,048 lines of tagged code would have become
  prose, and 45 prose lines would have become code. It is the clause most people
  reach for first, and it is the most expensive of the three.
- The **indent clause** was rejected on measurement: 1,456 lines of indented
  code lost their code style, because indenting a whole fenced block is a habit in
  our own answers (session `01a0b1b5` indents every block by four cells).
- The chosen rule's only **cost** is 4 lines, and 2 of them are the defect's own
  victims — the closing paragraphs of `a8553c96` above, which used to be code and
  are now the prose they are.
- The 47 **gains** are quoted-capture content (`\t`-indented code inside a block
  whose delimiter was the quoted fragment's own marker).
- **Not claimed**: `**` inside a REAL fence stays the literal the code block says
  it is (`rowFor`'s documented contract, `TestEmphasisInsideAFenceStaysLiteral`
  still green). This change narrows WHICH lines are in a block; it does not make a
  block's content prose.
- **Not claimed either**: a genuinely bare, top-level, unpaired marker still opens
  a block to the end of the answer — `an unpaired marker at top level still opens a
  block` is a case in the new table, asserted ON purpose. A marker line really can
  start a block (`cmd/agent/cli_web.go`'s `markdownNodes` does exactly the same),
  and nothing in a streaming answer distinguishes it from one whose closing marker
  has not arrived yet.

The fix is in the CLASSIFIER (`compose.go`), not at `rowFor`'s gate: decoupling
the span path from `LinePlain` would have made `**` inside a real fence bold — the
behaviour the epic explicitly preserves — and would have left the prose lines
painted in the code style, which is the other half of the operator's report.

## Regression tests (cell grid, not prose)

`internal/lineedit/fence_marker_test.go`

1. `TestLineComposerMarkerToggleNeedsAWholeLine` — the classifier's toggle rule,
   asserted by SEQUENCE (what a marker decides is the style of the lines after it):
   a quoted indented marker, a longer run of the fence character, the other fence
   character, an indented marker's own block, a bare top-level marker, and the
   closing marker's line style.
2. `TestBoldAfterASpuriousFenceMarkerIsBoldOnTheGrid` — the shipped
   `AnswerRenderer` draws the answer for real, and the resulting bytes are fed
   into the vt100 grid (`screenattr.go`): the bold span's cells carry the
   palette's `AnswerBold` STYLE, the marker never reaches a cell, and the prose
   rows carry no cell painted with the palette's `Code` style.

`internal/uiclient/emphasis_test.go` : `TestSpanPathDrawsBoldAfterAQuotedFenceMarker`
— `gopi ui`'s half, on the same `a8553c96` answer: the row's runs are built by the
shipping `assistantEntry`, the colour frame carries the palette's bold sequence
around the run (`\x1b[1mbold conclusion\x1b[0m`), the monochrome frame carries no
escape byte and is character-identical, and the quoted capture is still readable.

## Evidence

Red on master (the two new test functions, classifier reverted, tests in place):

```
--- FAIL: TestLineComposerMarkerToggleNeedsAWholeLine
    fence_marker_test.go:117: line 4: StyleFor("module github.com/the operator/gopi") = 0, want 1
    fence_marker_test.go:117: line 6: StyleFor("prose after the block") = 1, want 0
--- FAIL: TestBoldAfterASpuriousFenceMarkerIsBoldOnTheGrid
    fence_marker_test.go:193: row 9 still shows the marker: "the **bold title** line"
    fence_marker_test.go:193: row 9 still shows the marker: "the **bold conclusion** follows"
```

Full log: `~/.gopi/sessions/01a0dcc4-7967-7cce-8412-78e86983ab4a/red-final.txt`
(11 FAIL lines). Green after the fix:

```
ok  github.com/the operator/gopi/internal/lineedit   8.291s
ok  github.com/the operator/gopi/internal/uiclient   0.286s
```

Cell-grid measurement logged by the green run:

```
row 9  text="the bold title line"        | bold cells [4,13] of 19 | mask ....BBBBBBBBBB.....
row 10 text="the bold conclusion follows"| bold cells [4,18] of 27 | mask ....BBBBBBBBBBBBBBB........
View() bold run drawn as "\x1b[1mbold conclusion\x1b[0m"
```

And on the LIVE `gopi ui` surface (one real turn, `turns 1` in its status row),
read back from the TUI's own screen dump:

```
006|gopi> ```
007|      you> head -3 go.mod
008|            ```              <- the quoted capture's marker, indented: content
009|      ```                    <- the block's own closing marker
011|      the bold conclusion follows
...
90:011|0:10=default 10:15=bold 25:85=default   <- the bold run's cells carry the attribute
```

## Not done

- **A latently broken tail in the streaming renderer, found and NOT fixed here**
  (pre-existing on master, independent of this classifier): when a burst ENDS
  mid-line (`Delta("a **bold")` … `Delta(" rest\n")` — the ordinary shape while an
  answer streams), the next burst paints the row's text again and the grid reads
  the tail twice (`the bold conclusion followslows`). A whole-body single delta and
  a token-by-token delta are both clean; the trigger needs one delta ending
  mid-line AND the closing marker arriving in the next. It is a
  `computeRows`/`writeLive` seam, not the composer, and it stays reported rather
  than patched here.
- `devdoc/DOC-INDEX.tsv` carried a PRE-EXISTING duplicate row
  (`devdoc/docindex-write-path-row-ownership.md`, lines 10 and 762, introduced by
  commit `108ca498`) which kept `TestDocManifestCoversEveryDevdocFile` red. It is
  removed by this diff — a new doc row cannot pass the gate while it stands — and
  the removal is named here rather than smuggled in.
- **Not claimed**: `**` inside a REAL fence stays the literal the code block says
  it is, and a bare top-level marker still opens a block to the end of the answer
  (see the rule table above).
