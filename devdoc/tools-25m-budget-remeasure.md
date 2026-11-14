<!-- doc-class: normative -->
# `./internal/tools` vs the DECLARED 25m budget — re-measured and classified (2026-09-19)

**Deliverable**: measure `./internal/tools` against the repo's declared
per-package budget (`scripts/test-budget.sh`, `test_budget="25m"`) — NOT Go's
default 10m — with the **direct instrument only** (wall clock + the script's own
slowest-15 list; **no device or CPU load was generated, Rule 27**), then
classify the heavy offenders with the existing machinery.

**Branch**: `fix/tools-budget-25m-remeasure` (worktree-shared in
`/home/user/work/GoPi`; no push needed).

**Trees measured**: `36e0f0bc` (runs 1–2: master at measurement start) and
**`90e664e0`** (runs 3–4: master after concurrent merges — the branch is based
on it). The sibling fix `e9d72669` (the peer-alias inventory row + the
`GOPI_NODE_ROLE=client` fixture de-pin) is an **ancestor of both**
(`git merge-base --is-ancestor e9d72669 master` → yes), so the "26 unrelated
master reds dominate the run" condition of the original finding does not hold.

---

## 1. The headline

**On master as it stands, the declared 25m budget TRIPS — and the fix is the
classification, which brings the package back under it.**

| # | configuration | tree | budget | wall | exit | outcome |
|---|---|---|---|---|---|---|
| 1 | master, **unclassified** | 36e0f0bc | 25m | **1503 s** | **3** | **TIMEOUT (cap reached)** |
| 2 | master, **unclassified** | 36e0f0bc | 10m | 626 s | 3 | TIMEOUT (cap reached) |
| 3 | master, **classified, heavy ENABLED** | 90e664e0 | 45m | 1926 s | 1 | no trip — full suite |
| 4 | master, **classified, DEFAULT path** | 90e664e0 | **25m** | **1009 s** | 1 | **no trip — 491 s of headroom** |

Run 1 (the citable measurement of the unclassified package) and run 4 (the
same package after classification, on the default path) sit side by side:
**1503 s → 1009 s, and the named wall-clock timeout disappears.**

## 2. Run 1 — the budget trip, verbatim

```
=== gopi test budget run ===
packages: ./internal/tools/
budget:   25m per package (scripts/test-budget.sh test_budget; override GOPI_TEST_TIMEOUT)
host:     load 7.91 7.53 9.30  cores 4
raw json: …/test-budget/last.json

--- summary (1503s wall, load 7.91 7.53 9.30 -> 5.62 6.56 7.50) ---
packages: 1 finished, 1 with a failure
  FAILED: github.com/nick/gopi/internal/tools
slowest 15 tests (wall-clock seconds):
    34.05  tools                    TestFirstOpenBlockDeviceCostIsTheSync
    29.12  tools                    TestRepoSweepRefGuard_RealBranchStillReachesTheGates
    22.85  tools                    TestManyFreshDBsNoHang
    16.52  tools                    TestMergesetGate_RegressionIsAttributedToTheLanding
    16.48  tools                    TestVetGateRevert_BothArmsDifferOnIdenticalMeasurement
    13.19  tools                    TestMergeUnionGate_TargetAlsoRedAbstains
    12.53  tools                    TestMergesetGate_ReportOnlyNeverARollback
    12.14  tools                    TestMergesetGate_FilesABugRowOnRed
    12.02  tools                    TestMergesetGate_RedMergeResultIsReported
    11.97  tools                    TestReproSkipTagHeadBlocking
    11.96  tools                    TestTaskMerge_UnionRefusalArchivesAndNotifies
    10.90  tools                    TestMergesetGate_CleanResultFilesNoBugRow
    10.59  tools                    TestMergeUnionGate_ControlUnionCompilesIsNotRefused
    10.01  tools                    TestFinishCleanupActionBudgetOutlivesTheLedgerRead
     9.97  tools                    TestMergeUnionGate_RefusesTheIncidentShape

================== TIMEOUT, NOT AN ASSERTION FAILURE ==================
The per-package test budget (25m) ran out — a WALL-CLOCK cap,
not a failing assertion. …
the budget expired here:
  panic: test timed out after 25m0s
separate assertion failures in the same run (NOT the timeout):
  --- FAIL: TestAssertionTotalityAuditGate (0.64s)
  --- FAIL: TestMemorySearchPossiblyStale (0.37s)
  --- FAIL: TestProcStatParseHasOneImplementation (0.03s)
  --- FAIL: TestCovToolsNotifyAlarmRegistryIsTheSourceOfTruth (0.34s)
======================================================================

stamp: 2026-09-19T20:29:18+08:00 host=peerbox pkg=./internal/tools/ budget=25m \
       elapsed=1503s load_start=7.91 7.53 9.30 load_end=5.62 6.56 7.50 cores=4 \
       exit=3 outcome=timeout stream=run-1789820958.json
EXIT=3
```

| quantity | value |
|---|---|
| package | `./internal/tools/` |
| declared budget | `25m` (`scripts/test-budget.sh` `test_budget`) |
| **package wall time** | **1503 s** — the cap, not a completion |
| exit status | **3** (the script's dedicated TIMEOUT code — never 1) |
| classification | **budget trip**: `panic: test timed out after 25m0s` |
| load before → after | `7.91 7.53 9.30` → `5.62 6.56 7.50`, 4 cores |
| concurrent suites at start | 2 × `go test ./cmd/agent/` (5 min and 14 min old) |

### The "25m exceeded" finding is CONFIRMED, not an artifact

The earlier trip (`devdoc/test-budget-single-source.md` §1, acceptance run) was
recorded at load 32–43. This re-measurement reproduces it at **load 7.9** — a
quieter host — and a second isolated run reproduces it again:

| run | load start | package wall | outcome |
|---|---|---|---|
| `test-budget-single-source.md` acceptance | 32.0 | 1501 s | exit 3 — TIMEOUT |
| this task, 1st isolated run | 10.84 | 1503 s | exit 3 — TIMEOUT |
| this task, 2nd isolated run (quoted above) | 7.91 | 1503 s | exit 3 — TIMEOUT |

So the trip is a **cost**, not a load artefact and not a cap artefact. What the
epic's hypothesis got right is the OTHER half: the *reds* were the artifact
(widening the budget removes none of them — §5), and `e9d72669` closed 22 of
the 26. The budget trip itself stands. **The record should say: confirmed, at
half the load, and fixable (§3).**

## 3. What the 1503 s actually is (measured, not assumed)

From run 1's own `go test -json` stream (the citable per-run copy):

| quantity | value |
|---|---|
| tests with a terminal action | 1521 |
| **sum of per-test wall** | **1548.2 s** (the 1500 s cap was reached) |
| tests ≥ 5 s | 60, summing 565.9 s = **37.6 %** of the wall |
| tests < 5 s | 1461, summing 982.3 s = **65.4 %** of the wall |
| still running when the cap expired | 1 (`TestTaskPriorityBackfillLegacyBugRows`) |

**The suite is a long tail, not one slow test.** With 1521 tests, the *average*
test is 1.0 s — every one of them pays per-test fixture setup (fresh ledger
open, scratch tree), which is the mechanism `devdoc/test-budget-single-source.md`
§2 identified. `sum(per-test) ≈ package wall` (1548 ≈ 1503) also means nothing
is blocked or parked: the wall is work, not a hang.

## 4. The classification (the machinery, derived from that stream)

Applied with the existing mechanism, derived from run 1's measured stream
(60 top-level tests at or above the shared 5 s floor,
`scripts/lib/heavy-threshold.sh`):

- **60 call sites** gained `heavytest.Require(t, <Criterion>)` as the first
  statement of the test body, across **28 files**;
- **60 rows appended** to `testdata/heavy-tests.tsv` (43 → **103** data rows).
  Precision for a later reader: 9 of the 43 pre-existing rows are `internal/tools`
  rows whose call sites already existed (the `testsuite`-family, `TestMergeBuildGate_*`,
  `TestGOPI_ALLOW_DANGEROUS_*`, …), so `grep -c heavytest.Require internal/tools/*.go`
  reads **69**, not 60, and `internal/tools` rows in the inventory are 69 of the 103.
  Both directions were re-checked by parsing the TSV against the `Require` lines in
  the test bodies: 69 rows ↔ 69 call sites, zero on either side;
- **criteria assigned by mechanism, checked by a solo round trip** —
  "solo" = `go test ./internal/tools/ -count=1 -run '^Test$'`, i.e. the test's
  own cost without the suite's fixture contention:

| test | solo | in-suite | why |
|---|---|---|---|
| `TestFirstOpenBlockDeviceCostIsTheSync` | **61.9 s** | 34.1 s | real device minutes → `CriterionDiskFixture` |
| `TestManyFreshDBsNoHang` | **45.6 s** | 22.9 s | many fresh ledgers → `CriterionDiskFixture` |
| `TestRepoSweepRefGuard_RealBranchStillReachesTheGates` | 14.0 s | 29.1 s | drives the real toolchain → `CriterionDeviceExec` |
| `TestRankApplyMatrix` | 22.5 s | 6.4 s | intrinsic → `CriterionDiskFixture` |
| `TestTaskStateMachineRejectsIllegalTransitions` | 4.4 s | 7.4 s | contention only → `CriterionDiskFixture` (borderline) |
| `TestTaskReframeStatsSurfacesMalformedDetail` | 2.1 s | 5.1 s | contention only → `CriterionDiskFixture` (borderline) |

Final assignment: **33 `CriterionDeviceExec`** (the `rebuild_vet_*`, `repo_sweep_*`,
`merge_union_gate`, `task_finish_premerge` families exec the real
`go build`/`go vet` inside a git fixture repo) + **27 `CriterionDiskFixture`**
(fresh-ledger / fresh-scratch fixtures). **No gate package was classified**
(`internal/standards`, `internal/stylegate` appear in no row).

### The gate is green, both directions

```
$ go test ./internal/standards/ -run TestHeavyTestInventoryMatchesItsCallSites -count=1
ok  	github.com/nick/gopi/internal/standards	1.726s
$ go test ./internal/standards/ -run 'TestHeavy' -count=1
ok  	github.com/nick/gopi/internal/standards	1.718s
```

### Both halves of the switch were OBSERVED, not asserted

```
# OFF (default): classified tests skip, fast, and the skip is enumerable
$ env -u GOPI_HEAVY_TESTS GOPI_HEAVY_SKIP_LEDGER=<l> go test ./internal/tools/ -run '^(TestFirstOpenBlockDeviceCostIsTheSync|TestManyFreshDBsNoHang|TestMergeUnionGate_RefusesTheIncidentShape)$' -v
  firstopen_sync_test.go:268: HEAVY TEST SKIPPED (…): TestFirstOpenBlockDeviceCostIsTheSync	disk-fixture: …
  --- SKIP: TestFirstOpenBlockDeviceCostIsTheSync (0.00s)
  --- SKIP: TestManyFreshDBsNoHang (0.00s)
  --- SKIP: TestMergeUnionGate_RefusesTheIncidentShape (0.00s)
  ok  	github.com/nick/gopi/internal/tools	1.090s          # ledger lines: 3

# DECLARED ON: the same test runs
$ GOPI_HUMAN_TERMINAL=1 GOPI_HEAVY_TESTS=1 go test ./internal/tools/ -run '^TestMergeUnionGate_RefusesTheIncidentShape$' -v
  HEAVY TESTS REFUSED: GOPI_HUMAN_TERMINAL is declared, so the inherited work signature is stepped over and the heavy set RUNS.
  --- PASS: TestMergeUnionGate_RefusesTheIncidentShape (12.51s)
```

## 5. Run 4 — the SAME package, after classification, on the DEFAULT path

```
--- summary (1009s wall, load 5.84 5.73 6.75 -> 5.41 4.85 5.41) ---
packages: 1 finished, 1 with a failure
  FAILED: github.com/nick/gopi/internal/tools
slowest 15 tests (wall-clock seconds):
    11.00  tools                    TestTaskVerifyWaitDelayBoundsPipeHold
     9.07  tools                    TestVerifyGateReworkBriefingRendersNoVerdictAsNoVerdict
     8.61  tools                    TestTaskForBranchIgnoresRowsWithoutABranch
     6.72  tools                    TestVerifyGateDetectsBugfixRow
     5.54  tools                    TestNotifyAllowListDropReadsAsAFilterNotAFailure
     5.33  tools                    TestTaskRunGuardVerdicts
     5.21  tools                    TestTaskNextConcurrentClaimRace
     5.14  tools                    TestVerifyGateFailCapStopsRefiling
     5.06  tools                    TestTaskOpenMergeBacklogBranches_TerminalRowStillMasks
     4.93  tools                    TestWorkerProvenanceTagReachesLedgerWithoutWrapperEnv
     4.53  tools                    TestProvenanceCountsSumToPopulation
     4.37  tools                    TestTaskForBranchRepoNarrowing
     4.36  tools                    TestMergerSweepCarriesTheSalvageLines
     4.32  tools                    TestZZRedProof_DirtyDeadHolderWorkIsOrphaned
     4.07  tools                    TestAdoptionAlreadyIntegratedKeepsTheRefWhenGitRefuses

--- failures (assertion/build; the budget did NOT trip) ---
  --- FAIL: TestAssertionTotalityAuditGate (0.46s)
  --- FAIL: TestCovToolsNotifyAlarmRegistryIsTheSourceOfTruth (0.21s)

stamp: 2026-09-19T21:29:46+08:00 host=peerbox pkg=./internal/tools/ budget=25m \
       elapsed=1009s load_start=5.84 5.73 6.75 load_end=5.41 4.85 5.41 cores=4 \
       exit=1 outcome=fail stream=run-1789824586.json
EXIT=1
```

**1009 s (16 min 49 s), 491 s of headroom under the 25m budget, and no timeout
line at all.** The slowest-15 is now entirely sub-11 s tests — because the
minutes-scale tests are off the path. The package is still RED, and that is
correctly reported: the only two failures are the **pre-existing deterministic
master reds** (§6), and the budget had 33 % to spare.

Run 3 is the complementary control — the **heavy set ENABLED** on the same
tree, at 45m so the classification's own reading could be measured:

```
sum(per-test) = 1955 s over 1858 tests; package wall 1926 s; exit 1 (no trip)
the 60 classified tests RAN and account for 595 s = 31 % of the package wall
```

That arithmetic (1955 ≈ 1926) is also why "skipping the heavy tests" is a sound
reduction rather than a way to hide a hang: nothing is blocked, the heavy tests
are *additive* minutes, and run 4 confirms the residual directly.

## 6. The failures, attributed (this package is NOT called green)

Reproduced individually, and against **pristine master** (`90e664e0`, detached
control):

| test | this tree | pristine master | attribution |
|---|---|---|---|
| `TestAssertionTotalityAuditGate` | FAIL | **FAIL** | **pre-existing master red** (deterministic, ~1–2 s) |
| `TestCovToolsNotifyAlarmRegistryIsTheSourceOfTruth` | FAIL | **FAIL** | **pre-existing master red** (deterministic) |
| `TestMemorySearchPossiblyStale` | ok alone | ok alone | load-flaky (red only with concurrent suites) |
| `TestProcStatParseHasOneImplementation` | ok alone | ok alone | load-flaky (red only with concurrent suites) |

Neither red is touched or repaired by this branch — a measurement branch does
not fix other people's reds — and both are filed as a bug task (§8).

Two more master reds surfaced while validating the classification (both
pre-existing, both unrelated to this diff, both invisible in a
`internal/tools`-only run):

- `internal/standards TestDocManifestCoversEveryDevdocFile` — five *other*
  agents' devdocs have no `devdoc/DOC-INDEX.tsv` row (reproduced identically on
  pristine `90e664e0`; this branch adds only the row for its own doc);
- `internal/stylegate TestStyleGate` — new Rule-16/17 hits in
  `internal/tools/zz_redproof3_*`, `testsuite/web_test.go` (FAIL on pristine
  master too).

## 7. Two measurement traps that WILL corrupt this number

**(a) `test-budget.sh`'s raw stream is ONE shared path.** It writes
`<scratch queue>/test-budget/last.json` with a plain redirect, and the scratch
queue is host-wide. Round 1 of this measurement was destroyed by exactly that:
the file held **2422 `cmd/agent` + 1328 `internal/tools` events interleaved**
(another agent's run writing the same file), so the script's own printed
slowest-15 was half another package's. Every number in this report comes from a
run isolated with `GOPI_TEST_HOME=<per-task dir>`. Filed as a bug (§8) —
reported, not fixed here, because the fix is script behaviour.

**(b) an agent shell has the heavy opt-in ENABLED.** Run 3's env recorded
`GOPI_HEAVY_TESTS` truthy **and** `GOPI_HUMAN_TERMINAL` declared, so that run
executed the FULL heavy suite (1926 s) instead of the default path. Anyone
re-measuring MUST `env -u GOPI_HEAVY_TESTS` and record
`env | grep -E 'GOPI_HEAVY_TESTS|GOPI_HUMAN_TERMINAL'`, or they will measure a
configuration nobody ships. Runs 1–2 are unaffected (they ran on the
unclassified tree, where there is no classification to enable), but the trap is
worth stating because it silently inflates any post-classification measurement.

## 8. Not Done (honest)

- **The 2 deterministic master reds are left standing** — attribution only, no
  repair, in a measurement branch. Filed as task `01a0b9c7-ce03-71a5-b242-6c520531021c`.
- **The 2 flakes are not repaired** — load-induced; they belong to the
  flaky-class work (`devdoc/heavy-tests-rederivation.md` §4).
- **The shared-`last.json` defect is reported, not fixed** — filed as task
  `01a0b9c7-cbbe-70c0-b5e7-65dc55073b19`.
- **The 1461 sub-5 s tests were not individually optimised.** They are 65 % of
  the wall and none is honestly heavy; the only lever left is per-test fixture
  cost, which is a separate piece of work (the `GOPI_HOME`-pin family was
  already REJECTED for reddening 8 legitimate fixtures — it must not be
  re-attempted as a shortcut).
- **The two borderline rows** (`TestTaskStateMachineRejectsIllegalTransitions`
  at 4.4 s solo, `TestTaskReframeStatsSurfacesMalformedDetail` at 2.1 s solo)
  are classified on their in-suite measurement only; their solo cost is below
  the floor. They are recorded here as the weakest two of the 60.
- **The runs span two master tips** (`36e0f0bc` for runs 1–2, `90e664e0` for
  runs 3–4) because master moved under this task. Both carry `e9d72669`.

## 9. Raw evidence

| artifact | path |
|---|---|
| run 1 — the citable budget trip (script stdout) | `/home/user/workspace/_evid/tools-budget/run2.txt` |
| run 1 — its `go test -json` stream | `/home/user/workspace/_evid/tools-budget/tripped-run.json` |
| run 1 — env/load/head + concurrent-suite list | `/home/user/workspace/_evid/tools-budget/env2.txt` |
| run 2 (10m cap) | scratch queue `test-budget/stamps.log` |
| run 3 — classified, heavy ENABLED, 45m | `/home/user/workspace/_evid/tools-budget/run3-45m.txt` |
| run 4 — classified, DEFAULT path, 25m | `/home/user/workspace/_evid/tools-budget/run4-default-path.txt` |
| run 3/4 env + heavy-var listing | `…/_evid/tools-budget/env3.txt`, `env4.txt` |
| solo round-trip logs (6 tests) | `…/_evid/tools-budget/percase-<Test>.txt` |
| derived classification plan | `…/_evid/tools-budget/classification-plan.tsv` |
| contaminated round-1 stream (the trap, kept as evidence) | `…/_evid/tools-budget/contaminated-run.json` |
| skip ledger (3 rows, switch OFF) | `…/_evid/tools-budget/skipledger.tsv` |

## Evidence (self-filed)

- **Budget trip**: `scripts/test-budget.sh ./internal/tools/` → stdout in
  `run2.txt`, ending `… budget=25m elapsed=1503s load_start=7.91 7.53 9.30
  load_end=5.62 6.56 7.50 cores=4 exit=3 outcome=timeout`, plus its own
  `EXIT=3` line and `panic: test timed out after 25m0s`.
- **Post-classification default path**: `env -u GOPI_HEAVY_TESTS scripts/test-budget.sh ./internal/tools/`
  → `run4-default-path.txt`: `--- summary (1009s wall, load 5.84 … -> 5.41 …) ---`,
  `stamp: … budget=25m elapsed=1009s … exit=1 outcome=fail`, `EXIT=1`; no
  `panic: test timed out` line anywhere in the file.
- **Inventory gate**: `go test ./internal/standards/ -run TestHeavyTestInventoryMatchesItsCallSites -count=1`
  → `ok github.com/nick/gopi/internal/standards 1.726s` (103 rows ↔ 103 call sites).
- **Positive half of the switch**: `GOPI_HUMAN_TERMINAL=1 GOPI_HEAVY_TESTS=1 go test … -run '^TestMergeUnionGate_RefusesTheIncidentShape$' -v`
  → `--- PASS: TestMergeUnionGate_RefusesTheIncidentShape (12.51s)`.
- **Negative half**: `env -u GOPI_HEAVY_TESTS … -v` → three `--- SKIP:` at
  `(0.00s)`; skip ledger 3 lines.
- **Intrinsic cost**: `go test ./internal/tools/ -count=1 -run '^TestFirstOpenBlockDeviceCostIsTheSync$'`
  → `ok … 61.914s`; `TestManyFreshDBsNoHang` → `ok … 45.608s`.
- **Master-red attribution**: `… -run '^TestAssertionTotalityAuditGate$'` →
  `FAIL` on BOTH this tree and pristine master `90e664e0`; same for
  `TestCovToolsNotifyAlarmRegistryIsTheSourceOfTruth`; the two flake suspects
  are `ok` alone on both trees.
- **Rule 30**: `git status --porcelain` in the worktree prints nothing at
  close-out.
