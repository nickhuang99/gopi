<!-- doc-class: normative -->
<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

# Test-Process Leak: a SIGKILLed test binary left real gopi daemons running

> **WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md** — the operator owns this; an agent must never run it (submit the work instead: `gopi task submit`).

Task: `~/workspace/gopi-test-leak`, branch `fix/daemon-e2e-leak` (base `8605d4f2`).
Incident date: 2026-09-14. Author: gopi (worker session).

## 1. Symptom and what it actually was

`cmd/agent`'s e2e cases start REAL gopi daemons (`gopi worker --daemon`,
`gopi merger --daemon`) and stop them from `t.Cleanup`. When the test PROCESS was
killed outright — `go test -timeout` firing after the panic dump, `kill -9`, the
OOM killer — the daemons were reparented to PID 1, survived for hours, kept
their (soon deleted) `/dev/shm` sandbox files open, and filled the tmpfs to 100%,
at which point unrelated suites started failing.

**Bug distinction (deliberate, because the first reading of this incident was
wrong).** This was NOT a missing cleanup call. Every one of the 20 daemon-starting
cases in `cmd/agent` already had a `t.Cleanup` that stops its daemon, and those
hooks work — `testing` runs them on `t.Fatal` and on a panic. The defect is a
category error about what a cleanup hook can cover: **a killed process runs no
user code at all**, so no test-side hook — however careful — can be the mechanism
that stops a daemon. Anything that must survive a SIGKILL has to be enforced by
the KERNEL, which does run: it closes the process's descriptors.

So the fix has two halves, and both are needed:

- **prevent** (binding): make a daemon's life depend on the test process's life
  through a kernel-closed descriptor, so the daemon dies on its own;
- **clean up** (sweep): a daemon orphaned by an *earlier* run (started before
  this mechanism existed, or by a run where arming failed) is already
  unreachable — nothing can bind a detached process — so the next run of the
  suite finds it by ownership and stops it.

## 2. Prevent: the death pipe

`internal/daemonlifetime` (production code: the watchdog runs INSIDE the daemon).

- The holder creates a pipe. The daemon child receives the READ end; the holder
  keeps the WRITE end for its whole life.
- The daemon arms a watchdog goroutine that blocks in `read()` on that fd. When
  the holder dies — SIGKILL included — the kernel closes the write end, the read
  returns `(0, EOF)`, and the daemon SIGKILLs itself, after writing WHY to its
  log (Rule 15: an unexplained daemon death is exactly the confusion this epic is
  about).
- A production start arms nothing: the mechanism is opt-in through an
  environment variable that no production path sets, so an operator's
  `gopi worker --daemon` keeps its detached lifetime (asserted by
  `TestUnboundDaemonSurvivesItsSpawner`).

### The suite-level shape (a listen-fd), and why per-spawn was wrong

The first design required every daemon-spawning site to create a pipe and name
its fd. That put a number at N call sites — and the number is a trap (below).

The shipped design arms ONE pipe per test binary
(`daemonlifetime.ArmThisProcess()`, called by the suite's `TestMain`):

- the READ end keeps its fd across `exec` (its `FD_CLOEXEC` is cleared), so every
  descendant — including the short-lived launcher that fork/execs the real
  daemon — inherits and passes on the binding;
- the WRITE end keeps `os.Pipe`'s `O_CLOEXEC`, so NO descendant can hold it:
  only the test binary's death closes the pipe.

One place arms it, every daemon a test binary starts is covered, and no call site
computes a number. `testutil.ArmAndSweepForSuite(suite)` is that one place, and
it also runs the orphan sweep.

### The fd number is a trap — four MEASURED failures

Each of these was a real failure during development, and each is why the shipped
code is shaped the way it is. They are recorded here because they are all
silent-failure shapes: the daemon starts, nothing looks wrong.

1. **`os/exec` renumbers `ExtraFiles` from fd 3.** Naming the fd the descriptor
   has in the *parent* makes the launcher's check refuse it and the daemon runs
   unbound. `daemonlifetime.ExtraFilesOffset` is the single source of that
   arithmetic.
2. **fd 3 is CLOBBERED by `ExtraFiles[0]`.** The suite's inherited read end was
   at fd 3, and `cmd/agent`'s launcher passes a report pipe as `ExtraFiles[0]` —
   so the daemon's watchdog ended up reading that pipe's write end, got `n=0`
   with `EBADF`, read it as "the holder is gone", and self-terminated instantly.
   A bound daemon dying immediately. Fix: the inherited read end is moved above
   the renumbering range (`daemonlifetime.InheritFDFloor`, currently 200) with
   `F_DUPFD`, which also clears close-on-exec in the same call.
3. **The holder must be the process whose death is being detected.** A version
   had the test process hold the write end while a spawner child started the
   daemon: SIGKILLing the spawner proved nothing, because the write end was still
   open in the test process. Every holder now arms its OWN pipe.
4. **Arm, then build the environment.** Arming exports the holder variable; an
   environment captured before it does not carry the binding, and the daemon
   again starts unbound while everything looks right.

A fifth, non-fd trap: **`select {}` is not a way to wait** in a fixture whose
whole job is to stay alive. When the blocking goroutine is the only one left, the
Go runtime reports "all goroutines are asleep — deadlock!" and EXITS — which for
a bound fixture is indistinguishable from the binding working. The wait is a
timer loop (`cmd/agent.WaitForever`).

## 3. Clean up: the ownership-scoped orphan sweep

`internal/testutil/orphansweep.go`.

- A candidate must be an ORPHAN in the daemon SHAPE: session leader
  (`pid == session`) with `PPID == 1`.
- It must be OWNED by the scope: its cwd, its `GOPI_HOME`, or an open file inside
  the scope root. **Never a name-based `pkill`** — the negative case
  (`TestOrphanSweepLeavesAForeignProcessAlone`) builds a process with the same
  argv and the same orphan shape in a neighbouring directory and asserts the
  sweep neither lists nor touches it.
- An empty/blank root is REFUSED (an unscoped sweep is the one accident this must
  not enable).
- The suites call it at start-up (`SweepOrphansAtSuiteStart`), so the sweep of
  run N cleans up what run N-1 left.

Liveness is never a bare `kill(pid, 0)`: the triple is **pid + starttime
(`/proc/pid/stat` field 22) + cmdline**. `internal/testutil/procident.go` carries
the three measured lies that motivate it, each with a test:
a recycled pid that still answers signal 0, a ZOMBIE that still answers signal 0
(and whose cmdline is EMPTY by design — so the state letter must be checked
BEFORE the cmdline), and a stat line whose `comm` field contains spaces and `)`.

## 4. Per-site audit of the 20 daemon-stop sites (deliverable E)

All 20 `t.Cleanup` daemon-stop sites in `cmd/agent`, with the verdict:
**KEEP** = the hook stays exactly as it is (it is correct for every non-SIGKILL
exit and is what makes a test's own run tidy); the binding added underneath it
is what covers the SIGKILL case. No site is changed by this branch, and no site
NEEDED a change — that is the point of arming at the suite level: 20 sites, zero
edits, nothing forgotten.

| file:line | stop helper | what it stops | verdict |
|---|---|---|---|
| `cli_daemon_cwd_test.go:140` | `daemonCwdTestStop` | worker daemon (SIGTERM) | KEEP |
| `daemon_cgroup_e2e_test.go:183` | `dcStopPID` | daemon under test | KEEP |
| `daemon_cgroup_e2e_test.go:351` | `dcStopPID` | restart target | KEEP |
| `daemon_cgroup_e2e_test.go:392` | `dcStopPID` | restart replacement | KEEP |
| `daemon_scope_restart_e2e_test.go:64` | `dcStopPID` | restarted daemon | KEEP |
| `merger_graceful_restart_e2e_test.go:332` | `mgrKillPID` | probe child | KEEP |
| `merger_graceful_restart_e2e_test.go:450` | `mgrKillPID` | probe child | KEEP |
| `merger_graceful_restart_e2e_test.go:578` | `mdStopPID` | merger daemon | KEEP |
| `merger_graceful_restart_e2e_test.go:601` | `mdStopPID` | replacement merger | KEEP |
| `merger_graceful_restart_e2e_test.go:635` | `mdStopPID` | first of a pair | KEEP |
| `merger_graceful_restart_e2e_test.go:646` | `mdStopPID` | second of a pair | KEEP |
| `merger_lock_ownership_e2e_test.go:394` | `mlKillPids` | resolver + grandchild | KEEP |
| `merger_lock_ownership_e2e_test.go:460` | `mlStopDaemon` | lock-owning daemon | KEEP |
| `merger_lock_ownership_e2e_test.go:464` | `mlKillPids` | resolver + grandchild | KEEP |
| `merger_stop_target_e2e_test.go:58` | `mdStopPID` | merger daemon | KEEP |
| `worker_daemon_flag_e2e_test.go:349` | `wdStopPID` | worker daemon | KEEP |
| `worker_daemon_flag_e2e_test.go:482` | `wdStopPID` | worker daemon | KEEP |
| `worker_daemon_flag_e2e_test.go:513` | `wdStopPID` | worker daemon | KEEP |
| `worker_daemon_singleton_e2e_test.go:84` | `wdStopPID` | singleton daemon | KEEP |
| `worker_daemon_singleton_e2e_test.go:356` | `wdStopPID` | second daemon (must NOT start) | KEEP |

Non-daemon `t.Cleanup` sites in the suite (42 others: scratch dirs, mock
processes, temp homes) are out of scope — they hold no reparented-daemon risk.
`StartMockProcess` fixtures are bounded by their own hold timeout.

## 5. Acceptance evidence

- `TestDaemonDiesWhenItsSpawnerIsSIGKILLed` — real `bin/gopi worker --daemon`
  started by a re-exec'd spawner child; the spawner is SIGKILLed; the daemon is
  asserted GONE by the triple; the sandbox is swept and asserted EMPTY. The
  daemon is asserted ALIVE before the kill, so the case cannot pass vacuously.
- `TestBoundDaemonStaysAliveWhileItsSpawnerLives` — the negative half: a bound
  daemon must NOT die while its holder lives (a watchdog firing immediately would
  otherwise satisfy the case above).
- `TestUnboundDaemonSurvivesItsSpawner` — the production shape: an unarmed
  daemon outlives its spawner, so the mechanism is opt-in and operator daemons
  are unchanged.
- `internal/testutil`: the same shape at fixture level (holder child SIGKILLed →
  bound child dies), plus the pid-reuse/zombie/stat-parser cases and the sweep's
  three cases.

## Not Done

- `TestEveryTempUsingSuiteIsPinned` and `TestWorkerOwnershipPointerPresent` fail
  in this worktree on files this branch does not touch (`internal/daemoncwd/`,
  five devdoc files that predate the base commit). Pre-existing at base, left
  alone: fixing them is a different task.
- The sweep is invoked at suite start-up for `cmd/agent` and `internal/testutil`.
  Other suites that start daemons (if any appear later) must call
  `testutil.ArmAndSweepForSuite` themselves; nothing enumerates them
  automatically.
- The 20 sites keep their `t.Cleanup` hooks. Deleting them (relying only on the
  binding) was rejected: a hook is the right mechanism for a clean run, and the
  binding is the right mechanism for a killed one.
