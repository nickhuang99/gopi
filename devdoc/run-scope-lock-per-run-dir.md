<!-- doc-class: normative -->
# Run-scope lock: one run directory, one kernel flock

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

**Status:** implemented (epic `run-lock-per-run-dir`, task `01a1a88b`).
**Package:** `internal/runlock` · **Seams:** `cmd/agent/run_lock_seam.go`,
`runTaskRunOnce` (`cmd/agent/cli_tasks.go`), `workerRoundRun`
(`cmd/agent/cli_tasks_worker.go`).

## What it is

Before a process executes a run directory (`<gopi-dir>/sessions/<run-id>/`), it
takes an exclusive, non-blocking **kernel** `flock` on `<dir>/run.lock`:

```go
f, err := os.OpenFile(filepath.Join(dir, "run.lock"), os.O_CREATE|os.O_RDWR, 0o600)
if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
    // EWOULDBLOCK ⇒ another process is executing this directory: refuse, name
    // the lock path and the holder's stamp, and DO NOT touch the ledger.
}
```

The handle that keeps the descriptor open IS the lock. `runlock.Lock(dir)`
returns it; `(*Handle).Release()` frees it (idempotent); the process ending —
exit, panic, `kill -9` — frees it because the kernel does.

## Why the kernel and not the ledger's status column

The status column was carrying three incompatible jobs at once: mutual
exclusion, progress display, and crash recovery. It does the first one badly in
three measured ways, and each has a real cost:

| shape | measured |
|---|---|
| status written BEFORE the child is spawned | a crash in between leaves a row that says "running" and runs nothing |
| status never cleared when the holder dies | a row stayed `in_progress` for 8h38m after its process was gone |
| clearing needs ANOTHER process | a cleaner (a resident mechanism) whose own failure is invisible |

A flock has none of the three. **The kernel is the cleaner**, and it cannot
forget: the lock is released the instant the holder ends. There is no zombie
lock, no sweep, no timeout, no resident process (Rule 35).

## The three rules the package exists to keep

1. **The verdict is the flock alone.** The file's contents (`pid`,
   `startticks`, `acquired_at`, `host`) are for a human reading a refusal.
   Reading the pid and guessing liveness re-implements the very mechanism that
   is failing, and inherits every way a pid can lie (recycled, other host,
   unreadable).
2. **No `O_EXCL`.** Creating the file exclusively looks like a lock and becomes
   a PERMANENT one the moment the holder is killed — precisely the zombie.
3. **The lock file is never deleted.** `flock`'s subject is the inode, so
   unlinking races: a third process can create a NEW inode at the same path and
   lock that one, and two processes then both "hold" the same directory. The
   file stays; its content is overwritten by each holder.

## Scope: one run directory, never a machine-wide singleton

Two different run ids execute concurrently and must never block each other. The
lock lives IN the directory it guards, so the directory's own name is the whole
address — no table, no registry, nothing that goes stale.

## Where it sits, and the one arm it cannot precede

```
resolve the run id
  ↓
flock(<run-dir>/run.lock)  ── EWOULDBLOCK ⇒ exit non-zero, name lock + holder
  ↓
singleton guard → claim CAS → session workdir move → spawn child → …
```

* **An explicit row** (`gopi worker <id>`) knows its id at entry, so it locks
  **before** the singleton guard and **before** the claim CAS. A refused run
  leaves the ledger completely untouched.
* **A worker round whose row was already claimed** (`workerWriteback`) and a run
  that POLLS for the next row learn their id *from the claim*. They lock at the
  first statement that has it — before the dead-repo repair, the workdir move
  and the spawn. This is an honest limit of "lock before claim", not a choice:
  the id they must lock is produced by the claim they would have to precede.

The worker takes the lock itself (`workerRoundRun`, before `taskWorkdirEnter`)
and rolls the claim back on a refusal; the run funnel then recognises the same
process as the holder (`taskRunTakeLock`'s hand-off) instead of flocking twice.
That hand-off is **not re-entrancy**: flock is per open file description, so a
second `open()` in the same process is refused by its own first descriptor — the
worker would deadlock against itself.

## Verification (measured, not asserted)

Package tests (`go test ./internal/runlock/`, 8 arms, all PASS) include the
reverse control the epic asked for: the SAME two-child harness with the lock
switched off lets BOTH children enter (`TestWithoutTheLockBothProcessesEnter`),
which is what proves the refusals come from the lock and not from a serialising
harness.

Real cross-process run (sandbox profile, `bin/gopi` built from this branch):

```
########## ARM 1+2: A holds; gopi worker B must be REFUSED ##########
A lock file content (written by A):
pid=900826
startticks=173657408
acquired_at=2026-11-04T22:39:47Z
host=hosta
gopi worker B (same run dir) output:
worker: gopi worker: refusing to run 01a1a8b1-...: run directory is already
  locked by another process: ".../sessions/01a1a8b1-.../run.lock" (held by
  pid 900826, startticks 173657408, acquired_at 2026-11-04T22:39:47Z,
  host hosta)
B exit code: 1

########## ARM 3: kill -9 A -> acquire IMMEDIATELY ##########
A killed with SIGKILL (pid 900826)
immediate re-acquire: SUCCESS
elapsed_ms=4 (no sleep, no sweep, no cleaner)
the lock FILE still exists after the holder died: -rw-rw-r-- ... run.lock

########## ARM 4: two DIFFERENT run ids hold at the same time ##########
RESULT: BOTH holders alive at once (D=901209 E=901270) — per run directory,
  not a global singleton
```

Script: `~/.gopi/sessions/01a1a88b-.../evidence-runlock-crossproc.sh`.

## Two traps this mechanism was measured against while verifying it

1. **A pid is not a holder.** A shell that flocks and then `sleep`s without
   `exec` gives the descriptor to the `sleep` CHILD; killing the subshell leaves
   the flock held by a process nobody was looking at. This is the same class of
   error as "read the pid and judge liveness" — which is why the verdict is the
   flock and never the stamp.
2. **A holder started inside `$(…)` hangs the harness.** Command substitution
   waits for its pipe to close, and a long-lived holder keeps the write end
   open. The holder must be started at top level.

## Not done (honest)

* `gopi session resume` / the dead-holder-resume child (`gopi --session <id>`)
  does not take the run lock. It is a bare-prompt resume with no task row and no
  claim, so it is outside this epic's "before the claim CAS" framing; filing it
  separately is the right size (Rule 41).
* The two claim-first arms still claim before they lock (stated above). Closing
  that would require the CAS to hand out the id without claiming it, which is a
  ledger-semantics change this row was explicitly told not to make.
