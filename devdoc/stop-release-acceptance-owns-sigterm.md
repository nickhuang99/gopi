<!-- doc-class: normative -->
# The stop-release acceptance must own the process's SIGTERM disposition

## The symptom that was reported, and why it was the wrong diagnosis

The bug row said `TestWorkerStopReleasesOwnWorktree` "delivers a real SIGTERM to
the test process, so the test binary is killed and only `signal: terminated`
plus a package-level FAIL is left". Measured in this tree (`c9c0534`), that is
not what happens — the process SURVIVES and the test produces a genuine
assertion failure:

```
$ go test ./cmd/agent -run 'TestWorkerStopReleasesOwnWorktree' -count=1 -v > /tmp/single.txt 2>&1; echo $?
1
=== RUN   TestWorkerStopReleasesOwnWorktree
    worker_stop_accept_test.go:124: release lines:
    worker_stop_accept_test.go:126: the stopped round's own release did not run: ""
--- FAIL: TestWorkerStopReleasesOwnWorktree (0.12s)
FAIL
```

No `signal: terminated` line anywhere in that file. The "self-kill" story was
true of the SIGHUP sibling (`TestWorkerDrainsOnSIGHUP`, bug 01a16502) and was
carried over to this arm by the family resemblance; it does not describe this
one. This matters because the two have opposite remedies: a killed process needs
a signal-disposition fix, an assertion failure needs the measurement fixed.

## The actual mechanism (measured, not inferred)

`internal/testutil`'s pin arms a SIGTERM handler for the suite's run root
(`armSuiteRunRootReapOnSignal`, `internal/testutil/suiterootsignal.go`). It
subscribes to SIGTERM, and it CANNOT know whether the process is about to die
(the panel of `os/signal` peers is unreadable from outside the package — the
file's own header records the measurements), so its only action safe for both
outcomes is a REVERSIBLE one: rename the whole run root aside, re-raise, and
wait `suiteRunRootSurviveSettle` (500 ms) to learn which outcome happened.

The test delivers a real SIGTERM **to its own process**, and it lives INSIDE
that root — `t.TempDir()` is the pinned suite temp root. So the arm and the
worker's handler both fire, and while the arm is measuring its own survival the
test's ledger has been moved away:

```
testutil: terminated — moved this run's scratch root
  /dev/shm/suite-cmd-agent-run-1328525266 aside to
  /dev/shm/suite-cmd-agent-run-1328525266_shelved (the owner registration names
  this process) so it is gone if this process dies and back if it does not
scratch: recreated suite run root /dev/shm/suite-cmd-agent-run-1328525266
  (written to after that run's pin had removed it) — registered pid 1512748 as
  its owner so the next run of the suite can collect it
```

That second line is the destructive half: `ReclaimOwnFinishedWorktrees` opens
`GOPI_MEMORY_DB`, the path no longer resolves, `openTasksDB` materialises a
FRESH ledger at it (`MkdirAll` recreates the root, `ensureKnowledgeBase` builds
the schema), and the release pass then queries an empty database. A debug probe
forced the same shape and printed the counts:

```
ZZDEBUG reclaimSelfRows: db="/dev/shm/suite-cmd-agent-run-1328525266/.../001/memory.db"
  total=0 mine=0 filtered=0 stamp="host-b:1512748"
```

`total=0` is the whole defect: the row the test seeded seconds earlier is not
there, so "the release did not run" is the truthful report of what was
measured — an empty ledger.

The arm then fails to put the root back (`rename ..._shelved ...: file exists`,
because `MkdirAll` recreated the name), which is the second, noisier line every
run of this test has been printing.

## The fix, and why it is a DISARM rather than a change to the arm

The test calls `testutil.JoinSuiteRunRootSignalHandler()` before it delivers its
signal. That is the API's own documented contract — it disarms the arm and waits
for its goroutine, and `FinishSuiteForTestMain` calls it on the clean exit path
— so no production surface was added and the arm's own behaviour is untouched.

It is also the honest shape of the observation. The arm's job is a suite KILLED
FROM OUTSIDE (a budget timeout, `timeout(1)`, a supervisor). A SIGTERM raised by
the process against itself is exactly the "in-process speaker" case the arm's
header already rules out for SIGHUP/SIGINT/SIGQUIT — SIGTERM stayed in the list
because a supervisor sends it, not because a test does. In production the only
subscriber of the stop signal is `installWorkerStopSignals`, so the arm was
test-infrastructure noise that ALTERED the outcome under test.

## The honest trade, stated so it is not a silent one

The join is process-global and one-way: for the REST of the suite run, a SIGTERM
will not be reaped by that arm, so a run killed by an outside SIGTERM after this
test would leave its root on disk with a dead `.holder`. That residue is the
shape the existing start-time sweep collects (`ReapDeadSuiteRunRootsUnder`, at
any age, by owner record) and the 6 h age window also covers it — the same
recovery the arm's own disarm path already relies on. A re-arm API was rejected:
it would be a new production surface inside test infrastructure, for a window
that an existing collector already bounds.

## Verification (red → green, single-point)

Green, current tree:

```
$ go test ./cmd/agent -run 'TestWorkerStopReleasesOwnWorktree' -count=1 -v ; echo $?
    worker_stop_accept_test.go:125: release lines: own worktree released:
      feat/self-release-e2e (01a0962e-stop-release) freed for merge; branch kept
--- PASS: TestWorkerStopReleasesOwnWorktree (0.13s)
ok  	github.com/nick/gopi/cmd/agent	2.006s
0
```

Whole group, package verdict:

```
$ go test ./cmd/agent -run 'TestWorkerStop' -count=1 > /tmp/g.txt 2>&1 ; echo $?
ok  	github.com/nick/gopi/cmd/agent	12.885s
0        (--- FAIL: count = 0)
```

Red arm — the ONE line disabled (`testutil.JoinSuiteRunRootSignalHandler()` →
`_ = testutil.JoinSuiteRunRootSignalHandler`), nothing else touched:

```
$ go test ./cmd/agent -run 'TestWorkerStopReleasesOwnWorktree' -count=1 -v ; echo $?
--- FAIL: TestWorkerStopReleasesOwnWorktree (0.11s)
1        (--- FAIL: count = 1)
```

and the restore was verified against a revision, not the index:
`git hash-object <path>` == `git rev-parse HEAD:<path>` after re-applying the
one line, then the green run above re-taken.
