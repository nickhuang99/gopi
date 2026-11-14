<!-- doc-class: historical -->
# Cleanup and merge are separate concerns — the close-out face stops gating on the merge lock

**Epic:** 01a14a0b · **Host:** peerbox (server) · **Date:** 2026-10-17

## The reported defect

```
$ gopi task finish-cleanup --task 01a1416f-… --apply
gopi task finish-cleanup: NOT EXECUTED
the single-instance lock is held by a long-lived resident loop (pid 3860139, argv: gopi merge --every 3m) after 67ms of looking.
a resident loop holds the flock for its ENTIRE life (it is released only when the loop's last descriptor closes), so no bounded wait can ever succeed — this run did not wait out its budget for it.
```

Two separate wrongs in one message: the face was **gated on merge at all**, and the
reason it gave for refusing was **not true**.

## Root cause ① — why the apply arm took the merge flock

`cmd/agent/cli_tasks.go`, `runTaskFinishCleanup`: the `--apply` arm called
`defer mergerWorkLockWaiting(finishCleanupFaceProg)()` **unconditionally** — on every
host, for every row. The chain it then runs is dispatched by one predicate,
`finishCleanupClientArmApplies()` (`internal/tools/task_finish_cleanup.go`):

| host shape | arm | what it does with refs |
|---|---|---|
| client **with** a peer target | `taskFinishCleanupChain` | **pushes** the branch, **deletes** the local ref → repo surgery → needs the lock |
| server / single node | `finishCleanupServerRecycle` | removes the run's **worktree**, **leaves the branch** for the merger → writes **no ref** |

So the lock was taken for the arm that touches no ref, and the coupling turned a
resident loop into a permanent refusal of the only face that reclaims worktrees.
The operator's ruling (this epic): on a server, finish-cleanup's whole job is to
remove the worktree and leave the branch; cleanup and merge are separate concerns.

## Root cause ② — "for its ENTIRE life" is false

`merge_lock_wait.go` claimed a `gopi merge --every D` loop holds the flock for its
whole life, so no bounded wait can succeed. The code contradicts it:

- `runMergeLoop` (`merge_run.go:1293`) calls `runMergePass(opts)` once per pass;
- `runMergePass` (`merge_run.go:1716`) takes the flock there
  (`mergeAcquireLock`, `:1810`) and defers the release at the pass end
  (`mergeReleaseLock`, `:1814`);
- the loop's own log says so per pass: `gopi merge: holding the single-instance
  lock; 1 repo(s) …` then `gopi merge: run over — 0 branch decision(s), lock
  released with the run`.

**Measured** (evidence under
`~/.gopi/sessions/01a14a0b-6eae-7dbc-bd4e-7978d1714f2e/artifacts/`):

- against the resident `gopi merge --every 3m` on this host (pid 3860139): 240
  non-blocking `flock -n` probes over 48 s → **240 free / 0 busy**;
- against a fixture loop whose pass is elongated by a declared `build:"sleep 15"`
  (so a pass is observable): 30 probes → **27 free / 3 busy**.

The lock is held **during a pass** and free **between passes**. A `--every` holder
is therefore an ordinary waitable holder reached at a different cadence — not a
permanent one, and not a reason to refuse.

## The fix

1. **The gate follows the arm** (`internal/tools` + `cmd/agent`):
   `tools.FinishCleanupNeedsMergeLock()` is a thin face over the chain's own
   `finishCleanupClientArmApplies()` (Rule 14 — one answer, so a caller can never
   guard for one arm while another runs). `runTaskFinishCleanup` now:

   ```go
   if tools.FinishCleanupNeedsMergeLock() {
       defer mergerWorkLockWaiting(finishCleanupFaceProg)()
   } else {
       fmt.Fprintf(os.Stderr, finishCleanupNoMergeLockLine, finishCleanupFaceProg, mergeEveryFlag)
   }
   ```

   The client arm's acquire path is **untouched**: where refs are written, the
   merge lock is exactly what it was. The plan-only arm was already lock-free.

   The lock-free arm **announces itself** (Rule 15): a run beside a merge pass and
   a run that could not start must stay distinguishable in a captured log.

2. **The sentence stops asserting a falsehood** (`merge_lock_wait.go`):
   `serveLockAcquireBounded` no longer short-circuits on a `--every` holder. It
   announces the holder (pid, argv, and the per-pass mechanism) and then waits for
   the gap between two passes like any other holder. If the budget *is* spent, the
   verdict names the holder, states the per-pass mechanism with the measured
   number, and points at the **LOCK** rather than at the loop (a loop that never
   lets go is not the loop's normal shape: it is a path-level problem — a replaced
   inode, a stray holder). The destructive advice stays forbidden by name
   (`Do NOT delete serve.lock`).

   No new mechanism: no second lock, no pid file, no heartbeat, no state file, no
   daemon, no sweeper (Rule 35). One env reader for the budget (`GOPI_MERGE_LOCK_WAIT`),
   unchanged.

## Verification (real surfaces, Rule 31)

**A/B in ONE held window** — a real resident `gopi merge --every 1s` loop whose
pass holds the flock ~15 s (declared `build:"sleep 15"`), two real ledger rows,
two real linked worktrees, both close-out runs launched while a probe said the
flock was **held** (`artifacts/ab-window.sh`):

```
########## A: pre-fix binary, flock held by the live loop ##########
   [probe] the loop is HOLDING the flock
gopi task finish-cleanup: NOT EXECUTED
the single-instance lock is held by a long-lived resident loop (pid 3988190, argv: …/gopi-fixed merge --every 1s) after 139ms of looking.
a resident loop holds the flock for its ENTIRE life … so no bounded wait can ever succeed — this run did not wait out its budget for it.
A exit=1

########## B: fixed binary, SAME live loop ##########
   [probe] the loop is HOLDING the flock
gopi task finish-cleanup: this host's chain is the server/single-node recycle (worktree removal only, no ref written), so it runs WITHOUT the single-instance merge lock — a resident `--every` loop no longer blocks it
01a14a0b-…-b002 branch=task/01a14a0b-armB done=[branch-checks,worktree] worktree-freed=/dev/shm/fc-e2e-y83c/wtB
finish-cleanup (APPLY): candidates=1 removed=1 settled=0 refused=0
B exit=0
```

After B: `git worktree list` drops the armB entry, `refs/heads/task/01a14a0b-armB`
**present** (the merger's input survives), the directory is gone; armA — untouched
by the refused pre-fix run — is still there, which is what makes A's refusal a real
refusal and not a no-op.

## Regression tests

- `cmd/agent/merge_lock_wait_test.go`
  - `TestFinishCleanupWaitsOutAResidentLoopBetweenPasses` — a `--every` holder must
    be **waited out**, not refused: the face acquires the lock once the pass ends.
  - `TestFinishCleanupLoopVerdictStatesThePerPassMechanism` — the budget-spent
    verdict must state `once per PASS`, `between passes`, the measured
    `240/240 free probes`, the lock path, and must **not** contain `ENTIRE life`,
    `no bounded wait can ever succeed`, `did not wait out` (the falsehood is the
    regression).
- `cmd/agent/finish_cleanup_merge_lock_arm_test.go` —
  `TestFinishCleanupApplyArmGatesTheMergeLock`: the apply arm consults the
  predicate and the lock-free arm announces itself.
- `internal/tools/finish_cleanup_merge_lock_test.go` —
  `TestFinishCleanupNeedsMergeLockFollowsTheArm`: drives the **real** settings
  resolver (role=server → false; and the exported face always equals the chain's
  own arm predicate).

Red proof: the pre-fix binary — built from `git archive b20906810^` — produces the
`NOT EXECUTED` / `ENTIRE life` output in step A above; the fixed binary turns the
same window into `removed=1`.

## Not done / honest limits

- **Not fixed here:** the non-blocking flock refusal path used by the faces that
  still call `mergerWorkLock` (`task merge`, `task adopt`, `task merge-execute`,
  `merged-cleanup`, `sweep-worktrees`). Those floors are deliberate (they reject
  rather than risk two writers), and this epic's constraint says the merge acquire
  path stays untouched; the false-life sentence they share was only in the waiting
  face's vocabulary and is gone.
- **Rebuild/merge reaped the loop:** the resident `gopi merge --every 3m` on this
  host (pid 3860139) **exited** on its own at some point during this session
  (image self-check applies to it; the binary was rebuilt by the merger around
  then). The A/B fixture therefore uses its own real loop, which is the property
  the epic needs — a real holder, a real flock, a real `--every` argv — and both
  halves ran inside it.
- The `GOPI_MERGER_LOCK_OWNER … no ANCESTOR … holds` lines in the fixture loop's
  log come from the loop's owner marker reaching a `gopi merger` child that is
  not under the holder; pre-existing, unrelated to this change.
