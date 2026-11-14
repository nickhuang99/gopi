<!-- doc-class: normative -->
# Devdoc: the borrowed-fd site closure — classification, one shared detach helper, kernel-level guards

Task `01a0b6fa`; branch `feat/fd-finalizer-borrowed-sites` (off master `52090dc0`).
Predecessors read as information (Rule 23): task `01a0b5cd`
(`devdoc/fd-finalizer-leak.md`, commit `4a828ffc`, the narrow merger take-over
fix — its file is being retired by `feat/merger-delete-dead-impl`) and task
`01a0b5f5` (branch `fix/borrowed-fd-sibling-sites` @ `c0badb0f`, **completed but
unmerged**, covering the worker lock / death pipe / `daemonlifetime.Watch`).

## The mechanism, once

`os.NewFile(fd, name)` wraps an ALREADY-OPEN descriptor AND installs
`runtime.SetFinalizer(f.file, (*file).close)` (`GOROOT/src/os/file_unix.go`).
The wrapper therefore claims OWNERSHIP of a descriptor this process never
opened. When the wrapper becomes unreachable — the function returns, the result
is dropped into `_`, or only a local ever held it — the runtime's finalizer
goroutine (`runtime.runfinq`) closes that number. Two damages:

1. **survival** — the close lands on the number's real owner, whose next
   `readdirent`/`close` returns `EBADF`;
2. **recycle** — the kernel hands the freed number to whatever opens next, so the
   owner's own handle silently denotes a *stranger's* file.

The measured production shape (task `01a0b5cd`): `TempDir RemoveAll cleanup:
readdirent 001: bad file descriptor`, fd 7, close stack from `runtime.runfinq`.

## The load-bearing step is the CLASSIFICATION, not the detach

`borrowedfd.Detach` on an **OWNED** wrapper suppresses the close of a descriptor
this process really owns and leaks it for the life of the process. So each site is
classified before it is touched, and the reason is recorded in the code:

- **BORROWED** = the fd NUMBER was not opened by this process: it arrived through
  `os/exec`'s `ExtraFiles` renumbering (dup2 into 3..3+n-1), or another process
  named it in the environment.
- **OWNED** = this process called `os.Open`/`os.OpenFile`/`os.Pipe` itself.
- **BORROWED-but-CLOSED is NOT a defect site.** `os.File.Close` sets the poll.FD's
  closed flag and clears the finalizer (`os.(*file).close` →
  `runtime.SetFinalizer(file, nil)`, `GOROOT/src/os/file_unix.go`), so an
  already-run finalizer is a no-op. A descriptor borrowed only to write a byte and
  then closed is correct as it stands.

The rule that follows: **only a DROPPED borrowed wrapper needs `Detach`.**

## Classification of every surviving `os.NewFile` site

`grep -rn "os.NewFile" --include=*.go cmd internal | grep -v _test.go` returned
seven wrap sites at briefing time; **six SURVIVE on master** (the seventh, the
merger take-over lock, has since been deleted — see row 7). The count below is
the census at the moment of classification, kept whole so the row numbers and the
guard names that cite them stay stable.

| # | site | descriptor | class | treatment | status |
|---|---|---|---|---|---|
| 1 | `cmd/agent/cli_worker_daemon.go` `workerDaemonInheritLock` | parent's lock file, dup2'd in by `os/exec` (`ExtraFiles[0]` = fd 3) | **BORROWED** | `Detach` at all 3 refusal exits (incl. the contended deadline, the path an ordinary start takes); success exit deliberately NOT detached | covered here; also sibling ①a |
| 2 | `cmd/agent/cli_daemon_spawn.go` `daemonDeathPipe` | the holder's death-pipe read end, named by `GOPI_DAEMON_DEATHSIG_HOLD_FD` | **BORROWED** | `Detach` on the returned wiring slot (exec does not retain `ExtraFiles`; `Start` does not close them — only `Process.Release` does) | covered here; also sibling ①b |
| 3 | `internal/daemonlifetime` `Watch` | the holder's death-pipe read end (`EnvName`) | **BORROWED** | `borrowedfd.Detach(f)` at the construction site | covered here; also sibling ③ |
| 4 | `cmd/agent/cli_worker_daemon.go` `workerDaemonNoteReady` | readiness pipe, fd stated in `GOPI_DAEMON_READY_FD` | BORROWED, **CLOSED** (`defer f.Close()`) | none needed — the close is explicit and clears the finalizer | classified, not a defect site |
| 5 | `cmd/agent/cli_daemon_spawn.go` `daemonNote` | same readiness pipe | BORROWED, **CLOSED** | none needed | classified, not a defect site |
| 6 | `internal/testutil/lifetimeprobe.go` `writeReadiness` | readiness pipe, fd stated by the caller | BORROWED, **CLOSED** | none needed | classified, not a defect site |
# 7 | `cmd/agent/merger_takeover_lock.go` `mergerTakeoverLockAdoptAt` | fd 3, the launcher's hand-off lock | BORROWED | already fixed by `01a0b5cd` (`4a828ffc`) | RETIRED — the file no longer exists on master |

Row 7 is deliberately kept as a row rather than deleted, with its status corrected:
at briefing time it was "the already-fixed 7th", covered by `4a828ffc` and scheduled for
deletion by `feat/merger-delete-dead-impl` (unmerged then). **That branch has since
landed**: `git cat-file -e master:cmd/agent/merger_takeover_lock.go` → *"fatal: Not a
valid object name"*, so the merger site no longer SURVIVES on master and this branch's
job is the six sites above it. The row stays because the deliverable is a census of the
`os.NewFile` family — a reader grepping for that wrapper in an older tree should find
this line instead of concluding the site was never classified. Measured on master
`fa12d97f`: `git grep -n 'os.NewFile(' master -- 'cmd/*.go' 'internal/*.go' | grep -v
_test.go` returns the six wrap sites plus three comment lines, and no merger file.

**Honest overlap with the sibling branch.** Sites 1–3 are covered by both this
branch and `fix/borrowed-fd-sibling-sites` @ `c0badb0f` (a completed task's
unmerged branch, read as information per Rule 23 — never committed on). The text
differs (this branch imports `internal/borrowedfd` directly from
`daemonlifetime`; the sibling retained locally because it believed the leaf could
not import — it can) and the guard sets differ. A merge of both will conflict;
this is recorded rather than silently resolved.

## The shared helper (`internal/borrowedfd`)

Leaf package: `os` + `runtime` + `time` only, no project import, so **any**
consumer (including `internal/daemonlifetime`, itself a leaf) can import it — and
the retention idiom has ONE definition (Rule 14), never a copy per package.

```go
var wrappers []*os.File        // package-level: the retention must outlive the call

func Detach(f *os.File) {
	if f == nil { return }      // os.NewFile's unusable-number answer: nothing to forget
	runtime.SetFinalizer(f, nil)
	wrappers = append(wrappers, f)
}
```

`Detach` on a nil wrapper is a no-op by contract, because the refusal paths pass
`os.NewFile`'s result straight in and that answer is `nil` in the documented
`uintptr(0)` sentinel shape.

### The measuring instrument lives here too (`settle.go`)

`borrowedfd.SettleFinalizers()` is the ONE forced-GC window (8 rounds, 10ms
pauses — named constants, Rule 1), and `borrowedfd.FinalizerSettleRounds()`
reports the round count so a failure message need not repeat it.

**Why it moved here (a real merger finding, fixed in this branch).** The guards
first measured through `settleFinalizers()`, a helper defined in
`cmd/agent/merger_takeover_lock_test.go`. That file **no longer exists on
master** — it went with the retired merger takeover implementation
(`93009688`, landed by `feat/merger-delete-dead-impl`). The branch therefore
compiled against its own base and **failed to compile the moment it met master**,
proved by copying the two guard files onto a master checkout:

```
$ cp <branch>/cmd/agent/borrowed_fd_{sites,worker_child}_test.go <master-ckout>/cmd/agent/ && go vet ./cmd/agent/
vet: cmd/agent/borrowed_fd_sites_test.go:158:2: undefined: settleFinalizers
```

Ownership of the instrument now sits with the leaf that owns the retention idiom,
so a guard depends only on `internal/borrowedfd` — a package this branch adds —
and never on a test file another task may retire. The same class of dependency was
audited out of the file comments too (`mtlDup`, and a source-pin comment naming
`merger_takeover_prompt_gate_test.go`, also deleted on master).

## Guards — kernel-level, GC-forced, and each with its red-before

Every claim is asked of the KERNEL (`fcntl(F_GETFD)`: `0` = open, `EBADF` =
closed; `readlink /proc/self/fd/N`: which file the number denotes), never of a
wrapper's own state — the unfixed code also "intended" to stop using the wrapper.
The hazard window is opened with `borrowedfd.SettleFinalizers()` — 8 bounded
`runtime.GC()` rounds with 10ms pauses, named constants — because a single `GC()`
usually does not run a finalizer. Rule 27: this is repetition and a scheduling
window, never load — no device or CPU pressure is generated anywhere in this work.

### A second merger finding: the death-pipe fixture and the site's admission contract

`daemonDeathPipe` validates the number's SHAPE **before** wrapping it
(`daemonlifetime.ValidateHoldFD`: at or above `daemonlifetime.InheritFDFloor`,
and a live descriptor whose kernel target is a pipe — master's `ValidateHoldFD`
did not exist when the guard was written). The guard's original fixture was a
low-numbered regular file, so on a merged tree the call was REFUSED before the
wrap and the guard failed while measuring a refusal instead of a drop:

```
    borrowed_fd_sites_test.go:211: daemonDeathPipe refused a live descriptor:
    GOPI_DAEMON_DEATHSIG_HOLD_FD=7 is not a usable death pipe ... fd 7 is below fd 200,
    where os/exec's ExtraFiles entries start
```

The fixture is now what the site really receives — a PIPE read end placed at or
above the floor with `F_DUPFD`, the holder keeping the write end — so the guard
again measures the drop it exists for. The floor is referenced through the
production constant (Rule 1b/13), never a literal.

| guard | file | red on base `52090dc0` | green with fix |
|---|---|---|---|
| `TestBorrowedFDObservationIsSensitive` (calibration) | `cmd/agent/borrowed_fd_sites_test.go` | PASS (it asserts the defect) | PASS |
| `TestDaemonDeathPipeDroppedWrapperLeavesTheNumberLive` | `cmd/agent/borrowed_fd_sites_test.go` | **FAIL** | PASS |
| `TestWorkerDaemonInheritLockRefusalInARealChildLeavesTheLockFDAlive` | `cmd/agent/borrowed_fd_worker_child_test.go` | **FAIL** | PASS |
| `TestWorkerDaemonLockSiteSourcePin` | `cmd/agent/borrowed_fd_sites_test.go` | **FAIL** | PASS |
| `TestWorkerDaemonInheritLockSuccessKeepsTheBorrowedNumberLive` | `cmd/agent/borrowed_fd_sites_test.go` | n/a (needs the fix's own `…At` seam) | PASS |
| `TestDaemonReadyFDSitesCloseTheirBorrowedWrapper` | `cmd/agent/borrowed_fd_sites_test.go` | PASS (characterization) | PASS |
| `TestWatchDetachesItsBorrowedWrapper` (source pin) | `internal/daemonlifetime/borrowed_fd_watch_test.go` | **FAIL** | PASS |
| `TestWriteReadinessClosesItsBorrowedDescriptor` | `internal/testutil/borrowed_fd_ready_test.go` | PASS (characterization) | PASS |
| `TestDetachKeepsABorrowedDescriptorOpen` | `internal/borrowedfd/borrowedfd_test.go` | **FAIL** (body neutered) | PASS |
| `TestDroppedWrapperClosesABorrowedDescriptor` (calibration) | `internal/borrowedfd/borrowedfd_test.go` | PASS (asserts the defect) | PASS |

### The red-before outputs, quoted

Pristine base worktree, detached at the base commit `52090dc0`, with the
base-compatible guard variants **committed on a proof branch**
(`proof/fd-finalizer-borrowed-base-red` @ `10c9066c`, worktree
`~/workspace/gopi-fd-redcheck`, base commit `52090dc0`) so this red is RE-RUNNABLE
rather than a pasted transcript — and the guard BODIES are committed there too
(`10c9066c`, on top of `c59f96cb`'s probe harness), so `git status --porcelain`
is empty in that worktree and the red does not depend on uncommitted files.

THE PROOF BRANCH'S `settleFinalizers` MOVED (relevant if you diff the two sides):
the base commit defines it in `cmd/agent/merger_takeover_lock_test.go` (which
`feat/merger-delete-dead-impl` is retiring); the proof branch's probe harness
carries its own copy in `cmd/agent/borrowed_fd_probe_test.go` and DROPS the
original — both are `_test.go` files, so the proof branch still changes no
production file (`git diff --name-only 52090dc0 proof/… | grep -v '_test\.go$'`
prints nothing, re-measured this session).

```
$ cd ~/workspace/gopi-fd-redcheck   # detached at 52090dc0 + the proof commit
$ go test -count=1 -v ./cmd/agent/ -run 'TestDaemonDeathPipeDroppedWrapperLeavesTheNumberLive'
=== RUN   TestDaemonDeathPipeDroppedWrapperLeavesTheNumberLive/pipe
    daemon_death_pipe_wrapper_test.go:87: fd 8: owner's target "pipe:[121278238]" → after
    dropping the wrapper + 8×runtime.GC(): link "" err=readlink /proc/self/fd/8: no such
    file or directory ⇒ CLOSED (the wrapper's finalizer closed a descriptor its owner
    never opened)
=== RUN   TestDaemonDeathPipeDroppedWrapperLeavesTheNumberLive/regular-file
    daemon_death_pipe_wrapper_test.go:87: fd 7: owner's target "…/GOPI_DAEMON_DEATHSIG_HOLD_FD413278803"
    → after dropping the wrapper + 8×runtime.GC(): link "" err=readlink /proc/self/fd/7: no
    such file or directory ⇒ CLOSED …
--- FAIL: TestDaemonDeathPipeDroppedWrapperLeavesTheNumberLive (0.20s)
FAIL	github.com/nick/gopi/cmd/agent	2.522s

$ go test -count=1 -v ./cmd/agent/ -run 'TestWorkerDaemonDeadlineRefusalLeavesTheBorrowedFDLive'
    worker_daemon_refused_wrapper_test.go:132: production deadline refusal measured:
    inherited worker lock fd 7 not acquirable within 5s: resource temporarily unavailable
    worker_daemon_refused_wrapper_test.go:134: fd 7: owner's target "…/worker.lock4272519516"
    → after dropping the wrapper + 8×runtime.GC(): link "" … ⇒ CLOSED
--- FAIL: TestWorkerDaemonDeadlineRefusalLeavesTheBorrowedFDLive (5.33s)

$ go test -count=1 -v ./cmd/agent/ -run 'TestWorkerDaemonSourcePinsTheBorrowedClassification'
    worker_daemon_refused_wrapper_test.go:356: workerDaemonInheritLock no longer contains
    "borrowedfd.Detach(inherited)"
    worker_daemon_refused_wrapper_test.go:361: workerDaemonInheritLock hands its wrapper to
    borrowedfd.Detach on 0 exit(s), want ≥3 (not-open, not-usable-file, flock-deadline)
--- FAIL: TestWorkerDaemonSourcePinsTheBorrowedClassification (0.00s)

$ go test -count=1 -v ./internal/daemonlifetime/ -run TestWatchSourcePinsTheRetention
    daemonlifetime_watch_wrapper_test.go:249: Watch no longer contains
    "watchHold = append(watchHold, f)"
    daemonlifetime_watch_wrapper_test.go:249: Watch no longer contains
    "runtime.SetFinalizer(f, nil)"
--- FAIL: TestWatchSourcePinsTheRetention (0.00s)
```

The same file also carries the instrument's own calibration on the base commit —
the hazard is REAL there, and the retention is what stops it (this is what makes
the source pins above non-vacuous):

```
--- PASS: TestWatchDisarmShapeWouldCloseTheHolderFD (0.47s)
    daemonlifetime_watch_wrapper_test.go:192: round 1..5: the pre-fix shape CLOSED borrowed
    fd 8 (target "/dev/null" → "" err=readlink … no such file or directory)
    daemonlifetime_watch_wrapper_test.go:205: calibration measured: 5/5 rounds closed a
    borrowed descriptor when the wrapper was dropped with its read completed — the pre-fix
    body IS the defect, and the retention is what stops it
```

Two of the red guards are the same tests by name on both sides
(`TestDaemonDeathPipeDroppedWrapperLeavesTheNumberLive`,
`TestBorrowedFDObservationIsSensitive`); the other three are the base-compatible
variants of this branch's guards, because the branch's guard names refer to
`borrowedfd.Detach`, which the base commit does not have. Both directions are
reproducible with the commands in "Reproduction recipes" below.

The helper's own red-proof, one line apart (the body of `Detach` disabled):

```
--- FAIL: TestDetachKeepsABorrowedDescriptorOpen (0.00s)
    borrowedfd_test.go:98: Detach retained 0 wrappers, want 1: …
--- PASS: TestDroppedWrapperClosesABorrowedDescriptor (0.08s)
```

### The green outputs

```
--- PASS: TestBorrowedFDObservationIsSensitive (0.11s)
--- PASS: TestDaemonDeathPipeDroppedWrapperLeavesTheNumberLive (0.11s)
--- PASS: TestWorkerDaemonInheritLockSuccessKeepsTheBorrowedNumberLive (0.15s)
--- PASS: TestDaemonReadyFDSitesCloseTheirBorrowedWrapper (0.00s)
--- PASS: TestWorkerDaemonInheritLockRefusalInARealChildLeavesTheLockFDAlive (5.18s)
--- PASS: TestWatchDetachesItsBorrowedWrapper (0.00s)
--- PASS: TestWriteReadinessClosesItsBorrowedDescriptor (0.00s)
ok  github.com/nick/gopi/cmd/agent  6.892s
ok  github.com/nick/gopi/internal/borrowedfd  0.202s
```

## Why a real CHILD is the instrument for the worker-lock site

Three facts decided the guard's shape, each measured rather than assumed:

- A Go TEST process cannot reproduce the production fd number: its fd 3 is the
  harness's own. The fix's seam (`workerDaemonInheritLockAt(fd)`) does not exist on
  the base commit, so an in-process guard could never be shown RED there.
- The production damage is about the **fork/exec window**: `os/exec` renumbers and
  dup2s before the child runs, so a wrapper finalized in that window hands the
  child's descriptor number to an unrelated open. A re-exec'd child receives the
  descriptor through `ExtraFiles` exactly as production does, and is the honest
  observer of its own descriptor table.
- A wrapper still referenced by `cmd.ExtraFiles` is REACHABLE, so its finalizer
  cannot run before `Start` returns. The pre-fork arm this would have needed is
  therefore not provably red; the child arm replaces it (`ExtraFiles` is cleared
  by `Start`, so the parent's drop happens with the child already running — which
  is exactly how the guard measured "alive=false" on the base).

The flock fixture needs a SEPARATE PROCESS: `flock` lives on the open file
description, so a dup of the holder's own description re-takes the lock
successfully and the production refusal would never be reached. The guard starts a
`python3` `LOCK_EX` holder and drives the real refused path (it `t.Skip`s if
`python3` is unavailable, and the skip is a `t.Skipf` with its reason, never a
silent pass).

## The pre-`cmd.Start` window: measured, not assumed

The `daemonDeathPipe` wrapper's danger window is the interval between the wrap and
the fork. It was **measured reachable, so the window holds no hazard**: the caller
appends the wrapper to `child.ExtraFiles`, and `os/exec` retains that slice on the
`Cmd` until `Start` returns — the slice *holds* the wrapper rather than copying its
number, so it stays reachable and the finalizer cannot run in that window.

That is a property of the CALLER, not of the returned value: a different caller
(a refusal exit, or a future spawn path that stops populating `ExtraFiles`) gets no
such protection. So the classification is recorded at the site, and `borrowedfd.Detach`
stays where it is — the guarantee that does not depend on who calls it.

A pre-fork child-arm guard for this window was written and then **removed**: a wrapper
that `Cmd.ExtraFiles` keeps reachable cannot be driven red, and a guard that cannot
fail certifies nothing. The reason lives in the site comment instead of in a test
that would always pass.

## What is NOT observable (stated, not dressed up)

- **The `Watch` disarm route is unreachable in production today.** The blocking
  read's completion routes all call `selfTerminate` (EOF *and* a written byte), so
  the hazard's moment coincides with this process being SIGKILLed and no
  observation can be taken. The disarm branch needs an `O_NONBLOCK` read end, and
  the holder's pipe comes from `os.Pipe` (blocking by construction). The guard is
  therefore a SOURCE PIN, reported as such.
- **The `^uintptr(0)` and not-a-usable-file refusals are unprovokable with a live
  owner**: `os.NewFile` performs no validity check — it wraps a free number happily
  and the failure surfaces later as `EBADF` from the syscall. Those exits are
  pinned by source, not by fixture.
- **`daemonDeathPipe`'s "not usable" refusal (`death == nil`) is unreachable** for
  the same reason. Filed as its own task `01a0b61a` (bug) by the sibling work; the
  `Detach` there is harmless and keeps the exits uniform.

## Reproduction recipes

```bash
# RED — the base commit with the base-compatible guard variants COMMITTED on a
# proof branch, so this is re-runnable rather than a pasted transcript. The ref
# is ALSO a tag (`proof-fd-finalizer-base-red`), because the branch ref was
# pruned by a branch sweep mid-session while the worktree was live — a tag is the
# durable spelling of the same commit:
git branch proof/fd-finalizer-borrowed-base-red 10c9066c   # if the ref is missing
git worktree add --detach /home/user/workspace/gopi-fd-redcheck proof-fd-finalizer-base-red   # FIRST: a sweep removes this worktree, the tag survives
cd /home/user/workspace/gopi-fd-redcheck   # @ 10c9066c (base 52090dc0); git status --porcelain empty
go test -count=1 -v ./cmd/agent/ -run 'TestDaemonDeathPipeDroppedWrapperLeavesTheNumberLive|TestDaemonDeathPipeSourcePinsTheBorrowedClassification|TestWorkerDaemonDeadlineRefusalLeavesTheBorrowedFDLive|TestWorkerDaemonSourcePinsTheBorrowedClassification|TestBorrowedFDObservationIsSensitive'
go test -count=1 -v ./internal/daemonlifetime/   # TestWatchSourcePinsTheRetention FAILS there; the two calibrations PASS
go test -count=1 -v ./internal/daemonlifetime/ -run TestWatchDisarmShapeWouldCloseTheHolderFD   # the calibration: the hazard IS real there

# GREEN — on the branch:
cd ~/workspace/gopi-fd-sites
go test ./internal/borrowedfd/ ./internal/daemonlifetime/ ./internal/testutil/ ./cmd/agent/

# NOTE for ./cmd/agent in a FRESH worktree: bin/gopi is gitignored, so the e2e tests that
# shell out to it fail to spawn and hang to the package's 10m panic. Build it first:
CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/     # then the suite is green

# the helper's own red-proof: neuter Detach's body (SetFinalizer + append), rerun
go test ./internal/borrowedfd/ -run 'TestDetachKeepsABorrowedDescriptorOpen|TestDroppedWrapperClosesABorrowedDescriptor'
```

## Files

- `internal/borrowedfd/borrowedfd.go` (+ `borrowedfd_test.go`) — the leaf helper.
- `cmd/agent/cli_worker_daemon.go`, `cmd/agent/cli_daemon_spawn.go` — site
  classification comments, detach at the refusal/death-pipe exits, the
  `workerDaemonInheritLockAt` fd seam.
- `internal/daemonlifetime/daemonlifetime.go` — classification + `Detach` at the
  `Watch` construction site.
- `internal/testutil/lifetimeprobe.go` — classification of the closed readiness
  site.
- `cmd/agent/borrowed_fd_sites_test.go`, `cmd/agent/borrowed_fd_worker_child_test.go`,
  `cmd/agent/testmain_test.go` (the child dispatch hook),
  `internal/daemonlifetime/borrowed_fd_watch_test.go`,
  `internal/testutil/borrowed_fd_ready_test.go` — the guard set.

## Close-out re-verification (this session's own observations)

Everything above was re-measured at close-out rather than carried over, and three
claims were CORRECTED in the process — recorded here so a reader does not have to
trust the earlier text:

1. **The red-before tree**: the `/home/user/workspace/gopi-fd-base` worktree of the proof
   branch was removed by a branch/worktree sweep while this session ran, and the
   `proof/fd-finalizer-borrowed-base-red` ref was pruned with it (the commit object
   survived, so the red is intact — the SPELLING of it was gone). The ref is
   restored (`git branch proof/fd-finalizer-borrowed-base-red 10c9066c`) and is now
   ALSO a tag, `proof-fd-finalizer-base-red`, because a tag is the durable spelling;
   every path in the recipes above now names the live worktree
   `/home/user/workspace/gopi-fd-redcheck`. The sweep is the reason this devdoc states a
   recovery command next to each measurement.
2. **A red command in an earlier revision of this report named an arm that does not
   exist** — `-run '…TestWorkerDaemonInheritLockRefusal'` matches nothing on the
   proof branch (`ok … [no tests to run]`), so that transcript reproduced no red.
   The corrected command names the proof branch's real arms and its output is
   quoted in `tasks/fd-finalizer-borrowed-sites-report.md`.
3. **The stylegate hit counts** are 234/234 with an EMPTY `comm` in both
   directions, re-measured against a freshly created pristine base worktree
   (`/home/user/workspace/gopi-fd-pristine-off`, `52090dc0`, porcelain empty) — not merely
   equal counts but an identical hit SET.

| gate | command | observed |
|---|---|---|
| build | `go build ./...` | exit 0, no output |
| vet | `go vet ./...` | exit 0, no output |
| helper + leaf tests | `go test ./internal/borrowedfd/ ./internal/daemonlifetime/` | `ok` both |
| readiness guard | `go test ./internal/testutil/ -run TestWriteReadinessClosesItsBorrowedDescriptor` | `ok`, PASS |
| 6 cmd/agent guards | `go test -count=1 -v ./cmd/agent/ -run '…the 6 names…'` | all PASS (real-child arm ~5.5s) |
| **merge compatibility** | `git merge --no-commit master` in a scratch worktree at `master`, conflicts resolved, then `go build ./... && go vet ./…` + the guards | build/vet exit 0; all guards `ok` — see the merger findings above |
| stylegate | `go test -run TestStyleGate ./internal/stylegate/` in each worktree | 234 hits each, `comm` empty both ways |
| standards | `go test ./internal/standards/` in each worktree | same failing test, same 26-path file set, `comm` empty both ways |

## Close-out re-verification, rounds 2 and 3 (the merger round)

Round 3 added the two merger findings above (the instrument's ownership, the
death-pipe fixture) and re-measured every claim in this document on a tree that
already contains those two fixes:

- **RED re-measured, live** on the proof tag `proof-fd-finalizer-base-red` @
  `10c9066c` in `/home/user/workspace/gopi-fd-redcheck` (recreated this round — a sweep
  had pruned the worktree again, which is why the devdoc carries the recovery
  command): `TestDaemonDeathPipeDroppedWrapperLeavesTheNumberLive` FAILS on both
  sub-cases (fd 8 pipe, fd 7 regular file — `readlink /proc/self/fd/N` → `no such
  file or directory` ⇒ CLOSED), `TestDaemonDeathPipeSourcePinsTheBorrowedClassification`
  FAILS, `TestWorkerDaemonDeadlineRefusalLeavesTheBorrowedFDLive` FAILS (fd 7 CLOSED),
  `TestWorkerDaemonSourcePinsTheBorrowedClassification` FAILS, and
  `TestWatchSourcePinsTheRetention` FAILS (both literals absent) — while the two
  CALIBRATIONS PASS there (`TestBorrowedFDObservationIsSensitive`,
  `TestWatchDisarmShapeWouldCloseTheHolderFD` "calibration measured: 5/5 rounds
  closed a borrowed descriptor").
- **GREEN re-measured** on the branch after both fixes: the 6 `cmd/agent` guards
  (`ok`, real-child arm 5.765s), `internal/borrowedfd`, `internal/daemonlifetime`,
  and the `internal/testutil` readiness guard all `ok`.

A correction stated plainly, because the earlier rounds reported it differently:
the branch's guard NAMES differ from the proof branch's — the proof branch carries
base-compatible variants because a base commit has no `borrowedfd` package to
reference. `tasks/fd-finalizer-borrowed-sites-report.md` documents both name sets
and the exact `-run` patterns for each side.

## Close-out re-verification, round 4 — the branch now CARRIES master

Round 3 verified the branch against a *simulated* merged tree. This round merges
master (`fa12d97f`) into the branch itself, so the merge compatibility is no
longer a simulation:

```
$ git merge master
Auto-merging devdoc/DOC-INDEX.tsv
CONFLICT (content): Merge conflict in devdoc/DOC-INDEX.tsv
Auto-merging internal/daemonlifetime/daemonlifetime.go
Automatic merge failed; fix conflicts and then commit the result.
   UU cmd/agent/cli_daemon_spawn.go
   UU devdoc/DOC-INDEX.tsv
$ git commit -m "merge master into feat/fd-finalizer-borrowed-sites: …"
bc2f5b35
```

Both conflicts are the expected shape and both resolutions KEEP BOTH SIDES — the
one thing a resolution must not do here is drop a sentence another task added:

1. **`cmd/agent/cli_daemon_spawn.go`** — master added the `ValidateHoldFD`
   paragraph to `daemonDeathPipe`'s doc comment; this branch added the `OWNERSHIP:
   … BORROWED` note and the pre-`cmd.Start` window paragraph to the same comment.
   Resolved by keeping master's paragraph and the branch's paragraphs, in that
   order (the ownership note is the invariant; the validation note explains the
   refusal that precedes the wrap). No code line was disputed — the two sides'
   code changes (master's `ValidateHoldFD` pre-check, this branch's
   `borrowedfd.Detach(death)`) are both present in the merged function, which is
   exactly the pairing the round-3 finding predicted.
2. **`devdoc/DOC-INDEX.tsv`** — master appended `daemon-death-pipe-fd-shape.md`
   at the end of the manifest; this branch appended its own row. Resolved as the
   union (both rows retained, the empty separator line dropped, so the gate's
   "every devdoc file is classified" reading holds for both).

Re-measured on the MERGED branch (`bc2f5b35`), this session's own observations:

| gate | command | observed |
|---|---|---|
| build | `go build ./...` | exit 0, no output (the merge did not break the build — the round-3 blocker is now provably closed) |
| vet | `go vet ./...` | exit 0, no output |
| helper + leaf | `go test ./internal/borrowedfd/ ./internal/daemonlifetime/` | `ok` both |
| readiness guard | `go test ./internal/testutil/ -run 'Borrowed\|Readiness'` | `--- PASS: TestWriteReadinessClosesItsBorrowedDescriptor`, `ok 0.371s` |
| 7 `cmd/agent` guards | `go test ./cmd/agent/ -run 'BorrowedFD\|Detach\|DaemonDeathPipeDroppedWrapper\|WorkerDaemonInheritLockRefusalInARealChild\|DaemonReadyFDSitesCloseTheirBorrowed\|WorkerDaemonInheritLockSuccessKeeps'` | all PASS, `ok 11.541s` (real-child arm 5.13s) |
| full `cmd/agent` | `go test ./cmd/agent/` after `go build -o bin/gopi ./cmd/agent/` | see the report's close-out section for this round's run |
| stylegate | `go test ./internal/stylegate/` on the merged branch vs a pristine `fa12d97f` worktree | **225 unique hit rows on BOTH sides, `diff` of the two sorted sets prints NOTHING**, and 0 of the 225 name a file this change touches |
| standards / doc-class | `go test ./internal/standards/ -run 'TestDocManifest\|TestDocClass'` on both sides | 29 lines each, `comm -3` shows exactly ONE differing line: the manifest count `2018 rows cover 2054 files` → `2019 rows cover 2055 files` (class `normative` 404 → 405) — this change's own row + file, nothing else. The gate is RED at master on 25 pre-existing unclassified files, unchanged by the merge |

`git status --porcelain` in this worktree prints nothing, and `bin/gopi` (built
for the e2e tests) is gitignored (`.gitignore:2:bin/`), so the close-out tree is
clean by construction rather than by a cleanup step.
