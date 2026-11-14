<!-- doc-class: normative -->
# The wrap space's effect on the span ledger (answer-row emphasis)

Measured 2026-09-28, task `01a0e806-659a-7e91-a05d-462b43171453`.

## Symptom

An emphasis span in a rendered answer was drawn **off its own characters**: plain
prose carried the mark and the run's true tail came out plain. The operator's
117-column capture measured **17 columns** of displacement, and the marked string
was not in the source answer at all — so it was a render-layer column error, not a
content one.

## Root cause — one seam, two byte spaces

`internal/lineedit/answer.go`, `(*AnswerRenderer).writeRow`.

A single source line is one `answerRow`, and it wraps over several rows inside
`writeRow`'s loop. The row's emphasis `spans` are built over `row.text` — the
row's WHOLE text, wrap spaces included. `piecesFor` walks those spans from
`row.skip`, i.e. `row.skip` is a byte offset into `row.text`:

```go
pos := row.skip           // piecesFor (answer.go)
```

`writeRow` advanced it with the bytes it DREW:

```go
row.skip += len(displayed)   // the cells this pass painted
```

But the wrap path then drops the space(s) at the head of the continuation row:

```go
tail = trimWrapSpace(tail)   // answer.go — the gutter re-opened the column,
                             // so drawing them would push the row's first
                             // character one cell past it
```

Those dropped bytes are **still part of `row.text`**, and the next pass reads
them — so after every space-consuming wrap the ledger was **one byte short** of
the text left to walk. The span walk started that many characters early and every
drawn cell took the style of the source character **before** it.

The error is therefore **one character per space-consuming wrap**, not a constant:
the operator's 17 columns were 17 wraps of the same answer. That is exactly the
"suddenly 17" shape — a function of the input, not a slipped constant — and it is
why the `commands will only expand <TAB>` row drifted the most (longest line, most
wraps on it).

## Fix

Advance the ledger by what the pass put BEHIND the walk over `row.text`, taken
after `row.text` is replaced by the continuation tail:

```go
row.skip += len(text) - len(tail)
```

One expression covers both displacements:

- the cells drawn, i.e. `len(displayed)`;
- the wrap space(s) `trimWrapSpace` consumed, which the old form silently lost.

A row whose cut LABEL displaced its last character needs no special case: that
character is part of the removed prefix (it leads the row below), so it is
already inside `len(text) - len(tail)`.

## Why the suite was green on the defect

The existing end-to-end instrument
(`cmd/agent/chat_turn_dump_grid_pty_test.go`) asserts style continuity across a
wrap — but its fixture breaks in the MIDDLE of a token (`…16emph` / `17emph
18emph…`), so the continuation row's text never begins with a space and
`trimWrapSpace` never fires. The whole defect class was **unreachable in that
fixture**: a wrap that lands on a token boundary instead of a space.

The new cases make the wrap land ON the space:

- `internal/lineedit/emphasis_wrapspace_ledger_test.go` — unit layer, real pty at
  the operator's 117 columns, the shipped `AnswerRenderer`, per-cell Bold read off
  a vt100 grid. Includes the size guard (wrap counts 1/2/5/17/19 all measure **0**
  displacement; RED measured exactly the wrap count).
- `cmd/agent/chat_turn_dump_wrapspace_ledger_pty_test.go` — end to end: real
  117x26 pty, real chat layout, real `--dump-screen` sink, the artifact's own
  `attrs_begin` runs.

## Recipe (reusable)

Rendering a mark off its text on a WRAPPED answer? Ask three questions, in order:

1. Is the ledger in the same coordinate space as the index it drives? (Here: drawn
   bytes vs `row.text` bytes, while `piecesFor` indexes `row.text`.)
2. Does any path DROP bytes from the text without a corresponding ledger step?
   (`trimWrapSpace` was the only one.)
3. Is the error a FUNCTION OF THE INPUT (one per wrap) rather than a constant? If
   so the defect is accumulation, and a fixture with a single wrap will hide it —
   test several wrap counts, and check the displacement EQUALS the count.

## Evidence

Session dir `/home/user/.gopi/sessions/01a0e806-659a-7e91-a05d-462b43171453/wrapspace/`:

- `operator-answer-before.txt` / `operator-answer-fixed.txt` — the operator's own
  answer through the same probe: before, the marked runs are `"quit "`, `"ype "`,
  `"ey "`, `"oes this turn render right,"` (each one character early); after,
  `"/quit"`, `"type"`, `"key"`, `"does this turn render right"`.
- `zz_probe_shift_test.go` — the probe itself (kept out of the checkout: evidence,
  not a deliverable).
