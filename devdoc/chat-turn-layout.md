<!-- doc-class: historical -->
# Task Report: readable turn structure + aligned status columns in the text UI

## Completed

**Turn structure — a turn is a visible block, not a run of indistinguishable lines.**

- `internal/lineedit/answer.go`:
  - `TurnHeader(info, cols)` / `TurnRule(cols)` / `turnLabel` — the turn's chrome.
    A header is `──── turn N · <prompt> ─────────…` (rule lead, label, rule tail);
    a closing rule is the same glyph run with no label.
  - `AnswerRenderer.BeginTurn(TurnInfo)` / `EndTurn()` — opens a turn (commits the
    header through the **same** live-row/commit path the answer uses, so chrome
    inherits scrollback safety instead of adding a second writer) and closes it
    (commits the rule AFTER the tail — a rule committed first would land above it).
  - `AnswerRenderer.BeginThinking()` / `EndThinking()` / `ThinkingWrote()` — the
    reasoning block: reasoning lines carry `ThinkingGutter`, and closing the block
    commits exactly ONE blank row so the answer is visibly separated from the
    reasoning. An empty reasoning block adds no blank row.
  - A **marker column** on every committed body line (`TurnMarker` = `▌ `, 2 cells;
    `TurnMarkerSplit` for continuation lines, equal width) — one shared left edge
    per turn, wrapping rows included (`writeGutter`/`lineGutter`).
  - `TurnInfo` is the turn's identity (index + prompt), so the header and the
    status row's `iter N` get their number from the same counter.

- `cmd/agent/cli_chat.go`: `renderer.BeginTurn(iterCount+1, msg)` before the turn,
  `renderer.EndTurn()` after `Finish()`; `OnThinking` now routes to
  `renderer.Thinking(delta)` so reasoning is guttered rather than concatenated
  with the answer.

**Status column alignment — the fields hold their cells while the values change.**

- `internal/lineedit/answer.go`:
  - `StatusColumn` + `Status.Columns`, `Status.LiveWidth`, `Status.LayoutCols`,
    and the exported `Status.Line(cols)` — the row's composition made assertable
    without re-deriving the layout arithmetic.
  - `statusLineColumns` renders the ALIGNED form: every column at its reserved
    width (including a valueless one, tiled as padding so no later column slides),
    live hint right-aligned inside its own reserved block.
  - `alignedFits` — the form is chosen from the **terminal width alone**, never
    from today's values.
  - `StatusSepCells` / `StatusColumnSepCells` / `StatusLineGapCells` — the
    separators' widths **in cells**, with a test tying them back to the strings.

- `cmd/agent/chat_stream.go`: named reserved widths (`statusModelCol` 16,
  `statusIterCol` 7, `statusModeCol` 5, `statusToolCol` 20, `statusTokensCol` 21,
  `statusLiveCol` 12), `statusColumnsAtCols` (the pinned geometry, 99), and a
  compile-time budget guard `const _ = uint(statusLayoutBudgetCols - statusColumnsAtCols)`.
  The token field's heavy-context marker is now `statusTokensHeavySuffix` (was an
  inline literal).

**One real defect found and fixed during verification** (it was in the first attempt
at this work, and it was not visible without the grid capture):

1. **The pinned status geometry was arithmetic on BYTES, not cells.**
   `statusColumnsAtCols` summed `len(lineedit.StatusColumnSep)` — 5 bytes for a
   4-cell string — computing a 103-cell layout on a 100-cell terminal. The aligned
   form only runs on a terminal at least as wide as its pin, so the alignment
   **never appeared on the width it was designed for**, and the row silently fell
   back to the historic unaligned composition. Fixed by adding the cell-width
   constants and asserting the geometry against `statusLayoutBudgetCols` at compile
   time.

A second, related defect was in the column LIST rather than its arithmetic: the list
was built conditionally (the token column was appended only when `tokens > 0`), so
the field appearing at a turn's end added a column and pushed the row past its own
pin — the same silent flip, one repaint later. The list is now a constant of the
layout: every column is present on every repaint, and a valueless one renders as
its own width in padding.

**Correction (recorded rather than edited away):** an earlier draft of this report
claimed a third fix — that `Finish()` dropped an unpaainted tail when
`liveText == ""`. Re-checked against the diff: **`Finish()` is not modified by this
branch at all** (`git diff -U0 internal/lineedit/answer.go` adds nothing inside it;
the function is byte-identical to master). The claim was a misreading of the
pre-existing code and is withdrawn. The observed symptom that prompted it — turn 1's
answer not being displayed until turn 2 began streaming — is explained by the
missing turn chrome (the answer WAS committed, but with nothing marking where the
turn ended, the band read as one continuous stream), which is what this branch fixes.

## Not Done

- **A concurrency/race sweep of the new renderer entry points.** `BeginTurn` /
  `EndTurn` / `Thinking` are exercised single-threaded by the new tests; no
  `-race` pass was run over them specifically.
- **The status row's right edge is not padded.** The aligned row ends at the live
  hint's text (its block's trailing padding is trimmed), so the row measures 92 of
  its 99 pinned cells. The COLUMNS hold (that is asserted); only the trailing
  space run is not drawn. Padding the tail is a cosmetic follow-up, deliberately
  not done here because trimming avoids writing cells the terminal would have to
  repaint.
- **No capture of a `--no-editor` / piped-stdin run.** The degradation contract
  ("no turn chrome without a turn counter") is asserted by unit test only; the
  scripted grid evidence covers the reserved-layout path alone.
- **Pre-existing `cmd/agent` failures NOT fixed — audited, not assumed.** A full
  unfiltered symmetric audit (commit `e03a48e8`) found **8 tests failing
  identically on both this branch and pristine master `4aea3c76`**:
  `TestCompletionCandidateSetsMatchGroupDispatch`,
  `TestEveryHelpConstantFitsTheWrapBudget`,
  `TestLedgerOpenersUseProductionDSNContract`,
  `TestMergeCeilingFiresAndFilesReworkTask`,
  `TestMergeCeilingIsIdempotentAcrossRuns`,
  `TestMergeSmallConflictStaysUnderTheCeiling`,
  `TestMergeWithoutFastStillHandsTheDivergedBranchOver`,
  `TestProcStatParseSingleHomeDetector`.
  **Branch-attributable regressions: 0** (no test fails in isolation on this branch
  but not on master).
- **The `cmd/agent` suite is non-deterministic on this host — recorded, NOT fixed.**
  Two identical runs of the SAME branch binary gave 15 and 79 failures with
  disjoint sets, so a single full-suite run cannot attribute a failure to a diff.
  That defect deserves its own task; it is outside this layout diff's scope. (It is
  also what produced this doc's first, wrong, failure count.)

## Verification

- Build: `CGO_ENABLED=0 go build ./...` → exit 0. `go vet ./internal/lineedit ./cmd/agent` → OK.

- Tests (branch-scoped): `go test ./internal/lineedit/ ./cmd/agent/ -run 'Turn|Status|Chat'`
  PASS. Full branch-scoped budget run: see Evidence.
- Grid capture: `~/workspace/gopi-turn-layout-evidence/` — `before-tool-ui-*.grid.txt`
  (master binary) vs `after-tool-ui-*.grid.txt` (this branch), same script, same
  mock LLM, same 100×30 PTY, only the binary changed.

### BEFORE → AFTER (the measured defects)

BEFORE `before-tool-ui-5-answer2.grid.txt` (turn 2 streaming) — turn 1's answer and
turn 2's answer run together with nothing between them, and `Done.` of turn 1 sits
directly above turn 2's first paragraph:

```
10|Done.
11|Here is the answer with a deliberately long unwrapped paragraph so the renderer's wrapping behaviour
...
28|you>
29|deepseek-flash · iter 2 · tools · 34 tokens  thinking…
```

AFTER `after-tool-ui-5-answer2.grid.txt` — a closing rule, then a numbered header
naming the turn and repeating the prompt, then the marker-aligned body:

```
 9|────────────────────────────────────────────────────────────────────────────────────────────────────
10|──── turn 2 · and what did the second answer change? briefly ───────────────────────────────────────
11|▌ Here is the answer with a deliberately long unwrapped paragraph so the renderer's wrapping behavio
12|  ur is visible in the capture: the quick brown fox jumps over the lazy dog, and then it does it aga
...
25|▌ Done.
26|────────────────────────────────────────────────────────────────────────────────────────────────────
27|
28|you>
29|deepseek-flash   ·  iter 2  ·  tools ·                       ·  34 tokens              thinking…
```

Column starts, read off the real 100-column grid (`field start` in display cells) —
**identical in every captured state**, including the token-present states that
previously flipped the row to the unaligned composition:

```
after-tool-ui-3-answer    model@0  iter@20  tools@31  —       live@87  (thinking…)
after-tool-ui-4-turn2     model@0  iter@20  tools@31  tokens@67  live@87  (ready)
after-tool-ui-5-answer2   model@0  iter@20  tools@31  tokens@67  live@87  (thinking…)
after-tool-ui-6-quit      model@0  iter@20  tools@31  tokens@67  live@87  (ready)
```

vs BEFORE, where the row was a single concatenation and the fields moved with their
values: `before-tool-ui-3-answer` = 77 cells, `before-tool-ui-4-turn2` = 50 cells,
`before-tool-ui-5-answer2` = 54 cells (`deepseek-flash · iter 1 · tools · ⚙ read
/home/user/work/GoPi/dev…  thinking…` — no reserved column anywhere).

Editing line and status row are intact in every AFTER capture: row 28 is always
`you>` / `you> bye.` and row 29 always carries the status — no chrome row ever
moved them.

### Evidence (self-filed)

- `go test ./internal/lineedit/ ./cmd/agent/ -run 'Turn|Status|Chat' -count=1` →
  `ok  github.com/nick/gopi/internal/lineedit` / `ok  github.com/nick/gopi/cmd/agent`.
- `go test ./internal/lineedit/ -run 'TestStatusSeparatorCellWidths|TestStatusAlignedLayoutFitsARealTerminalWidth' -v`
  → both PASS (red before the cell-width fix: the aligned row measured 12 cells of
  a 96-cell pin — the fallback — and 92 of 99 after).
- Baseline audit for the pre-existing failures (unfiltered + isolated, symmetric on
  both sides), logs in `~/workspace/gopi-turn-layout-evidence/`:
  `baseline-master-4aea3c76.log` (master full run → 15 failures),
  `branch-full-rerun2.log` (the SAME branch binary re-run → 79 failures, disjoint
  sets ⇒ the suite is non-deterministic),
  `isolated-master-common8.log` / `isolated-branch-common8.log` (8 tests fail
  identically on BOTH sides), `isolated-master-common7.log` (the 7 tests seen only
  in the branch's full run pass in isolation on BOTH sides — `is not a git work
  tree` against a shared `~/workspace/gopi-test-scratch/suite-cmd-agent-run-*`
  scratch repo = suite-scratch interference). Conclusion: 0 branch-attributable
  regressions.
- Grid: `~/workspace/gopi-turn-layout-evidence/after-tool-ui-*.grid.txt` (this branch)
  and `before-tool-ui-*.grid.txt` (master), both captured by `capture.sh` at 100×30
  with `fakellm.py`.

## Files

- `internal/lineedit/answer.go` — turn chrome, thinking gutter, marker column,
  status columns (`StatusColumn`, `Status.Columns/LiveWidth/LayoutCols`,
  `Status.Line`), separator cell-width constants.
- `internal/lineedit/turn_layout_test.go` (new) — 9 grid/cell assertions.
- `internal/lineedit/render.go` — reverted an unused export; unchanged in the end.
- `cmd/agent/chat_stream.go` — reserved column widths + pinned geometry + compile-time
  budget guard; status source emits a value-independent column list.
- `cmd/agent/chat_turn_layout_test.go` (new) — 2 REPL-seam tests.
- `cmd/agent/cli_chat.go` — BeginTurn/EndTurn/Thinking wiring.
