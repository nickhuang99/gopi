<!-- doc-class: normative -->
# Perf Fix R1-R4 — Implemented

**Date:** 2026-08-22
**Branch:** feat/perf-fix-claude
**Status:** implemented + tested (build/vet/test/-race green, binary rebuilt)
**Source plan:** `devdoc/perf-vs-claude-fix-plan.md` (commit 46001e2) — this doc
records what was actually changed, where, and how to re-measure.

GoPi is ~3.1x slower per round than Claude Code on the same backend
(deepseek-v4-flash, same endpoint; 12.0s vs 3.9s median). The gap is
harness-side per-round overhead, not model speed. R1-R4 attack the four
biggest (impact × effort) levers from the fix plan. R5-R8 are out of scope
here (see "Not done" at the end).

---

## R1 — Bound the context aggressively  *(biggest lever on long tasks)*

| | Before | After |
|---|---|---|
| Trim trigger | 90% of window (810K tokens of history before anything trimmed) | **60%** (540K) |
| Old tool results | kept verbatim until dropped/compacted | folded into one-line stubs when over budget |
| bash soft output cap | 16KB | **8KB** |

### Changes

1. **`internal/agent/context.go:18`** — `TrimThresholdRatio` `0.9 → 0.6`.
   Rationale in the constant comment: at 0.9 the history grew to ~810K
   tokens before trimming; every late-task round then re-prefilled a
   200-400K-token tail (measured ~16s for a 290K prefill,
   `internal/deepseek/client.go:24` comment; 15-30s/round late in the
   2026-08-06 race). Trimming at 60% keeps the cache prefix stable.
2. **`internal/agent/context.go`** — new `Context.summarizeOldToolResults()`
   + policy constants: `maxVerbatimToolResults = 8` (most recent results
   stay verbatim), `maxToolResultLines = 20` (line count above which an OLD
   result is collapsed), `maxToolResultSummaryChars = 120` (first-line cap).
   Stub shape: `[tool result summarized: was N lines — first line: "exit=0"]`
   — for bash results the first line carries the exit status, so the model
   still sees the outcome.
3. **`internal/agent/loop.go` `trimContext`** — folding pass runs when the
   estimate is over the limit, BEFORE compaction/trim; if folding brings the
   estimate back under the limit, no destructive trim and no LLM compaction
   call is needed. Zero LLM cost.
4. **`internal/tools/bash.go:24`** — `softOutputSize` `16KB → 8KB` (already a
   named constant; comment updated). Halves per-call context growth at the
   source; the truncation hint ("narrow the command") already steers the model.

Deliberately NOT changed: `CompactionTargetRatio` stays 0.5 (task scope
listed only the trim threshold; the plan's 0.35 target is a possible
follow-up). Compaction now triggers earlier automatically because
`trimContext` trips at 0.6 — verified no regression in `compact_test.go`.

### Expected effect (per fix plan §4)

Late-task rounds 15-30s → 5-10s; on 30+ round tasks this is the single
biggest wall-time win. Each folded 16KB result saves ~4K tokens of context.

### Tests

- New: `TestSummarizeOldToolResults_FoldsOnlyOldLargeResults` (context_test.go).
- Existing compact/context threshold tests reference the constants and adapt:
  `TestTrimContext_*`, `TestNewAgent_DefaultContextWindow`, `TestBashOutputTruncation`.

---

## R2 — Task-list JSON only on change, not every round

| | Before | After |
|---|---|---|
| Rule 2 of [TASK MANAGEMENT RULES] | "Every response MUST end with [TASK_LIST_BEGIN]… full task list as JSON" | full list **only when changed**, or every **3** responses at the latest; omit on ordinary tool rounds |
| create_at | required field, fabricated unix timestamps | removed from prompt ("No create_at field"), parser still accepts it |

### Changes (`internal/agent/loop.go`)

- New const `maxRoundsWithoutTaskList = 3`; `taskManagementRules` is now a
  `var taskManagementRules = fmt.Sprintf(...)` so the cadence is
  single-sourced into the prompt.
- New rule 2 text:
  `End with [TASK_LIST_BEGIN]...[TASK_LIST_END] full task list JSON ONLY when it changed (new task / status change), or every 3 responses — omit on ordinary tool rounds. Format: [{"task_id":"1","summary":"Read source","status":"progressing"}]. Fields: task_id (unique, never reuse), summary, status ("progressing"|"completed"). No create_at field.`
- `malformedTaskListCorrection` no longer lists `create_at`.
- Harness tolerance **verified** (no harness change needed): `ParseTaskList`
  returns `nil,nil` for missing markers (tasklist.go), `Run` only updates
  `TaskState` when `tasks != nil` (loop.go), and the "exit with progressing
  tasks" warning only fires on a parsed list — an omitted round is never
  treated as deletion (`todoState.update` never wipes on nil).
- Parser backward compatible: `TaskItem` still accepts `create_at`/`created_at`
  when the model emits them (tasklist_test fixtures unchanged).
- System prompt stays under the 4.5K density budget (TestSystemPromptDensity).

### Expected effect

−150-300 output tokens/round of pure bookkeeping (−1-3s/round at ~60 tok/s)
and less thinking spent maintaining the list.

### Tests

- New: `TestSystemPrompt_TaskListCadence` (prompt_test.go) — asserts no
  "Every response MUST end with", cadence "every 3 responses" present, old
  required-fields phrase gone.

---

## R3 — Trust a completed end_turn (skip the +1 confirmation round)

| | Before | After |
|---|---|---|
| Normal completion path | 2 rounds: answer end_turn + confirmation end_turn (with injected continuation message) | **1 round** when the round's task list is all-completed AND the answer is substantive |
| Mid-task stops (progressing tasks / no list / terse answer) | confirmation round | unchanged (bug022 guard intact) |

### Changes (`internal/agent/loop.go`, `internal/agent/tasklist.go`)

- Exit block (was "bug022 fix"): on `len(toolCalls) == 0`, after the
  existing bookkeeping, exit on the FIRST round when
  `ParseTaskList(content) != nil && AllCompleted(tasks) &&
  len(contentWithoutTaskList(content)) >= minFinalAnswerChars`.
- New const `minFinalAnswerChars = 40` — the substantive-answer floor, so a
  terse/empty declaration is not trusted.
- New helper `contentWithoutTaskList` (tasklist.go): strips everything from
  the [TASK_LIST_BEGIN] marker onward so the check measures the answer, not
  the JSON block (which for a multi-task list alone can be 100+ chars).
- Comments updated on `maxConsecutiveEndTurnRounds`, the Run loop header and
  the exit block, documenting the R3 exception.

### Expected effect

−1 round per task (~12s) plus a smaller final context (no injected
confirmation exchange). No premature-exit regression: the guard only fires
on an explicit all-completed declaration with a real answer; the bug022
nudge (devdoc/bug026-parallel-tools-evaluation.md and bug022 history) still
covers every mid-task stop.

### Tests (loop_test.go)

- `TestAgent_Run_ExitsOnFirstCompletedEndTurn` — 1 API call (was 2).
- `TestAgent_Run_KeepsConfirmationForProgressingList` — progressing list
  keeps the confirmation round (2 calls) and continuation message.
- `TestAgent_Run_KeepsConfirmationForTerseCompletedList` — "Done." +
  all-completed list is NOT trusted (2 calls).
- `TestContentWithoutTaskList` — stripping semantics.
- Existing end_turn tests (`TestAgent_Run_SimpleResponse`, `_WithToolCall`,
  `_StreamFallback`, `_ThinkingBudgetDowngrade`, `_ParallelReadsEndToEnd`)
  never emit task lists → nil parse → confirmation path unchanged. All pass.

---

## R4 — Parallelize the read-only subset inside mixed batches

| | Before | After |
|---|---|---|
| Round `[read, read, bash]` | fully serial (any non-whitelisted tool disqualified the whole batch via `allParallelSafe`) | reads run **concurrently** (single batch timeout), bash serial afterwards |
| Result order | in call order | unchanged — index-keyed slice, single ordered append |

### Changes (`internal/agent/loop.go`)

- `executeToolBatch` now splits every round:
  - **Phase 1** — the read-only subset (`parallelSafeTools`: read,
    code_search, keyword_search, todo) runs concurrently via the existing
    `executeToolBatchParallel` (race-free pre-sized indexed slice, whole-batch
    timeout — the shape vetted in
    `devdoc/bug026-parallel-tools-evaluation.md` §4). A single read-only call
    runs inline (nothing to overlap with).
  - **Phase 2** — side-effect tools (write/edit/bash/job_*) run serially;
    effects never overlap.
  - Results are written into `results[idx]` in both phases, so the returned
    slice is always in tool_use order — the API requires tool_result blocks
    in tool_use order (`TestBuildClaudeRequest_CoalescesConsecutiveToolResults`).
- Removed `allParallelSafe` (dead after the split).
- `parallelTimeout` batch bound applies to the read subset as before.

### Expected effect

Mixed rounds drop from `sum(reads) + rest` to `max(reads) + rest` wall time
(e.g. two 30s-worst-case reads in a mixed round: up to −30s).

### Tests (loop_test.go)

- `TestExecuteToolBatch_MixedParallelizesReadSubset` — `[read, write,
  keyword_search]`, all same duration: max concurrency exactly 2 (reads
  overlap, write never does), order preserved with write at index 1.
- `TestExecuteToolBatch_MixedSingleReadSerial` — `[read, bash]` stays serial.
- Existing: `_SingleCallSerial`, `_ParallelOrdered`,
  `_ParallelTimeout_OthersStillReturn`, `TestAgent_Run_ParallelReadsEndToEnd`.
- **`go test -race ./...` green** — this is what the bug026 evaluation
  flagged for the naive map-write variant.

---

## Verification (all green)

```
go build ./...                                  OK
go vet ./...                                    OK
go test ./... -count=1                          OK (agent 15.5s, tools 21.8s)
go test -race ./... -count=1                    OK (agent 22.4s, tools 22.3s)
CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent  OK (9.2MB binary)
```

## Commits

| Commit | Change |
|---|---|
| `54e013b` | feat(perf): R1 — bound context aggressively (0.9→0.6 threshold, fold old tool results, 8KB bash soft cap) |
| `da28869` | feat(perf): R2 — task list JSON only on change, not every round |
| `af4e720` | feat(perf): R3 — trust a completed end_turn, skip the confirmation round |
| `510016f` | feat(perf): R4 — parallelize the read-only subset inside mixed tool batches |

## How to re-measure (fix plan §5, reproduced)

Methodology from `devdoc/gopi-efficiency.md` ("Benchmark integrity note") +
`perf-vs-claude-fix-plan.md` §5:

1. **Pin the state under test** — the Rule-6 clone checks out the intended
   commit; use three isolated clones: before (46001e2's parent or 46001e2
   itself), after (this branch HEAD), claude (baseline).
2. **Same task text** in all three; same model + endpoint
   (deepseek-v4-flash); run at the **current defaults**
   (thinking=enabled, reasoning_effort=minimal, max_tokens=16384) — NOT the
   old max-effort configuration.
3. **Extract metrics from the session JSONL** (`~/.gopi/sessions/*.jsonl`):
   - per-round tokens: `request` events (prompt / cache_hit / cache_miss /
     completion deltas)
   - wall time: `session_end` (elapsed), plus per-round timing via
     `request` event ordering vs `session_end`
   - context growth: prompt tokens across `request` events (expect the R1
     fix to hold late-task rounds near the 60% window, not 810K)
4. Compare: median round time, round count, total wall time. Targets from
   the plan: per-round gap 3.1x → ~1.5x on typical tasks; late-task blowup
   (5-8x on long tasks) eliminated; −1 round per task (R3); −1-3s/round
   bookkeeping (R2); read-heavy mixed rounds faster (R4).

## Not done (out of scope for this task)

- R5 batched log flushing, R6 code_search staleness cache, R7 adaptive
  reasoning effort, R8 subagent-style exploration — see fix plan §3 for
  designs. R1's `CompactionTargetRatio` 0.35 adjustment is a candidate
  follow-up if compaction frequency proves too low.
