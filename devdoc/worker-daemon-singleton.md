<!-- doc-class: normative -->
# Worker Daemon Singleton + Control Surface (`gopi worker --daemon` like `gopi merger`)

Epic: **worker-daemon-singleton** (2026-09-13). Branch `feat/worker-daemon-singleton`.

> **WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md** — the operator owns this; an agent must never run it (submit the work instead: `gopi task submit`).

## Why

`gopi worker --daemon` could be started any number of times. Nothing said whether a
resident worker existed, and the only way to end one was to find its pid and `kill`
it — which abandons an in-flight round's close-out (its terminal writeback and the
producer-side own-worktree release, the very things `cli_tasks_worker_stop.go` exists
to protect). The merger had already solved this (epic merger-graceful-restart): a
single-instance flock, per-pid state and request files, and a `--stop/--restart/--start`
control CLI. The epic asks the worker to behave the same way **without** losing the
thing that makes a worker easy to debug: running one in the foreground.

## What changed

### 1. The mechanism is SHARED, not copied (Rule 14)

`internal/tools/service_control.go` — new. The merger's control-file primitives,
parameterized by component:

| helper | purpose |
| --- | --- |
| `ControlDir` / `ControlDirReadOnly` | `<gopi-dir>/<component>`, create / don't-create |
| `ControlStatePath` / `ControlRequestPath` | `<prefix><pid>` encoding (one encoder) |
| `ControlPidFromName` | the one decoder |
| `PidAlive` | signal-0 **+ zombie check** (the one liveness predicate) |
| `LiveStateFiles` / `LiveStopRequestPIDs` | live instances / pending requests, pid-ascending |
| `WriteControlText` / `ClearControlText` | tmp+rename durable write, idempotent clear |

`merger_graceful.go` / `merger_status.go` now **delegate** (`MergerGracefulDir` →
`ControlDir`, `MergerPidAlive` → `PidAlive`, `MergerLiveStateFiles` →
`LiveStateFiles`, …), so there is exactly one implementation of each. The merger
status probe's shared parts were exported (`LockProbe`, `LockHolderNote`,
`ReadStampedPID`, `StatusAlive`/`StatusNotRunning`/`StatusOrphanLocked`).

**What is deliberately NOT shared: the state SCHEMA.** The merger's record carries
pass/repo/merge counters and an interrupt descriptor; the worker's carries round
counters. Only placement, the pid↔name encoding and liveness are common — a shared
struct would be a superset every component half-fills.

### 2. `--daemon` is a singleton; the FOREGROUND face is not

`cmd/agent/cli_worker_control.go` — new (the control surface + the lock).

- `acquireWorkerLock` takes `flock(LOCK_EX|LOCK_NB)` on `<gopi-dir>/worker.lock`.
  It is passed as `daemonSpec.control`, so the **spawning parent** takes it (a refusal
  is synchronous and there is no post-fork race) and the child inherits the fd via
  `ExtraFiles` (`workerDaemonInheritLock` re-takes it on the **inherited fd** — never a
  re-open by path — and `syscall.CloseOnExec`s it, because the inherited fd is an
  `ExtraFiles` dup with no `O_CLOEXEC`; without the fence every exec'd run child would
  carry a copy and keep the lock alive past the daemon's death: the merger's 2026-09-12
  incident).
- The child **restamps its own pid** into the lock file (the parent's pid dies
  immediately; a stale stamp would point `--status` at a corpse).
- The lock is a **separate file from `serve.lock`**: a running worker must never
  refuse a merger and vice versa.
- The refusal names the holder (`pid N` + liveness) and points at `--status` /
  `--restart`.
- `runTaskWorker` branches: a daemon child → `workerDaemonRun` (inherited lock →
  `runTaskWorkerLoop`); otherwise → `runTaskWorkerLoop` directly with **no lock**. The
  foreground face is byte-identically unlocked, so `gopi worker` in the front always
  works — the epic's observability requirement.
- `workerDaemonLockRef` holds the inherited `*os.File` for the process's life: an
  unreachable `*os.File` is finalized by the GC, which **closes** the descriptor, and
  flock dies with the last copy of the open file description — the guard would vanish
  mid-run while the daemon kept sweeping.

The daemon's loop lifetime contract is **untouched**: same rounds, same idle wait,
same self-exit after `GOPI_WORKER_IDLE_EXITS`. Detaching decides who owns the
terminal and who holds the guard, never how long the loop lives.

### 3. State file + durable stop

- `workerControlState{PID, Started, Rounds, LastRoundStart, LastRoundEnd, Note}` is
  written at every round boundary (`worker-state-<pid>`), so `--status` can answer
  "what would a stop have to wait for?" without probing the process. `Busy()` compares
  the **timestamps**, never a boolean (a boolean would be a second source of truth and
  could drift from what the file shows).
- `workerDaemonCtx` is the process's plumbing; it is **inert** for the foreground face
  (every method is a no-op), so the round code calls it unconditionally — one shape, no
  branching at the call sites.
- A durable per-pid request file (`TERM_<pid>`, `FORCE_<pid>`) is polled every
  `workerFileWatchInterval` (2s) and read at each **claim boundary**. The **signal is the
  nudge, the file is the record** — either alone is sufficient, so a signal that raced
  start-up cannot strand a daemon.
- `SIGUSR1` is the FORCE spelling (the merger's convention); a **second** signal also
  escalates. Both feed the one existing `workerStopState` flag: `force := !first ||
  workerForceSignalRequested(sig)`.

### 4. The control CLI

```
gopi worker --status                     # READ-ONLY: no lock, creates nothing
gopi worker --stop    [--wait 25m] [--force]
gopi worker --restart [--wait 25m] [--force]
gopi worker --start
```

Mirrors the merger's shape and exit codes (0 = requested end state holds, 1 = not
reached). `--stop` is the one primitive; `--start` starts a daemon; `--restart` is
stop-then-start, the atomic spelling of the two steps an operator would otherwise type
by hand (up to 10 minutes of polling). `--force` sends the durable FORCE request +
`SIGUSR1` **and** `SIGTERM` (a pre-epic binary has no USR1 handler and must not be left
running). Every signal records its intent via `signalrec.Intent` first, so the victim's
own "SIGTERM received" line becomes answerable.

## Two bugs found and fixed during verification

1. **Control flags leaked into the spawn validation.** `--restart` reached
   `workerDaemonSpec().prep` (the shared scanner), which correctly refused it as an
   unknown worker argument — so `--restart` stopped the old daemon and then failed to
   start the new one. Fixed by having `workerControlStartDaemon` pass
   `workerDaemonLoopArgs(args)` (the loop-flag whitelist) instead of the raw argv. The
   e2e case WS6 caught this: the restart reported exit 1 with a *stopped* daemon, the
   worst shape (work ended, nothing replaced it).
2. **The `*os.File` GC hazard** described in §2 — caught while reasoning about the
   hand-off, not by a test; a GC close of the lock fd would silently release the
   singleton guard. Guarded structurally by the package-level reference.

## Verification

- Commit: `63f72bd1` (branch `feat/worker-daemon-singleton`; feature commit `4ac68179`).
- Build: `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` = OK; `gofmt -l` clean on the touched trees.
- Tests: `go test ./internal/standards/` = ok; `go test -run 'TestWorkerDaemon_|TestWorkerControlParse_' ./cmd/agent/` = 16/16 PASS; `internal/tools` control cases PASS (the suite's one failure, `TestTaskStatusRunningForOwnRow`, reproduces on a pristine `master` worktree — not from this branch).
- Live binary probe (real `bin/gopi`, private `GOPI_HOME`, `GOPI_WORKER_IDLE_SEC=1 GOPI_WORKER_IDLE_EXITS=0`): 1st `--daemon` exit 0; 2nd `--daemon` exit 1 naming the holder; `--status` alive+pid+state; FOREGROUND `gopi worker` ran >4s (killed by `timeout` → exit 124) while the daemon held the lock; `--restart` exit 0 with a NEW pid and the old gone; `--stop` exit 0, pid gone, `--status` "not running" + STALE-lock note; no leftover probe daemons.
- Merge-compat: a scratch merge of this branch onto `master` (`d1110961`) is clean (0 conflicts); the merged tree builds, the worker e2e + standards gates pass, and the merge-built binary re-ran the live probe green.
- New e2e (`cmd/agent/worker_daemon_singleton_e2e_test.go`, real binary, private
  `GOPI_HOME`, every daemon stopped by EXPLICIT pid):
  - **WS1** a second `--daemon` is refused, non-zero, naming the holder pid, with no
    orphan left behind;
  - **WS2** `--status` names the live pid + log path and leaves the lock file
    byte-and-mtime **identical** (read-only proven on the filesystem, not on the text);
  - **WS3** `--status` with no daemon says so, exit 0;
  - **WS4** a FOREGROUND `gopi worker` starts and stays alive **while the daemon holds
    the lock** (the half that would catch a guard placed on the wrong verb);
  - **WS5** `--stop` ends the daemon, exit 0, and a follow-up `--status` agrees;
  - **WS6** `--restart` produces a **different** pid, kills the old one, and leaves
    exactly ONE resident daemon.
  - plus a unit case pinning the control grammar (each action, `--force` alone =
    stop+interrupt, two actions reported as a conflict, `--status` recognized as the
    read-only action).
- Pre-existing failure, NOT from this branch: `TestTaskStatusRunningForOwnRow`
  (internal/tools) fails identically on a pristine `master` worktree (verified) — the
  double-run guard test disagrees with the current code on master.

## Not done / limits

- The worker's lifecycle passes (`workerLifecyclePasses`: stale-report, zombie
  auto-dispose, finish-cleanup sweep) still ride the worker loop only. The `--daemon`
  singleton makes a resident worker the normal shape, so that sweep's availability now
  depends on that one daemon being up; decoupling it (scheduling it from the merger
  too) is a separate task and is NOT attempted here.
- No systemd unit / launcher change: `scripts/gopi-worker-up.sh` still exists. The
  daemon face now refuses a second resident worker, so a launcher that starts one
  unconditionally will report the refusal — that is the intended signal, but the
  script was not rewritten in this task.
