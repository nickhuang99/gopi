<!-- doc-class: historical -->
# Task Report: the worktree reap was wired to nothing — make it reachable, and give the ownerless-dirty orphan an exit
<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

Branch: `feat/worktree-reap-reachable` · worktree `~/workspace/gopi-wt-reap`
Rebased onto master `30450fc6` (tip at first commit was `1cfe2f1f`).

## Completed

### 1. Defect 1 — the reap ran from nothing but its own tests (fixed)

`serveWorktreeReapSweep` (`cmd/agent/cli_tasks.go`) — the ownership-INDEPENDENT
reap, the only instrument that can collect a registration **no ledger row
remembers** — was appended *inside* `serveMergedCleanupSweep` on 2026-09-14
(`35faf715`, "run the dead-registration reap on the resident merge-cleanup
tick"). Its premise was that `serveMergedCleanupSweep` rides a resident tick.
That premise had already stopped holding on 2026-09-04 (`d11af9a6`, "merger pass
is config-driven per-repo sweep"), which deleted that function's **only**
production caller (`git show d11af9a6 -- cmd/agent/cli_tasks.go` → `-	for _, line
:= range serveMergedCleanupSweep() {`). Measured at the tip of this branch,
`serveMergedCleanupSweep` has **no production caller at all** — the only
remaining references are its own definition and its test file:

```
grep -rn 'serveMergedCleanupSweep' --include=*.go cmd/agent | grep -v _test
cmd/agent/cli_tasks.go:3178:// serveMergedCleanupSweep runs tools.MergedCleanupSweep over the gopi repo
cmd/agent/cli_tasks.go:3183:func serveMergedCleanupSweep() []string {
```

So the reap was reachable from its own test and from nothing else: the wiring
test passed, the host never reaped. That is not "sometimes reachable" — the
carrier is dead in every mode, and the live counter confirms it
(`grep -c 'worktree-reap' ~/.gopi/logs/{worker,merger}.log` → 0 and 0).

**Fix**: the reap rides `taskLifecyclePass` (`cmd/agent/task_lifecycle_tick.go`),
the task-lifecycle tick that BOTH resident components fire (the worker loop and
the merger's tick) — a resident duty on a real clock, no new daemon. It is
deliberately NOT gated on the pass's throttle: the sweep is cheap and bounded
(one `git worktree list` per repo + a few stats) and its whole purpose is
latency. The dead carrier no longer carries it (two carriers would be two ticks
racing over one registration); a test now pins that too.

### 2. Defect 2 — the dirty, ownerless orphan (escalated, never removed)

A worktree that is DIRTY and that **no ledger row owns** was unreachable by
every arm on the host: the producer pass (`ReclaimOwnFinishedWorktrees`) needs a
row stamped with *this* host's own claim, the dead-holder arm needs a *terminal*
row, and the age sweep's guard ② (`reclaimDisposition`) refuses any dirty tree.
Live proof of the class (drive-through residue from sibling task
`fix/worktree-sweep-memory-base-policy`, whose branch was merged).

**Fix** (in `worktree_sweep.go`, a new rung of the judge, `worktreeEscalateOrphan`):
a dirty registration with **no owning row** past the age floor is **ESCALATED to
a human** — logged (`escalated orphan=…`) and pushed on the notify channel as the
`worktree-orphan` event, once per (repo, path) per process. It is **never
removed**: uncommitted work is a human's call and this sweeper's removal verb has
no `--force` by design. A dirty registration that DOES have an owning row keeps
the plain report (that is somebody's unfinished delivery, not residue). An
unmeasurable age also keeps the plain report — "nobody owns this and I cannot say
how long" is not an escalation.

### 3. Defect 3 (found live, while verifying defect 1) — the age signal was the REPOSITORY's age

`worktreeSweepCreatedAt` read `<worktree-gitdir>/commondir`. For a worktree of
the **main** repository — every worktree this host registers — `commondir`
contains the MAIN repo's git dir, so its mtime is the **repository's** age, not
the registration's. Measured on this host: the main repo's `commondir` mtime was
2026-06-22 (months old) while `~/workspace/gopi-wt-reap` had been created minutes
earlier.

This was invisible while the sweep was unreachable (defect 1) and became a **live
hazard** the moment the reap became reachable: five worktrees under
`/home/user/workspace` (mine, plus sibling agents' checkouts and detached probe
worktrees) all measured as months old — i.e. the age floor, whose entire purpose
is to leave alone a registration somebody just made, would have permitted the
resident tick to `git worktree remove` a sibling's working directory.

**Fix**: the age comes from the registration's OWN records — the `gitdir`
backlink file (fallback: `HEAD`), both written once at registration and stable
through routine verbs (`worktree list`, `status`, `add`, `prune` — measured).
The git dir's mtime is deliberately NOT used although it is the newest of the
three: any `git status` bumps it through a lock-and-rename, so "newest" would
make the registration look freshly created every time the sweeper looked at it.
Consequently the fixture recipe also changed (aging `commondir` no longer ages
anything); all sweep/reclaim fixtures age the registration's own record now.

**Safety re-check after the fix** — the operator verb in dry-run against the live
host, the sweep's own verdicts:
```
sweep-worktrees (dry-run): repos=2 reclaimed=0 would-reclaim=0 reaped=0 branches-reaped=0 escalated=0
  keep [main-checkout] age=0h /home/user/work/GoPi
  keep [live-holder]   age=0h …/queue-runtime-merge-resolve-2938248/…/wt
  report               age=0h /home/user/workspace/gopi-firstopen       (dirty, kept)
  report               age=4h /home/user/workspace/gopi-master-baseline (dirty, kept)
  report               age=3h /home/user/workspace/gopi-master-check    (dirty, kept)
  keep [live-holder]   age=0h /home/user/workspace/gopi-wt-reap
  keep [main-checkout] age=0h /home/user/Downloads/games/Maps
```
Nothing on the live host is a removal candidate: no `would-reclaim`, and the
dirty registrations are `report` (kept). The same run **before** the age fix
listed those registrations as `reclaim`/`age=?` — the difference is the fix.

### 4. Sub-finding: `"all"` did not mean all (fixed)

`GOPI_TASK_NOTIFY_EVENTS=all` expanded to the task-status set plus three alarms —
so the "subscribe to everything" subscriber was the one subscriber that would
never hear the new alarm. `worktreeOrphanEscalationEvent` was added to that set
(Rule 15: a refusal/escalation is never silent). Found because the end-to-end
escalation test subscribes with `all` — the honest failure was the feature, not
the test.

### 5. Guards preserved (no regression)

- Removal stays plain `git worktree remove` — **no `--force` anywhere**.
- The **branch is never deleted** by the orphan path (the escalated shape has no
  safe-merge evidence, so the dead-registration arm — which does `branch -d` only
  when git agrees — is not even reached).
- Guard ③ (`finishCleanupWorktreeInUse`) still protects a worktree a live worker
  holds (live output above: `keep [live-holder]`).
- Guard ② DIRTY semantics unchanged; the landed classifier
  (`reclaimDirtyVerdict`) is untouched.
- The clean-plus-terminal-row arms that already release keep releasing: the live
  dry run reports `reclaimed=0 would-reclaim=0` and the same three `report` lines
  as before the change — my rung adds no new removal anywhere on the live set.

## Not Done (honest)

- **The reap runs on the operator's NEXT worker/merger restart.** This session
  cannot restart the resident daemons (the agent-spawn guard exists for exactly
  that, and a live worker must not be killed). Live evidence is the **real binary
  driven through the real production entry point** (`gopi worker --once`, which
  fires the same `taskLifecyclePass`) in a sandbox — not a daemon restart.
  Counters to watch after the next restart:
  `grep -c '\[worktree-reap\]' ~/.gopi/logs/{worker,merger}.log` (0 today) and
  `grep -c escalated ~/.gopi/logs/merger.log`.
- **`/home/user/workspace/gopi-firstopen` is NOT escalated today**, and that is
  correct: it is DIRTY but a terminal ledger row *does* own it, so the arms above
  my rung already handle it (`report`). Once the sweep sees it with a direct path
  (after a restart) its dirt becomes a named operator action item in the logs.
- **The sibling residue worktree named in the briefing no longer exists** on the
  host (`ls ~/workspace/gopi-membase-wt-policy` → gone), so the orphan rule is
  demonstrated on a reconstructed fixture of the same shape (dirty tracked
  modification + no ledger row + aged registration) rather than that directory.
- **Pre-existing, unrelated failures** — see Verification; my branch's failure
  set is a strict subset of base-master's (`30450fc6`).
- **`internal/standards` gate failure `TestStandardGates/TestGate2TestEnvRedProof`**
  is RED at base-master too, in another agent's file; my change adds no new
  violation in any file it touches.

## Verification

### Build / vet
```
CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/   → OK
go build ./...                                    → OK
go vet ./cmd/agent/ ./internal/tools/             → clean
gofmt -l <touched files>                          → clean
```

### The regression test fails on the pre-fix tree and passes on the fix
`cmd/agent/worktree_reap_reach_test.go` drives the PRODUCTION entry point and
asserts the reap RAN *and ACTED* — existence is not reachability, which is
precisely the defect. Red-proof (reap leg removed from the tick, nothing else
changed):
```
--- FAIL: TestLifecyclePassInvokesWorktreeReap (0.04s)
    the lifecycle pass never ran the worktree reap (no `git worktree list` from the sweep)
      — serveWorktreeReapSweep is unreachable again
    output=[merger] [lifecycle] stale reported=0 disposed=0 sink=(no notify target configured)
--- FAIL: TestLifecycleTickInvokesWorktreeReap (0.03s)
    runTaskLifecycleTick did not reach the worktree reap; observed verbs=[]
FAIL	github.com/nick/gopi/cmd/agent	0.401s
```
With the leg restored: `ok github.com/nick/gopi/cmd/agent`.

New/updated tests:
- `TestLifecyclePassInvokesWorktreeReap` — the lifecycle pass runs the reap; the
  dead registration's admin dir is GONE afterwards (the leg acts, not prints).
- `TestLifecycleTickInvokesWorktreeReap` — the throttled tick entry point the
  daemons call reaches it too.
- `TestLifecyclePassEscalatesDirtyOrphanEndToEnd` — a real dirty linked worktree
  with no ledger row is escalated on BOTH surfaces (log line + notify channel)
  and **still exists afterwards**.
- `TestSweepEscalatesDirtyOrphanAndKeepsTheOwnedDirtyReport` — the rule at the
  engine level, with false-positive probes (terminal owner → report, live owner →
  keep, young orphan → report, unmeasurable age → report) and the untouched
  clean-unowned reclaim.
- `TestSweepCreatedAtUsesTheRegistrationRecord` — **now ages the MAIN repo's
  `commondir` to a year ago** and asserts the fresh worktree still reads as
  seconds old (the exact input of defect 3).
- `TestServeMergedCleanupSweepNoLongerReapsDeadRegistrations` — anti-regression
  on the carrier move (the retired carrier must not grow the reap back).

### Live evidence — real binary, real entry point, sandbox fixture (A/B/C)
`gopi worker --once` = the production `taskLifecyclePass`. Sandbox: private
`GOPI_HOME`/ledger/settings, repo + worktree root under the scratch tree; the
operator's repos are never touched. `/dev/shm/gopi-test-scratch/tmp/live_reap2.sh`.
Base binary = `/home/user/workspace/gopi-reap-basemaster` (detached at `30450fc6`).

```
A. BASE binary (30450fc6) + DEAD registration
   [worker] [lifecycle] stale reported=0 disposed=0        ← no reap leg exists
   after: /…/A/repo/.git/worktrees/wt-x  STILL PRESENT     ← the leak, reproduced

B. FIXED binary + same fixture
   [worker] [worktree-reap] reaped registration=…/B/root/wt-x (registration is dead
            (its directory is gone) — … branch feat/live-x deleted (git agreed it was safe))
   [worker] [worktree-reap] pruned: Removing worktrees/wt-x: gitdir file points to non-existent location
   [worker] [worktree-reap] worktree-reap: reaped=1 branches-reaped=1 reclaimed=0 escalated=0 repos=1
   after: no such file or directory                        ← collected

C. FIXED binary + DIRTY ownerless worktree (the orphan)
   [worker] [worktree-reap] escalated orphan=…/C/root/wt-x (ORPHAN WORKTREE: … (branch
            feat/live-x, age 72h) is DIRTY and NO ledger row owns it — … NOT removed
            (uncommitted work is kept, and this sweeper never uses --force) …)
   [worker] [worktree-reap] worktree-reap: reaped=0 branches-reaped=0 reclaimed=0 escalated=1 repos=1
   after: /…/C/root/wt-x  STILL EXISTS                      ← never --force
   notify.log: {"event":"worktree-orphan", … "summary":"ORPHAN WORKTREE: … branch=feat/live-x
                age=72h dirty, no ledger row — kept (never --force); a human must decide"}
```

### Operator host counters, before the fix
```
grep -c 'own worktree released'        ~/.gopi/logs/worker.log → 2
grep -c 'own worktree release refused' ~/.gopi/logs/worker.log → 242
grep -c 'worktree-reap'                ~/.gopi/logs/worker.log → 0
grep -c 'worktree-reap'                ~/.gopi/logs/merger.log → 0     ← never ran, ever
```
(0 reap lines in both logs is the defect stated as a number: the reap's own tests
were green throughout.)

### Full suites — branch tip (3 code commits) vs base
```
go test -count=1 ./cmd/agent/ ./internal/tools/ ./internal/standards/
```
Every `cmd/agent` failure in that run also fails at the pre-rebase base
`1cfe2f1f` (full-suite run, same binary): `TestCLIContractTaskServeAliasPrintsOneLinePointer`,
`TestTasksCLIListHeaderCountMatchesRows`, `TestDedupeActions_*`,
`TestStatusWithActingActionIsRefused/*`, `TestWorkerDaemon_*`, `TestMergerDaemon_*`,
`TestUserBusSeam_*`, `TestMergerFlock_*`, `TestTmpDirValueProbeLeavesNothingBehind`.
For `internal/tools` and `internal/standards` the comparison was run directly at
base-master `30450fc6`: `TestZZRedProof_DirtyDeadHolderWorkIsOrphaned`,
`TestZZRedProofFalseReclaimPromise`, `TestMemoryBaseKeepArmsOverrideTheNewRoot`,
`TestMemoryBaseOutOfPolicyRegistrationNamesTheRemedy`,
`TestDeadHolderHonestVerdict_RefusesTheReclaimableClaim` and
`TestStandardGates/TestGate2TestEnvRedProof` are all RED there too — other agents'
in-flight work in files this branch never touches.

**One flake is named honestly**: `TestTaskFindSessionJSONLStillHonoursEveryLayout`
fails in the full-suite run and **passes in isolation on BOTH trees** (verified
individually) — order-dependent, not attributable to this change, and not fixed
here.

**Zero new failures** from this branch in any file it touches.

## Incident during this task (reported, not hidden)

While establishing the live before/after evidence I planted three probe worktrees
under `~/workspace` (detached, clean) and ran the full `cmd/agent` suite. **All
three were deleted by that suite run** — twice, on two separate runs.

Root cause, from the suite's own log:
```
worker: own-worktree-release: own registered worktree freed: /home/user/workspace/gopi-v1-clean
   (a worktree of this run's repo that no ledger row owned; branch detached untouched)
```
`TestWorkerWaitsAndPicksUpNewTaskWithoutRestart` spawns the REAL binary as a
worker with a sandbox `GOPI_HOME`/ledger but a CWD inside the live repo; the
**pre-existing producer pass** `ReclaimOwnFinishedWorktrees` then frees any
worktree of that repo that is clean, named by no row of the *sandbox* ledger, and
older than its 5-minute floor. The dirty siblings on the host were correctly kept.

Two things follow, both recorded:

1. **It is not this branch's reap.** A/B: the full suite was re-run with
   `worktreeReapDryRun = true` (my leg doing nothing) and the worktrees were still
   deleted — the deleter is the own-release pass, which existed before this task.
2. **My own leg had the same class of exposure indirectly**, which is why the
   lifecycle tests were fixed here: they fired a pass (hence the sweep) from the
   suite's working directory — the live repository — so the sweep was handed the
   live host. They now pin an empty repos table + a sandbox CWD
   (`zwPinSweepToSandbox`), and the reap itself is dry-run by default in its
   public verb.

The remaining exposure belongs to the test that spawns a worker
(`worker_waits_e2e_test.go`), which this branch does not own: **dispatched as
bug task `01a0a80b-7f3e-79f6-ab4d-db74f0728d37`** with the mechanism, the log
lines, the A/B proof and the acceptance (a planted clean worktree older than 5
minutes must survive `go test ./cmd/agent/`).

## Files

| File | Change |
|---|---|
| `cmd/agent/task_lifecycle_tick.go` | new leg: the reap rides the lifecycle pass (`worktreeReapDryRun`, `worktreeReapLegLabel`) |
| `cmd/agent/cli_tasks.go` | dead carrier dropped; `serveWorktreeReapSweep(dryRun)` + escalation line + `escalated` in the summary |
| `internal/tools/worktree_sweep.go` | `worktreeEscalateOrphan` rung, `Escalated` counter, notify + once-per-path memo, corrected age record, `InstallWorktreeSweepObserver` |
| `internal/tools/tasknotify.go` | `worktree-orphan` added to the `"all"` event set |
| `cmd/agent/worktree_reap_reach_test.go` | NEW — reachability proof (pass + tick) and end-to-end escalation |
| `cmd/agent/cli_tasks_merged_cleanup_test.go` | carrier-move anti-regression (replaces the green-while-dead wiring test) |
| `internal/tools/worktree_sweep_test.go` | orphan escalation, corrected age fixture + defect-3 regression |
| `internal/tools/worktree_refusal_arms_test.go` | fixtures age the registration's own record |
