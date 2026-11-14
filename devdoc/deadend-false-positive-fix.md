<!-- doc-class: historical -->
# Task Report: Dead-end nudge false positive on memory_search (confusion root cause)

## Summary
A prior GLM run (log captured at `/tmp/thinking.txt`) was deeply confused in its
first iteration: the TASK-START memory_search returned 5 highly relevant records
(#1067,#1071,#898,#891,#222), but then the harness injected the dead-end nudge
*"The tool call above just returned a dead end... Trim it now with context_trim"*.
The agent correctly noticed the contradiction (productive result ≠ dead end) and
spent several thinking iterations second-guessing whether to trust the system
prompt or the evidence. This is a real bug in the **reactive dead-end detector**.

## Root cause
`resultsHaveDeadEnd` (internal/agent/loop.go) matched a list of **bare substring
markers** (`"failed"`, `"not found"`, `"[stderr]"`, `"Exception"`, `"Traceback"`,
`"no such file"`, ...) against **every tool result in the round** — including
memory_search, whose result body is *retrieved QA prose*, not an execution-sta-
tus report. Memory records about troubleshooting naturally contain the words
"failed", "not found", "[stderr]", "Exception". When any such word appeared in a
returned record's prose, `resultsHaveDeadEnd` returned true and the harness
injected `deadEndTrimMessage`, telling the model to context_trim a result of
genuine value.

Verified live in the session log (`gopi-raw-20260829-050552.log`): the round's
`results` array = [memory_search_result, bash_exit=0]. The memory result body
contained the markers (records #1022/#876/#904 carry "failed"/"[stderr]"/"not
found") → false positive → nudge fired at the productive memory result.

## Fix
- Split `deadEndMarkers` into:
  - **`toolErrorMarkers`** — each tool's own `"... error:"` contract prefix
    (`memory_search error`, `code_search error`, ...). These only appear when
    the tool's *execution* actually failed; they always fire.
  - **`deadEndMarkers` (now generic prose markers)** — `failed`, `not found`,
    `[stderr]`, `Exception`, `Traceback`, etc. Only applied to *status-reporting*
    tools (bash, read, write, edit, ...).
- Added `contentReturningTools` map (`memory_search`, `web_search`, `web_fetch`,
  `code_search`, `keyword_search`) — tools whose result body is retrieved content.
  For these, only `toolErrorMarkers` are evaluated, never the generic prose markers.
- `resultsHaveDeadEnd` now takes the index-aligned `toolCalls` slice
  (`toolCalls[j]` ↔ `results[j]`) and branches on the tool name.

## Not Done / assessed (the "other suspicious parts" from the Epic)
1. **Model hallucinated a call_id** — the prior run's agent quoted `call_1182d0b...`
   when the real id was `call_00_PF8Xd...`. Assessed: **not a bug** — the
   context_trim dispatch already no-ops safely on unknown/incomplete/already-
   trimmed ids (`"context_trim: no-op — call %s not found ... (silent)"`), and
   the code comment at the dispatch explicitly documents this model failure
   mode. No change needed.
2. **whitelistContextSlim dropping a round-1 search before the model votes** —
   assessed: **safe**. Round-1 searches are exempt this round
   (`currentMemorySearchIDs` grace set); the model has a full round to vote
   useful next round before the pair can be dropped. The false positive was the
   only real hazard; the fix removes it.

## Verification
- Build: `go build ./...` : OK
- Tests:
  - `go test ./internal/agent/ -run 'DeadEnd|ContextTrim'` : PASS
  - `go test ./internal/agent/` : PASS (25s)
  - `go test ./internal/gopilog/` : PASS
  - New regression: `TestResultsHaveDeadEnd` (per-tool cases incl. memory prose
    false-positives + genuine tool errors), `TestResultsHaveDeadEnd_batchMixed`
    (mixed-batch independence), `TestDeadEnd_NoFalsePositiveOnRealMemoryRound`
    (live-repro of the exact prior round + real tool-error flip case) : all PASS
- Commit: `be4e0d5` on branch `fix/deadend-false-positive-memory` (worktree
  `~/workspace/gopi-session-home`; shared with main repo — no push needed)

## Deliverable declaration (Rule 6d)
- Branch: **`fix/deadend-false-positive-memory`**
- Recovery: `git show fix/deadend-false-positive-memory:devdoc/deadend-false-positive-fix.md`
- Commit is in the main repo via the shared worktree object store.
