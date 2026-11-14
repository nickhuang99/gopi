<!-- doc-class: historical -->
> **CORRECTION (2026-09-19, epic `merger-delete-dead-impl`).** Read this as a
> record, not as current truth: the `--stop` / `--restart` faces it measured were
> deleted with the resident merger (`f9ff1a89`, `55c4d6f3`). Today `gopi merger`
> refuses at EVERY spelling (exit 1, `mergerRetiredCause`) and the one-tick merge
> is the bare `gopi merge`. `devdoc/merger-retired.md` is the record;
> `devdoc/merger-self-driving.md` carries the flag table with the same dated
> correction.

# Task Report: `gopi merger --stop` / `--restart` return immediately; `--force` retired

Epic: **merger-stop-immediate** (branch `feat/merger-stop-immediate`, worktree
`~/workspace/gopi-merger-stop-now`).

## The defect, in one paragraph

`gopi merger --stop` waited for the in-flight merge pass to reach its boundary —
bounded by `tools.MergerWaitBoundDefault` = **120m**, chosen because one gated
merge can spend a 50-minute baseline sweep plus a 45-minute verify child and the
merger interrupts neither. So the operator's "stop it" could block for 95
minutes, and the only escape the command offered was `--force`, which SIGKILLed
exactly the children the wait was honouring. The wait's stated guarantee ("never
a half-merge") and its escalation path (kill the merge) contradicted each other,
and the guarantee was empty anyway: a merge pass is **idempotent** — it asks git
what is merged and re-derives the whole surface every tick — so "let the
boundary happen" protects nothing the next pass (or the successor) does not
recompute.

## Completed

- **`--stop` is now a REQUEST + an OBSERVATION** (`cmd/agent/cli_merger_stop.go`).
  It delivers the durable request file and SIGTERM, then observes the target for
  a bounded `mergerDeliveryPoll` (3s) and reports what it sees. It never waits
  for a pass. Exit codes narrowed accordingly: `0` = the request was delivered
  (or nothing was running); `1` = it was **not** delivered, or the live set was
  ambiguous.
- **`--restart` needs no quiet window** — the successor waits on the singleton
  flock instead of the CLI waiting for the old daemon
  (`mergerAcquireServeLockForHandover`, `cmd/agent/cli_merger_daemon.go`, plus a
  new `daemonSpec.controlWait` in the shared spawn mechanism). The ordering
  primitive is the flock, never a sleep; the wait is bounded
  (`mergerHandoverWait` = 10m) and a timeout names the holder (Rule 15).
  A new sentinel `errServeLockHeld` (`cmd/agent/cli_tasks.go`) distinguishes
  "a live holder" (worth waiting for) from every other acquisition failure
  (returned at once).
- **`--force` is RETIRED** — removed from the daemon entirely: `killPassChildren`,
  `mergerForcedExit`, the out-of-band escalation watchdog (a 250ms ticker that
  ran *during* a pass), `mergerConfiguredRepos`, the force request-file read and
  the `force` stop flag are all deleted, not disabled. The word is still
  **accepted and ignored** at the CLI with a printed deprecation note, so an
  existing service unit keeps working. `--wait` likewise: still parsed and still
  validated, no longer bounding anything.
- **Master safety did not lose its caller — it changed caller.** All signals now
  mean the same graceful stop (`mergerStopSignals`), and
  `tools.MergerForcedSafety` (unchanged) is now invoked by the **successor's
  start-up** (`mergerMasterSafetySweep`), i.e. by a process that is alive and
  holds the lock, instead of by the dying one. That is strictly better: the
  repair runs for any predecessor death (SIGKILL, OOM, power), not only for a
  forced exit.
- **Asserted fallout fixed: a state file may no longer advertise a dead child.**
  `MergerInterrupt` now carries `ChildPIDs` beside the human-readable labels;
  `LiveChildren()` / `MergerState.LiveInterrupt()` drop entries whose pid is
  gone, `MergerState.Render()` renders only live children, and
  `MergerStateFile.Report()` prefixes "pid N is GONE" when the writer is dead.
  An entry with no pid is reported **alive** (never invent an absence you cannot
  measure). New reader `MergerStateFileAt` for a record whose writer is gone.
- **Unblocked the tree**: `cmd/agent` did not even compile on this base — two
  test helpers named `funcBody` (`cli_tasks_rebuild_pass_test.go:204`,
  `merger_resume_arm_wiring_test.go:104`). Renamed the merger gate's copy to
  `mergerFuncBody` (separate commit `fe8fe185`), so the whole package — and
  therefore every test in it — can run at all.

## Not Done (honest)

- **Three pre-existing failures were NOT fixed** (none is this epic's; each was
  reproduced on a pristine master worktree @ `9aec81eb` with only the
  `funcBody` collision patched out — baseline log `/tmp/master-baseline2.log`):
  - `TestMergerDaemon_LeavesTheLoginSessionScope` and
    `TestMergerFlock_DaemonLockStopsWithTheDaemon` — this host cannot obtain a
    cgroup escape (`daemon pid … is not a session leader (pgid=<launcher>)`, and
    `daemon: scope DEGRADED` never appears);
  - `TestCLIContractTaskServeAliasPrintsOneLinePointer` — the alias's `-h`
    prints the ambient `[gopi] tmpdir:` notice as an extra line (a test-hygiene
    issue in the suite's environment, red on master too).
- **The CLI's `--wait` flag is kept but inert.** Removing it was the other
  option; keeping it (parsed + validated + announced as retired) was chosen
  because an unknown flag in an existing unit file is a worse failure than a
  no-op, and a *silently* accepted garbage value would hide a typo.
- **`workerControlForce` / the worker's `--force` are untouched.** This epic
  retired the *merger's* escalation; the worker's drain semantics are its own
  and were explicitly out of scope.
- No live production host was exercised: the acceptance evidence below is the
  test rig (real binary, real daemon, real children), not the operator's own
  merger (Rule: never touch the live merger).

## Verification

**Live operator transcript (a real detached daemon, a real handover, this
branch's binary; `GOPI_HOME`/`GOPI_SETTINGS` pointed at a throwaway profile):**

```
$ gopi merger --daemon --every 5s
merger daemon started: pid 2676401, log .../logs/merger.log

$ gopi merger --stop
gopi merger: asking pid 2676401 (lock holder: pid 2676401, started ...,
             passes 3, last pass: 1 repo(s) swept, 0 landing(s), idle since ...,
             note: idle: no pass in flight (a stop would exit at once)) to finish in-flight work and exit
gopi merger: current state — pid 2676401, started ..., passes 3, ... note: idle: ...
gopi merger: request delivered to pid 2676401 (SIGTERM + a request file) — this command does NOT wait for the in-flight pass
gopi merger: pid 2676401 exited 1.107s after the request
exit=0 elapsed=1.220s          # the command's own wall clock; it shipped the request and observed, nothing more

$ gopi merger --stop --force --wait 25m          # the retired spellings, still parseable
gopi merger: no merger is running (no live merger state under .../merger)
exit=0
  stderr: gopi merger: --force is retired and has no effect — a stop returns at once now, and the merger
                  finishes its own in-flight work at its own boundary (there is nothing left to interrupt)
  stderr: gopi merger: --wait is retired and has no effect — this command never waits for a pass (it delivered
                  the request and returned); the value is still parsed, so `--wait=DURATION` keeps working in
                  existing scripts

$ gopi merger --start --every 5s   &&   gopi merger --restart --every 5s
merger daemon started: pid 2677468, ...
gopi merger: request delivered to pid 2677468 ...
gopi merger: pid 2677468 exited 706ms after the request
merger daemon started: pid 2677563, ...            # the successor took the lock the old one released
restart exit=0 elapsed=0.983s

$ gopi merger --status
merger:      alive
lock holder: pid 2677563
```

And the merger's OWN account of the stop, from its log (it is the process that
decides when to leave — the command only asked):

```
[merger] [repo] merged=0
[merger] sleeping 5s
[merger] graceful stop (terminated received): no pass in flight — releasing the single-instance lock and exiting
[merger] final state: pid 2677563, started ..., passes 2, ..., note: idle: no pass in flight (a stop would exit at once)
```

Build / vet:

```
$ go build ./...          # exit 0
$ go vet ./cmd/agent/ ./internal/tools/   # exit 0
```

Behavioural evidence (real binary, real daemon, real sleeping child process):

| # | What | Test | Result |
|---|------|------|--------|
| 1 | `--stop` against a **busy** merger returns at once (≤10s, vs the old 120m wait), exit 0, and the report says it does not wait | `TestMergerRestart_BusyIsNotInterruptedByStop` | PASS (44.7s) |
| 2 | the in-flight child is **not** signalled by any stop source (CLI stop, every signal, request file, stray `FORCE_<pid>`), runs to its natural end | `TestMergerStopPathsNeverSignalPassChildren` | PASS (43.1s) |
| 3 | `--force` is accepted, announced as retired, and interrupts nothing | `TestMergerForce_IsRetiredAndNeverSignals` | PASS (40.5s) |
| 4 | `--restart` replaces a running merger in ONE command (successor waits for the lock) | `TestMergerRestart_RestartRunsTheSuccessorInOneCommand`, `TestMergerRestart_StopReleasesTheFlockForASuccessor` | PASS |
| 5 | a state file naming a DEAD child is not rendered as in-flight work / busy | `TestMergerStop_StateFileWithDeadChildReportsNoChild` | PASS (0.6s) |
| 6 | two actions in one argv are still refused, and the refusal names every word (incl. the retired modifier) | `TestMergerRestart_ControlFlagsAreRefusedTogether`, `TestModifierForceIsNotASecondAction`, `TestControlConflictMessage_NamesEveryDistinctAction` | PASS |
| 7 | `merger -h` documents the actions, the retirement and the one-home rule | `TestMergerRestart_HelpDocumentsTheControlFlags` | PASS |
| 8 | the retired bound is not redeclared, and the ONE remaining wait is bounded | `TestMergerStopHasNoInFlightWaitBound`, `TestMergerDaemonSpec_OnlyAHandoverWaitsForTheLock` | PASS |
| 9 | the machine-checkable standards still hold after the retirements | `go test ./internal/standards/ ./internal/stylegate/ ./internal/tools/` | ok / ok / ok |

Full cmd/agent sweep (`-run 'TestMerger|TestModifier|TestStatusAction|TestControl|
TestTopLevel|TestUsage|TestWorkerForce|TestCLIContract'`, 186s): only the three
pre-existing failures below, plus nothing else — log `/tmp/final-tests.log`.

**How the stale state was forced (honesty note for check 5):** not by
hand-writing a file. The rig starts a real merger whose resolve child is
sleeping, waits for the state file to name that child, then `SIGKILL`s the
merger (uncatchable ⇒ no cleanup path runs, which is the real-world way this
file survives its writer) and kills the child. The file on disk therefore names
a dead child, and the assertion is that its verdict does not.

Regression tests red-on-master / green-after: checks 1, 2, 3, 5 are all red on
master by construction — (1) blocks for the whole pass, (2) and (3) kill the
child (`--force`/SIGUSR2 both SIGKILLed its process group), (5) renders the dead
child as in-flight.

## Deliverable location (Rule 6d)

- Branch: **`feat/merger-stop-immediate`** (checked out in the worktree
  `~/workspace/gopi-merger-stop-now`, which shares the main repo's refs — the
  commits are already in `~/work/GoPi`).
- Recover a file: `git show feat/merger-stop-immediate:devdoc/merger-stop-immediate.md`
- Commits: `fe8fe185` (test-helper collision fix), plus the epic commit
  (see `git log --oneline feat/merger-stop-immediate`).
- Nothing is left only in a temp clone; no `rm -rf` was run.

## Design notes worth keeping

1. **Why the wait was not just shortened.** A shorter bound still blocks the
   operator, and still needs an escalation path for the pass that outlives it —
   i.e. it keeps the contradiction. Deleting the wait deletes the need for the
   escalation.
2. **Why `--restart` did not need a wait of its own.** The single-instance
   flock already IS the handover primitive: whoever holds it is the merger, and
   the moment the old one exits the successor takes it. The CLI's job shrank to
   "ask, then try to start"; the successor's start absorbs the overlap.
3. **Why the state reader needed liveness, not just cleanup.** A writer cannot
   clean up after SIGKILL, so "the file is only written while alive" was never
   true. The liveness measurement belongs at render time, on the pid, which is
   what `MergerStateFile.Report()` now does.

4. **Which option was taken for `Busy()` — and why the liveness answer lives in
   the READER, not inside `MergerState.Busy()`** (follow-up to this epic,
   branch `fix/merger-busy-liveness`). `MergerState.Busy()` keys on
   `LastPassStart`/`LastPassEnd`, so a surviving file from a SIGKILLed merger
   still answered `Busy()==true` — the child list had been made liveness-aware
   (`LiveChildren`/`LiveInterrupt`, item 3 above) while the PASS-level claim was
   only asserted at render time and by nothing at all. Two options were weighed:
   (1) make `Busy()` itself consult the pid, (2) keep `Busy()` the written
   record's verdict and make the reader-only contract explicit.
   **Option 2 was taken.** `MergerState.Busy()` is a *value method*: it receives
   nothing but the parsed timestamps, so it *cannot* measure the process that
   wrote them — giving it liveness would mean reaching for a pid the record
   merely claims, and it would make the method lie about the record it is the
   verdict on (the same state must keep answering "the pass never
   ended", which is what a reader of the JSON, the writer's own snapshot, and
   the forced-exit report all want). The liveness measurement therefore stays in
   the ONE place that has the pid — the file — and it now has a NAME and a TEST:
   `MergerStateFile.Busy()` is `State.Busy()` **AND the writer's liveness**
   (pid measured, `PidAlive`), and `MergerStateFile.Report()` states the verdict
   instead of leaving it to be inferred from a stale "IN-FLIGHT pass" ("…, and
   no pass is in flight (the writer is gone — a dead record is not busy)"). A
   file whose name yields no usable pid reads **not busy**: no writer it can
   vouch for, and the fail-open direction is the one that never makes an
   operator wait for a phantom. Callers split accordingly and by name: the
   writer's own `Snapshot()` (which knows its process is alive by construction)
   reads `MergerState.Busy()`; anything holding a FILE reads
   `MergerStateFile.Busy()` — including `MergerLiveStateFiles`, which drops
   dead writers before any verdict is asked for. `TestMergerStateFileBusyRequiresALiveWriter`
   drives the real stale fixture (a state file written by `MergerWriteState` for
   a reaped pid — the bytes a SIGKILLed merger actually leaves) and asserts both
   halves plus the mirror (the same record with a live writer IS busy, so the
   test cannot pass by never saying busy); it is red on master
   (`MergerStateFile` has no `Busy`, and the record answers busy for a dead
   writer) and green after.
