<!-- doc-class: normative -->
# Fixture children die with their test binary — the binding, the refusal, the inventory

## The defect, measured

A test fixture child that outlives its owning test binary reparents to init and
stands wherever it was started. On peer1, 2026-09-26, that was inside a task
worktree:

```
python3 /home/user/workspace/gopi-test-scratch/01a0d687/blackhole.py   pid 1503983  ppid 1  11h+
signalrec.test                                                        pid 1525157
  both with cwd inside gopi-01a0d68746ed7ea090e56bff794e1b9f
```

`finish-cleanup`'s holder guard then refuses to release the directory — correctly:
a live process is standing in it — and the branch cannot be deleted either, so
`worktree in use by live process pid N (…): cwd …` repeats until an operator
intervenes (245 occurrences recorded in `devdoc/why-worktree-residue.md`).

## Root cause: the mechanism existed, the fixtures did not use it

`internal/daemonlifetime` already binds a process's death to its launcher: the
suite holds the write end of a pipe, the child inherits the read end (parked at
`InheritFDFloor = 200`, above `os/exec`'s `ExtraFiles` renumbering range), and the
kernel closes the pipe when the holder dies — so the child kills itself, SIGKILL
included. That mechanism was wired for `gopi … --daemon` processes only. A fixture
child never called the watchdog entry point, so it kept the default lifetime.

Three facts made the gap hard to see, and each is now encoded in the fix:

1. **`comm` cannot name the culprit.** It is a 15-character kernel field; the
   measured orphan read as `python3`, and two re-exec'd test binaries pinning the
   same worktree read as `signalrec.test`. The operator goes looking for a `gopi`
   process and does not find it.
2. **A zombie reads as "the child is there."** A child that died instantly was
   still in `/proc` (state `Z`) and still had a cmdline of `""`, so a liveness
   check on `Signal(0)` alone says "alive".
3. **A fixture's own diagnostics can kill it.** A victim whose stdout/stderr went
   to an undrained pipe died of SIGPIPE (`exit_code=13` = signal 13) *while
   writing the line that would have explained the failure*.

## What was built

**`internal/testutil/fixturelifetime.go` — the ONE definition** (Rule 14):

| half | call | who runs it |
|---|---|---|
| arm | `ArmAndSweepForSuite` → `daemonlifetime.ArmThisProcess` | the suite's `TestMain` |
| adopt + report | `WatchFixtureBinding` | the re-exec'd child, before it announces readiness |
| guard (suite) | `RequireFixtureBinding` | a case that depends on the binding |
| guard (child) | `RequireFixtureChildBinding` | the parent, after the readiness handshake |
| bind a spawn | `BindFixtureChild` / `StartFixtureChild` | the parent, before `Start` |

Two design points that cost real debugging and are worth inheriting:

- **Group scoping is not optional.** The death pipe covers the holder being
  *killed* (no Go code runs); the process group covers every in-process exit path
  (return, `t.Fatal`, panic — `testing` runs `t.Cleanup` on all three). A fixture
  needs both.
- **Which value wins is not "append".** `os.Getenv` returns the FIRST match in the
  environment block, so appending leaves the caller's value winning. A victim
  started with `append(os.Environ(), victimGuard+"=1")` inherited the *holder's*
  guard first and re-entered the holder body — the fixture recursed (measured: 521
  orphaned test-binary processes, which exhausted the host's process limit and
  made an unrelated `go build` fail with `resource temporarily unavailable`).
  `setFixtureEnv` is now the one place "which value wins" is answered.

**The measured leak site is wired**: `internal/signalrec`'s `TestMain` arms, every
victim child adopts + reports, and each of the six spawns binds before `Start`. The
suite went from leaking `signalrec.test` orphans to zero after a full run.

**The refusal names the holder** (`internal/tools/task_finish_cleanup.go`):
`finishCleanupHolderWho` renders `pid N (comm) argv="…" ppid=M`, and says
`(ORPHAN — reparented to init, nothing will reap it)` when the parent is init. It
degrades honestly: an unreadable argv yields comm with no PPID claim and never a
blank name (Rule 15b).

**The inventory** (`internal/testutil/fixture_spawn_inventory_test.go`) scans every
test-side spawn of a test binary and fails a site with the leak's shape (an
unbounded child with no binding, no wait, no kill, no group reaper). Measured: 48
sites, none leak-prone, and the classifier is itself red-proved on a synthetic
leak so it cannot pass by matching nothing.

## Verification recipes worth reusing

- **Red proof by mutating the mechanism, not the test.** `armed =
  daemonlifetime.WatchFromEnv()` → `armed = true` in `WatchFixtureBinding` makes the
  child *claim* a binding it never installed: the SIGKILL case then fails with
  "fixture victim SURVIVED its holder's SIGKILL", which is the leaked shape. Revert
  after; measured green 0.05 s, red 30.04 s.
- **Judge a death with the identity, not with `Signal(0)`.** `ReadProcIdentity` +
  `Alive()` (state letter, start ticks) refuses a zombie and a recycled pid.
- **Give a fixture that is *meant* to be killed a FILE for its output.** A pipe
  nobody drains turns the child's own diagnostics into SIGPIPE.
- **Do not `pgrep -f <pattern>` to check your own fixtures** — it self-matches the
  shell running the check (Rule 24's sibling trap); `pgrep -x <name>` or `/proc`.

## Not done (honest)

- The inventory gates the daemonize-to-PPID-1 *shape*; it does not prove a given
  exempt site is safe at runtime. `finishCleanupHolderWho`'s argv rendering is
  covered end to end through `finishCleanupWorktreeInUse`, but the message is not
  rendered through a live `gopi task finish` refusal in this run.
- Suites that start a blocking fixture child and do NOT call `BindFixtureChild`
  are not forced to by the inventory (they are only forced to be *bounded*). The
  seam is available through `RunSuiteMain`/`ArmAndSweepForSuite`; the arm is inert
  until a child adopts it.
