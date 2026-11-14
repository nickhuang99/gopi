<!-- doc-class: roadmap -->
# Heavy-test re-derivation recipe + the drift catch (2026-09-18)

Closes the gap left by the classification change (`devdoc/heavy-tests-rule27-gate.md`).
That change made heavy tests OFF by default and gated the inventory against the
**call sites** it lists. It could not see the other direction: a test that becomes
long **after** the inventory was written. This doc is the recipe that re-derives
the inventory from a real run, and the mechanism that NAMES such a test instead
of letting it quietly eat the smoke budget.

## 0. The three classes, and where each one is caught

| class | caught by | where |
|---|---|---|
| **heavy** — classified, skipped on smoke | `internal/standards` inventory gate (call site ↔ row, both directions) | `testdata/heavy-tests.tsv` |
| **unclassified-long** — crosses the floor, no row | `scripts/heavy-drift-report.sh`, called by every full smoke run | this doc §2–§3 |
| **flaky** — order/determinism, host-independent | **filed as its own task** (see §4) — NOT covered by a criterion, and deliberately not by `CriterionHostCondition` | task `01a0b191` |

Prior art for the record, since a reader may wonder whether the flaky class is
merely unaddressed: this repo has produced real flake work before — `01a0aa22`
/ `01a0aba1` (one shared root across layouts, map-order dependent),
`01a091f1` ("diagnose with runs, not reasoning"), `01a0ac24` (a
contention-flaky device A/B, closed by reclassifying it off the merge path),
`01a0920b` (a memory-store first-open race). What has never existed is a
*criterion* for instability — `heavytest` has no `CriterionFlaky` and should not
grow one (see §4).

## 1. What is actually defined once (no second copy)

`scripts/lib/heavy-threshold.sh` is the ONE definition of the shared values:

| shell name | value | Go side |
|---|---|---|
| `heavy_slow_threshold_s` | `5` (the measured floor) | — (stated in this doc) |
| `heavy_inventory_rel` | `testdata/heavy-tests.tsv` | `heavytest.InventoryRel` |
| `heavy_gate_packages` | `internal/standards internal/stylegate` | the set the Go gate protects |
| `heavy_derive_cmd` | `scripts/heavy-derive.sh` | — |
| `heavy_stream_tests_floor` | `100` (secondary, cheap guard) | — |
| `heavy_golist_format` | the `go list` format naming every package with test files | — |

`internal/standards/heavy_drift_report_gate_test.go` asserts the shell side and
the Go side agree (`InventoryRel`), that the floor is assigned exactly once, and
that every consumer sources the lib rather than re-declaring the value — so a
copy-paste cannot drift.

## 2. The drift catch (runs by itself)

Every **full** smoke run reports the drift, because `scripts/test-budget.sh`
calls `report_unclassified_slow()` right after its summary. It stays quiet where
a run cannot produce evidence: with `GOPI_HEAVY_TESTS` set every heavy test RUNS
(its duration then says nothing about the classification), and a narrowed scope
(`make test-budget PKGS=…`) reads a partial stream.

It **reports, it does not fail**: a threshold alert on a shared, load-varying
host is a lead for a human (at load 42–63 this suite's wall time triples), and a
probe that fires on host noise is one a reader learns to ignore — worse than no
probe. The mechanical half stays where it is deterministic: the inventory gate
fails on drift between the inventory and the call sites.

## 3. The recipe (re-derive the inventory from a real run)

```bash
# 1. produce a stream from a FULL smoke run (writes the run's own go test -json)
scripts/test-budget.sh ./...                 # ~25m budget/package; GOPI_TEST_TIMEOUT overrides

# 2a. see the drift the inventory cannot see (also printed by the run above)
scripts/heavy-drift-report.sh                # default: the latest stream under the scratch queue
scripts/heavy-drift-report.sh path/to/run-1789534773.json

# 2b. print the full candidate set, ready to paste as inventory rows
scripts/heavy-derive.sh
scripts/heavy-derive.sh path/to/run-1789534773.json
```

`heavy-derive.sh` excludes what may never be a row (gate packages, subtests —
`heavytest.Require` is a test-body statement, so only top-level tests qualify,
already-classified tests) and **refuses a narrowed stream**. The refusal is a
COVERAGE test, not a count: the stream must report a package-level result for
every package that carries test files (`go list` is asked for that set), because
a count cannot tell a whole suite from a slice of one — measured 2026-09-18, the
standards-only run carried 244 top-level tests, which any affordable count floor
would wave through. Deriving from a partial run shrinks the classification to
whatever that run happened to touch; the count floor (100) stays as a second,
cheap guard against a truncated stream.

### Observed output (real run, 2026-09-18, load ~19)

```
$ scripts/heavy-derive.sh
heavy classification floor: 5s   (source stream: /home/user/workspace/gopi-test-scratch/queue/test-budget/last.json)
top-level tests in stream:  1677
already classified (in heavy-tests.tsv): 41

CANDIDATES — test<TAB>criterion<TAB>source (pick the criterion by WHY it is heavy;
it must be one of the heavytest.Criterion* constants):
    16.97s  ledgerwindow       TestBridgeRejectsAnInvalidWindow
    16.87s  agent              TestDaemonSpawnFromWorktreeEscapesMeasuredCwd
    12.37s  agent              TestCLIContractEachSubcommandRoutesToItsHandler
     7.69s  agent              TestMemorySyncE2EBidirectional
     7.28s  agent              TestResolveRawLog_MissingExits
     7.25s  agent              TestCodeSearchBackendFindsDefinition
     6.28s  agent              TestMemorySyncStalePeerUpgradeHint
     6.22s  agent              TestMemorySyncWhitelistRefusal
     5.00s  scratch            TestBaseSweepFixture_MixedBaseCollectsOnlyOurs
```

```
$ scripts/heavy-drift-report.sh
heavy classification floor: 5s — 9 test(s) at or above it, 9 without a heavy-tests.tsv row
  UNCLASSIFIED   16.97s  ledgerwindow       TestBridgeRejectsAnInvalidWindow
  ... (the same nine tests, named one per line)
  -> either classify it (heavytest.Require(t, Criterion*) + an inventory row)
     or accept it, and re-derive the whole list from a real run:
     scripts/heavy-derive.sh
```

The two scripts agree on the same nine tests from the same stream — that
agreement is the recipe's own check, and it is what a reader quotes when adding
a row.

**What "reproduces the TSV" means here.** The recipe reproduces the inventory's
SET, not its rows: the derive run prints `already classified (in heavy-tests.tsv):
41`, which is exactly the row count the checked-in file carries (41 data rows,
`grep -vc '^#' testdata/heavy-tests.tsv`), and those 41 are excluded from the
candidates precisely because the inventory names them — so the re-derivation
recognizes today's classification and shows what it MISSES (the nine). The
nominal 44 rows → 41 call sites arithmetic behind that file is in
`devdoc/heavy-tests-rule27-gate.md` ("the arithmetic must close"); re-running
the recipe on a fresh full-suite stream is how a reader re-checks it.

### The catch, verified on the closing run (2026-09-18)

Four checks were run, because each one is a claim the report makes about itself:

```
# 1. narrowed scope -> the report must stay SILENT (no evidence from a slice)
$ make test-budget PKGS=./internal/standards/     # exit 0
  --- summary (38s wall, load 22.86 25.57 17.22 -> 21.80 24.96 17.37) ---
  packages: 3 finished, 0 with a failure
  slowest 15 tests (wall-clock seconds):
     67.80  agent        TestMergerRestartReplacesTheLoopWithoutOverlap
     ...
  stamp: … pkg=./internal/standards/ … exit=0 outcome=pass
  # -> no `heavy classification floor:` line: report_unclassified_slow returned
  #    before calling the report, as designed.

# 2. a missing stream -> loud, not silent (Rule 15b)
$ scripts/heavy-drift-report.sh /nonexistent.json ; echo $?
heavy-drift-report: no stream at /nonexistent.json — run `scripts/test-budget.sh ./...`
heavy-drift-report: (or pass a preserved run-<epoch>.json path) first.
2

# 3. a narrowed stream -> the derive script REFUSES it
$ scripts/heavy-derive.sh <standards-only run>.json ; echo $?
heavy-derive: the stream reports no result for 36 of 37 test packages — that is a
narrowed or truncated run, not a suite; refusing to re-derive from it …
2

# 4. the wiring gate RED-PROOFS the call, not the name
$ <delete the `report_unclassified_slow` call line from scripts/test-budget.sh>
$ go test -count=1 ./internal/standards/ -run TestHeavyThresholdReportIsWiredToTheSmokeRun
--- FAIL: … scripts/test-budget.sh never CALLS report_unclassified_slow on a
    line of its own — a report nobody runs leaves the inventory silent …
```

Check 4 is the one that mattered: the gate's FIRST version counted occurrences
of the function's name and required ≥2, so deleting the call left the count at 2
(the definition line plus a comment) and the gate stayed **green** — a gate that
could not catch the single failure it was written for. It now requires a call
statement on a line of its own. The false negative was found by red-proving, not
by reading the gate.

### Adding a row (the one manual step)

Classification is a **judgement about WHY** a test is heavy, so it is not
automatic: pick the criterion that names the cost (`CriterionDiskFixture`,
`CriterionWideParallel`, `CriterionDeviceExec`, `CriterionHostCondition`,
`CriterionE2ENetwork`), add `heavytest.Require(t, Criterion*)` as the test's
first statement, add the row to `testdata/heavy-tests.tsv`, and run
`go test ./internal/standards/` — the inventory gate fails on either half being
missing.

## 4. The flaky class — dispositioned, not silently absorbed

**It is NOT covered by `CriterionHostCondition`.** The two are different claims
and the difference is the whole point of naming the class:

- `CriterionHostCondition` is about a verdict that depends on **ambient host
  state** (wall clock, sudo availability, current load) — skipping it on smoke is
  correct because the host, not the code, decides it.
- **flaky is about determinism**: the same code on the same host passes and fails
  across repetitions (map-iteration order, one shared root between cases, a
  wall-clock race in the test's own seeding). Such a test is a **defect**, and
  classifying it heavy would *hide* it — a skipped flake reads as a passing
  green, which is exactly the `t.Skip`-without-a-reason failure this repo counts.

So the flaky class is an **honest, dispatchable remainder**: it has its own
pending task, `01a0b191` —

> flaky class (order/determinism, host-independent) has no criterion and no
> catch: add a root-cause registry + a repetition-based (never device-load)
> catch mechanism + a drift gate

That task owns the mechanism (a repetition-based catch, never device load —
GOPI.md Rule 27) and the registry of known flakes. Prior art it can build on, all
already in this tree: `01a0aa22` / `01a0aba1` (a real flake driven to its root
cause: one shared root across layouts, map-order dependent), the diagnosis brief
`briefings/brief-tools-flake-diagnosis.md`, and the precedent that a flake is
diagnosed with runs, not with reasoning. Nothing in this branch pre-empts it:
this branch classifies **duration**, it does not classify **instability**.

## Not Done (honest)

- **No row was added by this change.** The recipe *names* the nine candidates of
  the 2026-09-18 run; deciding each one's criterion is a judgement about a real
  cost, and the inventory gate is the right place for it, not a script. The list
  above is the dispatchable input.
- **The flaky class has no mechanism in this branch** — by design (§4). It is a
  criterion-shaped hole in `internal/heavytest` (there is deliberately no
  `CriterionFlaky`: a flake must not be skippable), and `01a0b191` owns it.
- **The report is non-fatal.** It does not make a smoke run red on a threshold
  crossing; the deterministic gate (inventory ↔ call sites) does the enforcing.
