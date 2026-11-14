<!-- doc-class: historical -->
# Bugfix: git push-back — current-branch only / no stale-branch resurrection

Branch: `feat/fix-pushback-current-branch`
Date: 2026-09-02

## The bug

`DefaultGitPushback` (harvest's git push-back phase, `gitPushbackDefault`) and
the serve-tick client git pass (`GitSyncPushLocalBranches`) pushed **every**
local non-master branch to the peer (`<peer>:~/work/GoPi`). Once HP accumulated
merged/abandoned branches (no client-side cleanup chain until the sibling
`feat/fix-merged-worktree-cleanup` closed it), each periodic sync/`task push`
re-pushed stale branches whose tips were already integrated into peerbox master
and whose refs the adoption chain had deliberately deleted.

Mechanism (observed 2026-09-02 14:39 — ~30 deleted `feat/*`/`review/*`/`docs/*`
branches re-appeared on peerbox, reflog shows "push" as the creating operation):
the adoption chain (task_adoption.go ③a) merges a deliverable into master,
then `branch -d`s it. A stale client re-pushing the same tip RE-CREATES the
deleted ref — `git push` of a deleted-then-recreated ref always succeeds.

## The design decision

The per-task deliverable push is a **separate, already-correct** path:
`TaskFinishCleanupSessionID` (task_finish_cleanup.go) pushes the just-finished
row's `deliverable.branch` at terminalization — that IS the "current task's
branch only" semantics.

The **buggy** sites are the *periodic blanket passes* (harvest push-back +
serve-tick `gopi task push`), which are not task-scoped — a serve tick has no
"current task". So option 1 ("push only the current task's branch") is not
well-defined there. This fix implements the Epic's option 4 sibling:

> push only branches whose tip is NOT an ancestor of the server's master for
> that branch — skip already-fully-merged server-side branches.

This satisfies requirement (a): an adopted+deleted branch is an ancestor of
server master → its tip lives inside master's history → skipped → the deleted
ref is NOT re-created. Requirement (b) (the just-finished deliverable branch,
still unmerged) crosses the wire. Requirement (d) (nothing unmerged) pushes
nothing.

## What changed

- **`internal/tools/task_sync_git_pushback.go`** (new) — one shared core both
  blanket sites route through:
  - `gitPushbackPlan(repoDir, target)`: load local non-integration branches →
    **one** server-master fetch into a fixed local mirror ref
    (`refs/remotes/gitPushcheck/master`) → per-branch `merge-base
    --is-ancestor <branch> <server-master>`: ancestor ⇒ already-integrated ⇒
    SKIP (resurrect guard) → push the rest.
  - `gitPushbackPushOne`: collision-rename retry — when the pristine-name push
    is non-fast-forward (server independently holds a same-named branch; rare),
    retry once under `<name>-<HHMMSS>`; report the rename (requirement c).
  - Fail-closed on the server-master fetch failing (server unreachable): refuse
    the flush rather than push blind (blind push re-introduces resurrection).
  - Rule 1: nameless timeouts/sizes live in one constant block.
- **`internal/tools/memory_sync_task.go`** — `gitPushbackDefault` no longer walks
  and pushes every branch; it resolves the repo root and delegates to
  `gitPushbackPlan`, folding the flush into `GitPushbackReport`. New optional
  report fields `Renamed`/`Skipped` (added, JSON `omitempty` — backward
  compatible). Removed now-dead `harvestGitTimeout`/`harvestGitMaxBranches`/
  `harvestGitErrLine` (the branch cap lives in `gitSyncMaxBranches`).
- **`internal/tools/task_sync_git.go`** — `GitSyncPushLocalBranches` delegates
  to `gitPushbackPlan` (the serve-tick / `gopi task push` face).
- **`internal/tools/memory_sync_task_gitpushback_test.go`** — updated the
  list-failure test assertion (repo resolution surfaces first now) and the
  compile-guard `init`.
- **`cmd/agent/cli_tasks.go`** — harvest CLI prints `skipped_stale(merged)` and
  `renamed` when present.

> **Superseded (2026-09-21, task `01a0c32e`).** The `renamed` half is GONE:
> the push-back lane no longer forks on a rejected push. A branch that cannot
> fast-forward onto the peer's master is REFUSED by name (with the remedy) and
> never pushed, and `GitPushbackReport.Renamed` / the harvest `renamed=[…]`
> print were removed with it — the fork multiplied one topic into five branches
> on one tip (measured 2026-09-17). The lane's contract is now the peer merger's
> own landing predicate; see `devdoc/delivery-point-sync.md` §9.

## Regression + acceptance tests

New `internal/tools/task_sync_git_pushback_test.go` — all real-git (a bare
server + a client clone), because the filter, the up-to-date no-op, the
non-fast-forward collision and the server-ref resurrection are git semantics:

- `TestPushbackSkippedMergedStaleBranchNotResurrected` (requirements a+b): a
  branch adopted into server master + deleted is in `Skipped`, NOT pushed, and
  its server ref is NOT re-created; the genuinely-unmerged deliverable branch
  IS pushed and its server ref exists.
- `TestPushbackNoBranchPushesNothing` (requirement d): a master-only client
  pushes nothing.
- `TestPushbackCollisionRenamesAndSucceeds` (requirement c): a same-named but
  different-tip server branch makes the push non-fast-forward ⇒ the client
  renames to `feat/dup-<HHMMSS>`, which lands on the server, and the server's
  own `feat/dup` is not clobbered.

## Verification

- Build: `go build ./...` — OK
- Vet:  `go vet ./...` — clean
- Targeted: the 3 new git-pushback tests + the existing `TestGitSync*`/`TestGitPushback*`/`TestTaskHarvestGitPushback*` suites pass (13 tests, ~7.4s); whole-package test binary `go test -c ./internal/tools/` compiles.
- Full: `go test ./internal/tools/` is slow on this multi-agent box. A 600s run panicked at the 10m package timeout (goroutine dump stuck in modernc sqlite `ensureTasksSchema` from the unrelated `TestTaskNotifyTransitionFires` — a documented box-load artifact, victim varies each run, isolated-PASS; see memory #1147/#1190/#1079). Re-ran with `-timeout 30m` for the authoritative result.

## Not done / follow-ups

- Client-side cleanup chain (branches merged on peerbox must be deleted on HP) is
  the sibling branch `feat/fix-merged-worktree-cleanup` / t-1788329004 — this
  task's fix stops the *resurrection*; the sibling stops the *accumulation*.
- The server-master mirror ref (`refs/remotes/gitPushcheck/master`) is created
  per pass on the client; it is harmless (a remote-tracking namespace) but is
  not pruned between passes (`--force` overwrites it each time — always one
  ref). A future cleanup could set it as a `--prune` rendezvous; not needed.
- Collision-rename uses HHMMSS timestamps (monotonic within a day; a re-tick
  within the same second is not expected — a pass has ≥1 git call per branch).
