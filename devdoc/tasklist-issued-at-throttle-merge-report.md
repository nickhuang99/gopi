<!-- doc-class: historical -->
# Merge Report: fix/tasklist-issued-at-throttle → master (2026-09-01, HP client)

## Branch / Tip reconciliation
- Epic quoted tip `0369d71` / ahead 21 — that was STALE: `git fetch peerbox` showed
  0369d71 is now an ANCESTOR of master (it IS the merge-base). The real branch
  content lived in the local worktree `~/workspace/gopi-tasklist-throttle` at
  `7f3aea8` (3 commits ahead / 14 behind master):
  - `ea28a6b` fix(agent): tasklist_issued_at gets its own throttle clock
  - `26e4820` docs: tasklist throttle fix report
  - `7f3aea8` docs(uat): task show liveness verification

## Diff review (master...branch, 6 files +208/−32)
- `internal/agent/taskheartbeat.go`: root-cause fix — `heartbeatState.lastWrite`
  (one shared 30s clock) split into per-column clocks `lastToolcallWrite` /
  `lastTasklistWrite`; `touchHeartbeat(clock *time.Time, touch fn)` now takes the
  clock pointer (same mutex). Previously every tool batch fired the toolcall
  touch first, silently dropping a [TASK_LIST] stamp <30s later (M4.5:
  8cbf768f emitted 3 blocks, tasklist_issued_at stayed NULL). New const
  `tasklistThrottleMin = 30s`; `setTaskHeartbeatSession` resets both clocks.
- `internal/tools/tasks.go`: TaskShow renders `liveness: last_toolcall_at=…
  tasklist_issued_at=…` on in_progress rows (non-NULL only) via new
  `taskLivenessStamp` helper; `updated_at` anchor untouched (M4.13).
- `internal/agent/taskheartbeat_test.go` (+89): fail-on-prefix regression test.
- 3 devdoc files (fix report, UAT, audit update).

## Conflict analysis (pre-merge, zero-conflict proven BEFORE touching worktree)
- `git merge-tree 0369d71 master <branch>` → 0 conflict markers.
- All 6 branch files untouched on master side since merge-base (checked per-file).
- The 14 behind master commits touch memory_sync/reflow/devdoc files — no overlap.

## safe-merge guard
- 1st run: exit 1 [c] worktree occupancy (branch checked out in
  `~/workspace/gopi-tasklist-throttle`) — expected-by-design for an active fix
  branch. `git worktree remove` (clean, no --force needed) → guard re-run
  exit 0: ✅a archive-tag ✅b name-blacklist ✅c worktree ℹ️d not-yet-merged.

## Merge
- `git merge --no-edit fix/tasklist-issued-at-throttle` → ort, clean,
  **merge commit `bb3d4e1`**. `merge-base --is-ancestor` OK. No force used.

## Verification
- Build: `go build ./...` OK (baseline pre-merge too).
- `go vet ./internal/agent ./internal/tools`: clean.
- Targeted: `go test ./internal/agent -run 'TaskHeartbeat|Tasklist|Heartbeat'` ok;
  `go test ./internal/tools -run TaskShow` ok.
- Full `./internal/agent`: ok (10.8s). Full `./internal/tools`: 1 failure —
  `TestMemorySearchNonBlocking`, single-run PASS on baseline `fd2d1e7` (detached
  worktree proof) → known env flake (memory #1220 pre-existing list), NOT
  merge-caused. Old reflow failures already fixed by earlier merge b47d994.

## Not Done
- Remote-tracking `peerbox/fix/tasklist-issued-at-throttle` left for host-side
  operator (winner branch — no archive-tag; local branch deleted below).

## Post-merge close-out (Rule 9)
- Local branch `fix/tasklist-issued-at-throttle` deleted post-merge (`branch -d`,
  safe: fully merged); tip reachable via merge commit bb3d4e1.
- Worktree removed before merge (guard requirement).
