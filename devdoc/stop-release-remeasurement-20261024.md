<!-- doc-class: historical -->
# Re-measurement: `TestWorkerStopReleasesOwnWorktree` is GREEN on master `adb7dc0` — the fix landed twice, and the earlier red-proof no longer applies

## Why this run exists

Task `01a16505` diagnosed this test's failure (`testutil`'s suite-run-root signal
arm renaming the pinned suite root aside under the test's own real SIGTERM, so
`ReclaimOwnFinishedWorktrees` opened a vanished `GOPI_MEMORY_DB`, `MkdirAll`
built an empty ledger, and the release scan found `total=0`) and delivered the
test-side fix at `268b058`. That diagnosis was anchored on `c9c0534`. Master has
moved since (12 commits at the time of this run), so the RED assertion was
re-measured on this ground's HEAD rather than inherited.

`devdoc/stop-release-acceptance-owns-sigterm.md` is the normative account of the
mechanism and the fix; **this page is the measurement record** and supersedes
nothing. Read that page for the mechanism; read this one for "what is true on
`adb7dc0`".

## Measured state on this ground's HEAD (`adb7dc0`)

```
$ git rev-parse HEAD
adb7dc0f7c7032710930907693da14218a311ad38

$ go test ./cmd/agent/ -run 'TestWorkerStopReleasesOwnWorktree' -count=1 -v > /tmp/a-01a1696d.log 2>&1; echo $?
0
--- PASS: TestWorkerStopReleasesOwnWorktree (0.12s)
ok  	github.com/nick/gopi/cmd/agent	2.138s
--- FAIL: count = 0

$ go test ./cmd/agent/ -run 'TestWorkerStop' -count=1 -v > /tmp/b-01a1696d.log 2>&1; echo $?
0
15 × --- PASS, 0 × --- FAIL
ok  	github.com/nick/gopi/cmd/agent	12.214s
```

The line the assertion reads, verbatim from the run:

```
worker_stop_accept_test.go:125: release lines: own worktree released:
  feat/self-release-e2e (01a0962e-stop-release) freed for merge; branch kept
```

**GREEN, and no `_shelved` / `rename ...: file exists` line appears anywhere in
either log.** That absence is the observable that the mechanism is gone, not
merely tolerated.

## Which commit made it green — two legs, and the SECOND one is the root-cause fix

Both are ancestors of this ground's HEAD (`git merge-base --is-ancestor <c> HEAD`
→ exit 0 for each), and their order is the interesting part:

| commit | direction | what it did |
|---|---|---|
| `749a2c1` `fix(cmd/agent): the stop-release acceptance disarms the suite's signal arm first` | test side | the test calls `testutil.JoinSuiteRunRootSignalHandler()` before it raises SIGTERM |
| `a3c6efc` `fix(scratch): a LIVE registered owner's suite root is never collectable` | arm side (CONTAINS `749a2c1`) | `armSuiteRunRootReapOnSignal` **stops touching the root at all** |

`749a2c1` alone made this test green by removing the *subscriber*.
`a3c6efc` removed the *move-aside itself*: the arm now asks
`scratch.SuiteRunSelfReapVerdict` (whose `SelfReapReasonOwn` says a live
registration is evidence of a RUN, never of residue), prints the root it is
leaving in place, then `signal.Stop` + re-raises. So the defect class cannot be
reintroduced by a *different* self-signalling test — there is no arm action left
to corrupt the root.

Corroboration in the green log's own text — the arm now reports the opposite of
what the 2026-10-23 log said:

```
testutil: terminated — left this run's scratch root /dev/shm/suite-cmd-agent-run-3657888818
  in place (the owner registration names this process, which is LIVE — a run keeps its
  own root); ... (collectable=false)
testutil: terminated — this process SURVIVED the re-raise ..., so the run continues with
  its scratch root /dev/shm/suite-cmd-agent-run-3657888818 intact
```

`grep -n 'shelved\|os.Rename' internal/testutil/suiterootsignal.go` now matches
only a COMMENT quoting the pre-fix log.

## The consequence a future reader must not get wrong: the old red-proof is DEAD

`devdoc/stop-release-acceptance-owns-sigterm.md` and memory `#14423` both describe
the red proof as "disable the `JoinSuiteRunRootSignalHandler()` line and watch it
go red". **On `a3c6efc` and later that proof no longer bites** — measured on this
ground's HEAD:

```
$ git diff --stat -- cmd/agent/worker_stop_accept_test.go      # Join call AND its import removed
 cmd/agent/worker_stop_accept_test.go | 2 --
$ go test ./cmd/agent/ -run 'TestWorkerStopReleasesOwnWorktree' -count=1 -v; echo $?
0    --- PASS: TestWorkerStopReleasesOwnWorktree (0.14s)   --- FAIL: count = 0
```

Green. Correctly so: with no arm action there is nothing to corrupt, so the join
is now a **belt**, not the load-bearing fix. Do not read that green as "the join
was unnecessary" — it was necessary at `749a2c1`'s baseline, and it is still
correct to keep (a self-signalling test should own the process's SIGTERM
disposition; production's only subscriber is `installWorkerStopSignals`).

A red proof must therefore mutate something that CAN still fail. The one used
here replaces the release call itself, keeping the assertion untouched:

```
$ # lines := tools.ReclaimOwnFinishedWorktrees()  →
$ # lines := []string{} // NON-VACUITY PROBE: the release never runs
$ go test ./cmd/agent/ -run 'TestWorkerStopReleasesOwnWorktree' -count=1 -v; echo $?
    worker_stop_accept_test.go:127: the stopped round's own release did not run: ""
--- FAIL: TestWorkerStopReleasesOwnWorktree (0.10s)
1    --- FAIL: count = 1
```

Restored revision-anchored (index-free), then green re-taken:

```
$ test "$(git hash-object cmd/agent/worker_stop_accept_test.go)" \
     = "$(git rev-parse HEAD:cmd/agent/worker_stop_accept_test.go)" && echo ok
ok        both = b9e945404a119a7a366dcacbd88e177f33170270
$ go test ./cmd/agent/ -run 'TestWorkerStopReleasesOwnWorktree' -count=1   # exit 0, PASS
$ git status --porcelain                                   # (empty)
```

## The assertion was not touched

`git diff adb7dc0..HEAD -- cmd/agent/worker_stop_accept_test.go` is **empty (0
bytes)** — this run changed no production or test code at all; the deliverable is
the measurement. `strings.Contains(joined, "own worktree released")` stands, and
the marker text is `internal/tools.ReclaimSelfReleasedMarker`
(`internal/tools/task_reclaim_self.go:102`), so the two sides stay one string.

## What would have been done if it HAD reproduced

Nothing new: `a3c6efc` already removed the root mutation, so a reappearance of
`did not run: ""` on this tree would NOT be the rename-aside family. The next
suspects, in order, would be (a) a genuinely empty ledger (`total=0` for a real
reason — the seeded row missing or its claim stamp not matching
`ReclaimSelfOwnStamp`), or (b) the seed's `deliverable.worktree` not resolving to
the registered worktree. **That is a diagnosis, not a claim** — it was not
measured here because the green run made it unnecessary, and Rule 41's discipline
is to mark inference as inference.
