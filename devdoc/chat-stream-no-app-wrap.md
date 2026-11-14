<!-- doc-class: historical -->
# Task Report: the chat answer stream carries the model's bytes, not the width's

> **Retired mechanism (the width-bounded answer stream)** — `cmd/agent/chat_width.go`,
> `chatRenderer.rows`, `wrapStreamText` and the two break constants described below
> were DELETED by this run. Do not run or look for them; this page is the record of
> why they went.

Branch: `master` in this run's own clone (cut at `61f286b`).

## What was wrong

The fallback chat renderer (`chatRenderer`, the path every terminal the reserved
layout does not host takes) bounded each emitted row to the terminal's width and
wrote the ROW BREAK as bytes: `emit()` at `cmd/agent/chat_stream.go` split the
text with `lineedit.WrapToCols` and printed each piece followed by `\r\n` (a
width-ended row) or `\n` (the answer's own line end).

A terminal folds a row the application never ended at ZERO bytes of stream. The
renderer wrote the fold instead, so:

- a long file path reached the terminal split, and copying the row out of the
  terminal carried a return into the middle of it (pasting a command ran it);
- the width was re-read once per turn, so the FOLD MOVED with the window — the
  same answer copied out differently at 63 and 101 columns.

Measured before the fix (this run's own evidence, `evidence/nontty/`):

```
width  40: whole 201-byte answer found verbatim: False
          raw region: b'/home/user/workspace/01a1bca5-4a71-71bf-\r\r\n9d4b-bcecdec15313/internal/tools/git_\r\r\npublish_target_guard.go /home/user/\r\r'
width 101: whole 201-byte answer found verbatim: False
          raw region: b'/home/user/workspace/01a1bca5-4a71-71bf-9d4b-bcecdec15313/internal/tools/git_publish_target_guard.go \r\r\n/home/user/works...'
```

After (same harness, same answer):

```
width  40: whole 201-byte answer found verbatim: True   CR=0 LF=0 bytes=201
width 101: whole 201-byte answer found verbatim: True   CR=0 LF=0 bytes=201
two widths delivered BYTE-IDENTICAL answer: True
```

## What changed

`cmd/agent/chat_stream.go`:

- `emit()` now writes the model's bytes and nothing else. It has one branch
  because there is one behaviour: the answer stream never gains a byte from a
  width — not on a terminal, not on a pipe, not on a test buffer. The terminal
  folds a long row itself, at zero bytes of stream.
- `chatRenderer.rows` deleted — the field WAS the width input, and a renderer
  with no width cannot be told to wrap by any caller.
- `newChatTurnRenderer` no longer reads a width (the once-per-turn
  `wrapColsFor(r.out, false)` call is gone, with the comment explaining why
  reading it there was the defect).
- `wrapStreamText`, `streamRow`, `chatRowBreakLF` and `chatRowBreakCRLF` deleted
  (Rule 43: their only references were this file and its own test — the test is
  inverted below).
- `chatAnswerLineFeed` briefly existed as a "the break comes from the model"
  marker and was deleted too: an unreferenced constant is a bare literal with a
  name (Rule 13), and the fact it documented is stated in prose where a reader
  looks for it.

`cmd/agent/chat_width.go` **deleted** (Rule 43 — measure the reference
distribution before deleting a file, and carry out anything general-purpose):

```
$ grep -rn "wrapColsFor" --include=*.go .    # before the deletion
cmd/agent/chat_stream.go:4238:	r.buffered.rows = wrapColsFor(r.out, false)
cmd/agent/chat_width.go:49:func wrapColsFor(...)
cmd/agent/chat_stream_wrap_test.go: ...      # the predicate's own cases
```

Its one production reader was the answer stream's width, and its only other
constants (`chatWrapColsMin`) served the same function. Nothing else in the tree
asked it anything, so the file went with its mechanism. `lineedit.TerminalCols`
(the exported reader it was built on) is untouched and still used by
`internal/terminalgeom`.

**NOT touched, on purpose** — two other width consumers have a real requirement
(the epic's constraint ①):

- `cmd/agent/chat_banner.go` — the panel/table lays out columns at the
  producer; a table NEEDS its width (the minimum, not a maximum).
- `cmd/agent/chat_flow_seam.go` — cuts at SGR boundaries inside the reserved
  answer band, where a mis-cut is a corrupt escape sequence rather than a broken
  copy. Different channel, different requirement.

## Tests

`cmd/agent/chat_stream_wrap_test.go` — rewritten around the property, not the
old behaviour (five of its eight cases asserted "the width broke the row" and
had to be inverted; the non-TTY case stayed):

| case | asserts |
|---|---|
| `TestChatStream_EmitsTheModelsBytesOnEveryWriter` | the emitted stream IS the model's bytes: no CR, no added LF, byte for byte |
| `TestChatStream_LongPathSurvivesWhole` | the reported shape — a long path streamed in pieces arrives as one line |
| `TestChatStream_TerminalFoldsTheLongRow` | non-degradation, on the CELL GRID: 200 cells still fill five 40-column rows |
| `TestChatStream_NeverStraddlesAWideRune` | CJK: no replacement character on the grid |
| `TestChatStream_MultilineAnswerKeepsTheAnswersOwnBreaks` | the only breaks are the answer's own |
| `TestChatStream_NonTTYOutputIsByteIdentical` | the pipe/file stream equals the HISTORIC renderer, written out |
| `TestChatStream_HasNoWidthToBeGiven` | the structural half of "a resize cannot reach the bytes": the constructor takes a writer and nothing else |

`testsuite/chat_wrap_pty_test.go` — rewritten as the wire-level case on REAL
`gopi chat` children (`--no-editor`, real pty, mock LLM):
`TestChatPTYAnswer_CarriesNoAppWrapBytes` starts two sessions at 40 and 101
columns, and asserts (1) the 201-byte answer appears verbatim at both widths,
(2) the two widths deliver byte-identical answer bytes, (3) no CR and no
interior LF in that span, (4) the terminal still FOLDS the row — 6 grid rows at
40 columns, 2 at 101, each holding exactly the next `cols` bytes.

**Red proof** — the same case driven against a binary built from the baseline
commit (`61f286b`, built into the evidence dir; `GOPI_BIN=` shortcut in
`internal/testutil/run.go`):

```
=== RUN   TestChatPTYAnswer_CarriesNoAppWrapBytes
    chat_wrap_pty_test.go:81: at 40 columns the child did not deliver the answer as one unbroken line.
--- FAIL: TestChatPTYAnswer_CarriesNoAppWrapBytes (10.37s)
```

Green on this run's binary:

```
=== RUN   TestChatPTYAnswer_CarriesNoAppWrapBytes
    chat_wrap_pty_test.go:112: the answer's 201 bytes are identical at 40 and 101 columns, with no CR and no interior LF
    chat_wrap_pty_test.go:128: at 40 columns the answer is folded across 6 grid rows, each the next 40 bytes
    chat_wrap_pty_test.go:128: at 101 columns the answer is folded across 2 grid rows, each the next 101 bytes
--- PASS: TestChatPTYAnswer_CarriesNoAppWrapBytes (11.33s)
```

## What did NOT carry over

- **A pty-pair width comparison inside `cmd/agent`.** Written, then deleted: the
  renderer's signature already proves no width can reach it, and a pty pair in
  that package measured the fixture rather than the fix. The real-terminal
  measurement lives in `testsuite/`, where a real `gopi chat` child runs.
- **The `app-wraps` devdoc** keeps its content (it is the historical record of
  U2) and is now inaccurate about the present; this page is the correction.

## Verification

- Build: `go build ./...` OK (`go vet ./cmd/agent ./testsuite` clean).
- Tests: `go test ./cmd/agent -run TestChatStream_` 10/10 PASS;
  `go test ./testsuite -run TestChatPTYAnswer_CarriesNoAppWrapBytes` PASS;
  package-wide runs of both in `evidence/pkg-tests.log`.
- Ran: the `gopi chat` wire measurements above (`evidence/nontty/`, driven by a
  pty harness against a scripted SSE stub), plus the two pty test runs.
- Result: the answer's bytes are the model's at 40 and 101 columns, the grid
  still folds, and the non-TTY stream is byte-identical to the baseline binary.
