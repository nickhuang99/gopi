<!-- doc-class: normative -->
# Heavy-test classification + the mechanical Rule 27 gate (2026-09-16)

Two things landed in one change, because they are the same failure seen from two
sides — *work that damages the shared host must not run by default, and must not
be manufactured to satisfy a test*:

- **A — heavy tests are OFF by default.** `heavytest.Require(t, criterion)` as a
  test's first statement skips it unless `GOPI_HEAVY_TESTS` is truthy, so
  `go test ./...`, `make test` and every merge/verify run are smoke-only;
  `make test-heavy` is the ONE named opt-in.
- **B — Rule 27 is now a check, not a paragraph.** `internal/testutil/rule27_gate_test.go`
  fails on a sync/O_DIRECT writer, on an unbounded loop wrapping a device writer,
  on a `dd` naming a raw device, and on a committed synthetic-pressure helper —
  scanning the working tree (untracked and dot-directories included).
- **C — the retired acceptance instrument.** The epic `01a0a63e` acceptance
  clause `≥90% /proc/pressure/io full avg300` is withdrawn and replaced by a
  direct measurement of the implicated call; the diagnosis it was attached to is
  kept verbatim. See `devdoc/ledger-dsn-cmd-agent-contract.md` §"Acceptance
  instrument: the pressure clause, REPLACED".

## 1. Criterion: what makes a test heavy

A test is heavy when **a reasonable person would not want it running on a shared
machine while other work proceeds**. The five criteria are constants in
`internal/heavytest` (never bare literals — Rules 1b/13/14), and the inventory's
per-entry criterion is one of exactly these five:

| constant | criterion | why |
|---|---|---|
| `CriterionDiskFixture` | `disk-fixture` | minutes of real device work: the ledger's first-open schema commit and per-fixture scratch DBs each pay an fsync |
| `CriterionWideParallel` | `wide-parallel` | spawns N concurrent processes sweeping one tree — contends for the host, not just the package |
| `CriterionDeviceExec` | `device-exec` | drives a real external process end to end (headless chromium, daemon/worker spawn, git/build) |
| `CriterionHostCondition` | `host-condition` | the verdict depends on ambient host state (wall clock, sudo, current load) rather than on the code |
| `CriterionE2ENetwork` | `e2e-io` | an end-to-end contract whose cost is host/network I/O, not the assertion |

Two selections that follow from the criterion rather than from taste:

- **The measured floor is ≥5 s per test.** The 2026-09-16 baseline stream
  (`run-1789534773.json`, 3553 tests) has 45 tests at or above 5 s; that set is
  the inventory. Nothing below it is classified — a classification that grows
  past the measurement is a guess.
- **Gate packages are NEVER heavy.** `internal/standards` and `internal/stylegate`
  stay on the smoke path: a merge must not be able to land with its own gates
  skipped. `internal/standards/heavy_inventory_gate_test.go` FAILS if anyone
  classifies a test in a gate package as heavy — that is the line between
  classifying cost and weakening a gate.

## 2. Inventory (reviewable, machine-checked)

`testdata/heavy-tests.tsv` — one row per heavy call site, `test \t criterion \t source`.
It is both human-reviewable and machine-checked: `TestHeavyTestInventoryMatchesItsCallSites`
(`internal/standards`) fails on

- a `heavytest.Require` call site with **no** row (an unlisted heavy test is a
  silent skip, which this repo counts as a defect),
- a **stale** row with no call site (renamed/moved/deleted test),
- a criterion that is not a `Criterion*` constant (Rule 1b),
- a heavy test in a gate package,
- a second reader of the opt-in env name, or a `make test` that is not smoke-only
  (`heavyAssertOneOptInName`, `heavyAssertMakeTargetsSplit`).

Count and shape at the time of this change — the arithmetic is exact, so the
inventory can be re-derived from the baseline stream rather than trusted:

```
45 tests ≥5s in the baseline stream
  − 1 in a gate package (internal/standards, TestStandardGates/TestGate6EnvReaderRedProof)
  − 3 subtests (TestCaptureWaitSelector/never_matches,
                TestSweepHomeCollectsUnmarkedQueueChildren/{accept,test-budget})
  = 41 top-level non-gate tests  →  41 inventory rows, 41 call sites
```

Spread over **26 files, 41 call sites in 8 packages** (`git grep -c` on the
call shape; call sites per package): `cmd/agent` 3, `internal/agent` 6,
`internal/scratch` 5, `internal/shot` 2, `internal/signalrec` 2,
`internal/testutil` 14, `internal/tools` 8, `testsuite` 1. Read
`testdata/heavy-tests.tsv` for the authoritative list; the counts here are a
snapshot, the file is the source of truth (and the gate keeps it so).

**A skip is never silent.** Every skip logs
`HEAVY TEST SKIPPED (set GOPI_HEAVY_TESTS=1 to run): <criterion>` and, when
`GOPI_HEAVY_SKIP_LEDGER` names a file, appends `Test<Name>\t<criterion>` to it.
`make test` sets it to `.heavy-skipped.tsv` (git-ignored), so **a smoke run
leaves its skipped set behind as an artifact** — the inventory is not a promise
about the code, it is a record of what the run did not run.

## 3. The merge/verify path is smoke-only, by construction

- Plain `go test ./...` → heavy tests skip. No env var, no tag, no flag needed.
- `make test` → `GOPI_HEAVY_SKIP_LEDGER=$(CURDIR)/.heavy-skipped.tsv scripts/test-budget.sh ./...`.
  Same budget script, same wall-clock/load stamping, smoke scope.
- `make test-heavy` → `GOPI_HEAVY_TESTS=1 scripts/test-budget.sh ./...` — the one
  named opt-in, deliberately run off-peak.
- The verifier's own brief (`internal/tools/verify_gate.go`) states the path's
  scope where the merger's child reads it, not only in this devdoc. Since
  2026-09-18 that statement is the FULL rule — **smoke, BRANCH-SCOPED, never the
  whole tree** — carried by `internal/smokegate` (`Command`, `ScopedCommand`,
  `Doc`) and enforced by `internal/standards`'s
  `TestSmokePathPrescribesNoWholeTreeSuite`, which fails on a whole-tree command
  span in any merge/verify-path briefing (a Go string literal under
  `internal/tools`/`cmd/agent`, or the shipped take-over prompt). See
  `devdoc/smoke-gate-merge-path.md`.

## 4. The three incidents (what the gate is written against)

| date | shape | damage | where it lived |
|---|---|---|---|
| 2026-08-27 | `dd if=/dev/zero of=… oflag=direct conv=fsync` in a loop | `/` filled | worktree script |
| 2026-09-11 | `while :; do dd … conv=fsync; done` (`suite-ab.sh`) | `/` 100% full, hermes killed | worktree script |
| 2026-09-16 | three `iopress.sh` batches × 16 workers, PPID=1, outliving their producer | 48 concurrent O_DIRECT+fsync writers, load 56.42, every `dd` in state D; `/` to 1.5 G free | **hand-written into a worktree's `.evid/` — untracked, invisible to every checked-in gate** |

The third is why the gate scans the **working tree** rather than the index: the
rule written after the second incident did not prevent the third because
`grep -rn 'iopress'` over the repo returned zero hits.

## 5. Where a hand-written generator IS and IS NOT caught (stated, not glossed)

**CAUGHT**

- A file anywhere under the session's own checkout — tracked **or untracked**,
  including `internal/`, `scripts/`, `devdoc/`, `testdata/` and a dot-directory
  such as `.evid/`: exactly where 2026-09-16's generator lived, whenever the gate
  runs in that checkout.
- The generated-script form: a script written at run time into a temp path is
  caught **provided the walk is pointed at it** — the red proof does exactly
  that (`TestRule27GeneratedScriptIsCaughtRedProof`), so the generated form is
  demonstrated rather than asserted.
- The Go half: a `dd` invocation in a `*_test.go`, or an unbounded `for {}`
  wrapping a device writer.
- A rename cannot launder it: a helper is reported by NAME (in the file's own
  name, `synthetic-pressure-helper-file`) *and* by its inner `dd`, so
  `while :; do dd …` under a friendlier filename is the same finding. Both halves
  are red-proved: `TestRule27GeneratedScriptIsCaughtRedProof` plants
  `iopress.sh` (content) and `stress_helper.sh` (a device-free body whose FILE
  NAME is still a generator's) and requires a hit from each.

**REPORTED (not failed)**

- A generator sitting in a **sibling** checkout under `~/workspace` (measured
  2026-09-16: the original batch was still parked in `gopi-firstopen/.evid/`).
  It belongs to another agent's worktree; failing this repo's gate for a foreign
  file is an unactionable red. The ambient half PRINTS what it found, bounded, on
  every run — a report, deliberately not a failure.

**NOT CAUGHT** (the honest limit of a text gate)

- A file written **outside** the checkout and outside `~/workspace`.
- A generator that never names `dd`/`fio`/`stress` — e.g. a Go program issuing
  O_DIRECT writes itself, or an `os.File.Sync()` loop. The gate matches the
  vocabulary the incidents used; it does not claim to be a syscall sandbox.
- A `dd` that is genuinely bounded and sync-free (a fixture writing 4 KiB) is not
  reported — that is a real test doing real work, not a load generator.

Both "not caught" limits are the reason PROJECT.md Rule 27 stays prose **as well as**
a check: the check closes the incident shapes, the rule states the ban.

## 6. The retired acceptance instrument (C)

The epic's clause `≥90% /proc/pressure/io full avg300` is a **kernel** metric, and
the cheapest way to satisfy it is to flood the device rather than to measure the
ledger — which is what happened. It is withdrawn as an acceptance input and
replaced, in `devdoc/ledger-dsn-cmd-agent-contract.md`, by a direct instrument:

> **Acceptance (direct instrument — supersedes any `/proc/pressure/io` figure).**
> The acceptance evidence is the run's OWN artifacts: (a) each package's wall
> time and the slowest ≤15 tests, from that run's `go test -json` stream
> (`run-<epoch>.json` in the scratch queue's `test-budget/` directory), and (b)
> the duration of the implicated call itself — the first-open schema batch's
> `tx.Commit()` in `execSchemaBatchOnce` (`internal/tools/memory.go`), as
> attributed by the stall watchdog's goroutine dump and by the per-test elapsed
> of the ledger-opening fixtures. No kernel pressure metric is an acceptance
> input, and no test may manufacture pressure to produce one (PROJECT.md Rule 27 —
> now mechanically gated).

The **diagnosis is unchanged**: every fixture's ledger open pays the host's disk
cost, the first-open `schema` batch is ONE transaction whose commit blocks, and
~360 such fixtures sum past the 10-minute package budget. Only the instrument
changed.

## 7. Zero-dependency, no new tooling

Both checks are stdlib-only Go tests (`go/parser`+`go/ast` for the inventory,
`regexp`+`filepath.Walk` for Rule 27) inside the existing `go test ./...` run — no
linter, no new module, no CGO, in the `internal/standards` tradition (Rules
16–21). Non-repo test scripts that use a device writer remain the scripts audit's
business (`internal/testutil/scripts_gate_test.go`); this gate ADDs the
post-index forms (untracked, dot-directories, generated), it does not replace it.
