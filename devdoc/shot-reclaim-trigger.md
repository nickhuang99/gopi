<!-- doc-class: historical -->
# The shot-unit reclamation's triggers — reachable on an idle host

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

**Epic:** shot-reclaim-trigger · **Commit:** `0b7924bb` (the branch this run's worktree
holds; the framework reports its name) ·
**Predecessors:** `7c8fe9f8` (the registry + the sweep), `devdoc/shot-unit-lifetime-registry.md`.

## The defect, precisely

The reclamation was correct and unreachable. `internal/scratch.ReclaimShotUnits`
(through `internal/shot.ReclaimOrphanedUnits`) stops the transient
`gopi-shot-*.service` units whose CREATING PROCESS IS MEASURED GONE — the units a
SIGKILLed capture leaves behind, because a `defer proc.stop()` does not run for a
process that was killed. Two carriers were wired, and on a quiet host neither one
fires:

| carrier | why it did not fire |
|---|---|
| the worker/merger tick (`cmd/agent/scratch_gc_tick.go:133`) | reached only while a worker ROUND is in flight; a worker self-exits after a drained queue (`workerDefaultIdleExits × workerIdleIntervalDefault` ≈ 90 s), and leaks happen exactly when the queue is empty |
| `gopi scratch-gc` (`cmd/agent/cli_scratch_gc.go`) | correct and one command away — but the host's cron line and systemd user timer were deleted deliberately on 2026-09-22 ("why do we need this kind of after-the-fact supervisory cleanup?") |

**A leak that happens when the machine is idle was reclaimed only by a mechanism
that runs only when the machine is busy.** Measured 2026-09-27 07:58: six ACTIVE
`gopi-shot-*` units, oldest ~1h50m, zero chrome reclaimed, six registrations on
disk.

## The fix: ride events that NECESSARILY happen

No daemon, no timer, no cron line, no script, no config key, no schedule
(Rule 35 — re-adding the removed scheduler under a new name is the one move this
epic exists to forbid). Both new triggers are in-process calls at event points
that already exist:

| trigger | event point | why it always happens |
|---|---|---|
| `shot-startup` | `cmd/agent/cli_shot.go:335` — `shotStartupRenewal`, in `runShotSubcommand` AFTER the argv is parsed and BEFORE the capture | `gopi shot` is the only thing that creates these units; every capture is therefore an event on the very host where the leak accumulates |
| `worker-startup` | `cmd/agent/cli_tasks_worker.go:449` — `shot.RenewNotice`, in `runTaskWorkerLoop` after the banner, BEFORE the first claim, guarded `if !once` | a worker is started whenever work is dispatched, so it fires on any host that ever runs tasks — and unlike the tick it does not depend on a round being in flight |

The pre-existing tick call site is **untouched** (`scratch_gc_tick.go:133`): a
live worker still sweeps opportunistically, and this change adds triggers rather
than moving one. Nothing self-schedules; both triggers die with the process that
fired them.

### Why start-up, not exit

The leak is caused by a capture that never reached its teardown, so the durable
event is a capture STARTING, not finishing. A start-up sweep also cannot be
skipped by the failure it is meant to clean up after. It is safe with respect to
the capture that fires it either way: the predicate only stops units whose
creator is MEASURED gone, and the capturing process is alive for the whole run.
This direction is unaffected by what happens later — a `gopi shot` that is itself
SIGKILLed still reclaimed the earlier leaks on the way in.

### The scope of each trigger

- **`gopi shot`**: only the WEB surface sweeps. The text and screen surfaces
  launch no chrome unit at all, so a sweep for them would be cost with zero
  possible benefit. The choice is driven by the same `shot.NormalizeSurface` the
  capture itself uses, so a new surface cannot silently default to "no sweep".
- **`gopi worker`**: the LOOP only (`if !once`), the same guard its sibling ticks
  already carry. `--once` is the hermetic unit/E2E seam (Rule 6: a test worker
  must not sweep real host state), and a NAMED row (`gopi worker <task-id>`) is
  addressed work that must not spend its budget on host hygiene. The loop is the
  face a host runs, foreground or `--daemon`; both enter it.
  - This guard is not decoration: the first version of this change omitted it and
    `TestWorkerOnceFaceKeepsTheStartupBoundary` failed RED — `--once` had already
    swept a fixture unit.

## Denied vs kept: the report had to stop hiding a failure

`ReclaimShotUnits` used to fold a FAILED stop into `Kept` with a suffix on the
reason ("the stop failed, registration kept for the next sweep"). That is a
Rule 15 defect in a report: a unit the host REFUSED to stop is still ACTIVE, and
it read exactly like a policy keep ("the creator is alive"). The outcome now
carries a third number:

```
shot-unit-reclaim role=shot-startup scanned=2 renewed=1 denied=0 kept=1
shot-unit-reclaim role=shot-startup renewed-unit=gopi-shot-e2e-green-shot-leak.service
shot-unit-reclaim role=shot-startup kept-unit=gopi-shot-e2e-green-shot-live.service verdict=creator-live reason="creator pid 2518565 is still running"
```

- `renewed` — the arm acted and the unit is gone (its registration removed).
- `denied` — the unit was reclaimable and `systemctl` refused; it is STILL ACTIVE,
  its registration is kept for the next pass, and the caller also gets a warning
  line saying so. A denial is a separate count and a separate per-unit line
  (`denied-unit=`), never a keep.
- `kept` — the policy refused (creator live/unmeasurable, or an unreadable
  registration).

`renewed` replaced the older `stopped` word: `gopi shot` now prints this report
on start-up, and "stopped" there reads like a failure. `renewed` says what
happened — the previous capture's unit was taken down and the capture path is
clean again.

**A registry the process cannot write at OPEN time** (a different user ran the
captures; a scratch home that is not writable) is now announced on the ways into
the sweep — `gopi worker` and `gopi shot` each probe it once and print one
sentence naming the path, the reason and the channel, instead of failing per
registration. Neither trigger changes what it does about it: the reclaim policy
is untouched, and a sweep that cannot read a record keeps that unit and names it.

## Visibility in `scratch-gc` (acceptance 4)

The measured defect: the verb stopped six `gopi-shot-*` units and printed
`removed=4` — where the four were scratch QUEUE DIRECTORIES
(`queue-runtime-job-*`, `-merge-resolve-*`, …). Not one line named a stopped
unit, so the capture defence was invisible in its own output.

The capture arm now has its own count line, printed on the zero path too (a
wired-but-quiet defence must be distinguishable from one that is not wired):

```
scratch-gc role=cli home=… removed=0 stale=6h0m0s disabled=false
shot-unit-reclaim role=cli scanned=2 renewed=1 denied=0 kept=1
shot-unit-reclaim role=cli renewed-unit=gopi-shot-e2e-green-gc-leak.service
shot-unit-reclaim role=cli kept-unit=gopi-shot-e2e-green-live.service verdict=creator-live reason="creator pid … is still running"
scratch-gc role=cli shot-units-renewed=1
```

`removed=` stays the directory count; `shot-units-renewed=` is the capture
count. Two arms, two numbers, never one conflated total.

## Enumerated callers (bug-report §Evidence 4)

| entry point | callers | fires unconditionally today? |
|---|---|---|
| `ReclaimOrphanedUnits(procRoot)` | `internal/shot.RenewNotice` (only) | yes — via RenewNotice |
| `ReclaimOrphanedUnitsReporting(role)` | `cmd/agent/cli_scratch_gc.go:505` (the CLI face), `cmd/agent/scratch_gc_tick.go:133` (the resident tick) | CLI: only when invoked; tick: only during a worker round |
| `PlanOrphanedUnits(procRoot)` | `cmd/agent/cli_scratch_gc.go:680` (`--dry-run` only) | no — a report face |
| `RenewNotice(role)` (new) | `cmd/agent/cli_shot.go:335` (shot start-up), `cmd/agent/cli_tasks_worker.go:449` (worker start-up, `!once`) | **yes, at each verb's start** |
| `shotStartupRenewal(opts)` (new, unexported) | `runShotSubcommand` | yes, for the web surface |

The enumeration covers function-VALUE references as well as calls: the only file
importing `internal/shot` outside `cmd/agent` is `internal/tools/screencapture.go`,
which uses the X11 capture path and touches none of these symbols
(`grep -rn "shot\.\(Renew\|Reclaim\|Plan\)"` for aliased forms returns nothing).

## Measured cost

One `os.ReadDir` plus one liveness read per registration, and a
`systemctl --user stop` only for a measured-dead creator. Measured on this host
with two registrations (one reclaimable, one live control):

| trigger | elapsed, whole invocation |
|---|---|
| `gopi shot --self --addr 127.0.0.1:1` (start-up sweep + the unreachable-address refusal) | 63 ms (RED binary, same argv, no sweep: 6.5 ms) |
| `gopi worker` (start-up sweep + one loop round) | 2.679 s (bounded by the fixture's `--max-rounds 1` / idle 1 s) |
| `gopi scratch-gc` (the pre-existing face) | 49 ms |

With an EMPTY registry the sweep returns before doing any work (an absent
directory is "nothing to do", not an error), so a fresh host pays a `ReadDir` on
a missing path and nothing else. With N registrations it is N liveness reads; the
expensive part is the host call, and it happens only for a dead creator.

## What this change deliberately does NOT do

- **No scheduler of any kind.** No unit, timer, cron line, script, goroutine tick
  or config key. `gopi scratch-gc` remains a foreground verb the operator runs;
  nothing runs it for them.
- **No policy change.** `ReclaimShotUnits`' predicate and its conservative
  direction (live ⇒ keep, unmeasurable ⇒ keep, no age-based sweep) are untouched.
  A registration whose creator cannot be measured still keeps its unit forever —
  an accepted residual risk, named in the predecessor devdoc.
- **No new tool** (Rule 3) and no change to `make fmt`.
- **No wait and no retry inside a capture's critical path**: the trigger runs
  before the capture starts and never fails the capture (fail-open, with the
  failure named on stderr).
- **`--once` and named-row workers do not sweep** — the deliberate boundary
  documented above, pinned by `TestWorkerOnceFaceKeepsTheStartupBoundary`.

## Residual gaps (honest)

1. **`gopi shot --help` was not updated.** The verb's help surface is
   exact-matched by `scripts/g6-tools-maintenance-help-surface.sh` and the CLI
   contract suites, so a reworded usage needs its own change with that script in
   step (the same gap its predecessor recorded).
2. **A host that runs neither `gopi shot` nor `gopi worker` still reclaims
   nothing automatically.** That is the operator's explicit ruling: no schedule.
   Such a host has the CLI verb; it does not have a leak source either.
3. **A `gopi shot` run that is SIGKILLed between its start-up sweep and its own
   unit launch is still a leak** — but the NEXT capture reclaims it, which is the
   whole point of the trigger placement.
4. **A denial is only retried at the next event** (the next capture or worker
   start). There is no backoff loop by design: a retry loop is a schedule.
