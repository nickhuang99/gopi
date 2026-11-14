<!-- doc-class: historical -->
# A budget run now says when a neighbour is inside its scope

**Status:** landed on `task/01a142e489ab7a0380ba70bb591c9bce` (bug row `01a142e4`).

## The measured defect

Two `scripts/test-budget.sh` runs were alive on this host at once. pid 2885273
asked for four packages, a neighbour session for two, and the two sets
intersected in two packages. Both runs wrote their start records, both started
`go test -count=1 -json`, and **neither printed a word**. Every red produced
inside that window was then quotable as one run's own verdict, which it was not
— the repository's own rule text has already named the shape an observed
failure ("Two go test sets in one run"), and the run that suffered from it was
never told.

The window is not exotic: the artifact directory on this host already holds
1628 `run-*-start.txt` records, and nothing ever collects them.

## What was added

`scripts/lib/budget-overlap.sh` — the predicate, and `report_concurrent_overlap`
in `scripts/test-budget.sh` — the report, called from `report_run_header`, i.e.
BEFORE the tested-binary build and the suite (the two long silences).

The input is what already exists: the `run-*-start.txt` records every run writes
(`start_iso=` / `pid=` / `pkg=`). **No new artifact, no lock, no daemon, no
registry** (Rule 35) — the reporter only reads.

Three properties carry the design:

- **Liveness is the process table, never the ppid** (Rule 24). `/proc/<pid>/` is
  the instrument. Pids are RECYCLED, so the number alone attributes nothing: a
  record CLAIMS a pid, and a readable command line that does not mention the
  entry point is a stranger wearing that number, not a peer. An UNREADABLE
  command line is an honest unknown and is reported (Rule 15) — a neighbour
  neither confirmed nor excluded must not be dropped silently.
- **The intersection goes through the SAME predicate as the run's own scope**
  (`gopi_scope_packages`, Rule 14), so `./x/`, `./x` and a bare `x` are one
  package and neither side resolves paths itself. The comparison is between
  RESOLVED import paths; the row prints the REQUESTED spelling, because that is
  the string the operator typed.
- **It warns and never refuses.** Whichever run started first must not win: a run
  that refused to start on seeing a neighbour would let a neighbour's start
  decide a third party's verdict. The run always proceeds; what it may not do is
  proceed silently — because the cost of the silence is paid later, by whoever
  reads the red.

The header line a reader greps for:

```
!!!!!!!!!!!!!!!! CONCURRENT BUDGET RUN OVERLAPS THIS SCOPE !!!!!!!!!!!!!!!!
1 of 1 other test-budget run(s) alive on this host asked for packages THIS RUN ALSO ASKS FOR:
  1649701
      its packages:  ./internal/docindex/
      shared:        github.com/nick/gopi/internal/docindex
      started:       2026-10-16T19:32:38+08:00
  this run's packages: ./internal/docindex/
This run STARTS ANYWAY: a later run never cancels an earlier one, stopping a neighbour is not this run's call.
a red inside the overlap window does NOT belong to any one run's change (重叠窗口内的红不作代码归属) — such a red is a verdict only when it is REPRODUCED ALONE, after the peer finishes.
to settle a red seen here: wait for those pid(s) to exit, then re-run this scope alone.
```

A live neighbour on DISJOINT packages is **not** silent either — it gets a
one-line `note:` with its pid/packages and an explicit "no overlap caveat
applies". The two quiet states stay distinguishable: no row at all = nobody was
alive (a run genuinely alone), rows with an empty intersection = neighbours were
running on other packages. That distinction is what makes "we looked" different
from "we never looked" without a second artifact.

## Two traps worth recording

**The reader that never ran and the header that read as "no neighbour found".**
`report_concurrent_overlap` is called as the body of a NEGATED `if` in
`report_run_header`, never bare. Under `set -u` each `local x="$(failing)"` is
fatal, so an unguarded call would abort every budget run on its own header — and
a call whose result is ignored makes "the check could not inspect the host"
indistinguishable from "no neighbour found", which is precisely the silent
failure the block exists to remove (Rule 15).

**`budgetFunctionBody` closes a function at the first line whose trimmed text is
`}`.** The gate's shell reader is deliberately simple (a brace counter would be
a second implementation of the shell), so a pretty-printed awk program inside
the reporter — closing brace on its own line — cut the body short and the two
caveat sentences fell outside it. Measured while writing this: the red proof
reported "the concurrency reporter never prints $overlap_attribution_note"
against a script that plainly did. The awk programs in the reporter are
therefore written one-line-per-fact, and the constraint is stated in the code
where the next editor will hit it.

## The gate

`internal/testutil/testbudget_overlap_gate_test.go`:

- `TestBudgetConcurrentOverlapIsReportedFromTheHeader` — the script declares the
  records glob, sources the shared predicate, defines the reporter and calls it
  GUARDED from the header.
- `TestBudgetOverlapGatePredicateIsFalsifiable` — the red proof: the SAME
  predicate is fed four mutations (call dropped / call made bare / marker not
  printed / caveat sentence removed) and must report each.
- `TestBudgetOverlapPredicateNamesOnlyLiveIntersectingPeers` — the dynamic half:
  synthetic records through the REAL library in `bash`. A live intersecting peer
  is reported with pid + its packages + the shared package; a live disjoint peer
  is reported with an EMPTY intersection; a dead pid and this run's own `$$` are
  not reported at all.
- `TestBudgetRunWarnsWhenANeighbourOverlapsItsScope` — the epic's acceptance,
  executed: the real script beside a live neighbour, header read through a pipe.

The peer stubs are bounded `sleep`s (Rule 27: no load is generated), and the
dead-pid arm uses pid `0`, which is never a process — deterministic instead of
racing another pid's death.
