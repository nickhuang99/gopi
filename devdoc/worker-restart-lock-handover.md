<!-- doc-class: historical -->
# The worker restart's flock HANDOVER (2026-10-04)

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

**The failure.** `gopi worker --restart` stopped its predecessor, watched its pid go, and then
had its successor refused by the lock that predecessor still held:

```
gopi worker: pid 322819 exited 501ms after the graceful stop request
gopi worker: daemon start failed: another worker daemon is already running (lock
  /home/user/.gopi/worker.lock held by pid 322819 (no such process: the flock
  outlived its owner — an inherited descriptor copy, or a replaced path))
```

The epic's framing carried two candidate mechanisms in the refusal's own wording — **an
inherited descriptor copy** or **a replaced path**. Both are real hazards the code already
guards against, and *both are the wrong reading here*. What the command actually hit is
neither: it is a **handover**, and a handover that attempts `LOCK_NB` exactly once.

## 1. The mechanism: pid-gone ≠ flock-released

A `flock` is released when the **last descriptor of the open file description** is closed —
not when the process that holds it stops being visible. The restart's own wait reads the
*first* fact (`workerWaitForExit` polls signal-0 plus the `/proc/<pid>/stat` state letter), and
then starts the successor. The predecessor still owns the description through the rest of its
exit path (its state write, its stop-request cleanup, the loop's unwind), and the kernel
releases the lock only at the end of that.

So the successor is asking for a lock its own command has just *requested* be released, and
the interval between the two facts is decided by the scheduler — short on an idle host, long
enough to lose on a saturated one. That is why the case was green standalone and red at load
10.9.

**Measured (this host, 2026-10-04).** Driving the daemon through start → restart with
`GOPI_WORKER_IDLE_EXITS=0`, a 10ms sampler of `/proc/<pid>/fd` + `/proc/locks` caught the
disagreement directly: the kernel's lock record for the lock inode named a pid that was
**already gone** (`locks: ["559428"]` with `hold: []`), while the live holder of the open
file description was a *different, still-running* daemon. Two facts, one window.

**What the window is NOT.** An inode-replacement race (`acquireWorkerLock` checks
`sameFileAsPath` and refuses a replaced inode) or an inherited fd (`internal/borrowedfd` +
`CloseOnExec`). Both were checked and neither is in this path.

## 2. The fix: use the handover primitive that already existed

`daemonSpec.controlWait` **is** the handover primitive, and `cli_daemon_spawn.go` documents it
in as many words:

> `controlWait`, when non-nil, replaces the plain control acquisition with a … the successor
> cannot take the flock the instant it starts, and refusing it would make `--restart` fail for
> doing exactly what it was asked to do. The wait is the HANDOVER primitive — the ordering
> lives in the flock, so there is no sleep and no "quiet window" to tune.

The **merger's** spec was its only user. `workerDaemonSpec()` leaves `controlWait` nil, and
`workerDaemonSpawnErr` — the call the restart makes — went through it. The fix is therefore not
new machinery (Rule 35): it is the worker's spec picking up the acquisition its own restart
needs.

- `workerDaemonHandoverSpec()` = `workerDaemonSpec()` + `controlWait = acquireWorkerLockWait`;
  used **only** by `workerDaemonSpawnErr`.
- `acquireWorkerLockWait()` retries the **complete** acquisition (flock + stray-holder check +
  inode identity + pid stamp) under `workerLockHandoverWait` (10s) at
  `workerLockHandoverPoll` (100ms), and on expiry returns the **last refusal** — the diagnosis
  itself (pid + liveness), never a bare timeout.
- A plain `gopi worker --daemon` is untouched: `daemonSpawn(workerDaemonSpec(), …)` still takes
  the lock on the first attempt and refuses at once. `acquireWorkerLock()` itself is unchanged.

**Why the wait cannot hide a genuinely live daemon.** It is bounded, and its failure is the
production refusal text; a live daemon simply makes the operator wait `workerLockHandoverWait`
before being told, and `--status`/`--stop` are the answer to that. The distinction the fix
preserves is *"the lock is being released"* vs *"the lock is held"* — the only thing the
successor can act on.

## 3. The tests, and their honest limit

`cmd/agent/worker_daemon_handover_test.go`:

| arm | what it drives | what it asserts |
|---|---|---|
| A `TestWorkerDaemonRestartHandsOverTheLock` | the real `gopi worker --restart --wait …`, against a foreign process holding the profile's `worker.lock` and releasing it 1s in | no refusal text; exit 0; a successor pid it then stops by **explicit pid** |
| B `TestWorkerDaemonPlainStartRefusesAtOnce` | `daemonSpawn(workerDaemonSpec(), …)` against a **live** holder | refused, naming the holder, inside `workerLockHandoverWait/4` |

No load is generated (Rule 27): the window is a **fact** (a live flock holder), not a
saturation. The holder is a separate process because flock lives on the open file description —
a dup of the test's own description re-takes the lock successfully and cannot create the
contention. The package's existing holder (`bfdStartFlockHolder`) is reused, never rebuilt.

**The RED proof**: with `workerDaemonSpawnErr` restored to `workerDaemonSpec()`, arm A fails by
name — `... was REFUSED by a lock that was released 1s in` with the production refusal line.

**The limit, stated plainly**: these arms pin the *fix's contract*, not the 2026-10-04 run's
interleaving. The natural window was **not** caught live, and it was genuinely hunted: 8 + 30 +
30 driven start→restart cycles at load 7.3–14.8 with a 10ms sampler, whose only observed loss
was a *different* defect (`pid N is STILL running 5s after the graceful stop request`). The
loser shape had to be produced by fixture. A report claiming "deterministically reproduced the
saturation failure" would be false; what is deterministic is that the **base code loses
whenever the lock is still held when the successor starts**, and that the fixed code does not.

## 4. Not diagnosed here (deliberately)

- **`daemonAwaitReady` hangs under a `systemd-run` scope.** An in-process
  `workerDaemonSpawnErr` under this suite's cgroup-escaped scope never receives the child's
  readiness byte (`daemon did not report readiness within 30s`). Arm A therefore drives the
  **binary**. The hang is a live, reproducible oddity in `cmd/agent`, upstream of this fix, and
  belongs in its own task.
- **A test can spawn a worker daemon into the OPERATOR's profile.** Measured during this run:
  a `go test ./cmd/agent` process left a `gopi worker` daemon running with the operator's
  profile (`/home/user/.gopi/settings.json`, `PWD` = the run's worktree) while carrying this
  run's `GOPI_EXECUTOR_SESSION_ID` / `GOPI_TASK_RUN_ID`. It self-exited after 3 idle rounds
  (~90s) and left nothing behind, but for that window it held `/home/user/.gopi/worker.lock` —
  a real, if narrow, interference with the operator's own machines. Filed separately.
- **`--restart` prints `replaced pid 0, binary unknown`** when its predecessor was already
  gone. Pre-existing, untouched.
- **`TestStyleGate` is red ON MASTER** (measured on tip `5e63ec6c`: 10 `ignored-error`, 6
  `test-sleep` and 2 `test-setenv` hits, all in files this branch does not touch). Filed as its
  own task — it is a tree-wide sweep, and it makes any scoped run that includes
  `./internal/stylegate` read red for a reason that is not on the branch.
