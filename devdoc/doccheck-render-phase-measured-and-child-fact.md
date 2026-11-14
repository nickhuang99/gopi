<!-- doc-class: historical -->
# doccheck's render phase, re-measured: 242s (not ~190s), and a tick that can tell "slow" from "runaway"

**Task** `01a1447c24e17e1f853210b0c6fc2c45` · worktree `/home/user/workspace/gopi-01a1447c24e17e1f853210b0c6fc2c45`
(branch `task/01a1447c24e17e1f853210b0c6fc2c45`, base `3e07a4052`) · bug row `01a14317a5d5` · host: hosta (8 cores)

## 1. The measurement: the phase is 242s, the comment said ~190s

Command, verbatim (`artifacts/run-doccheck.sh`, run as a background job — the phase
cannot be called from a foreground shell, the calling net kills a silent command at 120s):

```bash
cd /home/user/workspace/gopi-01a1447c24e17e1f853210b0c6fc2c45
CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/
./bin/gopi tool doccheck 2>&1 | while IFS= read -r l; do printf '%s\t%s\n' "$(date +%s.%N)" "$l"; done
```

Raw result (every line stamped as it ARRIVED, `artifacts/doccheck-before.tsv`):

```
=== corpus paths (arm) ===
2592
PIPESTATUS0=1
TEND=1792154813.543773679
lines=81 first=0.00s last=242.00s maxgap=15.01s from=1.51s to=242.00s
=== ticks ===
16
(240s of progress
```

| | comment's value (2026-10-14) | re-measured (2026-10-16) |
|---|---|---|
| render phase | ~190s (186.4s in one arm) | **242.0s** |
| corpus paths | 2507 → 2524 | **2592** (+2.7% over the 2524 arm) |
| liveness ticks | (not recorded then) | **16**, max line gap **15.01s** |
| exit | 1 (stored-cell drift) | 1 (**unchanged**) |

The 1.27× growth is **not** explained by the corpus (+2.7%): the ratio is far off, and
this run **does not claim to know** why. What it could measure cheaply is where the cost
lives, and the answer is *not* the corpus size:

```
n=1     wall 94.52s   (1 rendered row)
n=10    wall 86.63s
n=100   wall 95.37s
n=500   wall 115.54s
```

`n=1` costs 94.5s and `n=500` costs 115.5s — a 500× path increase costs 1.22× wall. The
classifier's fixed start-up dominates, so **no corpus-sized argument can make this phase
return sooner**, and none is offered.

## 2. The comment: what changed and why

`DocclassDriftProgressIntervalSeconds`'s comment carried a *promise about runtime*
("this phase runs ~190s on this repo's own corpus") that is now false by 1.27×. The fix is
**not** to substitute 242 (that number is this host's and this corpus's, and the next
reader would find it false too). What the constant actually guarantees is narrower and
stays true however long the phase runs:

> the longest silence this phase can produce is the interval — 15s — so no caller's
> watchdog can fire on it.

So the comment now states the **measured history** (190s then, 242.0s at 2592 paths on
2026-10-16, 16 ticks, maxgap 15.01s) and states the budget as a **ratio, not a runtime**.
The value is unchanged: 15s is a tenth of the 120s window, eight ticks fit inside one
window, and the interval test pins that floor (≥4).

## 3. The tick: a second fact that separates "slow" from "runaway"

Elapsed time alone could not answer the question the bug row was really asking. It grows
identically whether the classifier is computing, blocked, or **already dead behind an
outstanding `cmd.Wait()`**, and it grows without bound — which is exactly how 345s was
read as a hang.

Two facts settle it, and both are readable from `/proc` of a process this one is the
**parent** of (no privilege, no scrape-by-name, nothing persistent — Rule 35):

- `procname.CPUTicks` — the child's user+system CPU ticks (stat fields 14/15). A
  **counter**: two readings answer "did it compute in between?", and a flat pair does not.
- `procname.State` — the run state letter at that instant (`R` while on a CPU).
- `procname.Children` — the process's own direct children, so the sample covers the
  phase's tree (gopi → `python3` → `git …`) rather than one pid.

The tick now reads, beside the elapsed count:

```
doccheck: the corpus's own classifier is still running (15s of progress measured on it so far;
1 live process(es) in its tree, 1 on the CPU at this instant, 14.9s of CPU used by the tree)
— the stored-cell answer follows when it returns
```

**The abstentions are as important as the numbers** (Rule 15): a sample that could not be
read prints `no process facts could be read (<reason>)`, and a measured-but-empty tree
prints `no live process behind this wait — the classifier's tree is empty while the wait
has not returned`. Neither renders as `0 live process(es)`, because "nothing could be
read" and "nothing is running" are opposite answers and only one of them is evidence.

### Why a percentage is still not there

Measured, and it is the reason the tick reports CPU instead of progress
(`artifacts/first-byte.tsv`; 500 paths handed to the classifier, rows stamped on arrival):

```
total=130.95s
first_row_at=129.33s
last_row_at=130.95s rows=500
```

The classifier renders the whole corpus in ONE pass and writes its rows only when it is
done — 129.33s of 130.95s before the **first** row. There is no progress to relay at any
moment before the last, so any percentage/ETA would be fabricated. CPU time is
**liveness**, never progress, and the code says so where the tick is rendered.

## 4. What changed, file by file

- `internal/procname/procname.go` — the process-fact leaf gains `StatFieldUTime` /
  `StatFieldStime` / `ClockTicksPerSecond` (the single definition; `internal/tools`
  keeps its alias), `StateRunning`, `TaskName`/`ChildrenName`, and three readers:
  `State`, `CPUTicks` + `CPUTicksOfLine`, `Children`. All parse through the existing
  `StatField` (last-`)` rule), so no second `/proc` parser appears.
- `internal/docindex/childwatch.go` (new) — `sampleChildTree(pid)`: a bounded
  (depth 6, 256 nodes) walk of the child's tree collecting live count, running count and
  total CPU ticks; `docclassDriftChildFact` renders the clause; the abstention and
  no-live sentences live here with one definition each.
- `internal/docindex/docclass_drift.go` — `cmd.Run()` → `cmd.Start()` + `cmd.Wait()`
  (the pid must be known to the first tick); the tick renders elapsed **and** the child
  fact; the interval's comment now carries the measured history and a ratio-shaped budget.
- `internal/docindex/childwatch_test.go` (new) — 6 arms, below.

## 5. Regression tests

`internal/docindex/childwatch_test.go`:

1. `TestSampleChildTreeMeasuresALiveChild` — a real child is read (live ≥1) and the CPU
   counter never goes backwards (the property the two-reading comparison rests on).
2. `TestSampleChildTreeSeparatesGoneFromUnreadable` — an unknown child is a reason,
   never `0/0`; the measured-but-empty tree renders as its own sentence.
3. `TestDocclassDriftChildFactCarriesNoInventedNumber` — no `%`/ETA/`remaining`/… and the
   measured clause is present.
4. `TestDocclassDriftChildFactReportsATruncatedWalkAsAFloor` — a bounded walk says
   "at least".
5. `TestDocclassDriftRender_TickCarriesTheChildFact` — end-to-end: a REAL tick emitted
   while the stub classifier is still working carries the measured clause.
6. `TestProcnameExposesTheChildChannels` — the readers themselves (fields 14/15 on a
   line whose comm contains `)`), and an unknown pid is a refusal, not a zero.

`internal/docindex/docclass_drift_progress_test.go` — the existing
`ProgressLineCarriesNoInventedNumber` arm now asserts the **rendered** line carries the
child fact too (the template has two verbs now).

## 6. Honest gaps

- **Why 242s and not 190s is unexplained.** Not the corpus (+2.7% against 1.27×); the
  per-n measurement above points at the classifier's fixed cost, but the *change* since
  2026-10-14 was not isolated (host? python? the script itself?). Reporting it as a
  mystery beats inventing a cause — the comment now records both readings instead of
  promising one.
- The old 190s figure was measured on peerbox; today's 242.0s is on **hosta**. The two
  hosts' corpora and hardware differ, so part of the 1.27× may be the host. This is also
  why the comment no longer states a runtime as a budget.
- `Children` reads the main thread's children file only; a process spawning from a
  non-main thread would be under-counted (stated at the reader).
