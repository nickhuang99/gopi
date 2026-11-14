<!-- doc-class: historical -->
# Task Report: the resident merger loop's stop/restart control surface

> **CORRECTION (2026-09-19, epic `merger-delete-dead-impl`).** This is a record of
> a landed decision that was itself overtaken within a day: the control surface it
> built — `--stop`, `--restart` and the loop they drove — was deleted with the
> whole resident merger (`f9ff1a89` closed the entry points, `55c4d6f3` deleted
> the code). Read the argument below as the DESIGN record the re-development
> starts from, not as current truth: `cmd/agent/merger_restart.go` and the
> `merger_loop.go` it drives do not exist, `gopi merger --stop` prints the
> retirement sentence and exits 1, and the one-tick merge is the bare
> `gopi merge`. The claims in this report were TRUE when it landed (that is what
> it measured); the classification table that records the whole arc, with the
> same dated correction, is `devdoc/merger-self-driving.md`, and the retirement
> record is `devdoc/merger-retired.md`.

**Branch:** `fix/merger-loop-restart` (worktree `~/workspace/gopi-merger-restart`)
**Date:** 2026-09-19
**Trigger:** operator report — `gopi merger --every 5m` is a self-driving loop, but
its control surface went out with the daemon machinery: `--restart` and `--stop`
are refused as retired flags, so reloading a stale running binary meant hunting a
pid and killing it.

## Completed

### 1. `--stop` and `--restart` are a real control face (`cmd/agent/merger_restart.go`)

A new file owns the whole face, with the class decision argued per flag in its
header comment:

| flag | class | why |
|---|---|---|
| `--every D` | sleep parameter | repeats the SAME process's tick; detaches nothing |
| `--stop` | **restored** | ends the live loop by the signal it already documents |
| `--restart` | **restored** | stops the live loop, then starts a fresh one |
| `--daemon` `--start` `--wait` `--force` | **still retired** | they create or act on a process nobody can see |

The argument for restoring control while keeping the detach class retired, as
written in the file: the loop is **not hidden** (it runs in the operator's own
foreground, holds the flock, and `--status` reports its pid, interval and next
tick), and the mechanism is **the loop's own, already documented** — `merger_loop.go`:
"the signals stay caught for the loop's whole life and are answered at the tick
boundary". `--stop` is a NAME for the gesture Ctrl-C already performs; it invents
no second stopping path, writes no request file, and leaves the loop an empty
sleeper. A bare `--stop` therefore means only "end the loop"; `--stop --every D`
and `--restart --every D` spell the restart for an operator holding a shell.

**Validation happens before the stop.** `--restart --every 5` is refused for the
period while the old loop is still running — a typo must never leave the operator
with no merger at all.

### 2. The no-two-loops boundary is not re-implemented — it is the flock

The fresh loop calls `runTaskMerger → acquireServeLock → the SAME flock`. The
bounded wait before the spawn is the *ordering* the operator asked for, and the
flock is what makes a race harmless: a generation that does not leave in time gets
the fresh loop **refused** with a non-zero exit, never run beside it.

### 3. Flag lists became one decision (Rules 1b/13/14)

- `mergerRetiredFlags` is now the **detach** class only
  (`--daemon --start --wait --force`).
- `mergerControlFlags` (`--stop`, `--restart`) is its disjoint peer, and
  `mergerControlFlagSummary()` renders the usage page from it — so the page and
  the routing lists cannot drift.
- The spellings themselves live in named constants (`mergerStopFlag`,
  `mergerRestartFlag`, `mergerOnceFlag`, `mergerDBFlag`, `mergerNoBranchScanFlag`).
- Both merger faces route on ONE predicate, `(*tasksOptions).mergerControlRequested()`.

### 4. The retired-flag hint now names the successor

`mergerRetiredFlagHint` used to say the merger "has no resident service to act on"
— no longer true. It now says the flags DETACH a merger nobody can see, and names
the spellings that work (`--stop` / `--restart`).

### 5. `--status` no longer reads its log line as a fault (`internal/tools/merger_status.go`)

The log-destination line is now a measured DESTINATION with a redirect note, never
a "mismatch ... do not resolve" fault, while staying truthful about the two paths.
Gated by `TestMergerLogLineNeverReadsAsAFault`, which bans the fault wordings.

## What the measurements changed (design decisions that reversed)

Three claims in my own first draft were **falsified by measurement** and the code
was changed rather than the claim kept:

1. **"The fresh loop must come back in the operator's directory."** False. The
   merger's cwd is not an input: repos come from the settings table
   (`config.ResolveMergerRepos`), the profile from `GOPI_HOME`. Measured: `merger
   --once` from `/` and from a repo directory both report "no configured repos in
   settings repos table". Worse, *restoring* the old directory re-creates the
   un-recyclable-worktree defect `daemoncwd` exists to fix (a loop's life is
   unbounded, so it would veto its worktree's recycle forever). The fresh loop
   therefore gets the shared measured process root, with the escape announced by
   `daemoncwd.Report` — the same call the take-over makes.
2. **"The restart command becomes the fresh loop."** False, and a worse shape: the
   successor's pid is the fact worth reporting, and a process cannot announce its
   own pid before it exists. The face now **spawns and returns**, then prints the
   pid the LOCK confirms.
3. **"The loop can inherit the operator's streams."** A defect — measured by a
   hung test case. A resident loop holding the control command's stdout keeps that
   pipe open, so the ordinary spellings never return:

   ```
   out=$(gopi merger --restart)      # hangs forever
   gopi merger --restart | tee log   # ditto
   ```

   The loop now starts with its streams on `/dev/null`; its output is not lost —
   the host lifecycle record goes to `<gopi dir>/logs/merger.log`, the very path
   `--status` prints as "where the run's output goes". Not detached: no setsid, no
   cgroup move (Rule 24).

## Not Done (honest)

- **A refused fresh loop is not exercised by an autonomous test.** The refusal
  branch (predecessor still holds the flock after the start budget) was written and
  is reachable, but inducing a loop that ignores SIGTERM across a tick boundary
  needs a deliberately stubborn fixture; the branch is verified by reading, not by
  a test. This is the one acceptance clause of the epic without a mechanical
  witness.
- **The takeover-in-flight case (C9) is timing-dependent by nature.** It asserts
  the stub is alive after the stop+replace; the stub itself is only semi-long-lived
  (it holds while its hold-fd is open). Observed PASS, but the strength of the
  assertion depends on the stub still being inside its hold window when the restart
  completes — a stub that had already exited would make the case vacuous rather
  than failing. It is a real measurement of "the face reaches nothing else", not a
  proof that no future implementation could.
- **`--stop` was measured only for a foreground child of the test.** Under a
  `setsid nohup` launcher the signal path is identical (the loop's own
  `signal.Notify`), but that shape was not driven.
- **The cross-device relative-exec path was simplified away** once the cwd decision
  was corrected (the successor no longer needs a relative exec spelling), so no
  case covers it — it no longer exists.
- **`--stop` under a `setsid nohup` launcher**: the signal path is identical (the
  loop's own `signal.Notify`), but the case was measured only for a foreground
  child of the test.
- **Cross-device (`/proc/<pid>/cwd` unavailable) relative-exec**: the code path was
  simplified away (the successor no longer needs a relative exec spelling once the
  cwd decision was corrected), so no case covers it.

## Verification

- Build: `go build ./...` → OK; `go build -o bin/gopi ./cmd/agent` → OK
- Vet: `go vet ./cmd/agent/ ./internal/tools/` → clean
- gofmt: clean on every touched file
- Tests (`cmd/agent`, real binary, private profile) — **11 PASS in 10.8s, 0 leaked loops**:
  - `TestMergerControlFlagListsAreOneDecision` — PASS (disjoint lists, page names
    both, both faces honour both control flags)
  - `TestMergerStopEndsALiveLoop` — PASS
  - `TestMergerStopWithNoLoopIsTheEndState` — PASS
  - `TestMergerRestartReplacesTheLoopWithoutOverlap` — PASS (holder pid changes;
    max live holders in any one sample = 1)
  - `TestMergerRestartWithNoLoopIsAPlainStart` — PASS
  - `TestMergerRestartRefusesABadPeriodBeforeStopping` — PASS (no signal sent, the
    same pid still holds the lock)
  - `TestMergerRestartDoesNotTouchTheTakeoverInFlight` — PASS
  - `TestMergerRestartArgvDropsTheControlFlags` — PASS
  - `TestMergerRetiredFlagsHintNamesTheControlFace` — PASS
  - `TestMergerRetiredFlagsAreRefusedNotIgnored` / `...AreDocumented` — PASS
  - broader regression `go test -run 'Merger|MergerLog|MergerStatus' ./cmd/agent/`
    — ok, 100.4s
  - `go test -run 'TestMergerStatus|TestMergerLog' ./internal/tools/` — ok
- **Leak check** (the lesson this rig paid for): after the suite, zero
  `merger-restart-bin/gopi merger` processes remain. Two separate fixture bugs had
  to be fixed to reach that — an unreaped-child liveness check that read a ZOMBIE
  as alive, and a cleanup that relied on the control face alone while the
  successor was mid-first-tick (measured 32s; the face's own stop is 0.3s when the
  loop is sleeping). Cleanup is now the honest `--stop` PLUS a bounded SIGKILL
  backstop whose pid comes from the LOCK.
- **Live measurement** (real binary, private profile, evidence parked off the
  worktree):

  ```
  $ gopi merger --every 5m &               # first generation
  $ gopi merger --status | grep holder
  lock holder: pid 3936407
  $ gopi merger --restart --every 5m
  [merger] gopi merger: stop requested — pid 3936407, terminated
  [merger] gopi merger: stopped — pid 3936407 is gone and .../serve.lock is free
  [merger] gopi merger: fresh loop: pid 3938991, .../bin/gopi, dev
  $ ps: 3938991: bin/gopi merger --every 5m      # ← control flag DROPPED from argv
  ```

  Exactly one loop alive at every observation; the fresh argv carries no control
  flag (the pitfall that turns a restart into "stopped, then failed to start").

## Files

- `cmd/agent/merger_restart.go` (new) — the control face
- `cmd/agent/merger_restart_e2e_test.go` (new) — C1–C9 on the real binary
- `cmd/agent/main.go` — control/retired lists, routing, hint, usage page
- `cmd/agent/cli_tasks.go` — `--stop`/`--restart` parsing + alias routing
- `internal/tools/merger_status.go` — log-destination wording
- `internal/tools/merger_status_log_test.go` — fault-wording gate

## Verification recipes worth keeping (→ memory)

- **`kill(pid, 0)` succeeds on a ZOMBIE.** An unreaped child that already released
  the flock reads as "still alive". Read `/proc/<pid>/stat` and treat state `Z` as
  stopped (the state field is after the LAST `)`).
- **A spawned resident child must not inherit the caller's streams**, or
  `out=$(cmd)` hangs forever.
