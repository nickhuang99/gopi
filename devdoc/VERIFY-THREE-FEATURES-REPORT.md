<!-- doc-class: historical -->
# Task Report: Verify context-trim / memory_feedback / clock-tick are actually implemented

## Question
The Epic asks whether three gopi features — `context_trim`, `memory_feedback`
ranking, and the `[t+NNNs]` ticking clock — are **actually implemented** or
merely described in the system prompt. The reporter "could not find" them via
`gopi log --last --context-trim`.

## Verdict: ALL THREE ARE IMPLEMENTED AND TESTED ON MASTER
`go build ./...` OK; `go test ./...` all green (7 packages, incl.
`internal/agent`, `internal/tools`, `internal/gopilog`, `cmd/agent`).

| Feature | File(s) | Mechanism | Test evidence |
|---|---|---|---|
| `[t+NNNs]` clock | `internal/agent/clock.go` | `SessionClock` records `t0`; `withTick()` prefixes every tool result with `TickLabel()` = `[t+%ds]` | `clock_test.go`: Elapsed / TickLabelFormat / TickIncreases / Agent_WithTick / Agent_Run_ToolResultCarriesTick / TickIncrementsAcrossRounds |
| `context_trim` | `internal/agent/loop.go:646` def, `:2455` dispatch; `internal/agent/context.go:160` `RemoveToolPair` | Trims a **dead-end tool call+result pair** (delete, or swap result for `replace_with` conclusion) | `context_trim_test.go`: ReplaceKeepsPairSwapsContent / WireRoleAlternation / DedupOnce / ReplaceIncompletePairNoOp |
| `memory_feedback` ranking | `internal/tools/memory.go:2024` `fbScore`; `loop.go:575` def / `:2410` dispatch | Vote appended to event table; Beta-posterior `fbScore` adds a small ranking bias into the `memory_search` rescore | `memory_feedback_test.go`: RankingIntegration / VotePersists / Errors / UnusedNeutral |

## Root cause of the "not working" impression — a viewing/semantics mismatch
1. **`gopi log --last --context-trim` shows `context_trim` tool ACTIVITY, not
   thinking trimming.** The flag names a **different** mechanism. `context_trim`
   only ever removes/replaces a dead-end tool call+result pair
   (`RemoveToolPair`) — it does **not** strip assistant *thinking*.
   There is no separate "thinking-trim log" row here.
2. **Thinking trimming is a separate P2 feature** — `WireMessages(keepThinking)`
   strips assistant `ReasoningContent`/`ThinkingSignature` at the wire, marked
   by the `gopi-empty-thinking` placeholder (`internal/deepseek/claude_client.go:122`).
   To see it, grep the raw wire log: `gopi log <id> --raw | grep -c gopi-empty-thinking`.
   A bare `--context-trim` view can never reveal it, by design.
3. **`memory_feedback` is a ranking *bias*, not truth maintenance.** It does
   NOT rewrite `tier`/`confidence` in place (those are manual `memory_update`
   decisions). It appends a vote and nudges the search rescore by a deliberately
   small `fbScore = (BetaMean − 0.5) × gate(N_eff/4) × 1.0` (max |term| ≈ 0.25).
   With the gate, ~4 votes are needed to move a record's rank meaningfully;
   contradictory votes pull the mean back toward 0.5. Check with CLI:
   `gopi memory_get` shows `fb=0.67(1)`.

## Verification performed
- Confirmed source presence, exact line numbers, and matching unit tests for all three.
- `go build ./...` → exit 0.
- Targeted `go test` on the four packages containing the feature tests → all `ok`.
- `git worktree` isolated to `/tmp/gopi-verify-three` (branch `feat/verify-three`), no edits to shared master.

## Not Done / notes
- Did not run a live LLM session to observe `memory_feedback` being triggered
  organically (known: adoption rate is low because it relies on a soft prompt
  nudge, per prior audit #876). This is an adoption issue, not a code defect.
- No code changed. This task is a verification/diagnosis; the feature flag
  `--context-trim` is functioning as specified.
