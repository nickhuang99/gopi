<!-- doc-class: historical -->
# The agent's own flow output reaches the layout's single writer

Branch `fix/chat-turn-flow-band` (off `feat/chat-help-status-rows`, tips
`256d7ea0`), worktree `/home/user/workspace/gopi-agent-band`. Task 01a0c0a7.

## Completed

The residual this closes: the REPL's OWN text already streamed through the
reserved layout's answer band (`lineedit_wire.go` `WriteLines`/`WriteNotice`,
branch `feat/chat-help-status-rows`), but the **AGENT's** turn-time flow output
still wrote to `os.Stderr` — the descriptor the editor was painting on — so it
landed on the layout's rows.

### 1. The seam (`internal/agent`)

- `Agent.OnThinking func(string)` — new optional callback, mirroring the
  existing `OnText`. Nil (the default) keeps the historic `os.Stderr` rendering
  **byte-for-byte**.
- `(*Agent).flowWriterFor() io.Writer` — the ONE place that decides the
  destination: the seam when one is installed, `os.Stderr` otherwise.
- The three chat-visible flow sites in `chatOnceAttempt` re-pointed at it: the
  non-streaming fallback render, the `markers.open`, the reasoning deltas, and
  the two `markers.close` sites (text-delta arm and the deferred close). The
  request builder's `float32` field and every other write are untouched.
- `internal/agent/thinking_seam_test.go` pins the contract: the same turn run
  twice (stderr capture vs seam) must produce the **same bytes**, and with no
  seam the historic stderr rendering is what comes out.

### 2. The carrier (`cmd/agent`)

- `chat_flow_seam.go` — `chatFlowSeam`, the object the agent's flow output is
  handed to. It:
  - receives `Agent.OnThinking` (the reasoning stream, markers included) and
    **buffers the unfinished tail**, committing whole rows — one row per
    fragment would stack `Let` / `me` / `think` on three band rows. The bound is
    `flowRowBufferCells` (named, measured in cells via the newly exported
    `lineedit.DisplayWidth`), so a stream that never sends a break still shows
    its text;
  - is an `io.Writer` for the agent's `log.Logger`, so a `[gopi] …` notice
    flushes the pending reasoning row FIRST and then takes a row of its own —
    the row can never carry two sources;
  - `Flush()` at a turn boundary (called from the REPL next to
    `renderer.Finish()`), so a reasoning block whose last line carried no break
    is not left invisible.
- `installChatFlowSeam(ag, input)` — installs both destinations (the reasoning
  seam and the logger's writer) ONLY when the editor path is live, and returns
  the restore. The logger's previous writer is captured and restored, not
  assumed to be `os.Stderr`.
- `chatInput.bandActive()` / `writeBandRow()` — the ONE definition of "does the
  reserved layout own the screen" (Rule 14): every branch of the seam asks it.
- `internal/lineedit`: `Editor.BandActive()` and `Editor.WriteBandRow(row)`. The
  earlier `WriteBandChunk` was REPLACED, not kept alongside: it was an interim
  name for the same method, and leaving both would be two spellings of one
  policy. `WriteBandRow` adds **no decoration** to the row — an earlier revision
  wrapped it in `│ … │`, which made the band display a notice differently from
  every other row; the panel's own title decoration is a separate concern and
  was reverted with the axis-mismatch finding below.
- The per-turn renderer now takes the input layer (`newChatTurnRenderer(input)`)
  instead of a resolved `(band, out)` pair, so the answer band and the flow
  fallback cannot disagree about which stream is in charge.

## Not Done

- **Rows longer than the band still WRAP as fragments, not as whole rows.** The
  seam commits one row per SOURCE LINE, and the layout splits a row wider than
  the band across as many terminal rows as it needs (`region.writeBandLine`'s
  `cutCols` loop). A wrapped row therefore hands the layout's split points to
  the terminal, so a very long flow line can wrap mid-word — the text is intact
  and readable, but the row boundary is the layout's, not the source's. Fixing
  it means measuring the chunk against the band's width in the seam (it has
  `Editor`'s columns available through the region) and cutting with the same
  cell-width rule. Left as-is deliberately: it is a rendering refinement, not a
  tear, and it is the layout's documented trade-off already (the band's own
  answer path wraps the same way).
- **Two pre-existing failures on this branch are NOT fixed here** (both
  reproduced on the base worktree `gopi-band-base` at `256d7ea0`, before any of
  my edits, and my branch makes the count SMALLER, not larger):
  - `internal/agent` `TestDocAnchorsResolveInNormativeDocs`: 5 `:190` errors on
    the base, 5 on my branch, byte-identical modulo the worktree path
    (`devdoc/cov-shot-hp.md` ×2, `merge-ownership-requirement.md`,
    `test-home-isolation-read-tilde.md`, `verify-boundary-audit-20260912.md`).
    My `loop.go` edits DID shift 8 anchors in 5 other docs (`hippocampus.md`,
    `IMMEDIATE-FEEDBACK-REGRESSION.md`, `agent-five-organs-ontology.md`,
    `review-cli-config-options.md`, `review2-cli-config-options-variant-*.md`,
    `system-prompt-review-2026-09-07.md`) — those are re-keyed in this commit,
    measured against the base worktree, and the gate now reports only the
    pre-existing five.
  - `cmd/agent`: `TestCompletionCandidateSetsCoverTopLevel`,
    `TestTaskListReadPathE2E`, `TestSweepZombiesCLI_DryRunTouchesNoNotifySink`,
    `TestSweepZombiesCLI_ReportNotifiesOperator`,
    `TestTopLevelHelpCoversEveryDispatchedSubcommand` — all five fail
    identically on the base worktree at `256d7ea0`.
- Nothing was merged; the branch is the deliverable.

## Verification

- Build: `CGO_ENABLED=0 go build ./...` → OK; `go vet ./cmd/agent/
  ./internal/agent/ ./internal/lineedit/` → clean.
- Tests (branch-scoped): `CGO_ENABLED=0 go test -count=1 -run 'TestChatFlow'
  ./cmd/agent/` → **ok**; `-run 'TestThinkingSeam' ./internal/agent/` → **ok**;
  `./internal/lineedit/` → **ok 10.601s**. Full scoped run of the three touched
  packages: `internal/lineedit` ok, `internal/agent` and `cmd/agent` FAIL with
  exactly the pre-existing sets above (verified against the base worktree).
- **Red proof** (the regression test is not a tautology): with the routing
  reverted to the flow path (`writeBandRow` → `return false`),
  `TestChatFlow_AgentOutputTakesWholeBandRows` fails at
  `chat_flow_rows_test.go:170`: *"the reasoning text is not intact on any single
  band row [0,11] — the sources are interleaved"*, and
  `TestChatFlow_LongNotice…` fails at `:236`. With the seam not installed at all,
  `writeFlow` fails first: *"no flow seam is installed on the agent … would go
  straight to os.Stderr and tear the layout"*. Restoring → green.
- **Render truth (cell grid), before/after, only the binary varying**:
  `capture-flow.sh before /home/user/work/GoPi/bin/gopi` (master, 2fdc65e4) vs
  `capture-flow.sh after /tmp/gopi-band-fix2` (this branch). Same geometry
  (100×26), same script, same real endpoint.

## Evidence (self-filed)

The before-shot tears, read off `devdoc/evidence/sgr-grid/before-turn-2-answer.grid.txt`
(one line per screen row, `N|row`), exactly the defect's shape:

```
 2|[gopi] ℹ️ thinking budget 16384 clamped to 8192 (max_tokens/2 = 16384/2): the endpoint ignores budge
 3| do work? The instructions emphasize doing work with tools. Yet over-doing a greeting is wasteful.
 5|Hello — GoPi here, ready to work.                                                                 I
10|27-33a1085791db (mode=chat)
13|...chatter. So just respond.abled. Raise GOPI_MAX_TOKENS to keep a larger thinking budget.
14|                                                                                         [thinking]
15|The user said "hello world"Maybe check the repo quickly to be helpful? That'd be scope creep. Keep i
25|deepseek-flash · iter 1 · tools  thinking…[gopi] chat table: chat row ensured: 3be62868-159a-4a46-96
```

Three claims, each a row the run observed:

- row 2 is a NOTICE mid-line and row 3 is a REASONING fragment spliced onto the
  same row — the notice's tail (`abled. Raise GOPI_MAX_TOKENS …`) reappears at
  row 13, i.e. two writers interleaved in one stream;
- row 5 is the ANSWER cut by a reasoning fragment (`I` at the right edge);
- row 25 is the STATUS row: `deepseek-flash · iter 1 · tools  thinking…` followed
  by `[gopi] chat table: chat row ensured: …` on ONE row — the status row lost
  its own text to a flow notice, which is the measured defect verbatim.

The after-shot (`after-turn-2-answer.grid.txt`), same command, same endpoint:

```
 0|_tokens, so the harness aborts a round whose streamed thinking passes 8192 tokens (~32768 chars) and
 1| re-issues it thinking-disabled. Raise GOPI_MAX_TOKENS to keep a larger thinking budget.
 2|[thinking] The user just said "hello world". This is a trivial greeting — no task. Memory search
 3| is for tasks; this is off-topic chatter. Per rules, skip memory for weather-like chatter.
 4|Just respond briefly and helpfully.
 5|No tool calls needed. Keep it short.
 6|Hello! GoPi here — ready when you are. What would you like to work on?
 9|[gopi] chat table: chat row ensured: e9e6771e-3b87-4b97-ac90-03dcafcb7232 (mode=chat)
12|you>
25|deepseek-flash · iter 1 · tools · 10.2k tokens  ready
```

- the notice occupies rows 0-1 as ONE row's text (wrapped by its own length);
- the reasoning block is rows 2-5, each carrying only reasoning;
- the answer is row 6 alone;
- the session notice is row 9 alone;
- row 12 is the editing row (`you>`) and row 25 the status row
  (`deepseek-flash · iter 1 · tools · 10.2k tokens  ready`) — each carrying only
  its own text.

Gate claims I observed myself:

- `git status --porcelain` in this worktree: see the commit below; the raw/grid
  evidence files and the new sources are all committed, and the tree is clean
  after it (the command is re-run in the close-out message).
- The `cmd/agent` and `internal/agent` pre-existing failures: each of the five
  `cmd/agent` tests was re-run with `-run '<the five>'` in
  `/home/user/workspace/gopi-band-base` (detached at `256d7ea0`) and failed there
  the same way; the doc-anchor comparison was a `diff` of the two `:190` error
  sets, which came back identical modulo the worktree path.
