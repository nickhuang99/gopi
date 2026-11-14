<!-- doc-class: normative -->
# The heartbeat that stops when the engine exits: where the 38-minute stall actually lived

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

## The claim this page answers

The epic said: `01a17a1b`'s worker **"心跳 stale 38 分钟"** while it waited on a long
test. The parent briefing asked for one thing — **measure which of three layers the
freeze is in**, and do not guess:

- **(a)** `internal/agent/taskheartbeat.go`'s `startRunKeepalive` — a 2-minute ticker
  bound to the engine's context, possibly never running or cancelled;
- **(b)** `internal/tools/taskheartbeat.go`'s `touch()` holder guard — writes refused
  with `ErrTaskHeartbeatHolderDead`;
- **(c)** the ledger's `busy_timeout` — `touch` blocked behind a writer.

**The answer is none of the three.** All three were measured as healthy. The freeze
lives in a **fourth** place the briefing's list did not contain: the wrapper process
(`gopi worker <task-id>` → `runTaskRunOnce`) has **no heartbeat writer at all**, and
it goes on working — the clone channel's delivery leg and finish-cleanup's branch
check — long after the engine child has exited.

## The measurement

### What the row itself says

Read-only SQLite (`mode=ro`, `query_only(true)`), row
`01a17a1b-496f-7179-9567-d544fa5922f9`:

```
 status=completed
 claimed_by=hosta:4030207
 session_id=4a0957a3-bb4e-4337-a761-20606c87d1fd
 created_at=2026-10-27 05:26:11
 last_toolcall_at=2026-10-27 06:15:26
 tasklist_issued_at=(none)
 updated_at=2026-10-27 07:01:10
 ended_at=2026-10-27 07:01:10
 last_toolcall_at -> ended_at span: 45.7 min
```

**45.7 minutes** of `in_progress` work with the last liveness stamp at its start.
The scheduler observed 38m52s of that window and called it stale — correctly, by the
only predicate it has.

### Layer (a) — measured GREEN: the keepalive ticks on schedule

Two windows, both sampled from outside with a read-only handle.

**Window 1 — during a real 615-second branch-scoped suite run** (`make test-budget
PKGS='./internal/tools ./internal/agent'`, 45 samples / 15 minutes):

```
07:30:34 SAMPLE 0 status=in_progress last_toolcall_at=07:30:32 (age 2s) tasklist_issued_at=(none) (age (none)) updated_at=07:29:26 (age 68s)
07:31:34 SAMPLE 3 status=in_progress last_toolcall_at=07:31:27 (age 7s) tasklist_issued_at=(none) (age (none)) updated_at=07:29:26 (age 128s)
07:32:54 SAMPLE 7 status=in_progress last_toolcall_at=07:32:45 (age 9s) tasklist_issued_at=(none) (age (none)) updated_at=07:29:26 (age 208s)
07:34:15 SAMPLE 11 status=in_progress last_toolcall_at=07:34:03 (age 12s) tasklist_issued_at=(none) (age (none)) updated_at=07:29:26 (age 289s)
```

(4 of the 45 samples are shown; the other 41 are in that run's
`evidence/heartbeat-samples.log`, one line per sample, same shape.)

**Window 2 — the decisive one — 6m20s driven by ONE bash call**, so no other tool
batch could write and `startRunKeepalive`'s ticker was the *only possible* writer:

```
07:43:59 SAMPLE 0 status=in_progress last_toolcall_at=07:43:59 (age 0s) tasklist_issued_at=(none) (age (none)) updated_at=07:29:26 (age 873s)
07:45:39 SAMPLE 0 status=in_progress last_toolcall_at=07:45:27 (age 12s) tasklist_issued_at=(none) (age (none)) updated_at=07:29:26 (age 973s)
07:47:39 SAMPLE 0 status=in_progress last_toolcall_at=07:47:27 (age 12s) tasklist_issued_at=(none) (age (none)) updated_at=07:29:26 (age 1093s)
07:49:39 SAMPLE 0 status=in_progress last_toolcall_at=07:49:27 (age 12s) tasklist_issued_at=(none) (age (none)) updated_at=07:29:26 (age 1213s)
```

The window's full 19-sample transcript is `evidence/heartbeat-quiet-window.log`
in this run's session directory.

`07:43:59 → 07:45:27 → 07:47:27 → 07:49:27` — exactly 120 s apart, i.e. the
interval the code declares (`runKeepaliveInterval = 2 * time.Minute`). **The
goroutine runs, is not cancelled, and its writes are not refused.**

### Layer (b) — measured GREEN: the holder guard did not refuse

The guard reports every refusal on stderr as `[gopi] task heartbeat: <err>`
(`internal/agent/taskheartbeat.go:254`). Across that run's own record:

```
$ grep -c "task heartbeat" output.log          → 0
$ grep -c "task heartbeat" *.raw.log           → 0
```

Zero refusals. The row's `claimed_by` pid answered signal 0 (the wrapper was
alive), so there was nothing for the guard to refuse — the guard is a *correct*
implementation of "a dead holder must not keep a row alive", and this defect is
the opposite shape: **a live holder with no writer.**

### Layer (c) — measured GREEN: the ledger was never blocking

The read-only sampler that produced every reading above opened the same 560 MB
ledger **45 times in 15 minutes** on the live profile with `busy_timeout(5000)`
and never once got a busy refusal. A writer blocked for tens of minutes would have
shown up as a `touch` timeout, not as a silent clock.

### The fourth layer — where the writer actually is, and where it is not

Every heartbeat write in the tree lives in `internal/agent` and is bound to the
**engine's** lifetime:

| writer | site | lifetime |
|---|---|---|
| registration stamp | `internal/agent/taskregister.go` (`maybeRegisterSessionTask`) | engine start |
| per-iteration touch | `touchRunIterationStart` | each round |
| per-tool-batch touch | `touchLastToolcallAt` | each tool batch (30 s throttle) |
| run keepalive | `startRunKeepalive` (`taskheartbeat.go:191`) | engine's `ctx` |

The wrapper is a **different process** and it outlives the engine. The engine's own
session JSONL ends at `06:15:43`, eight seconds after its last ledger stamp, and the
wrapper's writeback landed at `07:01:10`. **Nothing in that 45-minute gap was ever
going to write** — not because a mechanism failed, but because no mechanism exists on
that side of the process boundary.

## The fix

`internal/tools/taskcloseoutkeepalive.go`, wired at the claim in
`cmd/agent/cli_tasks.go` (`runTaskRunOnce`, immediately after `TaskStartForce` +
the claim-lock protocol):

```go
stopKeepalive := tools.StartTaskRowKeepalive(context.Background(), target)
defer stopKeepalive()
```

It reuses the ONE write path (`TaskHeartbeatConn.ExecTouchLastToolcall` →
`touch`), so it inherits the holder guard, the terminal-row no-op and the column
semantics instead of re-implementing them (Rule 14). Two properties are
deliberate:

- **The interval is the engine's own 2 minutes** (`TaskRowKeepaliveInterval`):
  both are "a live holder saying so", and the 30-minute grace leaves nine ticks of
  headroom either way.
- **The keepalive's lifetime IS the function's lifetime.** It starts at the claim
  and stops at the deferred `stop()`, which cancels *and waits* for the goroutine,
  so the writeback cannot race a write still in flight and nothing stamps the row
  after the run is gone — the false-live direction the engine's own keepalive avoids
  by being ctx-bound.

**Zero new dependencies (Rule 35):** no process, no file, no timer outside gopi, no
config key. The wrapper already exists and already holds the claim; this is that
process saying what is true.

One clarity fix rides along: `OpenTaskHeartbeatConn` collapses its open error into a
nil handle, which made a failed stamp indistinguishable from a quiet one (Rule 15).
`stampTaskRowLiveness` is the wrapper-side door that **returns the reason**.

## What this does NOT fix

The 45.7-minute window was mostly the **delivery leg + branch check**, and the fix
makes that window *visible* rather than *shorter*. The run still took 45 minutes
after its engine finished; the heartbeat now tells the truth about it instead of
calling a working wrapper a zombie. Shortening that close-out is a separate
question — the epic's own R1–R4 (test classification, periodic channel, exclusive
long tests) address it from the test side.

## Reproducing the sampler

The measurement used a ~90-line read-only Go program
(`evidence/hbprobe/main.go` in this run's session directory) rather than the
`sqlite3` CLI, which is not installed on this host. It opens

```
file:<ledger>?mode=ro&_pragma=busy_timeout(5000)&_pragma=query_only(true)
```

and prints one line per sample: status, `last_toolcall_at`, `tasklist_issued_at`,
`updated_at`, each with its age. Any future heartbeat question should use it (or the
same DSN) — a write-capable handle would make the observer part of the observation.

## Verification

See `$GOPI_TASK_REPORT_PATH` for the run's `## Verification` block: the acceptance
tests, the suite scope, and the Rule 31 execution of the changed surface.
