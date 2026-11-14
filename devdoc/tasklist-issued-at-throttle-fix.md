<!-- doc-class: normative -->
# Task Report: tasklist_issued_at throttle bug — split-clock fix + regression test + task show liveness

## Completed
- **Regression test first** (fails on pre-fix code, verified below):
  - `internal/agent/taskheartbeat_test.go` — TestTouchTasklistNotThrottledAfterToolcall: calls touchLastToolcallAt() then IMMEDIATELY touchTasklistIssuedAt() (<30s apart, same sandbox ledger) and asserts BOTH columns land. Pre-fix: tasklist write silently dropped by the shared 30s clock.
  - `internal/tools/tasks_stale_test.go` — TestTaskShowLivenessColumns: task show renders Liveness: last_toolcall_at=…, tasklist_issued_at=… for non-NULL values.
- **Fix** (`internal/agent/taskheartbeat.go`): tasklist touch gets its own throttle clock (`tasklistState.lastWrite`, tasklistThrottleMin = 30s, Rule 1 named constant; new run resets it in setTaskHeartbeatSession) — the toolcall throttle clock is no longer shared. tasklist_issued_at is the only column touched; updated_at untouched.
- **task show liveness** (`internal/tools/tasks.go` TaskShow): in_progress rows with non-NULL liveness columns render a `Liveness:` line (best-effort, no new tool — Rule 3).

## Not Done
- No backfill of historical NULL tasklist_issued_at rows (raw facts only; history stands).

## Verification
- Build: `go build ./...` OK
- Regression FAILS pre-fix: `go test ./internal/agent/ -run TestTouchTasklistNotThrottledAfterToolcall` exit 1 before, PASS after fix.
- Tests: agent Heartbeat|Tasklist 14/14 PASS (incl. regression, FAIL-verified pre-fix); tools Stale|Touch PASS; full suite: pre-existing master failures only (reflow/jobkill/memsearch)
- Commit: ea28a6b (branch fix/tasklist-issued-at-throttle, worktree-shared)
