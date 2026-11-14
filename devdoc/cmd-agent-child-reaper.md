<!-- doc-class: normative -->
# `cmd/agent` child & goroutine reaper — the three leaks, their fixes, and the guard

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

Task row `01a0d547-48c0-73db-97a3-156458ae9439`, branch
`task/01a0d54748c073db97a3156458ae9439`, worktree
`/home/user/workspace/gopi-01a0d54748c073db97a3156458ae9439`.

Follows `devdoc/cmd-agent-master-reds-20260925.md` §2, which measured the three
leaks and explicitly did NOT fix them ("would be a second task's worth of work").
This is that task.

## The shape that was wrong

The suite did not HANG — it FINISHED, and then stayed alive, because work it
started was still holding the process open. Three kinds, all non-test:

| # | leak | owner | why it outlived its test |
|---|---|---|---|
| ① | four `cmd/agent.(*g5Probe).runLive` readers parked in `io.PipeReader.Read` (`cli_chat_web_help_surface_basic_test.go`) | a `t.Cleanup` registered by `runLive` | the pump's WRITER was an `io.Pipe` end, closed only by a line of test code that never ran after the reader was parked — the reader had no peer to signal EOF |
| ② | one `cmd/agent.voiceServeHTTP` goroutine in net/http's epoll accept (`cli_voice_server.go`) | `TestVoiceServeHTTPHandlerContract` | `ListenAndServe` is a one-way door: the test drove the real server on a real port and had no handle to close it |
| ③ | one live `gopi worker` child, unreaped | one of the foreground-worker fixtures | the child is ours by contract (`cmd.Wait` is owed) but its parent was still on the stack when the run ended |

## The fixes

1. **①** — `g5Probe.runLive` pumps through an **OS pipe** (`os.Pipe`), not
   `io.Pipe`. The write end is held by the CHILD, so `Read` returns the moment
   the child (or a grandchild that inherited the descriptor) is gone — the reader
   is closed by the kernel instead of by a line of test code that can be
   forgotten. `Setpgid` + the existing `t.Cleanup` group-kill keep the reap; the
   parent closes its own copy of the write end so the pump terminates even while
   the child lives.
2. **②** — a **shutdown seam**, read at exactly one place (`voiceServeShutdownAddr`,
   the sole reader of `voiceServeShutdownCh`). `voiceServeHTTP` keeps its shape:
   unknown option → the same `ListenAndServe` on `opts.listen` that ships. Armed
   (the contract test only), it serves the same server through
   `voiceServeUntilClosed`, which blocks on the channel and then calls
   `Shutdown` **and `Close`** — Close is not decoration: `Shutdown` only forces
   idle connections closed, so a client sitting in an HTTP keep-alive (the
   contract test uses a shared `voiceProbeClient`) would keep the accept loop's
   `s.listenerGroup.Wait()` inside `Shutdown` forever. Read from the stdlib, not
   assumed: `net/http.(*Server).Shutdown` → `closeIdleConns`
   (`server.go:3191-3213`, a `StateNew` connection under 5s is "not idle").
3. **③** — the sweep below reaps whatever this binary still owns, at the guard
   case and again at close-out.

## The guard (closes the blind spot the reporter left)

`suite_stall_watchdog_test.go` gained the reporter's counterpart:

- **`TestSuiteLeavesNoLeakedChildren`** — runs the same measurement TestMain
  arms, settles `suiteSweepSettle` (a test's `t.Cleanup` runs a few instructions
  after its verdict, so an instant sweep would name a child about to be reaped),
  prints `live child processes: N` **always** plus one line per leftover
  (`pid=… state=… cmd="…"`), stops each one (SIGTERM → SIGKILL on the child's own
  group, direct child waited while the group is polled), and **fails** when N
  exceeds `suiteSweepLeakLimit`.
- **`reportLeakedChildrenAtCloseOut`** (TestMain, after `m.Run()`) — re-reports
  and sweeps again, so a child leaked by a LATER case is stopped and named in the
  run's own log. It never rewrites the exit code: the verdict belongs to the test.

**Why the limit is 3 and not 0 — the honest boundary.** Three children of this
suite are DESIGNED to outlive their test's SIGKILL, because the property under
test is only observable in a process the test did not kill:

1. *fixture ②* — `borrowed_fd_worker_child_test.go` (a worker lock inherited
   across `SIGKILL`; a child that had exited would assert nothing);
2. `TestWorkerStartedOnAPtyWritesItsLifecycleToTheDeclaredFile`
   (`host_lifecycle_log_test.go` — the pty lifecycle record, whose child is
   SIGKILLed by its own cleanup);
3. the flock sibling of (1) in the same fixture family.

A limit of 0 would therefore be red on a healthy run, and a guard that is always
red is turned off. The sweep STOPS them (so the binary is free), and the count is
what is asserted: **a fourth leftover is a defect with a file:line.**

## Red proof (the guard must be able to fail)

The guard was red-proofed by reverting ONE fix at a time in a scratch copy of the
worktree and re-running the package; the measured reds are in the task report
(`tasks/01a0d54748c073db97a3156458ae9439-report.md`), with the command, the
count, and the pid/cmd line the guard printed. The proof is the point of the
whole file: a guard that no revert can redden is prose.

## The residual (NOT this task's)

`./cmd/agent` still costs ~13 minutes of wall clock on this host, and the
per-fixture FIRST ledger open still pays this box's block-backed journal commit
(`testutil: … the ledger sandbox … is on block-backed filesystem type 0xef53
(device-backed)` printed on every suite entry). Both are master-red and
host-cost, not leaks: they are unaffected by the three fixes above, and the
measured magnitudes live in the task report.
