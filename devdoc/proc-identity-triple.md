<!-- doc-class: historical -->
<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

# The liveness identity triple, completed: pid + starttime + cmdline

Branch `fix/proc-triple` (worktree `~/workspace/gopi-proc-triple`), based on master
`7b5d427a`. Deliverable branch, **not merged**.

## The gap this closes

The worker count was fixed on 2026-09-12: `scripts/gopi-worker-up.sh` stopped
counting by NAME (`pgrep -x gopi | wc -l`, which answered 13 while 5 workers ran)
and started counting by argv SHAPE — comm (`/proc/<pid>/comm`) **and** cmdline
(argv[1] == the worker verb). That removed the inflated number, but the identity it
computed was still 2 of the 3 factors:

* a **pid** is a NUMBER the kernel recycles; and
* **comm + cmdline** describe a SHAPE that any process can wear — a supervisor that
  restarts `gopi worker` produces byte-identical comm and argv under the same pid
  slot.

So a recycled pid whose comm and cmdline happen to match still read as "the same
live worker". The missing factor is **starttime** (`/proc/<pid>/stat` field 22,
clock ticks since boot): the kernel never reuses a `(pid, starttime)` pair, so the
ticks ARE the incarnation.

## What each factor defends against (stated in `internal/procname` too)

| factor | defends against |
|---|---|
| **pid** | nothing on its own — it only says WHICH slot is meant. It is recycled within seconds on a busy host, and it outlives the process (a zombie keeps its pid until reaped; `kill(pid, 0)` succeeds for a corpse). |
| **starttime** (stat field 22) | **PID REUSE** — the incarnation. Ticks differ ⇒ the number was reused ⇒ the process meant is gone. This is what comm+cmdline cannot substitute for. |
| **cmdline** (argv) | **ROLE CONFUSION** — a worker vs the task-serve daemon / merger / a one-shot `task run` child that re-execs the SAME binary. Weakest alone: a process sets its own argv, and a wrapper shares its target's. |

The verdict is three-valued — `alive` / `recycled` / `unknown` — never a bool.
Unreadable procfs is `unknown`, never a fabricated death *or* a fabricated life
(Rule 15).

## 1. `internal/procname` — the ONE definition site, extended

New (all in the leaf package, `internal/procname/procname.go`):

* `StatField(statLine string, field int) (string, bool)` — extracts one field of
  `/proc/<pid>/stat` **by counting tokens from the LAST `)`**. This is the whole
  subtlety: the line is `<pid> (<comm>) <state> …` and comm is the process's own
  name, truncated to 15 bytes, and it may contain **spaces and parentheses**
  (substituted shells are named `(bash)`; this host's workers are named
  `gopi worker`). A whitespace split shifts every field after comm — i.e. decides
  "same incarnation" vs "recycled" with a process NAME.
* `StartTicks(procRoot, pid) (string, bool)` — field 22 as a decimal string (string
  compare cannot round or overflow the way parse-then-compare can).
* `Verdict(procRoot, want Liveness) string` — the named three-factor predicate, the
  ladder: pid dir absent → `recycled`; stat/cmdline unreadable or malformed →
  `unknown`; recorded ticks differ → `recycled`; recorded cmdline differs →
  `recycled`; state `Z` → `recycled`; all recorded factors agree → `alive`.
* `Liveness{Pid, StartTime, Cmdline}` + `Joins(argv)`; an **unrecorded** cmdline
  (empty slice) matches anything — the caller cannot demand a factor it never
  recorded (the legacy-row rule from the task-run singleton guard).
* `WorkerShape(comm, argv)` — the counting predicate the shell mirrors, stated once
  for the Go side (Rule 13/14).
* Named layout constants (`CommName`, `CmdlineName`, `StatName`, `PidDigits`,
  `StatFieldState`, `StatFieldStartTime`, `ArgvVerbIndex`, `ZombieState`) and the
  new env seam `EnvStatField` (Rule 1a/1b: no literal `22` in a scan loop, no
  `"comm"` inline).

## 2. `scripts/gopi-worker-up.sh` — reads and prints the incarnation

* New knob `STAT_FIELD="${GOPI_PROC_STAT_FIELD:-22}"` — the field NUMBER is a fact
  two readers must agree on, so it is declared in `internal/procname` and reaches
  the script through the environment like the name and the verbs (Rule 13/14); the
  gate asserts the shell default equals `procname.StatFieldStartTime`.
* Each reported worker is stamped with its incarnation: the pid list is now
  `pid:starttime` (`"pids: 3445147:525775221 …"`). The word `pids:` is kept — that
  is what callers/tests read — the *value* changed from a bare number to an
  identity a caller can check for recycling.
* The stat parse uses the same rule as Go: `${statline##*)}` then a word-split,
  indexing `statf[STAT_FIELD - 3]` because the first token after `)` is field 3.
* A process whose stat cannot be read is still counted, stamped `-`: the count is
  about shapes, the stamp is extra evidence, never a filter (Rule 15).

Verified on the live host (`bash scripts/gopi-worker-up.sh --count`):

```
live gopi workers: 4 (pids: 2004739:528184246 3445147:525775221 3445263:525779028 3460822:525826590)
other gopi processes: 12 (not workers — daemons, the merger, one-shot run children)
```

Wrong control, same host, same moment: `pgrep -x gopi | wc -l` → **16**. The
launcher's 4 is the correct number (the 16 counts task-serve daemons, the merger and
one-shot `task run` children). Every reported tick was cross-checked against
`sed 's/.*) //' /proc/<pid>/stat | awk '{print $20}'`:

```
pid=3445147 stat_field22=525775221
pid=3445263 stat_field22=525779028
pid=3460822 stat_field22=525826590
```

(Note pid 2004739 was a short-lived `gopi-worker worker --once`, gone before the
cross-check — an honest instance of the shape the code handles with `-`.)

## 3. The red proof

`internal/testutil/procname_triple_redproof_test.go`:

* `TestRecycledPidPassesCommCmdlineAndFailsTheTriple` — a **pid-reuse fixture**: one
  pid slot whose live process carries the SAME comm (`gopi`) and the SAME argv
  (`gopi worker`) as a real worker, but a **different starttime** than the recorded
  incarnation. This is the shape a worker RESTART produces. The test asserts *both*
  directions:
  * the 2/3 predicate (comm + cmdline) **accepts** the pid — it logs that the caller
    would be told its worker is alive; and
  * the triple returns `recycled`.
  It also pins the boundary the other way: a matching incarnation reads `alive`, a
  same-incarnation process with a different argv reads `recycled`, a missing pid dir
  reads `recycled`.
* `TestLauncherCountRejectsRecycledPidButCountsTheLiveOne` — the real script, driven
  through `--count` over that fixture, must stamp the pid with the LIVE incarnation's
  ticks (`2001:5002`), not a bare number.
* `TestRealWorkerIncarnationMatchesProcStat` — the same decision against the
  **operating system** instead of a fixture: a real `sleep` process is read through
  the production reader (alive), the same pid with a different starttime is
  `recycled`, and after the process is killed and REAPED the recorded identity no
  longer reads alive.

### RED → GREEN, same test, same fixture

RED — the starttime compare removed from `Verdict` (the pre-fix 2/3 predicate):

```
$ go test ./internal/testutil/ -run TestRecycledPidPassesCommCmdlineAndFailsTheTriple -count=1
--- FAIL: TestRecycledPidPassesCommCmdlineAndFailsTheTriple (0.00s)
    procname_triple_redproof_test.go:103: 2/3 predicate: comm "gopi" + argv ["gopi" "worker"] MATCH the live process at pid 2001 (the caller is told its worker is alive)
    procname_triple_redproof_test.go:110: the triple answered "alive" for pid 2001, want "recycled" — the pid exists and
        is worker-shaped, but its starttime "5002" is not the recorded "5001", i.e. the number was REUSED by a
        different process. A verdict of alive here is the defect this test exists to catch.
FAIL
```

GREEN — triple restored:

```
$ go test ./internal/testutil/ -run 'TestRecycledPid|TestLauncherCountRejectsRecycledPid' -v -count=1
=== RUN   TestRecycledPidPassesCommCmdlineAndFailsTheTriple
    procname_triple_redproof_test.go:103: 2/3 predicate: comm "gopi" + argv ["gopi" "worker"] MATCH the live process at pid 2001 (the caller is told its worker is alive)
    procname_triple_redproof_test.go:124: RED/GREEN on one fixture: comm+cmdline → alive (the recycled pid is accepted); triple → recycled
--- PASS: TestRecycledPidPassesCommCmdlineAndFailsTheTriple (0.00s)
=== RUN   TestLauncherCountRejectsRecycledPidButCountsTheLiveOne
    procname_triple_redproof_test.go:185: launcher --count over the reuse fixture: workers=1 identities="2001:5002" (live incarnation ticks=5002)
--- PASS: TestLauncherCountRejectsRecycledPidButCountsTheLiveOne (0.02s)
PASS
```

The reuse event itself is **injected** (a fixture cannot make the kernel recycle a
number on command) — said plainly here rather than implied. What is *not* injected
is the decision: the fixture holds exactly the two observations a real recycling
produces (a recorded identity, and a live process at that pid with matching shape
and different ticks), and `TestRealWorkerIncarnationMatchesProcStat` exercises the
same ladder against real procfs entries.

## 4. The gate stayed green and was NOT loosened

`internal/testutil/procname_gate_test.go` — three tests, all green, assertions
**narrower, not wider**:

* `TestWorkerUpCountsArgvShapeNotName`: the fixture now also writes a `/proc/<pid>/stat`
  line per entry, and the pid assertions were replaced by identity assertions —
  `strings.Fields(pids)` is now parsed as `pid:starttime` and the ticks are compared
  against **the fixture's own stat field 22**, read by the test (not by the code under
  test). So the change *adds* a check (the incarnation must be read from the RIGHT
  field) and removes none: the shape/decoy/name-only-inflation assertions are
  unchanged.
* `TestWorkerUpScriptNamesItsValues`: `STAT_FIELD` joined the knob table, so the shell
  default (`22`) must equal `procname.StatFieldStartTime` — the same Rule 13/14
  contract as the name and the verbs. The gate's "unknown knob" branch fails on any
  undeclared process-identity knob, so this was required, not optional.
* `TestWorkerUpScriptHasNoPgrepOrPkill`: untouched.

## 5. `finishCleanupWorktreeInUse` — verdict: **do NOT add starttime** (evidence)

`internal/tools/task_finish_cleanup.go:1373` `finishCleanupWorktreeInUse` decides
whether a worktree is in use by scanning pids, reading each one's CWD and asking
whether it is in use. The question is whether its `pidAlive` probe
(`internal/tools/taskzombie.go:120`) can be fooled by a recycled pid.

**It cannot — and the reason is structural, not lucky.** A verdict from this guard is
derived from `/proc/<pid>/cwd` (`finishCleanupProcCWD`,
`task_finish_cleanup.go:1279`), read from a pid directory found by scanning
`/proc` (`finishCleanupPIDDirs`, `:1259`):

* The pid it inspects is never a number carried from an earlier read. It comes from
  the directory the scan just enumerated and from which the CWD symlink was just
  read successfully (`:1404-1412`).
* `finishCleanupPathUnder` (`:1303`) then requires the symlink target to *be*, or be
  nested under, the worktree's own path (plus its private git dir,
  `finishCleanupWorktreeGitDir`, `:1317`).

So the property that makes a pid a "holder" is a property of the process's **live
kernel state at the moment of the read**. A recycled pid is a different process, and
for it to produce the same `in use` verdict it would have to be a live process whose
**CWD is inside that very worktree** — in which case the worktree really does have a
live holder, and refusing the removal is the correct answer. The bytes a recycled
pid's arguments are irrelevant: this guard never compares a recorded shape against a
live one.

Contrast with the counting site, which is why the triple is needed *there*: the
launcher reports "a worker exists" from comm + argv, i.e. from a **recorded/no
connection at all to the pid's kernel identity** — two processes with the same comm
and argv are indistinguishable to it. That is the site where a factor describing
incarnation adds information. At `finishCleanupWorktreeInUse` a starttime compare
would not remove a false positive; it would add a third proxy for the question that
the CWD evidence (authoritative, current, and about THE process at that pid) already
answers.

The remaining gap at that site is different and pre-existing, and it is stated here
rather than papered over: `pidAlive` is `kill(pid, 0)` minus the zombie check, so it
is a *redundant* filter (the caller already skipped an unreadable CWD at
`:1404-1406`, and a dead pid's `/proc/<pid>/cwd` is gone) — but not a *foolable* one.
`finishCleanupProcCWD` reads the CWD without first checking the state letter, so a
**zombie** whose parent has not reaped it can still have a readable `cwd` symlink and
is only filtered afterwards by `!pidAlive(n)` at `:1413`. That ordering is a
zombie-handling question, not a pid-reuse question, and it is out of scope here
(noted so it is not re-derived; the guard's outcome is unchanged either way, since
`pidAlive` does reject zombies).

**Conclusion: `finishCleanupWorktreeInUse` needs no starttime. Its verdict is derived
from live kernel state (the pid's CWD read in the same scan), not from a recorded
shape, so pid recycling cannot turn it into a different answer. Evidence:
`task_finish_cleanup.go:1259, 1279, 1303, 1317, 1373, 1404-1413` and
`taskzombie.go:120`.**

## 6. Sibling task 01a0a07e (daemon SIGKILL leak) — overlap

Its code branch `fix/daemon-e2e-leak` (`a84eb76c`, `1689c252`) is **already merged
into master** (`7b5d427a Merge branch 'fix/daemon-e2e-leak'`); only a 2-line devdoc
delta remains unmerged. That branch is where `internal/testutil/procident.go` already
documents the identical triple — **it has no consumer**: a fresh edit to its source
does not rebuild any test binary (master carries the identical text), i.e. deleting
the file does not turn anything red.

That is exactly why `internal/procname` (the declared single definition site) now
carries the production predicate rather than `testutil`. `internal/procname` is a
**leaf that imports only `strings`/`os`**, so `internal/testutil`, `internal/tools`,
`internal/config` and `cmd/agent` can all reach it without a cycle — while a
testutil helper is unreachable for production code. No file of
`fix/daemon-e2e-leak` was touched; a follow-up could make `testutil.ProcIdentity`
delegate to `procname` (or retire it), which is a cleanup, not a correctness fix.

## Pre-existing reds encountered (measured, not assumed)

`go test ./...` on this branch reports three failures. None is caused by this
change; each was re-measured with my work stashed out of the tree (pristine
master), where it fails identically:

1. `internal/testutil: TestEveryTempUsingSuiteIsPinned` — `internal/daemoncwd`
   creates scratch dirs without a pinned `TestMain`. No file of this branch is
   involved.
2. `internal/testutil: TestWorkerOwnershipPointerPresent` — a list of files that
   teach a worker-launch command without the operator-only pointer. It flagged
   **my** devdoc, which is correctly part of the contract: fixed in `d9c5cb35`
   (my file left the list). The residual 10 files
   (`briefings/brief-cwd-*`, `devdoc/daemon-e2e-leak.md`, …) fail on pristine
   master too — a pre-existing backlog, not weakened by me.
3. `cmd/agent` — `TestBareRunKeepsAllItsArtifactsInOneSessionDir`,
   `TestWorkerExecutorChildStandsInTheTasksRepoNotTheLaunchers`,
   `TestWorkerServesEachTaskInItsOwnRepoAcrossConsecutiveRounds`,
   `TestWorkerChildDoesNotInheritTheDaemonsSessionHandle`,
   `TestTaskRunStandsInTheSessionDirAndHandsTheWorkdirToTheChild`,
   `TestFrontRunConsoleOutputLandsInItsSessionDir`, and
   `internal/testutil: TestPinCoverageTable` (plus the two in 1 and 2). Measured
   with `git stash -u` on this same commit — i.e. the tree WITHOUT this branch's
   five files — where they fail identically (`EXIT=1`). They exercise session-dir
   / workdir / spawn-guard behaviour; no file of this branch is on their path,
   and no test in this branch's packages imports it.
4. `internal/tools: TestRedProof_LandedButRefKeptIsFiledAsFailure`
   (`zz_redproof_test.go`, commit `fc28faeb` "RED PROOF for worktree-residue
   defect D2") — reports "DEFECT NOT REPRODUCED" on pristine master as well. A
   red proof whose defect no longer reproduces is its own finding; it is not this
   branch's to retire.

## Not Done (honest)

* **No live OS-level pid reuse was observed.** A real recycling event cannot be
  constructed on demand (pids are ~4.2M apart on this host, and `/proc/sys/kernel/pid_max`
  reuse would take weeks). The fixture INJECTS the reuse by fixing the two
  observations a recycling really produces; the residual risk is that the fixture's
  representation is not what a real recycle looks like — the real-process test above
  is the mitigation, not a proof.
* **`procident.go` was not consolidated** into `internal/procname` (declared as
  overlap-abstention, see §6). Two implementations of the ladder now exist in the
  tree; `internal/testutil/procident.go` has no production consumer, so nothing
  consults a rival predicate behind `procname`'s back, but the duplication is real
  and is left for a follow-up.
* **`testdata/fixtures` proc trees under `scripts/` (tmp-ban)**: no new temp files
  were created on `/` or `/tmp`; the new tests use `t.TempDir()` and the existing
  scratch roots only.
* **The three pre-existing reds above are left alone** — reported, measured
  against pristine master, and not touched (fixing another agent's failing gate
  or retiring a red proof needs its own task; Rule 26 dispatch, not a drive-by).
* **The `--check`/`--install` seam was not exercised** (installing the launcher is
  the operator's action). The installed copy at `~/.local/bin/gopi-worker-up.sh` is
  now STALE relative to this branch; `--check` will report that, by design.
