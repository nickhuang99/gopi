<!-- doc-class: historical -->
> **CORRECTION (2026-09-19, epic `merger-delete-dead-impl`).** Record, not current
> truth: the loop and its `--stop` face were deleted with the resident merger
> (`f9ff1a89`, `55c4d6f3`); every spelling of `gopi merger` now refuses and the
> one-tick merge is the bare `gopi merge`. See `devdoc/merger-retired.md` and the
> dated correction in `devdoc/merger-self-driving.md`.

# Task Report: the resident merger loop could not hear the stop the lock told it to expect

Branch: `fix/merger-loop-exit-bounded` (worktree `~/workspace/gopi-merger-loop-exit`, base `9ce8e159`)
Date: 2026-09-18
Epic: `gopi merger --stop` reports "still running after 1m0s" and exits 1 on a
busy host; `cmd/agent`'s `TestMergerStopEndsALiveLoop` (C2) and
`TestMergerRestartReplacesTheLoopWithoutOverlap` (C4) fail with it.

## Completed

### 1. Root cause, named with a measurement — a signal-delivery window, not a slow loop

The loop was never slow. It was **deaf for the first ~0.1s of its life**, and
`--stop` aims its signal exactly at that interval.

The 5 whys:

1. **Why does `--stop` fail?** It signals the pid stamped in
   `<gopi dir>/serve.lock` and waits `mergerStopDeliverTimeout` (60s) for the
   flock to go free (`mergerStopLiveLoop` → `mergerWaitLockFree` → `tools.LockProbe`).
2. **Why is the loop still alive after 60s?** Its `fatal` condition — "the loop
   has not reached a tick boundary yet" — is false: the loop WAS reaching tick
   boundaries the whole time (measured: sleep edges at `16:21:18` and
   `16:21:48`, exactly its 30s interval).
3. **Why does it keep ticking if it was signalled?** Because it never learned it
   was signalled: `/proc/<pid>/status` shows `State: S`, **`SigPnd: 0`** (the
   signal was consumed, not pending), and the log never prints the stop line —
   the loop's `mergerSleepOrStop` never saw anything on its channel.
4. **Why did the signal not reach the loop's channel?** `internal/signalrec`'s
   recorder (`Install("")`, `cmd/agent/main.go:799`) is armed for **SIGTERM
   before the subcommand chain runs**, and the loop's own `signal.Notify` was
   installed **after** `acquireServeLock()` — so in the window between the flock
   being won (which publishes the pid) and the loop arming itself, Go delivers
   SIGTERM to the recorder's channel.
5. **Why did that consume the stop instead of leaving it to the loop?** Because
   the recorder deliberately **does not re-raise for the merger**:
   `modesThatOwnTheirSignalHandlers["merger"] = true` (`internal/signalrec`), the
   policy whose entire purpose is "leave the signal to the mode that owns its own
   handler". The handler that was supposed to receive it did not exist yet, so
   the signal was dropped on the floor.

**Measured duration of the window (the blocking interval):** `T_armed − T_stamp`
= **0.117s / 0.095s / 0.107s** across 3 trials
(`scripts/merger-loop-signal-window.sh`), where `T_stamp` is the moment the pid
appears in `serve.lock` (the instant `--stop` decides to signal) and `T_armed` is
the banner printed immediately after `signal.Notify`.

**Direct proof of the mechanism** — one loop, SIGTERM sent 22ms after the stamp,
i.e. inside the window:

```
stamp observed for pid=702305; SIGTERM sent 22ms later (inside the ~100ms window)
{"ts":"2026-09-18T16:28:05+08:00","kind":"signal","signal":"SIGTERM","pid":702305,
 "role":"merger","argv":".../bin/gopi merger --every 30s","sourced":false}
*** LOOP STILL ALIVE 80s AFTER SIGTERM ***
State:  S (sleeping)        SigPnd: 0000000000000000
[merger] sleeping 30s from 16:29:07      <- and ticking again 30s later
```

The provenance record is the smoking gun twice over: it proves the recorder
**consumed** the signal, and `"sourced":false` proves nothing announced it as a
stop path — while the loop it was aimed at was still running and never heard it.

### 2. The fix — arm the stop handler before the lock can advertise the pid

`cmd/agent/merger_loop.go`:
- new `mergerArmStopSignal() (chan os.Signal, func())` — the one place the stop
  channel is created and `signal.Notify(stop, mergerStopSignals()...)` is called,
  with the ordering requirement documented at length (why the stamp makes the pid
  public and why the recorder will not re-raise for the merger);
- new constant `mergerStopChanCap = 1` (Rule 1: the buffer is the contract
  "a stop that arrives during a tick is remembered", not a literal);
- `mergerResidentLoop(opts, lock, interval, stop <-chan os.Signal)` now **takes**
  the channel instead of installing it, so the install cannot drift back into the
  window.

`cmd/agent/merger_tick.go` (`runTaskMerger`):
- the signals are armed **before** `acquireServeLock()` (only for the resident
  form, `interval > 0`), and released by `defer` — so the pid is never public
  before the loop can hear a signal aimed at it.

Everything else about the loop is untouched: it still answers at a tick boundary,
still "empties" (no watchdog, no registry, no state file), and `--stop` still
observes the signalled pid's exit through the flock.

### 3. Rig (checked in, no load generated — Rule 27)

| script | what it measures |
|---|---|
| `scripts/merger-loop-exit-rig.sh` | one loop in a private profile; SIGTERM → wall time to answer; on no answer, captures `/proc` state + a SIGQUIT goroutine dump |
| `scripts/merger-loop-signal-window.sh` | **the measured window** `T_armed − T_stamp`, and whether a signal sent inside it is answered |
| `scripts/merger-loop-hang-capture.sh` | drives the real failing case and captures the hung loop's state by an exact-`/proc/<pid>/exe` + test-parent match |
| `scripts/merger-loop-hang-sample.sh` | 100ms-resolution sampler: loop liveness/state, log staleness, lock stamp, `flock -n`, and **every fd holder of the lock inode** |
| `scripts/merger-loop-sigquit-dump.sh` | SIGQUIT goroutine dump of a live loop, for naming the blocking call |

Every script identifies its subject by its own profile's lock file or its exact
binary path — never `pgrep -f "gopi merger --every"`, which on this host matches
**three stale loops from earlier tasks** (Rule 24's trap, observed live: pid
1113464, 11192s old).

### 4. Regression test

**On the branch this was written on** (pre-retirement master) the guard was
behavioural: `TestMergerStopIsAnsweredTheMomentTheLoopPublishesItsPid` (C2b,
`cmd/agent/merger_restart_e2e_test.go`): it waits for the loop's pid to be
**stamped into the lock**, signals exactly then (and at +5ms, +20ms — inside the
measured window), and requires both halves of an answer: the process dies inside
`mreArmWindowAnswerWait` **and** printed its stop line (so a kill cannot pass for
an answered stop). Each delay runs in its OWN profile (two real loops never share
a GOPI_HOME).

**On post-retirement master that guard is superseded**, because both merger entry
points now call `retireMergerFace` before any dispatch: spawning
`gopi merger --every D` exits 1 and never takes the lock, so the fixture cannot
run for ANY implementation and re-deriving it as-is would only produce a case
that can never pass (or a silent `t.Skip`). The invariant is therefore re-derived
SOURCE-LEVEL as `cmd/agent/merger_stop_arm_order_gate_test.go`, and the ordering
is the only level at which it is still observable:

1. `TestMergerStopSignalIsArmedBeforeTheLoopIsSignallable` — in `runTaskMerger`'s
   body `mergerArmStopSignal()` must precede `acquireServeLock()`, the arm must be
   gated on the resident form (`if interval > 0 {`), and the release deferred.
2. `TestTheStopInstallLivesInExactlyOnePlace` — `signal.Notify(` belongs to
   `mergerArmStopSignal` alone; `mergerResidentLoop` must only read the
   `<-chan os.Signal` it was handed.

Red-proof by REMOVAL, observed 2026-09-19 in the rework worktree: restore the
pre-fix order (move the arm block below `acquireServeLock()`) and the gate fails
naming both body offsets (`"mergerArmStopSignal()" at 1255`, `"acquireServeLock()"
at 987`); put it back and it passes. The behavioural case is to be restored
TOGETHER with the entry point (it needs the `mre*` fixture helpers, which went
with `merger_restart_e2e_test.go`).

## Not Done (honest)

- **The window's second half.** The recorder's `modesThatOwnTheirSignalHandlers`
  policy (no re-raise for the merger) is what turns a *consumed* signal into a
  *lost* one. This fix removes the window the merger was vulnerable in, so no
  re-raise is needed and the deliberate "the merger finishes its in-flight tick"
  semantics stay intact. But the same shape is still reachable in principle by
  any future mode that (a) is listed as owning its handlers and (b) does not arm
  them before it becomes signallable — enumerated here because the policy, not
  this loop, is where the next instance would live. Not repaired in this task.
- **C4 (`TestMergerRestartReplacesTheLoopWithoutOverlap`)** was red for the same
  root cause through the same stop leg; the fix addresses that leg, but this task
  measured C4 only as part of the paired run (see Verification). No C4-specific
  arm was added.

## Verification

**Provenance (2026-09-18, pre-retirement master — the branch's original run).**
These numbers were observed on the branch before the retirement; they are kept
as the record of the root cause, not as a claim about post-retirement master.

- Build: `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → OK; `go vet ./cmd/agent/` → clean.
- Regression red before the fix (fix stashed, test kept):
  `go test ./cmd/agent/ -run TestMergerStopIsAnsweredTheMomentTheLoopPublishesItsPid -count=1`
  → **FAIL (135.13s)**, all three sub-cases (`/0s`, `/5ms`, `/20ms`) at 45.04s
  each, with the loop still ticking (`[merger] sleeping 30s from 16:36:05`).
- Regression green after the fix:
  `go test ./cmd/agent/ -run TestMergerStopIsAnsweredTheMomentTheLoopPublishesItsPid -count=1 -v`
  → **PASS (18.06s)**, all three sub-cases, `ok github.com/nick/gopi/cmd/agent 20.880s`.
- Paired acceptance, `-count=2` on a host at load 8.58:
  `go test ./cmd/agent/ -run 'TestMergerStopEndsALiveLoop|TestMergerRestartReplacesTheLoopWithoutOverlap|TestMergerStopIsAnsweredTheMomentTheLoopPublishesItsPid' -count=2`
  → `PASS` ×2 for every case: C2 `(4.63s)` and `(3.62s)`, C2b `(6.56s)` and
  `(5.93s)`, C4 `(4.07s)` and `(3.67s)`; `ok github.com/nick/gopi/cmd/agent 30.099s`.
  The same three C2 runs were `FAIL (60.21s)/(60.25s)/(60.22s)` before the fix.
- Branch-scoped smoke: see Evidence.

## Evidence (self-filed)

- **`merger --once` tick cost** (the blocking call was NOT the tick):
  `for i in 1..5: /usr/bin/time env -i … ./bin/gopi merger --once`
  → `tick1 wall=1.05s`, `tick2 wall=0.70s`, `tick3 wall=0.57s`, `tick4 wall=0.62s`,
  `tick5 wall=0.72s` (empty repos table).
- **Host was genuinely busy** (Rule 27 observation, never generated):
  `cat /proc/loadavg` → `5.24 6.32 7.23` and later `7.16 12.72 12.43`, 4 cores.
- **Reproduction of the epic's failure on this host**:
  `GOPI_TEST_BIN=$PWD/bin/gopi go test ./cmd/agent/ -run TestMergerStopEndsALiveLoop -count=3 -v`
  → `--- FAIL: TestMergerStopEndsALiveLoop (60.21s)`, `(60.25s)`, `(60.22s)`, each
  printing `pid 4154599 still holds …/001/.gopi/serve.lock after 1m0s — the loop
  has not reached a tick boundary yet`.
- **The loop is healthy while the stop fails** (`scripts/merger-loop-hang-sample.sh`,
  `sample.tsv`): the loop is alive at every sample, its log gains a NEW sleep edge
  one interval later, and the lock is held by the loop's own fd 3:
  ```
  0.0  alive S 1789719676 [gopi] [lifecycle] …resident task-lifecycle sweep…  no  358633 no 358633:3(gopi)
  8.1  alive S 1789719678 [merger] sleeping 30s from 16:21:18                 yes 358633 no 358633:3(gopi)
  33.1 alive S 1789719708 [merger] sleeping 30s from 16:21:48                 yes 358633 no 358633:3(gopi)
  55.6 alive S 1789719708 [merger] sleeping 30s from 16:21:48                 yes 358633 no -
  ```
- **Window measurement** (`scripts/merger-loop-signal-window.sh --trials 3`):
  `T_armed - T_stamp = 0.117s`, `0.095s`, `0.107s`.
- **Mechanism proof, pre-fix** (probe, SIGTERM 22ms after the stamp): provenance
  record `{"kind":"signal","signal":"SIGTERM",…,"role":"merger",…,"sourced":false}`
  written, `*** LOOP STILL ALIVE 80s AFTER SIGTERM ***`, `State: S`, `SigPnd: 0`.
- **Mechanism proof, post-fix** (same probe): `LOOP EXITED after ~4.12s`.
- **A/B on the recorder** (names the mechanism, one variable):
  `GOPI_SIGNAL_RECORD_OFF=1 … go test -run TestMergerStopEndsALiveLoop -count=3`
  → `PASS` ×3 in `0.37s/0.41s/0.42s` versus `FAIL` ×3 in ~60.2s with the recorder on.

## Close-out addendum (2026-09-18): what the full-package smoke did and did not show

The full branch-scoped run is **not green**: `go test ./cmd/agent/ -count=1
-timeout 25m` from the closing clone → `SMOKE_EXIT=1`, 1270 tests seen, **9
failing**. Each was attributed against a **master baseline** run over the same
nine names in a detached worktree of `9ce8e159`:

| test | master | branch |
|---|---|---|
| `TestMergerRestartDoesNotTouchTheTakeoverInFlight` | FAIL | FAIL |
| `TestServiceHelpIsCapabilityFirst` | FAIL | FAIL |
| `TestSpawnersPinTheRepoContext` | FAIL | FAIL |
| `TestTopLevelUsageDelegatesEnvVarsToHelpEnv` | FAIL | FAIL |
| `TestTopLevelUsageShowsTaskLedger` | FAIL | FAIL |
| `TestAbnormalChildRetryIsBoundedAndHandsOverOnce` | pass | FAIL |
| `TestJudgedChildFailureIsNotRetried` | pass | FAIL |
| `TestRetryCounterSurvivesARestart` | pass | FAIL |
| `TestRetryRefusesWhenNoRunStarted` | pass | FAIL |

Five are **pre-existing master red**. The other four live in
`cli_tasks_child_retry_test.go` — a file this diff does not touch — and pass under
isolation on BOTH revisions (`-count=3`: master `ok 53.651s`, branch
`ok 34.055s`), i.e. a full-suite order/load flake. In the same run the three tests
this work is measured by all passed: `TestMergerStopEndsALiveLoop`,
`TestMergerStopIsAnsweredTheMomentTheLoopPublishesItsPid` (all three sub-cases)
and `TestMergerRestartReplacesTheLoopWithoutOverlap`.

Two things the addendum must not hide:

1. **The reap.** The assigned worktree `/home/user/workspace/gopi-merger-loop-exit` was
   deleted **twice** mid-run, and each time the package suite failed ~174 tests
   whose per-test output named the deleted cwd (`fork/exec
   …/bin/gopi: no such file or directory` ×149; source-tree scans that "went
   blind" ×24 — e.g. `AST sweep found only 0 help constants`, `no file carries the
   gopi binary cache`). Attributing this by the pass/fail tally alone would have
   read as a catastrophic regression; the `-json` per-test OUTPUT is what settled
   it. The closing run was therefore made from a **clone** under the session
   directory (Rule 6a's exception; memory #1892/#3034).
2. **`TestMergerRestartDoesNotTouchTheTakeoverInFlight` is flaky, and stays
   filed.** Measured 3 ways across master and branch: variant A (`--restart`
   exits 1, `serve.lock` still held after 1m0s — plausibly the take-over's
   inherited fd-3 lock making the wait unmeetable) and variant B (the take-over is
   killed). An observation-only fd probe over one failing run captured only
   three long-lived **stale orphan loops** from earlier unrelated runs, so the
   holder of the lock was NOT observed; the mechanism is a code-reading
   hypothesis, filed as bug `01a0b402-5df4-7a8f-81d3-f3d2820f617a` (superseding
   `01a0b400-2270-7e9d-8220-dd02221c8d3a`, whose "deterministic" claim was
   wrong). An incomplete fix is repaired by dispatching again (Rule 26) — this
   addendum is what a successor needs to pick it up.
