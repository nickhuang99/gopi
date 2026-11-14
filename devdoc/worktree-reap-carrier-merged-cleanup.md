<!-- doc-class: historical -->
# The worktree reap's carrier: `gopi task merged-cleanup` (2026-10-30)

## The defect, twice

`serveWorktreeReapSweep` is the ownership-INDEPENDENT worktree reap — the only
instrument that can collect a `.git/worktrees` registration **no ledger row
remembers**. Every other arm on the host is keyed on a row: the producer pass
needs a row stamped with this host's own claim, the dead-holder arm needs a
terminal row, the age arm's guard ② refuses a dirty tree.

**First occurrence (2026-09-16).** The reap was appended inside
`serveMergedCleanupSweep` (35faf715), whose only production caller had been
deleted on 2026-09-04 (d11af9a6). The wiring test stayed green while no resident
component ran the reap.

**Repair ① (2026-09).** The reap moved to `taskLifecyclePass`, described as
"the tick the worker loop AND the merger's per-tick pass both fire".

**Second occurrence (2026-10-26), same shape one level up.** Both halves of that
sentence were gone:

- the merger's per-tick pass was deleted 2026-09-19 with the resident merger;
- `187d05c` removed the WORKER round's call to `runTaskLifecycleTick` (operator
  ruling 2026-10-26: a worker dispatches, it does not reconcile the ledger).

So from 2026-10-26 `runTaskLifecycleTick` had **no production caller** — every
remaining reference was a test — and the reap was again reachable from its own
tests and from nothing else.

What made it survivable for a month is worth naming: the first repair pointed the
reachability test at a **function**, not at a **command an operator types**. A
function stays callable forever; a carrier can be deleted out from under it
without a single test turning red.

## The repair (chosen: option A — a live verb, not a new tick)

The reap's carrier is now `gopi task merged-cleanup`
(`runTaskMergedCleanup` → `mergedCleanupPassOnce` → `serveWorktreeReapSweep`).

Why this verb, and not a new resident carrier:

- **It already takes the merger's work lock** (`mergerWorkLock(mergedCleanupFaceProg)`).
  The reap mutates git bookkeeping — a registration is a directory under
  `.git/worktrees` — i.e. exactly the surgery that lock exists to serialise.
- **Its subject is the reap.** The verb reaps merged branches and the worktrees
  they leave behind; collecting a registration nothing else will ever collect is
  the same duty, not a second one.
- **It is live on every host that merges**, without a scheduler: Rule 35 forbids
  buying reliability with a cron line, a systemd timer, a resident script, a new
  config key or a new database. Option B (a self-triggering resident carrier for
  the tick) would have been exactly that.
- **It is ordered.** The merged-branch report prints first (this face's own
  verdict), the reap leg after it: an operator reading one command sees "what the
  merged branch's worktree became", then "which registrations nothing
  remembers".

A reap failure is printed on the verb's stream and never changes its exit code —
the cleanup report is the face's own verdict, and the reap's ladder (age floor,
holder pass, declared roots, a removal verb with no `--force`) already refuses
anything it cannot prove safe.

## What moved, and what did NOT come back

- The leg was **MOVED** off `taskLifecyclePassOnce`, not copied: two carriers race
  over one registration, and a leg left behind would keep the reachability proof
  green while the host had no reaper again — literally how the first occurrence
  survived its own test.
- **No dormant switch.** There is no flag or `if false` seam by which a later
  reader could re-arm the tick leg (the deleted verify-closure leg's precedent: a
  switch that keeps a scheduler for a thing that no longer exists).
- The docstring lie at `cmd/agent/cli_tasks_worker.go` ("fired by the worker loop
  AND by the merger's per-tick pass") is corrected in both places it appeared, to
  state the measured fact: the tick has no production caller today, and the one
  duty that could not wait for one moved to the verb.

## Evidence

Real invocation against a hermetic fixture (a repo, a declared worktree root, one
linked worktree whose directory is removed and whose admin entry is locked
`initializing` and aged 2 h — the shape `git worktree prune` skips forever):

```
merged-cleanup (/dev/shm/gopi-reap-demo-hlli4gef/repo): removed=0 would-reap=0 skipped=1 failed=0
scan: 1 linked worktree(s), 0 close-out candidate row(s)
skipped branch=feat/dead-run: git status: git status --porcelain: chdir …/wt-root/wt-dead: no such file or directory: (no output)
[worktree-reap] reaped registration=/dev/shm/gopi-reap-demo-hlli4gef/wt-root/wt-dead (registration is dead (its directory is gone) — the branch goes with it if git agrees (`branch -d`) — branch feat/dead-run deleted (git agreed it was safe))
[worktree-reap] pruned: Removing worktrees/wt-dead: gitdir file points to non-existent location
[worktree-reap] worktree-reap: reaped=1 branches-reaped=1 reclaimed=0 escalated=0 repos=1
exit=0
```

After the run `.git/worktrees` no longer exists.

Red proof (the leg removed from `mergedCleanupPassOnce`, test run, then restored
and verified by blob hash): `TestMergedCleanupPassInvokesWorktreeReap` failed with
`observed verbs=[]` and `TestMergedCleanupPassKeepsItsOwnReportFirst` failed on
the missing leg; both green after the restore
(`git hash-object cmd/agent/cli_tasks.go` = the pre-seed blob).

## The tests now pin the verb, not the function

`cmd/agent/worktree_reap_reach_test.go`:

- `TestMergedCleanupPassInvokesWorktreeReap` — the verb's body runs the reap and
  the reap ACTS (the dead registration is gone), in production posture.
- `TestMergedCleanupPassKeepsItsOwnReportFirst` — the output order the verdict
  promises.
- `TestLifecyclePassNoLongerReapsDeadRegistrations` — the inverted guard: the
  retired carrier must not quietly grow the reap back.
- `TestLifecyclePassEscalatesDirtyOrphanEndToEnd` — a dirty ownerless worktree is
  ESCALATED (log line + operator notify) and never removed, now driven through the
  verb's body.
