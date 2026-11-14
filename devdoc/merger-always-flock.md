<!-- doc-class: historical -->
# merger-always-flock — the single-instance lock must cover the merge WORK, not only the merger LOOP

Task `01a0a500-90b1-78b6-bd29-26745c950f73`, branch `fix/merger-always-flock`.

## The epic's premise, and what was already true

The operator's framing: *"merger is supposed to handle git repo exclusively,
meaning even when merger is running without --daemon as front, it still needs to
flock to prevent a second instance to run. running front is necessary for
debugging purpose."*

Half of that was already implemented (epic `merger-foreground-flock`,
2026-09-12): `gopi merger` — foreground AND `--daemon` — wins `serve.lock`
before its loop runs, and holds it across a pass (pinned by
`TestMergerFlock_ForegroundHoldsItWhileWorking`). What that fix left open is the
**work**: the same repo-mutating merge engine is reachable from other command
faces that never consulted the lock.

## The measured hole

Two gopi processes, one independent `flock` holder on `~/.gopi/serve.lock`, a
sandbox repo with one clean-mergeable branch `feat/x`:

```
gopi merger --once             → exit 1  "another merge daemon is already running"
gopi task merge --all --repo R → exit 0  "merge feat/x merged clean merge,
                                branch deleted"   ← master ADVANCED, branch deleted
```

`gopi task merge --all` is not a niche face: it is what
`scripts/git-hooks/post-receive` fires on **every push** (one-line trigger, out
of band). So the single-instance guard protected the merger *loop* but not the
*merge*, and the hook could land work into master while the resident merger was
mid-pass.

## What the fix does

`cmd/agent/merger_work_lock.go` adds ONE caller-side rule: a face that lands
refs, deletes branches or reclaims worktrees must win the merger's flock first.

```go
defer mergerWorkLock(mergeFaceProg)()   // exit 1 naming the live holder
```

The lock itself is unchanged — `acquireServeLock` / `serveLockFilename`
(`cli_tasks.go`) remain the single implementation (Rule 14: no second lock
mechanism).

Guarded faces (each with its own diagnosed program name, Rule 1b):

| face | why it is merge work |
| --- | --- |
| `gopi task merge` | lands branches, deletes them — the post-receive hook's trigger |
| `gopi task merge-execute <id>` | lands ONE merge-backlog row onto master |
| `gopi task adopt` | its ③a half COMMITS probe-merged adoptions onto master |
| `gopi task merged-cleanup` | reaps merged branches + their worktrees |
| `gopi task sweep-worktrees --apply` | reclaims worktree registrations, reaps branches |

Two deliberate boundaries:

- **`sweep-worktrees` DRY RUN stays lock-free.** A bare run / `--dry-run` only
  reads and prints a plan; an operator diagnosing a busy host must not be
  refused by the sweep they are diagnosing. The guard sits behind
  `if !flags.dryRunGate()`, and the e2e test asserts a dry run still exits 0
  while the holder is live (the control that stops "guard everything" from
  passing as success).
- **A holder's own children are exempt** (`mergerWorkLockInherited`). The
  merger spawns two children *while holding the lock and while blocking on
  them*: the conflict RESOLVER (`tools.MergeResolveRoleEnv`) and the merge
  VERIFY child (`mergeVerifyRoleEnv`). A child that tried to win the lock would
  refuse the process its parent is waiting on — a lock turned into a deadlock.
  The verify child's spawn site carries the matching comment, so the exemption
  and the marker cannot drift apart.

Refusal semantics: exit 1, naming the lock file and the live holder. These are
script/hook triggers, so a refusal is the right shape — the branches stay
exactly where they were for the next merger pass (never a hang, never a merge
into a lock holder's repos).

## Verification (the epic's acceptance, executed)

`cmd/agent/merger_work_lock_e2e_test.go`, with a REAL separate holder process
(the suite binary re-exec'd as the holder fixture — never a self-pid shortcut):
private `GOPI_HOME` + settings + ledger, private temp repo, scratch sweep
throttled off, holder killed by pid.

1. **Refused while the holder runs** — all five faces exit 1, name the lock path
   and say another instance is running; the repo is byte-identical afterwards
   (master tip + branch ref unchanged).
2. **Accepted once the holder is gone** — `task merge --all` then exits 0 and
   actually lands the branch (a guard that could only refuse would be
   indistinguishable from a broken command).
3. **Read-only control** — `task sweep-worktrees --dry-run` still exits 0 under
   the live holder.
4. **In-process proof** — a foreign flock makes `mergerWorkLock()` refuse, and
   both child markers are recognized as exempt.

Red proof (same tree, the four production edits stashed): every face exits 0,
`master moved … while every merge face was refused`, `branch feat/x was deleted
…`. Green with the fix: `TestMergerWorkLock_MergeFacesRefuseWhileAHolderRuns`
PASS, `TestMergerWorkLock_RefusesInProcess` PASS.

## Not done / deliberately out of scope

- **The worker-side finish-cleanup chain** (`task run` / worker close-out:
  push → `worktree remove` → `branch -d`) is NOT put behind this lock. It is the
  legitimate owner of its own branch/worktree, it already has live-holder
  guards, and gate-ing it behind the merger's flock would stall task completion
  for the whole duration of a merger pass (which can block on a resolve child
  for minutes) for no protection the merger's own per-branch guards do not
  already give.
- **No source-level gate** asserting that a *future* merge-mutating face must be
  added to the guarded list. The e2e test enumerates the faces, so a new face is
  not covered until someone adds it; an AST gate ("every face in package main
  that calls tools.TaskMerge/TaskMergeBacklogExecute/AdoptionSweep/
  MergedCleanupSweep/WorktreeSweepRun must call mergerWorkLock or be listed as
  exempt") is the obvious follow-up.
- The child exemption is per-process-tree (an env marker), so a *descendant* of
  a resolver/verify child that itself ran a guarded face would also be exempt.
  Today neither child reaches a guarded face (the verify child runs
  `task verify`, whose runner takes no work lock), so the exemption is a
  deadlock safety net rather than an active path.
