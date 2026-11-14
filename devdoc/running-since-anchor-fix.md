<!-- doc-class: historical -->
# Task Report: running-duration display anchored on real execution start (stale re-arm fix)

Epic: `task list --status in_progress` running-duration display corrupted by the
stale-re-arm bump — "[running X since Y]" resets periodically on zombie rows,
making a long-dead task look restarted (nick's finding, 2026-09-03, row t-1788393729).

## Completed

### Display fix (internal/tools/tasks.go)
- **New `taskRunningAnchor(t *Task)`** — picks the wall-clock the running suffix
  anchors on, in order (first present wins):
  1. `claimed_at` — the claim moment (M4.5 double-lock): authoritative
  2. `tasklist_issued_at` — harness [TASK_LIST] bookkeeping (the run's liveness stamp)
  3. `created_at` — row creation (auto-registration == execution start)
- **`updated_at` is NEVER an anchor** — it is a write-churn column (status
  transitions, tags/verify/model backfills, sync merges, and the stale-sweep
  re-arm bump `taskZombieBumpSQL` all touch it). A zombie's updated_at resets
  every ~10-min stale window, which made the running clock lie.
- `taskRunningSuffix` now uses the anchor; the `"~"` approximation marker is
  GONE (it existed only to flag updated_at-as-approximation; all three real
  anchors are authoritative start signals).
- Comments updated (function doc, TaskList doc, inline M4.12 comment) to state
  the never-updated_at contract.

### Regression tests (internal/tools/tasklist_running_test.go, +3)
- `TestTaskRunningAnchorOrder` — pins the anchor ladder + asserts updated_at is
  never chosen even when newest.
- `TestTaskRunningSuffixMonotonicAcrossRearm` — the zombie-row regression: seeds
  an in_progress row with EMPTY claimed_at + frozen heartbeat, re-arms it twice
  by bumping updated_at, asserts the since-clock never moves and no "~" appears.
- Existing 4 tests still pass (claimed_at anchoring, no-suffix on pending/
  terminal, duration formatting, inapplicable statuses).

## Not Done
- Stage-2 zombie disposal (ticket t-1788382218) — kill + mark failed instead of
  notify+bump forever. This display fix stops the LIE; disposal stops the zombie.
- Real row t-1788393729 was already cancelled by the time this task ran (the
  stage-2 sweep disposed it) — verified the fix on an equivalent synthetic
  zombie row (same shape) instead.

## Verification

- Sweep: `taskRunningSuffix` was the ONLY updated_at-as-start-proxy in display
  code. `task show` prints `updated_at:` as a labeled raw field (legit); sync
  views (memory_sync_delta/harvest) use updated_at as the delta/rank anchor
  (correct — not a start display).
- E2E (real binary, sandbox DB with zombie row, claimed_at NULL, heartbeat
  frozen): fixed binary renders `[running 4h13m19s since 08:39:14]` → re-arm
  bump updated_at to now → `[running 4h13m20s since 08:39:14]` — monotonic,
  no reset. Pre-fix master binary on the same row renders `~12:52:46`
  (anchored on the bump moment, duration ~0) — bug reproduced, fix confirmed.
- Build: `go build ./...` OK
- Tests: `go test ./internal/tools/ -run 'TestTaskRunningSuffix|TestTaskRunningAnchor|TestTaskListRunning|TestTaskListNoRunning'` PASS
- Full `go test ./cmd/agent/ ./internal/tools/ -count=1` — see commit-time check.
- Commit: 537d82e (feat/running-since-anchor)
