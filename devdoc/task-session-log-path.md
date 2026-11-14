<!-- doc-class: historical -->
# Task Report: task show session log path + created/updated + task_id consolidation

Epic: t-1788304050 — "task should also include session log path for easy tail -f".
Three asks from the briefing (verbatim):
1. session log path in `gopi task show` for `tail -f` (esp. background worker sessions).
2. `created_at` == `updated_at` on live rows reads as "task not running" — investigate and fix.
3. task_id == session_id duplication — consolidate (two fields carrying identical info).

## Completed

### 1. `session_log:` line in `gopi task show` (headline ask)
- New `taskSessionLogPath(sessionID)` helper (internal/tools/tasks.go) resolves
  `<gopi-dir>/sessions/<session-id>.jsonl` (config.GopiDir() — $GOPI_HOME/.gopi or ~/.gopi),
  the same resolution taskModelFromSession uses.
- TaskShow renders `session_log: <full path>` right after `session_id:` when the row has a
  session; a missing file (pruned/not recorded) shows the resolved path with
  `(not found — session pruned or not recorded)`; a row with no session_id renders no line.
- CLI help text (cmd/agent/cli_tasks.go) updated to document the line.
- Live-verified: `gopi task show` on a real run now prints the exact tail-able path.

### 2. created_at == updated_at on live rows — root cause + fix
Root cause (confirmed against devdoc/task-updated-at-liveness-audit.md): updated_at is the
M4.13 delta-sync VERSION ANCHOR — it deliberately moves ONLY on status transitions
(claim → in_progress → terminal), NOT per heartbeat. An auto row mid-run therefore shows
created_at == updated_at by design; the liveness facts (last_toolcall_at /
tasklist_issued_at) are the "still running" signals, but TaskShow rendered the liveness
line ONLY when a stamp was non-NULL — a fresh live row rendered NO liveness line and read
as dead.
Fix: TaskShow now renders the `liveness:` line for EVERY in_progress row, with explicit
"(none)" for not-yet-recorded stamps — a mid-run snapshot is self-explanatory
(created==updated is NORMAL for an un-transitioned live row; the liveness line shows it is
alive). Terminal/pending rows render no liveness line (not alive / not started).
Regression tests updated + added (TestTaskShowLivenessColumns in agent package).

### 3. task_id == session_id consolidation (Option 2 from devdoc/task-id-style-analysis.md)
Auto-registered rows now mint a uniform `t-<unix>` task_id; the session UUID lives ONLY in
the session_id column (the JSONL link). Implementation:
- internal/tools/tasks.go: extracted `taskIDGenerate` (shared by TaskNew/TaskSubmit);
  added `TaskNewReturningID` (TaskNew's row-id-returning variant — TaskNew delegates to it).
- internal/agent/taskregister.go: start hook calls `TaskNewReturningID(summary, "", sessionID, a.Model)`
  and retains the row's task_id on the Agent (`autoTaskRowID`); the END hook, terminal push,
  finish-cleanup, resume-hint and wrapper tag all key on `autoTaskRowID` instead of the session id.
- internal/agent/loop.go: new `autoTaskRowID` field; heartbeat latch passes both
  session id and row id (`setTaskHeartbeatSession(sessionID, taskID)`).
- internal/agent/taskheartbeat.go + internal/tools/taskheartbeat.go: heartbeat touch keys on
  the row's task_id first, falling back to session_id==task_id for legacy rows
  (pre-consolidation rows keep working).
- taskSubmittedBy: auto rows now render `harness auto-registration (row IS the run: session <uuid>)`
  — the "task_id = session id" phrasing is gone (it no longer holds for new rows).
- Legacy rows (task_id == session_id) are NOT backfilled (per the analysis: sync/ref
  risk) — the heartbeat fallback and show rendering keep them working.
- Tests: taskregister_test.go updated to assert task_id != session_id + t- prefix; new
  TestHeartbeatTouchConsolidatedRow (heartbeat reaches the right row when keys differ).

E2E verified (GOPI_HOME sandbox): a one-shot `gopi` run registers
`task_id = t-1788305392`, `session_id = 6d3a08ad-...`, and `task show` prints
`session_log: .../sessions/6d3a08ad-....jsonl`.

## Not Done
- Legacy 151 auto rows (task_id == session_id) not migrated — deliberate (sync-peers +
  parent_task_id references make renaming risky; heartbeat fallback keeps them functional).
- updated_at itself still only moves on status transitions (M4.13 sync-anchor semantics) —
  the fix is the always-rendered liveness line, not making updated_at a heartbeat.

## Verification
- Build: `go build ./...` OK; `go vet ./internal/agent/ ./internal/tools/` OK.
- Tests: internal/agent + internal/tools full suites (job-1), cmd/agent suite (job-2),
  targeted: TestAutoTask*, TestChatTask*, TestHeartbeat*, TestTouch*, TestTaskShow*,
  TestSubmittedBy* all PASS.
- Live: e2e sandbox run → consolidated row + session_log path rendered (see above);
  real `task show b2fcf7d9-...` renders session_log + updated submitted_by.
- Branch: feat/task-session-log-path (worktree ~/workspace/gopi-task-session-log).
