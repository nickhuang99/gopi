<!-- doc-class: historical -->
# Merge Readiness Report: merge/clean-compaction → master

> Date: 2026-08-06
> Branch: merge/clean-compaction
> Target: master

## Branch Summary

Memory compaction (LLM-assisted context preservation) + measured-token
feedback loop, built on the Claude-only engine cleanup.

### Commits on branch (vs master)

| Hash | Description |
|------|-------------|
| `e886ba5` | Strip DFS engine and legacy OpenAI client — Claude-only gopi |
| `d9422ff` | **Memory compaction**: summarize oldest messages instead of dropping them when context nears the window |
| `a37208e` | Feed API-measured prompt tokens back into the context estimate (calibrates the chars/4 heuristic) |

(The `7a7b322` merge of `clean/no-dfs-claude-only` + `feat/memory-compaction`
is the branch's second parent and carries no additional content vs the
above.)

## Merge Status

- **Auto-merge**: Clean — verified with `git merge-tree` (0 conflict markers;
  `dfs.go`/`reduce.go`/`task_result.go` and their tests are removed in the
  merged tree as intended by the cleanup).
- **Build**: `go build ./...` — OK
- **Vet**: `go vet ./...` — clean
- **Tests**: `go test -count=1 ./...` — **166 tests PASS** (0 failures)
- **Race**: `go test -race ./...` — **PASS** (0 data races)

## Feature: Memory Compaction

When the estimated context exceeds 90% of the window, `trimContext` no longer
just drops the oldest messages. It pops them with `Context.TakeOldest` and
makes one cheap summarizer call (`ClaudeChat`, no tools, thinking disabled,
≤ 2048 output tokens) that distills them into a compact
`[MEMORY COMPACTION — EARLIER WORK]` user block: goal, decisions, findings,
file paths, command/test outcomes, task list, next steps. Recent history
stays verbatim (cache-friendly). If the summarizer fails, the dropped
messages stay removed — equivalent to the old trim behavior (graceful
degradation).

### Measured-token feedback (commit `a37208e`)

The chars/4 heuristic only *guesses* context size. Now every response's
`input_tokens` + `cache_creation_input_tokens` + `cache_read_input_tokens`
is fed back via `Context.SetPromptTokens`, so the compaction trigger fires
on the API's authoritative count (heuristic applies only to messages
appended since the last measurement). `trimContext`'s log line prints both
estimates. Trimming invalidates the measurement, falling back to the
heuristic until the next response re-measures.

## Verification

| Check | Result |
|-------|--------|
| `go build ./...` | OK |
| `go vet ./...` | clean |
| `go test -count=1 ./...` | 166 PASS, 0 FAIL (agent 4.1s, tools 13.0s, deepseek 0.01s) |
| `go test -race ./...` | PASS, 0 data races |
| Live run `GOPI_CONTEXT_WINDOW=6000` | 1 summarizer request (7 input messages), summary response, `[MEMORY COMPACTION — EARLIER WORK]` block present in all subsequent requests; run completed OK |
| Compaction unit tests | 7 in `compact_test.go`: compaction, fallback-to-trim, no-trigger-under-threshold, disabled, TakeOldest ordering, token-calibration ×2 |

## Files Changed

24 files, +533 / −4068 (vs master):

- **New**: `internal/agent/compact.go`, `internal/agent/compact_test.go`,
  `devdoc/memory-compaction.md`
- **Modified**: `internal/agent/context.go` (TakeOldest, SetPromptTokens,
  calibrated EstimatedTokens), `internal/agent/loop.go` (compaction-first
  trim, CompactionEnabled, token feedback), `cmd/agent/main.go` (usage doc),
  `internal/deepseek/types.go`
- **Removed (Claude-only cleanup)**: DFS engine (`dfs.go`, `reduce.go`,
  `task_result.go` + tests), legacy OpenAI client, SSE integration tests,
  DFS prompt-template docs

## Recommendation

**Merge.** The compaction feature is complete, the token feedback loop makes
the trigger authoritative rather than heuristic, and the entire suite
(build, vet, 166 tests, race detector) is green. The DFS-engine removal is a
deliberate architectural decision from `clean/no-dfs-claude-only` (Claude-only
gopi) and merges without conflict. Post-merge: re-run the full suite on
`master` and re-verify the live compacted run.
