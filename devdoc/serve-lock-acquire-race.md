<!-- doc-class: normative -->
# The serve-lock acquire refusal — mechanism, measurement, and the worker hole

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

**Re-derived onto master from** `archive/fix/serve-lock-race` (tip `dd9c53b4`,
archived 2026-09-17; the full original analysis lives at
`git show archive/fix/serve-lock-race:devdoc/serve-lock-acquire-race.md`).

---

## The mechanism, in one paragraph

A refusal reading *"another merge daemon is already running (lock … held by
pid N)"* while the kernel's own table `/proc/locks` carries **no flock record
for the lock path's inode** is the signature of a flock held on an inode the
path no longer names: **`flock` is not invalidated by unlink/replace**. The
concurrent hypothesis — a non-`EWOULDBLOCK` errno mis-read as "held" — was real
too, and it is why the refusal *labelled* any failure "held by a live daemon";
that half landed on master as `c50e7f2a`
(`serveLockErrnoIsContention` / `serveLockHeldError`, with
`cmd/agent/serve_lock_errno_test.go`).

## The measurement

`cmd/agent/serve_lock_replacement_test.go` measures the mechanism directly:

- `TestServeLockReplacementMechanism` — a holder's flock **survives** its path
  being unlinked and recreated, on the OLD inode, while the FRESH inode's lock
  is free. That is why a holder cannot notice the replacement by looking at its
  own descriptor.
- `TestServeLockFlockOutlivesItsInheritedCopy` — the flock lives on the open
  file description, so an inherited copy keeps it held after the parent closes.

## Two faces, two levels of paranoia — and the worker's hole

The **merger** re-proves identity once per tick (`ensureServeLockIdentity`,
`cmd/agent/cli_tasks.go`; present since the 2026-09-12 double-merge incident).
The **worker** — the other resident face, holding `worker.lock` through the same
machinery — checked identity only at acquisition (`acquireWorkerLock`), and its
daemon lock is *inherited from the spawning parent*. So one `rm worker.lock`
left a resident worker locking an inode no name points at while the path carried
a fresh inode whose lock was free: the next `gopi worker --daemon` acquires it
and **two resident workers sweep the same queue**.

`workerDaemonGuardIdentity()` (`cmd/agent/cli_worker_daemon.go`) closes that
hole: once per round, **before any work**, `runTaskWorkerLoop` re-proves that the
held descriptor is the file `worker.lock` NAMES, and then either repairs
(report the stray holder, drop the detached lock, re-acquire the current path)
or **STOPS** when another process already owns the fresh file. An unprovable
state is announced and the round proceeds under the held lock (Rule 15) — the
same conservative half `ensureServeLockIdentity` takes.

The regression guard is `cmd/agent/worker_lock_guard_test.go` (the tag's own
test, moved verbatim except its wiring constant: the tag asserted the ordering
against `workerRound()`, which master renamed `workerRoundClaiming()`).
