<!-- doc-class: normative -->
# The serve-lock diagnosis fixture's readiness window: derived, not typed

**Task**: epic `serve-lock-diagnosis-budget`. **File changed**:
`cmd/agent/serve_lock_diagnosis_liveness_test.go` (+ its unit test in
`cmd/agent/serve_lock_diagnosis_budget_test.go`, + one line of `TestMain`).

## The defect

`diagnosisWaitMarker` waited for the re-exec'd helper's readiness marker with a
**FIXED** window, `diagnosisPollTicks × diagnosisPollTick = 100 × 25ms = 2.50s`,
and on running out it said only that the helper "never published its pids" —
naming its own budget and nothing about the child. Two independent defects hid in
that sentence:

1. **The window sat below its own fixture's floor.** The helper is *this test
   binary*, re-exec'd, so it pays the suite's whole `TestMain` (testutil's pins +
   the close-out sweeps) before its body runs. The sibling fixture of the same
   suite measured that cost at **5.02s** (`agent.test -test.run='^$'`, commit
   `99e2eacac`). A 2.50s window is a race against the binary's own startup, not a
   test of the diagnostic.
2. **A child that DIED and a child that was SLOW were the same string.** A broken
   fixture (a helper that dies taking the flock) would have been "fixed" by
   widening the window — the diagnosis said nothing that could tell the two apart.

## The measurement (this host, 2026-10-12)

Host **host-b**, 8 cores, load average **7.7–12.8** during the acceptance
runs (`uptime` at 14:20 through 14:34). Rule 27: no load was generated — only
this fixture's own children were measured.

| what | samples | result |
|---|---|---|
| `agent.test -test.run='^$'` (suite startup, n=5) | 5 | 0.77, 0.80, 0.82, 0.83, 0.93 s |
| helper readiness marker after spawn (n=15) | 15 | 0.781–1.049 s, median **0.863 s** |
| helper readiness inside the suite, reported by the handshake | 2 | 1.309 s, 1.510 s |
| the sibling fixture's floor (commit `99e2eacac`, other host) | — | **5.02 s** |

**The two hosts disagree by ~6×, and that is the point.** This host's fixture
floor is ~0.8s (load 8–12), while the suite's own sibling fixture measured 5.02s
elsewhere. A floor derived from the *fastest* host is the same defect with a new
number: a window must cover the slowest host the suite runs on. So the named
floor carries the **worst measurement** (5.02s), and this run's own startup may
only *grow* it — never undercut it:

```
diagnosisReadyFloor = diagnosisReadyFloorMultiple × diagnosisMeasuredSuiteStartupFloor
                    = 8 × 5.02s = 40.16s
diagnosisPollWindow = 2 × 5.02s = 10.04s   (the rig's own polls; also above the floor)
```

The pre-fix 2.50s window is below both — that is the defect, in one number.

## The fix

1. **The window is DERIVED, as a pure function**
   (`diagnosisReadyBudget(deadline, elapsed, measuredStartup, floor)`): a named
   floor, raised by `diagnosisReadySampleMultiple` (3) × a startup an earlier
   handshake in the same run MEASURED, always capped by what is left of the run's
   own `-test.timeout` minus a reserve — so *this* handshake fails naming the
   child instead of Go's `-test.timeout` panicking over the whole binary. Past the
   deadline it collapses to the poll tick rather than going negative (a negative
   window would skip the loop and mislabel a live child as dead).
2. **The helper's death is an EVENT**: one `cmd.Wait()` goroutine per spawn
   feeding `h.exited`, `select`ed inside the poll loop. A child that dies is
   reported **immediately** with its own exit status and its own captured
   (bounded, drained) stderr; the timeout verdict reports the child's state
   (`still RUNNING` vs `EXITED (…) as the window ran out`), so "slow" and
   "crashed" can never be read as the same thing again.
3. **The poll `return`s after `Fatalf`** in the exit arm. `*testing.T.Fatalf` ends
   the goroutine, but a *recording* sink does not: without the return the loop
   keeps polling and the later timeout verdict overwrites the correct exit
   message. This is measured, not theoretical — see the red proof below.
4. **The floor's inputs are the run's own measurements**: `diagnosisTestStart` is
   stamped in `TestMain` before the pins, and a run that never stamped it gets the
   named floor rather than a silent zero (Rule 15a).
5. **The three rig polls keep polling conditions the rig itself produces** (the
   helper's death, the kernel's lock row, the rig's own inode replacement) — they
   changed only from a tick count to `diagnosisPollWindow`, which is likewise not
   typed below the measured floor.

**The assertions are unchanged**: the refusal must still carry
`lock-stray-holder:` (the stray holder's pid *and* its liveness) and
`lock-kernel-records:` (joined on the inode the acquired **descriptor** was
refused on, naming the dead owner the kernel still records).

## Verification

- **Build**: `go build ./...` → OK; `go vet ./cmd/agent/` → OK; `gofmt -l` clean.
- **Tests**: `make test-budget PKGS='./cmd/agent'` scoped to the touched file's
  package, plus the three new unit cases:
  `go test ./cmd/agent/ -run 'TestServeLockDiagnosisReadyBudget|TestServeLockDiagnosisHandshake|TestServeLockDiagnosisStderr' -count=1` → `ok 4.025s`.
- **Ran** (Rule 31, the changed surface, verbatim):
  `go test ./cmd/agent/ -run 'TestServeLockRefusalDiagnosis' -count=3`
  → `ok github.com/the operator/gopi/cmd/agent 8.003s` at load **7.78 / 8.99 / 10.05**,
  then `ok … 13.763s` at load **9.57 / 12.59 / 11.70**, with earlier identical
  runs green at load **12.81** and **12.24** (the latter under the pre-fix window,
  see red proof 2). `uptime` before and after each is recorded in the session's
  `acceptance-load.txt`. Acceptance asked for load > 5: met, on five runs.
- **The three new unit cases**, repeatedly, at load 9.6–22.8:
  `go test ./cmd/agent/ -run 'TestServeLockDiagnosis' -count=1` → green 4/4 after
  the wording-discriminator change (see the flake note in red proof 1).
- **Result**: `TestServeLockRefusalDiagnosisNamesTheStrayHolderAndItsLiveness`,
  `TestServeLockRefusalDiagnosisReadsTheDescriptorNotThePath` and
  `TestServeLockLivenessNoteReportsADeadHolder` all PASS.

### Red proof 1 — the death event is load-bearing

Mutating the death event —
`go func() { h.exited <- cmd.Wait() }()` → `go func() { _ = cmd.Wait() }()` —
makes `TestServeLockDiagnosisHandshake_ReportsAHelperThatDiesBeforeReadiness` go
**RED on the exit wording**, deterministically: **3/3 red** on the mutation
(42.2s / 42.2s / 48.1s, load 22.8 at the time) and 3/3 green once restored, at
load 9.6–22.8:

```
--- FAIL: TestServeLockDiagnosisHandshake_ReportsAHelperThatDiesBeforeReadiness (42.17s)
    the failure must name the helper's exit status 66, got:
        the lock-holder helper did not publish its pids at …/helper.pids within 43.1s
        (pid …, started 43.1s ago): the helper is still RUNNING.
    a dead helper must be reported through the EXIT arm, got: … the helper is still RUNNING.
    the window ran out over a child that had already died — the death event is not wired, got: …
```

Note what the mutant proves the *pre-fix* machinery could not say: the helper's
own stderr was captured, and its exit status was available — but with the death
event removed the parent blames the host ("still RUNNING") anyway. A mutation
that merely widened the window would have proven nothing.

**A flake in this fix's own first draft, and what it taught (measured).** The
regression case originally asserted "reported at once" as
`elapsed < diagnosisPollWindow` (10.04s). Under load 14.9 it went **red on a
correct report**: a helper that dies in milliseconds still pays the suite's
startup before its body runs, and that cost measured **0.8s idle and >10s at
load 17.5** — so the assertion was a wall clock on the CHILD, i.e. the very
defect this fix removes, one level down (exactly the trap `memory #6111` names:
`0.00s` vs the host). The fix is to drop the clock and assert the
**discriminator** instead: the EXIT arm's wording (`EXITED before publishing`)
versus the timeout arm's (`did not publish its pids … within <budget>`). The
elapsed time is now reported with `t.Logf` as evidence, never asserted. 4/4 green
after the change at load 17.5; 3/3 red under the mutation.

### Red proof 2 — A/B on the window itself (honest negative)

Substituting the pre-fix window (`diagnosisReadyFloor = 100 × diagnosisPollTick`)
into the fixed file and running the acceptance command **6 times** at load
11.0–12.8 produced **6 passes on this host** (51.9s then 17.4s wall). The pre-fix
failure is probabilistic and, per the two measurements above, this host's fixture
floor (~0.8s) is ~6× faster than the host where 2 of 3 runs failed at a 2.50s
window. So the A/B does **not** reproduce the red here, and it is recorded as the
negative it is rather than dressed up: the arithmetic (`2.50s < 0.8s×3` never
held on this host, and `2.50s < 5.02s` does) plus the two measurement tables are
the evidence for the defect, and the green acceptance run at load > 5 is the
evidence that it is closed. The A/B experiment was reverted before commit.

## Reverting this fix

The single load-bearing edit is the death event (red proof 1). Reverting the
budget to a fixed constant narrower than
`diagnosisMeasuredSuiteStartupFloor × diagnosisReadyFloorMultiple` re-opens the
defect; the unit test asserts both that the derived floor is above the measured
floor and that the pre-fix window is below it, so the regression cannot land
silently.

## Rule 35 note

No dependency was created: no new process, file, config key, cron entry or
artifact. The change is entirely inside the test that was racing.
