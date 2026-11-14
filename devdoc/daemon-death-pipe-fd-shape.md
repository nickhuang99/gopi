<!-- doc-class: historical -->
# daemonDeathPipe's unreachable refusal — validate the fd's SHAPE, not wrappability

Branch: `fix/daemon-death-pipe-fd-shape` (worktree `~/workspace/gopi-fd-shape`, based on
master `52090dc0`). Task row: `01a0b61a`.

## The defect (Rule 15 — a validation the code claims but does not perform)

`cmd/agent/cli_daemon_spawn.go:daemonDeathPipe()` wrapped the number from
`daemonlifetime.HoldEnvName` and refused only when the wrapper came back nil:

```go
death := os.NewFile(uintptr(fd), daemonlifetime.HoldEnvName)
if death == nil {
    return nil, fmt.Errorf("daemon death-pipe fd %d (%s) is not usable", fd, ...)
}
```

**`os.NewFile` validates nothing.** It returns nil only for a negative number and
otherwise hands back a wrapper whose `Fd()` is whatever it was given — measured on
this host and recorded in memory #4045 (`os.NewFile(9999)` is non-nil with
`Fd()==9999`; `Stat()` is what fails). So the documented "is not usable" refusal was
unreachable, and a free number (100000) or a live non-pipe number travelled on to
`child.ExtraFiles`, where **os/exec's own "invalid argument"** was the only report —
a precise refusal replaced by a generic failure one layer down.

The sibling branch `fix/borrowed-fd-sibling-sites` (88351183, unmerged) found the
same thing and deliberately logged instead of asserting it
(`TestDaemonDeathPipeRefusesAnUnusableNumberLeavesTheNumberAlive`).

## The fix — one predicate, two readers

### A. `internal/daemonlifetime.ValidateHoldFD(n, floor int) (string, error)` (new, exported)

The SINGLE SOURCE of the death-pipe shape test (Rule 14). It returns the kernel's
target for the number and an error naming one of three shapes:

| shape | verdict |
|---|---|
| `n < floor` | refused — os/exec renumbers ExtraFiles from fd 3, so a lower inherited descriptor is silently REPLACED |
| not open in this process | refused — `fdTarget` finds no target |
| open, but not a pipe (`pipeTargetPrefix` = `pipe:[`) | refused, **quoting the kernel's target** |
| a pipe at/above the floor | accepted, `(target, nil)` |

**The floor is the CALLER's argument, and that is not a second spelling.** The two
readers stand on opposite sides of one renumbering, so the lowest number that can be
the pipe differs between them and only the caller knows which side it is on:

* the **spawn side** (`daemonDeathPipe`) passes `InheritFDFloor` — the number the
  HOLDER named in ITS numbering, where the suite's read end sits at fd 200 so a
  launcher's ExtraFiles cannot replace it;
* the **daemon side** (`ArmedFD`) passes `firstExtraFD` — the number os/exec
  ASSIGNED the entry, which is exactly why it was handed over. Refusing it for being
  "below 200" disarms every bound daemon.

The pipe shape — the genuinely common part — has exactly one implementation, in the
predicate.

`ArmedFD` was rewired to call it, so the local floor test and the
`fdTarget`/`pipeTargetPrefix` test are gone from it and the two readers cannot drift
into disagreeing about what a usable binding looks like.

### B. `cmd/agent/cli_daemon_spawn.go:daemonDeathPipe()`

Validates the number **before** wrapping it (so a refused number never becomes a
wrapper this process owns), and the refusal names the variable, the number and the
kernel's reading:

```
GOPI_DAEMON_DEATHSIG_HOLD_FD=100000 is not a usable death pipe — refusing to start a daemon
that would outlive its spawner: fd 100000 is not open in this process (the kernel names no
target for it)
```

The `death == nil` branch is kept as the last line of defence, but its doc comment now
states the truth: `os.NewFile` cannot produce it for a non-negative number.

## Branch dependency (stated explicitly — constraint 5)

`internal/borrowedfd` and `cmd/agent/daemon_death_pipe_wrapper_test.go` exist **only**
on `fix/borrowed-fd-sibling-sites` (commits `bebdce9e`/`c2f1109c`), which is **NOT** an
ancestor of master (merge-base `52090dc0`, master has advanced) — so `git merge
--ff-only` of it is impossible and the call `borrowedfd.Detach(death)` cannot exist on
this branch.

**Decision: this branch does NOT touch the detach obligation.** It neither ports the
sibling's wrapper guard nor invents a second helper (Rule 14). What it changes is
*when the wrapper is created*: a refused number is no longer wrapped at all, so the
refusal paths that the sibling epic detaches on **cease to build a wrapper**, and the
single accepted hand-off path is unchanged. At the merge of either branch the two
halves compose: `Detach` is still called on the one path that returns a wrapper.

**Reconciliation for the sibling branch.** When `fix/borrowed-fd-sibling-sites` rebases
onto this fix, its `TestDaemonDeathPipeRefusesAnUnusableNumberLeavesTheNumberAlive`
must convert its `t.Logf("OBSERVED (reported, not asserted)…")` branch into the
assertion its own comment files as a follow-up: `death == nil && err != nil`, with the
message quoting `not open`. Its `borrowedfd.Detach(death)` line stays exactly where it
is (after a successful wrap). The same change makes its
`TestDaemonDeathPipeSourcePinsTheBorrowedClassification` needles still match: the
production text keeps `daemonlifetime.HoldEnvName`, `os.NewFile(uintptr(fd)` — and the
`borrowedfd.Detach(death)` needle will match once that branch is merged.

## The regression this fix produced and then removed (measured, not narrated)

The first cut of `ValidateHoldFD` hardcoded `InheritFDFloor` and `ArmedFD` called it
with no floor of its own. The two daemon e2e cases went **red**:

```
--- FAIL: TestDaemonDiesWhenItsSpawnerIsSIGKILLed (60.48s)
    daemon pid 3578586 SURVIVED its spawner's SIGKILL — this is exactly the leak the epic is about
--- FAIL: TestBoundDaemonStaysAliveWhileItsSpawnerLives (63.34s)
    daemon 3624470 survived its spawner even in the control case
```

5-why: the daemon outlived the holder → its watchdog never armed → `ArmedFD` refused
the number → the number is the ExtraFiles-assigned fd 4, below 200 → one floor was
being applied on both sides of a renumbering. The repair is the floor argument above;
`TestArmedFDAcceptsThePipeAtItsOwnFloor` now asserts it in both directions on the same
number, so the distinction cannot be re-flattened silently. **This is why the acceptance
list includes the daemon e2e and not just the unit guards.**

## Regression tests

* `internal/daemonlifetime/validate_hold_fd_test.go` (external) — the predicate alone:
  every refusal shape, the ACCEPTED suite shape (a pipe claimed at ≥ `InheritFDFloor` the
  way `ArmThisProcess` claims it, *without* arming the process-wide watchdog).
* `internal/daemonlifetime/validate_hold_fd_internal_test.go` (internal) — the
  DAEMON side: `ArmedFD` accepts a pipe at `firstExtraFD`, refuses below it, and the
  drift guard asserting `ArmedFD`'s verdict equals `ValidateHoldFD(n, firstExtraFD)`'s
  for a table spanning every shape.
* `cmd/agent/daemon_death_pipe_shape_test.go` — the production function is the drive:
  a free number, a live non-pipe ABOVE the floor (so it fails on the shape, not the
  floor), a PIPE below the floor (so it fails on the floor, not the shape), the
  accepted suite shape, and the pre-existing non-number refusals. Each fixture claims
  its number with raw `F_DUPFD`/`dup` + `syscall.Close` and every wrapper the file
  builds is retained, because `os.NewFile` installs a GC finalizer that would
  otherwise close a number out from under the fixture (the sibling epic's recycling
  hazard, in the guard rather than in the code).
* The guard's PREMISE is re-asserted on every run
  (`osNewFileWrapsAnything`): the number the spawn path refuses is one `os.NewFile`
  wraps happily — else a future Go that validated would leave the guard green against
  a state that can no longer occur.

## Evidence

Red-before (production files stashed, tests kept — `git stash push -- cmd/agent/cli_daemon_spawn.go internal/daemonlifetime/daemonlifetime.go`):

```
--- FAIL: TestDaemonDeathPipeRefusesANumberNoProcessHolds
    daemonDeathPipe ACCEPTED GOPI_DAEMON_DEATHSIG_HOLD_FD=100000, a number no process here holds …
--- FAIL: TestDaemonDeathPipeRefusesANonPipeNumber
    daemonDeathPipe ACCEPTED GOPI_DAEMON_DEATHSIG_HOLD_FD=201, which the kernel reports as
    "…/death-pipe-shape-3429972251" …
--- FAIL: TestDaemonDeathPipeRefusesANumberBelowTheFloor
    daemonDeathPipe ACCEPTED GOPI_DAEMON_DEATHSIG_HOLD_FD=8, a pipe BELOW the ExtraFiles floor 200 …
FAIL	github.com/nick/gopi/cmd/agent	1.120s
```

Green-after: `go test ./cmd/agent/ -run TestDaemonDeathPipe -count=2` → `ok`; `go test
./internal/daemonlifetime/` → `ok`. Full commands and outputs are in
`tasks/daemon-death-pipe-fd-shape-report.md`.

## Not done (honest)

* The sibling's wrapper guard is not ported (constraint 4/5 — the file and its
  `internal/borrowedfd` dependency are branch-only, not ancestors of master). The
  hand-off is stated above; this branch makes the refusal paths stop building a wrapper
  entirely, and the sibling keeps `Detach` on the one path that returns one.
* The three side-effect sites #4035/#4038 name as still-leaking (`daemonNote` /
  `readyFD`, the merger take-over refusal, the worker daemon refusal) are **owned**
  descriptors — out of scope per constraint 8 (and the sibling branch covers the
  merger/worker ones).
* Not verified here: the sibling branch's own guards
  (`TestDaemonDeathPipeRefusesAnUnusableNumberLeavesTheNumberAlive`,
  `TestDaemonDeathPipeSourcePinsTheBorrowedClassification`) — they cannot run on this
  tree (file + package absent). The reconciliation they need is stated in the
  "Branch dependency" section, not applied.
