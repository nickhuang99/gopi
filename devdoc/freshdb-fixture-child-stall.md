<!-- doc-class: historical -->
# The fresh-DB fixture child: the blocking child named, and the two arms that bound it

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

**Date:** 2026-09-26 · **Worktree:** `~/workspace/gopi-01a0dc30fa1f7e35941908309147dd32`
· **Branch:** `fix/memory-sync-e2e-freshdb-stall` · **Task:** `01a0dc30-…`
Full report: `tasks/01a0dc30-fa1f-7e35-9419-08309147dd32-report.md`.

This is what a future agent needs without re-deriving it: **what the blocking
child is, how to re-observe it in two minutes, and why both fixes are shaped the
way they are.**

## 1. The blocking child (named, /proc evidence)

The stalled process is the test's own fixture child, **not** the sync loop:

```
<scratch>/suite-cmd-agent-run-<r>/TestMemorySyncEveryLoopE2EConvergesAcrossRounds<r>/001/gopi \
  memory_store --topic t-every --question "every-loop q alpha?" … \
  --db <scratch>/…/002/a/memory.db          ppid = the test binary (cmd/agent.test)
```

Its parent frame — the `os/exec.Waitid` of the epic's dump — is
`cmd.CombinedOutput()` at `cmd/agent/cli_memory_sync_every_test.go:539`, in the
`store()` closure opened at `:537`. The sync loop
(`gopi memory sync --peer hp --push --every 1s`) starts later, at `:547`.

Live capture (`/home/user/workspace/tmp/gopi-hang/capture-livechild.txt`,
13:46:27, load 12.8):

```
  child pid=3233442 ppid=3232285 etime=5s
  db:  …/002/a/memory.db  (device=2052, journal=2052)
  process io: write_bytes: 368640 cancelled_write_bytes: 0
  parent (suite) state: S wchan=do_wait      ← the suite, parked on this ONE child
  parent children: 3233442
  tid=3233450 state=D wchan=jbd2_log_wait_commit   ← the child's fsync, in the journal
  fd 3 -> …/a/memory.db   fd 6 -> …-wal   fd 7 -> …-shm
```

Reproduce (generates no load — the probe only stops the child and reads /proc,
Rule 27): `sh /home/user/workspace/tmp/hang/run-redx.sh pre` — frozen child
logged with its ppid, state, cmdline and fds; with master's tree the suite then
emitted **no test output at all** for 75 s (`timeout 75` → rc 124, the log holds
only `=== RUN TestMemorySync…`).

## 2. Two distinct waits — do not conflate them

- **The live hang**: a D-state thread in `jbd2_log_wait_commit` — an ext4 journal
  commit (an `fsync` that had not returned) against a device the merger and the
  workers were writing. The child is I/O-bound on the filesystem its root is on.
- **The structural defect**: an unbounded `CombinedOutput()` on a child. Whatever
  the child waits for, the parent's `Waitid` has no deadline and the suite's only
  exit is its own timeout kill. The stall *looked* like the sync loop because the
  dump pointed at the test's last frame, not at the child.

## 3. The two arms (independent; both kept)

**Arm A — placement** (`internal/testutil/scratch_placement.go`). The suite root
is chosen memory-first by ONE resolver (Rule 2) and the decision is reported once
per run (`memory-root` / `memory-home` / `memory-base` / `device-root`). A caller
pin wins (`GOPI_TEST_TMPDIR`, then a memory-backed `TMPDIR`) — Rule 22's explicit
layer beats the ambient default; otherwise the host's memory base is used when it
is memory-backed; otherwise the run is DEVICE-BACKED and SAYS so, naming the
channel that changes it. Never a silent `/tmp` fallback.

**Arm B — budget** (`internal/testutil/runfixture.go`). `FixtureCommand` /
`FixtureCommandEnv` run a fixture child under a 90 s context budget and a 5 s
`WaitDelay`, capture combined output, and report a killed child as an ERROR
naming the budget and carrying the output so far. `cli_memory_sync_every_test.go`
is converted (the store closure at `:537` — the site the epic's dump names — and
the peer probe).

A alone leaves a device-backed host unbounded again; B alone leaves the cost
paid. Both.

## 4. Measurements (this host, contemporaneous, live merger + 3 workers)

| workload | wall clock | fsyncs |
|---|---|---|
| fresh DB, scratch device 2052 (ext4) | 1014 / 2101 / 1084 / 1248 / 1751 ms | 13 |
| warm DB, same device (isolates first-open) | 301–453 ms | 5 |
| fresh DB, `/dev/shm` (tmpfs) | 16 / 18 / 16 / 16 / 24 ms | — |
| bare fsync probe | 550 ms per 5 fsyncs (110 ms each); `/dev/shm`: 5 ms (1 ms each) | — |

Device 2052 is `/home`; `/` is 2051; `/dev/shm` is 28 — no root violates Rule 22,
the memory root is simply cheaper. Raw logs:
`/home/user/workspace/tmp/gopi-hang/measure-under-load.txt`, `strace-cold2.txt`,
`strace-warm.txt`.

## 5. Re-observed on the delivery tip (2026-09-26 14:09–14:14, live merger + 2 workers)

The whole chain was re-measured AFTER the delivery-point merge (`6cf06c8b`), so
the numbers below are reproducible from the delivered branch without re-deriving
anything. Raw logs: `/home/user/workspace/tmp/acc-final/`.

**The blocking child, live, on the DEVICE arm** (caller pinned
`GOPI_TEST_TMPDIR=/home/user/workspace/tmp`, device 2052 ⇒ the placement arm
degrades by design and REPORTS itself). 11 hits, one probe every 50 ms
(`device-arm-capture.txt`, load 4.92, io some avg60 47.93):

```
child pid=3341521 ppid=3341315 (parent exe=agent.test)
cmdline: …/001/gopi memory_store --topic t-every --question "every-loop q alpha?" … --db …/002/a/memory.db
tid=3341527 state=D wchan=jbd2_log_wait_commit      ← the child's fsync, in the ext4 journal
fd 3 -> memory.db    fd 6 -> memory.db-wal    fd 7 -> memory.db-shm
parent: state=S wchan=do_wait children=3341521      ← the suite, parked on this ONE child
db dev=2052 fs=ext2/ext3    (device 2052 = /home; / = 2051; /dev/shm = 28)
```

D-thread wchan distribution over the capture: **11/11 `jbd2_log_wait_commit`** —
never a pipe, never a lock of ours. The child does 135–168 write syscalls and
369–475 KiB in that window; under the *memory* placement the same child does 2.
The `do_wait` line is the epic's frame, seen from the parent's side.

**The red arm, re-run on MASTER's tree in the same conditions**
(`gopi-01a0dc30-masterbase@e22996c6`, one `SIGSTOP` on that child,
`master-redarm2.log`): `panic: test timed out after 1m35s`, and the dump parks at

```
internal/syscall/unix.Waitid → os.(*Process).pidfdWait → os/exec.(*Cmd).Wait
…cli_memory_sync_every_test.go:539 …:543
```

— the same two frames the epic's `stuck100.log` carries. **The fix arm, same
conditions** (`freeze-fix-arm.log`, tip `6cf06c8b`): the bounded runner returns in
**91.22 s**, FAILING with the child named and its budget quoted instead of parking
forever:

```
cli_memory_sync_every_test.go:548: store "every-loop q alpha?" on …/002/a/memory.db:
fixture command timed out: …/001/gopi memory_store … exceeded its 1m30s budget and killed
```

**Acceptance, re-run on the merged tip** (`device-arm.log`,
`job-17` output): `ok github.com/nick/gopi/cmd/agent 6.055s / 7.333s`, PASS in
5.71 s / 6.57 s, with the merge, two workers and the web daemon live. A third run
after the SECOND delivery-point merge (`a51c9a63`) is the same shape: `--- PASS …
(6.72s)`, `ok … 6.878s`, rc=0, load 6.97, peers `2502831 gopi worker` +
`2542845 gopi merge --every 5m` + `3094769 gopi worker`. The run
that pinned `TMPDIR` at the DEVICE (job-17, 14:07:58) first resolved the
placement to `/dev/shm` (`placement memory-base`) — the memory-first resolver
ignores a caller's TMPDIR over a device, by design; the run that wanted the device
arm pinning used `GOPI_TEST_TMPDIR` (the suite's own override), which is how the
device arm above was captured.

## 6. Traps for the next reader

- `exec.Cmd.WaitDelay` does **not** shorten the error path: only a **non-zero
  ExitCode** arms the "wait for the pipes" branch. A context-killed child reports
  code −1, so the pipe wait still runs — the call returns at `budget + WaitDelay`.
  Budget for that when asserting a bound.
- `cmd.Env = nil` inherits the parent's environment; a fixture needing a peer map
  must pass it (`FixtureCommandEnv`).
- The red proof's budgets are constants in `runfixture_test.go` on purpose: the
  kill arm is provable in 200 ms; proving it by paying the real 90 s would be a
  slow test proving the same thing.
