<!-- doc-class: normative -->
# The worker round, audited: what it calls, and what it no longer may

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

Operator ruling (2026-10-26): a worker's round is **CLAIM → RUN → REPORT**, and
nothing else rides a claim loop. This page is the **line-by-line audit** that
made the ruling actionable: every call the round makes, classified as one of
three duties, with the ones that are none of the three named instead of quietly
tolerated.

The two removals this audit produced (epic `01a186d6`): `workerSyncPeers()` —
automatic row **exchange** with every declared peer, called at the end of
`workerRoundRun` — and `workerTakeOverAttempt()` — a **peer-ledger WRITE** (CAS
pending→cancelled on the source ledger) reached from the drained branch of the
loop. Both hid behind switches that default **OFF** (`task_sync`,
`task_pull`), which is exactly why nobody noticed them: on every node measured
they were unreachable code, so nothing ever demonstrated them failing.

**The capability is not gone.** `tools.SyncPeerSession`, the `gopi task sync`
family (`--peer` / `--peers` / `--status` / `--full` / `--push-only`),
`config.ResolveSyncPeers`, and `tools.SyncTransferTakeOver` (now reached only
from the manual `gopi task receive --takeover`, `cli_tasks_sync.go:1077`) are
all intact. What was deleted is the **automatic** leg, not the verb.

## The three duties

| duty | definition |
|---|---|
| **认领** (claim) | obtaining a row: the CAS scan, eligibility guards, the claim's own reversal, the stop/boundary checks that decide *whether to claim* |
| **跑** (run) | executing the row this round holds: entering its session dir, the run funnel, the child, the round's own cwd/record close-out |
| **写回** (write back) | recording what happened to THIS round's own row: the state record, the provenance latch, the terminal writeback, the release of the ground this process finished |

Anything else is **out of scope**. `## Not Done` is where the exceptions live.

## A. Loop prologue (`runTaskWorkerLoop`, once before any round)

| call | verdict |
|---|---|
| `assertTaskChildFailEscapeSandboxed` | 跑 (a refusal guard on the run entry) |
| `workerMaxRounds` / `workerIdleExits` | 认领 (loop bounds) |
| `acquireTaskLifecycleOwnership` | **exception** — a process-role mark so a co-resident merger tick yields instead of double-sweeping |
| `announceWorkerDisplay` | 跑 |
| `installWorkerStopSignals` / `startWorkerStopForwarder` | 跑 |
| `workerCtxFor` / `pruneWorkerStateResidue` / `installFileWatcher` | 跑 (the worker's OWN state files) |

## B. Per-round boundary

| call | verdict |
|---|---|
| `imageAlarm.Report` | 跑 (self-check of the running binary) |
| `refLag.Report` | **exception** — read-only ref comparison (see `## Not Done`) |
| `workerCtx.markClaiming` / `workerDaemonGuardIdentity` | 认领 |
| `consumeDrainRequests` / `workerStopRequested` | 认领 (stop honoured at the claim boundary) |
| `workerRoundClaiming()` | **认领** — the CAS scan (`tools.TaskNextFiltered`) |
| `time.Sleep(workerIdleRecheckMS)` + second `workerRoundClaiming()` | 认领 (the settled re-check) |
| `tools.LiveRowsOwnedByOthers` | 认领 (the drain line's honesty) |
| `setIdle` / `workerIdleDrainedWait` / `waitWorkerIdleOrStop` / `notifyWorkerIdleDrain` | 认领 (waiting for work) |
| `awaitWorkerStopChild` / `recordStopVerdict` | 跑 (close-out under a stop) |
| `hostLifecycle*Print*` | 写回 |

## C. `workerRoundClaiming` / `workerRoundNamed`

| call | verdict |
|---|---|
| `consumeDrainRequests` / `workerStopRequested` | 认领 |
| `tools.TaskNextFiltered` | **认领** — the ONE queue scan |
| `workerRoundRun(…, workerRowClaimed)` / `workerRoundRun(…, workerRowNamed)` | 跑 |

## D. `workerRoundRun` — the body both faces execute

| call | verdict |
|---|---|
| `workerRecordStopTask` | 写回 (provenance for a later termination line) |
| `workerCtx.setRoundStart` | 写回 |
| `tools.TaskSessionID` + `setRoundTask` | 跑 (where the round's artifacts live) |
| `tools.TaskSessionDir` + `signalrec.SetSessionDir` | 跑 (record destination) |
| `taskWorkdirEnter` | 跑 |
| `tools.RevertClaimUnattributed` | **认领** (the claim is REVERTED when the round cannot enter its dir) |
| `workerWriteback = target` | 写回 (provenance latch) |
| `workerRoundResetRetryResume` | 跑 |
| `runTaskRunOnce(roundOpts)` | **跑** — the ONE run funnel |
| `taskWorkdirRestore` / `SetSessionDir("")` | 跑 (cwd/sink close-out) |
| `workerRoundAwaitRetryResume` | 跑 (the round's own handed-off child) |
| `defer workerRoundOwnWorktrees` | 写回 (releases the ground of the rows THIS process finished) |
| `workerCtx.setRoundEnd` | 写回 |

**There is no longer a ⑥.** The round ends at `setRoundEnd`; the DB-sync leg that
used to follow it (and the `workerSyncSettleDelayMS` pause it slept) is gone.

## E. `runTaskRunOnce` (`cmd/agent/cli_tasks.go`) — inside 跑

| call | verdict |
|---|---|
| `signal.Ignore(SIGPIPE)` / `refuseIfUnsafeTaskChildFailEscape` | 跑 |
| `TaskResumeClaimed` / `TaskRunGuardFor` / `TaskRunGuardAdmits` / `TaskRepoGuardWarn` | **认领** (eligibility of the row) |
| `tools.TaskNextFiltered` (the funnel's own claim arm) | **认领** |
| `tools.TaskClaimExplicitForce` | **认领** |
| `tools.TaskRepairDeadRepoInPlace` | 认领 (repairs the row's own deliverable pointer) |
| `taskWorkdirEnter` | 跑 |
| `staleSweep()` (M4.8 ③) | **exception** — ledger reconciliation on the run path, see `## Not Done` |
| `tools.TaskStartForce` | **跑** (pending→in_progress) |
| `tools.RunClaimLockProtocol` | **exception** — Phase-2 remote CAS, default-OFF gated, see `## Not Done` |
| `tools.StartTaskRowKeepalive` | 跑 |
| `taskRunBriefing` / `runMethodFor` / `tools.TaskStampRun` | 跑 |
| `writebackOutcome` / `taskRunReleaseOwnWorktrees` | **写回** |

## Not Done — the exceptions, named

1. **`staleSweep()` in `runTaskRunOnce`** (`cli_tasks.go:2602`). Reports stale
   `in_progress` rows to task-notify on every run. Ledger reconciliation riding
   the run path, one level deeper than the worker boundary — and shared with
   `gopi task run`, so removing it is a change to the run funnel, not to the
   worker.

2. **`tools.RunClaimLockProtocol`** (`cli_tasks.go:2640`). Phase-2 remote CAS
   acquire: contacts every peer's ledger on every run. Gated by
   `TaskSyncEnabled` (default OFF) + a declared peer set — **the same shape as
   the two legs this page just deleted**: a peer-writing leg a default-OFF
   switch hides. It writes no row (a lock, not a transfer), which is the only
   reason it was not deleted alongside them.

3. **`refLag.Report()`** (`worker_ref_lag_check.go`). Reads two refs it already
   has and prints one sentence when they disagree — no fetch, no push, no ref
   move, no ledger write, no socket. It is deliberately named a **check**, not a
   sweep, and it may ride a claim loop because the ruling forbids a worker that
   *stops claiming for a fetch*, not a worker that *reads two refs*.

4. **`acquireTaskLifecycleOwnership`** — a mark, not a duty: it exists so a
   co-resident merger yields. It performs no reconciliation.

## The guard, and why it missed both legs (a PROPOSAL, not a change)

`cmd/agent/worker_round_dispatch_only_test.go` scans the span **marker ① →
`workerRoundClaiming(`** for names containing `Tick|Sweep|GC|Reap`. Measured
consequence, on the tree this page was written against:

- `workerTakeOverAttempt(once)` sat at `:811`, i.e. **after** the first
  `workerRoundClaiming()` (`:758`) — outside the scanned span entirely, and its
  name carries none of the four tokens.
- `workerSyncPeers()` sat at the **end of `workerRoundRun`**, a function the scan
  never opens, and again carries no token.

Both were invisible to a gate whose whole subject they were. Proposed widening:

1. Scan to the **end of the round body** (marker ① → the `}` closing the `for`
   loop), anchored by a parse rather than a string index.
2. **Enter the callees** the round calls — `workerRoundRun`,
   `workerRoundClaiming`, `workerRoundNamed` ARE the round.
3. Add a **verb list** beside the suffix list: `Sync`, `Push`, `Pull`, `Dial`,
   `Transfer`, `Send`, `Receive`, `Fetch`, `TakeOver`. Naming conventions are
   necessary but not sufficient — `workerTakeOverAttempt` is only catchable by
   naming `TakeOver` explicitly, which is the honest statement to carry in the
   file header.

Until that lands, this page is the instrument: a reader adding a leg has the
table above to check against.
