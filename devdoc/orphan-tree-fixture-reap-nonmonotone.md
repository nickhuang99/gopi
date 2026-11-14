<!-- doc-class: historical -->
# Orphan-tree fixture: the load-sensitive reap was a non-monotone liveness predicate, not a short grace window

**Task**: `01a10214-8360-7bbd-9816-c29534024c5c`
**Symptom under investigation**: `TestOrphanTreeDescendantIsReleasedWithItsAncestor`
intermittently failing inside a whole-package run with

```
task_finish_cleanup_orphan_tree_test.go:185: orphan tree fixture leaked: pid 1210673 survived SIGKILL
--- FAIL: TestOrphanTreeDescendantIsReleasedWithItsAncestor (0.56s)
```

while passing 15/15 in isolation.

## Verdict from measurement: the grace window was never the problem. The fixture's
cleanup judged "leaked" from a **single snapshot of a predicate that is not
monotone** across a process's exit: `internal/tools.taskzombie.go:pidAlive()`

```go
func pidAlive(pid int) bool {
    if syscall.Kill(pid, 0) != nil { return false }
    return !pidIsZombie(pid)   // state != 'Z'
}
```

can report a SIGKILLed pid **dead and then alive again**, and `collect()`'s
arithmetic is exactly the shape that trips over that:

```go
poll every 20ms; break the moment pidAlive(pid) says dead;   // loop SAW it dead
...
if pidAlive(pid) { t.Errorf("... leaked: pid %d survived SIGKILL") }   // one snapshot, later
```

## The measurement

### 1. What the failing pid actually was (the instrumented run)

The crash log itself pins the arithmetic before any new instrument: the fixture
line is written at `.492669274` and the FAIL line at `.540031306` — **48 ms
later**, while the loop that produced the error belongs to a *10 s* deadline
that was never reached. `collect()` breaks on the FIRST poll that reads the pid
dead and only then makes its final check, so a 48 ms gap means the loop had
**already observed death** and the final snapshot re-sampled the very same pid.
That is 40× the 20 ms poll interval and 3× the whole test budget left over.

The pid is a `sh` session leader (the fixture's launcher), `ppid=1` (reparented to
init — the state the ladder requires), named in the fixture's own log as
`launcher=1210673 (reparented, dead record=1210640) child=1210674`.

### 2. What the predicate does across that kill (scratch instrument, off-worktree)

Re-executing the fixture's own spawn shape (`setsid /bin/sh launcher.sh`, launcher
reparented to init, child under it) and sampling **both** channels of `pidAlive()`
on the same dying pid, at full speed, 150 kills — the recorded sample sequence:

```
state R   alive=true    the kill has not landed yet
state Z   alive=false   ← the state the poll loop breaks on
state X   alive=true    'X' is not 'Z' — the predicate says ALIVE again
kill(0)==0 with /proc/<pid>/stat already ENOENT → alive=true
          (pidIsZombie's documented best-effort contract returns false on an
           unreadable stat, leaving signal-0's verdict in place)
kill(0) finally fails (ESRCH) → alive=false     ← the pid is gone
```

Window from "the predicate first says DEAD" to "kill(0) stops succeeding",
over 144 measured kills:

```
n=144  min=0us  p50=151us  p90=212us  p99=239us  max=244us  mean=139us
84.7% of kills left a window >= 10us
```

Reproduced again **inside the test binary** (same predicate, same fixtures, no
scratch code), 8 kills:

```
round 7 pid=1452043 seq=[…/R …/R …/R …/X  no longer reaches the pid/released …]
round 4 pid=1452039 seq=[…/R …/R …/R …/R  unreadable  no longer reaches the pid/released …]
```

`unreadable` is the second face of the same window: `/proc/<pid>/stat` is already
gone while `kill(pid,0)` still returns 0.

### 3. Why "isolated passes, loaded fails" follows

The window only exists **while the exit is being reaped** (init reaping the
reparented orphan) — a killed child whose parent never waits stays a zombie and
reads dead immediately, which is why the isolated arm is stable. Under
whole-package load the scheduler can preempt the test between the loop's last
observation and the final snapshot for tens of milliseconds; landing inside a
~150 µs window then costs the assertion. **The grace window (`orphanTreeSettleWait
= 10s`) was never consumed** — the loop exits on its first dead sample — so
widening it would have changed nothing. The defect is the predicate's contract,
not the timeout.

### 4. A SECOND measured defect, found by this fix's own guard (ESRCH)

The first version of the terminal predicate accepted only `fs.ErrNotExist` for a
`/proc/<pid>/stat` read that fails. Inside this package's own suite the guard
reddened within three runs on a **correct** verdict whose wording had gone down a
different branch — verbatim:

```text
pid 1499730 was judged terminally gone on evidence that names no terminal state:
"the pid's incarnation is unobservable (read /proc/1499730/stat: no such process)"
```

`read /proc/<pid>/stat: no such process` is **ESRCH**, not ENOENT: the `open()`
resolved the path and the `read()` then found the pid released — the same
released-pid fact arriving through the other face. Two consequences, both landed:

1. `isReleasedProcError(err)` accepts **both** faces (`errors.Is(err, fs.ErrNotExist)
   || errors.Is(err, syscall.ESRCH)`), and an unrelated read failure (EACCES, EIO)
   is now **"not judged"** rather than "gone" — declaring a pid gone on no evidence
   is the same class of leap this file exists to remove.
2. The guards assert on a **typed evidence value** (`fixtureGoneEvidence`), never on
   message text. The instrument that reddened was a string match; a verdict must not
   be accepted or rejected by its wording.

Both faces are pinned by `TestReleasedProcErrorRecognisesBothFaces` (ENOENT and
ESRCH released; EACCES and EIO not), and the tally of which face was taken is
logged, not asserted, by `TestKilledPidNeverReadsGoneWhileSchedulable`.

## The fix

`internal/tools/task_finish_cleanup_fixture_reap_test.go` (new, ONE definition —
Rule 14) introduces the terminal-death predicate and the shared reap:

- `finishCleanupFixtureTerminalGone(pid) (bool, string)` — judges a pid gone only
  on evidence the kernel cannot schedule it again: `/proc/<pid>/stat` gone
  (ENOENT **or ESRCH**), state `Z` (zombie, awaiting reap — judged here, *before*
  the transient `X`), or state `X` (the measured transient death state). Any
  schedulable state is a refusal that NAMES the state it measured, and an
  unobservable stat is **not judged** rather than called gone.
- `fixtureStateIsTerminal(state)` / `fixtureStateEvidence(state)` — the decision
  table, pure, so the states are pinnable in a unit test, and the ONE switch
  behind both the boolean contract and the evidence-carrying reader.
- `fixtureGoneEvidence` — the verdict as a VALUE, so guards assert on evidence and
  never on a message: measured the hard way when a guard string-matching the
  detail reddened on a correct ESRCH verdict (§4).
- `finishCleanupFixtureReapChild(t, pid, label)` — kills and waits for that
  evidence; on timeout it reports the measured state, distinguishes a **recycled
  pid** by the kernel's start ticks (a fixture-environment failure, not a leak),
  and never asserts an opinion.

The three fixtures that used the racy arithmetic now call it:
`task_finish_cleanup_orphan_tree_test.go` (`collect`), `task_finish_cleanup_orphan_test.go`
(`collect`), `task_finish_cleanup_holder_shared_session_test.go` (`reapSessionFixture`).
Every `syscall.Kill` remains; the SIGKILL is still delivered exactly as before, so
the shape each test reproduces is unchanged. **No assertion was weakened and no
skip was added**: the same "did the process we killed actually leave?" question is
still asked and still fails the run — it is now asked of terminal evidence
instead of one snapshot of a non-monotone predicate.

### Guards (Rule 10.5)

| test | what it pins |
|---|---|
| `TestFixtureReapPredicateIsTerminalNotASnapshot` | a LIVE child is refused (mutation guard: an unconditional "gone" reddens here), and a killed child converges on terminal death |
| `TestFixtureTerminalStateTable` | the decision table: `R`/`S`/`D` not terminal; `Z` and `X` terminal |
| `TestKilledPidNeverReadsGoneWhileSchedulable` | real kills: when the predicate says gone, its EVIDENCE value names a terminal state; and it COUNTS the dying window it observes (2 samples / 12 kills) so the premise is recorded rather than assumed |
| `TestReleasedProcErrorRecognisesBothFaces` | a released pid's two /proc error faces (ENOENT, ESRCH) are accepted; an unrelated failure (EACCES, EIO) stays "not judged" |

The window observation is deliberately an observation: the window is ~150 µs, so
an assertion that demanded it every run would be the same load-sensitive
assertion this change removes.

## Mutation proof (the guards are load-bearing)

The fix's guards were falsified by breaking the thing they protect, each run
observed:

| mutation | observed result |
|---|---|
| drop the measured transient state from the decision table (`case fixtureProcDeadState:` → an unmatchable case) | `--- FAIL: TestFixtureTerminalStateTable` — `state "X": terminal=false, want true (X is the measured transient death state) — detail="state X is still a process the kernel schedules"` |
| make the table answer "gone" unconditionally (`return fixtureEvidenceZombie, …`) | `--- FAIL: TestFixtureReapPredicateIsTerminalNotASnapshot` — `a live child (pid 1504866) was judged terminally gone (mutation B: unconditional gone) — the predicate must refuse`, plus `--- FAIL: TestFixtureTerminalStateTable` |
| restore | `ok github.com/nick/gopi/internal/tools 0.519s` |

## Verification

- **Build**: `go vet ./internal/tools/` — clean (0 diagnostics); `gofmt -l` clean on
  all four files touched.

- **Ran (the failing shape — the two-package budget)**: `make test-budget PKGS='./internal/tools ./cmd/agent'`
  — the budget the flake was observed in (the original failure is stream
  `run-1791035621-1185089.json`, `internal/tools` failing at
  `task_finish_cleanup_orphan_tree_test.go:185`).

  Two runs, and the FIRST one is reported with its honest scope: stream
  `run-1791038115-1452873.json` went green
  (`stamp: … budget=25m elapsed=492s load_start=1.67 1.66 2.10 load_end=2.24 2.30 2.34 cores=4 exit=0 outcome=pass`)
  — but it had **already compiled** the package when the ESRCH fix landed 74 s
  into it, so it covers the terminal predicate and the three rewired fixtures and
  NOT §4's branch. The confirmation run on the committed tree is
  `run-1791038621-1501928.json`:

  ```text
  stream run-1791038621-1501928.json, host load 2.02 2.25 2.32, cores 4:
    package github.com/nick/gopi/internal/tools ................ PASS
      TestOrphanTreeDescendantIsReleasedWithItsAncestor ......... pass (0.29s)
    package github.com/nick/gopi/cmd/agent ..................... FAIL, pre-existing and unrelated:
      TestWorkerDeliveryRecyclesDespiteAStaleSiblingRow (cli_tasks_worker_recycle_e2e_test.go:540)
      "the production backfill seam must stamp the row's ground: worktree=\"\" branch=\"\""
  ```

  **The cmd/agent failure is not this change's.** Scope proves it before any
  rerun does: `git diff master...HEAD --name-only` lists only
  `internal/tools/*_test.go` + two devdoc files — `git diff master...HEAD --name-only | grep -c '^cmd/'` = **0**,
  and no non-test `internal/tools` code changed either, so `cmd/agent`'s test
  binary is byte-identical with and without this branch. It also reproduces
  standalone — `go test ./cmd/agent/ -count=1 -run TestWorkerDeliveryRecyclesDespiteAStaleSiblingRow`
  → `--- FAIL … 0.07s` with the same assertion — and it is a REAL assertion about
  the ground-stamp refusal (`[task-notify] task t-worker-stale: creation-time ground
  stamp REFUSED — …/001/wt is already named by row t-stale-sibling`), i.e. a live
  defect in the worker-recycle path, not a fixture artifact. That it PASSED in
  `run-1791038115-1452873.json` on identical code three minutes earlier makes it a
  flake in its own right — a separate investigation, filed rather than folded in here.

- **Ran (the same predicate, isolated and repeated)**: `go test ./internal/tools/ -count=1 -run
  'TestFixtureReapPredicateIsTerminalNotASnapshot|TestFixtureTerminalStateTable|TestKilledPidNeverReadsGoneWhileSchedulable|TestReleasedProcErrorRecognisesBothFaces|TestOrphanTreeDescendantIsReleasedWithItsAncestor'`
  → `ok` on 5 consecutive runs AFTER the ESRCH fix; the pre-fix form reddened on
  round 2 of 3 (see §4) — that red is what found the second defect.

- **Commit**: `d8ce92ac` — the fix, its guards, the rewired fixtures, the report
  and its `DOC-INDEX.tsv` row (`gopi doccheck` → "the devdoc corpus is consistent").
  The `internal/tools` half of the failing shape is green on the committed tree:
  the run above compiled AFTER the last source edit to those files.

## Evidence (self-filed)

```text
$ git show master:internal/tools/task_finish_cleanup_orphan_tree_test.go | grep -n 'survived SIGKILL'
185:  t.Errorf("orphan tree fixture leaked: pid %d survived SIGKILL", pid)

$ grep 'Action":"fail"' run-1791035621-1185089.json (internal/tools)
"Action":"fail","Package":"github.com/nick/gopi/internal/tools","Test":"TestOrphanTreeDescendantIsReleasedWithItsAncestor","Elapsed":0.56
  output: task_finish_cleanup_orphan_tree_test.go:185: orphan tree fixture leaked: pid 1210673 survived SIGKILL
  fixture log 21:56:42.492669274 → FAIL 21:56:42.540031306   (48ms later; the loop's deadline is 10s)

$ scratch instrument, 144 measured kills (both pidAlive channels, full speed)
phantom window w (predicate first says DEAD -> kill(0) finally errors):
  n=144 min=0us p50=151us p90=212us p99=239us max=244us mean=139us
  84.7% of kills left a window >= 10us
sample sequence: R(alive) → Z(dead) → X(alive) → kill(0)==0 with /proc gone (alive) → ESRCH(dead)

$ go test ./internal/tools/ -count=1 -run TestKilledPidNeverReadsGoneWhileSchedulable -v
sampled the dying window (signal 0 still reaching a pid whose /proc entry was already gone) 2 times across 12 kills
  … and on another round: 0 times across 12 kills — the observation is logged, never asserted

$ §4's ESRCH face, verbatim from the guard that reddened on a CORRECT verdict (round 2 of 3)
pid 1499730 was judged terminally gone on evidence that names no terminal state:
"the pid's incarnation is unobservable (read /proc/1499730/stat: no such process)"

$ go test ./internal/tools/ -count=1 -run 'TestFixtureReapPredicate…|TestFixtureTerminalStateTable|TestKilledPid…|TestReleasedProcError…|TestOrphanTreeDescendant…'
ok  github.com/nick/gopi/internal/tools  0.516s / 0.518s / 0.878s / 0.518s / 0.501s   (5 consecutive runs, post-fix)

$ go test ./cmd/agent/ -count=1 -run TestWorkerDeliveryRecyclesDespiteAStaleSiblingRow
--- FAIL: TestWorkerDeliveryRecyclesDespiteAStaleSiblingRow (0.07s)   ← pre-existing, unrelated (0 cmd/ files touched)
```

## Not Done (honest)

- **The `cmd/agent` half of the two-package run is RED, and not on this branch.**
  `TestWorkerDeliveryRecyclesDespiteAStaleSiblingRow` fails both inside the budget
  run and standalone, and asserts something real (the production backfill seam did
  not stamp the row's ground; `worktree="" branch=""`). This branch touches ZERO
  files under `cmd/`, so the two facts cannot be related — but I did not fix it
  either, and the failing shape is therefore green only for `internal/tools`
  (`internal/tools` PASS, its target test pass). It is left as an unfixed,
  independently reproducible flake: fixing it is a different mechanism and would
  not fit this session's budget.
- **No synthetic load was used to force the flake** (Rule 27 bans it): the
  evidence is the observed window width plus the crash log's own arithmetic, and
  the falsification is the mutation proof, not a stress script.
- The window's *probability* under whole-package load was not measured directly —
  the flake appeared in 1 of the 42 recorded `internal/tools` budget runs kept in
  the queue. What is measured is the mechanism (window width, non-monotone
  sample sequence) and that the new predicate cannot express the contradiction.
- `pidAlive()` itself was left alone: it answers "is this pid alive NOW" for
  task/ledger decisions where a live-looking dying process SHOULD keep a worktree
  held. The defect was the fixtures' leap from "alive now" to "was never reaped".
