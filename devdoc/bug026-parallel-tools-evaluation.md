<!-- doc-class: normative -->
# Task Report: bug026 — parallelize read tool calls (evaluation)

## Summary

Evaluation of `bugs/bug026-parallel-tools.txt` against the current codebase.
**Verdict: the architectural split (reads in parallel, write/edit/bash
sequential) is correct and reads are genuinely side-effect-free — but the
proposal as written is NOT shippable.** It has two critical defects:

1. **A data race / crash**: Phase-1 goroutines write to shared maps
   (`readResults`, `readIDs`) concurrently. Go maps are not safe for concurrent
   writes even to distinct keys — this pattern crashes with
   `fatal error: concurrent map writes` and is flagged by `go test -race`
   (reproduced live below).
2. **tool_result ordering breaks for mixed batches**: all read results are
   appended to context before all non-read results, so a batch like
   `[read, write, read]` yields tool_results in order `[0, 2, 1]` while the
   assistant's tool_use order is `[0, 1, 2]`. The Claude API requires
   tool_result blocks in the same order as the tool_use blocks, and the repo's
   own test `TestBuildClaudeRequest_CoalescesConsecutiveToolResults` asserts
   that order.

This is an **evaluation-only** epic — no code was changed.

---

## Completed — findings

### 1. Current implementation (as-built)

- Tool execution is a single sequential loop in `internal/agent/loop.go`
  (lines 662–673): `for j, tc := range toolCalls { result := a.executeTool(tc); ... }`.
  Each result is logged to the session JSONL and appended to the context
  (`a.Ctx.AddToolResult`) **in toolCalls order** — that order is load-bearing
  (see defect #2).
- `executeTool` (loop.go:999) dispatches by `tc.Name` to
  `tools.Read / tools.Write / tools.Edit / tools.Bash*`. It touches no Agent
  fields — it is a pure function of its argument, so concurrent execution of
  the `read` case is safe in itself.
- `internal/tools/read.go`: `Read` → `readWithTimeout` (own 30s timeout,
  `GOPI_TOOL_TIMEOUT_SEC`) → `readImpl` (pure: `os.UserHomeDir`, `filepath.Abs`,
  `os.Stat`, `countLines`, `os.Open` read-only, `os.ReadFile`). **No side
  effects, no shared mutable state** — the bug file's "reads are always safe"
  claim is verified correct.
- Context serialization (`internal/deepseek/claude_client.go`,
  `BuildClaudeRequest`): consecutive `tool` messages are coalesced into ONE
  user message whose `tool_result` blocks appear **in the order AddToolResult
  was called**. `internal/deepseek/claude_client_test.go:167` asserts
  `Content[0].ToolUseID == "c1" && Content[1].ToolUseID == "c2"` — order is
  part of the project's contract.
- Imports in loop.go currently have **no `sync`** — the snippet as pasted does
  not compile without adding it.

### 2. Critical defects in the proposal

| # | Issue | Severity | Evidence / Resolution |
|---|-------|----------|------------------------|
| 1 | **Concurrent map writes in Phase 1.** `readResults[j] = ...` and `readIDs[j] = ...` run from N goroutines with no synchronization. Go maps are unsafe for concurrent writes even to *distinct* keys. | **Critical (crash + race)** | Reproduced live: the exact pattern crashes with `fatal error: concurrent map writes` and `go run -race` reports `WARNING: DATA RACE` at `mapassign`. **Fix:** pre-size a slice `results := make([]string, len(toolCalls))` — concurrent writes to *distinct slice elements* are race-free (also verified live). |
| 2 | **tool_result order no longer matches tool_use order for mixed batches.** Phase 1 appends ALL read results, then Phase 2 appends non-read results. Batch `[read(0), write(1), read(2)]` → context order `[0, 2, 1]` vs tool_use order `[0, 1, 2]`. | **Critical (API 400 risk / contract break)** | Claude API requires tool_result blocks in the same order as the tool_use blocks; `TestBuildClaudeRequest_CoalescesConsecutiveToolResults` enforces it. **Fix:** collect all results into the indexed slice, then append to SessionLog + Ctx in a **single pass over original index order** (Phase 3 below). |
| 3 | **Snippet doesn't compile as-is.** `sync.WaitGroup` requires adding `"sync"` to loop.go imports. | Medium | The claimed `go build ./... PASS` only holds after the import is added. |
| 4 | **Verification cannot catch defect #1.** `go test ./internal/agent/ -count=1` (no `-race`) does not detect the map race, and no automated test covers parallelism or ordering — only a manual "echo" check is proposed. | Medium | Add a unit test (grouping/ordering invariant) and run `go test -race ./internal/agent/`. |
| 5 | **Debug logging dropped for parallel reads.** Current loop logs `[j/n] result: %d bytes` per call under `a.Debug`. Phase 1 has no equivalent. | Low | Preserve in the unified Phase 3 append loop. |
| 6 | **Session JSONL interleaving diverges.** tool_result events for reads are written before non-read results, so the log no longer interleaves in call order (each line still carries its `tool_use_id`, so replay remains possible). | Low | Fixed by the same single-pass Phase 3. |

### 3. What the proposal gets right

- Reads are genuinely idempotent and side-effect-free (verified in read.go) —
  parallelizing them is safe in principle.
- Worst-case read-group latency drops from N×30s to ~30s (each read keeps its
  own independent `FileToolTimeout`), which is the real win for DeepSeek
  multi-tool batches.
- write/edit/bash correctly stay sequential (file conflicts, ordering).
- `j, tc := j, tc` loop-variable capture is correct; the `call_%d` fallback ID
  matches the sequential path; results are appended only after `readWg.Wait()`
  from the main goroutine (so SessionLog/Ctx are never touched concurrently).

### 4. Corrected minimal patch shape (for the implementer)

```go
// Phase 1: run read calls in parallel (safe — no side effects).
results := make([]string, len(toolCalls))
var readWg sync.WaitGroup
for j, tc := range toolCalls {
    if tc.Name != "read" {
        continue
    }
    j, tc := j, tc
    readWg.Add(1)
    go func() {
        defer readWg.Done()
        results[j] = a.executeTool(tc)
    }()
}
readWg.Wait()

// Phase 2: run all other calls sequentially (write/edit/bash).
for j, tc := range toolCalls {
    if tc.Name == "read" {
        continue
    }
    results[j] = a.executeTool(tc)
}

// Phase 3: append in original index order (matches tool_use order).
for j, tc := range toolCalls {
    id := tc.ID
    if id == "" {
        id = fmt.Sprintf("call_%d", j)
    }
    if a.SessionLog != nil {
        a.SessionLog.LogToolResult(id, results[j])
    }
    a.Ctx.AddToolResult(id, results[j])
    if a.Debug {
        a.Logger.Printf("  [%d/%d] result: %d bytes", j+1, len(toolCalls), len(results[j]))
    }
}
```

This is race-free (distinct slice elements), preserves tool_result ordering
exactly, keeps debug + session logging byte-for-byte identical to today, and
still parallelizes reads. Add `"sync"` to loop.go imports. (Optional: a
semaphore if batches can be large — each read also spawns an inner timeout
goroutine, so N reads ≈ 2N goroutines; fine for typical <10 batches.)

### 5. Suggested test

- Unit test over the grouping/ordering invariant: build an Agent, feed a
  `toolCalls` slice mixing reads and non-reads, and assert `Ctx.Messages`
  tool results appear in the same order as the input tool_use indices
  (mirrors `TestBuildClaudeRequest_CoalescesConsecutiveToolResults`).
- Run `go test -race ./internal/agent/` — this is what would have caught
  defect #1.

---

## Not Done

- No code changes made (evaluation-only epic; Rule 6 — never develop in the
  main directory).
- Did not verify DeepSeek's Anthropic-compatible endpoint *enforcement* of
  tool_result ordering live (no API key/run performed). The project's own
  contract (test + comment) already treats order as mandatory, so the
  ordering defect stands regardless.

---

## Verification

- Build: `go build ./...` → **OK** (baseline, unmodified tree).
- Tests: `go test ./internal/agent/ -count=1` → **PASS** (8.8s);
  `go test ./internal/tools/ -count=1` → **PASS**.
- Race repro (defect #1): minimal Go program using the proposal's exact map
  pattern → `fatal error: concurrent map writes`; `go run -race` →
  `WARNING: DATA RACE` at `runtime.mapassign_fast64`. Same program with a
  pre-sized slice → clean under `-race`.
- Commit: none (no changes made — evaluation only).

## References

- `bugs/bug026-parallel-tools.txt` (the proposal under review)
- `internal/agent/loop.go` — sequential tool loop (lines 662–673),
  `executeTool` (line 909), imports (no `sync`)
- `internal/tools/read.go` — `Read`/`readImpl` (pure, side-effect-free)
- `internal/agent/context.go` — `AddToolResult` appends in call order
- `internal/deepseek/claude_client.go` — coalesces consecutive tool results;
  order preserved on the wire
- `internal/deepseek/claude_client_test.go:167` — tool_result order asserted
