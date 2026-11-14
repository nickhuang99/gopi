<!-- doc-class: normative -->
# A fixture's wait for its `gopi` child is judged by PROGRESS, not by the wall clock

## The incident (measured 2026-10-31, four whole-package runs)

`TestTaskRunSeamFallsBackToSkeleton` failed once in four runs of
`go test ./cmd/agent/ -count=1 -timeout 45m`:

```
--- FAIL: TestTaskRunSeamFallsBackToSkeleton (180.04s)
    cli_tasks_report_seam_test.go:178: gopi [worker 01a11500-0000-7000-8000-000000000024] timed out after 3m0s
```

The failing run was the one whose loadavg peaked (13.81 at start, 8.33 at end, on
a 32-core host). The other three passed with `ok` in 744s / 662s / 642s, and the
same test alone on an idle host passed in **9.271s** — twenty times inside its
budget.

## Where the 180s came from

`internal/testutil/run.go` declared

```go
subprocessTimeout = 180 * time.Second
```

and `RunGopi` used it as `exec.CommandContext(ctx, bin, args…)` with
`context.WithTimeout(ctx, subprocessTimeout)`. The fixture's own `t.Fatalf`
rendered `180s` as Go's `time.Duration` string, `3m0s` — that is the whole
provenance of the number in the failure line.

**The criterion was elapsed time and nothing else.** Whatever the child was
doing, 180 seconds of wall clock ended it. Two different situations therefore
got one verdict:

| situation | what it needs | what the old wait did |
|---|---|---|
| the child deadlocked | FAIL, saying "stuck" | FAIL at 180s, saying "timed out" |
| the child is working on a host with loadavg 13.8 | keep waiting | FAIL at 180s, saying "timed out" |

The failure's own stack dump shows the second: the child was `state=S`. `S` is
the kernel's letter for an **ordinary wait**, and it is what the kernel reports
for *both* rows of that table — a process queued behind other CPU work and one
deadlocked against a lock it will never get are both `S`. **The state letter
cannot distinguish "stuck" from "slow"**, so no amount of reading it fixes the
criterion; the criterion itself has to change.

## The fix

`internal/testutil/runprogress.go` adds a progress criterion, and `RunGopi` uses
it in place of the context deadline:

- **the measured quantity** is the CPU time of the child's whole process TREE —
  `/proc/<pid>/stat` fields 14+15 (utime+stime), summed over the root and every
  descendant reachable through each process's own `children` file. It is a
  **counter**, so two readings separated by an interval answer "was anything in
  this tree scheduled in between?" — the question the wall clock cannot answer.
  A child that is merely queued still advances its own ticks; a deadlocked tree
  advances nothing, ever.
- **three exits, all distinct**: `RunWaitLeft` (the child exited — the normal
  path), `RunWaitStalled` (nothing scheduled anywhere in the tree for
  `runNoProgressStall` = 30s → a HANG, and it is decided in 30s instead of
  burning the whole budget), `RunWaitCeiling` (the tree kept moving and
  `subprocessTimeout` arrived → the run never STOPS, which is a different defect
  and says so).
- **the ceiling is UNCHANGED** at `subprocessTimeout`. This is not a larger
  timeout: a run that does not terminate still fails, exactly when it used to.
- **an unreadable `/proc` is not a zero reading** (`RunProgressSnapshot.Readable`):
  "cannot measure" keeps the wait going to the ceiling rather than being read as
  "scheduled nothing", which would turn a procfs hiccup into a hang verdict
  (Rule 15).
- **`Live` is part of the signal** so a child that FORKS counts as progress — the
  run moved on to its next step — while a tree that merely shrinks is not.
- the child is signalled by the pid handle this fixture holds (Rule 42), and a
  child that refuses to die within `runKillGrace` is reported rather than dropped.

`runTreeState` renders the snapshot into the failure message, so the verdict
quotes the numbers it was taken on.

## Why not simply raise the number

Raising `subprocessTimeout` would move the same defect further out on a host
with more load — the wall clock would still be the criterion, and a real hang
would still cost the entire budget to detect. The rule this obeys is Rule 5's:
**do not weaken the assertion.** A hang still fails (and now fails sooner, with a
message that says "no progress" rather than "timed out"); a slow host no longer
fails at all.

## The regression guard (`internal/testutil/runprogress_test.go`)

Six arms, each failing for exactly one reason, with the criterion driven through
an injected progress reader so nothing waits out a real 30s window:

1. keeps moving, exits late → must end `RunWaitLeft` (the incident, reduced);
2. nothing moves → must end `RunWaitStalled`;
3. keeps moving past the ceiling → must end `RunWaitCeiling`;
4. unreadable tree → must NOT be read as a stall;
5. a **real** idle `sleep` spawned through the reaping helper → must end
   `RunWaitStalled` (the kernel is the judge, not an injected reading);
6. a synthetic `/proc` tree → the tick columns are SUMMED over the tree and the
   walk follows the `children` file.

**Red proofs, each restored and verified by content hash:**

```
A) runProgressAdvanced always false (the old wall-clock criterion):
   --- FAIL: TestRunProgressIsNotCondemnedByElapsedTime  — "ended the wait as stalled after 62ms, want RunWaitLeft"
   --- FAIL: TestRunProgressEndsCeilingWhenItNeverStops  — "ended the wait as stalled after 62ms, want RunWaitCeiling"
B) the stall branch removed (a hang can no longer fail):
   --- FAIL: TestRunProgressEndsStalledWhenNothingMoves — "ended the wait as ceiling after 400ms, want RunWaitStalled"
   --- FAIL: TestRunProgressDetectsARealStuckChild      — "a REAL idle process (pid 3423499) ended the wait as ceiling after 5.001s"
```

`sha256sum internal/testutil/runprogress.go` before and after both mutations:
`34fc840a0ad61688b003dd20707b7164c93388b7af24b1e94fed35f58d18fd04` — identical,
so the restore is a content measurement and not an index-relative diff (Rule 5).

## Rule 27

Nothing here creates load. Arms 1–4 are pure functions of injected readings, arm
5 spawns one **idle** child (a `sleep` is scheduled for no CPU time at all, which
is precisely what makes it a hang specimen), arm 6 reads a fixture directory.
The evidence for the real incident is a whole-package run that happened at load
13.8 on its own.
