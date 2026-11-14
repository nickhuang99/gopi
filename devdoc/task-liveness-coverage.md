<!-- doc-class: historical -->
# task-liveness-coverage — the ledger's liveness column, completed

**Date:** 2026-10-16 · **Task:** `01a1430b-ce3f-71c7-a19e-8dd854286e4b` ·
**Scope:** `internal/agent` + `internal/tools` (and their tests)

## The defect (measured)

The ledger's liveness column `last_toolcall_at` was written at exactly TWO call
sites, both inside `executeToolBatchCtx` / `executeToolBatchParallel`
(`internal/agent/loop.go`). A run that was alive but wrote no tool batch in the
window therefore left the column untouched, and **`quiet but alive` and `dead`
were indistinguishable in the ledger** — while the zombie sweep's stall leg is
the PRIMARY candidate query over that same column.

Three uncovered intervals:

1. **between session registration and its first tool batch** — the row exists,
   carries `created_at`, and nothing else;
2. **between two batches** — a long model inference writes nothing;
3. **inside ONE long tool call** — the measured 10-minute `bash`. No loop
   iteration returns while a single call runs, so interval 2's fix alone does
   not reach here.

The reported family (`01a138bd-18ef-7eec-bbec-e32e83969ccd` on peerbox: `reason:
zombie disposal (zombie: heartbeat stalled past grace (last_toolcall_at idle));
heartbeat stop + claimed pid 2295338 dead`) is a **true positive** — the pid was
independently dead. This task does **NOT** claim the disposer misjudged it. What
it fixes is the *input*: the sweep was reading a column that a live run does not
have to write.

## The fix

### Coverage — five touches (was three)

| # | interval | writer |
|---|---|---|
| 1 | session registration | `setTaskHeartbeatSession` → `touchLastToolcallAt()` |
| 2 | loop-iteration start | `touchRunIterationStart()` at the top of `runEngine`'s ReAct loop |
| 3 | tool-batch start | the two original call sites (**unchanged**) |
| 4 | `[TASK_LIST]` block | `touchTasklistIssuedAt()` (**unchanged**) |
| 5 | anything slower than the tick | `startRunKeepalive(ctx)` |

The keepalive (5) is the only one that reaches interval 3: it is an
**in-process goroutine bound to the run's own context** (`ctx, cancel` built in
`runEngine` from `a.runScopedRoot()`, the same scope the tools receive), so it
dies with the run and can never stamp a row after the run is gone — the
false-live mode a detached keeper would have. No daemon, no cron, no script, no
state file, no new tick of its own (Rule 35): it rides a goroutine of the run
that already exists.

Every write goes through the ONE existing throttled, best-effort path
(`touchHeartbeat`): a ledger problem prints to stderr and never blocks or errors
a round.

Named constants (Rule 1):

```go
runKeepaliveInterval     = 2 * time.Minute  // 9 ticks fit inside the 30min default grace
taskHeartbeatThrottleMin = 30 * time.Second // (pre-existing)
```

Two rebindable seams (`taskHeartbeatThrottle`, `runKeepaliveTick`) default to
those constants and exist only so a test can observe a real ledger write without
waiting the production pace — the `taskLiveHolderProbe` pattern already used in
`internal/tools`.

### The stall predicate — ONE expression (was two copies)

Two independent readers answered "is this row idle?" from `last_toolcall_at`
alone, in two different languages:

- SQL: `COALESCE(last_toolcall_at, created_at)` — duplicated in
  `taskzombie_sweep.go` (candidate query) and `taskheartbeat.go`
  (`DetectStaleTasks`);
- Go: `hb := t.CreatedAt; if t.HasToolcall { hb = t.LastToolcallAt }` — in the
  sweep's per-row re-check and in the display's `taskHeartbeatReading`.

Both now read the row's **last recorded liveness instant** through one shared
definition each (Rule 13):

```go
func taskLivenessInstantSQL() string   // internal/tools/tasks.go
func taskLastLivenessAt(t *Task) float64
func taskStallHit(t *Task, stallSec int, now float64) bool
```

**The SHAPE matters, and it is deliberately not a plain `MAX` over all three
columns.** The expression is *"the newest fact the row RECORDED; `created_at`
only when it recorded none"*:

```sql
CASE WHEN last_toolcall_at > 0 OR tasklist_issued_at > 0
     THEN MAX(COALESCE(last_toolcall_at,0), COALESCE(tasklist_issued_at,0))
     ELSE COALESCE(created_at, 0) END
```

Folding `created_at` into the `MAX` was written first and **failed an existing
test** (`TestTaskStateSuffixMarksStaleHeartbeat`): a young row whose toolcall
stamp is ancient had its creation mask the heartbeat, silently changing the
meaning of every existing row. The pre-existing contract — *a recorded stamp
wins over `created_at`, `created_at` is the FALLBACK for rows that never
ticked* — is preserved byte for byte; the ONE delta is that a recorded
`tasklist_issued_at` now counts as a liveness fact. `TestLivenessInstantHalvesAgree`
drives five row shapes over both halves and asserts they pick the same instant.

The audit reason widened accordingly (the `last_toolcall_at` substring is kept,
so existing devdoc/evidence references still resolve):

```
zombie: heartbeat stalled past grace (last_toolcall_at / tasklist_issued_at idle)
```

No schema migration: the columns stay, their meaning is completed and
documented.

## Red proofs

Both directions were measured with the fix stashed to a file (never a shared
`git stash` stack — 8 linked worktrees share `refs/stash`):

**(a) query side** — `TestStallPredicateReadsTasklistFact`, pre-fix code:

```
--- FAIL: TestStallPredicateReadsTasklistFact (0.01s)
    tasks_stale_test.go:127: DetectStaleTasks reported 1 stale row(s); the tasklist fact must keep this row out
```

**(b) in-process side** — `TestHeartbeatWrittenAtSessionRegistration` (with the
three new writers reduced to no-ops while keeping the declarations, so the red is
an ASSERTION and not a compile error):

```
--- FAIL: TestHeartbeatWrittenAtSessionRegistration (0.01s)
    taskheartbeat_test.go:203: registration left last_toolcall_at NULL: a run killed before its first batch is indistinguishable from a dead one
--- FAIL: TestHeartbeatAdvancesWithNoToolBatchDuringPause (0.00s)
--- FAIL: TestHeartbeatWrittenAtIterationStart (0.00s)
```

**Restore** — verified by content identity against an explicit revision
(`git hash-object` of the worktree file == the saved copy's blob hash):

```
internal/agent/taskheartbeat.go 6258aaa65cf162b991f913ddf116a30b7bcbf61f saved=6258aaa65cf162b991f913ddf116a30b7bcbf61f
internal/agent/loop.go          83bde0208e45141920894648c1fdeeb3cd8350c0 saved=83bde0208e45141920894648c1fdeeb3cd8350c0
internal/tools/tasks.go         8c2d86bd9534c55b5bd70c2c2adde1a350cc3bde saved=8c2d86bd9534c55b5bd70c2c2adde1a350cc3bde
```

## Rule 31 — the real run

A sandboxed run (`gopi tool sandbox create --label liveness`) whose single tool
call was `sleep 150` — i.e. **one** tool batch, then a pause far longer than the
probe interval, with no new batch anywhere in it. The ledger column was queried
directly (not through a unit test):

```
21:04:38  created_at = last_toolcall_at = 1.792155878e9   (registration touch)
21:05:26  last_toolcall = 1.792155878e9
21:06:39  last_toolcall = 1.792155998e9   +120s — inside the same bash
21:07:39  last_toolcall = 1.792155998e9
21:08:44  last_toolcall = 1.792156118e9   +120s — still the same bash
```

The stamp advances in `runKeepaliveInterval` steps while **no** tool batch
starts, which is exactly the pre-fix blind spot. `gopi task show` on the same
row renders liveness honestly:

```
liveness:       last_toolcall_at=21:08:38 (age 6.24s — fresh) tasklist_issued_at=(none — unknown)  (clock 21:08:44)
updated_at:     2026-10-16 21:04:38  (state/sync anchor — not a heartbeat)
```

An **absent** fact reads as `(none — unknown)`, never as `stale` (Rule 15), and
`updated_at` states what it is.

## Not done (honest)

- The three doc'd intervals are covered, but a run blocked in a **non-context
  aware** call that ignores cancellation will keep its keeper until the process
  dies — the keeper is bound to the ctx, and a tool that ignores the ctx does
  not release it. That is the ordinary Go cancellation contract, not a new
  limitation introduced here.
- No adjustment to the default grace (30min) or to the max-run backstop; the
  fix is about the input, not the threshold.
