<!-- doc-class: historical -->
# The in-process daemon start: a test binary is not the CLI

**Epic:** daemon-inprocess-spawn (task `01a10742`), measured 2026-10-04 on peerbox.
**Files:** `cmd/agent/cli_daemon_spawn.go` (`daemonChildExecutable`),
`cmd/agent/daemon_inprocess_spawn_test.go`.

## The symptom, and the two hypotheses the briefing carried

A direct in-process `workerDaemonSpawnErr(nil)` from inside `go test ./cmd/agent`
never returned while the cgroup escape was on rung ① (`systemd-run --user
--scope`): it sat in `daemonAwaitReady` until the `daemonHandshakeWait` bound and
then answered `daemon did not report readiness within 30s`. The same start driven
through the real binary worked. The briefing proposed two candidates: the child
never writes the byte, or the parent's inherited `ExtraFiles` descriptor does not
survive the `systemd-run` wrapper.

## The measurement

A temporary harness (`TestZZReproInProcessDaemonSpawn`, since deleted) started the
daemon and watched `/proc` for the child while the call was pending. Preserved at
`~/.gopi/sessions/01a1076a-8893-7c26-9a9b-2a179abdb6b7/inproc-spawn-repro.log`:

```
CHILD pid=4091424 ppid=4091410
  cmdline="/home/user/workspace/gopi-go-tmp/go-build3421581563/b001/agent.test worker "
  cgroup="0::/user.slice/.../app.slice/gopi-daemon-worker-001-5846b322156c.scope"
  env GOPI_DAEMON_READY_FD=4
  fd 3 -> /dev/shm/.../001/.gopi/worker.lock
  fd 4 -> pipe:[55270799]        <-- ExtraFiles SURVIVE the systemd-run wrapper
  fd 200 -> pipe:[55272809]
parent RETURNED after 30.02612797s: reused=false err=daemon did not report readiness within 30s
```

**The second hypothesis is falsified**: fd 4 is present, and it is the readiness
pipe. **The first is true but not the cause.** The child's cmdline is
`<builddir>/b001/agent.test worker` — the process the seam re-exec'd is
`os.Executable()`, and inside `go test` that is the **compiled test binary**. Its
`main()` is Go's generated test main: it runs `TestMain` and then the whole
`cmd/agent` suite, which is what its daemon log contained — the suite's own
output, `testutil: suite "cmd-agent": …`, chat-case transcripts, all 34 KB of it.

So the child never reaches `workerDaemonNoteReady` because **it is not running the
daemon face at all**. Nobody was losing the byte; there was never a writer. The
parent cannot tell an empty pipe that means "the child is wedged" from one that
means "the child is running a test suite", which is exactly why the only bounded
thing it could do was burn the budget and report the transport symptom.

## The fix

`daemonChildExecutable` resolves the re-exec target before the fork:

1. the launcher convention `runcmd.EnvBinary` first (the same variable a launcher
   already exports for its children), then
2. `os.Executable()` — and a path ending in
   `worktreerelease.TestProcessExecutableSuffix` (`.test`) is **refused by name**,
   immediate, before the pipe is created, before the cwd escape and before the
   fork.

The refusal is placed **after** the control flock so that a start which could not
win the singleton lock still reports *that* — the cause an operator is actually
looking for — and before every other side effect, so a refused start leaves no
process and no pipe behind.

Two deliberate choices, each with a reason a later reader would otherwise
"simplify" away:

- **The predicate is the file-name suffix, not `testing.Testing()`.** A re-exec'd
  child is a real subprocess, where `Testing()` reports false; the suffix is
  evaluated on the resolved path — the very path that gets `exec`'d — so it
  catches the environment-provided binary too. It is the same one-spelling
  constant `internal/worktreerelease` already uses for this class, referenced and
  never re-typed.
- **It refuses rather than falling back.** Quietly substituting a real binary
  would turn a caller's explicit choice into a different one; refusing means the
  caller learns the one thing that makes the surface work (`GOPI_BINARY=<path to
  gopi>`), and the refusal message carries it.

## What the fix makes possible

The same in-process call now has two honest outcomes, and the regression test
measures both arms against the production entry point:

- **No binary published** → fails in ~6 ms, naming the cause and the remedy
  (pre-fix: 30.11 s and `daemon did not report readiness within 30s`).
- **`GOPI_BINARY` published** → the start **completes** (~0.8 s): the daemon is
  live on its published state file and its log carries
  `procname.WorkerLoopStartLine`. That arm is what shows the refusal is aimed at
  the wrong *target*, not at the in-process case.

## The generalisable lesson

**A seam that re-execs `os.Executable()` silently changes meaning under `go
test`.** In production that path is the program; in a test process it is a
harness whose `main()` runs a suite. The failure mode is especially costly because
it is *invisible from the parent's side* — the child is alive, the descriptors are
intact, and the only observable is an empty read, which every timeout-shaped
diagnosis will describe as a readiness problem rather than a wrong-binary
problem. When a spawn seam picks a binary, resolve it through the launcher
convention and refuse an executable that cannot be the program.
