<!-- doc-class: historical -->
# The session-leading holder fixture: setsid(1) forks, and the pid you wrote down is not the leader

**Scope**: `internal/tools/task_finish_cleanup_holder_shared_session_test.go`
(`TestSharedSessionHolderProbe/leader`) and its regression test
`TestSessionLeadingHolderSpawnerReportsTheLeader`. No product code changed.

## Symptom

`TestSharedSessionHolderProbe/leader` was flaky **under load**, failing with

```
fixture defect: holder N must LEAD its session, got sid=M
```

on a file that was byte-identical to master — a false red in a fixture, which is
the worst kind: it accuses the code under test of nothing, and it only appears on
a busy host, so it teaches a reader nothing when it does appear.

## Root cause

The leader arm built its session-leading holder by spawning it **through the
external `setsid(1)` binary** and writing `holder.Process.Pid` to `pidFile`:

```go
holder := exec.Command("setsid", os.Args[0], sharedSessionHolderRunArg)
holder.Start()
os.WriteFile(pidFile, []byte(strconv.Itoa(holder.Process.Pid)), 0o600)
```

`setsid(1)` (util-linux) has two branches:

| condition | what setsid(1) does | the pid the fixture wrote down |
|---|---|---|
| `getpid() != getpgrp()` | `setsid(2)` **in place**, then exec | **is** the session leader (correct) |
| `getpid() == getpgrp()` | **forks**; the *child* calls `setsid(2)` and execs | is the short-lived **parent**, which leads no session (defect) |

The **fork branch is selected by the position of the `setsid(1)` process itself** —
specifically whatever process group the caller's spawner chain gave it. That is
why the arm was green on a quiet machine and red under load: the branch is decided
by host conditions the fixture neither controls nor checks.

Measured on util-linux 2.37.2, with the `setsid(1)` command given its own process
group (`SysProcAttr{Setpgid: true}`):

```
4121703 execve("/usr/bin/setsid", …)          ← the setsid process
4121703 clone(flags=SIGCHLD) = 4121704        ← IT FORKED
4121704 setsid()                              ← the CHILD takes the session
4121704 execve(…test binary…, "-test.run=TestHelperSharedSessionCwdHolder")
```

and the pid the fixture wrote to `pidFile` is **4121703**, which leads no session
at all. Its session is the caller's, so the probe read session 27203 — the
*operator's* session — and reported a fixture defect.

A first attempt at this investigation asserted the fork was produced by making the
**launcher** a group leader. That is wrong, and the strace above is what corrected
it: giving the launcher a group merely makes the launcher the ordinary parent that
forks `setsid(1)` as a child; the group must be on the `setsid(1)` process. The
comment in the file records the correction, so the next reader does not repeat it.

## Fix

**Take the session in the child's own fork.** `SysProcAttr{Setsid: true}` makes the
kernel call `setsid(2)` in the child *before* exec, so the process whose pid is
reported **is** the process that leads the session — `sid == pid` is the only thing
the kernel could have done, not a claim about timing or about which branch an
external binary chose. There is no second process, so there is no handshake to race
and no pid to lose.

```go
holder.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
```

Consequences, all deliberate:

- **The external `setsid(1)` dependency is gone from the fixture's own path**
  (Rule 35 — one fewer program the test needs on the host, and the zero-dependency
  direction the rule asks for: nothing was added to satisfy this).
- `Setsid` and `Setpgid` are never combined: `setsid(2)` makes the caller its own
  group leader, so asking for both is `EPERM`.
- The spawn lives in **one** function, `sharedSessionSpawnOwnedHolder`, used by both
  the launcher and the regression test, so the shape the test proves is the shape
  the launcher uses (Rule 14).
- The fixture now **asserts** the property it depends on (`sharedSessionWaitLeadsSession`,
  called before the probe judges anything), so it fails with a fixture-shaped message
  naming the true cause instead of handing the arm a pid that reddens an unrelated
  assertion.
- `reapSessionFixture` (also one implementation) replaces the inlined kill loop.

## The regression test (Rule 10.5)

`TestSessionLeadingHolderSpawnerReportsTheLeader` has two arms and runs in ~0.05s:

- `pre-fix-shape-through-the-setsid-binary` — **reproduces the old shape live**, in
  the test, with the `setsid(1)` process given its own group, and **requires** the
  reported pid to be observably NOT a session leader. If a future util-linux stops
  forking, this arm fails and says so: the regression it guards has changed shape.
- `post-fix-shape-is-provably-the-leader` — drives the real spawner and requires
  `sid == pid`.

**Red-then-green, observed by temporary revert of the spawner to the pre-fix shape**
(the reverted file was restored; it is not in the commit):

```
--- FAIL: TestSessionLeadingHolderSpawnerReportsTheLeader/post-fix-shape-is-provably-the-leader
    fixture defect: holder 4131977 never became the leader of its own session within 20s
FAIL	github.com/nick/gopi/internal/tools	21.045s
```

Against the fix the same test passes, and the `pre-fix-shape` arm prints the
measurement:

```
pre-fix reproduction measured: the reported pid 4123842 leads session 27203, not
itself — this is the false red the fix removes
```

**A safety defect caught while writing it**: an earlier draft killed the fork child
by its *session id*. A session id is not a permission to signal — on this host the
reported pid sat in the operator's session, so that cleanup would have signalled an
unrelated process (the operator's own shell leader). The arm now kills only the pid
it started; the holder exits by itself after `procHolderHoldTime`.

## What was NOT done

- The flake cannot be *forced* here, and forcing it was not attempted: Rule 27 bans
  generating load to make a test number appear. The condition was made **pinned**
  instead (`Setpgid` on the launcher) and the old shape is reproduced **live** by
  the regression test, which is the deterministic evidence the epic asked for.
- Other `setsid(1)` uses in this package (`task_finish_cleanup_orphan_test.go`,
  `orphan_tree_test.go`, `run_closeout_reap_test.go`) were **not** touched — they are
  out of this task's scope. They are not obviously exposed (the orphan fixtures call
  `setsid` through a shell in shapes where it execs in place), but they share the same
  pattern and a reader who hits a similar flake there should start from this document.

## Verification recipe (reusable)

- Reproduce the branch decision: give a command its own process group
  (`SysProcAttr{Setpgid: true}`) and strace it —
  `strace -f -e trace=clone,setsid,execve CMD`; a `clone` immediately followed by a
  `setsid()` **in the new pid** is the fork branch.
- Assert session leadership against the kernel, never against fixture bookkeeping:
  read `/proc/<pid>/stat` field 6 (the 4th field after the last `)`, i.e. `sid`) or
  `procname.ProcSessionKey`.
