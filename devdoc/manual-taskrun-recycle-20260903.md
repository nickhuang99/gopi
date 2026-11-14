<!-- doc-class: historical -->
# Task Report: Manual `task run` Recycles Its Worktree at Close-Out

Epic: t-1788397248 — "FEATURE: manual task run must recycle its worktree at
close-out (same resource-principle as worker — merger trigger starves
without it)".

## Completed

### Root cause (verified in code + ledger)
The finish-cleanup chain (`TaskFinishCleanupSessionID` in
`internal/tools/task_finish_cleanup.go`) had THREE gaps that left manual-run
worktrees behind:

1. **Client-only gate**: `if IsServerNode() { skip "no client hand-off" }`.
   On the server (peerbox, single-node: no sync_peers) the chain returned
   early BEFORE any worktree removal could happen — a terminal run's
   worktree was never released.
2. **Deliverable-required resolution**: the chain only recycles when the
   row's `deliverable` already names `worktree`/`repo` — but the manual-run
   child's AUTO row (the row the child's own end-hook cleans) carries NO
   deliverable. The wrapper row carries the deliverable but was closed by
   `writebackOutcome` with no chain call.
3. **No delivery-time hook**: only the worker lifecycle's throttled
   `serveFinishCleanupSweep` ran the chain, and it ran on the *child* rows

> **WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md** — the operator owns this; an agent must never run it (submit the work instead: `gopi task submit`).

   (no deliverable → skip). A manual `gopi task run` completed → writeback
   → returned, and nothing recycled the worktree.

### The fix (three moves)

1. **Server-mode arm of the chain** (`finishCleanupServerRecycle` +
   `finishCleanupBranchWorktree`): when the node is the server or has no
   sync peer to push to, the chain now runs a server recycle — remove the
   clean linked worktree the branch is checked out in, **KEEP the branch**
   (it is the shared repo's merge input; deleting it would starve the
   merger). Same guards as the client arm: terminal row, clean worktree,
   non-exempt branch, fail-soft per step. `TaskFinishCleanupSessionID` now
   mode-splits instead of skipping on server.
2. **`finishCleanupGitRaw`** (raw multi-line output reader): `git worktree
   list --porcelain` is multi-line; the old `finishCleanupGit` first-line
   summarizer truncated it to one line, so both porcelain parsers
   (`finishCleanupBranchWorktree` new, `finishCleanupOwningWorktree`
   pre-existing) silently never matched a linked worktree.
3. **Delivery-time close-out hook** in `runTaskRunOnce` (the shared funnel
   for manual `task run`, `task run next`, and the worker round — all
   launch methods): after the completed writeback + mirror reconcile, call
   `TaskFinishCleanupSessionID(wrapperTaskID)`. The wrapper row is the one
   with the deliverable (repo + the branch the writeback just backfilled
   from the child session's recorded workdir). Fail-soft: a skip/error is a
   log line, never fatal to the run's close-out.

### Recycle matrix (per launch method × node role)

| Launch | Node | Result |
|---|---|---|
| manual `task run` / `task run next` | client (peer) | push → worktree remove → branch -d |
| manual `task run` / `task run next` | server / single-node | worktree remove only, **branch kept** (merger input) |
| worker round | client | push → worktree remove → branch -d |
| worker round | server / single-node | worktree remove only, **branch kept** |
| run on main checkout (branch master) | any | skip (no linked worktree — nothing to recycle) |
| dirty worktree | any | skip (live-bond guard — never pull the ground out from under a writing run) |

## Not Done / Deferred

- The live merger daemon (`gopi task merger --every 30s`, started by nick
  08:32) auto-merged my FIRST worktree branch mid-development via the
  **merge-backlog audit** path (`TaskMerge`, task_merge.go) which has NO
  worktree-occupancy guard — grep 'worktree' = 0 hits there — while the
  executor path (task_merge_executor.go `mergeExecSafeMergeBlock`) HAS the
  guard. Recorded to memory (#1344). Closing that audit-path guard gap is a
  SEPARATE task (out of scope here: this epic is about the manual run
  releasing ITS worktree, not about the merger respecting an active one).
- Live-ledger evidence rows cleaned by hand earlier (nick) were NOT
  re-created; the fix prevents the class going forward.

## Verification

- Build: `go build ./...` OK.
- Tests:
  - `go test ./internal/tools/ -run 'TestFinishCleanup|TestServerRecycle'`
    — 10/10 PASS (incl. 4 new: server removes+keeps, dirty skip, no-linked-
    worktree skip, branch-worktree resolver).
  - `go test ./cmd/agent/ -count=1` — full cmd/agent suite PASS (65s).
  - NEW E2E acceptance (real binary + real child + mock LLM + real git
    repo/worktree):
    - `TestManualRunRecyclesItsWorktree` — manual `task run` whose child
      executes in a linked worktree on a feat branch: worktree REMOVED at
      close-out, branch PRESERVED, wrapper row completed. PASS.
    - `TestManualRunMainCheckoutSkipsRecycle` — run on master/main checkout
      completes normally, no recycle (nothing to remove). PASS.
  - `go test ./internal/tools/` full suite: running (pre-existing >600s
    weight; job log /tmp/tools-test.log).
- Commit: see git log on `feat/manual-taskrun-recycle`.
