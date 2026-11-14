<!-- doc-class: historical -->
# Task Report: the resident task-lifecycle tick (breaking the workerLifecyclePasses coupling)

> **WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md** — the operator owns this; an agent must never run it (submit the work instead: `gopi task submit`). The `gopi merger --daemon` and `gopi worker --daemon` probes below are started by TESTS in a hermetic sandbox (private `GOPI_HOME`, explicit pid stop) — never against a real host.

Epic item 3/5: *"Break the workerLifecyclePasses coupling so recovery works with NO
worker alive: extract the sweep (stale report, confirmed-zombie auto-disposal after
the grace, finish-cleanup) so it is driven by a resident tick independent of the
worker loop (e.g. merger-side tick or the daemon supervisor), and prove by test that
it runs with zero worker processes."*

## Completed

### The defect

All three lifecycle legs were reachable **only** from the worker loop:

```
cli_tasks_worker.go round ① → workerLifecyclePasses()
      ├─ serveStaleSweep()                    (stale report → notify sink)
      ├─ autoDisposeConfirmedZombies(grace)   (confirmed zombie → terminal)
      └─ serveFinishCleanupSweep()            (stranded close-out + dead-holder reclaim)
```

A worker is **self-terminating by design** — `workerDefaultIdleExits` (3) consecutive
drained rounds and it exits 0 ("a task-DRAIN tool, not a daemon", nick 9/1) — while
the component an operator actually leaves resident on a task host is the **merger**.
So on a host with no worker alive, and for *every* window in which the single worker
daemon is stopped / restarting / draining out, a confirmed zombie row (dead claim pid)
sat non-terminal indefinitely and a stranded close-out stayed stranded. The same
defect had already been fixed once for **one** leg only (the dead-holder reclaim,
2026-09-13, `mergeDeadHolderReclaimLeg` called from `mergerPassOnce`) — the stale
report and the zombie disposal never got the same treatment.

### The fix

1. **`cmd/agent/task_lifecycle_tick.go` (new)** — the sweep extracted as
   `runTaskLifecycleTick(role string)`, role-parameterized exactly like the
   pre-existing `scratchGCTick(role)` (same problem, same solution, zero new
   daemons):
   - ONE process-wide throttle clock (`taskLifecycleLastTick`,
     `taskLifecycleThrottleMin = 10min`), replacing `workerLifecycleLastTick` +
     `workerLifecycleThrottleMin`.
   - The three legs are unchanged pre-existing code
     (`serveStaleSweep` → `autoDisposeConfirmedZombies` →
     `serveFinishCleanupSweep`); only the composition, the role bracket and the
     summary line moved.
   - Fail-soft, as before: a malformed grace config, a dead notify sink or an
     unreadable ledger is reported loudly and never fails the tick (Rule 15).
   - The operator line is now `[<role>] [lifecycle] stale reported=%d disposed=%d
     sink=%s`, and a once-per-process banner records that a resident component
     took on task-lifetime duty (an operator must be able to grep a daemon log for
     the tick's contract).

2. **Two carriers, one owner per process** — `acquireTaskLifecycleOwnership()` is
   taken at the top of the worker loop and released by `defer`; while that mark is
   live, a tick fired by another role prints
   `[merger] [lifecycle] skip: this process's worker loop owns task-lifetime duty
   (owner was pid N at T)` and touches nothing. With no worker alive no mark is
   ever held, so the merger's tick owns the duty — which is the whole point. The
   `defer` covers the early-return *and* the panic path, so a dead worker cannot
   strand the sweep (a worker never hands the duty to a *different process*: the
   mark is in-process, and the durable facts are per-process claim stamps).

3. **The merger's tick carries it** (`runTaskMergerWithLock`, next to
   `scratchGCSweep(mergerRoleLabel)`, both on the non-`--once` face — `--once` is
   the unit/E2E seam whose stdout other tests pin). `autoDisposeConfirmedZombies`
   and `workerNotifySinkTarget` gained the role parameter so a worker-less host's
   log names the merger as the sweeper instead of claiming "worker".

4. **`workerLifecyclePasses` deleted** — no alias, no shim. A reader following the
   old name lands on the new file, which documents the defect. The retired
   `workerLifecycleLastTick` is *gone* rather than left as dead state, so the six
   test sites that armed it as a throttle now call the real thing
   (`throttleLifecycleTick()` / `zwResetLifecycleClocks()`).

### Tests (`cmd/agent/task_lifecycle_tick_test.go`, new)

| test | proves |
| --- | --- |
| `TestMergerTick_DisposesZombieWithNoWorkerAlive` | no worker loop exists — one merger tick disposes the SIGKILLed-mid-round row (dead claim pid), leaves `suspected` + live-pid rows untouched, writes the zombie audit reason, reports under `[merger]` and still delivers the stale event to the (sandboxed) sink — the *same* outcome `TestWorkerLifecyclePass_AutoDisposesConfirmedZombie` pins for the worker role |
| `TestMergerTick_YieldsToLiveWorkerLoop` | the other direction: while a worker loop owns the duty the merger tick skips, says why, and touches nothing; after that loop exits, the very next tick takes over (the restart/stop window) |
| `TestMergerTick_ReclaimsDeadHolderWorktreeWithNoWorkerAlive` | the finish-cleanup leg with no worker: the dead holder's clean linked worktree is freed and the **REAL** `scripts/safe-merge.sh` gate [c] flips refuse → exit 0, branch tip byte-identical |
| `TestMergerDaemon_DisposesZombieWithZeroWorkerProcesses` | **end to end**: a real `gopi merger --daemon` (private `GOPI_HOME`, empty `repos` table, `--every 1s`) is the host's only resident gopi process; the seeded zombie row goes terminal from its own tick, and the daemon's log shows the `[merger] [lifecycle] …` pass and the `[merger] [zombie-dispose] <id>` line |
| `TestKilledWorkerDaemon_MergerTickRecoversTheStalledRound` | **the epic's acceptance, with real processes**: a real `gopi worker --daemon` starts (the pre-existing resident sweeper) → its own log must show the round line → **every** worker of the sandbox is `SIGKILL`ed (no drain, no close-out) → the SIGKILLed round's row (dead claim pid, heartbeat hours old, no live process) is seeded → a real `gopi merger --daemon` is the sandbox's **only** resident process → the row goes terminal from the merger's tick, and a final `wdSandboxPIDs(home) == 0` asserts the recovery happened with **zero workers alive** |

The kill is scoped by the sandbox's private `GOPI_HOME` (`wdMarks`: the worker-daemon env marker **and** this home), never by process pattern — the operator runs live worker/merger daemons on this host.

### Red-proof (mutation)

Commenting out the single added line `runTaskLifecycleTick(mergerRoleLabel)` in
`cli_tasks.go` and re-running BOTH e2e cases:

```
--- FAIL: TestMergerDaemon_DisposesZombieWithZeroWorkerProcesses (48.35s)
    task_lifecycle_tick_test.go:291: a host with ZERO worker processes must still
    dispose the confirmed zombie: status = "in_progress", want "failed"
--- FAIL: TestKilledWorkerDaemon_MergerTickRecoversTheStalledRound (47.04s)
    task_lifecycle_tick_test.go:466: recovery STALLED after every worker was
    killed: status = "in_progress", want "failed"
```

Both rows stay `in_progress` for the whole budget — the tests fail for exactly the
defect being fixed, not for a timing accident. The line was restored immediately
after (`git diff` shows no mutation left).

### Three defects the first e2e runs exposed (all fixed, all load-bearing)

The zero-worker e2e did not pass on its first run. None of the three causes was
"unlucky timing" to be retried away: each was the test proving something it did
not mean to, or a fixture whose stability was not the fixture's to decide.

1. **The worker-daemon marker leaked into the merger daemon's environment.**
   The zero-worker property was asserted through `wdMarks`/`wdSandboxPIDs` — an
   ENVIRONMENT scan (workerDaemonEnv present AND this `GOPI_HOME`). But `go test`
   runs, on this host, inside a gopi worker session, so the TEST process itself
   carries `GOPI_WORKER_DAEMON_CHILD=1` + `GOPI_DAEMON_READY_FD=3`; `mdEnv`/`wdEnv`
   hand `os.Environ()` to every child, so the MERGER daemon inherited the marker
   and matched the scan — the run ended with
   `a worker daemon is alive at the end of the run ([3325253 3325288])`, two pids
   that were the merger daemon and its spawn helper, NOT workers. The assertion
   the epic turns on ("recovery happened with ZERO workers alive") was therefore
   unrunnable, and an operator-visible defect hid behind it.
   **What is NOT the mechanism** (checked, not assumed): the inherited marker does
   not derail the daemon itself. Started by hand with both handles inherited in a
   sandbox home, `gopi merger --daemon` runs its own loop and logs
   `[merger] gopi merger: resident loop started` /
   `[merger] [lifecycle] stale reported=0 disposed=0 …` — the `[gopi]` line the
   first failing run showed is the once-per-process BANNER (taskLifecycleBannerRole),
   not a worker-child role.
   Fix: `zwDropDaemonHandles` strips `workerDaemonEnv` + `daemonReadyEnv` from
   every child env this file builds, so a marker-based worker scan can only see
   real workers, and the zero-worker property is now ASSERTED
   (`wdSandboxPIDs(home) == 0`) at ④ BEFORE the recovery is allowed to count
   (the merger-daemon case asserts it too, immediately after the start).
2. **The seeding depended on the daemon's own schema initialization, which the
   kill can interrupt.** The ledger used to be created by the DAEMON's first open;
   one run failed at ③ with `… is not a distilled knowledge base (missing
   distilled_qa table)` (`task_lifecycle_tick_test.go:491`) — the memory layer's
   correct refusal of a file whose schema is not (yet) there, i.e. the kill had
   landed while the schema was being created. Being honest about the evidence:
   this was observed ONCE, and the same code passed on the immediate re-run, so it
   is a RACE, not a proven deterministic path — which is exactly why the fix is
   structural rather than a retry: the ledger is now seeded in ⓪ BEFORE any daemon
   starts (the healthy open that CREATES the schema) and the daemons only ever
   attach to it, so no daemon's death can decide whether the fixture is openable.
   The rows are seeded FRESH there too (no staleness), so a daemon's own first
   round — a fresh process with zeroed pass clocks — has nothing to report; ③
   backdates the heartbeat past both graces, leaving precisely the shape a killed
   round leaves behind: dead claim pid, hours-old heartbeat, no live process.
   Verified by 6 consecutive `-count` runs of the kill case and 3 consecutive
   runs of all five cases, green, after the change.
3. **The log assertion raced the daemon's log flush.** The status poll returns
   the instant the ledger flips terminal, but a daemon writes its log through a
   separately flushed stream — reading it that same millisecond found the
   disposal already recorded and the `[merger] [lifecycle] stale reported=…` line
   (and its buffered `disposed=1`) not yet visible, which is what made the first
   run report "the recovering component must be the merger's lifecycle tick".
   Fix: `zwWaitLogMarker` polls the LOG for the marker (bounded by `zwLogGrace`),
   separately from the status, returning the body either way so a genuine failure
   stays diagnosable.

## Files

- `cmd/agent/task_lifecycle_tick.go` (new) — the resident tick
- `cmd/agent/task_lifecycle_tick_test.go` (new) — the zero-worker proof (4 in-process
  cases + 2 real-process e2e: `TestMergerDaemon_DisposesZombieWithZeroWorkerProcesses`,
  `TestKilledWorkerDaemon_MergerTickRecoversTheStalledRound`)
- `devdoc/lifecycle-resident-tick.md` (new) — this report
- `cmd/agent/cli_tasks.go` — the merger's per-tick call site
- `cmd/agent/scratch_gc_tick.go` — the cadence comment now names `taskLifecycleThrottleMin`
- `cmd/agent/cli_tasks_worker.go` — `workerLifecyclePasses` removed, ownership taken
  in `runTaskWorkerLoop`, role passed, `autoDisposeConfirmedZombies`/
  `workerNotifySinkTarget` moved to the new file with the role parameter
- `cmd/agent/cli_tasks_zombie_dispose_test.go`, `cli_tasks_worker_idle_exit_test.go`,
  `cli_serve_test.go`, `serve_worker_split_test.go`, `singlenode_serve_test.go`,
  `cli_serve_stale_test.go` — retargeted at `runTaskLifecycleTick(workerRoleLabel)`
  / `throttleLifecycleTick()`

## Not Done

- The **daemon supervisor** alternative in the briefing's "e.g." list was not used:
  the merger tick is the better carrier (it is the component already resident on a
  task host, the scratch sweep already rides it, and it adds **zero** resident
  processes). `gopi worker --daemon` keeps working unchanged.
- `go test ./...` in this worktree: **only pre-existing failures remain** — see
  Verification. No new failure, no skip, no flake added by this change.
- The other epic items (1/5, 2/5, 4/5, 5/5) are out of scope here.

## Verification

- Build: `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → **OK**; `gofmt -l cmd/agent/` → clean; `go vet ./cmd/agent/` → clean.
- Focused: `go test ./cmd/agent/ -run 'TestMergerTick|TestMergerDaemon_DisposesZombieWithZeroWorkerProcesses|TestKilledWorkerDaemon_MergerTickRecoversTheStalledRound|TestWorkerLifecyclePass|TestRunTaskWorker_OnceSweepsZombieFirst'` → **ok** (all 9 PASS).
- E2E: both zero-worker cases PASS; red-proof mutation → both FAIL as quoted above.
- Full: `go test ./...` (log kept at `/dev/shm/lifecycle-final.log`). The remaining
  failures were each re-run on a PRISTINE master worktree
  (`git worktree add --detach … master`, tip `7358ee18`) and **reproduce there**
  — they are pre-existing, not caused by this diff (table below).
- The **5 stale-binary** e2e failures an earlier full run showed
  (`TestWorkerExecutorChildStandsInTheTasksRepoNotTheLaunchers`,
  `TestWorkerServesEachTaskInItsOwnRepoAcrossConsecutiveRounds`,
  `TestWorkerChildDoesNotInheritTheDaemonsSessionHandle`,
  `TestTaskRunStandsInTheSessionDirAndHandsTheWorkdirToTheChild`,
  `TestFrontRunConsoleOutputLandsInItsSessionDir`) were self-inflicted: this
  worktree's `bin/gopi` predated my last source edit, and the suite's
  build-freshness gate correctly refused to test a stale binary. Rebuilt → those
  five PASS (hence the build step above; **an edit to `cmd/agent/*.go` requires
  `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` before `go test ./...`**).
- **Ambient-env gotcha found while verifying**: a `go test` started from inside a
  gopi worker session inherits `GOPI_WORKER_DAEMON_CHILD=1` +
  `GOPI_DAEMON_READY_FD=3`, which makes `runTaskWorker` take the daemon path and
  die with `inherited worker lock fd 3 is not a usable file`. Every run in this
  report was therefore done as
  `env -u GOPI_WORKER_DAEMON_CHILD -u GOPI_DAEMON_READY_FD go test …`. It is an
  environment artifact of running the suite from inside gopi (reproduces on
  pristine master); its e2e half is what hermeticity defect 1 above fixed
  properly.
- Focused (three consecutive runs, no flake): the five cases above → **ok**, and
  `-count=6` of the kill case → **ok**.
- Red-proof re-run AFTER the hermeticity fixes (mutation: the one
  `runTaskLifecycleTick(mergerRoleLabel)` line commented out):
  `TestMergerDaemon_DisposesZombieWithZeroWorkerProcesses` FAIL 48.6s +
  `TestKilledWorkerDaemon_MergerTickRecoversTheStalledRound` FAIL 46.7s, both
  `status = "in_progress"` for the whole budget. Restored immediately
  (`git diff cmd/agent/cli_tasks.go` = the 12 added lines only).
- Full: `go test ./... -count=1` in this worktree. Only PRE-EXISTING failures
  remain, each reproduced on the pristine master worktree
  (`/home/user/workspace/gopi-master-baseline`, tip `7358ee18`):
  `cmd/agent` `TestParseTasksSweepZombiesArgs` / `TestEveryHelpConstantFitsTheWrapBudget`,
  `internal/signalrec` (2), `internal/stylegate`, `internal/testutil`
  `TestWorkerOwnershipPointerPresent` (a pre-existing devdoc missing the
  operator-only pointer — `devdoc/worker-status-record.md`, NOT a file this
  change adds).
- **Whole-suite diff vs pristine master (the acceptance's "no new failure"
  evidence)**: `env -u GOPI_WORKER_DAEMON_CHILD -u GOPI_DAEMON_READY_FD go test ./... -count=1`
  on the pristine master worktree (`7358ee18`) → log `/dev/shm/baseline-full.log`,
  **8** failing cases; this branch's full run → **6**, and every one of the 6 is
  in master's 8. Master's 2 extra are artifacts of the detached baseline worktree
  (no `bin/gopi` built there → `TestSweepZombiesCLI_DryRunTouchesNoNotifySink` /
  `TestSweepZombiesCLI_ReportNotifiesOperator` refuse to skip and report the
  missing binary) — this branch's worktree HAS the fresh binary, so they pass
  there. Failing set this branch: `TestParseTasksSweepZombiesArgs`,
  `TestEveryHelpConstantFitsTheWrapBudget`, `TestSignalRecChildVictimRecordsEndToEnd`,
  `TestSignalRecForwardingPreservesDefaultDeath`, `TestStyleGate`,
  `TestWorkerOwnershipPointerPresent`. **Delta introduced by this change: zero.**
- Commit: see the branch tip (final message names the hash).
