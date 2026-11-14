<!-- doc-class: historical -->
# Task Report: the inline span's cells on the screen (117 columns)

## Completed

- **The defect's mechanism, pinned by measurement, not by reading.** The row a
  delta boundary commits was decided from the WHOLE line while the live row above
  it showed a TRUNCATED head, and the seam that decides "is the head already in
  this row?" compared the two as *decisions* instead of as *spellings*. Head
  `**b*` (the closing marker cut by the boundary) tokenizes with its opening `**`
  kept LITERAL — an unclosed pair must stay literal on screen — so
  `headIsInRow(row.text, shown)` answered "no" for a row that already opened with
  the head's own bytes, the head was glued back on, and the committed row read
  `**b*b`. Nothing repaints a committed row, so the operator saw a literal `**`
  and a bold run that did not match the source. Trace (measured): `a\n**b**` at 2
  bytes per delta → `commitLines line=[*] compPrefix=[**b*]`, and the grid then
  drew `*` where there was no `*` in the source.
- **`internal/lineedit/emphasis.go` — the fix (15 lines, one seam).**
  `fitCommitRow` now tests the head as a SPELLING before it tests it as a
  decision: `headIsTheLinesOwnEarlierHalf(raw, head)` — `raw` is the line the row
  was decided from, and a head that is a **strict** prefix of it is the same
  characters in the stream's own spelling, so the row already opens with them and
  nothing may be prefixed. The `len(raw) > len(head)` half is not decorative:
  `commitPrefix` hands the SAME string as both head and line (the partial paint
  drew exactly the head — the hold in `unfinishedLineWouldWrap` is what keeps a
  wrapping line whole), and an equality there means the head is the row's WHOLE
  spelling rather than an earlier half of it. Treating that equality as
  "already carried" drops the head from every row whose break arrives in its own
  delta — which is what the wrap-space ledger's fixtures read, and it is why the
  first cut of this condition (a bare `HasPrefix`) was rejected by them.
- **`internal/lineedit/emphasis_span_grid_test.go` (new) — the regression, at the
  package.** Drives the SHIPPED renderer at **117 columns** and compares, rune by
  rune and Bold flag by flag, the terminal grid (`Screen.StyledLine`) against the
  tokenizer's own spans (`LineSpans`) — never a string equality on the row, which
  cannot see a shift of attributes. 8 fixtures × 2 turn shapes × 9 delta sizes
  (1…64 bytes), including pure-ASCII emphasis, a CJK run, a CJK line with inline
  code, long lines that WRAP in both bytes-per-cell regimes, and the delta split
  that makes the defect reachable.
- **`cmd/agent/emphasis_span_grid_e2e_test.go` (new) — the regression, at the real
  REPL (Rule 31).** A real `gopi chat` on a real pty at **117×30**, the production
  input layer, a mock model answering over SSE: it streams `方向\n**线索**` in
  **2-byte** frames, which splits the `**` pair by construction, and reads the
  painted grid per cell. The frame size is not lucky — the case was swept over
  1…16 against the UNFIXED renderer, and 2, 5, 8 and 16 reproduce while 1, 3 and 4
  do not (at those cadences the boundary happens to miss the pair); 2 is kept as
  the smallest reproducing cadence.
- **`internal/lineedit/emphasis_wrapspace_ledger_test.go` — two PRE-EXISTING reds
  repaired.** Both tests were red 3/3 on pristine master (see `## Not Done`), and
  both failed for a reason that had nothing to do with the ledger they guard:
  `wrapLedgerMeasure` scanned the WHOLE band, so the turn's echoed PROMPT row —
  drawn with the palette's `Prompt` style, which is **bold by design** — was read
  as 19 "foreign" bold cells on every case. The measurement now skips the turn's
  own chrome (`RequestGutter` and the header's rule), which is what makes it a
  statement about the answer's BODY.

## Not Done

- **`TestTurnHeaderLines_BlockRowsAreCommittedOnePerRow` is still red, and it is
  NOT this task's.** It fails identically on PRISTINE `HEAD` with all three of this
  task's files reverted (measured 3/3: `BeginTurn committed 8 row(s), want 5`). It
  belongs to the turn-header block's row accounting and needs its own dispatch
  (Rule 26). No claim of a green package is made.
- **The `**` -literal symptom was reproduced end to end; a shift WITHOUT a lost
  marker (a boundary that splits a pair in a wrapped row) was not driven through
  the real REPL.** The package gate covers the wrapped cases; the e2e covers the
  single-line split. A reproducing e2e for the wrapped-split shape is a worthwhile
  follow-up.
- **No claim is made about other marker families.** This fix is about the seam that
  rejoins a line split inside a pair; `**` is the case measured. Whether the same
  folding question affects code spans or fence markers was not investigated.

## Verification

- Build: `go build ./...` → OK (no output)
- Tests: `go test ./internal/lineedit/ -count=1` → 1 FAIL
  (`TestTurnHeaderLines_BlockRowsAreCommittedOnePerRow`, pre-existing, red on
  pristine HEAD — see `## Evidence`), everything else PASS.
  `go test ./cmd/agent/ -run TestChatEmphasisSpanCellsMatchTheSourceAtTheOperatorWidth -count=5`
  → `ok github.com/nick/gopi/cmd/agent 1.958s`.
- Ran: the real surfaces, verbatim —
  - **RED** (defect present): `cp <HEAD's emphasis.go> internal/lineedit/emphasis.go && go test ./cmd/agent/ -run TestChatEmphasisSpanCellsMatchTheSourceAtTheOperatorWidth -count=1 -v`
    → `the grid's cell 2 after "方" is "*", the answer has "线" — the drawn CHARACTERS are wrong` … `--- FAIL`
  - **GREEN** (fixed): `go test ./cmd/agent/ -run TestChatEmphasisSpanCellsMatchTheSourceAtTheOperatorWidth -count=1 -v`
    → `the live REPL at 117x30 painted the model's answer "方向\n**线索**" with the span's cells exactly as the tokenizer decided: 方向[线索]` … `ok`
  - **RED/GREEN at the package**: `cp <HEAD's emphasis.go> internal/lineedit/emphasis.go && go test ./internal/lineedit/ -run TestEmphasisSpanCellsMatchTheSourceAtTheOperatorWidth -count=1`
    → `marker split across deltas: marked=false chunk=2: the grid's cell 1 is "*", which the source does not have at that point` … `FAIL`;
    with the fix → `ok github.com/nick/gopi/internal/lineedit 0.573s`
  - **The two repaired reds**: `go test ./internal/lineedit/ -run 'TestEmphasisSpanAfterAWrapSpaceKeepsItsOwnCells|TestEmphasisWrapSpaceLedgerHoldsAtEveryWrapCount' -count=1`
    → before: `the marked run is off its own cells: 0 of its 9 characters are plain and 19 foreign cell(s) carry Bold` … `FAIL`;
    after: `ok github.com/nick/gopi/internal/lineedit 0.568s`
- Result: at 117 columns, every `**…**` span's cells carry Bold and no other cell
  does, asserted per cell against the tokenizer, on both the shipped renderer and
  a live `gopi chat` REPL; the two pre-existing ledger reds are green; the third
  pre-existing red is untouched and still red.
- Commit: `18db7f2f feat(lineedit): the head a delta split is rejoined by its spelling, not its decision`

## Evidence (self-filed)

- The pre-existing red, on PRISTINE HEAD (all three of this task's files reverted) —
  `git checkout -- internal/lineedit/emphasis.go internal/lineedit/emphasis_wrapspace_ledger_test.go`,
  e2e/tests moved out of tree, `git status --porcelain` empty:
  `for i in 1 2 3; do go test ./internal/lineedit/ -run TestTurnHeaderLines_BlockRowsAreCommittedOnePerRow -count=1; done`
  → `--- FAIL` three times, `turn_header_block_test.go:364: BeginTurn committed 8 row(s), want 5 (one per block row)`.
- The two repaired reds, same pristine tree and `emphasis_wrapspace_ledger_test.go`
  present but unmodified: the same two test names FAIL, with the failure text
  quoting 19 foreign cells at grid row 17 — the row whose text is
  `▸ wrap space ledger`, i.e. the echoed prompt row, read at master's own
  `wrapspace` file: `less` (measured) `row 17 boldcells=19 text="▸ wrap space ledger"`.
- No new red was introduced: the full-package failure SET is byte-identical before
  and after this change —
  `comm -13 /tmp/fails.master.txt /tmp/fails.fix.txt` → empty
  (4 failing test names each side: the three header/wrapspace reds plus the two
  subtest rows of the ledger, which the repair removed from the fix side).
- gofmt: `gofmt -l internal/lineedit/emphasis.go internal/lineedit/emphasis_span_grid_test.go internal/lineedit/emphasis_wrapspace_ledger_test.go cmd/agent/emphasis_span_grid_e2e_test.go` → no output.
  (`gofmt -l internal/lineedit/` also lists `paste_envelope_test.go`, which is
  unformatted at HEAD and untouched by this change — `git diff --stat HEAD -- internal/lineedit/paste_envelope_test.go` is empty.)
- The e2e's cadence sweep, against the UNFIXED renderer:
  `for C in 1 2 3 4 5 6 7 8 9 10 12 16; do SPAN_E2E_CHUNK=$C go test ./cmd/agent/ -run TestChatEmphasisSpanCellsMatchTheSourceAtTheOperatorWidth -count=1; done`
  → red at 2, 5, 8, 16; green at 1, 3, 4, 6, 7, 9, 10, 12.
