<!-- doc-class: normative -->
# The answer band moves a halved token down whole — and the band's cut label narrows to "wider than the row"

## Completed

`internal/lineedit/band_word_move.go` — the band's row cutter applies the wrap
policy's fourth disposition, the one `wrapCols`/`WrapToCols` already carried
(`wrap_whole_word_move.go`, pinned exhaustively by
`wrap_whole_word_sweep_test.go` at 1dbabcd7):

```go
func bandCut(text string, width int) (head, tail string, halved bool)
func bandWordShift(text string, from, cut int) int
func bandCutHalvesToken(text string, cut int) bool
```

`writeRow` calls `bandCut` where it called `cutCols` (one line), and
`cutLabelFor(text, cut, halved)` takes the verdict the SAME cut produced instead
of re-deriving it from the source.

## Why the band was different from the exported policy

`wrapCols` ends a row at a boundary the source offers, and inside a token only
when that token is wider than a whole row. The band cut with `cutCols` alone — a
pure CELL cut — so at 32 cells

```
run /compact then check the equa↵
lity of s//ty↵
le now
```

every row stopped inside a token and wore `↵`. A row that looks like that does
not run when copied, which is the whole point of the band.

## The difficulty was not the break — it was that the line is drawn twice

One answer line reaches `writeRow` along two paths: the streaming paint
(`drain` → `writeLive`, which commits each row as it fills) and the commit replay
(`commitPrefix` → `commitLines`, which holds the whole line). `fitCommitRow`
reconciles them by the head the live row already shows, and that reconcile is
arithmetic on COLUMNS — so the two paths must reach the SAME break or the erase
leaves a tail on a row nothing repaints (the measured `delta=1` +60 cells).

The previous attempt tried to satisfy that by asking `wrapWholeWordShift` (whose
verdict needs the token's own END), which the paint has not seen yet; the paint
and the replay then disagreed, and the band's six cell-grid cases went red.

**So the band's move asks only what the text in hand can answer** — the cell edge
lands between two word runes, the token starts after the row's own first byte,
and that start is a position a row may end at (`wrapSafeRowEnd`). It reads no
byte past the cut, so the unfinished line and the finished line it becomes
decide identically. That is what makes one disposition serve both paints.

## What narrowing the label means

`↵` (`answerCutLabel`) now says exactly one thing: **the token being cut is wider
than the whole row** — the only break that has no wider row to move the token to.
Every other mid-token break is repaired, not labelled. `TestAnswerCut_*` were
re-pointed at that class (one wide token; `strings.Repeat` of letters with no
separator), and `TestAnswerCut_MidWordBreakIsMarked` gained the opposite arm: a
line of separable tokens wears no label AND its rows reassemble to the line.

The measured PTY case (`testsuite/chat_cut_pty_test.go`) had the same latch: its
fixture was `fix/pushback-rule31-evid:…`, a run of SMALL tokens separated by
`/ - _ : .`, so under the new disposition nothing in it was cut and, correctly,
no row was labelled. The fixture is now ONE over-wide token
(`chatCutSuiteTokenPieces`), and on a real terminal at 117 columns the rows read

```
band row 0|fixpushbackrule31evidencealphabetagammadeltaomega…↵
band row 1|1evidencealphabetagammadeltaomega…↵
band row 2|tagammadeltaomega…↵
band row 3|afixpushbackrule31evidencealphabetagammadeltaomegafixpushb
band row 4|-cut-end-
```

## Invariants

- `head + tail == text`, byte for byte: the rows of a line still reassemble to
  the line. Where a wrap consumes a leading space (`trimWrapSpace`), the fixture
  walks the source and counts exactly one consumed space per boundary.
- A wide rune is never split; a row ends at whitespace, at `wrapSoftBreakPunct`,
  at a wide↔narrow junction, and never inside an SGR sequence.
- `answer.go` SHRANK, 265,870 → 265,588 bytes (Rule 39's ceiling is 262,144 and
  it was already over): the label's decision moved into the new file rather than
  growing the old one.

## Verification

- `go test ./internal/lineedit/ -run 'TestAnswerBand|TestAnswerCut_'` → ok
- `go test ./internal/lineedit/ -run 'TestWrapCols_|TestWrapToCols_'` → ok
- `go test ./internal/lineedit/` (whole package) → ok
- `go test ./testsuite/ -run 'TestChatPTYCut…|TestChatPTYAnswer…' -v` → 2 PASS on a
  real pty, rows quoted above
- `go build ./...` → ok

## The one case the earlier attempt's report called out, and what it turned into

Its note said the six red cases could only be closed from the PAINT side (hold the
token that might move down, decide once the whole line is in). That held for the
old rule, whose verdict needs the token's end. It does not hold for the band's
rule, and the cheaper shape won: the paint does not have to hold anything, because
the question it can answer is sufficient. `TestEmphasisWrappedLine*` needed one
constant adjusted (`wrapBoldHeadCells` 8 → 7 — the space in front of the run now
opens the row below), which is a statement about where the row BREAKS, not about
what the span contains.
