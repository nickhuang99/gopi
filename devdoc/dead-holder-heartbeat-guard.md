<!-- doc-class: normative -->
# Devdoc: a heartbeat write is refused when the row's holder is dead

Epic task: `01a163df` (measured incident) → run `01a16409`. Branch `master`,
cut point `339967c`.

> **A NOTE ON THE STALE SWEEP THIS PAGE REFERENCES (2026-11-05).** The guard this
> page describes is LIVE and unchanged — a heartbeat write addressed at a dead
> holder's row is still refused. What is gone is the CONSUMER it was measured
> against: `DetectStaleTasks` and its `gopi task list --sweep` trigger were
> deleted, so the steps below that run it (`④ DetectStaleTasks(30min) → …`) no
> longer reproduce — that symbol does not exist. Read those steps as the
> measurement that justified the guard, and `internal/tools/claim_holder_gone_test.go`
> as the property that still pins it.

## The defect, as measured

A row's liveness columns (`last_toolcall_at`, `tasklist_issued_at`) are written
by `(*TaskHeartbeatConn).touch` (`internal/tools/taskheartbeat.go`). Before this
change the write was addressed **only by row identity** — `task_id`, falling
back to `session_id`. Nothing about *who* is writing was checked, which is
correct in the healthy case (the writer IS the holder) and wrong the moment a
process outlives the row's holder:

- task `01a163df`'s holder `pid 1190977` was dead;
- a child `pid 1191807` survived it, with `cwd` = that run's ground and
  `GOPI_TASK_RUN_WRAPPER_PID` / `GOPI_RUN_GROUND` / `GOPI_TASK_RUN_OUTCOME` in
  its environment;
- that survivor kept stamping the dead holder's row, so the stale sweep's
  candidate query — `COALESCE(last_toolcall_at, created_at) < staleSince` — never
  selected the row again, and the zombie was never reclaimed.

Measured on the pre-fix tree through a real-program probe (Rule 31; source and
raw output in the run's session directory, `probe/src-corrected/main.go` +
`probe/run-prefix-corrected.txt`):

```
private ledger pinned: .../probe/prefix-corrected.db
holder-dead pid = 173664  (SIGKILLed + reaped; /proc/173664 exists = false)
holder-live pid = 173657  (this survivor process)

seeded 2 in_progress rows, liveness backdated 2h (stale by the 30min grace)

① survivor touches the DEAD holder's row → err = <nil>
② owner   touches the LIVE holder's row → err = <nil>

③ probe-dead-holder  last_toolcall_at = 1792699752 (1s)
③ probe-live-holder  last_toolcall_at = 1792699752 (1s)

④ DetectStaleTasks(30min) → reported 0 row(s), err = nil
```

Both rows accepted the write, both read back as "just now", and the stale sweep
saw **zero** candidates — the zombie stayed invisible.

### An evidence defect this run created and repaired (recorded, not hidden)

The probe's FIRST version carried its private-ledger path in its own
`PROBE_LEDGER` variable and read its assertions from that file — but `TaskNew`,
`OpenTaskHeartbeatConn` and `DetectStaleTasks` all resolve through
`config.LedgerPath()` (`GOPI_MEMORY_DB`), never through a caller's variable. So
the seeding landed in the OPERATOR'S LIVE LEDGER (`/home/user/.gopi/memory.db`) while the
readings came from the private file: **one junk `in_progress` row in the real
ledger**, and evidence whose two halves were different databases. Both halves
were measured, and both were wrong in the same direction — which is exactly why
a split-brain fixture is dangerous: it looks like a coherent result.

Found by the next run's own output (`task_id "probe-dead-holder" already
exists`), then measured: `SELECT ... WHERE task_id LIKE 'probe-%'` against the
real ledger returned exactly 1 row (read-only handle, `query_only(true)`). That
row was deleted by a purpose-written one-shot (`probe/src-corrected/main.go`)
that prints every row BEFORE removing it and re-verifies `probe-* rows
remaining = 0`. The probe now pins `GOPI_MEMORY_DB` itself and **refuses to
run** unless `tools.MemoryDBPath()` agrees with its private path.

Every figure above and below comes from the CORRECTED probe, whose seed, write
and read all resolve to one private file.

## The mechanism

`touch` now consults the row it is about to stamp, on **both** address legs
(`task_id`, and the pre-consolidation `session_id` leg), and refuses when the
row's `claimed_by` stamp names a holder on **this** host that is confirmed dead:

```go
refusal, err := h.holderRefusal(taskID)   // SELECT claimed_by FROM tasks WHERE task_id = ?
...
if refusal != nil {
    return fmt.Errorf("touch %s: %w", column, refusal)   // Rule 15: named, wrapped
}
```

Three properties make this the right seam:

1. **It is the only write path.** `internal/agent/taskheartbeat.go`'s two
   throttled writers (`touchLastToolcallAt`, `touchTasklistIssuedAt`) and the
   run-keepalive goroutine all funnel through `touch`, so one guard covers
   every liveness write; a second gate at the agent layer would be a second
   source of truth (Rule 14).
2. **The guard reads the SAME row the UPDATE targets.** `holderRefusalBySession`
   filters on `session_id = ? AND task_id = ?`, matching its UPDATE exactly — a
   guard that judged a different row than it stamps would be its own bug.
   Both reads also carry the UPDATE's own **`status = in_progress`** predicate,
   deliberately: a terminal row is a SILENT no-op on the heartbeat path ("a
   finished task is not alive, its timestamps stand"), so without the predicate
   every finished row still stamped with its long-dead runner would emit an
   stderr error on every tool batch — a new noise channel on the healthy path,
   which is the fail-soft direction violated. Pinned by
   `TestHeartbeatGuardLeavesTerminalRowsSilent`.
3. **It reuses existing judgements, it does not invent one** (Rule 13): the
   stamp is parsed by `claimHolder` (`internal/tools/taskzombie.go`, the same
   `hostname:pid` parse every other liveness decision in this package uses) and
   the /proc state rule is `pidIsZombie`.

## The criterion: "the holder is dead", not "the writer is the holder"

The epic states it and it is the only shape the ledger can decide. In the
healthy path the writer *is* the holder, and a process cannot compare itself
against a stamp it just wrote without inventing a second identity for the same
fact. What the ledger *can* decide is narrower and sufficient: **the stamped
holder is gone** — which is exactly the incident shape (the holder died,
something else kept writing). So the guard never asks who is writing; it asks
whether the recorded holder still exists.

## Fail-soft direction (the second half of the rule)

A heartbeat that is *not* written turns a healthy run into one the stale sweep
kills, so the guard must only ever fire on a positive measurement of death.
The direction is deliberately the same as `pidAlive`'s, established more
strictly:

| observation | heartbeat write |
|---|---|
| `kill(pid, 0)` → `ESRCH` (no such process) | **REFUSED** — measured death |
| `kill(pid, 0)` → `nil`, `/proc/<pid>/stat` state `Z` | **REFUSED** — terminated, unreaped (the SIGKILLed-wrapper shape) |
| `kill(pid, 0)` → `nil`, process alive | written |
| `kill(pid, 0)` → `EPERM` (exists, not ours) | written |
| any other probe failure (unreadable /proc, etc.) | written — "cannot tell", never death |
| no `claimed_by` stamp (an auto-registered session row) | written |
| a stamp this host cannot parse | written — nothing was claimed, nothing is known dead |
| a **foreign-host** stamp (`claimHolder` → `local=false`) | written — the pid names a different process here |

Why not simply `!pidAlive(pid)`: `pidAlive` folds `EPERM` and *every* probe
failure into "not alive", so a guard built on it would refuse on a restricted or
unreadable `/proc` — the reverse of the required direction, and a hole that
would kill healthy runs (`taskHolderConfirmedDead`'s own comment records this).

## Red proof and green proof

Test file: `internal/tools/taskheartbeat_holder_guard_test.go` (four tests: the
three below plus `TestHeartbeatGuardLeavesTerminalRowsSilent`, which pins the
`status = in_progress` predicate found by self-review — its own red proof is in
the task report).
The dead pid is minted by the package's **existing** `orphanHolderProbeRunArg`
reaped-probe mechanism (reused, not re-invented); the ledger comes from
`testutil.NewTestDBPath` (Rule 32).

- **RED A — the unmodified tree.** At `HEAD = 339967c` with only the test file
  added, all three tests fail: the dead holder's row accepts the touch
  (`heartbeat touch on a DEAD holder's row succeeded — the survivor can keep a
  dead run alive and the stale sweep will never see it again`).
- **RED B — a real mutation.** Guard body replaced by `if true { return nil }`
  (guard disabled): all three fail again at the same assertions, which proves
  the tests measure the guard rather than the surrounding plumbing.
- **GREEN — guard restored.** 3/3 PASS.

Note on RED B: the first attempt wrote the mutation as `false &&` — that makes
the guard refuse *everything*, so only the live-holder case goes red. The
mutation has to disable the guard, not invert it; recorded so the next reader
does not repeat it.

## Real-run evidence (Rule 31)

`make`-independent probe program on the production code path
(`tools.OpenTaskHeartbeatConn` + `ExecTouchLastToolcall` + `DetectStaleTasks`),
real pids, private ledger under the session directory — never the operator's
live ledger. After the fix:

```
private ledger pinned: .../probe/final.db
holder-dead pid = 173127  (SIGKILLed + reaped; /proc/173127 exists = false)
holder-live pid = 173120  (this survivor process)

seeded 2 in_progress rows, liveness backdated 2h (stale by the 30min grace)

① survivor touches the DEAD holder's row → err = touch last_toolcall_at: heartbeat refused: the row's claimed holder is dead (row probe-dead-holder, holder host-b:173127 — process 173127 does not exist on this host; a heartbeat is a liveness claim and only its holder may make it)
② owner   touches the LIVE holder's row → err = <nil>

③ probe-dead-holder  last_toolcall_at = 1792692544 (2h0m0s)   ← did NOT move
③ probe-live-holder  last_toolcall_at = 1792699744 (0s)        ← moved

④ DetectStaleTasks(30min) → reported 1 row(s), err = <nil>     ← the zombie is a candidate again
```

The last line is the epic's actual acceptance: the dead holder's row returned to
the stale sweep's candidate set (0 → 1), while the live holder's row kept its
heartbeat uninterrupted.

## What this does NOT claim (honest scope)

- The guard judges **this host's** pid namespace only; a stamp from another host
  is written through untouched (that host's sweep owns its own rows).
- A survivor whose holder's pid is **unreadable** (e.g. `/proc` mounted with
  restricted visibility) keeps writing by design — the alternative kills healthy
  runs, and the trade is stated rather than hidden.
- It does not decide *who* the writer is, so it cannot catch a survivor whose
  holder is still alive; that is a different defect with a different instrument
  (the run-identity/rung-③ family in `devdoc/live-runner-never-a-dead-holder.md`).
