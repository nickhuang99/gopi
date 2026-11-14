<!-- doc-class: historical -->
# Decision Record: the peak orphan tests — keep the guard REMOVED (direction b)

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

**Task**: `01a0a3f9-8835-7e26-bf0e-2f710f15885d`
**Branch**: `fix/peak-orphan-audit` (fresh off master `e52ff94e`, Rule 6a worktree
`/home/user/workspace/gopi-peak-orphan-audit`)
**Date**: 2026-09-15
**Verdict**: **direction (b)** — master's orphan test files are already deleted;
the peak-hour cost guard stays **REMOVED**. Nothing in the cost path is
restored, re-classified, or re-wired.

---

## 1. What the epic reported, and what master actually is

The epic's premise: `cmd/agent`'s test build fails because
`cmd/agent/merger_verify_peak_child_test.go` references `config.PeakHoursEnv` /
`config.IgnorePeakHoursFlagSpelling`.

**Measured on current master (`e52ff94e`)** — both referenced paths are
**absent** from the tree:

```
$ git cat-file -e master:cmd/agent/merger_verify_peak_child_test.go      → absent
$ git cat-file -e master:internal/config/peakhours_flag_spelling_test.go → absent
$ go vet ./cmd/agent/ ./internal/config/                                 → (no output, exit 0)
$ go vet ./...                                                           → (no output, exit 0)
$ go build ./...                                                         → exit 0
$ grep -rn 'PeakHoursEnv|IgnorePeakHoursFlagSpelling' --include=*.go .   → only peak_guard_removal_test.go
                                                                           (the absence proof naming
                                                                            its own subject)
```

So master is **not** build-red. The premise is a **stale snapshot**: it was true
for the window between the archive merge that re-introduced the two files and
the commit that deleted them again.

**The full provenance** (five commits, each verified in git):

| # | commit | date | what it did |
|---|---|---|---|
| 1 | `bb0c4c1a` | Sep 15 00:12 | `remove(peak-guard)` — deleted the guard, its config surface, and both test files |
| 2 | `03526655` | Sep 15 13:22 | `archive(vp-a03d probe)` — preserved the verification run's *uncommitted staged residue*, i.e. **re-added both orphans to master** (+469 lines), "no branch contained them" |
| 3 | `c1cdf311` | Sep 15 | `Merge branch 'archive/vp-a03d-probe-…'` — carried the orphans into the merge result |
| 4 | `82f44223` | Sep 15 16:20 | `fix(config,cmd/agent): delete the stray peak-guard test files that broke the test build` — −469 lines, the deletion master carries |
| 5 | `e52ff94e` | — | current master tip; `82f44223` is an **ancestor** (`git merge-base --is-ancestor 82f44223 master` → YES) |

Step 2 is the defect *class* — a preservation/archive commit is a first-class way
to break a package's test build, because `go build ./...` does **not** compile
test files; only `go vet` or `go test -run XXXNONE` exposes it. That lesson is
already distilled in memory (record `#2861`). Step 4 is the repair, and it landed
**with no devdoc** (`git show --stat 82f44223` = 2 test files, 0 docs) — that
missing written decision is what this record supplies.

## 2. Why direction (b), and not (a)

Direction (a) would land the missing peak-hour config constants + cost-class
wiring out of the archived rework. Three independent lines of evidence say no.

### 2.1 The removal was an operator decision, and the guard's mechanism was the defect

`devdoc/peak-guard-removal.md` (on master, from `bb0c4c1a`) records the
operator's call — *"remove the feature, not fix it again"*. The guard classified
the command **verb** to guess whether a start would spend, and a verb is not a
cost. In twelve related tasks it produced 3 bug rows and 5 merge-conflict
branches:

| Kind | Task / branch | What it was |
|---|---|---|
| bug | `01a09a69` | `NextOffPeak` over-stated the window by up to 59m |
| bug | `01a09d73` | guard blocked non-LLM commands (`version`/`--help`/daemon) → e2e red 4h/day |
| bug | `01a0a03d` | post-merge verify child killed by the guard (exit 4) — a landed merge went **unjudged** |
| conflict ×5 | `feat/peak-hour-guard`, `fix/peak-hour-build-gaps`, `fix/peakhours-exact-boundary`, `backup/peakhours-exact-on-ec6ba9f1`, `fix/peak-guard-cost-class` | all conflicted into master; the cost-class rework landed as `c7ab803a` and was **then removed anyway** |

Read but deliberately not continued (Rule 23): the archived rework revisions
`ae98aea1` / `c7ab803a` / `f6c29122` are the best version of the guard that ever
existed, and the operator's call was still to delete it.

### 2.2 The measurement: known-cost-free verbs were refused — reproducible today

The decisive evidence is not the narrative, it is a **run**. I built the
pre-removal tip (`bb0c4c1a^` = `7099edf0`, where `cmd/agent/peak_guard.go`
exists) as a **control binary**, declared a window covering the whole Beijing day
(`GOPI_PEAK_HOURS=00:00-24:00` — the grammar accepts hour 24), and ran the same
argv through both binaries, same sandbox, same clock:

```
sandbox=/home/user/workspace/peak-ab-RLQ0

CONTROL 7099edf0 (guard present)          CURRENT e52ff94e (guard removed)
gopi version                  exit=0 dev  gopi version                  exit=0 dev
gopi task list                exit=4 ✗    gopi task list                exit=0 "No tasks found."
gopi task show does-not-exist exit=4 ✗    gopi task show does-not-exist exit=1 no such task (normal)
gopi task run bogus           exit=4 ✗    gopi task run bogus           exit=1 singleton guard, normal
gopi worker --once            exit=4 ✗    gopi worker --once            exit=0 (drains, exits)

control refusal banner (verbatim):
[gopi] peak-hour cost guard: refusing to start — `gopi task list` would run inside DeepS…
```

`task list` and `task show` are **ledger reads — they cannot issue an LLM call**
and they cost nothing; the control refuses them with the retired exit class 4.
That is bug `01a09d73`/`01a0a03d` still reproducing on a binary the epic proposes
to resurrect. A guard that refuses a zero-cost verb has no cost-guard value.

### 2.3 Landing (a) would make master's own gate red

`cmd/agent/peak_guard_removal_test.go` scans `cmd/agent`, `internal/config` and
`internal/pricing` and fails on any of **15** identifiers
(`applyPeakGuard`, `peakGuardVerb`, `costFreeInvocation`, `PeakHoursDecision`,
`PeakSettings`, `ResolvePeakHours`, `ParsePeakFlags`, `PeakRefusal`,
`PeakDeclarationWarning`, `IgnorePeakHoursFlagSpelling`, `IgnorePeakHoursFlag`,
`IgnorePeakHoursEnv`, `PeakHoursEnv`, `PeakHoursFlag`, `PeakGuardExitCode`),
with a path-keyed exemption list that names exactly where each may live.
Direction (a) restores the production half of that set → the scan fails → a
second removal task becomes mandatory. Restoring the guard is not a repair of
master; it is a **reversal of a decided removal**, which the epic did not ask for
and the evidence does not support.

## 3. The epic's non-negotiable, answered honestly

> "if direction (a) is chosen, the cost guard must not weaken — every argv that
> can issue an LLM call must refuse in a peak window with the SAME banner
> (`config.PeakRefusal`) and exit code (`config.PeakGuardExitCode=4`)"

The constraint is **conditional on (a)**, and (a) is declined — so there is no
guard to keep strong. What would otherwise be the epic's strongest objection to
(b) — *"does (b) weaken cost protection?"* — is answered by measurement rather
than assertion, and the answer is **the guard never provided it**:

- a verb-shape refusal is not a spend gate: `task list`/`task show` were refused
  (0 cost) while any verb the classifier did not know, and any **in-process** API
  call made after startup, was not;
- the guard broke the one spend-adjacent flow it was aimed at — the merger's
  mechanism-derived post-merge verify child, which costs money by design and
  inside a window was killed at start (bug `01a0a03d`);
- the removal kept the **rate fact**: `internal/pricing` still bills DeepSeek's
  高峰时段 at 2x, and `gopi stats --cost` still prices peak
  (`internal/pricing/peakwindows_test.go`, `TestPeakDoublesMoney`). Removing a
  launcher *policy* did not remove the *fact*.

`devdoc/peak-guard-removal.md` §1 states the rule for any successor: a
start-time gate must not decide by verb — gate the moment of spend, or defer
*claims* inside the resident loop that owns the queue. That successor is a
separate design task, deliberately **not** built here (the epic is the orphan
tests + the decision).

## 4. Where the refusal behaviour is asserted now

The epic accepts "the authoritative removal's absence-proof tests" as the
assertion form for (b). Master carries both halves:

| Test (file) | What it proves |
|---|---|
| `TestPeakGuardSymbolsAreAbsentFromTheTree` | 15 identifiers, **whole-word**, exhaustive walk of 3 trees — a resurrected call site fails |
| `TestPeakGuardWireTokensAreAbsentOutsideHistoryAndBackwardTolerance` | no `--ignore-peak-hours` / `GOPI_IGNORE_PEAK_HOURS` / `peak_hours` / `ignore_peak_hours` outside the two reasoned exceptions |
| `TestRemovedPeakHoursFlagsNoLongerAffectAStart` | runtime: the boolean flag is refused **by name** as unknown; the value flag is inert; **nothing exits 4** |
| `TestCostFreeVerbsRunInsideAPeakWindow` | a window covering the *current* hour (real at any clock time), same argv: this tree exits 0 and prints no refusal; its control half asserts the **pair** the epic names on the pre-removal binary |
| `TestWorkerIdleWaitDoesNotDeferToAPeakWindow` | the resident loop's wait is not clamped to a window boundary |
| `internal/config/peak_guard_removed_test.go` | backward tolerance: a settings.json still carrying both removed keys loads, knob intact; no `Config` field reclaims either json tag |

`TestCostFreeVerbsRunInsideAPeakWindow` defines the **A/B control hook** this
analysis used: with `GOPI_REMOVAL_CONTROL_BIN=<pre-removal binary>` the test runs
the *same* argv against the control and asserts it still exits 4. §2.2 is that
control, executed by hand. The absence proof therefore has an executable **red
proof**: the guard's refusal is not asserted from memory, it is reproduced on a
binary that carries the code the epic wanted to restore.

The control half asserts the **acceptance pair, not the exit code alone** — for
every spending argv the control must exit `4` **and** render the retired
`config.PeakRefusal` wording (`retiredPeakRefusalHead`, verbatim from
`internal/config/workhours.go` at `7099edf0`) **and** name the refused verb in
that banner. The verb is pinned per case because the retired classifier rendered
the VERB rather than the argv — `worker --once` refused as `gopi worker`, i.e. the
guard could not tell the one-shot form from the never-returning daemon, which is
precisely the guessing the removal answered. **Negative probe** (what makes the
assertion load-bearing rather than decorative): pointing
`GOPI_REMOVAL_CONTROL_BIN` at THIS tree's binary fires all three assertions for
all four argv — the green run above cannot be produced by a control that does not
refuse, and cannot be produced by a refusal that renders different words.

## 5. Environment notes (Rule 22 — temp work stays off `/`)

- The A/B sandboxes and binaries were created under `/home/user/workspace`
  (`mktemp -d /home/user/workspace/peak-ab-XXXX`); nothing was written to `/tmp`.
- `/dev/shm` measured **100% full** (`7.8G/7.8G`, 2.4M free) on this host, so
  `internal/testutil.SuiteTempRoot()` (which defaults under `/dev/shm`) was
  redirected with `GOPI_TEST_HOME=/home/user/workspace`. A `/dev/shm` **symlink**
  (`/dev/shm/gopi-verify-work → /home/user/workspace`) was created as a shim for
  verification helpers that hardcode that root; it consumes no space on the full
  tmpfs.
- **The detached-daemon rig does not come up on this host.** Three full-suite runs
  on this branch returned three different residual SETS; each residual was then
  reproduced on a pristine master worktree in isolation. Beyond the `SQLITE_BUSY`
  pair (`TestMergerDaemon_DisposesZombie…`, `TestKilledWorkerDaemon_MergerTickRecovers…`),
  `TestWorkerDaemon_StopEndsTheDaemon` and `TestWorkerDaemon_RestartReplacesTheDaemon`
  fail as `cgroup escape: systemd-scope (/usr/bin/systemd-run)` →
  `daemon start failed: … EOF` on master too (twice, two homes). Read the census in
  `tasks/01a0a3f9-report.md` before treating any single suite run's failure list as
  this tree's defect; a residual that is green on a re-run proves it is host state,
  not the diff.
- The control worktree is detached at `7099edf0` and is to be removed after this
  task (`git worktree remove --force`), together with the two A/B binaries.
- **Two `go test ./cmd/agent/` runs must never share one `GOPI_TEST_HOME`.** The
  first full-suite run of this task HUNG (10m `-test.timeout`, in
  `TestVoiceServeHTTPHandlerContract`) while a *concurrent* run by a sibling task
  held the same `GOPI_TEST_HOME` ledger; the same test then passed in 0.256s in
  isolation and the serial re-run completed in 400.327s. The suite's daemon rigs
  and any sibling process contend for one ledger SQLite file, and the symptom is
  a hang or `SQLITE_BUSY` — never a legible "someone else is using this home".
  Give each run its own home and serialise; do not read a hang as this tree's
  defect.

## 6. Not Done (honest)

- **The orphan files were not re-deleted by me.** They are already absent from
  master via `82f44223`; re-deleting would edit nothing. This task's contribution
  is the **record** (the deletion landed with no devdoc) plus the independent
  re-verification and the executable A/B red proof.
- **No cost guard was restored** — deliberate (§2, §3). Cost protection today is
  *pricing visibility* (`stats --cost` peak at 2x) plus the operator's own
  discipline; the claim-gate successor sketched in `peak-guard-removal.md` §1 is
  not built.
- **`fix/peak-orphan-tests-doc` (`79256d65`) was not merged.** It is a sibling
  task run that reached the same conclusion on its own branch (its devdoc is the
  long-form version of §1's provenance table); its one non-doc change touches the
  ledger DSN, outside this task's scope. Per Rule 9 the dispatcher merges, not
  me.
- **`internal/stylegate` is red on master and is NOT mine** — the briefing names
  it as a separate task; it is reported with its pre-existing status in the
  Rule-5 report, never as a failure of this change.
- **The acceptance PAIR was originally asserted only by hand.** Direction (b)'s
  control half checked the exit class alone; the follow-up branch
  `fix/peak-removal-banner-assert` adds the banner + subject assertions (with the
  negative probe) so the epic's criterion is executed by CI, and carries the
  corrected report. Master already carried the decision itself (`a6715b7b`).

## 7. Recovering this work

```
git log --oneline -3 master          # a6715b7b — the decision record
git show a6715b7b --stat             # the decision + this devdoc + the report
git show 82f44223 --stat             # the deletion master already carries
git show 03526655 --stat             # the archive commit that re-added the orphans
git show fix/peak-removal-banner-assert:tasks/01a0a3f9-report.md   # branch report
```
