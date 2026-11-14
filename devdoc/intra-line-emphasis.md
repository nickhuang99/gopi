<!-- doc-class: historical -->
# Task Report: intra-line emphasis (`**bold**`) — one span layer, two carriers

Branch: `task/01a0d2f84b637ee28aa67178ac20337f`, commit `5ef5a997`, off master
`99f6bbf3`. This is the task report for the epic whose goal was: extend the
EXISTING `LineStyle` layer from line-level to span-level, so `**bold**` renders
as emphasis on `gopi ui` (internal/uiclient) and is recognised from the same
semantic naming by `gopi web` (cli_web's minimal markdown) — with no new render
layer, no new dependency, and colour-off output byte-identical.

## Completed

### 1. `internal/lineedit/emphasis.go` — the tokenizer (new file, the one place `**` is spelled)

- `Emphasis` (`EmphasisNone`/`EmphasisBold`), `LineSpan{Text, Emphasis}`,
  `LineSpans(line)` — a PURE tokenizer.
- `BoldMarker = "**"` is defined HERE and nowhere else. Measured, precisely:
  `grep -rn '"\*\*"' internal/ cmd/ --include=*.go` (excluding tests) returns
  the definition (`internal/lineedit/emphasis.go:52`), a literal in
  `internal/workerownership/workerownership.go:282` (`return "> **" + Line +
  "**" + OwnerTail` — the WORKER-OWNERSHIP annotation's own formatting of a
  devdoc banner, a different concern that never touches an answer body), and
  three comments. No renderer spells it: the renderers receive spans.
- Grammar (one rule): a marker-open is a `BoldMarker` followed by text, and the
  span ends at the NEXT `BoldMarker`. **Unclosed → literal** (`**hi`), **empty
  pair → literal** (`****`), no-marker line → one span (the fast path every
  ordinary answer line takes).
- `SpanText`/`SpanLine`/`DisplaySpans`/`DisplayText`/`HasBoldMarker`/
  `SpanStyle(pal, em)` — `SpanStyle` is the ONE emphasis→style mapping, shared by
  both carriers rather than duplicated.
- `Palette.AnswerBold = Style{open: sgrBold}` (`BasicPalette`); the monochrome
  palette leaves it zero, so a span under it renders as its own plain text.
- `fitCommitRow`/`withHead`: a commit reconciles the row with what the live row
  ALREADY painted. A partial line paints its markers literally, the decided line
  drops them, so the head can be wider than the row it was painted on — the row's
  cell arithmetic is computed from head+line, never from line alone (a 5-cell
  `**bold**` head measured against a 2-cell `bold` row is the shear this prevents).

### 2. `AnswerRenderer` — span-level styling end-to-end

- `answerRow` carries `spans []LineSpan`; the two forms (one style / spans) are
  exclusive.
- `rowFor(line, ls)` — on a **plain** line the tokenizer decides the spans; on a
  code/diff line the composer's verdict wins, so `**` inside a fence is the
  literal the code block says it is. The gate is the composer's verdict, not a
  marker test in the renderer.
- `rowForPartial` — an unfinished line is painted **provisionally, with its
  markers intact**: removing a marker on a split point would commit to a decision
  the next burst can revoke (`**bo`→`ld**` is bold; `**bo`→`ld` is literal), and
  a reader's letters may not change when the next frame arrives. The line is
  decided ONCE, when its break arrives.
- `writeRow` writes per-piece bytes; `liveText`/`liveCol` carry **plain** text
  only, so an escape byte can never enter the cell arithmetic.
- `appendPiece` COALESCES same-style neighbours — required, not an optimisation:
  under a monochrome palette every style is the empty one, so splitting without a
  style change would put extra cursor motion on the wire and break byte identity.

### 3. `internal/uiclient` (`gopi ui`)

- The transcript `entry` carries `spans []answerSpan` (`lineedit.Emphasis` is the
  vocabulary — no second enumeration, Rule 14); `spansFor` returns **nil** for a
  row with no marker, which is the plain path (byte-identical to pre-change).
- The row's spans are rebuilt on every delta, because a marker can arrive in
  halves; the tokenizer is pure so asking again is cheap and stable.
- `wrapEntry` wraps by span through ONE `spanCursor` for the whole row (a run the
  wrap splits keeps its emphasis on the next line); `plainText()` is the DRAWN
  text every measurement works on, so markers occupy no cell.
- `Config.Palette` is the CALLER's capability decision — `cmd/agent/cli_ui.go`
  passes `lineedit.PaletteForEnv(false, out)` against the real tty fd (not the
  counting writer, which is not a tty by construction).
- `model.View()` stays fully readable as text (no marker, no escape byte under
  monochrome) — `TestEmphasisViewIsReadableAsText` logs the frame.

### 4. `gopi web` — the same semantic naming

- `webMarkdownBold = lineedit.BoldMarker` (NOT a second spelling) and the page's
  `boldSpanSplit` implements the terminal tokenizer's own rules; a code span wins
  over emphasis, exactly as a fence wins in the terminal.
- `TestWebM2B_PageBoldMarkerIsTheTerminalTokenizersOwn` pins the Go↔JS boundary:
  the page's code point, its length and its construction from `MD_ASTERISK`.

## Not Done (honest)

- **Only `**bold**`**: no italic, links, lists or HTML — by the epic's own scope.
- **`gopi web` needs a browser to be seen**: the page leg is verified by the
  shipped page's own node harness (`TestWebM2B_PageHarnessRunsTheRealPage`,
  136/136 checks, including the new bold checks) and by the constant pin; no
  real-browser screenshot was taken.
- **`cmd/agent` is not green as a package** — see `## Verification`. FIFTEEN
  tests fail and **all fifteen are reproduced on a pristine HEAD tree**
  (`git archive HEAD` → `before-tree/`, `GOPI_BIN=gopi-before`). Two families:
  (a) the G5/CLI help-surface four, failing with the SAME message on the
  pristine tree; (b) the resume/worktree/task-run family, which fails as a
  function of the BINARY PATH: with `bin/gopi` built from this worktree the
  own-ground guard refuses to delete the worktree the child is standing in
  (`own-ground refusal (arm guestRowOwnCheckoutRefusal)`), and the pristine
  binary reproduces it identically outside `bin/gopi`. Neither family is in this
  change's diff — no resume, worktree, help-surface or CLI-verb file was
  touched. `internal/lineedit` and `internal/uiclient` — the packages this epic
  owns — are GREEN.
- The colour-off RAW pty streams of a marked and an unmarked answer are not
  byte-identical to each other (different frame counts from the stub's timing,
  and a per-run scratch path in the banner) — the byte-level claim is made on the
  **answer row**, where it is exactly true (identical sha256), and on the
  renderer capture, where the whole stream is compared
  (`TestEmphasisColourOffBytesAreUnchanged`).

## Verification

- Build: `CGO_ENABLED=0 go build ./...` → exit 0; `go build -o bin/gopi ./cmd/agent/` → exit 0; `go vet` on the three touched packages → exit 0.
- Tests: `GOPI_HEAVY_SKIP_LEDGER=… scripts/test-budget.sh ./internal/lineedit/... ./internal/uiclient/... ./cmd/agent/` → `outcome=fail`, elapsed 497s, load 4.50→5.35 on 4 cores. `internal/lineedit` PASS, `internal/uiclient` PASS, `cmd/agent` FAIL with 15 named failures, **all 15 reproduced on the pristine HEAD tree** (see below).
- Ran (the surface this change owns, verbatim):
  `python3 drive_bold.py ./bin/gopi http://127.0.0.1:18099 colour-on.raw --expect BOLD`
  against an SSE stub (`stub_server.py`) serving `/api/chat` with
  `delta` frames `**bo` | `ld** is the ` | `**row shear** ` | `case.`
  under a real pty (`pty.fork`, 200x100, `TERM=xterm-256color`).

## Evidence (self-filed)

**1. Before / after, the same stub answer, on the reconstructed screen.**

| binary | answer row on screen | `ESC[1m` in the stream |
|---|---|---|
| `gopi-before` (pristine HEAD `99f6bbf3`) | `gopi> **bold** is the **row shear** case.` | 0 |
| this branch (`5ef5a997`) | `gopi> bold is the row shear case.` | 2 |

The before binary is built from `git archive HEAD` into a scratch tree
(`go build -o gopi-before ./cmd/agent/`), so the comparison is the code, not a
recollection. Marker count on the answer row: 2 → 0; the words are unchanged.

**2. StyledLine — the bold cell range, measured on the attribute grid.**

`go test ./internal/lineedit/ -run TestStyledLineMarksExactlyTheBoldCells -v`:

```
emphasis_test.go:237: StyledLine: row 6 text="the bold word" | bold cells [4,7] of 13 cells | grid line="the bold word"
```

`the **bold** word` → the drawn row is `the bold word` (13 cells — the markers
took NO column) and exactly cells 4..7 carry Bold. On the marked-turn shape
(a two-cell `▌ ` gutter):

```
emphasis_test.go:303: marked turn: row 5 text="▌ the bold word" | gutter 2 rune(s) | bold cells [6,9] of 15 | grid line="▌ the bold word"
```

The gutter's cells are not bold; the span's range shifts by the gutter and by
nothing else — the guard for "a tokenizer-removed marker was counted as a cell".

**3. Colour-off sha256.** The renderer capture, `MonoPalette`, one answer with
`**` and the same answer without:

```
go test ./internal/lineedit/ -run TestEmphasisColourOffBytesAreUnchanged   → PASS
```

The test compares the two captures as bytes (`a != b` → fail; no SGR may appear).
On the REAL surface, the answer row's bytes and their sha256 — colour off
(`NO_COLOR=1`), the marked stub answer vs the unmarked one:

```
colour-off-marked : answer row b'gopi> bold is the row shear case.'
                    sha256 2efb69414882c0aa47aa3506e048257c98620199eb6c5ea107afde0497f62e1d
colour-off-plain  : answer row b'gopi> bold is the row shear case.'
                    sha256 2efb69414882c0aa47aa3506e048257c98620199eb6c5ea107afde0497f62e1d
                    raw stream ESC[1m count = 0
```

Identical. The colour-ON run of the same answer carries `ESC[1m` twice
(`bold` and `row shear`) and draws the same characters.

**4. Streaming across frames.** `**bo` + `ld**` (two `delta` frames) is the stub's
first answer above and renders bold in one go: the screen shows `bold`, never
`**bo`. Its mirror — `**bo` + `ld` never closed — stays literal
(`TestEmphasisStreamedAcrossFrames/never_closed_stays_literal`, and
`**hi` in the tokenizer table).

**5. Fence parity.** `TestEmphasisInsideAFenceStaysLiteral`: `\`\`\`\na **b** c\n\`\`\``
keeps its literal markers and NO cell is bold, while the prose line right after
drops them and marks exactly one cell — the same rule the page implements.

**6. Pre-existing failures, isolated.** The same `cmd/agent` invocation against
the pristine HEAD tree (`git archive HEAD`, `GOPI_BIN=gopi-before`) reproduces:

```
$ cd before-tree && GOPI_BIN=…/gopi-before go test ./cmd/agent/ -count=1 -run '<the 15 names>' 
--- FAIL: TestCLIPositionalSubmitRunsThroughDispatcherE2E | TestG5InventoryMatchesTheHelp
          TestManualRunRecyclesItsWorktree | TestResumeReclaimsTheWorktreeItFinishedIn
          TestTaskRunChildNonZeroExitClosesWrapperRowE2E | TestTaskRunChildReceivesSummaryAndPayloadE2E
          TestTaskRunExplicitResumeClaimedStillWorks | TestTaskRunNormalExitSingleRowE2E
          TestTaskRunSeamWritesReportAndShowRendersIt | TestTaskRunSingleRowE2E
          TestTaskRunSurvivesClosedStdoutPipe | TestWorkerRunRecordsTheMissingRanLineE2E
FAIL	github.com/nick/gopi/cmd/agent	29.838s
```

`TestG5InventoryMatchesTheHelp` fails on the pristine tree with the same message
("the inventory lists flags [no-editor tools verbose], the help advertises
[no-color …]") — neither file is in this change's diff. The resume/worktree family
fails on the pristine tree too once `GOPI_BIN` names a binary built OUTSIDE
`bin/gopi`; the guard's own sentence is in that output:
`own-ground refusal (arm guestRowOwnCheckoutRefusal): the worktree … is the
checkout THIS process is standing in`. Notably `os.Stat(wt)` reports the worktree
still present, and `git status --porcelain` in this worktree printed nothing.

**7. The answer to "can `gopi ui` read markdown bold now?" — YES.**
The strings it reads are markdown-free by the time they reach the reader: `**` is
consumed by the tokenizer, and the drawn text (`bold`, `row shear`) carries the
emphasis as terminal attributes. Colour on: two `ESC[1m` runs with no visible
marker. Colour off: the same characters, zero SGR, byte-identical to the answer
written without any emphasis. See §1 §3.

## Files

`internal/lineedit/emphasis.go` (new, 279L), `internal/lineedit/emphasis_test.go`
(new), `internal/lineedit/answer.go`, `internal/lineedit/compose.go`,
`internal/lineedit/palette.go`, `internal/uiclient/model.go`,
`internal/uiclient/run.go`, `internal/uiclient/emphasis_test.go` (new),
`cmd/agent/cli_ui.go`, `cmd/agent/cli_web.go`, `cmd/agent/cli_web_m2b_test.go`,
`cmd/agent/cli_web_page_scenarios.js`, `cmd/agent/cli_web_page_harness.js`.

## §4 The carried head: a provisional paint's markers (2026-09-27)

**The symptom on the operator's screen.** `gopi chat` showed markdown's
intra-line emphasis LITERALLY — a row read

```
▌ So the answer is **not** a missing knob — it is a single reader.
```

with the marked run carrying no emphasis at all (that row's attributes were one
plain fg run). Every §1 §3 unit case was green.

**Reproduced, then named.** A pty at 117x30 (`probe/drive.py`) drove the real
`gopi chat` against an offline SSE stub (`probe/sse_stub.py`) streaming a known
answer in 6-byte deltas — the operator's chunk shape, and the shape their terminal
actually receives. The reproduction crossed the defect on the FIRST run.

Two conditions are both necessary, which is why the unit cases never saw it:

1. **The cadence window.** `answerFlushWindow` (30ms) is what makes an unfinished
   line reach the live row at all: a flush paints it PROVISIONALLY, and that paint
   is literal — `rowForPartial` has not decided the line yet, so a `**` is drawn
   as two cells (removing a marker is part of a decision not yet taken).
2. **A stream whose deltas split the marker pair.** With the whole line in one
   flush the provisional row and the committed row are decided from the same
   string and agree. Split, they disagree — and the commit carried the RAW head.

Measured boundary: red for every delta size 1..16 bytes, green at 32. That is why
`TestEmphasisStreamedAcrossFrames` (§1) was green over a live defect: its deltas
arrive microseconds apart, so the flush window never opens and the head it tests
with is always `""`.

**The mechanism.** The commit prefixes the row being committed with the text the
live row already shows (the "head", `fitCommitRow`/`withHead`). The head was the
raw spelling; the row was the decided text. The glued result drew the head's
markers as literal cells on a committed band row, where nothing ever repaints
them. The cell arithmetic had the same split: a `**bold**` head was measured by
its spelling's width (8 cells), not the drawn run's (4).

**What changed** (`internal/lineedit/`, commit `9c6b8da8`):

| seam | rule |
|---|---|
| `headSpans` | the head is carried as SPANS, so its emphasis survives the carry |
| `headIsInRow` | a head whose text the committed row already holds is prefixed NOTHING (the `compPrefix` rejoin case) |
| `commitLines` | a fragment split INSIDE an emphasis pair is rejoined and decided as the WHOLE line (the tokenizer answers about a balanced pair) |
| `commitPrefix` | the carry tests everything queued for the commit — `prefix` + the parked fragment — not just the finished-line buffer |
| `commitLines` | a provisional paint WIDER than the row committed over it leaves the cells past that row cleared (`escEraseToEnd`, `sgr`'s own sequence) |

**Residual, NOT fixed here — a line that WRAPS.** A line long enough to fill the
band row is finalized by `writeLive` as a provisional row (markers literal), and
the remainder continues on the frontier. So a wrapped line still shows its markers
on the wrapped part and can repeat a fragment (measured on the 60-column probe:
`The key point is that **the config resolver reads exactly on` /
`e source**, a source**, and`). The operator's screenshot — a line visibly
truncated with `...` — is consistent with this shape. It is dispatched as its own
task rather than guessed at here: each wrapped row is committed as it fills, so
the decision has to be taken BEFORE the wrap, which is a different seam from the
one this fix repairs.

**Evidence.** `probe/run1` (before) vs `probe/run2` (after):
row 13 goes from one plain run to `fg=colour-178 bold` over the marked span, its
markers gone; rows 15/16 likewise; `probe/drive.py` + `probe/sse_stub.py` are the
harness. Row 20 changes style between the two binaries on the FINAL-line path
(`Finish` carry) and needs the wrapped-line work to be pinned honestly.
