<!-- doc-class: normative -->
# The Peak-Hour Cost Guard Was Removed Outright (2026-09-14)

**Branch**: `fix/remove-peak-guard` · **Base**: master `7099edf0`, merged with
master `c7ab803a` (the cost-aware rework landed while this task ran)
**Removal commit**: `bb0c4c1a` (this is the commit the other sites name when they mark the
retired `GOPI_IGNORE_PEAK_HOURS` knob as gone)
**Operator decision**: remove the feature, not fix it again.

---

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

## 1. What the guard was, and what it did wrong

The guard sat at the very top of `main()` — before the subcommand dispatch — and
**refused to START** a gopi invocation while the clock was inside a declared
peak window (DeepSeek 高峰时段, Beijing 09:00–12:00 / 14:00–18:00, billed at 2x).
The intent was money: a run started inside a 2x window spends double for its
whole life.

The mechanism was the defect. To decide "would this start spend?", the guard
answered a different question — "is this a command *shape* we associate with
spending?" — by classifying the **command verb** (`applyPeakGuard` →
`peakGuardVerb` / `costFreeInvocation` / later `spendingStart`). A verb is not a
cost:

- `gopi task show <id>` costs **nothing** (it reads the ledger) and was refused.
- The **merger's mandated verify child** is spawned by the merger *after* a merge
  lands on master. Inside a window the child was refused at start (exit 4), so
  the landing was never judged — the merge flow itself was broken, up to four
  hours a day.
- The **e2e suite** went red inside a window: 84 `cmd/agent` cases, including two
  merger rigs, because the clock — not the code — decided the verdict. The
  operator's own `gopi worker --daemon` could not start.

Three successive reworks (token-free verbs → exact boundaries → cost classes)
each moved the whitelist without changing the mechanism: a start-time guess at
spend, made before anything is known. Twelve related tasks produced **3 bug rows
and 5 merge-conflict branches**:

| Kind | Task / branch | What it was |
|---|---|---|
| bug | `01a09a69` | `NextOffPeak` walked hours from the current minute → refusal over-stated the window by up to 59m |
| bug | `01a09d73` | guard blocked non-LLM commands (`version`/`--help`/`daemon`) → e2e suite red 09:00–12:00 & 14:00–18:00 |
| bug | `01a0a03d` | post-merge verify child killed by the guard (exit 4) — a bugfix merged in a window is never judged |
| conflict | `feat/peak-hour-guard` (`01a09897`, reworked `01a098a1`/`01a098b0`) | merge conflicts into master |
| conflict | `fix/peak-hour-build-gaps` (`01a09957`) | merge conflicts into master |
| conflict | `fix/peakhours-exact-boundary` (`01a09c48`) | merge conflicts into master |
| conflict | `backup/peakhours-exact-on-ec6ba9f1` (`01a09aaf`) | merge conflicts into master |
| conflict | `fix/peak-guard-cost-class` (`01a09fc2`) | merge conflicts into master — the rework that landed as cost-aware |

> **Honest note on the count.** The briefing says "4 bugs and 5 merge conflicts".
> An enumeration of the live ledger finds **3** bug rows (the table above) and
> the **5** conflict/rework rows. I report what I could verify; if a fourth bug
> row exists it is not tagged or worded such that a ledger scan finds it.

The **cost-aware rework** (`fix/peak-guard-cost-class`, merged as `c7ab803a`
while this task ran) fixed bug `01a09d73` properly — a per-cost-class predicate,
default = spending, fail closed — and is the best version of the guard that
exists. The operator's call was nonetheless to remove the feature: an elaborate
classifier that must be kept in sync with every verb, to stop a hazard that a
resident worker should handle by deferring its *claims*, not by refusing starts.

**One rule for the next feature:** a start-time gate must not decide by *verb*.
If the concern is spend, gate the *moment of spend* (an API call), or defer work
in the loop that owns the queue — never the launcher.

---

## 2. What was deleted

**Production (guard + configuration surface)**

| Path | What it held |
|---|---|
| `cmd/agent/peak_guard.go` | the start gate: `applyPeakGuard`, `peakGuardVerb`, `costFreeInvocation`, `ParsePeakFlags` |
| `cmd/agent/cost_class.go` | master's cost-aware classifier (`spendingStart`, `costClassOf`) — guard-only, no other consumer |
| `internal/config/workhours.go` | window grammar, `ResolvePeakHours`, `PeakHoursDecision`, `PeakRefusal`, `PeakGuardExitCode`, `NextOffPeak` |
| `internal/peakhours/` (whole package) | window model + `Beijing()`; its **rate windows moved into `internal/pricing`** (§3) |

**Wiring**

- `cmd/agent/main.go` — the start-gate block and its `costFreeInvocation` / `spendingStart` / `applyPeakGuard` call.
- `cmd/agent/cli_tasks_worker.go` — the per-claim peak deferral (`workerPeakWait`, `peakWarned`, `workerPeakWarnOnce`) and its call in the round loop.
- `cmd/agent/cli_tasks_worker_stop.go` — the drained-wait clamp that slept to the peak boundary.
- `cmd/agent/merger_verify_child.go` — the exemption argv token, `mergeVerifyPeakChildReason`, `mergeVerifyRecordPeakChild`.
- `cmd/agent/cli_verbs.go` — `topLevelVerbWords` / `isTopLevelVerb` (guard-only argv classifier) and the guard-facing doc comments on the vocabulary sets.
- `internal/config/{config,env,flags,settings,help}.go` — the `PeakHours` / `IgnorePeakHours` fields, the env feed, both flag cases, both settings-table rows, the `ignore_peak_hours` bool-key, the guard's help group.

**Tests (guard-only)**

Deleted: `cmd/agent/peak_guard_test.go`, `cmd/agent/peak_guard_costaware_e2e_test.go`, `cmd/agent/cost_class_test.go`, `cmd/agent/merger_verify_peak_child_test.go`, `internal/config/peakhours_decision_test.go`, `internal/config/peakhours_flag_spelling_test.go`, `internal/peakhours/peakhours_test.go`.

**Workarounds restored** (the tests went back to their pre-guard clean form):

- `cmd/agent/testmain_test.go` — the suite-wide `unpinPeakHourGate()` pin removed (it existed because the clock decided test verdicts).
- `agent_spawn_guard_test.go`, `merger_graceful_restart_e2e_test.go`, `merger_singleton_e2e_test.go`, `worker_daemon_flag_e2e_test.go` — every `disarmedPeakEnv(...)` call removed; each rig states in a comment why it no longer needs a peak exemption.

**Gate bookkeeping**: the standards gate's Rule-16 permit was re-keyed
`cmd/agent/main.go:943 → 917` (the site shifted because the guard's block above it
was deleted). Re-keyed **with its reason**, never relaxed — the site is still the
same byte-identical false positive.

## 3. What was deliberately KEPT

- **`internal/pricing`** and `gopi stats --cost`. Peak windows are a **provider
  rate fact** (DeepSeek bills 2x), not a gopi policy: deleting the guard removes a
  *policy*, never the fact. The windows now live in `internal/pricing`
  (`deepseekPeakWindows` + `inPeakWindow` + `beijing`), the package that prices
  sessions — and the deleted `internal/peakhours` was its only other consumer.
  New tests re-establish the boundary coverage the deleted package owned:
  `internal/pricing/peakwindows_test.go` (windows, half-open edges, fixed +08:00
  zone), plus the existing `TestPeakDoublesMoney`.
- **`internal/testutil/worker_ownership_gate_test.go`** — its two "peak" strings
  are **devdoc filenames** in a gate allow-list, not guard symbols. Devdocs are
  history; the filenames stay.

## 4. Backward tolerance (a configured host must still start)

An existing `settings.json` may still carry `peak_hours` / `ignore_peak_hours`.
`settingsFromFile` decodes with a plain `json.Unmarshal` (no
`DisallowUnknownFields`), so **unknown keys are ignored** and the file keeps
loading. That property is now **pinned**, not assumed:
`internal/config/peak_guard_removed_test.go` feeds a settings.json carrying both
removed keys beside a live knob and asserts it loads with the knob intact, and
asserts no `Config` field carries either json tag again (a tripwire: reviving the
guard must be deliberate).

The command-line spelling behaves the same way: `--ignore-peak-hours` is now an
**unknown flag** (refused by name), and `--peak-hours X` is an **inert token**
(no refusal, no effect) — asserted at runtime in §5.

## 5. Proof of absence (not an assertion — a measurement)

### (a) Exhaustive scan

```
$ cd <worktree> && git grep -niE \
  'peakHour|PeakHour|peak_hour|PeakGuard|peakGuard|IgnorePeakHours|ignore_peak_hours|\
PeakRefusal|PeakGuardExitCode|NextOffPeak|ResolvePeakHours|PeakHoursDecision|\
applyPeakGuard|costFreeInvocation|spendingStart|costClass' \
  -- . ':!devdoc' ':!tasks' ':!briefings' ':!internal/standards/baseline.go' \
     ':!cmd/agent/peak_guard_removal_test.go' ':!internal/config/peak_guard_removed_test.go' \
     ':!internal/testutil/worker_ownership_gate_test.go' ':!internal/config/config.go'
internal/pricing/pricing.go:211:  // internal/peakhours package shared with the peak-hour START GUARD, a launcher
internal/pricing/peakwindows_test.go:12: // WHY IT LIVES HERE: these windows used to be owned by an internal/peakhours
```

Two hits, both **prose recording where the rate windows came from** (the
`internal/peakhours` *directory* name, which no longer exists). The four excluded
files are each explained: the two removal proofs must name what they forbid, the
gate allow-list holds devdoc filenames, and `config.go` carries the tombstone
comment for the removed settings keys. **No production code path references the
guard.**

### (b) Negative test (run, not claimed)

`cmd/agent/peak_guard_removal_test.go`:
- `TestPeakGuardSymbolsAreAbsentFromTheTree` — walks every `.go` file in
  `cmd/agent`, `internal/config`, `internal/pricing` and fails on any of the 15
  removed identifiers as a **whole word** (a substring match would cry wolf).
- `TestPeakGuardWireTokensAreAbsentOutsideHistoryAndBackwardTolerance` — no
  `--ignore-peak-hours` / `GOPI_IGNORE_PEAK_HOURS` / `peak_hours` /
  `ignore_peak_hours` left anywhere, with the backward-tolerance proof and the
  tombstone as named, reasoned exceptions.

**Red proof** — the same file transplanted onto pristine master **fails**:

```
$ go test -run TestPeakGuardSymbolsAreAbsentFromTheTree ./cmd/agent/   # on master + this test
--- FAIL: TestPeakGuardSymbolsAreAbsentFromTheTree
    is not the file that should be tested  (29 hits across peak_guard.go,
    cost_class.go, main.go, peak_guard_test.go, …)
```

A negative test that cannot fail proves nothing; this one fails on master.

### (c) The clock cannot decide a verdict — A/B against pristine master

The removed guard read its windows from `GOPI_PEAK_HOURS`, so a window covering
the **current** hour (`TestCostFreeVerbsRunInsideAPeakWindow` derives it as
`fmtWindowCovering(time.Now())` — real at any clock time, not "run this at
14:30") makes the refused condition genuine. Measured **2026-09-14 23:58 CST**,
window `23:00-24:00`:

```
master binary (7099edf0, guard present)          this branch (guard removed)
gopi task list               exit=4  REFUSED     gopi task list               exit=0  1212 task(s):
gopi task show 01a098a1      exit=4  REFUSED     gopi task show 01a098a1      exit=1  no task with id (normal)
gopi version                 exit=0  (exempt)    gopi version                 exit=0  dev
```

Same argv, same clock, same window — master refuses, this tree runs. The refusal
text was `[gopi] peak-hour cost guard: refusing to start — 'gopi task list' would
run inside DeepS…`. With `GOPI_REMOVAL_CONTROL_BIN=<master binary>` the test runs
this comparison itself, so the control is executable rather than a claim:

```
$ GOPI_REMOVAL_CONTROL_BIN=/path/to/master-gopi go test -run TestCostFreeVerbsRunInsideAPeakWindow ./cmd/agent/
--- PASS: TestCostFreeVerbsRunInsideAPeakWindow
```

### (d) `stats --cost` still prices peak (kept feature, verified)

```
$ GOPI_HOME=<sandbox> gopi stats <session-id> --cost
Session 8793b212…   total 0.4716 元      (regime resolved; peak windows intact)
```

### (e) What happened to `--peak-hours` / `--ignore-peak-hours`

Both are **gone**, and they leave in different shapes (both asserted by
`TestRemovedPeakHoursFlagsNoLongerAffectAStart`):

```
gopi --ignore-peak-hours task list       exit=1  gopi: unknown flag --ignore-peak-hours
gopi --peak-hours 09:00-12:00 task list  exit=0  No tasks found.   (inert — no refusal, no effect)
```

Nothing exits **4** for a clock reason any more.

### (f) Full-suite verification (`go test ./...`)

`go test -count=1 ./...` on the merged branch: **all touched packages green** —
`cmd/agent` (470s, including the four repaired rigs and every removal proof),
`internal/config`, `internal/pricing`, `internal/standards` (the six machine
gates), `testsuite`, `internal/stylegate`.

Three gate failures appeared and are **NOT from this change** — each was
reproduced on a pristine worktree of the base this branch merges
(`git worktree add --detach <dir> master`): `TestPinCoverageTable` +
`TestEveryTempUsingSuiteIsPinned` (`internal/daemoncwd` creates scratch with no
TestMain pin — master diagnostic text identical) and
`TestRedProof_LandedButRefKeptIsFiledAsFailure` (`internal/tools`, a red-proof
test that no longer reproduces its defect). Master has since advanced to
`7b5d427a` *"Merge branch 'fix/daemon-e2e-leak'"*, which targets exactly the
daemoncwd/daemon-leak family; **the branch was not re-merged** — the merger
lands on the current tip and these are orthogonal to the removal.
One failure WAS mine and is fixed: the devdoc itself tripped
`TestWorkerOwnershipPointerPresent` (it quotes the worker-launch form), so it now
carries the canonical operator-only pointer line.

### (g) What happened to the guard's own knobs (operator-facing summary)

| Knob | Before | After |
|---|---|---|
| `--ignore-peak-hours` | disarmed the guard | unknown flag (refused by name) |
| `--peak-hours W` | set the windows | inert token (no refusal, no effect) |
| `GOPI_IGNORE_PEAK_HOURS` | disarmed the guard | ignored |
| `GOPI_PEAK_HOURS` | set the windows | ignored |
| `settings.json: ignore_peak_hours` | durable disarm | ignored (file still loads) |
| `settings.json: peak_hours` | window declaration | ignored (file still loads) |
| exit code 4 | clock refusal | retired — nothing exits 4 for a clock reason |

## 6. Not Done (honest)

- **A fourth bug row could not be found.** The briefing says 4 bugs / 5
  conflicts; the ledger enumeration (§1) finds 3 bug rows / 5 conflict rows.
  Nothing was suppressed — the scan is reported so it can be re-run.
- **The redundancy was not re-architected.** Deferring *claims* inside the
  resident worker loop (rather than refusing starts) was considered as the
  *replacement* for the guard and deliberately **not** built: the epic is a
  removal. A future task may want it; §1 records the rule it must follow.
- **`--peak-hours` is inert rather than refused.** The generic flag path
  (`--zzz 5 task list` behaves identically on master) ignores a leading unknown
  token when a recognized verb follows, so `--peak-hours X` is swallowed. Making
  it a hard error would be a **behaviour change for global flag parsing**, not
  part of removing the guard. Behaviourally it is what an operator wants (no
  refusal); if the swallowed token is judged a defect it is a separate task
  (`cmd/agent/main.go`'s global flag scan).
- **No host-level configuration was touched.** `/home/user/.bashrc`,
  `settings.json` on the host, launcher scripts and cron entries that may still
  export `GOPI_IGNORE_PEAK_HOURS` were out of scope — those are the operator's
  files. The variable is **RETIRED: read by nothing** (the env reader left with
  the guard), so setting it changes nothing at all — no exemption is needed to
  run any gopi verb or any suite. A later sweep
  (`fix/peak-guard-stale-instructions`, 2026-09-16) removed the last live
  instruction sites that still taught it as load-bearing.

## 7. Recovering this work
```
git log --oneline master..fix/remove-peak-guard
git show fix/remove-peak-guard:devdoc/peak-guard-removal.md
```
