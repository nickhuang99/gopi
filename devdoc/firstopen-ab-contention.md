<!-- doc-class: roadmap -->
# The first-open A/B: one sample per arm is not a measurement

Branch: `fix/firstopen-ab-contention` (worktree `~/workspace/gopi-firstopen-ab`),
based on master `bf4d1941`. It replaces the instrument in
`internal/tools/firstopen_sync_test.go` and adds
`internal/tools/firstopen_sync_decision.go` + `internal/tools/firstopen_ab_repro_test.go`.
No production code is touched: `firstOpenSyncMode`, `enableWALWithSync` and
`execSchemaBatchOnce` are unchanged. The only non-test change is a `.gitignore`
entry for the in-tree device scratch dirs (see the hygiene section below).

## The defect

`TestFirstOpenBlockDeviceCostIsTheSync` asserted an ORDERING between the two arms
of a same-file A/B:

- arm "durable": the first-open schema build under `synchronous=NORMAL`
  (`ledgerSyncMode`),
- arm "relaxed": the same build under `synchronous=OFF`
  (`ledgerSyncModeWorkingFS`),

and it read each arm from **ONE wall-clock sample**, asserting `relaxed < durable`.

The claim it pins is real and narrow — *the sync is the cost the first-open change
removes* — but the instrument encoded that claim as a single noisy sample of a
shared block device. The file was byte-identical on master and on the branch whose
merge run was red, and it passed 3/3 alone: the merge-path red was the INSTRUMENT,
not the code under test.

The damage is not a near miss. Measured on this host while other agents were
working (table below): the arm medians move by 3x between runs (10.3ms to 40.8ms
for the same arm), the arm-to-arm effect is 1-14% (median ~10%), and one
contended sample reaches 5-16x its arm's median. The old assertion compared two
such samples.

## What replaced it

### 1. An order-balanced paired sampler (`firstopen_sync_test.go`)

`firstOpenABPairs = 32` pairs, each a QUADRUPLE of fresh ledgers
(`firstOpenABBuildsPerPair = 4`) built inside one pair with the pragma as the only
difference. The order within a pair is palindromic in arm labels — `ABBA`
(durable, relaxed, relaxed, durable) or its mirror `RDDR` — and the two
palindromes alternate across pairs (`firstOpenABPairOrderFor`):

| property | what it buys |
|---|---|
| palindrome | a monotone drift across the pair's four builds adds the same amount to both arms, so the per-arm-averaged difference (`firstOpenABPairDelta`) cancels it exactly instead of leaving it to whichever arm went second |
| alternating mirror | a SHORT burst covering the pair's two middle builds falls on whichever arm sits there, so the mirror makes that a fair coin across the run rather than a bias |

Measured: 128 fresh ledgers, ~4-6s per A/B run on this host.

### 2. The statistic as a pure decision function (`firstopen_sync_decision.go`)

`firstOpenABDecide(pairDeltas []float64) firstOpenABDecision` — samples in,
verdict out; `firstOpenABMisattribution` is the assertion itself; nothing in the
file touches a device.

It is the **signed-rank** statistic (Wilcoxon), not a mean or a vote:

| property | why |
|---|---|
| ranks, not magnitudes | a contended pair carries a magnitude many times the whole effect and would dominate any average; under ranks it is one observation with one rank. Noise-tolerant by construction, not by hoping the outlier is small |
| ranks of the run's OWN pairs | the null does not need a prior distribution, and a run whose pairs disagree wildly has a broader null and therefore needs a bigger lead |
| **exact conditional null** (`firstOpenABWilcoxonCritical`) | the burden of proof is the run's own measured noise, computed by exact subset-sum DP over the observed ranks WITH their ties (midranks, `firstOpenABMidranks`) — no normal approximation, because with a couple of dozen pairs and ties the approximation is exactly where a flake threshold would be wrong |
| tiers by `firstOpenABMaxFalseVerdictRate = 0.002` | a direction is asserted only when the probability of that lead under the null is below 0.2%; the threshold is a COMPUTED value, not a constant (there is no magic number in the file) |
| asymmetry | an inconclusive run SKIPS with its full distribution; only a decisive wrong-way lead is reported. A merge must not be blocked by a host's noise, and saying nothing is not a failure |

The DP is checked against brute-force enumeration over every subset
(`TestFirstOpenABExactNullMatchesBruteForce`), and the tie handling against
hand-computed midranks — the two pieces a reader cannot verify by eye.

### 3. The calibration, MEASURED (`Rule 10.4`)

The old instrument's replacement cannot be picked from taste, so the noise was
measured on this host first: a temporary diagnostic (deleted after the runs; its
transcript is `.evid4/calibration/measured-noise-model.md`) built 32 pairs of the
real builds in two designs plus an **A/A control** — the same instrument with both
arms durable, i.e. a run in which no effect exists.

| measurement | value |
|---|---|
| A/A control (the null) median \|log ratio\| | 0.07-0.11 |
| A/A control p90 / max \|log ratio\| | 0.17-0.31 / 0.36-0.65 |
| AB per-pair relaxed win rate | 0.55, 0.69, 0.75, 0.80, 0.84, 0.90 (six rounds, load 6.7-11.9) |
| AB median effect | 1.2-13.7% cheaper (relaxed) |
| implausible "effects" in the previous design | the A/A control run once read 13-18% the wrong way — i.e. the host can produce a 6-3 wrong-way split with NO effect present |

From that model (per-build σ ≈ 0.12 log units, ~5% of builds at 3x that, a 20%
chance a pair carries a neighbour burst) the final design was simulated —
transcript in `.evid4/calibration/simulation-transcript.md`:

| situation | wrong-direction report | correct direction | inconclusive (skip) |
|---|---|---|---|
| no effect at all (the null) | **0.203%** | 0.1% | 99.6% |
| relaxed 6% cheaper | 0.000% | 18.8% | 81.2% |
| relaxed 10% cheaper (the real effect) | 0.000% | **62.1%** | 37.9% |
| relaxed 14% cheaper | 0.000% | 88.8% | 11.2% |
| DURABLE 10% cheaper (a genuine misattribution) | reported 61.2% | — | 38.8% |

So: ~1-in-500 false report, 62% power at the real effect (rising to ~90% on a
quiet device), the rest honest skips with the distribution logged — where the old
instrument inverted on a single contended sample that this host produces in a few
percent of samples.

**This is still a probability, and that is deliberate**: a measurement of a shared
device cannot be made noise-proof, only noise-tolerant. When the residual risk
materialises the run reports a decisive wrong-way lead WITH its whole distribution
— evidence for a human — instead of a bare red on a coin flip.

### 4. The repro and the negative probe (`firstopen_ab_repro_test.go`, Rule 10.5)

The merge-path red is reproduced **as data, not as load**. Rule 27 forbids making
the device sick to demonstrate that a busy device breaks the instrument, and it is
not needed: the defect was never "the device was busy", it was "one busy sample
decided the verdict", which is fully reproducible in a slice. The generator
(`firstOpenABReproClean`) draws from the MEASURED noise model above, seeded with a
fixed constant so the data is identical on every run — a repro whose data varies
would be the flake it exists to prevent.

- `TestFirstOpenNoisyNeighbourDoesNotFlipTheVerdict` — one sample set, both halves
  of the claim: the OLD one-sample comparison is red on it (the bursted pair's
  relaxed build is the slower one), the NEW statistic is green on the same data and
  the burst costs the losing side exactly ONE pair more than the same run without
  it.
- `TestFirstOpenABGenuineMisattributionStillFails` — the negative probe: the
  arms' roles swapped (a systematic wrong-way effect) still reports the
  misattribution through the same path, and the assertion stays silent on a
  too-short run, so it cannot be satisfied by an assertion that always fires.
- `TestFirstOpenABDecisionTable` — verdict, counts and evidence over eight literal
  sets, including a uniform lead, a near-even split, one huge wrong-way pair, ties
  dropped, and the too-few-pairs floor.
- `TestFirstOpenABExactNullMatchesBruteForce`, `TestFirstOpenABMidranksAreAveragesInTieGroups`,
  `TestFirstOpenABRankStructureDecidesNotTheWrongWayOnly`,
  `TestFirstOpenABMedianIsTheLowerMiddle`, `TestFirstOpenABPairOrderIsBalancedAcrossTheRun`
  — the arithmetic (DP vs brute force), the tie convention, the rank behaviour, the
  median's shape, and the pairing design.

Two limit statements the tests state rather than hide: with 3 pairs no lead can
reach the rate (the verdict is ambiguity — the rate implies it), and DOWNWARD
outliers (a pair made unusually fast) carry large ranks and can suppress a real
signal into a skip. That direction fails safe, which is the asymmetry the rework
is built on; a contended device cannot produce it, since a slow neighbour only
adds time.

## Two false starts, kept because they are the path (`Rule 23`)

The first rework — 9 interleaved pairs + a paired-sign MAJORITY with a "≥3 pairs
against" rule — is the design recorded in `.evid2/` (7 green A/B runs, votes 8-1 …
6-3) and it is SUPERSEDED. Its acceptance evidence is green and its reasoning
still has something to offer, but the rule was not a significance test and the
first fresh run under the acceptance protocol (`.evid3/lead3-redrun.log`) produced
a genuine **6-3 wrong-way split** and went RED — with the devdoc of the time
claiming in the same breath that a 6-3 split is what a fair coin produces ~25% of
the time. A threshold that fires on a fair coin is the original defect one level
down.

A second intermediate (`mean ± sd` t-test with the control's sd) was rejected on
measurement too: with 4-12 control pairs its heavy tails gave 16% false RED and
~12-43% power, and a single 200ms burst in an 11% contaminated field flipped it
red — the mean is exactly the statistic a burst can buy.

Both are why the final statistic is rank-based with an EXACT null: the two things
the false starts got wrong (a threshold too weak to be a test, and a statistic a
burst can buy) are the two properties the DP + ranks fix.

## Verification

Evidence, raw: `.evid4/final-runs.log` (three focused runs + three full-package
runs + both gates, with the load at each), `.evid4/stylegate-final.txt`,
`.evid4/calibration/` (the measured noise model and the simulation transcript),
`.evid3/lead3-redrun.log` (the superseded design's real red), `.evid2/` (the
first rework's runs, superseded), `.evid/` (an earlier three-run set).

| check | result |
|---|---|
| `CGO_ENABLED=0 go build ./...` | BUILD_OK |
| `go test -count=1 ./internal/standards/` | PASS — the six Rule 16-21 gates, still on the smoke path |
| `go test -count=1 ./internal/stylegate/` | FAIL at the package level on this host, **finding sites identical on both sides (216 each, empty set diff)** and no finding naming any file this branch touches — see below |
| `go test -count=1 ./internal/tools/` x3 | 0 failures of `TestFirstOpenBlockDeviceCostIsTheSync` (three full runs at load 19.87 / 2.44 / 1.61, each ~30-260s); the A/B log line reports its verdict and distribution in every run. The three full runs' FAIL sets are IDENTICAL and are exactly the five pre-existing tests below |
| the focused A/B + all new tests, x3 | all green, including the repro and both probes |
| `sha256(internal/tools/firstopen_sync_test.go)` | master `02ed49c9dd821f6e80d18435690998c5694ee2383ab779b1060654a032d70e0c` → `cc592964d6bf82c9fa8560bddbc565765b3f8a225ff5b21299873d34eeb05b35` — **changed** |
| `sha256(internal/tools/firstopen_sync_decision.go)` | `74889cb194f71ee0743a7d73d7d22899ec2cda7dbe5f9123704e68786fb53bdf` (new file) |
| `sha256(internal/tools/firstopen_ab_repro_test.go)` | `b9fa9906ba9c78f7e9b1f112f2f49a1649dc7bbcc65105e4c0861f991fd58bf5` (new file) |
| `git ls-files \| grep -c 'firstopen-device'` | **0** — the 106 blobs `c2f5a9de` tracked are untracked again (`9918ecec`), and the pattern is ignored at any depth (`git check-ignore -v` verified) |

### The A/B's measured distribution

`go test -count=1 -v -run TestFirstOpenBlockDeviceCostIsTheSync ./internal/tools/`,
five runs on this host while other agents' work was running.

| run | load | pairs relaxed/durable | W- | W+ | required | verdict | outcome |
|---|---|---|---|---|---|---|---|
| 1 | 30.69 | 27 / 5 of 32 | 493.0 | 35.0 | 414.5 | `relaxed-cheaper` | **PASS** |
| 2 | 28.16 | 23 / 9 of 32 | 382.0 | 146.0 | 414.5 | ambiguous | SKIP |
| 3 | 30.27 | 19 / 13 of 32 | 324.0 | 204.0 | 414.5 | ambiguous | SKIP |
| 4 | 29.43 | 22 / 10 of 32 | 366.0 | 162.0 | 414.5 | ambiguous | SKIP |
| 5 | — | 17 / 15 of 32 | 322.0 | 206.0 | 414.5 | ambiguous | SKIP |
| 6 (focused, load 23.74) | 23.74 | 24 / 8 of 32 | 445.0 | 83.0 | 414.5 | `relaxed-cheaper` | **PASS** |
| 7 (focused, load 20.85) | 20.85 | 25 / 7 of 32 | 462.0 | 66.0 | 414.5 | `relaxed-cheaper` | **PASS** |
| 8 (focused, load 21.18) | 21.18 | 17 / 15 of 32 | 322.0 | 206.0 | 414.5 | ambiguous | SKIP |
| (the calibration rounds, same code path, quieter host) | 6.7-11.9 | 22-29 / 3-10 | — | — | — | `relaxed-cheaper` | PASS |

No run reported the wrong direction. The skips are the honest answer to a host at
load ~30 where the effect and the noise are the same size — and they are the
behaviour the old instrument lacked, which had no way to say "this run cannot
tell". Run 5 is the extreme case in one row: the two arms' MEDIANS differ by 5%
(23.967 vs 22.723ms) and the pair count is 17-15, i.e. the classic coin-flip
picture the old test would have read as an ordering one way or the other, while
the statistic correctly reports that this run has nothing to say.

### The acceptance batch at the quiet end of the host (`.evid4/`, the re-run)

The rows above were collected while the host was busy. The acceptance batch was
then re-run on the same code at load ~1.6-2.1 (`go test -count=1 -json
./internal/tools/` x3, streams in `.evid4/tools-run{1,2,3}.json.gz`, and
the summarised batch in `.evid4/accept-batch.log.gz`), which is the condition the
test has to be right in as well:

| run | tests seen | the A/B's verdict line | outcome |
|---|---|---|---|
| `.evid4/tools-run1.json.gz` | 1586 | 30 relaxed / 2 durable, W- 496.0, W+ 32.0, required 414.5, `relaxed-cheaper`, noisy >3x median 0/128 | **PASS** (1.59s) |
| `.evid4/tools-run2.json.gz` | 1586 | 27 / 5, W- 435.0, W+ 93.0, required 414.5, `relaxed-cheaper`, noisy 3/128 | **PASS** (2.26s) |
| `.evid4/tools-run3.json.gz` | 1586 | 30 / 2, W- 493.0, W+ 35.0, required 414.5, `relaxed-cheaper`, noisy 0/128 | **PASS** (1.55s) |

Three of three, no skip and no wrong direction, and the whole A/B costs ~1.6-2.3s
of the package's budget. The `noisy >3x median` counter is the trace of the real
outliers the run met: run 2 saw a 106.764ms durable sample and a 79.743ms one
against a 10.450ms median — the exact shape the OLD instrument would have read as
evidence about the arms. The rank statistic absorbs them (W+ = 93 against a
required 414.5) and reports the same verdict the other two runs do.

The three runs' FAIL sets are identical and are exactly the five pre-existing
tests below — run 3's stream is the only one that does not reach the end of the
package (it is the run whose `TestMemoryBaseOutOfPolicyRegistrationNamesTheRemedy`
row differs); the A/B row itself is `pass` in all three.

### The pre-existing failures, measured rather than assumed

Five other tests fail in every full-package run on this host, and stylegate fails
at the package level too. All were proven pre-existing by running the identical
selection in a detached worktree at master `bf4d1941`
(`~/workspace/gopi-firstopen-base`): `TestFirstOpenSyncModeDecisionTable/memory-filesystem-ledger`,
`TestMergeBuildGate_GoCacheLeavesNoScratchCache`, `TestSuiteScratchRootIsMemoryBacked`,
`TestScratchRootFilesystemClassification`,
`TestMemoryBaseOutOfPolicyRegistrationNamesTheRemedy`, plus the stylegate gates.

They share one cause: this host's suite scratch root
(`/home/user/workspace/gopi-test-scratch`) is **block-backed** while the fixtures
and those two gates expect a memory-backed one, so `firstOpenSyncMode` correctly
measures `NORMAL` where the decision table wants `OFF`. Pointing `GOPI_TEST_TMPDIR`
at a tmpfs makes that selection green — measured: with
`GOPI_TEST_TMPDIR=/dev/shm/gopi-fo-smoke`, `-run 'TestFirstOpen' -v` reports 0
failures. That is the environment, not this branch.

**The stylegate proof is a finding-SET diff, not an exit code.** Because the
package is red at master for unrelated reasons, its exit code proves nothing about
this branch. The evidence is a finding-site set diff between the branch and a
DETACHED worktree at the branch's base `bf4d1941` (`/home/user/workspace/gopi-fo-base`,
removed after the measurement), each side run as
`go test -count=1 ./internal/stylegate/` and each side's sites extracted by the
SAME `grep` (`^\s+<path>.go:<line> \(`) and sorted:

| side | file | finding sites | `ignored-error` (test) | `errorf-verb` (prod) | `ignored-error` (prod) |
|---|---|---|---|---|---|
| base `bf4d1941` | `.evid4/stylegate-base.txt` | 216 | 201 / ceiling 182 | 9 / ceiling 8 | 1 / ceiling 0 |
| branch tip | `.evid4/stylegate-final.txt` | 216 | 201 / ceiling 182 | 9 / ceiling 8 | 1 / ceiling 0 |

`comm -13 <base.set> <branch.set>` → **empty** (and `comm -23` empty too): the same
216 sites, and the same nine `errorf-verb` sites, five of them in
`internal/tools/memory_procedural.go`; the same `ignored-error` test hits; the same
single production hit in `internal/tools/merge_union_gate.go`. **No file this
branch touches appears in any rule's site list.**

One counter MOVES between the two runs and it is neither a finding nor a
regression: `gate_test.go:115: undecidable sites (reported, never guessed)` reads
**145** at the base and **146** on the branch. It counts sites whose type
information the checker could not resolve — `stylegate.go`'s `Visitor` skips a
file whose merged-in type checking fails and counts those, deliberately reporting
rather than guessing (`Undecidable int` is a report field, not a violation). It is
a run-time artifact of the shared host and it changes between runs of the SAME
code, which is why the finding-site set (identical) is the evidence and the
counter is not.

A previous editing pass in this report compared `.evid2/gates-base.txt` against
`.evid4/stylegate-final.txt` and concluded "223 findings each, empty diff" on the
strength of line counts. That comparison was measured, not reasoned: the two files
held 216 finding sites each, the seven-line difference was header/boilerplate
(`FAIL` verdict lines, the block-backed-scratch note), and the "223" figure came
from `diff`ing unsorted files whose rule-block ORDERING differs run to run. The
table above is the corrected measurement.

One finding WAS introduced on the way and is fixed (below).

### A data-hygiene defect this branch also introduced, and its repair

The evidence commit `c2f5a9de` swept **106 SQLite scratch files (~13.6 MB) from
`internal/tools/firstopen-device-<pid>/` into the tree**. The mechanism, not the
typo: the A/B's fixture needs a REAL block device and must therefore create its
directory inside the checkout (`os.MkdirTemp(".", "firstopen-device-")` — a
memory-backed temp root would defeat the measurement), the directory is removed in
cleanup, and a `git add` run while the test was mid-flight captured it. The
repo-root junk gate (`internal/standards.TestRepoRootHasNoTrackedJunk`) reads
`git ls-files` but validates repo-ROOT paths only, so a **nested** tree is outside
its field of view — the gate was not wrong, it was blind to this shape.

Repaired in `9918ecec`: the 106 blobs untracked (`git rm --cached`, files left on
disk for the running batch), and both in-tree device patterns ignored
end-anchored (`firstopen-device-*/`, `ledgerpin-device-*/` — `git check-ignore`
confirms the pattern matches at any depth), with the reason, the two call sites and
the incident written as a comment above them so the next reader does not re-add the
pattern's justification from scratch. `git ls-files | grep -c 'device-'` is now 0
(the one remaining hit, `devdoc/rule18-voice-device-else.md`, is a filename that
merely contains the word).

The general lesson is worth more than the incident: **a fixture that has to write
inside the source tree must arrive together with its ignore pattern**, because the
"is this path junk" gate cannot see nested trees.

### One finding this branch introduced, and the mechanism behind it

`t.Skipf`'s reason is a format string built at run time, which stylegate's
`test-skip` rule accepts only with a documented reason — `//stylegate:ignore
<reason>`. Both markers were initially written as trailing comments on the LAST
line of the multi-line call. Both were reported: the rule resolves the marker from
the offending **call's** line (`reasonOverSpan(call.Pos(), …)` in
`internal/stylegate/stylegate.go`, `checkSkip`) and scans only FORWARD, so a marker
two lines below the call sits outside the span — while statement-position rules
(`os.Chdir`) span the whole statement and are happy with a trailing marker. Moving
both markers onto the `t.Skipf(` line took the finding-site count back to the
base's 216.

## Not done (honest)

- **The branch is based on `bf4d1941`; master has moved.** Re-checked at the end of
  this session against master's tip `6a6aef6d`: `git diff --name-only bf4d1941
  master -- <the three files + .gitignore>` is **empty**, i.e. master touched none
  of them, and master still carries the ORIGINAL test file
  (`sha256 = 02ed49c9dd82…d70e0c`, this branch's base). The merge channel was also
  verified earlier by merging into a detached worktree at master's then-tip
  `b627a081`: no conflicts, and it built. The branch was NOT rebased — rebasing
  would rewrite commits the operator may already be reviewing, and the A/B's
  outcome does not depend on the base.
- **Five pre-existing `internal/tools` tests and the stylegate gates are still red
  on this host** (above), all traceable to the suite scratch root being block-backed
  here. None is touched by this branch. Repairing them means re-deciding where the
  suite's memory-backed scratch root lives (or making
  `TestFirstOpenSyncModeDecisionTable` assert the MEASURED mode rather than a fixed
  expectation) — a different task with its own blast radius, Rule 26 territory.
  Stated so it is dispatchable: a red gate package cannot distinguish
  "pre-existing" from "new" by exit code, so any branch touching a gated shape needs
  the finding-set diff as its evidence (the recipe is memory record #1931).
- **The instrument's own limit, stated rather than hidden**: the signed-rank null
  is symmetric, so while UPWARD outliers (the only shape a contended device
  produces) cannot manufacture a wrong-way verdict, a DOWNWARD outlier — one pair
  made unusually fast — carries a large rank and can suppress a real signal into a
  skip. That fails safe (a logged non-verdict, never a red merge) and is the
  direction the rework is built around, but it is a cost, not a free lunch.
- **The threshold is a calibrated probability, not a proof.** At the real effect
  size the run skips ~38% of the time on a host at load ~30. That is the price of
  not going red on noise, and it is paid in skips rather than in false merges. An
  operator who wants higher power on a quiet host gets it for free (the same code
  passes ~90% at load ~7); one who wants it on a busy host needs more pairs, which
  is a smoke-budget decision rather than a statistical one.
- **No heavy-test classification was added**, deliberately: ~6s of smoke cost is
  not heavy, and Rule 27's "if a contention-dependent measurement were genuinely
  required it must be capped and opt-in" case does not arise — nothing here depends
  on contention any more.
- `firstOpenABPairs`, `firstOpenABMaxFalseVerdictRate` and
  `firstOpenABMinSignalMs` are this host's calibration, in one place each, and the
  log line prints both the values and the measured distribution — a host where they
  are wrong says why instead of going quietly red.

## The evidence files, and which design they belong to

| path | design | still valid? |
|---|---|---|
| `.evid4/` (streams gzipped; `accept-batch.log.gz` summarises the second batch) | the FINAL rework (order-balanced signed rank, exact null) | **yes — the acceptance evidence** |
| `.evid4/calibration/` | the measured noise model + the simulation that sized the design | yes — the calibration of record |
| `.evid3/lead3-redrun.log` | the first rework's real red under the acceptance protocol | yes — why it was superseded |
| `.evid4/final-runs.log` | the final acceptance batch: 3 focused runs + 3 full-package runs + both gates, with the load at each | yes — the acceptance evidence |
| `.evid4/stylegate-{base,final}.txt` + `.set` | the stylegate finding-SITE sets on both sides of the branch | yes — the zero-new-findings proof |
| `.evid2/` | the first rework (9 interleaved pairs, majority + lead-3) | history: green runs of a superseded instrument |
| `.evid/` | an earlier three-run set (same superseded design) | history |
