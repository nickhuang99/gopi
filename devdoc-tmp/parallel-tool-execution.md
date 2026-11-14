<!-- doc-class: normative -->
# Parallel execution of read-only tool calls

**Date:** 2026-08-20
**Branch:** `feat/parallel-claude` (from `feat/code-search`)

## Motivation

DeepSeek V4 can emit MULTIPLE tool calls in a single round, each with its own
`id`. GoPi executed them strictly serially: `for ... range toolCalls {
executeTool(...) }`. That is safe but wasteful — a round with three
`code_search` calls took the sum of their latencies, and search/read tools are
pure (no file writes, no process spawning), so the order of execution is
irrelevant to their results.

This change executes all-whitelisted rounds concurrently. Wall-clock latency
for a round becomes `max(call latencies)` instead of `sum(call latencies)`.

## What changed

`internal/agent/loop.go`:

- **Whitelist** (`parallelSafeTools`): `read`, `code_search`, `keyword_search`
  — read-only, side-effect free, safe in any order.
- **`executeToolBatch(toolCalls)`** — the single entry point both `Run()` and
  `runNonStreaming()` call for a round's tool execution:
  - `len > 1` AND every call whitelisted → `executeToolBatchParallel`
  - anything else (single call, or any `write`/`edit`/`bash` present) →
    the original serial path, byte-for-byte unchanged behavior
- **`executeToolBatchParallel(toolCalls)`**:
  - one goroutine per call + `sync.WaitGroup`
  - results written into an **index-keyed slice**, so results come back in
    tool-call order regardless of completion order (callers index by `j` and
    feed the model by `id` — ordering is part of the contract)
  - **one batch timeout** for the whole batch: `tools.FileToolTimeout()`
    (default 30s, `GOPI_TOOL_TIMEOUT_SEC` override), using the same
    goroutine+channel pattern as the file tools. A timed-out call returns an
    error result; the other calls in the batch still return normally. (The
    hung tool goroutine may keep running — identical trade-off to
    `readWithTimeout`.)
  - **logging** (via `a.Logger`, when set): per-call duration
    (`parallel tool [i/N] name: dur`) and batch summary
    (`parallel batch: N concurrent calls, total, timeout`) so parallelism is
    observable in the debug output.
- `executeTool` became a thin wrapper with a test-only hook (`executeToolFn`);
  the real dispatch is `executeToolDefault`. `executeToolBatchParallel` runs
  `executeTool`, which is safe to call concurrently: it touches no agent
  state, only `tools.*` stateless functions.

## Side-effect tools stay serial

`write`, `edit`, `bash` are excluded from the whitelist on purpose:
- file mutations must not overlap (lost writes / interleaved edits)
- bash can spawn processes with persistent effects; two concurrent commands
  could interfere (and the daemon-isolation machinery in `bash.go` is
  per-invocation)

Any round containing one of these falls back to the serial path — the batch
executes exactly as it did before this change.

## Tests (`internal/agent/loop_test.go`)

Deterministic concurrency measurement via a `concurrentTracker` (atomic
max-overlap counter) injected through the `executeToolFn` hook — no real
files/processes needed, no timing-only assertions:

| Test | Verifies |
|---|---|
| `TestExecuteToolBatch_SingleCallSerial` | 1 call → serial path, result intact (regression) |
| `TestExecuteToolBatch_ParallelOrdered` | 3 whitelisted calls → max overlap ≥ 2 (concurrent) AND results in call order while the first call finishes LAST |
| `TestExecuteToolBatch_MixedFallsBackToSerial` | read + write + keyword_search → max overlap == 1, order kept |
| `TestExecuteToolBatchParallel_TimeoutOthersStillReturn` | hung call → "timed out" result at the batch deadline; fast call returns normally |
| `TestAgent_Run_ParallelReadsEndToEnd` | full `Run()` loop: one round with two read calls, both results arrive at the model as separate tool messages in call order |

## Verification

- `go build ./...` — clean
- `go vet ./...` — clean
- `gofmt -l .` — clean
- `go test ./...` — all packages green
