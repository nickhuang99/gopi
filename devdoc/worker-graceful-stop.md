<!-- doc-class: historical -->
# A stopped run still releases its OWN worktree — the graceful-stop half

> **WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md** — the operator owns this; an agent must never run it (submit the work instead: `gopi task submit`).

Branch: `fix/worker-graceful-stop` (worktree `~/workspace/gopi-wt-release`,
based on `fix/worker-self-release` @ `cd6ff9f8`)

## The defect (sandbox rig `/tmp/self-release-ZhF9`, round 2026-09-12)

The producer-side release (`83e7c531`: "a finished run releases its OWN
worktree") **is a defer on the round's close-out** — `workerRoundOwnWorktrees`
in `cmd/agent/cli_tasks_worker.go` and the wrapper's own call in
`runTaskRunOnce` (`cmd/agent/cli_tasks.go`). Go's default SIGINT/SIGTERM
disposition kills the process immediately, and **`gopi worker` trapped no
signal at all**, so any stop that landed mid-round unwound *nothing*.

Measured, in the rig the epic names:

| fact | value |
|---|---|
| supervisor | `timeout 300 bash scripts/acceptance-self-release.sh` |
| worker | `gopi worker --once` (pid 3840674, claim stamp `peerbox:3840674`) |
| worker exit | **143** (= SIGTERM), last stdout line `worker: claimed 01a0962e-…` |
| own-release lines anywhere | **none** (worker.log, session output.log, notify sink) |
| row 01a0962e | still `in_progress`, `claimed_by=peerbox:3840674` |
| `git worktree list` | still names `/tmp/self-release-ZhF9/wt` |
| safe-merge gate [c] | still refuses `feat/self-release-e2e` |

So the rig reproduced **the exact defect the producer release exists to fix —
one signal earlier**: a run that was already over kept its ground, and the
finished deliverable was withheld silently. (Measured, not inferred: the
`timeout` child is the worker itself, so the signal hits the worker directly;
and a *child started by* `timeout` survives, which is why the fix has to be
in-process — nothing outside can release the ground for it.)

## The fix — one flag, two faces, the repo's existing pattern

`cli_chat.go`, `cli_web.go` and `merger_graceful.go` already establish the
house pattern: the signal handler **records a flag and does no work** (signal
context: no locks, no I/O, no exit), and the loop acts at its own boundary.

`cmd/agent/cli_tasks_worker_stop.go` (new):

* `installWorkerStopSignals()` — SIGTERM/SIGINT → `workerRequestStop(reason,
  force)` (first writer wins the reason; a *second* signal is the escalation),
  then **forwards** the stop to the in-flight run child.
* The forward uses `livePassChildren()` / `registerPassChild` — the merger's
  **one** child registry (Rule 14), not a second list. `runTaskRunOnce` now
  registers its child with kind `mrsChildKindRun`, pid **and** group, so the
  escalation arm reaches a grandchild (a build/test the executor spawned)
  without touching this worker's own group.
* `awaitWorkerStopChild()` — the loop waits (bounded, `GOPI_WORKER_STOP_GRACE`,
  default 5s) for that child, then **terminates** whatever is left. Rationale:
  the close-out *is* the guarantee, and the supervisor that sent the stop may
  escalate to SIGKILL next — a worker that dies waiting loses exactly the
  release. A second signal skips the wait.
* `runTaskWorker` returns only at a **claim boundary** (it never claims again
  once a request is banked) and exits **0** — "you asked it to stop and it
  stopped cleanly" is SUCCESS (the `mergerGracefulExitCode` contract). A
  drained-queue wait wakes early on the request (`waitWorkerIdleOrStop`).
* `runTaskRun` (the standalone wrapper face) installs the **same** handler: its
  close-out holds the terminal writeback and the same release.
* Every stop prints one line naming the reason and that the round was closed
  out — a stopped worker must not look like a crashed one (Rule 15).

Nothing about *what* may be freed changed: the release still runs through the
existing guarded predicate `reclaimDisposition` + `reclaimGitRemove` (clean
directory, no live process inside, still on the row's branch, never `--force`,
**branch never touched**), and `GOPI_SELF_WORKTREE_RELEASE` stays the
report-only kill switch. This epic only guarantees that the pass **runs**.

### Ordering, and why it is the whole fix

```
round: claim → runTaskRunOnce(child …) → writeback → workerDetachOutputLog()
       └ defer: workerRoundOwnWorktrees()   ← the release
       └ defer: workerCloseOutputLog()
signal arrives mid-round
  → handler records the request, forwards SIGTERM to the child
  → the child winds down; the wrapper's cmd.Wait returns
  → the round's close-out runs as usual: writeback + own-release (in the
    task's own output log) — and only then does the loop exit
```

## Verification

| check | result |
|---|---|
| `go vet ./cmd/agent/ ./internal/tools/` | clean (`VET=0`) |
| `go test -run TestWorkerStopReleasesOwnWorktree ./cmd/agent/` | **PASS** (0.41s) |
| existing release acceptance set (`TestWorkerReleaseOwnWorktree_Acceptance`, `TestOwnRelease_RefusesLiveSibling`, `TestAcceptance_DeadHolderReclaimUnblocksSafeMerge`, `TestAcceptance_LiveHolderKeepsSafeMergeRefused`, `TestAcceptance_VanishedWorktreeLeavesNoFailureLine`) | all PASS — the pair is still green |
| sandbox E2E `scripts/acceptance-self-release-stop.sh` with the fixed binary | **VERDICT: PASS** — `worker exit=124` (the supervisor stopped it mid-round), release line present, `released=yes branch-tip-kept=yes peer-kept=yes peer-tip=same` |

`TestWorkerStopReleasesOwnWorktree` drives the **real** signal path, not a flag:
a real repo + linked worktree, a row claimed by `host:pid` of the test process,
a **real** `sleep` registered the way `runTaskRunOnce` registers (kind + group)
and put in **its own process group** (so nothing but the forward can end it), a
**real** `syscall.Kill(os.Getpid(), SIGTERM)`, the production
`awaitWorkerStopChild()` and then the production
`tools.ReclaimOwnFinishedWorktrees()`. The elapsed-time assertion is what proves
the forward happened (the wait ends in one poll interval, not at the pinned 3s
grace), and the test env is named in its own comment because the whole
assertion rests on it: the child is group-isolated, so "it exited" can only
mean "the handler forwarded".

Sandbox E2E output (this sandbox, fixed binary):

```
C: worker exit=124 (124/143 = the supervisor stopped it mid-round)
   worker: own-worktree-release: own worktree released: feat/self-release-e2e (01a09651-…) freed for merge; branch kept
   worker: terminated received — stopping after the in-flight round's close-out (own-worktree release included)
D: released=yes branch-tip-kept=yes peer-kept=yes peer-tip=same
VERDICT: PASS
```

## Not done / honest limits

* **SIGKILL is unchanged and uncatchable.** `kill -9` still takes the close-out
  with it; the dead-holder sweep (`ReclaimDeadHolderWorktrees`) is the arm for
  that, and it now also has a producer-side leg whose window is only as wide as
  one round.
* **The wait is bounded, so a child that ignores SIGTERM is terminated** after
  `GOPI_WORKER_STOP_GRACE` (default 5s). Its row is then written back as failed
  by the wrapper's normal `cmd.Wait` path; the release runs regardless. A stop
  during a long verification run therefore cuts that run short by design — the
  operator asked for the stop.
* **A stop between claim and the child spawn** is handled by the loop-boundary
  check: the round returns, the close-out runs, nothing is claimed again.
* The registry is per-process; a stop that arrives while the worker is *between*
  rounds has no child to forward to (nothing is in flight) and simply exits at
  the boundary — correct, but worth naming.
* The rig's own `timeout 300` still reports 124: the SCOPE is one round, not the
  supervisor's deadline. The acceptance script therefore judges the release, not
  the exit code.
