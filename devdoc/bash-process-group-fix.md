<!-- doc-class: historical -->
# Bash Tool: Process-Group Isolation Against Daemon-Spawn Deadlock (bug011/012)

## Problem

gopi hung 2h34m executing a bash command that launched a background daemon via
`nohup ./bin/scheduler daemon ... &`. Mechanism (code-level, internal/tools/bash.go):

1. `exec.CommandContext(ctx, "bash", "-c", command)` had NO `Setpgid`. A daemon
   spawned by `nohup ... &` is a GRANDCHILD that inherits the stdout/stderr pipe
   write-ends (`nohup` only ignores SIGHUP; it does NOT redirect output).
2. bash exits quickly, but the daemon still holds the pipe write-ends → the read
   goroutines' `stdoutPipe.Read()` NEVER return EOF.
3. The idle monitor and absolute timeout called `cmd.Process.Kill()` — kills only
   bash, NOT the daemon grandchild (no process-group kill).
4. `<-readDone; <-readDone` blocks forever → gopi deadlocks. The 60s absolute
   timeout is useless because killing bash does not close the pipes held by the
   daemon.

The previous timeout fix (12f7306) covered FOREGROUND hanging commands
(`sleep 100`, FIFO): kill bash → pipes close → reads return EOF → clean timeout.
It had no test for "background process inherits pipes".

## Fix

`internal/tools/bash.go`:

- `cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}` before Start() — bash
  becomes its own process-group leader. Non-interactive shells don't job-control
  background jobs into separate groups, so the daemon grandchild stays in bash's
  group.
- Idle monitor kill site: `syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)`
  (was `cmd.Process.Kill()`). Negative pid = signal the whole group.
- NEW ctx-watcher goroutine for the absolute timeout: `exec.CommandContext`'s
  internal kill-on-ctx-done kills only bash, so we ALSO group-kill when
  `ctx.Done()` fires AND `ctx.Err() == context.DeadlineExceeded`. The
  `DeadlineExceeded` guard prevents a redundant kill on the normal path where
  `cancel()` closes ctx.Done() as bash() returns. The watcher exits via an
  `execDone` channel (defer close) — it never outlives bash().

`cmd.Process.Kill()` is no longer used for this command. The read-drain
structure (goroutines + readDone) is unchanged: once the group dies, all pipe
write-ends close, EOF arrives, `<-readDone` unblocks, `cmd.Wait()` returns, and
the existing timeout attribution (absolute vs idle) produces the error.

## Goroutine Lifecycle (why nothing leaks)

| Goroutine | Started | Exits |
|-----------|---------|-------|
| stdout reader | after Start | pipe EOF (group kill or normal exit) → send readDone |
| stderr reader | after Start | pipe EOF → send readDone |
| idle monitor | if idleTimeout > 0 | idleStop closed (defer) or fires → returns |
| ctx-watcher   | after Start | ctx.Done (group-kill, then return) OR execDone closed (defer) |

Defer order at return: close(execDone) → close(idleStop) → cancel() (LIFO), so
the watcher and idle monitor are always released before the context timer is
cancelled.

## Tests (internal/tools/bash_daemon_test.go)

- `TestBashDaemonSpawn_ReturnsAfterTimeout` — `nohup sleep 300 &`, subtests for
  idle-first and absolute-first kill paths; bash() must return within 5s with an
  error.
- `TestBashDaemonSpawn_DoesNotLeakProcesses` — daemon pid file + `kill(pid,0)`
  ESRCH poll + /proc argv scan: the background grandchild must not survive.
- `TestBashForeground_StillWorks` — `echo hello` sanity guard.

Negative proof: reverting the fix makes the daemon tests HANG in the stdout read
goroutine (killed by `go test -timeout`) — i.e. they reproduce the exact bug011
deadlock.

## Manual Verification (live)

- Real scheduler daemon via fixed bash(): returned in 2.0s ("no output for 2s"),
  no daemon leftover (was 2h34m hang).
- Real gopi dispatch path (`executeTool` → `tools.Bash`) with `nohup sleep 30 &`:
  returned in 2.0s, no leaked process.
- Shell-level: `timeout 10 bash -c 'nohup ./bin/scheduler daemon ... & sleep 30'`
  → daemon alive at t=4s, dead after the rc=124 group kill.

## Limitations / Future Work

- A daemon that calls `setsid()` escapes the process group; the group kill cannot
  reach it. Not observed for the scheduler daemon (it stayed in the group and
  inherited the pipes). `cmd.WaitDelay` (Go 1.20+) would force-close the pipes as
  a second line of defense if ever needed.
- The 1s idle-monitor ticker granularity is pre-existing.
