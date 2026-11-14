<!-- doc-class: historical -->
# The merger tick's scope — child 2/3 of epic 01a0b19d (slim the tick, rewrite the pins)

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

This is the CODE half of the epic. Child 1/3 produced the boundary table
(`devdoc/merger-duty-boundary.md`, branch `docs/merger-duty-boundary`, NOT on
master at the time of writing — its content was read from that branch's worktree
and is quoted by row number below). Child 3/3 re-homes the removed legs and
guards their fate. This document records what left the merger's execution path,
what stayed, what was rewritten rather than deleted, and the measurements the
acceptance asked for.

## THE CRITERION (child 1, quoted — it decides every verdict here)

> Key every resident duty by what it **acts on**: work keyed on a git ref /
> branch / worktree / repo path belongs to the git-scoped component (the merger);
> work keyed on a ledger row / task status / process claim stamp is task-manager
> scope and does **NOT** belong there — reachability ("something resident has to
> run it") is not a scope argument.

## Completed

### 1. Removed from the merger's execution path (`cmd/agent/merger_tick.go`)

| what left | where it was | its key (why it is not merger scope) | its surviving carrier |
|---|---|---|---|
| `scratchGCSweep(mergerRoleLabel)` | `merger_tick.go:197` (row 1, MOVES) | filesystem paths under the scratch root — neither git nor ledger | `cli_tasks_worker.go` (worker round ①′, already present) + the operator verb `gopi scratch-gc` |
| `runTaskLifecycleTick(mergerRoleLabel)` | `merger_tick.go:198` (rows 2/3/4 ride this pass) | ledger rows + process claim stamps (stale report, confirmed-zombie disposal, finish cleanup, and the throttle/ownership bookkeeping that arbitrated it) | `cli_tasks_worker.go:469` (worker round ①, already present) + `gopi task sweep-zombies` / `gopi task list --sweep` |
| `setFinishCleanupSweepOwnerRemoverWired()` | `merger_tick.go:102` (row 4) | the row-owned-worktree handover's subject is a ROW's delivery chain (claim stamp, row status) | `acquireTaskLifecycleOwnership` (`task_lifecycle_tick.go`) — taken where the row-keyed pass is owned, cleared by its release |

The replacement is a contract comment in the tick that **states the boundary and
why the task-row bookkeeping left** (the acceptance's "retarget the tick's
contract comment"), names each removed leg, its key, and its surviving carrier —
including the explicit note that reachability was the whole of the deleted
justification and that the component it named (a resident merger) no longer runs
at all (`devdoc/merger-retired.md`).

### 2. What STAYS, and why

* master safety (`mergerMasterSafetySweep` → `tools.MergerForcedSafety`) — git
  refs / a mid-merge working tree (row 7: STAYS). Still above the lock and above
  the launch, unchanged.
* the take-over launch (`mergerTakeoverRun`) — the tick's whole remaining job on
  the cron face.
* the serial face legs (`mergerLaunchSurvivingLegs` → `mergerSerialFaceLegs`) —
  the durable-rebuild mark, the dead-holder ladder (resume → reclaim) and the
  declared-peer sweeps (row 8). Every one is keyed on a git artifact, a holder's
  worktree, or an operator declaration.
* the pre-spawn gate + the finished-reap backstop (row 6) — the launcher's own
  child state, untouched.
* the `--once` face's synchronous pass (`mergerPassOnce`) and its completion
  line (`mergerOnceDoneLine`) — **the stdout seam is intact** (measured below).

### 3. The one ADDITION: the reap gets a merger-owned carrier (row 5)

`serveWorktreeReapSweep` is keyed on worktree **registrations** of a repo (git
admin state, aged, no ledger row required), so row 5's verdict is "STAYS by key
— carrier flagged": its carrier *was* the ledger-keyed lifecycle pass this change
removes. Leaving it there would have de-wired the reap on the very host it exists
for — the measured 2026-09-16 shape (`worktree_reap_reach_test.go:5-12`: the wiring
test stayed green while no resident tick ran the reap).

So the launching face now runs it too (`mergerLaunchSurvivingLegs`, inside the
same bounded goroutine as the rest of the serial legs). The lifecycle pass **also
still** fires it — the worker round is today's live carrier, and the merger's
entry points are refused — so the reap has a reader on every host shape, and the
git-scoped component has its own trigger for the re-development. This is the
"pair it with a merger-owned carrier" instruction of row 5, executed rather than
recorded as a decision (child 3 can still rule on the duplicate).

### 4. Rewritten, not deleted — the pin tests

| old test | where it went | what changed |
|---|---|---|
| `TestMergerTick_DisposesZombieWithNoWorkerAlive` | → `TestTaskLifecycleTick_DisposesZombieOnTheWorkerRound` | same leg, same assertions; role label `merger` → `worker` (the component whose key the leg is) |
| `TestMergerTick_ReclaimsDeadHolderWorktreeWithNoWorkerAlive` | → `TestTaskLifecycleTick_ReclaimsDeadHolderWorktree` | same end-to-end acceptance (safe-merge gate [c] flips refuse → exit 0); driven on the worker tick. **SUPERSEDED 2026-10-30**: the worker-tick call site was retired in `58b5618b`, so the acceptance moved again, to the seam that runs the arm now — `TestMergerPass_ReclaimsDeadHolderWorktree` (`cmd/agent/cli_tasks_dead_holder_accept_test.go`). The name above exists nowhere in the tree. |
| `TestMergerTick_YieldsToLiveWorkerLoop` | **dropped, with its subject** (see `## Not Done`) | the ownership-yield ladder existed only to arbitrate merger-vs-worker duplication; with the merger out of the pass there is nothing to arbitrate. The worker's own carrier keeps the pass's other tests. |
| `scratch_gc_tick_test.go` role-label case | rewritten in place | `scratchGCTick(mergerRoleLabel, …)` → `workerRoleLabel` |
| `TestLifecyclePassInvokesWorktreeReap`, `TestLifecycleTickInvokesWorktreeReap`, `TestLifecyclePassEscalatesDirtyOrphanEndToEnd` | rewritten in place | merger role → worker role (the pass's remaining carrier) |
| — | **NEW** `TestMergerLaunchingFaceInvokesWorktreeReap` | the criterion's one addition, proven behaviourally on the launching face |

**NEW FILE: `cmd/agent/merger_tick_scope_test.go`** closes child 1's finding F1
("the tick's own call sites are asserted by nothing" — `scratchGCSweep` occurred
in tests only as a role-label assertion and `mergerLaunchSurvivingLegs` in zero
test files; the file that used to pin exactly this wiring,
`cmd/agent/merger_tick_legs_test.go`, had been deleted by the retirement commit
`f9ff1a89`). Its pins are **two-sided on purpose**:

1. `TestMergerTickCarriesNoTaskPlaneLegs` — the removed calls are ABSENT from the
   merger's own file, and the boundary is STATED there.
2. `TestMergerTickKeepsItsGitScopedLegs` — the git-keyed legs are still wired (so
   "slim" cannot be satisfied by emptying the tick).
3. `TestMergerLaunchingFaceCarriesTheWorktreeReap` — row 5's addition.
4. `TestRemovedLegsStillHaveALiveReader` — Rule 15: each removed leg still has a
   live reader (the worker round's two calls; the ownership mark's wire *and*
   clear). A removal that reaches nothing fails here.
5. `TestMergerTickEmitsNoLifecycleLines` — the operator-observable acceptance,
   in-process: no `[lifecycle]`, no `[zombie-dispose]`, no `[scratch-gc]`, no
   `[finish-cleanup]` line from a merger tick, with both removed legs explicitly
   DUE and a stale scratch entry present (so "no output" cannot mean "empty
   tree"), while master safety still reports and the `--once` completion line is
   still printed.

## Evidence (self-filed)

All commands run in the worktree `/home/user/workspace/gopi-merger-tick-slim`
(branch `feat/merger-tick-slim`), the red-proof in
`/home/user/workspace/gopi-redproof` (detached at base `1b3d54b2` with ONLY the
new/rewritten test files copied in).

### E1 — the acceptance measurement: no lifecycle / zombie-sweep line (before → after)

**Why a tick RIG and not a live `gopi merger`:** the merger verb is retired at
BOTH entry points (`merger: retired — the resident merger daemon was retired for
re-development`, measured on this branch's own freshly built binary, exit 0), so
no live tick can be driven from the CLI. The rig below is therefore the built
code path itself — `runTaskMerger` is the function the cron face calls — driven
in a sandbox with a configured repo. It is a measurement of the shipped code, not
an assertion about it.

One bare merger tick, both streams captured, a confirmed zombie seeded, the
lifecycle clock zeroed, the scratch sweep DUE with a stale entry present:

```
BEFORE (base 1b3d54b2, TestZZTickDump):
  [merger] [scratch-gc] removed 1 stale scratch entry older than 6h0m0s from …/003
  [gopi] [lifecycle] resident task-lifecycle sweep (stale report + confirmed-zombie disposal + finish cleanup) active on this process; every 10m0s
  [merger] [zombie-dispose] t-zd-confirmed disposed (confirmed, stalled) — heartbeat stop + claimed pid 245196 dead (peerbox) — confirmed zombie
  [merger] [lifecycle] stale reported=2 disposed=1 sink=cat >> …/002/notify.log
  [merger] [/…/sandbox-repo] merged=0
  [merger] --once: single tick done
  ledger row t-zd-confirmed status after the tick = failed
  stale scratch entry: COLLECTED

AFTER (feat/merger-tick-slim, same harness):
  [merger] [/…/sandbox-repo] merged=0
  [merger] --once: single tick done
  [merger] master safety [/…/sandbox-repo]: tip (unknown), INSPECTION FAILED: git rev-parse HEAD failed (not a git repo?)
  [merger] gopi task merger: single tick (cron form) — master safety, then one full pass
  ledger row t-zd-confirmed status after the tick = in_progress
  stale scratch entry: still present (not collected)
```

`[lifecycle]`, `[zombie-dispose]` and `[scratch-gc]` are gone from a bare merger
tick; master safety still reports; `[merger] --once: single tick done` (the
`mergerOnceDoneLine` seam) is still printed. The harness file was **temporary and
is not committed**; it is reproduced verbatim below so the measurement is
repeatable:

```go
// cmd/agent/zz_tick_dump_test.go (TEMPORARY evidence harness — not committed)
// seeds a confirmed zombie + a stale scratch entry, zeroes the clocks, points
// settings at a sandbox repo, then:
out := mtsCaptureAll(t, func() { runTaskMerger(&tasksOptions{once: true}) })
t.Logf("=== TICK OUTPUT BEGIN ===\n%s=== TICK OUTPUT END ===", out)
```

### E2 — `grep -n 'runTaskLifecycleTick\|scratchGCSweep' cmd/agent/merger_tick.go`

```
$ grep -n 'runTaskLifecycleTick\|scratchGCSweep' cmd/agent/merger_tick.go
103:	// row 4). `setFinishCleanupSweepOwnerRemoverWired` wires the handover the   ← comment
```
(no runTaskLifecycleTick / scratchGCSweep line at all; the only hit is the
comment line that names the criterion and the removed wiring — see E3.)

### E3 — the boundary is stated in the tick

```
$ sed -n '/NO TASK-PLANE SWEEP RUNS HERE ANY MORE/,+2p' cmd/agent/merger_tick.go
	// NO TASK-PLANE SWEEP RUNS HERE ANY MORE — THE CRITERION
	// (epic 01a0b19d, child 2/3; the verdict table is
	// devdoc/merger-duty-boundary.md). Key every resident duty by what it ACTS
```

### E4 — the rewritten pins are RED on the pre-change tree, GREEN after

Same test files, same binary, two trees:

```
$ (gopi-redproof, base 1b3d54b2 + the new/rewritten test files)
--- FAIL: TestMergerTickCarriesNoTaskPlaneLegs (0.00s)
    merger_tick.go still calls scratchGCSweep(mergerRoleLabel) — the leg is keyed on …
    merger_tick.go still calls runTaskLifecycleTick(mergerRoleLabel) — …
    merger_tick.go no longer states the duty boundary ("criterion" absent) — …
--- FAIL: TestMergerLaunchingFaceCarriesTheWorktreeReap (0.00s)
--- FAIL: TestRemovedLegsStillHaveALiveReader (0.00s)      ← merger_tick.go still calls the wiring
--- FAIL: TestMergerTickEmitsNoLifecycleLines (3.05s)
    a merger tick emitted "[merger] [lifecycle]" … "[merger] [zombie-dispose]" … "[lifecycle] stale reported=" … "[merger] [scratch-gc]"
    the stale scratch entry was collected by the merger tick … — the sweep ran
    the merger tick touched ledger row t-zd-confirmed (status "failed") — it must sweep no rows at all
--- FAIL: TestMergerLaunchingFaceInvokesWorktreeReap (1.31s)
FAIL  github.com/nick/gopi/cmd/agent  3.698s

$ (gopi-merger-tick-slim, feat/merger-tick-slim)
--- PASS: TestMergerTickCarriesNoTaskPlaneLegs (0.00s)
--- PASS: TestMergerTickKeepsItsGitScopedLegs (0.00s)
--- PASS: TestMergerLaunchingFaceCarriesTheWorktreeReap (0.00s)
--- PASS: TestRemovedLegsStillHaveALiveReader (0.00s)
--- PASS: TestMergerTickEmitsNoLifecycleLines (2.81s)
PASS  github.com/nick/gopi/cmd/agent  3.428s
```

### E5 — build / vet / branch-scoped suite

```
$ go build ./...                              → exit 0 (no output)
$ go vet ./cmd/agent/                         → exit 0 (no output)
$ go test ./cmd/agent/ -run 'TestTaskLifecycleTick|TestMergerTick|TestLifecyclePassInvokesWorktreeReap|TestLifecycleTickInvokesWorktreeReap|TestMergerLaunchingFaceInvokesWorktreeReap|TestScratchGCTick|TestMergerOnceLeavesPendingTaskUntouched|TestCloseLoopWiringIsComplete|TestPassOnceDrainsMarksAtPassStart|TestKilledWorkerDaemon' -count=1
ok  github.com/nick/gopi/cmd/agent  27.099s
$ make build                                  → CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/ ; bin/gopi --version → dev
$ GOPI_TEST_TIMEOUT=30m scripts/test-budget.sh ./cmd/agent/
   (branch-scoped smoke: the diff touches cmd/agent only)
```

### E6 — the retire decision is measured, not assumed (Rule 27, in its positive form)

`TestMergerTick_YieldsToLiveWorkerLoop` was dropped because its subject — a
duplication the merger's participation created — is gone. The measurement that
the dropped case had no remaining subject:

```
$ grep -n 'runTaskLifecycleTick\|scratchGCSweep' cmd/agent/merger_tick.go   → no call site (E2)
$ grep -n 'taskLifecycleSkipFormat\|yield' cmd/agent/task_lifecycle_tick.go
```

The pass's own tests (the zombie-disposal and dead-holder acceptance above) run
UNCHANGED in substance and GREEN on the worker tick (E4/E5) — so the leg's
coverage survived the carrier move, which is what "rewrite, not delete" means
here.

## Not Done (honest)

* **Child 3's re-homing is not in this branch.** The removed legs keep their
  existing carriers (the worker round's two calls and the operator verbs); this
  change does not add a new trigger for them, and it does not measure that they
  fire from the new home. That is child 3's acceptance, by the epic's own split.
* **The reap now runs on TWO carriers** (the worker's lifecycle pass and the
  merger's launching face). Boundary row 5 asked for the merger-owned carrier;
  retiring the lifecycle-side call is a decision for child 3 (it must first show
  the launching face fires on the deployed host). Recorded here rather than
  silently "cleaned up".
* **`TestMergerTick_YieldsToLiveWorkerLoop` was dropped, not rewritten.** Its
  subject — arbitrating merger-vs-worker duplication inside one process — has no
  referent once the merger does not run the pass. The ownership mark itself is
  kept (it is what wires the sweep-ownership handover now), and
  `TestRemovedLegsStillHaveALiveReader` pins its wire+clear. A reviewer who
  disagrees can restore the case against the mark alone.
* **No behaviour change to `mergerPassOnce`'s own legs.** The `--once` face still
  runs `mergerSerialFaceLegs` (drain, ladder, peers) via the pass prologue; that
  is unchanged by this task and is where the finish-cleanup SWEEP is not (the
  sweep is the row-keyed leg that left).
* **The boundary table is not on master.** `devdoc/merger-duty-boundary.md` lives
  on `docs/merger-duty-boundary` (child 1's branch, still unmerged), so this
  branch references it by branch name; row numbers in this document are its rows.

## Files

* `cmd/agent/merger_tick.go` — the slimmed tick + the retargeted contract comment.
* `cmd/agent/task_lifecycle_tick.go` — the sweep-ownership handover moved onto the
  task-lifetime ownership mark (take + clear).
* `cmd/agent/merger_tick_scope_test.go` — NEW: the two-sided scope pins.
* `cmd/agent/task_lifecycle_tick_test.go`, `cmd/agent/worktree_reap_reach_test.go`,
  `cmd/agent/scratch_gc_tick_test.go` — the rewritten pins.
