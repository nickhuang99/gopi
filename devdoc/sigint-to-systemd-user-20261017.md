<!-- doc-class: historical -->
# SIGINT that reached `systemd --user` — who sent it, why it killed the GNOME session, and the guard

Date: 2026-10-17 · task `01a1470f-804f-7d3f-aef1-9d50eaa3b35b`
Evidence: `~/.gopi/sessions/01a1470f-804f-7d3f-aef1-9d50eaa3b35b/artifacts/`

## What happened

On 2026-10-17 the operator's GNOME session stopped twice, and the session manager
was restarted several more times. journald names the killer exactly:

```
journalctl -b -o short-iso | grep 'Received SIG'
2026-10-17T01:14:12 systemd[3175]:    Received SIGINT from PID 3199746 (agent.test).
2026-10-17T01:16:09 systemd[3322902]: Received SIGINT from PID 3324839 (agent.test).
2026-10-17T01:22:15 systemd[3325473]: Received SIGINT from PID 3329111 (agent.test).
2026-10-17T01:52:31 systemd[3329727]: Received SIGINT from PID 3445033 (agent.test).
2026-10-17T07:28:35 systemd[3539644]: Received SIGINT from PID 4144045 (agent.test).   ← desktop died
```

Five lines, one sender: `agent.test` — the test binary of `./cmd/agent`, started by
`make test-budget PKGS='./cmd/agent'`. SIGINT to the *user manager* is not a
harmless signal: `systemd --user` treats it as "stop the whole user session", so
every scope under it (gnome-shell, the terminal, the VTE children) is torn down.

## Why the signal reached the user manager at all

Not a stray `kill -1`. It is a **recycled pid**, and the mechanism is worth
stating exactly because the fix is a policy about it:

1. A test spawns a fixture child and keeps its **pid number** in a place that
   outlives the child — a `t.Cleanup` closure, a struct field, a lock record.
2. The child exits. The number returns to the kernel's pool.
3. Later the teardown runs and signals *the number*.
4. Between (2) and (3) the kernel handed that number to `systemd --user`
   (the per-user manager is respawned frequently on this host — `systemd[3175]`,
   `systemd[3322902]`, `systemd[3325473]`, `systemd[3329727]`, `systemd[3539644]`
   are five different incarnations in one boot).
5. `systemd --user` receives a SIGINT nobody meant for it, and the user session goes.

The pid numbers confirm the churn: the run at 07:28 spawned `4140043` (its own
identity, self-reported in the suite's JSON stream) and the signal came from
`4144045` — two numbers apart. `ns_last_pid` was `127500` at diagnosis time, so
the space recycles constantly.

On Unix the lookup that *looks* like it prevents this does not:
`os.FindProcess` returns a handle unconditionally, and Go's own documentation
says it "is not a valid test for the existence of the process". The first act of
the pre-fix body —

```go
proc, err := os.FindProcess(pid)   // always succeeds
_ = proc.Signal(os.Interrupt)      // → whoever owns that number NOW
```

— is therefore a statement about a *number*, not about a process.

## The primitive that already existed

This repo had solved the problem next door (the run-closeout reap, the orphan
sweep): identity is the **incarnation** — pid **plus** `/proc/<pid>/stat` field 22
(starttime).

```go
id, err := testutil.ReadProcIdentity(pid)   // pid + StartTime + cmdline + state
testutil.KillIfIdentical(id, syscall.SIGTERM)  // refuses a changed incarnation
```

`KillIfIdentical` refuses when `SameIncarnation` (pid + starttime) fails, and
reports the refusal instead of signalling. That is the whole guard.

## The fix

Five call sites held a pid across time and signalled the bare number. Each now
reads the incarnation first:

| file | call site | shape |
|---|---|---|
| `cmd/agent/task_ground_e2e_test.go` | `groundKill` | pid held in `t.Cleanup`; now reads identity, delivers through a named seam `groundKillSignal`, returns early when the process is already gone |
| `cmd/agent/cli_daemon_cwd_test.go` | `daemonCwdTestStop` | same shape |
| `internal/shot/displayalloc.go` | `FilmDisplay.Close` | **production**: `FilmDisplay` now records `StartTicks` at spawn; `Close` goes through `displayKillIncarnation`, which refuses a changed incarnation and an empty one |
| `internal/testutil/tmproot_assert_test.go` | `killProbeProcess` | pid of a grandchild that cannot be waited on |
| `internal/tools/reclaim_live_run_veto_test.go` | killing the recorded runner | pid read back from the ledger |

Deliberately **not** changed, with the reason: the signals aimed at a process the
caller spawned microseconds earlier and holds a live `*exec.Cmd` for
(`internal/tools/bash.go`, `job.go`, `workerSignalPID`, `signalChild`,
`child_stop.go`). Their pid cannot have been recycled inside that window, and
Go's `Process.signal` already refuses after `Wait` (`pidStatus`). Widening the
diff there would have diluted the change, not improved it.

`kill(pid, 0)` sites were checked separately and left alone: signal 0 *sends
nothing*, and `internal/procstat` deliberately keeps a three-valued verdict
(`alive`/`gone`/`unknown`) so "cannot measure" is never reported as death.

## The regression guard, and why it has two arms

`cmd/agent/ground_kill_identity_test.go`:

- **`TestGroundKillSignalsOnlyTheRecordedIncarnation`** aims a record with a
  deliberately wrong starttime and the *same live pid*, then asserts (a) the
  refusal, (b) — measured through `/proc`, not inferred from the error — the live
  process is untouched; and then (c) that a bare `syscall.Kill(pid, SIGTERM)` on
  that same pid **really does deliver**. Without (c) the guard could refuse for
  the wrong reason and the test would still be green.
- **`TestGroundKillGoesThroughTheIdentityPath`** pins the other direction: a live
  pid must be delivered through the identity path and be measurably gone
  afterwards; an already-exited pid must NOT reach the delivery path at all
  (counted). Without the second half, a zero count is indistinguishable from
  "`groundKill` is dead code".

Both fixtures are processes the test spawned, reaped by `StartMockProcess`.

## What a future reader should take away

- **A pid is not a handle.** Anywhere a pid is stored and used later — a state
  file, a lock record, a struct field, a `t.Cleanup` closure — the check before
  signalling is `pid + starttime`, and `ReadProcIdentity`/`KillIfIdentical` is
  the one place that implements it.
- **A teardown that signals an already-exited pid is a bug even when nothing
  goes wrong.** The correct outcome (ESRCH) is not a reason to keep signalling:
  the signal is how the bug reaches a stranger.
- **Do not re-run `make test-budget PKGS='./cmd/agent'` unchecked on this host.**
  It was the sender five times; the branch-scoped runs in the task report are the
  safe form. A whole-package run belongs in a controlled, session-detached
  environment.
