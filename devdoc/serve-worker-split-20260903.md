<!-- doc-class: normative -->
# Task Report: serve/worker responsibility split (serve→merger)

Epic t-1788389175 — nick's final design, 2026-09-03. `task serve` becomes
`task merger`, a server-only PURE merge daemon; `gopi worker` becomes the
general task-management loop running on BOTH client and server.

## Completed

### 1. Pass-ownership split (the matrix)
| Pass | Owner after split |
|---|---|
| stale sweep | WORKER (per-round lifecycle pass, throttled) |
| finish-cleanup (terminal rows → push → worktree remove → branch -d) | WORKER (per-round lifecycle pass) |
| dispatch + claim/run (task execution) | WORKER (runTaskWorker loop) |
| adoption sweep (branch discovery + probe-merge) | MERGER (serve tick) |
| merge-backlog audit/executor | MERGER |
| merged-branch cleanup (merge leftovers) | MERGER (fallback for worker-death leftovers) |
| conflict dispatch (merge conflict → LLM task) | MERGER dispatch → WORKER executes |
| git-sync channel | MERGER tick |

Serve/merger tick **never claims or runs tasks** — the stale sweep,
finish-cleanup sweep, dispatch and run passes were stripped from
`runTaskMerger`. Task execution is the worker's job, on both sides.

### 2. Rename: `task serve` → `task merger`
- CLI verb: `task merger` added; `task serve` accepted as deprecated alias
  (`case "serve", "merger"`).
- Function: `runTaskServe` → `runTaskMerger`; `runTaskServe` remains as a
  deprecated forwarding alias (unit tests pin the merger via either name).
- Usage/help text rewritten: `task merger` documented as the merge daemon,
  `gopi worker` documented as the task-management loop.
- Lock file stays `serve.lock` (a merger and a serve never double-merge).

### 3. Worker long-running loop
- `runTaskWorker(once bool)` — the loop no longer exits on a drained queue.
  Default: wait `GOPI_WORKER_IDLE_SEC` (default 30s) and re-check like a
  daemon tick. `--once` preserves the M4.20-D drain-exit (exit 0 on drain).
- Worker survives failed runs: `workerRound` now calls `runTaskRunOnce`
  (error-returning) instead of `runTaskRun` (os.Exit on error) —
  bugfix-task-p1 semantics on the worker face.
- Per-round task-lifecycle passes: stale sweep + finish-cleanup sweep,
  throttled to `workerLifecycleThrottleMin` (10min), the shared pass
  functions the serve tick used to call (compose, don't duplicate).
- `mergerPassOnce`: the six-sweep merge surface extracted and shared by
  the --once drain, the daemon's first drain and every per-tick pass.

### 4. Resource-responsibility principle
Worktree = always removed by the WORKER at delivery (finish-cleanup chain:
push → worktree remove → branch -d on the client; worktree remove with the
branch ref kept on the server — the branch is the merger's input).
Branch = removed by whoever CONSUMED its deliverable (client worker after
push; merger after merge via merged-branch cleanup).

### 5. Acceptance tests (new file serve_worker_split_test.go)
- (a) `TestMergerOnceLeavesPendingTaskUntouched`: a merger --once pass with
  a pending task does NOT claim/run it — the row stays pending.
- (b) `TestWorkerWaitsAndPicksUpNewTaskWithoutRestart`: the worker waits on
  a drained queue and picks up a task submitted during the wait — SAME
  process, no restart.
- Converted tests: claim/run/survive tests moved from the serve tick to the
  worker loop (`TestRunTaskWorker_SurvivesFailedRun`,
  `TestRunWorkerClaimLocalOnlyWithNoPeers`,
  `TestRunTaskWorker_OnceSweepsZombieFirst`).

## Not Done
- Server-side delivery E2E (a worker finishing in a server worktree) not
  exercised as a full integration test — the server is single-node (peerbox)
  today with no peers; the finish-cleanup chain + merged-branch cleanup
  paths are covered at the tools level (internal/tools task_finish_cleanup
  and task_merged_cleanup suites, pre-existing and passing).
- GOPI.md task-management rules not yet updated (the rule text still says
  `gopi task serve` for the daemon) — deferred; runtime transition note
  below is the operational doc.

## Verification
- Build: `go build ./cmd/agent/` OK; `go build ./...` OK
- Tests: `go test ./cmd/agent/` → ok (144s, EXIT=0, full suite incl. new
  acceptance tests); `go test ./internal/agent/` → ok (47s);
  `go test ./internal/tools/` → ok (655s with -timeout 900s). NOTE: the
  internal/tools suite needs >600s of wall time, so the default 600s go
  test timeout trips it — verified PRE-EXISTING: clean master (zero epic
  changes) also times out at 60s/600s on the same suite. No internal/tools
  files were touched by this epic.
- Commits (branch feat/serve-worker-split-epic2):
  - 06a5154 feat: merger = pure merge daemon; worker = long-running loop
  - 0124c72 test: convert claim/run/survive tests to worker loop
  - e4c9ea4 test: reset shared stale-pass clock in worker zombie test
  - (70e878b head) refactor mergerPassOnce + usage docs + acceptance tests
    + devdoc (7 commits ahead of master)

## Transition (how to run today)
- peerbox = `gopi task merger --every 5m` (merge) + `gopi worker` (tasks)
- HP = `gopi worker` on demand (tasks)
- `gopi task serve` still works (deprecated alias) for running deployments.

## Root-cause incident (environment)
The worktree at ~/workspace/gopi-serve-worker-split was repeatedly removed
mid-task by the production serve daemon's MergedCleanupSweep: a fresh
worktree whose branch tip sits at master tip (no commits yet) passes every
cleanup guard (merged ✓, linked ✓, clean ✓, still-ancestor ✓, on-tip ✓, no
live ledger row ✓) and is swept as a "merged leftover". Fix: commit
immediately after `git worktree add` so the branch diverges from master
(an unmerged branch is never touched). Recorded to memory (id 1325).
