<!-- doc-class: historical -->
# cmd/agent order-dependent reds — the differential row 01a1458b-3644 could not run

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

Baseline `237bb48` (`237bb486e149b033ebaa4de808185145c8e26614`), branch
`task/01a14710fc2a70839ba952ca6d64a323`, task `01a14710-fc2a-7083-9ba9-52ca6d64a323`.
Raw evidence: `/home/user/.gopi/sessions/01a14710-fc2a-7083-9ba9-52ca6d64a323/evidence/`.

**Verdict, plainly: the six reds REPRODUCE on a frozen, unedited tree.** All six are
GREEN in an isolated `-run` selection and RED in the package-wide run, measured on the
same frozen build. The differential also localises the interaction: **it is inside the
worker family, and the carrier is process-wide state that an earlier test in the same
test binary leaves set** — a shared `workerStopState` latch and a durable stop-request
file that the next worker round consumes. One added preceding test, in a two-test run
that finishes in 11 s, turns the same victim red (armE), and the mechanism is visible
verbatim in the victim's own output (armD). Everything beyond that is marked below as
inference.

## (a) Frozen-build identity

Every reading below was taken on ONE revision, with **no edit to any file under the
worktree inside the measurement window** — this run changed no production file at all
(the Rule 30 close-out then commits only the devdoc and its manifest row).

| reading | value | artifact |
|---|---|---|
| HEAD at the window's start (08:21:55) | `237bb486e149b033ebaa4de808185145c8e26614` | `evidence/frozen-identity.txt` |
| HEAD at the window's end (08:41:21) | `237bb486e149b033ebaa4de808185145c8e26614` (unchanged) | `evidence/post-run-freeze.txt` |
| cut point | `237bb48` — NOT `refs/heads/master` = `9adad635b9a66ef51783c809436803a8a06b4efa` (10 commits behind) | both files |
| `git status --porcelain` | **prints nothing**, at the start and at the end | both files |

```
$ git rev-parse HEAD
237bb486e149b033ebaa4de808185145c8e26614
$ git status --porcelain
                       <- nothing
```

`find` for files written under the worktree after the window opened reports exactly two
paths, and neither is a source file:

```
./.gopi-tags
./cmd/agent/.gopi-tags
```

They are **gitignored build-index artifacts the tooling writes by itself** (the tags
index code_search rebuilds), they are untracked, and `git status --porcelain` is empty
at the end of the window — the tree's tracked content was frozen throughout.

## (b) Host readings (OBSERVED, never manufactured — Rule 27)

No load was generated: both halves are the ordinary suite. Load was sampled every 20 s
for the whole window (`evidence/host-sampler.txt`).

| moment | load (1/5/15) | `/home` | artifact |
|---|---|---|---|
| window open (08:21:45) | 1.55 / 2.29 / 5.05 | 309G used, 126G avail, 72% | `evidence/pre-run-host.txt` |
| isolation run (08:22:03) | 1.28 / 2.19 / 4.97 → 3.86 / 2.87 / 4.98 | 126G avail | `evidence/isolation-run.log` |
| armD, armE, full-run start (08:25–08:26) | 2.11 … 2.66 | 125G avail | `evidence/armD-*.log`, `evidence/armE-*.log` |
| full-package run (08:26:02, 872 s wall) | 2.66 / 2.64 / 4.55 → **15.10 / 9.09 / 6.56** | 123G avail | `evidence/full-package-run.log` |
| window close (08:41:27) | 11.08 / 8.87 / 6.63 | 123G avail | `evidence/full-package-run.log` |

**Two honest confounds about the host, stated before any verdict.**

1. **The full-package run was NOT alone on the host.** Another agent's identical
   `make test-budget PKGS=./cmd/agent` (`go test … ./cmd/agent`, pid 307745) started at
   08:29:53 — 3 min into my 08:26–08:41 window — and ran to ~08:40. The budget script
   itself named it, in my own later runs' output:
   `!!!!!!!!!!!!!!!! CONCURRENT BUDGET RUN OVERLAPS THIS SCOPE !!!!!!!!!!!!!!!!`
   (`evidence/armK-shuffle-seed7.log:15`). The stamp's own `load_end=15.10` is the
   measurement of that overlap, and the count of gopi processes at 08:41 confirms it.
2. **The reproduction does NOT depend on it.** The decisive subset arms — `armE`
   (2 tests, 11 s, 08:25:37, load 2.11 → 2.24) and `armD` (3 tests, 14 s, 08:25:48,
   load 2.24 → 2.66) — ran **before** the neighbour started and carry **no** overlap
   warning at all. The reds reproduce there, on an unloaded host, in seconds. So the
   package-wide run's overlap weakens *that run's* load reading; it does not carry the
   verdict, which armE/armD carry on their own.

Also note what did NOT appear: **the run refused nothing.** `grep -c "would not verify"`
over `evidence/full-package-run.log` is **0**, and the script never printed a drift or
stale-binary verdict. This is the honest difference from row `01a1458b-3644`, whose 11
refusals made its "0 of 6 reproduced" an absence of measurement, not a measurement of
absence.

## (c) The ISOLATION run of the named cases

```
$ make test-budget PKGS="-run 'TestWorkerTakeOver|TestRunWorkerClaimLocalOnlyWithNoPeers|TestWorkerLoopLeavesNoWatcherPollBehind' ./cmd/agent"
```

exit code **0** (`EXIT=0`), 76 s, 17 tests, `packages: 1 finished, 0 with a failure`.
Stamp, verbatim from `evidence/isolation-run.log`:

```
stamp: 2026-10-17T08:22:04+08:00 host=hostb pkg=-run TestWorkerTakeOver|TestRunWorkerClaimLocalOnlyWithNoPeers|TestWorkerLoopLeavesNoWatcherPollBehind ./cmd/agent budget=45m elapsed=76s load_start=1.28 2.19 4.97 load_end=3.86 2.87 4.98 cores=8 exit=0 outcome=pass stream=run-1792196524-290042.json
```

| named case | isolation |
|---|---|
| `TestWorkerTakeOverWalksToTheNextPeer` | **PASS** |
| `TestWorkerTakeOverGatesRefuseWithoutDialling/task_sync_off_still_allows_the_transfer` | **PASS** |
| `TestWorkerTakeOverFollowsThePullSwitchOnly/pull_on,_row_exchange_off` | **PASS** |
| `TestWorkerTakeOverFollowsThePullSwitchOnly/pull_on,_row_exchange_on` | **PASS** |
| `TestRunWorkerClaimLocalOnlyWithNoPeers` | **PASS** |
| `TestWorkerLoopLeavesNoWatcherPollBehind` | **PASS** |

## (d) The FULL-PACKAGE run

```
$ make test-budget PKGS='./cmd/agent'
```

872 s wall, `executed: 4484 test(s)`, exit **1**. Stamp, verbatim from
`evidence/full-package-run.log`:

```
stamp: 2026-10-17T08:26:02+08:00 host=hostb pkg=./cmd/agent budget=45m elapsed=872s load_start=2.66 2.64 4.55 load_end=15.10 9.09 6.56 cores=8 exit=1 outcome=fail stream=run-1792196762-295521.json
```

**The two counts the acceptance asks to be read side by side:**

```
--- FAIL (level-1)                       : 8
"would not verify" refusals              : 0      <- informative run, not a refused one
```

The 8 level-1 failures (`evidence/fullpkg-per-case-verdicts.txt`, read from the run's
own stream — 12 fail events in total, the other 4 being the parents/subtests):

```
TestWorkerTakeOverFollowsThePullSwitchOnly        <- named
TestWorkerTakeOverGatesRefuseWithoutDialling      <- named
TestWorkerTakeOverWalksToTheNextPeer              <- named
TestRunWorkerClaimLocalOnlyWithNoPeers            <- named
TestWorkerLoopLeavesNoWatcherPollBehind           <- named
TestPublishedHelpPagesStayWithinTheirUsageBudget  <- a different class (isolation-red)
TestToolsMaintenanceHelpSurfaceEveryFormIsTestedOrBlockedWithAReason  <- ditto
TestSigintDocsCiteTheLineNumbersTheSourceActuallyHas                  <- ditto
```

**All six named cases are RED in the package-wide run.** Measured, per case, from the
run's own `-json` stream (not from prose):

```
fail  TestWorkerTakeOverWalksToTheNextPeer
fail  TestWorkerTakeOverGatesRefuseWithoutDialling/task_sync_off_still_allows_the_transfer
fail  TestWorkerTakeOverFollowsThePullSwitchOnly/pull_on,_row_exchange_off
fail  TestWorkerTakeOverFollowsThePullSwitchOnly/pull_on,_row_exchange_on
fail  TestRunWorkerClaimLocalOnlyWithNoPeers
fail  TestWorkerLoopLeavesNoWatcherPollBehind
```

The three reds that are red in isolation too (`…HelpPagesStayWithinTheirUsageBudget`,
`…HelpSurfaceEveryFormIsTestedOrBlockedWithAReason`, `…SigintDocsCiteTheLineNumbers…`)
are the **same set the original filing already classified as isolation-red** — a
different defect, not this one, and not touched by this row.

`--- FAIL` text of the takeover arm, verbatim (`evidence/fullpkg-per-case-verdicts.txt`,
stream `run-1792196762-295521.json`):

```
task_pull_switch_test.go:57: workerTakeOverAttempt = "", want "t-taken-over" (task_pull=1 task_sync=0)
task_takeover_trigger_test.go:100: with task_pull on and task_sync off, the round took over "", want the peer's row
task_takeover_trigger_test.go:140: takeover returned "", want the id the second peer handed over
```

## (e) The differential table — isolation vs package-wide, all six

| # | named case | isolation (`-run`, exit 0) | package-wide (`PKGS='./cmd/agent'`, exit 1) | raw line that produced the reading |
|---|---|---|---|---|
| 1 | `TestWorkerTakeOverWalksToTheNextPeer` | **PASS** | **FAIL** | `armE-minimal-pair.log:37-40`; `fullpkg-per-case-verdicts.txt` |
| 2 | `TestWorkerTakeOverGatesRefuseWithoutDialling/task_sync_off_still_allows_the_transfer` | **PASS** | **FAIL** | `subset-ack-plus-takeover.log` (`task_takeover_trigger_test.go:100`); `fullpkg-per-case-verdicts.txt` |
| 3 | `TestWorkerTakeOverFollowsThePullSwitchOnly/pull_on,_row_exchange_off` | **PASS** | **FAIL** | `subset-ack-plus-takeover.log` (`task_pull_switch_test.go:57`); `fullpkg-per-case-verdicts.txt` |
| 4 | `TestWorkerTakeOverFollowsThePullSwitchOnly/pull_on,_row_exchange_on` | **PASS** | **FAIL** | same two artifacts |
| 5 | `TestRunWorkerClaimLocalOnlyWithNoPeers` | **PASS** | **FAIL** | `armD-other-two-named.log:41-49` (`singlenode_serve_test.go:132`); `fullpkg-per-case-verdicts.txt` |
| 6 | `TestWorkerLoopLeavesNoWatcherPollBehind` | **PASS** | **FAIL** | `armD-other-two-named.log:50-53` (`cli_worker_control_watcher_test.go:158`); `fullpkg-per-case-verdicts.txt` |

**6 / 6 reproduce.** The original filing's "6 of 9 order- or load-dependent" is now
confirmed with both readings on one frozen build, for every one of the six.

## (f) Localisation — what the bisect supports

Seven subset arms were run on the same frozen build. Each is a `make test-budget`
`-run` selection, so each is the declared entry point over a narrowed scope. Verdicts
are read from each run's own stream (`evidence/stream-run-orders.txt`,
`evidence/fullpkg-per-case-verdicts.txt`).

| arm | selection (in registration order) | exit | verdict | artifact |
|---|---|---|---|---|
| isolation | the 6 named cases | 0 | all PASS | `isolation-run.log` |
| **armB** (control) | `TestWorkerAckIsNotAcceptedForAnotherRequest` (2121) + the whole `TestWorkerTakeOver*` family (2697–2737) | **0** | **all 16 PASS** | `armB-control-no-leaker.log` |
| **armE** | `TestWorkerAckIsWrittenOnConsumeAndAnswersOnlyItsOwnRequest` (2120) + `TestWorkerTakeOverWalksToTheNextPeer` (2735) | **1** | walk **FAIL** | `armE-minimal-pair.log` |
| **armD** | `TestWorkerAckIsWritten…` (2120) + `TestWorkerLoopLeavesNoWatcherPollBehind` (2127) + `TestRunWorkerClaimLocalOnlyWithNoPeers` (2625) | **1** | **both FAIL** | `armD-other-two-named.log` |
| subset | `TestWorkerAckIsWritten…` (2120) + the whole `TestWorkerTakeOver*` family | 1 | the takeover family's **pull-on arms FAIL**, pull-off arms PASS | `subset-ack-plus-takeover.log` |
| armF (control) | `TestWorkerTakeOverWalksToTheNextPeer` (2735) + `TestWorkerDrainConsumesRequestAtBoundary` (2838) | 0 | PASS | `armF-latchconsumer-plus-walk.log` |
| armH (control) | `TestWorkerTakeOverWalksToTheNextPeer` (2735) + `TestWorkerIdleDrainedWaitWakesOnStopFlag` (2866) | 0 | PASS | `armH-cleanup-reset-control.log` |
| armI (control) | `TestWorkerTakeOverWalksToTheNextPeer` (2735) + `TestWorkerDrainNudgeDoesNotReArmItself` (2801) | 0 | PASS | `armI-no-reset-nudge.log` |

**The single variable that separates red from green is ORDER, and specifically whether
a latch-writing test ran BEFORE the victim.** The registration numbers are from
`go test -list '.*' ./cmd/agent` (`evidence/latch-leaker-positions.txt`). In armE, armD
and the subset the writer's position is *below* the victim's, and the victim is red; in
armF, armH, armI the victim sits *below* the writer, so the writer never runs first, and
the victim is green. armB is the discriminating control: `TestWorkerAckIsNotAccepted…
` (2121) is also registered before the takeover family, but it does **not** call
`consumeDrainRequests` and writes no request file — it touches the latch not at all —
and the family stays green.

So the interaction is **inside the worker family**, not cross-family, and not
"package-wide parallelism as such": two tests in one 11-second selection reproduce it.

### The mechanism the measurement supports

**A process-wide latch + a durable request file, written by one test and never restored.**

1. `workerStopState` is a **module-level** record — its own doc says so:
   `cmd/agent/worker_drain_unit_test.go:57` — "the record is deliberately module-level:
   it is the ONE flag every exit path reads". `resetWorkerStopState()` exists to clear
   it, and a source scan (`evidence/static-latch-scan.py`, output
   `evidence/static-latch-analysis.txt`) finds **17 test functions that write the latch
   and only 3 that restore it via `t.Cleanup`**. `TestWorkerAckIsWrittenOnConsumeAndAnswersOnlyItsOwnRequest`
   (`cmd/agent/cli_worker_ack_test.go:65`) calls `resetWorkerStopState()` **at its start
   only** and calls `ctx.consumeDrainRequests()` twice (lines 77, 107), which sets
   `workerStopState.stop = true` and leaves it set for the whole rest of the binary.
2. Four call sites on the takeover path read exactly that flag as a **drain guard**:
   `cmd/agent/cli_tasks_worker.go:1234` inside `workerTakeOverAttempt` —
   `if workerStopRequested() { return "" }`. Every takeover assertion that FAILED wants a
   **non-empty** return (`"" → want "t-taken-over"`); every takeover assertion that PASSED
   wants **""**, which is what the guard returns anyway. The failure text is the guard's
   own value.
3. The other two victims fail the same way through the same latch. `armD`'s output shows
   the leak as **a durable request file** — a second carrier, and the one a reader can
   see with their own eyes, quoted verbatim from `evidence/armD-other-two-named.log`:

   ```
   worker: stop request file /dev/shm/suite-cmd-agent-run-1240883223/TestWorkerAckIsWrittenOnConsumeAndAnswersOnlyItsOwnRequest3426092085/001/TERM_295312 — draining (consumed at the claim boundary: nothing new claimed, leaving) | SENDER pid 295313 argv "gopi worker --stop" SIGNAL SIGTERM TASK none AT 2026-10-17T08:26:01+08:00
   ```

   `TestRunWorkerClaimLocalOnlyWithNoPeers` then fails at `singlenode_serve_test.go:132`
   because the round **drained before reaching the claim gate it asserts on**: the output
   it was handed contains the drain line above instead of `[claim-lock] task sync OFF`.
   The same consume makes `TestWorkerLoopLeavesNoWatcherPollBehind` fail at
   `cli_worker_control_watcher_test.go:158` — the in-process loop returns at its top
   before arming the poll (`consumeDrainRequests()` at `cli_tasks_worker.go:545`, guard at
   `:546`), so the poll is never on the stack and the test reads `0, was 0 before`.

**Reproduction count, stated plainly: 6 of 6, on a frozen tree, with no edit in the
window.** The mechanism is supported by (1) the flag's four guard readers on the exact
path the assertions exercise, (2) the writer-before-victim ordering that flips every arm,
(3) the discriminating control armB, and (4) the leaked request file printed in the
victim's own output.

## (g) Mechanism NOT localised — what stays inference

- **Which test leaks in the FULL package run is INFERRED, not instrumented.** The
  full-package run was not probed for the leaker's identity (no per-test latch dump was
  taken). The ack test (position 2120) is the first latch-setter in registration order
  and precedes all five victims by ≥ 600 positions, and it is the one armE/armD/subset
  **measured** to flip. But other latch-writing tests sit between it and the victims
  (`DrainNudge` 2801, `DrainConsumes` 2838, and the rest of the 17), and more than one
  may contribute. Naming "the" leaker of the package-wide run would be inference, and is
  not claimed.
- **The carry is *both* of two carriers; their relative weight is not measured.** The
  in-process flag and the durable request file are separately sufficient in principle.
  armE's 2-test evidence is consistent with the flag alone; armD's failing output proves
  the request file was also in play there. No arm isolates one from the other.
- **Why the ack test's request file is readable by a later test's worker round** —
  i.e. why the path printed in armD is the ack test's directory rather than the later
  test's own — is **not explained by this run**. That is a second, independent question
  (the control directory's resolution, or the child worker inheriting environment from
  the earlier test) and it is the part of the shape most worth a follow-up row.
- **The three isolation-red reds are a different defect.** They were red in isolation in
  the original filing and are red here; this row did not investigate them and does not
  attribute them.
- **The full-package run's overlap** with another agent's identical run (confound 1 in
  (b)) is real and is not worked around. The verdict does not rest on it — armE/armD
  reproduce unambiguously and unaccompanied — but a reader re-deriving the package-wide
  reading should know the host was shared for part of it.

## What this implies for a repair (not done here — this row measures, it does not fix)

The shape is a **test-hygiene defect with a product-visible symptom**: a package-level
latch is written by 17 test functions and restored by 3, and the durable request file
outlives the `t.TempDir()` that made it. A fix belongs in the tests (`t.Cleanup(resetWorkerStopState)`
everywhere the latch is written; the request file cleaned up with its directory), or in
the harness (`TestMain`), not in an assertion — weakening `workerTakeOverAttempt`'s
`""` expectations would hide a real drain guard. **No repair is made in this row**: the
acceptance is the differential, and Rule 41 says the finder files, the finder does not fix.

## Reproduction

```bash
cd <worktree at 237bb48>
# GREEN (nothing precedes the victims):
make test-budget PKGS="-run 'TestWorkerTakeOver|TestRunWorkerClaimLocalOnlyWithNoPeers|TestWorkerLoopLeavesNoWatcherPollBehind' ./cmd/agent"
# RED (one latch-writing test precedes them) — 11s, 2 tests:
make test-budget PKGS="-run 'TestWorkerAckIsWrittenOnConsumeAndAnswersOnlyItsOwnRequest|TestWorkerTakeOverWalksToTheNextPeer' ./cmd/agent"
# RED (the whole package):
make test-budget PKGS='./cmd/agent'
```

## Evidence artifacts (all under the session dir, none in the tree)

```
evidence/frozen-identity.txt              HEAD + master sha + empty git status, window open
evidence/post-run-freeze.txt              the same, window close (unchanged)
evidence/pre-run-host.txt                 uptime + df at 08:21:45
evidence/host-sampler.txt                 load + df every 20s for the whole window
evidence/isolation-run.log                (c) the isolation run, stamp verbatim
evidence/full-package-run.log             (d) the package-wide run, stamp verbatim
evidence/fullpkg-per-case-verdicts.txt    (e) per-case verdicts read from the run's own stream
evidence/stream-run-orders.txt            execution order + FAIL set of every arm's stream
evidence/armB-control-no-leaker.log       (f) the discriminating control: PASS
evidence/armD-other-two-named.log         (f) the leaked request file, verbatim
evidence/armE-minimal-pair.log            (f) 2 tests, 11s, walk FAIL
evidence/armF-latchconsumer-plus-walk.log (f) control: PASS
evidence/armH-cleanup-reset-control.log   (f) control: PASS
evidence/armI-no-reset-nudge.log          (f) control: PASS
evidence/subset-ack-plus-takeover.log     (f) takeover family + the ack test
evidence/armG-shuffle-seed{1,2,3}.log     -shuffle control: exit 5, nothing ran (see below)
evidence/armK-shuffle-seed{7..11}.log     same, flags re-spelled
evidence/latch-leaker-positions.txt       registration position of every latch-writing test
evidence/static-latch-scan.py             the scan that produced the table below
evidence/static-latch-analysis.txt        17 latch-writers, 3 with a restoring t.Cleanup
```

**A negative control that produced nothing, recorded so it is not mistaken for a pass.**
Five `-shuffle=on -shuffle.seed=N` arms were attempted to show the order effect directly.
All returned exit **5**, `outcome=no-test-executed`: `test-budget.sh` forwards `PKGS` to
`go test` as a single word list, so a selection whose FIRST token is a flag was handed to
`go test` as a *package* argument (`run-1792196864-298851.json`:
`no Go files in /home/user/workspace/gopi-01a14710fc2a70839ba952ca6d64a323`). The script
correctly refused to call that a pass, and it is not one. **An explicit shuffle seed was
therefore never obtained**, and the order dependence is established by the registration
ordering of the arms in (f) — measured, but not by a seed that can be quoted.
