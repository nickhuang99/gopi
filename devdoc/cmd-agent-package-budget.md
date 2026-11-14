<!-- doc-class: normative -->
# `go test ./cmd/agent/` — the anonymous package FAIL, measured and classified (2026-09-21)

> **Provenance.** `TestDocAnchorsResolveInNormativeDocs` was retired on 2026-10-22 in `5a35f61` (deleted with `internal/docclass`), so a `file.go:NNN` reference in this page is a landing-time record, not an anchor a live gate enforces — nothing resolves one today. The line numbers stay useful to a reader; they are not checked by the suite. Citations below are quoted as they were written — where one of them reads as present tense ("is RED", "resolves", "fails"), read it as the state of THAT run, never as a gate this tree still has.

**Task** `01a0c04b` (repair dispatch; the row it repairs is `01a0a63e`).
**Branch** `fix/cmd-agent-package-budget` (worktree
`/home/user/workspace/gopi-cmdagent-budget`, a linked worktree of the main repo
`/home/user/work/GoPi`, fresh off master `2fdc65e4` — Rule 23).
**Host** `hostb-Laptop`, 4 cores, load 1.5–3.4 during the runs (recorded per
run, below). **No load of any kind was generated** (Rule 27): every number here
is `go test -json` wall clock on an ambient host.

---

## 1. The headline

The epic's anonymous red is **two different defects wearing one output shape**,
and neither of them is a hang:

| # | configuration | wall / verdict | what it actually is |
|---|---|---|---|
| **M1** | whole package, **credential env stripped** (`env -u DEEPSEEK_API_KEY -u ANTHROPIC_AUTH_TOKEN`), Go's default 10m | **13.9 s**, `FAIL github.com/nick/gopi/cmd/agent 13.963s`, **no `--- FAIL:` line**, no panic, 46 of 1129 tests ever ran | the test BINARY dies mid-suite: `TestIsBrewSubcommand` calls `isBrewSubcommand(["brew"])`, which **executes** the subcommand, whose credential resolution fails and calls **`os.Exit(1)`** |
| **M2/M3** | identical, same env | **10.1 s**, same anonymous shape, 46 tests | deterministic — not a flake, not a timeout |
| **M5** | whole package, **ambient env (credentials present)**, Go's default 10m | **447.354 s**, exit 1, **5 NAMED `--- FAIL:` tests**, `1129/1129` terminated | the real, honest package cost — 152 s UNDER Go's 600 s default |
| **M6** | same as M5, **after the classification this branch lands** | see §5 | the default (smoke) path, heavy tests skipping themselves |

Read together: **the `600s`/anonymous-FAIL shape reproduced by the epic is the
credential-stripped binary exit (M1–M3)**, and the true whole-package cost is
**447 s**, which the epic's own 600 s default accommodates. What was *not*
accommodated is a whole-package run at 447 s with 19 tests at or above the
repo's 5 s heavy floor still running unconditionally on every merge/verify run —
which is what this branch fixes (§4).

### Why the anonymous FAIL is not a timeout (the discriminator)

A `go test -json` stream names a killed test binary by what it *lacks*. In M1/M2:

```
"Action":"run"   TestIsBrewSubcommand      <- last event of the run
"Action":"output"                          <- package-level, no Test field
   gopi brew: brew: deepseek: ANTHROPIC_AUTH_TOKEN not set (env, DEEPSEEK_API_KEY, or ~/.gopi/settings.json)
"Action":"fail"  Package=…cmd/agent  Elapsed=13.963

$ grep -c 'panic: test timed out' /home/user/workspace/_evid/cmd-agent-budget/m1.json   -> 0
$ grep -c '"Action":"fail"' with a Test field     -> 0
```

The control (same test, same binary, different env):

```
$ go test ./cmd/agent/ -run '^TestIsBrewSubcommand$' -count=1 -v      # ambient env
--- PASS: TestIsBrewSubcommand (0.00s)        ok …cmd/agent  0.557s

$ env -u DEEPSEEK_API_KEY -u ANTHROPIC_AUTH_TOKEN go test … -run '^TestIsBrewSubcommand$' -json
output  TestIsBrewSubcommand  gopi brew: brew: deepseek: ANTHROPIC_AUTH_TOKEN not set …
fail    (package)             0.955           # the process is gone
```

The mechanism, read from the production code: `cmd/agent/cli_brew.go`
`isBrewSubcommand` → `runBrewSubcommand` → `tools.NewBrewEngine()` →
`deepseek.ResolveClaudeAPIKey` (`internal/deepseek/claude_client.go:649`), whose
error path is `fmt.Fprintf(os.Stderr, "gopi brew: %v\n", err); os.Exit(1)`
(`cli_brew.go:88`). `os.Exit` inside a test goroutine ends the whole test
binary, so no test can report anything — the package gets a FAIL nobody wrote.

**This is the repair half of the epic, and it has a landed fix on another
branch** (read, per Rule 23 — never continued on): `fix/brew-subcommand-test-hermetic`
commit `b3a59e1a` *"fix(cmd/agent): TestIsBrewSubcommand asserts the predicate,
not the execution"* — it splits the pure predicate `brewSubcommand` out of the
dispatching `isBrewSubcommand` and points the test at the predicate. That branch
is **not merged into master** at `2fdc65e4` (verified below), which is why the
defect is still reproducible on this branch's base. **This branch does not
re-fix it** (Rule 23: never edit on top of someone else's attempt; Rule 26: the
row is a repair dispatch, and I name the gap instead of claiming it).

---

## 2. The direct measurement (the epic's deliverable 1)

Command — no load, no `-timeout` override, ambient environment, raw stream to a
file (nothing between the tool and the evidence):

```
uptime -> 09:58:38  load average: 2.00 2.37 2.34   (4 cores, sda3=/ ext4, sda4=/home ext4)
nice -n 10 go test ./cmd/agent/ -count=1 -json > /home/user/workspace/_evid/cmd-agent-budget/m5.json 2>&1     # Go's default 10m
```

**Per-package wall time — 447.354 s**, from the stream's own package event:

```
{"Action":"fail","Package":"github.com/nick/gopi/cmd/agent","Elapsed":447.354}
```

1129 top-level tests, **1129 terminated** (`run` events without a terminal
`pass`/`fail`/`skip` event: **0**), 71 skips, span 446.8 s, sum of test elapsed
440.7 s. There is **no** `panic: test timed out` line in the stream.

**Slowest 15 tests** (`Action:"pass"`, `Elapsed`):

| # | elapsed (s) | test |
|---|---|---|
| 1 | 20.00 | `TestWorkerDrainStillLetsTheRoundFinish` |
| 2 | 16.16 | `TestMergePassWithdrawsOnlyTheRedRepo` |
| 3 | 14.64 | `TestMergeCleanBatchIsVerifiedAndChangesNothing` |
| 4 | 13.14 | `TestVoiceSubcommandHelpSmoke` |
| 5 | 10.72 | `TestStrandedScanReachesADeepRow` |
| 6 | 9.95 | `TestMergeFastWithdrawsABatchThatOnlyFailsVET` |
| 7 | 9.90 | `TestWriteToolAnnotatesTeachingDocsInAuditedTrees` |
| 8 | 9.72 | `TestSweepRecyclesADeepStrandedWorktree` |
| 9 | 9.32 | `TestMergePassVerifiesOncePerPassNotPerBranch` |
| 10 | 8.62 | `TestWorkerBusyRoundResetsIdleStreak` |
| 11 | 8.41 | `TestMergeNoVerifySkipsTheMeasurement` |
| 12 | 8.28 | `TestMergeFastWithdrawsABatchThatDoesNotBuild` |
| 13 | 8.20 | `TestMergeWithdrawalIsAResetNotARevert` |
| 14 | 6.87 | `TestServeRebuildNoHandleStillMeasuresAndKeeps` |
| 15 | 6.61 | `TestServeRebuildRevertsIntroducedVetFailureThroughTheCloseLoop` |

Re-derivable with `scripts/heavy-drift-report.sh /home/user/workspace/_evid/cmd-agent-budget/m5.json`.

### The named slow call, with its measured duration

The epic names the candidate (`execSchemaBatchOnce` → `tx.Commit()`). Rather
than infer it, the repo's own phase probe was run (`internal/tools`,
`TestZZFirstOpenProbe`, gated by `GOPI_FIRSTOPEN_PROBE_DIR`; a fresh ledger,
phase by phase, with a per-thread kernel-state histogram):

```
arm=working-path fs=block-backed filesystem type 0xef53 (device-backed) sync=NORMAL fresh=0
    phase=openDB               wall=   0.000s
    phase=enableWAL            wall=   0.149s   <- blocked-in fsync wchan=jbd2_log_wait_commit  49 samples
    phase=ensureSchema         wall=   0.059s   <- blocked-in fsync wchan=jbd2_log_wait_commit  17 samples
    phase=ensureFeedbackSchema wall=   0.004s
    phase=ensureTasksSchema    wall=   0.008s
    phase=close                wall=   0.106s
```

So on this host **at this moment the first open measured 0.15–0.33 s end to
end, and the ONLY phase that parks in `fsync`/`jbd2_log_wait_commit` is
`enableWAL` (the journal commit), not `execSchemaBatchOnce`'s `tx.Commit()`
(0.059 s, 17 samples)**. The schema batch's own commit is real device work but
it is not minutes. Stated honestly: the epic's named call is **not** what these
cmd/agent fixtures are paying for today — the ledger-first-open cost is a
~0.15 s floor per fresh ledger, and the tables above show where the seconds
actually go (real child processes and real repositories: `go build ./...` +
`go vet ./...` per merge pass, `git init`/`commit`/`worktree` chains, a
re-exec'd `gopi` binary, and deliberately durable waits such as
`TestWorkerDrainStillLetsTheRoundFinish`'s bounded "did the drain kill the
round?" window). Extrapolating / re-measuring the ledger was rejected as a
guess (Rule 27's note: measure our own code, and measure the right call).

### The 5 NAMED failures inside M5 (attribution only — not this branch's work)

| test | message (verbatim, truncated) |
|---|---|
| `TestTaskListReadPathE2E` | `sweep-zombies E2E needs a gopi binary: set GOPI_BIN to THIS tree's build … ../../bin/gopi` — **the sibling row `01a0c049`, left alone (Rule 26)** |
| `TestSweepZombiesCLI_DryRunTouchesNoNotifySink` | same `GOPI_BIN`/`../../bin/gopi` requirement |
| `TestSweepZombiesCLI_ReportNotifiesOperator` | same `GOPI_BIN`/`../../bin/gopi` requirement |
| `TestCompletionCandidateSetsCoverTopLevel` | `topLevelCommandNames contains "config" but no dispatcher handles it` — **pre-existing on master**: `gopi config` is a merged verb (`a2ef3c6a`, "feat(config): add 'gopi config show' read-only verb") whose dispatch predicate is not in the completion table's handler map |
| `TestTopLevelHelpCoversEveryDispatchedSubcommand` | `dispatch predicate ConfigSubcommand has no help mapping … add its help entry to internal/config/help.go` — **same root cause, pre-existing on master** |

All five fail at `0.00s` — they are assertions, not slowness. They are named,
not repaired (Rule 26: reported, dispatchable, each with its own row or an
existing one).

---

## 3. What landed: the remedy

Of the epic's two allowed remedies, **the classification one landed**: the
whole-package run does not exceed Go's default in the honest configuration
(447 s), but 20 tests at or above the repo's declared 5 s heavy floor ran on
every merge/verify pass, and the epic's demand is that a whole-package run stop
reporting an anonymous result. So:

**(a) The offenders are classified heavy per Rule 27** — `heavytest.Require` as
the first statement, criterion from the `heavytest.Criterion*` set (Rule 1b),
default-OFF, auditable, excluded from the merge/verify path:

| test | criterion | file |
|---|---|---|
| `TestWorkerDrainStillLetsTheRoundFinish` | `CriterionDeviceExec` | `cmd/agent/worker_stop_forward_regression_test.go` |
| `TestMergePassWithdrawsOnlyTheRedRepo` | `CriterionDeviceExec` | `cmd/agent/merge_verify_test.go` |
| `TestMergeCleanBatchIsVerifiedAndChangesNothing` | `CriterionDeviceExec` | `cmd/agent/merge_verify_test.go` |
| `TestMergeFastWithdrawsABatchThatOnlyFailsVET` | `CriterionDeviceExec` | `cmd/agent/merge_verify_test.go` |
| `TestMergePassVerifiesOncePerPassNotPerBranch` | `CriterionDeviceExec` | `cmd/agent/merge_verify_test.go` |
| `TestMergeNoVerifySkipsTheMeasurement` | `CriterionDeviceExec` | `cmd/agent/merge_verify_test.go` |
| `TestMergeFastWithdrawsABatchThatDoesNotBuild` | `CriterionDeviceExec` | `cmd/agent/merge_verify_test.go` |
| `TestMergeWithdrawalIsAResetNotARevert` | `CriterionDeviceExec` | `cmd/agent/merge_verify_test.go` |
| `TestServeRebuildNoHandleStillMeasuresAndKeeps` | `CriterionDeviceExec` | `cmd/agent/cli_tasks_rebuild_vet_revert_test.go` |
| `TestServeRebuildRevertsIntroducedVetFailureThroughTheCloseLoop` | `CriterionDeviceExec` | `cmd/agent/cli_tasks_rebuild_vet_revert_test.go` |
| `TestServeRebuildKeepsPreExistingVetFailureThroughTheCloseLoop` | `CriterionDeviceExec` | `cmd/agent/cli_tasks_rebuild_vet_revert_test.go` |
| `TestStrandedScanReachesADeepRow` | `CriterionDiskFixture` | `cmd/agent/cli_tasks_stranded_sweep_test.go` |
| `TestSweepRecyclesADeepStrandedWorktree` | `CriterionDeviceExec` | `cmd/agent/cli_tasks_stranded_sweep_test.go` |
| `TestWriteToolAnnotatesTeachingDocsInAuditedTrees` | `CriterionDeviceExec` | `cmd/agent/write_tool_ownership_e2e_test.go` |
| `TestWorkerWaitsAndPicksUpNewTaskWithoutRestart` | `CriterionDeviceExec` | `cmd/agent/serve_worker_split_test.go` |
| `TestWorkerOnceTickE2E_DisposesZombieWithZeroWorkerDaemons` | `CriterionDeviceExec` | `cmd/agent/task_lifecycle_tick_test.go` |
| `TestWorkerDaemonInheritLockRefusalInARealChildLeavesTheLockFDAlive` | `CriterionDeviceExec` | `cmd/agent/borrowed_fd_worker_child_test.go` |
| `TestVoiceSubcommandHelpSmoke` | `CriterionDeviceExec` | `cmd/agent/cli_voice_review_test.go` |
| `TestWorkerServesEachTaskInItsOwnRepoAcrossConsecutiveRounds` | `CriterionDeviceExec` | `cmd/agent/task_executor_repo_cwd_e2e_test.go` |
| `TestWorkerIdleExitZeroWaitsForever` | `CriterionDeviceExec` | `cmd/agent/cli_tasks_worker_idle_exit_test.go` |
| ~~`TestWorkerBusyRoundResetsIdleStreak`~~ | — | **reverted: it is a REGISTERED FLAKE** (`testdata/flakes.tsv:46`) — see (c) |

**Why each criterion is honest, per file:** the merge/serve/rebuild set drives a
real repository through `go build` + `go vet` + `git` chains (an external
process end to end); the stranded-sweep and worker-idle set drives the worker
loop over fresh ledgers and real `git worktree` fixtures; `TestVoiceSubcommandHelpSmoke`
re-executes the built `gopi` binary four times; `TestWorkerDrainStillLetsTheRoundFinish`
spends a bounded, deliberate wall-clock window. None of them is heavy because
"it felt slow" — each spends the time on a device or a child process.

**(b) A second wave, from M6's own stream.** The drift report on the
*post-classification* run (`scripts/heavy-drift-report.sh …/m6.json`) named four
tests that crossed the 5 s floor, and the two halves of that finding are NOT
symmetric:

| test | M6 elapsed | decision |
|---|---|---|
| `TestWorkerServesEachTaskInItsOwnRepoAcrossConsecutiveRounds` | 5.84 s | **classified** (`CriterionDeviceExec`) — it drives the real `gopi` binary through consecutive worker rounds over its own fixture repos |
| `TestWorkerIdleExitZeroWaitsForever` | 5.43 s | **classified** (`CriterionDeviceExec`) — the worker loop with an unbounded idle exit, bounded only by a round cap |
| `TestTmpDirSettingsValueReachesTheSpawnedChild` | 17.03 s | **accepted, NOT classified** — see below |
| `TestTmpDirSettingsValueAmbientFallbackLeaks` | 17.03 s | **accepted, NOT classified** — same reason |

The two `TestTmpDirSettingsValue*` cases are **host-configuration GATES** (Rule
22): they read the OPERATOR's real `/home/user/.gopi/settings.json`, require `tmpdir` to
resolve off the `/` partition **by measured device id**, and drive a spawned
child whose temp files are observed from both sides. They were classified for one
pass and **deliberately reverted**: the merge/verify path is exactly where a
configuration-gate must run, and default-skipping a gate to make a
classification land is the "weakening a gate" move Rule 27's companion text
forbids. They stay above the floor, on the smoke path, with the reason recorded
here and named by every drift report until someone re-measures them.

**(c) A classification REVERTED because the repo's own gate caught it.**
`TestWorkerBusyRoundResetsIdleStreak` (8.62 s) was classified in the first pass
and **reverted** after `internal/standards TestFlakeRegistryMatchesTheTestsItNames`
refused it, verbatim:

```
flake_registry_gate_test.go:185: testdata/flakes.tsv:46 registers
TestWorkerBusyRoundResetsIdleStreak, which also classifies itself
heavytest.Require — a flake is a DEFECT, not a heaviness class: a skip reads as a
passing green and hides exactly the defect this registry documents (fix the
flake or remove the classification; never both)
```

That gate is right and this report would have been wrong without it: a flaky
test MUST stay on the path that exposes its flake, and it now carries **no**
`heavytest.Require` and **no** inventory row. It remains above the 5 s floor, so
every drift report will keep naming it — which is exactly the visibility the
flake registry wants.

**(d) Two borderline tests deliberately NOT classified.**
`TestStrandedScanCapsAndDedupes` (3.09 s) and
`TestStrandedRowInvisibleToTheNewestScanAlone` (4.49 s) sit below the 5 s floor;
they were classified in the first pass and **reverted**, because classifying
below the floor inflates a reviewed list and contradicts the derivation recipe.
They are recorded here as known sub-floor members of the shared-cost family.

**(e) The inventory stays auditable and bidirectional.** 20 rows appended to
`testdata/heavy-tests.tsv` (103 → 123 rows net: 19 in the first pass, 2 in the M6 second pass, 1 reverted). The gate
`internal/standards TestHeavyTestInventoryMatchesItsCallSites` checks both
directions (call site without a row = defect; row without a call site = stale),
so the classification cannot drift from the code.

---

## 4. Proof the remedy works (both halves of the switch)

**Gate + build:**

```
$ go build ./...                                              -> exit 0 (BUILD_OK)
$ go vet ./cmd/agent/                                         -> exit 0
$ go test ./internal/standards/ -run 'TestHeavy' -count=1
ok  github.com/nick/gopi/internal/standards  0.905s            # 123 rows ↔ 123 call sites
```

**Drift report on the measured stream** (every slow test accounted for):

```
$ scripts/heavy-drift-report.sh /home/user/workspace/_evid/cmd-agent-budget/m5.json
heavy classification floor: 5s — 19 test(s) at or above it, 0 without a heavy-tests.tsv row
  every slow test is accounted for by the inventory

$ scripts/heavy-drift-report.sh …/m6.json     # the POST-classification run
heavy classification floor: 5s — 4 test(s) at or above it, 2 without a heavy-tests.tsv row
  UNCLASSIFIED   17.03s  agent  TestTmpDirSettingsValueReachesTheSpawnedChild
  UNCLASSIFIED   17.03s  agent  TestTmpDirSettingsValueAmbientFallbackLeaks
```

**Default path (the classification OFF — what a merge/verify run executes):**

```
$ go test ./cmd/agent/ -run '^TestMergePassWithdrawsOnlyTheRedRepo$' -count=1 -v
--- SKIP: TestMergePassWithdrawsOnlyTheRedRepo  (HEAVY TEST SKIPPED (set GOPI_HEAVY_TESTS=1 to run))
```

**Declared path (the opt-in — `make test-heavy` sets it for the operator) —
ALL 21 classified tests, in one run:**

```
$ GOPI_HUMAN_TERMINAL=1 GOPI_HEAVY_TESTS=1 go test ./cmd/agent/ -run '<the 21>' -count=1 -v
… 21 × "--- PASS: Test…"   # the then-21-row set; 20 rows remain after the flake revert
ok  github.com/nick/gopi/cmd/agent  161.517s        EXIT=0
$ grep -cE '^--- PASS' classified-on.log   -> 21
$ grep -cE '^--- (SKIP|FAIL)' …            -> 0
```

Every classified test still RUNS and PASSES when the opt-in is declared — the
classification does not disable a test, it moves it off the default path. The
run also prints the refusal disclosure verbatim
(`HEAVY TESTS REFUSED: GOPI_HUMAN_TERMINAL is declared, so the inherited work
signature is stepped over and the heavy set RUNS.`), so the override is never
silent.

**And on the default path (the classification in force, what a merge runs):**

```
$ go test ./cmd/agent/ -run '^(TestWorkerIdleExitZeroWaitsForever|TestWorkerServesEachTaskInItsOwnRepoAcrossConsecutiveRounds)$' -count=1 -v
--- SKIP: TestWorkerIdleExitZeroWaitsForever  (HEAVY TEST SKIPPED (set GOPI_HEAVY_TESTS=1 to run): device-exec: …)
--- SKIP: TestWorkerServesEachTaskInItsOwnRepoAcrossConsecutiveRounds  (… same …)
ok  github.com/nick/gopi/cmd/agent  1.545s
```

Both halves are OBSERVED, and a skip is never silent — it logs a greppable line
and appends to `GOPI_HEAVY_SKIP_LEDGER`.

---

## 5. After: the whole-package number (M6)

Same command as M5, ambient env, Go's default 10m, **with the classification in
force**:

| | wall | tests run / terminated (top-level) | skips | verdict |
|---|---|---|---|---|
| **before** (M5, classification absent) | **447.354 s** | 1129 / 1129 | 71 | 5 named FAILs |
| **after** (M6, classification in force) | **382.513 s** | 1129 / 1129 | 38 | **2** named FAILs |
| **after, re-measured** (M7, same command as M6, second run) | **382.769 s** | 1129 / 1129 | 39 | **3** named FAILs |

(The same three streams carry 1361 / 1363 / 1363 `run` events once **subtests**
are counted — every event terminated in every run. The counts are quoted here as
top-level tests so the three rows are directly comparable; an earlier revision of
this table mixed the two framings, which made M7 look as if it had run 234 more
tests than M6.)

**M7 is the reproducibility control for M6**, and it reproduces to 0.26 s
(382.513 s → 382.769 s on the same command, ambient env, Go's default 10 m, load
1.9–3.9). Both runs terminated every test they started (1129/1129 top-level and
1363/1363 including subtests), both had zero `panic` lines — so the classified run
is stable, not a lucky sample.

**M7's third named FAIL is run-order-dependent and is NOT this branch's**:
`TestCLICodeSearchSelfHealsCorruptIndex` fails inside the whole-package run
(0.02 s) and **passes in isolation on this same branch**
(`go test ./cmd/agent/ -run '^TestCLICodeSearchSelfHealsCorruptIndex$' -count=1`
→ `ok 0.550s`); it is not a classified test, and M6 did not produce it. The
other two (`TestCompletionCandidateSetsCoverTopLevel`,
`TestTopLevelHelpCoversEveryDispatchedSubcommand`) reproduce on the **pristine
master control** in isolation (`/home/user/gopi-pristine-gate` @ `2fdc65e4` →
both `--- FAIL: … 0.00s`) — they are master's pre-existing `gopi config`
completion/help mapping gap, unchanged by this branch.

**The post-classification slow set (M7)** — 3 tests at or above the repo's 5 s
floor, all three *passing*, and all three deliberately unclassified (reasons in
§7):

| elapsed | test | classified? |
|---|---|---|
| 17.07 s | `TestTmpDirSettingsValueReachesTheSpawnedChild` | no — Rule 22 host-configuration GATE (accepted) |
| 17.07 s | `TestTmpDirSettingsValueAmbientFallbackLeaks` | no — same |
| 8.89 s | `TestWorkerBusyRoundResetsIdleStreak` | no — registered a FLAKE (`testdata/flakes.tsv:46`) |

Everything below them is ≤ 4.86 s, and the sum of all terminal test elapsed
times is 389.4 s against the package's 382.8 s wall — i.e. the suite is
**serial-dominated by hundreds of small fixtures**, not by one slow call.

```
$ tail -2 /home/user/workspace/_evid/cmd-agent-budget/m6.log                    # the same command as M5
EXIT=1
 10:20:35 up 67 days, 12:47,  load average: 1.67, 2.04, 2.28
$ # package event
{"Action":"fail","Package":"github.com/nick/gopi/cmd/agent","Elapsed":382.513}
```

**−64.8 s (−14.5 %) with no `-timeout` override, both runs on Go's default 10 m
and both fully terminated (1129/1129 — zero in-flight tests, zero panic).**
The 19 newly-classified tests skip themselves and say so; the remaining verdict
is two named assertions.

Two honest notes on the deltas:

- **Skips went DOWN (71 → 38) even though 19 more tests skip.** That is not a
  contradiction: three fixtures SKIP when no `bin/gopi` exists and RUN when one
  does, and M6 ran after a fixture in the same package had built it — so ~33
  binary-dependent tests executed in M6 that skipped in M5. The 19 classified
  tests skip in both. The wall-time comparison therefore understates the
  classification's effect (M6 did *more* work and still finished 64.8 s
  earlier), which is the conservative direction for this claim.
- **M6's failures are 2, M5's were 5.** The three `../../bin/gopi` fixtures
  (`TestTaskListReadPathE2E`, `TestSweepZombiesCLI_*`) failed in M5 (no binary
  in a clean worktree) and passed in M6 (the fixture that builds `bin/gopi` ran
  before them). The two `gopi config` mapping failures fail in both — they are
  master's, not this branch's, and not order-dependent.

The remedy's point is not the smaller number alone — it is that the run's result
is **legible**: the heavy set names itself as skipped (enumerable, 19 rows in
the inventory), and the remaining verdict is a set of named assertions.

---

## 6. Acceptance, gate by gate

| epic acceptance | result |
|---|---|
| `go build ./...` OK, changed packages' tests green | §4: build exit 0, `internal/standards` gate `ok` |
| the report names the slow call and its measured duration (from `go test -json`) | §2: per-package 447.354 s; slowest-15 table; the ledger phase probe shows `enableWAL` 0.149 s / `ensureSchema` 0.059 s and states plainly that the epic's named `tx.Commit()` is **not** today's cmd/agent cost driver |
| whole-package run within its declared budget and PASS, or heaviness declared/classified | §4/§5: the declared budget at measurement time was `scripts/test-budget.sh`'s **25 m** (the script's own `-timeout 25m`, one reading site) — **re-derived to 45 m on 2026-10-15; see §6's current-value note** — and the whole-package cost is **447.354 s / 382.513 s / 382.769 s**, i.e. ~17 % of the 45 m cap, on a host whose disk latencies fluctuate (§1); the 19 offenders are classified + inventoried + gated; the anonymous shape itself is root-caused (§1) |
| no Rule 27 violation, worktree clean, commit on the branch | no generator written; `git status --porcelain` empty at close-out (litter outside the worktree); commits listed in §8 |

**Test-budget declaration (the epic's "declare it explicitly" half).**
`scripts/test-budget.sh` already owns the ONE per-package budget for this repo
(`test_budget="25m"` **at measurement time — the constant is `45m` today**, env
override `GOPI_TEST_TIMEOUT`, a trip reported as a
TIMEOUT with exit 3), and `internal/smokegate` already names the merge/verify
command (`make test` → that script; branch-scoped `make test-budget PKGS=…`).
This branch does **not** add a second budget constant (Rule 2 would be violated):
the declaration is referenced, and this report plus the classification are what
made the cmd/agent package fit it.

**Measured against that declaration, not against Go's default.** The script's
own invocation then was `go test -count=1 -timeout 25m -json`
(`scripts/test-budget.sh:461` **in the tree this was measured on**; the same line
is `scripts/test-budget.sh:2461` today and reads `-timeout "$test_budget"` —
anchors are a landing-time record, see below),
so the number a merge/verify run was actually capped by was **1500 s**, and the
honest whole-package cost measured here (**447.354 s**, §2; **382.513 s** after
the classification, §5) is **30 %** of it. The epic's `600.594s`/anonymous shape
is reproducible only by running `go test` **directly and bypassing the script**
(which is what M1–M3 did, deliberately, to reproduce the report's premise) —
on the merge path that wall clock would have been a comfortable pass, and what
actually reddens the package there is §1's binary exit, which no budget change
can address.

> **CURRENT VALUE (2026-11-08).** The cap was re-derived on 2026-10-15 to
> **45 m / 2700 s** (`test_budget="45m"`, `scripts/test-budget.sh:147`) after the
> 25 m cap expired mid-suite at ΣElapsed 1493.0 s of 1500 s on a loaded host —
> the post-mortem and the full arithmetic are in that script's own comment above
> the constant, which is the authority. This document's §-references to "25 m"
> and "1500 s" record the tree as it was measured and are kept for that reason
> (Rule 36/§7): the numbers above still hold, the cap they are compared against
> does not. **The relation is now machine-checked rather than re-read**:
> `internal/testutil/testbudget_covers_demand_gate_test.go`
> (`TestDeclaredBudgetCoversTheSlowestPackagesDemand`) asserts that the declared
> constant covers the demand its own derivation records
> (`demand_quiet = 993s`) **and** that this demand exceeds Go's 10 m default —
> which is the machine-checked statement of this document's thesis: **a bare
> `go test ./cmd/agent/` is a cap verdict, not a code verdict.**
>
> **2026-11-08 — the BARE spelling now has its own declared budget, in the
> package itself** (commit titled `test(cmd/agent): declare the package's own
> wall-clock budget` — named by TITLE, not hash: a hash written here would name
> an unpublished tip a rebase rewrites, Rule 38). Everything above is about the *declared
> entry point* (`scripts/test-budget.sh`, 45 m); a developer who types
> `go test ./cmd/agent` bypasses it and gets Go's **10 m**, against a measured
> replay of **579.944 s / 3240 top-level tests / ΣElapsed 575.4 s** — i.e.
> **96.6 % of the alarm, 20.1 s of headroom**, which is the state where a run
> that finished and a run the alarm killed are indistinguishable. A setup-only
> control (`-run '^$'` → **8.08 s** wall) puts 98.6 % of the wall in real test
> work, so this is demand, not fixture overhead. The fix declares the package's
> own budget in `cmd/agent/testmain_test.go` (`declareSuitePackageBudget`):
> **10 m → 25 m**, making demand **38.7 %** of the alarm.
>
> Three properties, each measured rather than argued (`cmd/agent/suite_package_budget_test.go`
> is the gate, `TestSuitePackageBudgetLeavesReadableHeadroom`):
> (a) it **never lowers** a caller's budget — `-timeout 45m` stays 45 m, measured
> as **0** `package budget declared` lines; (b) it never turns a hang into a
> pass, because the alarm still fires, at 25 m (`internal/standards`' own real-timeout
> control still panics, per §8's `timeout_probe.json`); (c) it can be
> **opted out** with `GOPI_SUITE_PACKAGE_BUDGET_SEC`, whose reader exists and
> whose message says the budget was NOT declared (Rule 21).
> The declaration sits BEFORE the stall watchdog on purpose: the watchdog
> derives its own budget as a share of `-test.timeout`, so raising the flag
> afterwards would leave the reporter tuned to the old, smaller budget and libel
> a healthy suite. `flag.Parse()` runs first because `M.Run` parses only
> `if !flag.Parsed()` and the flag reads `0s` until then (both measured).
>
> **What this does NOT claim.** The wall is a long tail, not a few monsters
> (84 tests ≥ 2 s carry 318.6 s; 2900 cases under 0.5 s still sum to ~79 s), and
> the suite is 100 % serial, so the spawn-family route this episodic plan
> proposed (262 `RunGopi`/`StartGopi` call sites) is only **~9.1 %** of the wall
> — batching it perfectly cannot reach a 60 % target. Only `t.Parallel` or a
> scope cut moves the number, and each needs its own decision.

---

## 7. Not Done (honest)

- **The `os.Exit(1)` hang-adjacent defect (`isBrewSubcommand`) is NOT fixed
  here.** It is fixed on `fix/brew-subcommand-test-hermetic` (`b3a59e1a`) —
  verified present, verified NOT an ancestor of this branch's base, verified not
  merged into master. Rule 23/26: named, not re-fixed. While that branch is
  unmerged, a credential-stripped whole-package run still produces the epic's
  anonymous FAIL — an environment-sensitive result that no amount of
  classification can change, because the process dies before any test reports.
  **Recommended follow-up: merge that branch** (it is small and reviewed there).
- **`internal/standards TestDocManifestCoversEveryDevdocFile` — pre-existing red, named and MEASURED, deliberately not repaired here.** On a pristine control worktree from the same master (`/home/user/gopi-pristine-gate` @ `2fdc65e4`, `git status --porcelain` empty): `docclass_gate_test.go:130: devdoc/DOC-INDEX.tsv: 2110 rows cover 2114 files under devdoc/` — RED, naming exactly four tracked-and-unclassified docs that are **tracked on master itself** (`git cat-file -e master:devdoc/<f>.md` → present for each): `cli-as-eyes-and-hands-feasibility.md`, `config-show-verb.md`, `go-toolchain-env-both-nodes-20260920.md`, `handoff-20260920-evening.md`. Repairing them is a corpus-classification task (four judgement calls about foreign docs, each needing a class + a ≥24-char reason, plus a banner where the class is bannered), not this branch's work. What this branch DID fix is its own contribution to that red: `devdoc/cmd-agent-package-budget.md` now carries a **normative** row with a re-verifiable spot-check (`sym cmd/agent/merge_verify_test.go TestMergePassWithdrawsOnlyTheRedRepo` — a declaration that sits at `merge_verify_test.go:372` with this branch's own classification on the very next line, `:373 heavytest.Require(t, heavytest.CriterionDeviceExec)`, so the spot-check breaks when that classified test is renamed, moved out of the package or deleted), inserted in path order between `cmd-agent-contract-reds.md` and `cmd-agent-red-reanchor.md`, and re-verified by `internal/agent TestDocAnchorsResolveInNormativeDocs` (which calls `docclass.Evidence.Check` on every normative row: measured, this doc appears nowhere in that gate's reported drift, while other rows in the same run do). Measured after the insert, on this branch: the gate now names **four** files, not five — the same four as master.
- **`internal/agent TestDocAnchorsResolveInNormativeDocs` is RED too — pre-existing on master, and this branch's classification does not touch it.** Control, same master: on `/home/user/gopi-pristine-gate` @ `2fdc65e4`, `docanchor_test.go:214: doc anchors verified: 2111 across 442 normative docs (1727 explicit + 384 bare continuations; 408 bare out of row/table/heading scope; 243 pre-existing drift entries permitted, 5 new, 10 stale permits)` → FAIL; on this branch the same line reads `2115 across 443 normative docs (1731 explicit + 384 bare …; 243 pre-existing drift entries permitted, 5 new, 10 stale permits)` → FAIL. **Identical `5 new` and `10 stale permits` on both** — so this branch's new normative doc and its spot-check add **zero** new anchor drift (its anchors resolve), and the gate's red is the 10 stale baseline permits plus the 5 new entries that master already carries. Named, dispatchable, not papered over.
- **A normative row carries NO banner; this branch's first attempt added one and the repo's own gate refused it.** `TestDocClassBannerMatchesManifest` → `devdoc/cmd-agent-package-budget.md: carries the "normative" banner but … classes it "normative" — only [historical] classes are bannered`. Removed; measured green after (`docclass_gate_test.go:212: … (0 mislabelled)`), with the banner count going 704 and mislabelled 0. The by-hand row insert is done in the manifest's own **path order** (between `cmd-agent-contract-reds.md` and `cmd-agent-red-reanchor.md`), and it round-trips: `TestDocManifestRowsAreWellFormed` reports `443 normative rows (each carrying a spot-check)` and the corpus gate's count moves 2110 → 2111 rows / 2114 → 2115 files, i.e. exactly one row for exactly one new file.
- **The 5 named failures are attributed, not repaired.** Three are the sibling
  row `01a0c049` (`../../bin/gopi`); two are the pre-existing `gopi config`
  completion/help mapping gap on master.
- **`scripts/heavy-derive.sh` refuses a single-package stream** (it wants a
  whole-module run; 48 of 49 packages missing → refusal). That is correct
  behaviour for the inventory as a whole, and it is why §3's table was derived
  by hand from the measured stream plus `scripts/heavy-drift-report.sh` (which
  reads a narrowed stream fine). A per-package derivation mode is **not**
  added here — it would be a second source for the inventory.
- **The first-open cost was re-measured, not "optimised".** If a future
  whole-package run spends minutes inside `execSchemaBatchOnce`, that is a
  different finding and needs its own direct measurement (the probe in
  `internal/tools/firstopen_probe_test.go` is the instrument).
- **Two sub-floor/above-floor tests are accepted rather than classified**, on
  purpose and with the reason recorded: `TestStrandedScanCapsAndDedupes` (3.09 s)
  and `TestStrandedRowInvisibleToTheNewestScanAlone` (4.49 s) sit below the 5 s
  floor, and the two `TestTmpDirSettingsValue*` gates (17.03 s each) must stay on
  the smoke path (Rule 22 host-configuration gates). Every drift report will
  name the latter two as UNCLASSIFIED until someone re-measures them — that is
  the designed state, not a missed row.
- **The M1–M3 anonymous runs are evidence of a defect, not of the suite's
  cost.** Anyone re-measuring cmd/agent MUST NOT strip `ANTHROPIC_AUTH_TOKEN`/
  `DEEPSEEK_API_KEY` first, or they will measure the brew defect instead of the
  suite (this was this task's own first mistake, caught by the 46-vs-1129
  test-count discrepancy and corrected to M5).

---

## 7b. The void run (M8) — a measurement whose subject was deleted underneath it

**M8 measured nothing, and its numbers appear nowhere above.** It is recorded
because the failure shape is dangerous: it looks exactly like a package verdict.

`make test-budget PKGS='./cmd/agent/'` — the **merge-path command itself**
(`internal/smokegate.ScopedCommand`, which is the shape this branch's diff
actually has to satisfy, rather than the epic's bare `go test ./cmd/agent/`) —
finished **289 s** and reported **15 named FAILs** where M5/M6/M7 reported 2–3.
Read at face value that is "the classification made things worse".

The stream says otherwise. Bucketing the failing tests by their own output:

```
$ python3 … over run-1789958550-2306629.json
FAILs with cwd-deleted signature: 44
FAILs with other causes:          24
```

44 failures carry one of three signatures, all of them 0.00 s:

```
getwd: no such file or directory
readdirent .: no such file or directory
repo "/…/suite-cmd-agent-run-638947970/gopi-repo-ctx-…" is not a git work tree
  (git rev-parse failed: exit status 128: fatal: not a git repository …
   Stopping at filesystem boundary (GIT_DISCOVERY_ACROSS_FILESYSTEM not set).)
```

Those are not test verdicts. They are the process discovering that **its current
working directory no longer exists** — every fixture calls `os.Getwd`,
`ReadDir(".")` or `git rev-parse`, and each one fails the moment the directory is
unlinked. And it was not only the fixtures: **this task's own worktree
`/home/user/workspace/gopi-cmdagent-budget` was gone after the run**, its
`git worktree list` registration removed, while this session was live in it. The
fixture sandbox named in the failure (`suite-cmd-agent-run-638947970`) is
likewise absent.

**So M8 is void: it ran in a directory that was deleted mid-run.** Its 289 s wall
and its 15 FAILs are measurements of a broken environment, not of cmd/agent.

**What this does NOT impugn.** M6 and M7 ran 10:14–10:39, before the deletion
(the worktree's own mtime and the removal window are ~10:43–10:47). They reported
2 and 3 named FAILs, all attributed in §5, with `panic` events 0 and full
termination. Those numbers stand.

**Mechanism: ROOT-CAUSED (M9 + M10), and it is not a stray reaper — it is this
suite's own subject under test.** M8 was not a one-off: it was **reproduced**
(see M9 below), and then **isolated to the worktree form specifically** (M10).

The suite *contains* the arm that removed the worktree.
`cmd/agent/cli_serve_stale_test.go:156 TestRunTaskWorker_OnceSweepsZombieFirst`
drives `runTaskWorker` **in-process**, and that driver "runs the round's
own-worktree release under THIS process's cwd" (the test's own comment). The
release pass it invokes is `internal/tools/worktree_sweep.go`, whose eligibility
conditions are stated at the top of that file — the removal happens only if the
registration is **① unowned** ("no ledger row owns it") **and ④ clean**
("`git status --porcelain` empty — uncommitted work is never destroyed") **and
⑤ older than the age threshold**.

All three held for this task's worktree, for a reason worth stating plainly:

- **④ clean** — by design. Rule 30 requires the worktree to print nothing, and
  this run had just finished committing (§7b's own commits). The discipline that
  makes a deliverable mergeable is exactly what makes it eligible.
- **① unowned — inside the test.** The test sets `t.Setenv("GOPI_MEMORY_DB",
  <sandbox>)`, so the ledger that pass consults is an **empty sandbox**, not the
  operator's `~/.gopi/tasks.db`. The row that *does* name this worktree
  (`01a0c04b`, stamped `deliverable.worktree=/home/user/workspace/gopi-cmdagent-budget`
  — verified with `gopi task show`) is invisible to it. The sweep's most
  destructive condition is evaluated against a ledger that cannot own anything.
- **⑤ age** — the registration's own `commondir` backlink was written when the
  worktree was created; the measured basis for the default floor is
  `DefaultWorktreeSweepMinAge = 24 * time.Hour` (`worktree_sweep.go:90`), and
  the age is read from that backlink file, **not** from mtime (the file's
  documented reason: "any later access refreshes it and which therefore cannot
  answer 'when was this made'"). M8's first deletion came ~2.5 h into the task —
  consistent with a shorter, environment-supplied floor
  (`GOPI_WORKTREE_SWEEP_MIN_AGE_H`) rather than the 24 h default. The exact
  floor value in force during the runs is **not** claimed here; what is measured
  is that a registered, clean, unowned-from-the-sandbox's-view worktree became
  removable within hours, twice.

The repo's own gate text describes this incident before it happened, in
`cmd/agent/repo_context_sandbox_guard_test.go`:

> "The spawned child inherits this process's working directory — the live
> repository — so its own-worktree-release pass enumerates the developer's
> worktrees and frees every clean, unowned one past its age floor (fulldry.log:522-524)."

That gate exists to force `SandboxRepoContext(t)` into every spawning function,
and `TestRunTaskWorker_OnceSweepsZombieFirst` **does** call it — so the guard is
satisfied by the letter. The escape is that the guard protects the *`GOPI_HOME`
sandbox*, while the chdir-into-the-live-checkout half still means the in-process
release enumerates the real `/home/user/workspace` registrations. **The
isolated run passes and survives** (`go test ./cmd/agent/ -run '^TestRunTaskWorker_OnceSweepsZombieFirst$'` → `ok 2.653s`, worktree intact); <!-- selector-ok: RECORD of the 2026-10 run quoted verbatim. The selector was NOT merely renamed — `d2f2d23d` INVERTED it into `TestRunTaskWorker_OnceLeavesTheLedgerAlone`, whose own commit message says four tests "pinned the OLD shape and went red against the correct code; each is INVERTED rather than deleted". The successor therefore asserts the OPPOSITE property, and citing it here would claim evidence for a claim it does not make. The measured sentence stands as written, as the record of that run. -->
under whole-package run order it is the first test to observe the damage, so
run order is what turns the hazard into a loss.

**M9 — the reproduction.** Same command as M8 (`make test-budget PKGS='./cmd/agent/'`)
from a freshly re-materialised worktree at `bf4d4337`, load 1.36→1.59:
**220 s, 15 named FAILs, 38 failures carrying the deleted-cwd signature**, and
the worktree **was gone again** immediately afterwards. So the removal is
reproducible on demand, per-run, and correlated with the worktree form.

**M10 — the isolation.** The same script run from a **`git clone --shared`**
of the branch (`/home/user/workspace/gopi-cmdagent-measure`, `bf4d4337`) on the
same host minutes later: **304 s, 5 named FAILs, ZERO deleted-cwd-signature
failures**, and **the directory survived** (both `/home/user/workspace/gopi-cmdagent-measure`
and the re-created worktree were still present afterwards). A clone has no
registration in the main repo's `.git/worktrees/`, so `git worktree list` cannot
see it — and nothing removed it. **M10 is therefore the branch's real
merge-path measurement**, and M8/M9 are void.

M10's 5 failures are the branch-independent set §2/§5 already attributes
(M5's set): `TestCompletionCandidateSetsCoverTopLevel`,
`TestTaskListReadPathE2E` (sibling row `01a0c049`),
`TestSweepZombiesCLI_DryRunTouchesNoNotifySink`,
`TestSweepZombiesCLI_ReportNotifiesOperator`,
`TestTopLevelHelpCoversEveryDispatchedSubcommand`.

**Why no audit entry exists** (the searches, so the next reader does not repeat
them — and so the *absence* is understood rather than re-investigated):

- `~/.gopi/logs/removal-audit.log` — last entry `2026-09-21T01:54:20Z`, a
  different worktree; no entry names this worktree or branch at any time.
- `~/.gopi/logs/worker.log` — last written `02:48`, hours before.
- `~/.gopi/logs/scratch-gc-watchdog.log` — only ever operates on
  `gopi-test-scratch` (`role=cli`, plus `role=cli-trees` for
  `go-tmp`/`raw`/`session-raw`/`build-cache`); never `/home/user/workspace`
  directly. Its lock was touched `10:30`, ≥13 min before the first removal.
- `dmesg` — nothing; `grep -rl 'cmdagent'` across the log tree — no hit.

The removal went through **`git worktree remove`/`prune` inside a test
process**, which is why it is audited nowhere: the audit arm
(`finish-cleanup-worktree` / `own-worktree-release`) belongs to the worker and
merger, and what ran here was the *library function under test* being called by
a test. **Dispatchable as its own task** (Rule 26): "the cmd/agent suite's
in-process own-worktree-release frees the developer's *live* worktree — the
`SandboxRepoContext` guard pins the GOPI_HOME base but not the chdir target, so
a clean registered worktree is removed mid-run with no audit entry; make the
in-process release refuse a registration it does not own in the REAL ledger, or
pin the sweep's target root the way it pins GOPI_HOME." Evidence: this section,
`/home/user/workspace/_evid/cmd-agent-budget/{m8,m9,m10}.log`, and
`run-1789959010-2684404.json` (first observer:
`TestRunTaskWorker_OnceSweepsZombieFirst`).

**Recovery, for the next agent.** A worktree is a *registration* over a shared
object store (Rule 6a), so the deliverable survived: every commit on
`fix/cmd-agent-package-budget` was intact, and re-materialising it is two
commands —

```
$ cd /home/user/work/GoPi && git worktree add /home/user/workspace/gopi-cmdagent-budget fix/cmd-agent-package-budget
HEAD is now at 82f867da docs(cmd-agent): quote one test-count framing per row …
$ git status --porcelain      # (empty)
```

Uncommitted work in the deleted worktree would have been **lost** — the reason
Rule 30's discipline (commit what is deliverable) is not tidiness.

**The generalisable lesson**, and why M9 exists: a measurement's environment can
be destroyed *during* the measurement, and the resulting failures are
indistinguishable from real ones by their count. Bucket failures by signature
(the three strings above), and check that the run's own cwd still exists, before
reporting any long-suite number.

---

## 8. Raw evidence

| artifact | path (inside the worktree, `/home/user/workspace/_evid/cmd-agent-budget/` is untracked litter by design) |
|---|---|
| M1 — credential-stripped whole package (anonymous FAIL) | `/home/user/workspace/_evid/cmd-agent-budget/m1.json`, `/home/user/workspace/_evid/cmd-agent-budget/m1.exit`, `/home/user/workspace/_evid/cmd-agent-budget/m1.start` |
| M2 — the same shape reproduced (default 10m, `nice`) | `/home/user/workspace/_evid/cmd-agent-budget/m2.json`, `/home/user/workspace/_evid/cmd-agent-budget/m2.log`, `/home/user/workspace/_evid/cmd-agent-budget/m2.host` |
| M3 — ambient-env control (killed; superseded by M5) | `/home/user/workspace/_evid/cmd-agent-budget/m3.log` |
| M5 — the honest whole package (447.354 s, 1129 tests) | `/home/user/workspace/_evid/cmd-agent-budget/m5.json`, `/home/user/workspace/_evid/cmd-agent-budget/m5.log`, `/home/user/workspace/_evid/cmd-agent-budget/m5.start` |
| M6 — after classification, same command | `/home/user/workspace/_evid/cmd-agent-budget/m6.json`, `/home/user/workspace/_evid/cmd-agent-budget/m6.log`, `/home/user/workspace/_evid/cmd-agent-budget/m6.start` |
| M7 — the after-run REPLICATED (382.769 s; 3 named fails, one order-dependent, passing in isolation) | `/home/user/workspace/_evid/cmd-agent-budget/m7.json`, `m7.log`, `m7.err`, `m7.start` |
| the DOC-INDEX row + its spot-check | `devdoc/DOC-INDEX.tsv` line in path order between `cmd-agent-contract-reds.md` and `cmd-agent-red-reanchor.md`; the evidence cell is re-verified by `internal/agent TestDocAnchorsResolveInNormativeDocs` (`docanchor_test.go:254` calls `ev.Check(root, doc)` on every normative row's cell). Measured: the gate reports `2115 across 443 normative docs … 5 new, 10 stale permits` over this branch — the pristine-master control reports `2111 across 442 … 5 new, 10 stale permits`, i.e. the new row adds **0** new drift |
| the classified tests, heavy DECLARED ON | `/home/user/workspace/_evid/cmd-agent-budget/classified-on.log` |
| timeout-shape control (a real timeout DOES panic) | `/home/user/workspace/_evid/cmd-agent-budget/timeout_probe.json` (`grep -c 'panic: test timed out'` → 1) |
| ledger first-open phase probe | `/home/user/workspace/_evid/cmd-agent-budget/probe.out`, `/home/user/workspace/_evid/cmd-agent-budget/probe/firstopen-probe.txt` |
| host/device facts | `/home/user/workspace/_evid/cmd-agent-budget/m2.host`, `/home/user/workspace/_evid/cmd-agent-budget/probe.host` |

Commit(s) on `fix/cmd-agent-package-budget`:

| commit | what |
|---|---|
| (see `git log --oneline -3`) | classify the 19 measured offenders + this report |

Recovery (Rule 6d): the branch is worktree-shared with the main repo
`/home/user/work/GoPi`, so the commits are already in the shared object store —
`git show fix/cmd-agent-package-budget:devdoc/cmd-agent-package-budget.md`.

## Evidence (self-filed)

- **Anonymous FAIL is a binary exit, not a timeout**: `grep -c 'panic: test timed out'` on the M1 stream → **0**; no `"Action":"fail"` event carries a `Test` field in `/home/user/workspace/_evid/cmd-agent-budget/m1.json`; 46 `"Action":"run"` events vs 1129 in `/home/user/workspace/_evid/cmd-agent-budget/m5.json`.
- **The named cause**: `env -u DEEPSEEK_API_KEY -u ANTHROPIC_AUTH_TOKEN go test ./cmd/agent/ -run '^TestIsBrewSubcommand$' -count=1 -json` → package-level `fail 0.955` with the last output line `gopi brew: brew: deepseek: ANTHROPIC_AUTH_TOKEN not set (env, DEEPSEEK_API_KEY, or ~/.gopi/settings.json)`; the same run with ambient env → `pass 0.557`.
- **Whole-package wall** (M5): `{"Action":"fail","Package":"github.com/nick/gopi/cmd/agent","Elapsed":447.354}` in `/home/user/workspace/_evid/cmd-agent-budget/m5.json`; `grep -c '"Action":"panic"'` → 0.
- **Timeouts still panic in this package** (so the M1 shape is distinguishable by construction): `go test ./cmd/agent/ -run '^TestWorkerDrainStillLetsTheRoundFinish$' -timeout 5s -json` → `grep -c 'panic: test timed out' /home/user/workspace/_evid/cmd-agent-budget/timeout_probe.json` → **1**.
- **Ledger first-open probe**: `/home/user/workspace/_evid/cmd-agent-budget/probe.out` phase lines `enableWAL wall=0.149s` (49 samples blocked-in `fsync`, wchan `jbd2_log_wait_commit`), `ensureSchema wall=0.059s` (17 samples), on `fs-type 0xef53 (device-backed)`, `sync=NORMAL`.
- **Build/vet/gate**: `go build ./...` → exit 0; `go vet ./cmd/agent/` → exit 0; `go test ./internal/standards/ -run 'TestHeavy' -count=1` → `ok … 0.905s` (123 rows ↔ 123 call sites).
- **Drift**: `scripts/heavy-drift-report.sh /home/user/workspace/_evid/cmd-agent-budget/m5.json` → `19 test(s) at or above it, 0 without a heavy-tests.tsv row`.
- **Both halves of the switch**: OFF → `--- SKIP: TestWorkerIdleExitZeroWaitsForever`, `--- SKIP: TestWorkerServesEachTaskInItsOwnRepoAcrossConsecutiveRounds`, `ok … 1.545s`; ON → `grep -cE '^--- PASS'` on `classified-on.log` = **21**, `grep -cE '^--- (SKIP|FAIL)'` = **0**, `ok github.com/nick/gopi/cmd/agent 161.517s`, `EXIT=0`.
- **Rule 30**: `git status --porcelain` in the worktree prints nothing at close-out.
- **M6 → M7 replication**: same command, same env, Go default 10 m → `{"Action":"fail","Package":"…/cmd/agent","Elapsed":382.513}` (M6) vs `382.769` (M7); `panic` events 0 in both; terminated tests 1129/1129 (M6) and 1363/1363 (M7).
- **The 3 named FAILs of M7**: `TestCLICodeSearchSelfHealsCorruptIndex` (0.02 s, whole-package run) → in isolation on this branch `go test ./cmd/agent/ -run '^TestCLICodeSearchSelfHealsCorruptIndex$' -count=1` → `ok 0.550s`; the other two → control on pristine master `2fdc65e4` `-run '^(TestCompletionCandidateSetsCoverTopLevel|TestTopLevelHelpCoversEveryDispatchedSubcommand)$'` → `--- FAIL:` each, `FAIL 0.934s`.
- **Declared budget is the SCRIPT's, not Go's default**: `grep -n 'test_budget\|GOPI_TEST_TIMEOUT' scripts/test-budget.sh` → `test_budget="25m"` (line 82) **in the tree this was measured on — the constant is `test_budget="45m"` at line 147 today, and `GOPI_TEST_TIMEOUT` is still the only override (lines 150-158)**; the actual run line was 461 (now line 2461, `-timeout "$test_budget"`). The current value, its derivation and the checks that hold it are in §6's "CURRENT VALUE" note.
- **DOC-INDEX row**: `awk -F'\t' '{print NF}'` on the inserted row → `4` (path/class/reason/evidence); after the insert the corpus gate's line reads `2111 rows cover 2115 files (classes: map[historical:1655 index-meta:1 normative:443 roadmap:12])` — exactly one row for exactly one new file, and the count of files it still names is **4**, the same four the pristine master control names (`2110 rows cover 2114 files`).
- **The row's spot-check RESOLVES**: `go test ./internal/agent/ -run TestDocAnchorsResolveInNormativeDocs -count=1 -v | grep -i cmd-agent-package` → **no match**, i.e. this doc's evidence cell is not among the gate's reported drift, while other rows in the same run do appear (e.g. `BASELINE DRIFT … devdoc/IMMEDIATE-FEEDBACK-REGRESSION.md: anchor loop.go:118 points at a BLANK line`). Mechanism: `docanchor_test.go:254` calls `docclass.Evidence.Check`, whose `sym` tier requires the named file to still declare the symbol (`internal/docclass/evidence.go:88`), and `cmd/agent/merge_verify_test.go` declares `TestMergePassWithdrawsOnlyTheRedRepo` at package level — one of the tests this branch classifies heavy (`merge_verify_test.go:372` declaration, `:373` its `heavytest.Require`) — so the cell breaks the day that classified test is renamed, moved out of the package, or deleted, which is exactly the coupling the row claims.
- **No banner on a normative row**: `TestDocClassBannerMatchesManifest` → `devdoc/cmd-agent-package-budget.md: carries the "normative" banner but … classes it "normative"` (red) → banner removed → `docclass_gate_test.go:222: … 704 markdown docs of a bannered class carry their banner (0 mislabelled)` (green).
