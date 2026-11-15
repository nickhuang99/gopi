<!-- doc-class: normative -->
# Task Report: Fix P0 — `task run <id>` Double-Executes an Already-Running Task

Branch: `feat/fix-task-run-double-exec` (worktree `~/workspace/gopi-fix-task-run-double-exec`, base fc1dfc9)
Date: 2026-09-02
Source finding: devdoc/review-cli-task-core.md (branch `review/cli-task-core`) — P0 finding #1, E2E-proven.

## The Bug

`TaskClaimExplicit` (internal/tools/tasks.go:2850) — the Phase-1 claim behind
`gopi task run <id>` — has two shapes:

1. **Shape ①** `pending → claimed` CAS: RowsAffected checked, exactly-one-winner. Correct.
2. **Shape ②** crash-recovery re-stamp (`UPDATE ... WHERE status='claimed'`):
   **RowsAffected was NEVER checked.** For a row in ANY other status
   (in_progress, terminal, or nonexistent) the UPDATE affected 0 rows yet the
   function returned `nil` (success).

Consequence: `gopi task run <id>` on an **already-running** task passed claim,
then `TaskStart` (`claimed→in_progress`) was a same-status no-op for the
in_progress row, and the runner spawned a SECOND gopi child on the same task
with the same briefing — double execution, two runners writing the same
deliverable. E2E-proven in the review. This is exactly the concurrent-execution
hazard the M4.21/4.22 claim-lock protocol was built to prevent; the explicit-id
path bypassed it (`task run next` / TaskNextFiltered did the CAS correctly).

## The Fix (internal/tools/tasks.go, TaskClaimExplicit shape ②)

The re-stamp's conditional UPDATE is now itself a CAS: `RowsAffected` is read,
and `0` means the row is NOT in a claimable state — refuse instead of silently
succeeding:

- **in_progress** → refused: "task %s is in_progress (holder %s) — refusing to
  run" — a runner is executing this task RIGHT NOW; double-running would
  double-execute the briefing.
- **terminal** (completed/failed/cancelled/verified) → refused naming the status.
- **missing row (ghost id)** → refused: "no task with task_id=…".

The refusal names the actual **status and holder** (the M4.5 "already
live-claimed locally" refusal shape: `memory_sync_claim.go` names
`status + claimed_by` the same way), and reads the row via `taskByID` so the
error is truthful rather than a guess from the failed UPDATE.

**Crash-recovery semantics preserved**: `claimed` is the ONLY recoverable
non-pending state — a holder that died between the Phase-1 CAS and the Phase-3
advance leaves a transient `claimed` row whose sole durable justification is
this resume. Shape ② still re-stamps a `claimed` row (RowsAffected==1 → nil).
A live `in_progress` row is never resumable via claim — that is the bug.

**Contrast preserved**: `task run next` (TaskNextFiltered) is untouched — it
already did the CAS correctly.

## Files Changed

| File | Change |
|---|---|
| internal/tools/tasks.go | `TaskClaimExplicit` shape ②: check RowsAffected; on 0, load row and refuse (in_progress/terminal/ghost) with status+holder in the error |
| internal/tools/tasks_claim_explicit_test.go | NEW engine tests (3) |
| cmd/agent/cli_tasks_double_exec_test.go | NEW CLI E2E regression tests (2) |
| devdoc/bugfix-task-run-double-exec-2026-09-02.md | This report |

## Verification

- **Build**: `go build ./...` OK; `go vet ./internal/tools/ ./cmd/agent/` clean.
- **New engine tests** (`-run TestTaskClaimExplicit`): 3/3 PASS —
  - refuses in_progress (error names status + original holder; row untouched),
  - refuses terminal (completed) + ghost id,
  - legitimate shapes: pending claim (①) and claimed crash-resume (②) still succeed.
- **New CLI E2E tests** (`-run TestTaskRunExplicit`, real binary + mock LLM):
  2/2 PASS —
  - `task run <id>` on in_progress → exit non-zero, refusal on stderr naming
    in_progress + holder, **mock LLM request count == 0** (briefing NOT
    executed — no child spawned), row untouched.
  - `task run <id>` on claimed (dead holder) → resumes, executes (mock LLM
    request > 0), writeback → completed.
- **Regression scope**: `TestCrashRecovery*` / `TestNoClaimedLingers*` /
  `TestTaskNext*` / `TestTaskSubmitAndNext*` (internal/tools) + `TestTaskRun*`
  / `TestParseTasks*` (cmd/agent) — all PASS.
- **Full suites**: `go test ./internal/tools/ ./cmd/agent/` — see job output;
  expected green (both suites green on the scoped runs above).

## Acceptance Checklist

1. ✅ `task run <id>` on in_progress → refused, exit non-zero, briefing NOT
   executed (mock LLM count 0), row untouched.
2. ✅ Legitimate resume (claimed + dead holder) still works (engine + CLI tests).
3. ✅ `go test ./internal/tools/ ./cmd/agent/` green.
4. ✅ devdoc committed.

## Not Done / Notes

- The review's other findings (P1 verify-briefing `--summary`, P1 serve daemon
  os.Exit, P2 items) are OUT OF SCOPE for this fix — this epic is the P0 only.
- No magic numbers introduced (Rule 1): no new literals; the fix reads
  RowsAffected against the existing `taskStatusClaimed`/`taskStatusPending`
  named constants and reuses `taskByID`.
- `TaskResumeClaimed` (the worker round + recovery-only entry) already checked
  the row's status before re-stamping and is unchanged — the fix closes the
  parallel hole in `TaskClaimExplicit` only.
