<!-- doc-class: historical -->
# Task Report: `gopi task list` — the stale sweep is off the read path

> **RETIRED MECHANISM — the subject of this page no longer exists (2026-11-05).**
> This page records a 2026-09 fix that took the stale sweep OFF the read path and
> made it an explicit `--sweep` request. The whole sweep is now DELETED: the
> detector (`tools.DetectStaleTasks` / `agent.DetectStaleTasks`), the `--sweep`
> flag and its parse arm, the persisted throttle stamp
> (`cmd/agent/tasklistsweep_gate.go`), the re-arm UPDATE and the `task run` /
> `session resume` prologues. It is a record of a decision at a time, not a
> description of the tree — `gopi task list` still accepts no `--sweep`, but its
> body is now a pure read BY CONSTRUCTION rather than because a throttle says so.
> The operator's ruling that ended it: no process watches another process; each
> process closes itself out, and a stale row is READ as stale (`gopi task show`
> resolves the holder's identity per row against `/proc`).

Branch: `fix/task-list-sweep-readpath` (worktree `/home/user/workspace/gopi-listsweep`)

Epic: `gopi task list` (the plain read verb) synchronously ran the stale/zombie
sweep BEFORE rendering, paid for it on every look, and paid it in WRITES.

## Completed

### 1. The measurement (done BEFORE choosing the shape)

Two ledgers, two binaries. `bin/gopi-prefix` = master + the three accidental
`0oNNN` mode fixups (byte-equivalent to master semantics in this path);
`bin/gopi-nosweep` = the identical source with `taskListSweepThrottleMin = 0`
(the package's own documented off switch) — the cleanest A/B available.

**A. Sandbox ledger, one zombie `in_progress` row (dead claim pid, 2h
backdated), min of 7 runs, in-process harness (temporary, deleted before
commit):**

| leg | cost |
|---|---|
| open ledger + schema ensure (`OpenTaskHeartbeatConn`) | 3.6 ms |
| stale query, NO stale row (= the healthy case) | 3.9 ms |
| sweep, 1 zombie, blocking notify cmd | **45.0 ms** |
| sweep, 1 zombie, notify target unset | 8.6 ms |
| sweep, 1 zombie, async notify | 24.8 ms |
| **the RENDER alone** (`TaskList(in_progress)`) | **3.7 ms** |
| the render, all statuses | 4.1 ms |

Breakdown of the 45 ms: ~4 ms ledger open + stale query, ~36 ms the notify
delivery (a real configured `notify.cmd` is a forked process), ~4 ms
verdict + re-arm UPDATE. **The sweep costs ~12x the render it precedes.**

**B. Real process boundary (two binaries, identical sandbox ledger, `task
submit`-seeded so the schema is production's):**

| binary | 3x `task list --status in_progress` | ledger effect |
|---|---|---|
| sweep disabled (`throttle=0`) | 0.06 / 0.06 / 0.07 s | 0 events, sink untouched |
| sweep on (prefix) | 0.14 / 0.13 / 0.16 s | a `"event":"stale"` line per read, `updated_at` bumped per read |

Per read: **+0.07-0.09 s, +1 ledger UPDATE, +1 notify event.** Repeated reads
keep paying — the row is re-armed (`updated_at` bumped) and re-fires as soon as
the window allows, *and* the read's own `[died …]` rendering drifts
(`2h0m0s` → `2h0m1s`) because the row's clock moved under it.

**C. Does the throttle bounded the cost? No — it can never fire.**
`taskListSweepLastTick` is a *process* variable; every CLI invocation is a
fresh process, so it is always zero. Confirmed: the 5 back-to-back prefix reads
in (B) each delivered and each bumped. A dead process variable is not a
throttle.

**D. The healthy case is not free either:** every read pays the ledger
open + schema-ensure + stale query (~4 ms, measured) even when nothing is
stale, plus the churn of a second `database/sql` handle.

**E. A realistic ledger does not amplify it.** On a copy of the live ledger
(1268 rows, 5 `in_progress`), with **0 stale candidates**, the sweep's extra
cost was inside the noise of process startup — the read-path cost is dominated
by per-zombie work, not ledger size.

### 2. The decoupling

- **A plain `gopi task list` is now a PURE READ.** `runTaskList` (new, named —
  `cmd/agent/cli_tasks.go`) runs `maybeTaskListSweep()` **only** when
  `--sweep` was asked for. No sweep, no ledger UPDATE, no notify, no verdict.
- **The explicit trigger is `gopi task list --sweep`** (new flag; added to
  `taskFlagMember` and to the `tasksUsage` head). It sweeps *then* renders.
- **The HP 2026-09-02 intent is preserved, not deleted**: a killed one-shot
  runner's row still surfaces on a daemon-less node — through the deliberate
  trigger above, and (as before) through the resident worker/merger lifecycle
  tick (`task_lifecycle_tick.go` → `serveStaleSweep`).
- **The throttle moved from a dead process variable to a PERSISTED, LEDGER-SIDE
  stamp** — `cmd/agent/tasklistsweep_gate.go` (`<gopi-dir>/task-list-sweep.stamp`,
  RFC3339 UTC, `taskListSweepThrottleMin` window). Two hundred shell
  invocations agree on one clock because the clock is on disk. This is the
  crash-dump gate's proven marker shape (`crashdump_sweep_gate.go`).
  - Fail-open: a missing / unreadable / unparsable / future-dated stamp reads
    as DUE — a bookkeeping file's failure mode is "sweep", never "hide a stale
    row".
  - Rule 15 both ways: a refused `--sweep` prints *why* (`already swept at
    <t> (window 10m0s)`), and a sweep-stamp write failure is reported (the cost
    of losing it is one extra sweep, never a missed row).
- **Re-arm idempotence is intact wherever the sweep runs**: `DetectStaleTasks`
  still bumps `updated_at` after a notify, and the stamp is written **only
  after a completed sweep** (a stamp is a record of work, never a promise of it
  — the crash-dump gate's contract).
- The `--sweep` render path is byte-identical to the plain one: it sweeps
  first, then calls the same `runTaskList`.

### 3. Regression tests

`cmd/agent/cli_tasks_list_sweep_readpath_test.go` (in-process) and
`cmd/agent/cli_tasks_list_sweep_test.go` (E2E, real process boundary).

- `TestTaskListReadPathSweepsNothing` — acceptance (a)+(b): with the zombie
  PRESENT, a plain read runs **0 sweeps** (a counter seam on `staleSweep`, not
  a wall-clock threshold), leaves `updated_at` and `task_events` unchanged, and
  leaves the notify sink empty.
- `TestTaskListSweepFlagStillFiresStaleEvent` — acceptance (c): the explicit
  trigger on a daemon-less sandbox still delivers the `"stale"` event naming
  the zombie, **and** the re-arm bump fires, **and** the stamp is persisted.
- `TestTaskListSweepStampThrottlesAcrossProcesses` — the throttle now crosses a
  process boundary: a second `--sweep` in the window is refused *with a reason*,
  writes nothing, and the window yields exactly ONE stale event.
- `TestTaskListReadPathE2E` — the same three properties measured at the **real
  process boundary** (three invocations of the built binary), because that is
  the only honest way to test a cross-invocation property — the pre-fix
  throttle lived in a variable an in-process test can always satisfy.
- Cost properties only: counters and ledger byte comparisons. No test can fail
  because a CI box is slow.

**RED PROOF (the pre-fix throttle cannot pass).** `TestTaskListReadPathE2E`
against `bin/gopi-prefix` (master + the mode fixups):

```
plain `task list` must not UPDATE the ledger (updated_at 1.789379947e+09 → 1.789387147e+09)
plain `task list` must fire no notify event, sink has:
  {"event":"stale", ... "task_id":"t-readpath-zombie", ... "pid_dead":true ...}
task list --sweep exit=1 stderr=gopi task list: unknown flag --sweep
```

It fails on all three legs on master and passes on this branch — i.e. the test
detects the defect, it is not a tautology.

### 4. Independent re-measurement of this branch, at the process boundary

Re-run from scratch on the committed tree (`93bbf5d3`, `bin/gopi` rebuilt from
it), a 40-row sandbox ledger with ONE stale `in_progress` row (dead claim pid,
2h backdated) and a real notify cmd. The row is **re-backdated before every
timed read**, so every read meets the same stale row and the only variable is
whether the sweep is on the read path. Three binaries from the same tree:
pre-fix = master mirror (`bin/gopi-prefix`), post-fix = this branch
(`bin/gopi`), and the pure floor (`bin/gopi-nosweep`, the same source with
`taskListSweepThrottleMin = 0` — it renders, and runs no sweep at all).

| binary | min-of-7 read | ledger UPDATEs in 7 reads | stale events in 7 reads |
|---|---|---|---|
| PRE-FIX (sweep on the path) | 23.68 ms | **7 / 7** | 1 |
| POST-FIX (this branch) | 11.19 ms | **0 / 7** | **0** |
| NOSWEEP floor (no sweep anywhere) | 8.43 ms | 0 / 7 | 0 |

Reading: the read path no longer carries the sweep (11.19 ms is the render plus
the always-on per-row sessions-tree walk of the sibling branch — the floor is
8.43 ms), it performs **zero** ledger UPDATEs, and it fires **no** event. The
one stale event the pre-fix column shows is the pre-fix read delivering it;
post-fix the same 7 reads deliver none. (min-of-N because this host is loaded;
the *write* counts are exact, not sampled.)

Acceptance (b) as a byte-level proof, same sandbox, zombie present:

```
post-fix 20 plain reads: updated_at 1789388134.0 -> 1789388134.0
                         md5 0aa51a7a1b6274741feac118932df3ef -> (same)
** LEDGER BYTE-IDENTICAL ACROSS 20 PLAIN READS **
pre-fix  plain reads   : updated_at 1789380932.0 -> 1789388134.0  (MOVED), sink 509 B
```

Acceptance (c), daemon-less, same sandbox (`task list --sweep`):

```
[stale] reported 1 stale in_progress row(s) via task-notify (env/settings)
updated_at 1789380892.0 -> 1789388092.0   (the re-arm bump)
stale events delivered: 1   names the zombie: 1
stamp written: .gopi/task-list-sweep.stamp = 2026-09-14T12:14:52Z
second --sweep (a NEW process): "[stale] already swept at 2026-09-14T12:14:52Z
(window 10m0s) — nothing swept (`gopi task list --sweep` is the trigger)"
  → updated_at unchanged, still exactly 1 stale event in the sink
```

The stamp lives under the gopi data root the sandbox GOPI_HOME pins, so a
refusal is disk evidence (`.gopi/task-list-sweep.stamp`), never a leftover
process variable. Pre-fix, the same three legs: `updated_at` moved on the plain
read, the sink got the event, and `--sweep` exited 1 with
`unknown flag --sweep`.

### 5. Notifications/observability

`--sweep` prints its verdicts on stdout (`[stale] reported N …` /
`[stale] already swept at …`), so an operator (or a cron line) can tell a real
sweep from a refused one from a no-op.

## Not Done

- **Not changed:** the render path itself, and the ledger/output format. The
  render is byte-identical (proven in the acceptance run below and pinned by the
  E2E test's row-comparison half).
- **Not changed:** `internal/tools/DetectStaleTasks` — it still bumps
  `updated_at` after a notify (the re-arm), as required.
- **Not removed:** the read-side sweep code. `maybeTaskListSweep`,
  `taskListSweepThrottleMin` and `staleSweep` are all still live — they are now
  reached only through the explicit trigger. (A future cleanup could re-home it
  under `sweep-zombies`; not done here because the briefing asked for the sweep
  off the read path with the notify still reachable, and moving the verb would
  have widened the blast radius to the help/completion gates.)
- **Not done:** the sibling cause (`fix/tasklist-inprogress-scan`, the per-row
  sessions-tree walk on `--status in_progress`). That branch is untouched; this
  branch changes only the sweep.
- **Not fixed (out of scope, observed):** the render's own relative-time drift
  — the `[died 2h0m0s since …]` field is computed per invocation, so two reads
  a second apart differ by a second. That is the render path, which the epic
  requires to stay byte-identical; it is recorded here, not changed.

## Verification

- Build: `CGO_ENABLED=0 go build ./...` → OK.
- `go vet ./cmd/agent/` → clean.
- `go test ./cmd/agent/ ./internal/tools/ ./internal/agent/ ./internal/standards/`:
  `cmd/agent` FAIL, `internal/tools` ok, `internal/agent` ok,
  `internal/standards` ok. The ONE failure is
  `TestWorkerDaemon_StatusReportsTheLiveDaemon` — **pre-existing and unrelated**
  (it fails identically on a clean master worktree at `16f871c3`, verified in
  `/dev/shm/gopi-test-scratch/ab-master`). Re-run with only that test skipped:
  `ok cmd/agent 234.4s`, `ok internal/tools 170.6s`, `ok internal/agent 138.1s`.
- Acceptance, real binary, sandbox ledger, one zombie row present:

```
BEFORE updated_at=1789379957.0 events=0 md5=41cb2c6968056bbdd36bf714674686b8 notify=none
--- 3 plain reads (sweep OFF the path) ---  0.00 s 0.00 s 0.00 s
AFTER  updated_at=1789379957.0 events=0 md5=41cb2c6968056bbdd36bf714674686b8 notify=none
** LEDGER BYTE-IDENTICAL ACROSS 3 PLAIN READS **
--- explicit --sweep ---
[stale] reported 1 stale in_progress row(s) via task-notify (env/settings)
AFTER --sweep updated_at=1789387157.0 (was 1789379957.0) notify=474
--- second --sweep (new process: must refuse on the persisted stamp) ---
[stale] already swept at 2026-09-14T11:59:17Z (window 10m0s) — nothing swept (`gopi task list --sweep` is the trigger)
updated_at=1789387157.0   (unchanged)
stamp: 2026-09-14T11:59:17Z
```

- Red proof: the E2E test fails on `bin/gopi-prefix` on all three legs (output
  quoted above).

### Re-verification of the committed branch (independent, this session)

- `CGO_ENABLED=0 go build ./...` → OK; `bin/gopi` rebuilt from `93bbf5d3`.
- `go test ./cmd/agent/ -run 'TestTaskListReadPath|TestTaskListSweep' -count=1 -v`
  → `TestTaskListReadPathSweepsNothing` PASS, `TestTaskListSweepFlagStillFiresStaleEvent`
  PASS, `TestTaskListSweepStampThrottlesAcrossProcesses` PASS,
  `TestTaskListReadPathE2E` PASS.
- Full `go test ./cmd/agent/ ./internal/tools/ ./internal/agent/ -count=1` run
  this session (see the job log for the tail); the pre-existing unrelated
  `TestWorkerDaemon_StatusReportsTheLiveDaemon` failure is the only known red.
- Acceptance (a)/(b), measured again this session at the process boundary on a
  40-row sandbox ledger with a zombie re-backdated before EVERY read:
  PRE-FIX **7/7** ledger UPDATEs and min-of-7 **23.68 ms**; POST-FIX **0/7**
  UPDATEs, **0** stale events and min-of-7 **11.19 ms**; NOSWEEP floor
  **8.43 ms**. 20 consecutive post-fix plain reads left the ledger
  **byte-identical** (same `updated_at`, same md5) and the notify sink empty.
- Acceptance (c), measured again this session: `task list --sweep` reported the
  stale row, bumped `updated_at`, delivered exactly one `"stale"` event (naming
  the zombie), persisted `.gopi/task-list-sweep.stamp`; the second `--sweep` in
  a NEW process was refused on that stamp ("already swept at … (window 10m0s)")
  and wrote nothing (row unchanged, still exactly 1 event).

## Files

- `cmd/agent/tasklistsweep_gate.go` (new) — the persisted-clock due predicate,
  the stamp writer, and the explicit-trigger sweep.
- `cmd/agent/cli_tasks.go` — `runTaskList` (new), the `--sweep` option + flag
  membership + usage head, the `taskListSweepThrottleMin` contract rewrite.
- `cmd/agent/cli_tasks_list_sweep_readpath_test.go` (new, in-process contract).
- `cmd/agent/cli_tasks_list_sweep_test.go` (rewritten: E2E cost-property +
  red-proveable).

## A deliberate non-change, recorded (Rule 1b/13 scope)

`--sweep` is spelled `"sweep"` inline in the three places a flag name lives
(`parseTasksArgs` → `fs.has("sweep")`, `taskFlagMember`, the usage head) —
exactly as its three `task list` siblings are (`"db"`, `"status"`, `"all"`,
all inline one line away). Flag NAMES have no constant or constant package in
this package, and Rule 1b/13 govern *meaningful values that already have a
definition* — introducing a single-file constant for one flag while its
siblings stay literals would be a fourth spelling of the same idea, not a
single source of truth. Named constants were added where the reviewer's
concern actually bites and the value IS a compared/keyword value: the stamp
name, the stamp layout, the create modes, and six `taskListSweepReason*`
verdict strings (each is compared and quoted by the tests).

