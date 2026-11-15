<!-- doc-class: historical -->
# Server-side worker worktree recycling — E2E coverage report

Date: 2026-09-04 · Epic t-1788389175 (BUG FIX + QUALITY) · Branch feat/server-worker-recycle

## Problem (the operator's finding, 2026-09-03)

The HOMM3 maps project ran 4 feasibility bonds on peer1 (the SERVER). All 4
delivered — branches + reports committed — but all 4 worktrees
(maps-fix/maps-title/maps-vcmi/maps-visual under ~/work/workspace/) were left
behind. PA removed them by hand so the maps merger could see unprotected
branches.

Root cause: the finish-cleanup chain (push → worktree remove → branch -d)
was client-gated (`IsServerNode` gate inside internal/tools/task_finish_
cleanup.go). On the SERVER a worker's close-out never removed its worktree,
so every delivered branch stayed worktree-PROTECTED under the merger's
merge-time indicator ("no worktree protection = merge candidate"). Server-side
delivery therefore (a) piled up worktrees and (b) starved the merger.

The resource-responsibility principle: **the party who checked out the
worktree cleans it up** — the worker on BOTH sides. The client half worked
(push → worktree remove → branch -d); the server half (remove worktree ONLY,
branch preserved as the merger's input) was never implemented/tested.

## The fix — two-side recycle matrix

The finish-cleanup chain mode-splits by node role
(TaskFinishCleanupSessionID, internal/tools/task_finish_cleanup.go).
The server arm fires when `IsServerNode() || finishCleanupServerTarget()==""`
— i.e. an EXPLICIT role=server node OR any node with no declared sync peer
(the peer1 shape: no "role" field AND no sync_peers → IsServerNode() is false
via the N1 hostname fallback, but the empty peer target selects the server
arm all the same). The client arm runs only on a node with a declared sync
peer to push to.

| Side | Selection condition | Delivery close-out | Branch fate |
|---|---|---|---|
| CLIENT | declared sync peer (finishCleanupServerTarget != "") | ① clean-worktree guard → ② push <peer>:~/work/GoPi → ③ worktree remove --force → ④ branch -d | Deleted locally — the branch reached the server |
| SERVER / single-node | IsServerNode OR no sync peers (peer1 shape) | ① clean-worktree guard → ② worktree remove (linked worktree on the deliverable branch) | **PRESERVED** — the branch is the merger's input (deleted only after the merger merges it) |

No branch delete on the server — deleting here would remove work the merger
has not yet consumed. The merger's config-sweep (t-1788468369) then sees the
now-unprotected branch and merges it on its next tick.

Where it is wired:

- `internal/agent/taskregister.go` — session-end hook runs the chain
  (`finishCleanupServerRecycle` server arm).
- `cmd/agent/cli_tasks.go` — delivery-time hook in `runTaskRunOnce` (the
  shared funnel for manual task run / task run next / WORKER round) runs
  `TaskFinishCleanupSessionID` on the WRAPPER row after the terminal
  writeback. The child's own close-out runs on its AUTO row (no deliverable);
  the wrapper row is the one carrying deliverable{repo,branch}.
- Worker lifecycle pass (`workerLifecyclePasses`) re-attempts any terminal
  row whose chain never ran (killed process / crashed session-end).
- Exemptions: master/main/archive/* + GOPI_FINISH_CLEANUP_EXEMPT.
- Dirty-worktree safety: a worktree with uncommitted changes is skipped
  (fail-soft — never destroy a live bond's work).

## The E2E coverage (the tests that were MISSING)

New file `cmd/agent/cli_tasks_worker_recycle_e2e_test.go` — drives the REAL
binary through the FULL chain with a scripted mock LLM and sandboxed
GOPI_HOME/GOPI_MEMORY_DB/GOPI_SETTINGS. Real git world: temp repo + linked
worktree pre-created with the deliverable committed (the GOPI.md Rule 6a /
HOMM3 dispatcher shape).

### TestWorkerServerDeliveryRecyclesThenMergerMerges (QC #1 + #3 + #5)

The exact scenario HOMM3 exposed, end to end, ZERO manual intervention:

1. Fixture: repo on master; feature branch `feat/worker-chain-e2e` committed
   and checked out in linked worktree W (the dispatcher pre-created shape).
2. settings.json: `{"role":"server","repos":{<repo>:{}}}` (the chain test
   runs the explicit-role shape; the dirty test below runs the REAL peer1
   shape — no "role" field, no sync_peers — proving the empty-peer-target
   arm fires the same server recycle).
3. Submit a pending task with `--repo <repo>`.

> **WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md** — the operator owns this; an agent must never run it (submit the work instead: `gopi task submit`).

4. `gopi worker --once` with cwd=W (the server worker picks the task up from
   the workspace worktree): claims → runs a real child via the mock LLM →
   terminal writeback (completed) → branch backfill → **delivery-time
   recycle removes W automatically**.
5. Assertions:
   - `git worktree list` no longer shows W (worktree AUTO-REMOVED — the
     missing server leg; 4 manual removals in HOMM3 are now zero).
   - The branch `feat/worker-chain-e2e` STILL EXISTS after the recycle
     (QC #3 — the merger's input is never deleted server-side).
   - The wrapper row is terminal `completed`.
6. `gopi task merger --once` (the config-sweep merger, t-1788468369): the
   now-unprotected branch is merged into master — assertion by captured tip
   hash ancestry (`merge-base --is-ancestor <tip> master`) because the
   adoption/merge chains delete the branch ref (`branch -d`) after a clean
   merge. QC #5: no starvation.

Observed pass output (trimmed):

```
[serve] [adopt] scanned=1 adopted=1 ...        ← adoption merged the branch
[merger] [/tmp/.../repo] merged=0              ← repo sweep: nothing left
master log: adoption: merge feat/worker-chain-e2e (<tip>)
captured tip ... ancestor-of-master=true
```

The recycle (step 4) is what makes step 6 possible: with the worktree still
parked the merger's occupancy guard would report `protected=1` and starve.

### TestWorkerServerDeliveryDirtyWorktreeKeepsBranch (QC #4)

Same worker chain, but the worktree carries an UNCOMMITTED file (wip.txt —
a live bond still writing). The delivery-time recycle MUST fail soft:

- Worktree survives, uncommitted file survives, branch survives.
- The run still completes normally (recycle failures are never fatal to the
  close-out).

### Client-side regression (QC #2) — unchanged behavior

The client leg is pinned by the pre-existing unit fixture:
`internal/tools/task_finish_cleanup_test.go` TestFinishCleanupChainCleanWorktree
asserts push OK → worktree removed → branch deleted (plus dirty-worktree
skip, push-fail keeps branch, idempotent push, repo=worktree deliverable
shape, branch exemptions).

## Verification

- Build: `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` OK
- New tests: `go test ./cmd/agent/ -run TestWorkerServerDelivery -v` — 2/2 PASS
  (also `-count=2` stability-checked)
- Regression sweep (recycle/merger/worker family):
  `go test ./cmd/agent/ -run 'TestManualRun|TestWorker|TestMergerOnce|TestFinishCleanup|TestMergedCleanup|TestMergerRepoSweep'` OK
  `go test ./internal/tools/ -run 'TestFinishCleanup|TestServerRecycle|TestMergerRepoSweep|TestMergedCleanup|TestSyncTaskDeliverable|TestBackfillDeliverableBranch'` OK
- Full suites: `go test ./cmd/agent/ ./internal/tools/` (QC #6) — see below.
- Commit: see git log on feat/server-worker-recycle.

## Not done / notes

- No production-code change was required for this epic: the server-mode
  recycle arm (319f063, epic t-1788397248) and the delivery-time hook were
  already in master — what was MISSING was exactly what the operator called out:
  the full-chain E2E proof that a WORKER (not just a manual run) delivering
  on the server recycles its worktree and that the merger then merges
  without manual intervention. This report + the two E2E tests are the
  quality-control deliverable.
- A live worker/merger pair (Sep-03 daemons on the real repo) was running
  during this work; the E2E tests are fully sandboxed (GOPI_HOME /
  GOPI_SETTINGS / GOPI_BIN point at the worktree) and never touched it.
