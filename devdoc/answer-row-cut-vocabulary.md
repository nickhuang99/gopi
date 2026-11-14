<!-- doc-class: normative -->
# Task Report (PART 1 — historical, superseded): the busy status row was silent at its right edge — located, prototyped, REVERTED

> **PART 1 of 2.** This is the report of the attempt that WITHDREW its fix (the
> prototype drew the marker into the row's source bytes and broke the emphasis
> path). It is kept verbatim below because its root-cause measurement is what the
> landed fix is built on. **PART 2, the landed fix, is further down this file.**
> The fix IS in this tree as of PART 2 — the Part 1 status line below is history.

**Status: PARTIAL — the defect is precisely located and its mechanism is measured,
but the fix is NOT in this branch.** This report says exactly where it lives, what
was tried, why each attempt was withdrawn, and what a follow-up must decide. It
does not claim a fix.

## Completed

- **The defect is located, with the mechanism measured** (not inferred): the answer
  body's right-edge cut is silent — the row simply stops, mid-identifier, with no
  `…` and no marker. Reproduced verbatim on a real pty (`gopi chat --tools
  --dump-screen`, 117x26, scripted model), rows 005 and 011 of the real grid:
  `… delta:epsilon_zeta_eta_theta/iota-kappa_lambda.mu:nu_xi_o…` (measured: the
  defect fires when the cut lands inside a run of narrow word characters).
- **The wrap half is confirmed HEALTHY on the operator's own screen** and must NOT
  be redone: every full answer row in `/tmp/error.txt` (117 cells) is continued by
  a row that opens with the same 2-cell block column (`▌ ` → `  `) and carries the
  rest of the line — rows 39/40/41 are `…requires a p` / `  assword/key I don't
  have…` / `   properly.` The word `password` is split across the row boundary and
  the text is complete, exactly as designed. **A correct wrap is therefore
  indistinguishable from the silent cut at the reader's level, which is why the
  cut needs its own vocabulary.**
- **A cell-grid regression test is authored and verified RED on master**
  (`internal/lineedit/answer_cut_marker_test.go`): 3 of its 4 test functions fail
  on master with the defect's own grid quoted; the control
  (`TestAnswerCut_FittingLineIsNeverMarked`) passes on both trees, so the file
  discriminates the defect instead of asserting vacuously.
- **The trap in the obvious fix is measured and documented** so the next attempt
  does not lose the same hours: the marker is DRAWN (one cell in the row's last
  cell) while `row.skip` — the emphasis path's offset — is arithmetic on the row's
  SOURCE bytes. Marking the row and slicing the source by the drawn head's byte
  length silently dropped a character the reader could see (`…and the` / `old
  conclusion…` instead of `…and the b` / `bold…`), because a head carrying `**`
  decides to SHORTER text than its bytes.

## Not Done (honest)

- **The fix is not in this branch.** The answer-row cut vocabulary was working on
  the real pty (rows 005/011 carried the marker) but each correct-by-grid version
  disturbed a byte-arithmetic consumer of the row's text: `row.skip` (emphasis span
  walk) and the committed-row text comparison. Reconciling "the marker occupies a
  cell" with "the offset arithmetic counts source bytes" needs a wider change than
  this session's budget allowed, and this repository names the alternative
  explicitly — a half-fix that LOOKS fixed is worse than none.
- **`internal/lineedit/answer_cut_marker_test.go` ships GATED, not green**:
  `cutRowMarkerOpenDefect = true` skips its four assertions with the reason in
  their own doc comment. This is a listed, auditable skip (Rule 27), not a silent
  one — flip the constant in the commit that lands the fix.
- **Rule 31's `Ran:` line covers the DIAGNOSIS, not a fix**: the dump below is the
  changed surface (the renderer) executed for real, and it is the evidence for the
  location. There is no fixed behaviour to demonstrate.
- The note/tool row (`⚙ call bash …`) was NOT the subject and was left untouched.

## Verification

- Build: `go build ./...` → `BUILD_OK` (both on the working tree and on the
  master baseline).
- Tests: `go test ./internal/lineedit/ -timeout 900s` → the package's pre-existing
  suite passes with the new file skipped:
  `ok github.com/nick/gopi/internal/lineedit`.
  RED proof on master (the new file alone, dropped into a `git archive master`
  tree at `/home/user/workspace/gopi-redbase-gi7m`):
  `go test ./internal/lineedit/ -run TestAnswerCut` →
  `--- FAIL: TestAnswerCut_MidWordBreakIsMarked`,
  `--- FAIL: TestAnswerCut_MarkerIsTheCutLabel`,
  `--- FAIL: TestAnswerCut_NoCellOfTheAnswerIsLost` (each quoting the grid),
  `--- PASS: TestAnswerCut_FittingLineIsNeverMarked`.
- Ran: the real surface, verbatim —
  `env -u GOPI_TASK_REPORT_PATH -u GOPI_TASK_REPORT_OWNER PTY_SCRIPT=…/script2.txt GOPI_HOME=<sandbox> ANTHROPIC_BASE_URL=http://127.0.0.1:9117 ANTHROPIC_AUTH_TOKEN=sk-fake ANTHROPIC_MODEL=fake-model timeout 90 python3 pty_multi.py raw-fixed.log 117 26 bin/gopi chat --tools --dump-screen`
  (a scripted Anthropic-compatible model on 127.0.0.1, a real pty via TIOCSWINSZ at
  117x26, char-by-char typing).
- Result: the real grid at 117x26, rows quoted verbatim in the RED proof above —
  the answer row stops mid-identifier (`…/iota-kappa_lambda.mu:nu_xi_o|…`) with no
  marker, while the NEXT row opens with the correct 2-cell block gutter and
  continues the text. Both halves observed on ONE screen, which is the defect's
  exact shape.
- Commit: _to follow in this branch_ — `internal/lineedit/answer_cut_marker_test.go`
  (gated, verified RED on master) + this report.

## Evidence (self-filed)

- `go build ./...` → `BUILD_OK`.
- `go test ./internal/lineedit/ -run TestAnswerCut` on master →
  3 FAIL / 1 PASS; on this tree → 4 SKIP (gated), package `ok`.
- Real-pty dump 117x26 (scripted model, `--dump-screen`): the cut row is 117 cells
  and ends `…nu_xi_o` with no `…`; the continuation row is `  icron_pi/…` — the
  gutter is present and the text is continuous, so the row IS a wrap; the marker
  is what is missing.
- `/tmp/error.txt` (operator's screen, 117 cells): 46 full rows, every one
  continued by a row whose text starts exactly 2 cells in — the wrap mechanism
  works. Adjacent rows are never two halves of one token (a word split by a wrap
  is rejoined across the row boundary).
- The prototyped fix (withdrawn): on the pty the cut rows carried `…`; in the unit
  suite `TestEmphasisWrappedLineKeepsTheBoldRunContiguous` then read `â` at cell 8
  of the bold run (a UTF-8 continuation byte drawn as a cell) and the A5B wrap
  guard's last row measured 4 cells — the byte/cell offset divergence above.

---

# Task Report (PART 2 — LANDED): the chat answer band's right-edge CUT is now visible — a mid-word cut wears the product's `…`, and the cell it takes is handed to the row below

**Status: COMPLETE.** The defect is fixed, the four gated assertions are live (no
skip), the wrap/emphasis family is green, and the fix is measured on a REAL pty at
the operator's geometry.

Branch: `task/01a0e524883e7c69b0060b9f24f69e83` (this worktree).

## Completed (the landed fix)

**This is the reconciliation the Part 1 report above says was missing.** The marker
is OUT of the source, the class is decided on the source, and the displaced
character is the row below's first — the wrap/emphasis family is green with no skip.

### The vocabulary (what the reader sees)

> **THE GLYPH CHANGED ON 2026-11-01 (task `01a1a325`).** The label is now
> `answerCutLabel` = **`↵` (U+21B5)**, not `statusEllipsis` (`…`). `…` means "the
> tail was DROPPED" everywhere else in this product (fitField, the request row),
> while the band's cut DROPS nothing — it displaces one character onto the row
> below — so a reader took it for an elision and went hunting for text that was
> never missing. The mechanics below (which rows earn a label, the ONE cell it
> costs, the displaced character leading the next row, the record it leaves in
> `CutLabels()`) are unchanged; only the cell's spelling moved, and every fold in
> the tests reads `lineedit.AnswerCutLabel`. See
> `devdoc/answer-cut-mark-is-a-continuation-not-an-elision.md` for the measured
> misreading and the properties that chose the glyph. **The grid rows quoted below
> are the historical measurement and keep their `…`** — they are what the screen
> showed then.

A row that runs out of room **between two narrow word runes** — an identifier, a
path component, a shell word — now ends with the product's own cut label, `…`
(`statusEllipsis` **at the time of this measurement; `answerCutLabel` today**), in
its last cell; the character the label displaced **opens the
row below**. Measured on a real pty (117×26), verbatim grid rows:

```
row 19: "▌ fix/pushback-rule31-evid:alpha-beta_gamma.deltafix/pushback-rule31-evid:alpha-beta_gamma.deltafix/pushback-rule31-…"
row 20: "  evid:alpha-beta_gamma.deltafix/pushback-rule31-evid:alpha-beta_gamma.deltafix/pushback-rule31-evid:alpha-beta_gamma"
row 21: "  .deltafix/pushback-rule31-evid:alpha-beta_gamma.deltafix/pushback-rule31-evid:alpha-beta_gamma.deltafix/pushback-r…"
row 22: "  ule31-evid:alpha-beta_gamma.deltafix/pushback-rule31-evid"
```

Row 19 ends `...pushback-rule31-…` and row 20 opens `evid:` — the `e` that was row
19's last cell is row 20's first. Row 21 ends `...pushback-r…` and row 22 opens
`ule31-evid` — the `u`. The band's rows concatenate to the answer exactly, once.

Three cases deliberately carry NO label, each because a label would lie:

* a line that ENDS there (nothing follows — nothing was cut);
* a row whose last character is an unbreakable WIDE one (that cell is the rune's own
  continuation column; taking it would leave the row short);
* a text boundary the source itself already offers (whitespace, punctuation) — the
  reader already sees that stop. The wrap SPACE consumed at a break is this case:
  a space is not a word rune.

### The byte/cell reconciliation (the deliverable)

The first attempt failed because the marker was a **drawn cell in the source
string**, which moved every byte offset the emphasis path is built on. The landed
reconciliation, all of it in `internal/lineedit/answer.go`:

1. **The class is decided on the SOURCE, never on the drawn cells.**
   `cutLabelFor(text string, cut int) bool` asks `noteBreakInsideWord(text, cut)` —
   the existing helper — over the row's own whole text at the row's own byte offset.
   A rule read off the drawn row would mislabel the moment emphasis (markers, SGR)
   moved the drawn cells away from the source bytes.
2. **The label stays OUT of the source.** `liveText` (the row's replay text) gets the
   head minus one character; the label is written to the grid and counted in
   `liveCol` (a screen column), never appended to `liveText`. A committed row is
   therefore still a slice of the answer, and the committed-row source comparisons
   keep working.
3. **The displaced character is the tail's first character** — no ledger, no second
   copy. `writeRow` re-cuts the row's tail as `text[len(displayed):]`, so the same
   call paints it on the row below. An earlier draft carried the character in a
   `cutHeadList` and re-seeded continuations; the cell-loss rig caught it as a
   duplication (a continuation of the SAME row would have prefixed the character
   twice), and removing the ledger removed the duplication.
4. **The span walk counts the row's DRAWN source** (`row.skip += len(displayed)`):
   `skip` is an offset into the row's text, and the row below starts its walk at the
   character the label displaced — counting the label's byte here walked the walk one
   rune past what the row below paints (measured: `TestEmphasisWrappedLinePaintsThe…`
   and the CJK marker-column case failed with the bold run's last cell plain).
5. **Rows whose text is a CARRY carry no label** (`answerRow.allowCutLabel`). A head
   painted provisionally, or a fragment replayed by `commitPrefix`, is a slice of a
   line whose full text the row does not hold; a break read off the slice would be a
   guess about a boundary the row cannot see. Set at the three construction sites
   that hold a whole source line (`rowsFor`'s composer-less path, `buildRow`,
   `buildRowFast`).

### Tests

* Four gated assertions in `internal/lineedit/answer_cut_marker_test.go` are LIVE —
  `cutRowMarkerOpenDefect` is `false` with **no skip**. The constant is kept as a
  NAMED seam (its comment now records the history and why flipping it back
  reintroduces the skip).
* The class test is not on the marker: `TestAnswerCut_MarkerIsTheCutLabel` reads the
  RAW grid; the stream-level claim (`…NoCellOfTheAnswerIsLost`) asserts the band
  still holds every character of the answer, in order, once.
* `cutReadingFold` (one helper) is the fold that readers of band-as-source-text use:
  `bandContents`, the cell-loss rig (`lossCommitted`, `dupBandRows`), the table-grid
  readers (`noSpace`, `contentRow`), the CJK marker-column reader, and the emphasis
  row-text walk. A rendering glyph is folded exactly as the emphasis markers already
  are.
* `internal/lineedit/emphasis_wrap_test.go` walks row text through `texts` (folded)
  while its attribute array stays the RAW grid's — the label is a row's LAST cell,
  so every column the walk visits keeps its own index.
* `TestA5B_WideInFlightLineIsFinalizedNotDropped` now asserts the WORD's cell total
  across the band (160 characters) instead of every row being exactly `a5Cols` wide:
  on a 160-character single word every cut lands mid-word, so a row's last cell is
  the label's. A row is still full; the word is still whole.
* NEW wire-level case `testsuite/chat_cut_pty_test.go`:
  `TestChatPTYCut_MidWordCutIsMarkedOnARealTerminal` — a real `gopi chat` child on a
  pty set to 117 columns with TIOCSWINSZ, asked (typed, not pasted) for a 400-character
  single token, asserting (1) a band row ends with `…`, (2) the row below opens with
  the character the label displaced, (3) folding the label away, the band IS the
  answer in order, once.

### A `cmd/agent` reader the fold MISSED — found by the whole-package run, fixed in this branch

The reconciliation needed every reader that compares the band against the answer's
own TEXT to fold the label. `internal/lineedit`'s readers were folded; **one reader
in `cmd/agent` was missed**, and only the whole-package run found it:

    --- FAIL: TestChatTurnDump_ReadsTheWholeAnswerOnARealPtyWithoutInput (0.01s)
        chat_turn_dump_grid_pty_test.go:356: the emphasized source line "01emph02emph…
        …emph34emph" is not painted on the band as a contiguous run of cells

The fixture (34 `emphNN` tokens) wraps mid-WORD, so a cut row's last cell is the
label's — the reading showed `…emph17…emph18…` where the source has `emph17emph18`.
The walk (`turnDumpAssertBoldSpansTheWrap`) locates the source span as a run of
cells and asserts EVERY cell of it is bold, so the inserted rune broke the match
**before** any style was read: an unmarked cut would have matched and the emphasis
across the wrap would still have been asserted.

The fix is the same fold shape, in the one place that keeps `chars()` and `cells`
in step — `turnDumpCellStream`'s cell builder, NOT `chars()`: the assertion indexes
`cells` by an offset it computes on `chars()`, so a drop applied to one and not the
other would slice by an offset the other was not counted with. `turnDumpCutLabel`
reads the glyph from the exported name of the one place that renders it —
`lineedit.StatusEllipsis` when this was measured, `lineedit.AnswerCutLabel` since
task `01a1a325` moved the band's mark to `↵` — never a second spelling of the rune —
Rule 14.

This is the pattern the brief predicted (`in tests that read the band as source
text, fold rendering glyphs away`), and it is worth recording WHICH instrument
caught the miss: not a unit test, not a scoped `-run` filter (mine matched
`TestChatDump`, which does **not** match `TestChatTurnDump_…`), but the full
`go test ./cmd/agent/`. A scoped filter is only as good as its pattern.

### Devdoc

This file. Part 1 (above) is the withdrawn attempt's root-cause measurement — the
evidence the landed design is built on; Part 2 (this section) is what shipped. Part
1's status line ("the fix is NOT in this branch") was true of that attempt and is
now history, which is exactly why it is kept rather than deleted: the design
decisions in Part 2 are answers to the failures Part 1 measured.

## Not Done (honest)

* **The label is drawn in the row's own trailing STYLE**, not the row's base style
  (`writeCutLabel` takes `row.st`). Inside an emphasised run this paints the label
  as emphasised (`[1m…`) rather than as the body. A monochrome palette — and every
  unemphasised answer, which is what `gopi chat` emits — shows the base style, which
  is why the shipped grid evidence and every unit case read plain. A future task
  that wants the label to close a marked run as body text while keeping its SGR
  bytes out of the row's span arithmetic needs `row.st` replaced by a style chosen
  from the piece the cut landed in; measured and stated rather than silently
  approximated.
* **The `--no-editor` path is unchanged and still unmarked.** `chat --no-editor`
  takes the historic buffered renderer (AssertCols + screen-side wrapping), which
  does not run the composer at all; the operator's terminal uses the editor path, so
  that is where the fix lives. Verified by measurement, not assumed: the same probe
  under `--no-editor` showed three hard row breaks with no label.
* **The narrow-fallback (below the reserve threshold) is out of scope** — with too
  few rows for the reserved answer band the renderer degrades by design, and this
  change deliberately does not touch that path.
* **A cut in a table row** takes the same path (writeRow is common) but no case was
  added for it; the table grid tests were run and are green, which is coverage of
  the path they already exercise.

## Verification

Build:
* `go build ./...` → OK (no output).
* `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → BUILD_OK (the binary the pty
  case runs).

Tests:
* `go test ./internal/lineedit/` → `ok github.com/nick/gopi/internal/lineedit 38.3s`
  (whole package, `-count=1`; includes the four cut cases, the emphasis family, the
  table grid family, the CJK marker-column case, the cell-loss rig and the A5B wrap
  guards).
* `go test ./internal/lineedit/ -run TestAnswerCut -count=1` → `ok … 0.9s`.
* `go test ./testsuite/ -run TestChatPTYCut_MidWordCutIsMarkedOnARealTerminal -v`
  → `--- PASS: TestChatPTYCut_MidWordCutIsMarkedOnARealTerminal (12.86s)`.
* `go test ./cmd/agent/ -count=1` → **`ok github.com/nick/gopi/cmd/agent 499.024s`**
  (exit 0, zero failures) — the whole package, and the leg that mattered: its FIRST
  run was `FAIL … 470.811s` with exactly one failure,
  `TestChatTurnDump_ReadsTheWholeAnswerOnARealPtyWithoutInput` (0.01s), which is the
  missed reader documented above; after the fold the case passes in isolation and the
  package is green.
  NOTE the shaping error that hid it: the first full run was launched as
  `go test ./cmd/agent/ 2>&1 | tail -30`, and `tail` swallowed both the `--- FAIL`
  line and the exit status — for a heavy package whose verdict you intend to quote,
  redirect to a FILE and grep it, never pipe through `tail`.

Ran:
* The real surface, at the operator's geometry:
  `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/ && GOPI_CUT_DUMP=1 go test ./testsuite/ -run TestChatPTYCut_MidWordCutIsMarkedOnARealTerminal -v`
  — a real `gopi chat` child on a pty, `TIOCSWINSZ` at `Col: 117, Row: 26`, the
  question typed keystroke by keystroke, and the child's own bytes fed to
  `lineedit.NewScreen(117, 26)`.

Result:
* PASS, and the grid rows quoted at the top of this report are the run's own output
  (row 19 marked, row 20 opens with the displaced `e`; row 21 marked, row 22 opens
  with the displaced `u`).

Evidence (self-filed):
* `go test ./cmd/agent/ -count=1` → the RED leg and the GREEN leg, same session, same
  tree: before the fold `FAIL github.com/nick/gopi/cmd/agent 470.811s` with
  `--- FAIL: TestChatTurnDump_ReadsTheWholeAnswerOnARealPtyWithoutInput (0.01s)`;
  after it the whole package is `ok github.com/nick/gopi/cmd/agent 499.024s` (exit 0).
  The 0.00–0.01s duration is the discriminator that rules out load: a wall-clock
  failure would not fail in a hundredth of a second.
* Attribution of the FIRST full-package run (the one whose failure line `tail` ate):
  the two host-shaped causes this repo documents for a red `./cmd/agent` on this host
  were each run IN ISOLATION and both PASS —
  `go test ./cmd/agent/ -run 'TestCwdReleaseRig|TestTmpDirSettingsValueReachesTheSpawnedChild' -count=1`
  → `ok … 17.711s` (the tmpdir case printing its own measured device ids, and the
  cwd-release rig passing). So neither host cause explains a red run, and the
  remaining failure was the diff's — which is why it was chased to a name rather
  than filed as "pre-existing". The precondition those notes name (a built
  `bin/gopi` for the CLI E2E guards) also held: `bin/gopi` present, 28 MB, built
  07:46.
* `go test ./internal/lineedit/` → `ok … 38.320s` (whole package green).
* pty case PASS at 117×26 with the grid rows pasted above; the case's own assertion
  3 (`got.String() != token` is the failure arm) proves the band equals the 400-char
  token character for character, so "no cell of the answer is lost" is measured at
  the wire, not argued.
* The four gated assertions were RED before the change and are green after, in the
  same session: the pre-change run printed
  `--- FAIL: TestAnswerCut_MidWordBreakIsMarked … row 0 runs out of band mid-word and wears no marker`,
  and the post-change run is `ok`.

## Files

* `internal/lineedit/answer.go` — `cutLabelFor`, `cutLabelCell`, `runeAtEnd`,
  `runeCount`, `writeCutLabel`, `answerRow.allowCutLabel`, the `writeRow` label path,
  the `row.skip` count.
* `internal/lineedit/answer_cut_marker_test.go` — `cutReadingFold`, the seam comment
  for `cutRowMarkerOpenDefect` (now `false`, no skip).
* `internal/lineedit/answer_test.go`, `answer_wrap_loss_test.go`,
  `answer_wrap_scroll_dup_test.go`, `emphasis_wrap_test.go`,
  `marker_column_narrow_test.go`, `table_grid_test.go` — the fold wired into each
  band-as-source reader; the A5B word-total assertion.
* `cmd/agent/chat_turn_dump_grid_pty_test.go` — the missed band-as-source reader:
  `turnDumpCutLabel` + the drop in `turnDumpCellStream`'s cell builder.
* `testsuite/chat_cut_pty_test.go` — NEW: the real-pty wire case.
* `devdoc/answer-row-cut-vocabulary.md` — the reconciliation section amended to the
  landed design; the stale PARTIAL status corrected.
