<!-- doc-class: historical -->

> **RETIRED 2026-10-26 — this page describes machinery that NO LONGER EXISTS.**
> `gopi task sweep-zombies` and the automatic zombie disposer were deleted whole; a stuck
> row is now REPORTED and left exactly where it is. Read this page as history, not as
> instructions: any command below that names `sweep-zombies` exits 1 with a pointer.
> The current behaviour is `devdoc/zombie-sweep-removed.md`.

# Task Report: zombie AUTO-DISPOSAL — the worker disposes CONFIRMED zombies behind a second grace

> **WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md** — the operator owns this; an agent must never run it (submit the work instead: `gopi task submit`).

**Branch**: `fix/zombie-auto-dispose` (worktree `~/workspace/gopi-zombie-dispose`)
**Base**: master `c0211ad4`
**Date**: 2026-09-12

## Completed

### 1. The defect, measured (not asserted)

The ledger had a zombie **detector with no automatic disposer**. The worker's
lifecycle pass (`cmd/agent/cli_tasks_worker.go` `workerLifecyclePasses` →
`serveStaleSweep` → `agent.DetectStaleTasks` → `tools.DetectStaleTasks`,
`internal/tools/taskheartbeat.go:141`) only fired a `stale` notify event and
bumped `updated_at` (`taskZombieBumpSQL`) to re-arm the dedup. Rows at the
**strongest** verdict — `confidence=confirmed`, i.e. the claimed pid is dead
(`internal/tools/taskzombie.go` `zombieVerdict`) — therefore sat
non-terminal for days. The only disposer was the MANUAL verb
`gopi task sweep-zombies --kill` (`cmd/agent/cli_tasks.go:3114` →
`internal/tools/taskzombie_sweep.go` `SweepZombies`), which no tick calls.

Repro on the operator sink `~/.gopi/notify/task-events.jsonl` (one physical
line, 47.6 MB — split on `}{`, `json.loads` each fragment), streaming:

```
sink bytes=47615920  stale_events=1331  confirmed_rows=368  suspected_rows=8
re-report spread (events per confirmed row): max=31
```

**368 distinct rows at `confidence=confirmed`** — one of them re-reported 31
times (the ~30 min stall window, for a day+). The epic's ~352 rows is
reproduced and slightly exceeded.

### 2. A second, longer dispose grace, resolved in ONE place

`internal/config/config.go`:

- `TaskDisposeSecEnv = "GOPI_TASK_DISPOSE_SEC"` — read in exactly one place,
  `resolveGraceInt` inside `ResolveTaskGracePeriods` (Rule 2 + Rule 21: the
  constant has a real reader).
- `TaskDisposeSecSettingsKey = "task_dispose_sec"` + `Config.TaskDisposeSec`.
- `TaskDisposeGraceMultiplier = 2` — the default is **derived**, never a
  second magic 3600: `default = TaskDisposeGraceMultiplier × the RESOLVED
  stall grace`. Precedence per knob: env > settings > default.
- `TaskGracePeriods.DisposeSec`, `0 = auto-disposal off`. When the stall grace
  is 0 (detection disabled) the derived dispose grace is 0 too — no confirmed
  verdict can ever exist, so a derived grace would be meaningless.
- `internal/config/settings.go` catalog entry (empty CLIFlag column — the
  knob is worker-pass policy, not a per-invocation flag).

The sweep itself still never reads env/settings: the caller resolves.

### 3. Auto-disposition wired into the worker lifecycle pass

`cmd/agent/cli_tasks_worker.go workerLifecyclePasses` now:

1. reports (`serveStaleSweep`, unchanged detection half);
2. **disposes**: `autoDisposeConfirmedZombies(grace.DisposeSec)` calls the
   EXISTING disposer `tools.SweepZombies{SweepZombiesOptions{StallSec:
   disposeSec, Kill: true, Notify: &false, IgnoreRearm: true}}` — the same
   entry point the manual verb drives, the same `zombieVerdict` chain, **no
   new evidence rule and no new daemon**;
3. prints ONE operator line, so the write-only sink has a consumer surface:

```
[worker] [lifecycle] stale reported=2 disposed=1 sink=cat >> /home/user/.gopi/notify/task-events.jsonl
```

Each disposal also prints its own evidence line
(`[worker] [zombie-dispose] <id> disposed (confirmed, stalled) — heartbeat
stop + claimed pid N dead (host) — confirmed zombie`). A failed disposal is a
loud stderr line — never swallowed (Rule 15); a malformed grace config
degrades to "auto-disposal skipped this pass" with the report half intact.

### 4. The starvation bug found during implementation (and why it matters)

The **reported** invariant would have been satisfied while the feature did
nothing. The report pass bumps `updated_at` every stall window (that bump IS
its notify dedup), and the sweep's candidate query carries a re-arm guard
`updated_at < now − grace`. With a dispose grace **longer** than the stall
grace, a reported row's `updated_at` never falls further behind than one
stall window → **no confirmed zombie would ever reach the longer grace**.
Measured: disabling `IgnoreRearm` leaves the seeded confirmed zombie
`in_progress` after a lifecycle pass.

Fix: `SweepZombiesOptions.IgnoreRearm` drops the re-arm guard from the
disposal leg only — the guard is NOTIFY dedup, not evidence, so the verdict
chain is untouched. The disposal leg also passes `Notify=false`: it is the
disposer, not the reporter (the report pass already emitted the operator's
`stale` event, and the row's own `failed` transition notifies normally), so
no `updated_at` rewrite happens on that path. The manual CLI sweep keeps the
guard unchanged.

### 5. Disposal safety (the critical invariant — unchanged, and re-proved)

Enforced by the existing predicate, not by new code:

- a **live claimed pid vetoes disposal outright** (the suspended-host case);
- a **`suspected` row is never killed** (the evidence chain did not confirm
  death — including the foreign-host / no-claim-stamp cases);
- a confirmed row **inside** the dispose grace is reported and left
  `in_progress`.

## Not Done

- **No CLI flag for the dispose grace.** Auto-disposal is a worker-pass
  policy, so it resolves from env/settings only; `--stall-sec`/`--max-run`
  remain the manual verb's flags. Adding `--dispose-sec` would advertise a
  knob the worker cannot take from argv.
- **No change to the evidence chain.** The epic explicitly forbade new
  evidence rules; `zombieVerdict` is untouched.
- **`internal/stylegate` does not pass** — pre-existing (see Verification).
- The 368 already-zombied rows in the live ledger were deliberately NOT
  disposed by this task: they are the operator's data and the acceptance run
  was sandboxed. Deploying this binary disposes them on the first worker
  lifecycle pass (they are all past the dispose grace by construction).

## Verification

**Build**: `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → OK.

**Tests** (all `-count=1`):

| Suite | Result |
|---|---|
| `go test ./cmd/agent/` | `ok … 92.7s` |
| `go test ./internal/tools/` | `ok` |
| `go test ./internal/config/` | `ok` |
| `go test ./internal/standards/` | `ok` (gates 16–21 green) |
| `go test ./internal/stylegate/` | **FAIL — pre-existing**: `git stash -u` (my whole diff) on the same worktree still yields the identical failures (ignored-error 186>182, test-sleep 3>0, test-skip 1>0), and master `~/work/GoPi` fails them too. My working-tree sites added by my diff: **0** (proven with `git stash -u` A/B). |

New tests:

- `internal/config` `TestResolveGracePeriods_DisposeGrace` — derived default,
  follow-the-tuned-stall, env override, settings fallback, explicit 0 off,
  stall-0 ⇒ dispose-0, negative ⇒ error.
- `internal/tools` `TestSweepZombies_IgnoreRearmDisposesPastLongerGrace` —
  pins BOTH directions: with the guard the longer grace sees nothing
  (the starvation), without it the confirmed zombie is disposed.
- `cmd/agent`:
  - `TestWorkerLifecyclePass_AutoDisposesConfirmedZombie` — ONE lifecycle
    pass, no manual verb: confirmed → `failed` (+ zombie audit reason in
    `task_events.detail`), `suspected` and live-pid stay `in_progress`,
    summary shows `stale reported=2 disposed=1` and names the sink, the
    `stale` event reaches the sandbox sink.
  - `TestWorkerLifecyclePass_ReportsButDoesNotDisposeInsideDisposeGrace` —
    heartbeat past the stall grace, inside the dispose grace → disposition 0,
    all three rows `in_progress`.
  - `TestWorkerLifecyclePass_DisposeDisabledWhenGraceZero`.

**Red proof** (the tests are not vacuous):

- `if false && grace.DisposeSec > 0` (disposal call disabled) →
  `TestWorkerLifecyclePass_AutoDisposesConfirmedZombie` FAILS:
  `confirmed zombie status = "in_progress", want "failed"`.
- `IgnoreRearm: false` (disposal leg) → same test FAILS.

**Real-binary acceptance** — `/tmp/zombie_dispose_acceptance.sh`, run against
`~/workspace/gopi-zombie-dispose/bin/gopi` (absolute path — `testutil.GopiBin`
SKIPS silently on a missing binary, so an acceptance run against the wrong
tree proves nothing). Each phase uses its own sandbox home + schema-only
ledger clone + sandbox notify sink, then drives the REAL
`gopi worker --once` lifecycle pass:

```
== phase: past the dispose grace (heartbeat age 7200s) ==
  ledger: t-acc-conf=failed t-acc-live=in_progress t-acc-susp=in_progress
  PASS confirmed status = failed
  PASS suspected status (never auto-disposed) = in_progress
  PASS live-pid status (always vetoed) = in_progress
  PASS lifecycle summary counts          (disposed=1)
  PASS lifecycle summary names the sink
  PASS stale report delivered to the sandbox sink
  PASS disposal logged with evidence
  PASS zombie audit reason recorded
  summary line: [worker] [lifecycle] stale reported=2 disposed=1 sink=cat >> …/notify.log
== phase: inside the dispose grace (heartbeat age 600s, stall 300 / dispose 1200) ==
  PASS confirmed status = in_progress
  PASS suspected status (never auto-disposed) = in_progress
  PASS live-pid status (always vetoed) = in_progress
  PASS lifecycle summary counts          (disposed=0)
  PASS stale report delivered to the sandbox sink
  summary line: [worker] [lifecycle] stale reported=2 disposed=0 sink=cat >> …/notify.log
ACCEPTANCE PASS
```

**Operator sink isolation**: the live `~/.gopi/notify/task-events.jsonl` is
appended by OTHER sessions concurrently, so byte-identity is only observable
when none of them writes at that moment. Both cases were measured against
this harness: one run grew the sink by 33–47 KB with **none of the sandbox
task ids** (`t-acc-*`) in the appended region, and the final run (no
concurrent writer) came out **byte-identical**
(`7a127b24…`, 47636385 bytes). An in-process test asserting the sink's bytes
is therefore unsound; every test here points the notify target at a temp file
instead, and the harness asserts the honest, falsifiable property — "the
region this run appended contains none of our task ids".

**Commit**: `c75975a1` — `feat(zombie): the worker disposes CONFIRMED zombies
behind a second grace` (8 files, added one by one per Rule 8).

## Deliverable declaration

- **Branch**: `fix/zombie-auto-dispose` — the commit is in the SHARED repo
  (linked worktree, no push needed): `git log --oneline -1` in
  `~/workspace/gopi-zombie-dispose` shows `c75975a1`.
- **Recovery**: `git show fix/zombie-auto-dispose:devdoc/zombie-auto-dispose-2026-09-12.md`
  (from `~/work/GoPi`), and `git log --oneline master..fix/zombie-auto-dispose`.
- **Worktree**: `~/workspace/gopi-zombie-dispose` — left in place for review;
  the merger/PA owns removal after merge (Rule 6: I do not merge my own
  branch).
