<!-- doc-class: historical -->
# Task Report: the auto-row disposal chain — two classes it could not reach

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

**Task**: `01a12b2c6ba27c07b905a05da3b69c87` (branch
`task/01a12b2c6ba27c07b905a05da3b69c87`)
**Date**: 2026-10-11  **Host**: hosta (client — Rule 28: the fix lands on a
branch; the system layer itself is peerbox's)
**Epic**: `task-ledger-auto-row-disposal`

## The defect, restated after measurement

The briefing named two gaps in how `source=auto` rows are ended and disposed. I
measured both before touching code, and **one of the two framings did not
survive the measurement** — which is worth recording first, because the fix
follows the measurement, not the framing.

### Gap A — "the hook writes the empty string"

**Not reproduced.** The registration hook has stamped
`RunMethod: runcmd.MethodAuto` since **2026-09-13** (commit `6e8e12200`, "rework:
re-apply the run_cmd verbatim fix"), and it does so on current master —
`grep -c 'RunMethod: runcmd.MethodAuto' internal/agent/taskregister.go` → `4`.

The measurement that matters is the **live** one, on peerbox (whose ledger is the
project's own):

```
SELECT date(created_at,'unixepoch') d,
       COALESCE(source,'<n>') s, COUNT(*) FROM tasks
 WHERE summary LIKE '[auto]%' GROUP BY d,s ORDER BY d DESC LIMIT 12;
  2026-10-11  server 3    2026-10-10  server 2    2026-10-09  server 8
  2026-10-08  server 9    2026-10-07  server 40   2026-10-06  server 27
```

Every `[auto] ` row created since 2026-09-13 carries `auto`; of the 25 created in
the last four days, **zero** are empty. So no live writer drops the value.

### Gap A — what IS true, and it is the same bug from the other side

The old candidate query was

```sql
SELECT … FROM tasks WHERE status = ? AND run_method = ?
-- args: in_progress, auto
```

and the corpus it has to select from contains **737 rows with an EMPTY
`run_method`** out of ~1050 on peerbox (hosta: 742 empty / 1367 total; `auto`=525
on peerbox, `auto`=1 on hosta). The empty column is the **historical** shape —
written before the 2026-09-13 hook — and it is not decayed data: **438 rows
carry the `[auto] ` registration prefix and no method**, the oldest from
2026-08-28, the newest from 2026-09-30. A row's class does not change with its
age: an old row whose run is still standing is exactly as unfinished as a new
one.

So the defect is **the query**, not the writer: `status='in_progress' AND
run_method='auto'` returns **0 rows on both hosts** (measured), while the class
it was written for — the harness's own `[auto] ` registrations — is 1053 rows
wide. The one row this disposer has ever been observed disposing (task
`01a10f59`'s `frobnicate9` ghost) was caught only because a *fresh* harness row
does carry `auto`.

### Gap B — confirmed exactly as framed

`autoRunLitterIsLitter` early-returned at `if runDirExists { return false }`,
and that was also the end of the pass's interest in the row. The shape —
**run directory present, no `session_end`, holder dead** — therefore had no
disposer anywhere:

* not this pass (early return),
* not `SweepZombies` (its candidates need a heartbeat that ages, and a run killed
  before its first tool call never writes one),
* not the manual stale pass (30 days).

Measured on the live ledger 2026-09-11 (`01a08e9b-…`, `01a08e7f-…`: both
`[auto] ` rows, terminal by hand a month later).

## Completed

Both documents are in `internal/tools/taskzombie_extinct.go` unless stated.

1. **The candidate set is the REGISTRATION, not the method.** One predicate now
   owns it — `autoRunLitterCandidateWhere()` + `autoRunLitterCandidateArgs()`
   (Rule 1b/13: one spelling, and the arguments cannot be paired in a different
   order):

   ```sql
   status = ? AND run_method IN (?, ?) AND summary LIKE ?
   -- in_progress, '' (the historical shape), auto, '[auto] %'
   ```

   The `LIKE` is a **prefix** (no leading wildcard), so it is a range scan on
   the summary index, and the pattern is built from the constant, bound as a
   parameter — never interpolated.

2. **The class decision is one function, and both spellings agree.**
   `autoRunLitterRegistrationSignature(summary, method)` is the Go half;
   `TestAutoRunLitterCandidateQueryAgreesWithTheClassifier` feeds one fixture
   ledger every shape and asserts the SQL set and the Go set are identical, with
   a floor (`len==2`) so an empty-vs-empty agreement cannot pass.

   The rule it encodes, and the refusal in it: `[auto] ` prefix **required**;
   the method must be `auto` **or empty** (the historical writer). A method that
   names *another* writer while the summary claims `[auto] ` is a contradiction
   this pass does not resolve — it refuses the row (Rule 15: an uncertain answer
   keeps the row).

3. **Gap B: the run-started row now gets its own arm, and it is NOT called
   litter.** `autoRunLitterClassify` returns one of three values —
   `autoRunLitterNone` / `autoRunLitterNeverStarted` / `autoRunUnfinishedRun`.
   The second new arm **records** the class on the row's own audit trail
   (`task_events`, event `auto-run-unfinished`, detail naming the measured run
   directory and the age), reports it, and **leaves the status alone** — the row
   stays `in_progress` with its holder stamp, which is exactly the input the
   finish-cleanup / zombie chain reads. It is not failed, because it
   demonstrably ran and this pass has no evidence about *how* it ended.

   The record is **idempotent by the dedup needle** (`taskEventHasDetail`, the
   same one-branch-one-record discipline `repo_sweep_ref_guard` uses), so a row
   seen across ticks leaves ONE record, not one per tick — asserted.

4. **The tick reports the two arms as the two different things they are.**
   `AutoRunLitterSweep(sec)` keeps its contract **unchanged** (the rows whose
   STATUS this call changed) — the shape the 2026-09-11 regression test and any
   existing caller was written against — and a new
   `AutoRunLitterSweepReport(sec) (AutoRunLitterReport, error)` carries both arms
   as two separate lists, `Disposed []autoRunLitterDisposed` and
   `Routed []autoRunUnfinishedRouted`. The tick calls the report face: it prints a
   routed row on its own line and counts it as neither disposed nor candidate.
   Reporting a row whose status did not change as "disposed" would be a false
   claim, and a silent route would be indistinguishable from a router that never
   ran (Rule 15).

5. **Tests, with controls** (`internal/tools/taskzombie_extinct_test.go`):

   | test | what it pins |
   |---|---|
   | `…DisposesTheHistoricRowWithNoMethod` | **A**: `[auto] ` + empty method, no run dir → collected |
   | `…RoutesTheRunThatStartedWithoutAnOutcome` | **B**: run dir present → routed, status untouched, ONE event, ONE after a second pass |
   | `…StillCallsTheSameRowLitterWithoutItsRunDir` | **control**: the same row minus the run directory → disposed (non-vacuous) |
   | `…ClassifySparesForeignRows` | imported/agent/merger rows and a summary+method contradiction → `None` |
   | `…CandidateQueryAgreesWithTheClassifier` | the SQL and the Go predicate select the same rows, with a qualifying-count floor |
   | `…SummaryPrefixMatchesTheAgentRegistrationHook` | the mirrored `[auto] ` constant still matches `internal/agent`'s source |

6. **The merge's interaction with the sibling fix, stated rather than assumed.**
   Merging master brought in `internal/tools/taskzombie_extinct_20260911_test.go`
   (task 01a12b08's forward guard, `TestAutoRunLitterSparesTheRunThatEnded`) —
   the SAME 2026-09-11 rows, reached from the other side. Its assertion is that
   the run-started row must NOT be collected as never-started litter and must
   stay `in_progress`; both still hold after this change (the test PASSES), and
   the two fixes compose rather than conflict: arm ② claims nothing about a run
   that ended, it only stops the silence. The mechanical collision between the
   two branches was one duplicated test function name — mine, appended earlier
   from the deleted `_20260911_test.go` — and it is REMOVED here in favour of the
   upstream original, which is the authority for that case.

7. **Red proof, taken by reverting the fix** (not asserted — measured): a
   throwaway test file run against the pre-fix file logged

   ```
   RED PROOF: the `[auto] ` row 01a12b41-091d-… with an EMPTY run_method was NOT
              collected by the pre-fix query
   RED PROOF: the run-started row 01a12b41-0926-… (run dir exists, no session_end)
              was neither collected nor recorded by the pre-fix code
   ```

## Not Done (honest)

* **The two 2026-09-11 rows were NOT touched.** They are already terminal, and
  the fix is forward-looking; re-opening another run's rows is not this task's
  business.
* **The routed row is not literally handed to `finishCleanup`/`SweepZombies` by a
  call.** Routing here means "recorded + reported + left in exactly the state
  that chain consumes". A row with a dead holder and no heartbeat is *still*
  invisible to `SweepZombies`' own candidate query — that is a property of that
  query, not of this pass, and making this pass dispose of a started run would
  be the widening the arm exists to avoid. What the arm changes is that the
  class is no longer silent: it is on the row's trail with a timestamp, and on
  the tick's report line. **A follow-up belongs to peerbox** (Rule 28: it is the
  zombie chain's candidate query, i.e. the system layer) and I did not file it
  from here.
* **No live-ledger fixture.** The tests drive a fresh per-test ledger
  (`testutil.NewTestDBPath`, Rule 32) and a fixture profile; the *live* corpus
  numbers above are measurements I read from the operator's ledger read-only, not
  rows any test writes.
* The classification of the empty method as "historical" is an **argument from
  the corpus** (no live writer produces it; the hook has stamped the value since
  2026-09-13), not from a schema-level constraint — nothing prevents a future
  writer from leaving it empty, and if one does, the `[auto] ` prefix still
  decides.

## Verification

* **Build**: `CGO_ENABLED=0 go build ./...` → exit 0 (`BUILD_OK`).
* **Tests**:
  * `CGO_ENABLED=0 go test ./internal/tools/ -run 'AutoRunLitter|AutoRunUnfinished' -count=1`
    → `ok github.com/nick/gopi/internal/tools 0.728s` (all 8 test functions +
    their subtests PASS).
  * `make test-budget PKGS='./internal/tools ./cmd/agent'` → the run's own
    stream under `/home/user/workspace/gopi-test-scratch/queue/test-budget/`
    (`run-1791727173-149199.json`): **2395 pass, 15 fail**, and every failing
    test is a PRE-EXISTING ENVIRONMENT failure, not this diff — verified by
    running two of them against **master in the main repo** and getting the same
    red:

    * `TestQueryDefinitionSelfHealsUnsearchableIndex` — `ctags: executable file
      not found in $PATH` (the container has no ctags);
    * `TestMergeBuildGate_*` / `TestTaskMerge_BuildRefused*` — the merger's
      BUILD gate **abstains** because the suite's scratch root `/dev/shm` is
      tmpfs ("Go build cache root … is on RAM-backed storage"), which is the
      2026-09-15 incident the abstenation exists for, not a code failure;
    * `TestPrefetchTopNShapeKeepsProductionRanking` (cmd/agent) — passes when run
      alone on BOTH trees; it failed under the budget run's parallel load.
* **Ran**: the changed surfaces were executed, not merely compiled —

  ```
  $ CGO_ENABLED=0 go test ./internal/tools/ -run 'AutoRunLitter|AutoRunUnfinished' -count=1 -v
  === RUN   TestAutoRunLitterSweepDisposesTheRealGhostRow
  [task-notify] auto-run litter 01a12b3f-f5e8-…: disposed (auto-run litter (no run
      ever started): run_method=auto, sessions/<task_id> was never created …)
  --- PASS
  === RUN   TestAutoRunLitterSweepDisposesTheHistoricRowWithNoMethod
  [task-notify] auto-run litter 01a12b3f-f5fd-…: disposed (… run_method=, …)
  --- PASS: TestAutoRunLitterSweepDisposesTheHistoricRowWithNoMethod
  === RUN   TestAutoRunLitterSweepRoutesTheRunThatStartedWithoutAnOutcome
  [task-notify] auto-run unfinished 01a12b3f-f607-…: routed (auto-run unfinished
      (run started, outcome never written): run_method=auto, sessions/<task_id>
      EXISTS and the session log carries no session_end …)
  --- PASS
  ```

  **The sweeper itself, on a REAL ledger, over exactly the two shapes** (a
  throwaway `main` under the module, driving the exported
  `AutoRunLitterSweepReport` — the surface the tick calls — with the probe file
  deleted afterwards):

  ```
  $ go run ./tmpprobe "$(mktemp -d)"
  seeded: 01a12b43-5d9c-… (historic, empty method)
          01a12b43-5da4-… (run dir …/.gopi/sessions/01a12b43-5da4-…)
  REPORT disposed=1 routed=1
    DISPOSED 01a12b43-5d9c-… :: auto-run litter (no run ever started): run_method=, …
    ROUTED   01a12b43-5da4-… :: runDir=…/.gopi/sessions/01a12b43-5da4-… :: auto-run unfinished …
  AFTER: historic=failed (want failed)  run-started=in_progress (want in_progress)
         its task_events=auto-run-unfinished
  ```

  → the historic row (empty method) is collected, the run-started row is routed
  and **keeps `in_progress`**, and its `task_events` row is there. This is the
  end-to-end proof that the two gaps are closed, on the real call path.

  The run-registration hook was also exercised on a real binary, in a sandbox
  profile
  (Rule 34), by a real `gopi` binary driving the run-registration hook:

  ```
  $ gopi tool sandbox create --label probeA --task-id 01a12b2c…
  $ GOPI_HOME=<sandbox> /tmp/gopi-fix --base-url http://127.0.0.1:9 --api-key dummy --timeout 3 --prompt 'probe A'
  [gopi] task ledger: failed 01a12b35-901d-… (auto)
  $ <read the sandbox's own ledger>
  01a12b35-901d-7eed-addf-c4ae622459e0 | failed | server | auto | gopi-fix --base-url …
  ```

  → the hook's live row carries `run_method=auto` **and** the `[auto] ` summary,
  which is the pair the new candidate predicate selects on, end to end.

  **The REAL CLI face, on a sandbox profile, by a real tick** (Rule 31/34: the
  surface the change lives on, invoked for real — not the library behind it).
  The probe used the standard initializer, a sandbox ledger seeded with exactly
  the two shapes and nothing else, and the ordinary `gopi worker --once` tick:

  ```
  $ gopi tool sandbox create --label probe01a12b2c --task-id 01a12b2c
  /home/user/workspace/gopi-sandbox/sandbox-probe01a12b2c-9b91f7a5
  # seeded into <sandbox>/.gopi/memory.db: aa01 = [auto] + EMPTY method, no run dir
  #                                          bb02 = [auto] + auto, sessions/<id> EXISTS, no session_end
  $ GOPI_HOME=<sandbox> GOPI_TASK_DISPOSE_SEC=300 gopi worker --once
  [gopi] [lifecycle] resident task-lifecycle sweep (stale report + confirmed-zombie
         disposal + finish cleanup) active on this process; every 10m0s
  [task-notify] auto-run litter 01a12b2c-…-aa01: disposed (auto-run litter (no run
      ever started): run_method=, sessions/<task_id> was never created …)
  [worker] [zombie-dispose] 01a12b2c-…-aa01 disposed (auto-run litter, no run ever started)
  [worker] [lifecycle] stale reported=0 disposed=1 sink=bash /home/user/.gopi/notify/notify.sh
  ```

  …and after correcting the probe's own artifact (the session file was `touch`ed
  to 10 minutes old — without that the row is correctly vetoed as a LIVE writer,
  which the first pass demonstrated), the SECOND arm on the same real tick:

  ```
  $ GOPI_HOME=<sandbox> GOPI_TASK_DISPOSE_SEC=300 gopi worker --once
  [task-notify] auto-run unfinished 01a12b2c-…-bb02: routed (auto-run unfinished
      (run started, outcome never written): run_method=auto, sessions/<task_id>
      EXISTS and the session log carries no session_end …)
  [worker] [zombie-dispose] 01a12b2c-…-bb02 ROUTED to the finish-cleanup/zombie
      chain (auto-run unfinished: <sandbox>/.gopi/sessions/01a12b2c-…-bb02)
  [worker] [lifecycle] stale reported=0 disposed=0 sink=…
  ```

  Read back from the sandbox's own ledger afterwards:

  ```
  ROW aa01 status=failed       run_method=""     reason=auto-run litter (no run ever started) …
  ROW bb02 status=in_progress  run_method="auto" reason=<empty>
  EVENT bb02 auto-run-unfinished @1791727798
      auto-run-unfinished: <sandbox>/.gopi/sessions/01a12b2c-…-bb02 — auto-run unfinished …
  ```

  → the empty-method row is `failed` with the disposal reason; the run-started
  row **keeps `in_progress`**, its `reason` column is untouched, and its
  `auto-run-unfinished` record is on the trail. The dispose arm's count and the
  route arm's count are reported separately by the tick (`disposed=1` then
  `disposed=0`), which is the contract the epic asked for.
* **Result**: both new arms fire on real rows read back from the ledger
  afterwards; the routed row's status is unchanged and its `task_events` row is
  present exactly once after two passes.
* **Commit**: (this branch's tip — see the closing message)

## Evidence (self-filed)

* `SELECT COALESCE(run_method,'') rm, COUNT(*) FROM tasks GROUP BY 1` on peerbox
  `~/.gopi/memory.db` → `''=737, auto=525, worker=374, manual=68, child=39`.
* `SELECT COUNT(*) FROM tasks WHERE status='in_progress' AND run_method='auto'`
  → **0** on peerbox, **0** on hosta. (The old query's own answer.)
* `SELECT COUNT(*) … WHERE summary LIKE '[auto] %' AND run_method=''` → **438**
  on hosta (min 2026-08-28, max 2026-09-30).
* `git log -S "RunMethod: runcmd.MethodAuto" -- internal/agent/taskregister.go`
  → `6e8e12200 2026-09-13`.
* Red proof (pre-fix file, throwaway test): the two `RED PROOF:` lines quoted
  above, both `--- PASS`.
* `TestAutoRunLitterCandidateQueryAgreesWithTheClassifier` → PASS with the
  derived non-zero qualifying-count floor.
* Live-run evidence in the sandbox: `01a12b35-901d-… | failed | server | auto`.
* **CLI end-to-end (this run, hosta, 2026-10-11)**: `gopi worker --once` on a
  `gopi tool sandbox create` profile, `GOPI_TASK_DISPOSE_SEC=300`, ledger seeded
  with exactly two synthetic rows → tick printed `disposed=1` (the EMPTY-method
  `[auto] ` row, no run dir) and `disposed=0` (the run-started row routed, not
  disposed); read back: `aa01 status=failed`, `bb02 status=in_progress` +
  one `task_events` row `auto-run-unfinished`. Both arms reached from a real
  binary, no library shortcut.
* **The live-writer veto also reproduced for real**: the first pass over `bb02`
  skipped it because its session file had just been written (`sessionFreshWithin`)
  — the conservative branch, observed rather than assumed; the row was routed
  only after the file was aged past `autoRunLitterSettleSec`.

## Files

* `internal/tools/taskzombie_extinct.go` — the candidate predicate, the class
  signature, the classifier, the route record, `AutoRunLitterResult`.
* `internal/tools/taskzombie_extinct_test.go` — the tests above.
* `cmd/agent/cli_tasks_worker.go` — the tick's report line for the routed arm.
* `devdoc/task-01a12b2c-auto-row-disposal-chain.md` — this report.
* `devdoc/DOC-INDEX.tsv` — classified (Rule 36).
