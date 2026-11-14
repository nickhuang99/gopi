<!-- doc-class: historical -->
# The serve-lock diagnosis helper died with exit 99 — a fixture that ran the suite entry point

**Kind:** measured post-mortem of a defect fixed in this change.
**Host:** hosta. **Tree:** clone `01a1ba46-9a8e-7bc2-b867-7d558d96eb28`, base `a53308f84c04`.
**Evidence (raw commands and their output):**
`~/.gopi/sessions/01a1ba46-9a8e-7bc2-b867-7d558d96eb28/evidence/`
(`probe_helpers.sh` + `.out`, `probe_locks.sh`, `pkg-run.log`).

## The symptom, verbatim

`go test ./cmd/agent/ -count=1 -run TestServeLockRefusalDiagnosisNamesTheStrayHolderAndItsLiveness -v`
— alone, with no other run in flight — printed:

```
=== RUN   TestServeLockRefusalDiagnosisNamesTheStrayHolderAndItsLiveness
    serve_lock_diagnosis_liveness_test.go:657: diagnosis: the helper reached its body 658ms after spawn with a 40.16s window (fixture floor 5.02s)
    serve_lock_diagnosis_liveness_test.go:689: the lock-holder helper EXITED before publishing its pids at
        /dev/shm/suite-cmd-agent-run-218087734/TestServeLockRefusalDiagnosisNamesTheStrayHolderAndItsLiveness2307748291/001/.gopi/serve.lock.fresh.pids — it died with exit status 99.
        This is a BROKEN FIXTURE, not a slow host (pid 2401325, started 505ms ago, window 40.16s).
        helper stderr:
        FATAL: suite "cmd-agent": another run of this package is in flight.
          lock:     /dev/shm/suite-cmd-agent-run-218087734/TestServeLockRefusalDiagnosisNamesTheStrayHolderAndItsLiveness2307748291/001/workspace/gopi-test-scratch/suite-package-github.com_nick_gopi_cmd_agent-7fc38fdc
          holder:   pid 2401207, started 2026-11-08T10:16:57Z, startticks 203760384, directory /home/user/workspace/01a1ba46-9a8e-7bc2-b867-7d558d96eb28/cmd/agent, verdict live
--- FAIL: TestServeLockRefusalDiagnosisNamesTheStrayHolderAndItsLiveness (3.25s)
```

Two words in that text are the whole answer, and neither is the one the rig's own
message suggested: **`exit status 99`** is `testutil.SuiteBusyExitCode`
(`internal/testutil/suitepackage.go:55`), and the `holder:` line names the
*helper's own sibling* — a pid the test binary had started milliseconds earlier,
not a stray operator run.

## The branch that produces 99

```
testutil.PinSuiteForTestMain(cmd-agent)                    cmd/agent/testmain_test.go:42
  └ testutil.AcquireSuitePackageLock(suite)                internal/testutil/suitepackage.go:131
     └ scratch.AcquireSuitePackageLock(home, key, wd)      internal/scratch/suite_package_lock.go:199
        ├ os.Mkdir(lockdir) == nil        → Acquired
        ├ EEXIST → OwnerVerdictFor(dir)
        │     ├ verdict.Registered && suitePackageHolderIsAncestor → Nested (proceed)
        │     ├ verdict.Collectable                               → break, retry once
        │     └ otherwise → &SuitePackageBusyError{   ← **the exit-99 branch**, internal/scratch/suite_package_lock.go:243
        └ caller prints the refusal and os.Exit(SuiteBusyExitCode)
```

`SuiteBusyExitCode = 99` is defined once, at `internal/testutil/suitepackage.go:55`,
and the take is refused at `internal/scratch/suite_package_lock.go:199`'s
`else`-shaped arm — the three lines that build `&SuitePackageBusyError`.
`internal/testutil/suitepackage.go:133` is what turns it into the process exit.

## Why the helper reached that branch AT ALL

The rig spawns its holders by re-exec'ing **this same test binary**:

```go
cmd := exec.Command(os.Args[0], "-test.run=TestServeLockDiagnosisHelperProcess")
cmd.Env = append(os.Environ(), env...)          // startDiagnosisHelper, :434
```

`-test.run=TestServeLockDiagnosisHelperProcess` does **not** bypass `TestMain`.
The helper process therefore ran the entire suite entry point first — every pin
(`PinSuiteTempRoot`, `PinSuiteLedgerEnv`, `PinSuiteNotifyEnv`, `PinSuiteGitEnv`, `PinSuiteGopiProfile`, `PinSuiteAutoRegisterEnv`)
and then `AcquireSuitePackageLock`, which took a whole-package cross-run lock on
behalf of a process that was only ever going to `flock()` one file and die.

That is survivable while one helper is alive. The arc that kills it is the rig's
own second stage: **stage 1's holder is still alive** (`diagnosisHoldSec = 60`)
when stage 2 spawns its helper, and both are children of the test binary. So:

| process | what it took |
|---|---|
| test binary | the package lock in **its own** scratch placement (`/dev/shm/suite-cmd-agent-run-218087734/workspace/gopi-test-scratch/`), taken at `TestMain` — before any `t.Setenv` of the case exists |
| helper A (`hold`, stage 1) | a SECOND lock, in a DIFFERENT home — its pins ran with the case's `HOME` (inherited through `t.Setenv`) already in place, so its lock landed at `<the case's 001/ directory>/workspace/gopi-test-scratch/suite-package-github.com_nick_gopi_cmd_agent-7fc38fdc`, the exact path quoted in the refusal |
| helper B (stage 2's `hold`) | `EEXIST` on **A's** lock. The recorded holder is A: live, and a **sibling** (both are children of the test binary), so `suitePackageHolderIsAncestor` correctly answered false |

The lock is keyed on `ScratchHomeDir()`, and each helper's own pins move that
home (`PinSuiteTempRoot` derives it from the process's own `HOME`/`TMPDIR`
pins), which is why the directories in the two helpers' environments differ:

```
--- pid 2450969 (hold, stage 1) ---
HOME=/dev/shm/suite-cmd-agent-run-8003020/TestServeLockRefusalDiagnosisNamesTheStrayHolderAndItsLiveness3162562399/001
GOPI_TEST_SCRATCH_HOME=/home/user/workspace/gopi-test-scratch
TMPDIR=/dev/shm/suite-cmd-agent-run-8003020
--- pid 2451618 (inherit, stage 1's leak child) ---
GOPI_TEST_SCRATCH_HOME=/dev/shm/suite-cmd-agent-run-8003020/TestServeLockRefusalDiagnosisNamesTheStrayHolderAndItsLiveness3162562399/001/workspace/gopi-test-scratch
TMPDIR=/dev/shm/suite-cmd-agent-run-8003020/suite-cmd-agent-run-2121077179
```

Two helpers, two *different* scratch homes, one package name: the second one's
`Mkdir` lost to the first one's directory, the recorded holder was neither dead
nor an ancestor, and the fixture exited 99 before writing the pid marker. The
rig's readiness handshake then truthfully reported "a BROKEN FIXTURE" — the
handshake was right; the fixture had become a run of the suite.

**Measured, not inferred:** both helpers were caught alive in one poll loop with
their own environments (`evidence/probe_helpers.out`), and the parent's own
stderr carried the `FATAL: suite "cmd-agent": another run of this package is in
flight` block with `holder: pid <the stage-1 helper>`.

## The fix

`cmd/agent/testmain_test.go` dispatches a helper-mode process **before the pins**,
beside the two fixture children that already lived there
(`RunDaemonLeakSpawnerChild`, `RunBorrowedFDWorkerChild`):

```go
if RunServeLockDiagnosisHelperChild() {
    return
}
```

`RunServeLockDiagnosisHelperChild` reads `GOPI_TEST_SERVE_DIAGNOSIS_MODE`; unset
(the ordinary `go test` case) it answers false and nothing changes. Set, it calls
`os.Exit(serveLockDiagnosisHelperMain(mode))` — the holder body moved out of
`TestServeLockDiagnosisHelperProcess` verbatim, except that a fault now returns a
non-zero code instead of calling `t.Fatalf`, because the process never reaches
`m.Run` and there is no `*testing.T` to report to.

`TestServeLockDiagnosisHelperProcess` keeps its role as the re-exec target
(`-test.run` names it) and now **fails loudly** if a helper mode ever reaches it:

```
helper mode %q reached the suite's TestMain dispatch bypass — see RunServeLockDiagnosisHelperChild
```

That is the guard against a future spawn that re-adds `-test.run=<some other test>`
without the dispatch: it says what broke instead of silently re-entering the pins.

**Not** the alternative fixes, and why:

- *Pass `GOPI_SUITE_PACKAGE_LOCK=0` into the helper's env.* It would silence the
  fixture by disengaging the operator's escape in a child — the suite pins,
  scratch placement, ledger pin and notify pin would all still run in a process
  that must only `flock()` a file. The exclusion was right to fire; the fixture
  was wrong to be a suite.
- *Wait for the lock instead of refusing.* That is the model the exclusion
  explicitly rejects (`a silent wait would be reported as a stall (NO PROGRESS)
  about the wrong thing`), and it would make a 60 s holder part of the run's wall
  clock.
- *Teach `suitePackageHolderIsAncestor` about siblings.* Siblings ARE different
  runs by the rule's own definition; widening it would let two whole-suite runs
  interleave, which is the defect the lock exists to prevent.

## Verification (the numbers travel with this record)

```
$ go test ./cmd/agent/ -count=1 -run 'TestServeLockRefusalDiagnosisNamesTheStrayHolderAndItsLiveness' -v
=== RUN   TestServeLockRefusalDiagnosisNamesTheStrayHolderAndItsLiveness
--- PASS: TestServeLockRefusalDiagnosisNamesTheStrayHolderAndItsLiveness (0.31s)
```

RED proof — with the `TestMain` dispatch temporarily removed, the new regression
case goes red on the very sentence this post-mortem is about:

```
--- FAIL: TestServeLockDiagnosisHelperRunsOutsideTheSuiteEntryPoint (2.41s)
    serve_lock_diagnosis_helper_dispatch_test.go:99: the lock-holder helper EXITED before publishing its pids at /dev/shm/suite-cmd-agent-run-3240561312/TestServeLockDiagnosisHelperRunsOutsideTheSuiteEntryPoint3306691023/001/.gopi/serve.lock.dispatch.pids — it died with exit status 1.
    — it died with exit status 1.
    helper stderr:
        testutil: suite "cmd-agent": scratch root /dev/shm/suite-cmd-agent-run-3240561312 is tmpfs (memory filesystem) (placement memory-root, 17.7 GiB free) — fresh ledgers under it are built on a memory filesystem, so their first opens pay no block device's journal commits
        testutil: suite "cmd-agent": GOPI_MEMORY_DB="/dev/shm/suite-cmd-agent-run-3240561312/suite-gopi-home/.gopi/memory.db" is INHERITED (a test named this ledger, typically to hand it to a child process) — the pin leaves it alone rather than overruling a deliberate choice. Nothing falls back to /dev/shm/suite-cmd-agent-run-3240561312/TestServeLockDiagnosisHelperRunsOutsideTheSuiteEntryPoint3306691023/001/.gopi/memory.db.
```

(exit **1**, not 99, in that run: without the dispatch the helper reached the
suite pins and the moved body's `t.Fatalf` bypass guard — the same class of
failure, one layer earlier. With the dispatch present both are unreachable.)

## What this is NOT

The pre-existing stale holder `pid 3634507` — the one every run's first line
mentions (`kept suite run root /dev/shm/suite-cmd-agent-run-769077469: the owner
registration names another LIVE process`) — is **unrelated**. It names the
`gopi-test-scratch` base's run root, not `cmd-agent`'s package lock
(`suite-package-github.com_nick_gopi_cmd_agent-7fc38fdc`), and it is reported by
`ReapDeadSuiteRunRootsAtSuiteStart` on every suite start whether the diagnosis
case passes or fails. Measured: the case is green while that line is still printed
(quoted above), and green again under `GOPI_SUITE_PACKAGE_LOCK=0` (exit 0, PASS
1.59s) while the same line was present.
