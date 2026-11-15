<!-- doc-class: normative -->
# Task Report: `gopi log --todo` Silent-Drop Fix — balanced-prefix recovery ported to CLI display layer

**Date:** 2026-08-28
**Branch:** `feat/cli-todo-fix` (worktree `~/workspace/gopi-cli-todo-fix`, shared with `~/work/GoPi`)
**Commits:** `953e91a` (agent export) → `bcca252` (CLI fix) → `4d4a3a3` (regression tests)
**Prior art:** devdoc/tasklist-p0-evaluation.md + devdoc/tasklist-p0-impl-report.md (agent-side P0 fix, commit `af8d432`); audit findings in devdoc/tasklist-audit-report.md; the third-layer finding recorded in the PA session log (2026-08-28: "三层同病")

---

## Problem

`gopi log <session-id> --todo` silently dropped task lists from sessions whose LLM response carried `[TASK_LIST_BEGIN]` but forgot `[TASK_LIST_END]`.

The CLI display layer (`internal/gopilog/formatter.go`, `extractAllTaskLists`, ~line 991) scanned text blocks for marker pairs and used:

```go
endIdx := strings.Index(remaining, taskListEndMarker)
if endIdx < 0 {
    break   // ← silent drop: the whole block vanished
}
```

A missing END marker aborted the scan of that text block, so the block (and any task list in it) was never added to the snapshots. The session then rendered as **"No task list found in this session."** even though the JSON was complete and perfectly parseable.

This was the **third layer** of the same disease already fixed agent-side:
- agent parse (`ParseTaskList`, tasklist.go:97-98 `endIdx<=beginIdx → return nil,nil`) — fixed in `af8d432` (balanced-prefix recovery + `ErrTaskListRecovered`/`ErrTaskListUnparseable` sentinels),
- CLI view (`extractAllTaskLists`, formatter.go:991 `endIdx<0 → break`) — **this fix**.

**Real repro:** session `68827372-1b53-4e1f-b21a-92c97bf5f177` (git_blame-motivate competition run) contains **two** complete 7-task JSON blocks in assistant text (lines 281 and 286) with no `[TASK_LIST_END]` — the LLM forgot the closing marker both times. Pre-fix `gopi log 68827372 --todo` printed "No task list found in this session."

## Root cause

Same design flaw as the agent side: the parser treated "no END marker" as "no task list", conflating *marker presence* with *content validity*. The task-list JSON between the markers is complete and valid; only the closing bookkeeping marker is missing. Dropping the entire block on a missing marker destroys real data instead of recovering it.

The CLI layer additionally duplicated (rather than reused) the agent's parsing logic — the pre-existing audit (devdoc/tasklist-audit-compare-report.md) flagged this as a cross-package divergence risk; the aliasing refactor (formatter.go:959-966) had already made `agent.TaskItem` the single source of truth for types, but the *recovery algorithm* was still not shared.

## Fix

1. **Export the recovery algorithm (agent/tasklist.go, commit `953e91a`):**
   `parseTaskListBalancedPrefix` → **`ParseTaskListBalancedPrefix`**, doc comment extended to state it is part of the shared task-list contract between the agent and the CLI display layer. No behavior change; call site in `ParseTaskList` updated; sentinel contract (`tasks, ErrTaskListRecovered`) unchanged.

2. **Use it in the CLI (gopilog/formatter.go, commit `bcca252`):**
   In `extractAllTaskLists`, the `endIdx < 0` branch no longer breaks blindly. It calls `agent.ParseTaskListBalancedPrefix(remaining)` on the tail after BEGIN:
   - recovery succeeds (`errors.Is(err, agent.ErrTaskListRecovered)` and non-empty tasks) → append a normal `todoSnapshot` — **first-class**: renders identically to closed-block lists, exactly like the agent-side contract;
   - unrecoverable tail (mid-JSON truncation, prose marker reference) → still dropped, **no garbage list is ever fabricated** (same no-fabrication guarantee as the agent side).

   Trailing-prose stripping (e.g. a `</parameter></invoke>` pastiche after the JSON) comes for free — balanced-prefix closes at the matching bracket, not at EOF.

3. **Preserved existing behavior:** closed-block sessions (`BEGIN`+`END` both present) go through the untouched `endIdx >= 0` path — the strict parse, alt-name resolution, and multi-block scan are byte-identical to before. `extractTaskList` (last-list compatibility) and `FormatTodo` need no changes because they build on `extractAllTaskLists`.

## Regression tests (formatter_test.go, commit `4d4a3a3`)

| Test | Fixture | Expectation |
|---|---|---|
| `TestExtractAllTaskLists_BeginWithoutEnd_RecoversRealFixture68` | **real** session 68827372 line-281 text (7-task complete JSON, no END) — same source as agent-side `classAFixture68` | 1 snapshot, 7 tasks, task[6] = completed / "逐文件 commit + devdoc 报告" |
| `TestExtractAllTaskLists_MixedClosedAndUnclosed` | iter 1 closed block + iter 2 BEGIN-no-END block | 2 snapshots in order — normal path untouched, recovered block visible |
| `TestExtractAllTaskLists_UnrecoverableTail_DropsNotPanic` | class B (mid-JSON truncation) + class C (prose quoting marker) | 0 snapshots, no panic, zero garbage |
| `TestFormatTodo_RecoversBeginWithoutEnd_RealFixture68` | real fixture through `FormatTodo` | output contains "TASK LIST #1", "7 total, 7 completed, 0 progressing", the task summary; **NOT** "No task list found in this session." |

Existing tests (`TestExtractAllTaskLists`, `TestExtractAllTaskListsAlternateFieldsAndNumericIDs`, `TestFormatTodo`, `TestFormatTodoNoTaskList`, all agent-side `TestParseTaskList_*` incl. the Class A/B/C recovery suites) still pass unchanged — normal-path regression guard.

## Verification

- **A/B on the real session** (the exact bug report):
  - before (master `27d9d2b` binary): `gopi log 68827372 --todo` → `No task list found in this session.`
  - after (`feat/cli-todo-fix` binary, `bin/gopi-cli-todo-fix` in worktree): same command → **TASK LIST #1 — Iteration 67** (7 total, 7 completed) **and** TASK LIST #2 — Iteration 69 (7 total, 7 completed), all 7 summaries correct. Both BEGIN-no-END blocks recovered.
- **Build:** `go build ./...` OK (worktree), `go build ./internal/gopilog/` + `go vet ./internal/gopilog/` clean.
- **Tests:** `go test ./internal/gopilog/` PASS; full `go test ./...` — see final result below (all 9 packages).
- **Main repo untouched:** the only tracked-file change in `~/work/GoPi` during this task was my own accidental edit of `internal/agent/tasklist.go`, reverted immediately (`git restore`); final `git status` in the main repo shows only pre-existing untracked files. All real edits live in the worktree.

## Not Done (honest scope)

- The `--last N --todo` path and `FormatSession`'s embedded todo rendering were not touched — they route through the same `extractAllTaskLists`, so they inherit the fix; no separate test added for them.
- The agent-side `ParseTaskList` (uses `LastIndex` = last block) and the CLI multi-block scan remain semantically different by design (one "latest wins", one "show all"); this fix unifies only the *recovery algorithm*, not the block-selection policy (flagged low priority in tasks/code-review-redundancy-report.md #10).
- No binary was installed to the main repo (`bin/gopi` untouched) — verification used the worktree-only `bin/gopi-cli-todo-fix`.

## Deliverable declaration (PROJECT.md Rule 6d)

- **Branch:** `feat/cli-todo-fix` — commits `953e91a`, `bcca252`, `4d4a3a3` (worktree `~/workspace/gopi-cli-todo-fix`, shared with the main repo `~/work/GoPi`).
- **Recovery command:** `git log feat/cli-todo-fix --oneline` / `git show feat/cli-todo-fix:devdoc/tasklist-cli-todo-fix.md`.
- **Confirmed in the MAIN repo:** yes — the worktree shares the main repo's refs; `cd ~/work/GoPi && git log --oneline -1 feat/cli-todo-fix` → `4d4a3a3`.
- **master untouched:** `27d9d2b`; no merge performed (left to the dispatcher per Rule 6e/9).
