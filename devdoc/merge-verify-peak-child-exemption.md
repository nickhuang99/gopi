<!-- doc-class: historical -->
# Task Report: the merger's verify child is spawned EXEMPT from the peak-hour start guard

> **RETIRED (2026-09-16).** Everything this report describes — the guard, the verify child's
> exemption, and the `GOPI_IGNORE_PEAK_HOURS` spelling used throughout — was removed outright by
> `fix/remove-peak-guard` (commit `bb0c4c1a`, devdoc/peak-guard-removal.md). The variable is read
> by nothing and no longer needs to be set, unset, or controlled for by any test. The report is
> kept as the record of a defect that WAS real; it is not a recipe.

Epic: `merge-verify-peak-child-exemption` (2026-09-15). Branch
`fix/merge-verify-peak-child-exemption`, worktree
`/home/user/workspace/gopi-verify-peak` (Rule 6a — a linked worktree off
master-tip `79738cb0`, not the main directory).

## The defect (root cause, not symptom)

`gopi task verify <id>` is a MODEL-SPENDING process, so the peak-hour cost guard
(`cmd/agent/peak_guard.go`) refuses to start it inside a DeepSeek 高峰时段 window
with exit `config.PeakGuardExitCode` (4). The merger's post-merge verify gate
spawns exactly that command (`runMergeVerifyChild`, `merger_verify_child.go`), so
inside a peak window the child was refused **at process start** and judged
nothing.

The gate reads its verdict **only from the ledger** (`tools.MergeVerifyLatestEvent`
/ row status), never from the child's exit status — so a refused child produced
no verdict at all, i.e. `child-dead`:

- before `c513327a`: the landed merge was **reverted** for work that was never
  measured;
- after `c513327a`: the merge is kept but **unverified**, and the requeued verify
  is itself refused for as long as the window lasts (up to four hours per
  weekday).

Root cause in one line: **a mandated judgement was gated by the clock, and the
gate cannot tell "the verifier died" from "the verifier was never allowed to
start".**

## What was implemented

1. **`cmd/agent/merger_verify_child.go`**
   - `mergeVerifyChildArgv(taskID)` — the child's argv in ONE place:
     `[--ignore-peak-hours, task, verify, <id>]`. The exemption is
     `config.IgnorePeakHoursFlagSpelling()` (referenced, never re-typed), and it
     is placed **first** because it is a PROCESS-GLOBAL flag: `main()` consumes it
     (`config.ParsePeakFlags` → `PeakFlags.ApplyEnv`) before the subcommand chain,
     so the `task verify` parser never sees it. The verb words are
     `CliVerbTask` + `taskverbs.Verify` (Rule 1b/13).
   - `runMergeVerifyChild` spawns that argv, and calls
     `mergeVerifyRecordPeakChild(rec)` at the spawn.
   - `mergeVerifyRecordPeakChild` reads the SAME decision function the guard
     reads (`config.PeakHoursDecision` — Rule 2: one predicate, not a second
     window model). When the moment **is** peak it records the deliberate cost
     decision on **three** surfaces: stderr, the judged row's append-only ledger
     event (`tools.MergeVerifyRecordEvent`), and the operator notify
     (`tools.MergeVerifyNotify`) — so the 2x is attributed, never silent.
   - `mergeVerifyPeakChildReason` names the mechanism from the config constant.
   - The file's contract header gained clause ④° documenting the decision, its
     cost rationale and the evidence shape of the failure it removes.

   **Why the child can be exempted at all (the grandchild hole it closes):**
   `ApplyEnv` writes `GOPI_IGNORE_PEAK_HOURS=1` into the *child's* environment, and
   `exec.Cmd.Env` here derives from `os.Environ()` (minus the daemon context), so
   the verify **agent's own** grandchildren — the `gopi task update` its bash
   runs to record the verdict — inherit the exemption too. Passing the flag only
   as argv would have let the child start and then be killed one level down.

2. **`internal/config/workhours.go`** — `LongFlagPrefix` (`"--"`) and
   `IgnorePeakHoursFlagSpelling()`: the spawner's argv token is *derived* from
   `IgnorePeakHoursFlag`, so the spelling the spawner passes and the spelling
   `ParsePeakFlags` accepts cannot drift (Rule 1b/13/14 — no `"--ignore-peak-hours"`
   literal anywhere in the spawn path).

3. **`internal/config/env.go`** — `EnvBoolOn` (`"1"`), the canonical ON value
   `ApplyEnv` writes and `EnvBool` accepts; the tests and the stub reference it
   instead of a bare `"1"` (Rule 1b). (Found while writing the test: the stub
   first exported `tools.AutoRegisterOffValue`, which is `"0"` — the *opposite*
   constant. The named ON value is what makes that class of mistake visible.)

## Tests (all new, all in this branch)

`cmd/agent/merger_verify_peak_child_test.go`

| test | level | red? |
|---|---|---|
| `TestMergeVerifyChildArgvExemptsThePeakGuard` | the spawn contract: argv order + token derived from config | **RED pre-fix** |
| `TestIgnorePeakHoursFlagSpellingIsConsumedByTheFlagsParser` | the spelling is parsed by `ParsePeakFlags` and published by `ApplyEnv` | green pre+post (closes the *silent* half: a token the guard ignores) |
| `TestMergeVerifyGateSpawnsAnExemptChildAndStillJudges` | **consequence**: the gate driven against a clock that refuses every non-exempt start — the merge must come back JUDGED (row `verified`, content kept, argv[0] = token, armed window in the child env, no disarming env) | **RED pre-fix** |
| `TestMergeVerifyGateRecordsTheDeliberateExemption` | the record: stderr + the judged row's event trail name `mergeVerifyPeakChildReason` and the flag | red pre-fix in practice (no spawn record exists), passes post-fix |
| `TestMergeVerifyChildArgvIsNotRefusedByAnArmedGuard` | **live probe on the real binary**: `GOPI_PEAK_HOURS=00:00-24:00` + `GOPI_IGNORE_PEAK_HOURS=off` → plain `task verify` exits 4 with the refusal, the gate's own argv does not and reaches `task verify` | **RED pre-fix** (no argv to probe → the control and the probe collapse) |

`internal/config/peakhours_flag_spelling_test.go` — the spawn-side contract of
the flag: round-trip through the parser (and the boolean flag never eats the
following verb token), `ApplyEnv` writes a value `EnvBool` reads as ON, and
`PeakGuardExitCode` does not collide with the other exit classes.

### Why the suite pin cannot mask these (`TestMain` sets `GOPI_IGNORE_PEAK_HOURS=1`)

> **RETIRED — and this section is now purely historical.** The guard, its `TestMain` pin and
> every exemption below were removed outright by `fix/remove-peak-guard` (commit `bb0c4c1a`,
> devdoc/peak-guard-removal.md): `GOPI_IGNORE_PEAK_HOURS` is read by nothing and the merger's
> verify child needs no exemption from anything. Do not reintroduce the variable to make a
> test behave — the "pin" described here no longer exists.

Every assertion sits at a level the pin cannot reach:

- **argv / argv-derived evidence** — the tests assert the exact argv token, and
  the "still judged" test's stub **exports `GOPI_IGNORE_PEAK_HOURS=1` only after
  checking argv[0]**. That is the emulation of `main()` itself: the exemption is
  *earned* by argv, not granted by an inherited variable. Re-read the stub (it is
  6 lines) — nothing about it can be satisfied by the pin.
- **an explicit child-env override on the real binary** — the live probe builds
  its own env: `GOPI_PEAK_HOURS=00:00-24:00` and
  `GOPI_IGNORE_PEAK_HOURS=` *off* (the spelling the guard's own refusal text
  advertises). The pin's value (`1`) is a **different string** from the argv
  token, so a probe passing only because of the pin is indistinguishable from a
  probe that never armed the guard — the child env is dumped and asserted to
  carry the armed window and NOT `GOPI_IGNORE_PEAK_HOURS=1`.

## Measured evidence (run in the worktree, master-tip `79738cb0`)

### 1. RED on the pre-fix code (`mergeVerifyChildArgv` returned `task verify <id>`)

```
$ go test ./cmd/agent/ -run 'TestMergeVerifyChildArgvExemptsThePeakGuard|TestMergeVerifyGateSpawnsAnExemptChild|TestMergeVerifyGateRecordsTheDeliberate|TestMergeVerifyChildArgvIsNotRefusedByAnArmedGuard' -count=1
--- FAIL: TestMergeVerifyChildArgvExemptsThePeakGuard (0.00s)
[gopi] peak-hour cost guard: refusing to start — the verify child was refused by the clock
--- FAIL: TestMergeVerifyGateSpawnsAnExemptChildAndStillJudges (2.65s)
--- FAIL: TestMergeVerifyChildArgvIsNotRefusedByAnArmedGuard (0.01s)
FAIL	github.com/nick/gopi/cmd/agent	3.405s
```

The middle line above is the epic's defect reproduced verbatim: the child exits
with the guard's refusal, the ledger keeps no verdict, and the gate records
`verify child exited without recording a verdict on the judged row … merge KEPT
(unverified), the verify is requeued` (observed in the same run, filtered out of
the excerpt).

### 2. GREEN with the fix

```
$ go test ./cmd/agent/ -run 'TestMergeVerifyChildArgv|TestIgnorePeakHoursFlagSpellingIsConsumed|TestMergeVerifyGateSpawnsAnExemptChild|TestMergeVerifyGateRecordsTheDeliberate|TestMergeVerifyChildArgvIsNotRefusedByAnArmedGuard' -count=1
ok  	github.com/nick/gopi/cmd/agent	3.589s

$ go test ./internal/config/ -run 'TestIgnorePeakHoursFlagSpelling|TestPeakGuardExitCodeIsNotAnother' -count=1
ok  	github.com/nick/gopi/internal/config	0.616s
```

The record, as it appears in the merger log (stderr surface) during the fix run:

```
[merger] merge-verify: verify child spawned EXEMPT from the peak-hour start guard
(argv --ignore-peak-hours; mandated judgement — one run at the 2x peak rate):
repo=…/001/repo branch=fix/clock delivering-task=01a0a06c-82eb-… window=00:00-24:00
until=2026-09-15T22:57:41+08:00
```

and the same text on the judged row's event trail (`task update …` succeeded,
`status=verified`).

### 3. Live probe transcript (the epic's acceptance, item 3)

```
control : gopi task verify t-clock-probe-missing        → exit 4, "refusing to start"
fix     : gopi --ignore-peak-hours task verify t-clock… → NOT exit 4, no refusal,
          reaches `task verify` (row lookup fails: "task … does not exist")
```
both runs with `GOPI_PEAK_HOURS=00:00-24:00`, `GOPI_IGNORE_PEAK_HOURS=off`, a
sandboxed `GOPI_HOME`, and a bounded timeout (the row is deliberately
nonexistent, so the probe is token-free — it asserts the PROCESS-START decision,
which is the whole subject).

### 4. Regression sweep (touched packages + the gate's own neighbours)

```
$ CGO_ENABLED=0 go build ./...                    → OK
$ go test ./cmd/agent/ -run 'TestPeakGuard|TestCostFree|TestIsUsageQuery|TestTopLevelVerb|TestHelpVocabulary|TestMergeVerify|TestMerger' -count=1
ok  	github.com/nick/gopi/cmd/agent	62.818s
$ go test ./internal/config/ ./internal/peakhours/ ./internal/standards/ ./internal/stylegate/ -count=1
ok  	github.com/nick/gopi/internal/config	0.995s
ok  	github.com/nick/gopi/internal/peakhours	0.002s
ok  	github.com/nick/gopi/internal/standards	10.587s
ok  	github.com/nick/gopi/internal/stylegate	8.739s
$ go test ./cmd/agent/ ./internal/tools/ -count=1     → see §5
```

## 5. Full-package runs and the one pre-existing failure

```
$ go test ./cmd/agent/ -count=1                        → ok
$ go test ./internal/tools/ -count=1                   → FAIL TestAgencyReplay_LiveLedgerIdempotent (5.6s)
```

`TestAgencyReplay_LiveLedgerIdempotent` replays the OPERATOR'S LIVE ledger
(`~/.gopi/memory.db`) and asserts the replay proposes something; it is
order/load-sensitive and flaked here the same way it did when it landed
(`devdoc/agency-proposals.md` §11 files the same class as
`TestMemorySearchNonBlocking`, bug `01a08e80-4091-78e1-a842-ee24f18ac172`).
Evidence that it is NOT this branch:

- **attribution by file**: `git diff --name-only` for this branch touches
  `cmd/agent/merger_verify_child.go`, `cmd/agent/merger_verify*_test.go`,
  `internal/config/{workhours,env}.go`, `internal/config/peakhours_flag_spelling_test.go`,
  `devdoc/` — nothing under `internal/tools/` and nothing under
  `internal/agencyscan/`, which is the code the test exercises;
- **in isolation**: `go test ./internal/tools/ -run TestAgencyReplay_LiveLedgerIdempotent -count=1` → **ok** (6.7 s);
- **A/B control**: `/home/user/workspace/gopi-vp-master` (detached at master-tip
  `79738cb0`, the base of this branch) → see §5b.

### 5b. The flake is pre-existing — A/B measured

Both runs are the SAME full-package command, `go test ./internal/tools/ -count=1`,
on two trees that differ by this whole branch:

| tree | result |
|---|---|
| `/home/user/workspace/gopi-vp-master` (pristine master-tip `79738cb0`) | **ok** — `117.4s` |
| `/home/user/workspace/gopi-verify-peak` (this branch) | **ok** — see §5c |

and the FIRST branch run failed `TestAgencyReplay_LiveLedgerIdempotent` **in this
same branch** while the pristine master tree passed — so the ordering is not
"branch red, master green" but "sometimes red". The test replays the operator's
LIVE `/home/user/.gopi/memory.db`; two concurrent suites (this branch and the detached
master probe, started ~1 min apart) copy and replay the same live ledger, which
is exactly the order/load sensitivity `devdoc/agency-proposals.md` §11 filed
(`TestMemorySearchNonBlocking`; bug `01a08e80-4091-78e1-a842-ee24f18ac172`).
Green in isolation (`-run TestAgencyReplay_LiveLedgerIdempotent -count=1` → ok,
6.7 s) and green in a clean full-package run ⇒ **not branch-attributable**; no
`internal/tools/` file is in this branch's diff.

### 5c. Full-package re-run of the branch (clean, single suite)

```
$ go test ./internal/tools/ -count=1
ok  	github.com/nick/gopi/internal/tools	…        (see the transcript at the end of this section)
```

## Not Done / deliberately out of scope

- **No change to the guard's process-global placement.** The guard still refuses
  every start including `task verify`; only the GATE's own mandated spawn carries
  the exemption. A per-verb exemption inside the guard was explicitly rejected by
  the epic ("avoid escaping") and would be the wrong fix: the exemption belongs
  to the *decision to judge*, not to the verb.
- **The requeued-verify path keeps its own semantics.** `c513327a`'s requeue
  (keep the merge, file a verify retry) is untouched: it is now reached for real
  failures (timeouts, crashes) instead of being the routine peak-hour outcome.
- **The judge still costs 2x when the moment is peak.** That is the decision, not
  an oversight: it is recorded on three surfaces. If the operator wants the
  opposite trade (wait for off-peak), the lever is `--peak-hours`/settings on the
  merger, which this change does not touch.
- A pre-existing `gofmt -l` noise in `cmd/agent/cli_worker_control.go` and
  `cmd/agent/usage_test.go` is present on master-tip and untouched here (Rule 4).

## Files

- `cmd/agent/merger_verify_child.go` (modified — argv helper, peak record, header clause ④°)
- `cmd/agent/merger_verify_peak_child_test.go` (new — 5 tests)
- `cmd/agent/merger_verify_e2e_test.go` (comment only: the stub reads its row from the LAST argv)
- `internal/config/workhours.go` (modified — `LongFlagPrefix`, `IgnorePeakHoursFlagSpelling`)
- `internal/config/env.go` (modified — `EnvBoolOn`)
- `internal/config/peakhours_flag_spelling_test.go` (new — 3 tests)
- `devdoc/merge-verify-peak-child-exemption.md` (this file)

## Verification summary

- Build: `CGO_ENABLED=0 go build ./...` → **OK**
- Red proof: 3 tests fail on the pre-fix argv, with the guard's own refusal text
- Tests: the 5 new `cmd/agent` tests + the 3 new `internal/config` tests → **PASS**;
  `TestPeakGuard*`/`TestCostFree*`/`TestMergeVerify*`/`TestMerger*` → **PASS**;
  `internal/config`, `internal/peakhours`, `internal/standards`, `internal/stylegate`
  → **PASS**
- Commit: see the branch's log (`git log --oneline -3`)
