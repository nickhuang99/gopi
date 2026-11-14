<!-- doc-class: historical -->
# Investigation: t-1788305267 "fake in_progress" — orphaned wrapper row, NOT a state-machine bug

**Date**: 2026-09-02 · **Node**: HP (client→peerbox) · **Trigger**: `/tmp/fake_claimed.txt` (PA task) —
`t-1788305267` shows `in_progress`, `claimed_by: hostb-Laptop:890052`, empty session_id/model,
`last_toolcall_at=(none)`, and `task run` on it errors with
`"already live-claimed locally ... status not acquire"`-shape refusals.

## Verdict

The row is a **real** `in_progress` row whose runner **died without a terminal writeback** —
an orphaned wrapper. The dispatcher state machine did exactly what it is designed to do
(`claimed→in_progress` 双锁齐, Phase-3 advance). There is no fake status, no sync bug,
no state-machine hole. What is missing on a daemon-less node is a **close-out trigger** —
fixed by `feat/hp-zombie-closeout` (list-read sweep).

## Evidence chain (file:line / data)

1. **The pid is dead**: `ps -p 890052` → no process. `claimed_by=host:pid` is stamped by
   `taskDefaultClaimHolder` (internal/tools/tasks.go:2509) — a real local wrapper spawned it.
2. **No terminal event**: `task_events` for the id contains ONLY `in_progress` (06:17:26) —
   the writeback (`writebackOutcome` → `TaskRunWriteback` → `TaskUpdateWithResult`,
   cmd/agent/cli_tasks.go:runTaskRunOnce tail) never ran because the wrapper process died
   first (SIGKILL / terminal close leaves no exit hook).
3. **No session**: the child `gopi` either never spawned or never flushed a session JSONL
   (bug054 early-stamp is best-effort) — hence empty session_id/model columns.
   No `sessions/*.jsonl` in the run window carries `t-1788305267`.
4. **A live control group exists**: `t-1788329788` (pid 890006) and `t-1788330229`
   (pid 890282) are ALSO `in_progress host:pid` rows — but their pids are alive and their
   children run right now (`/home/user/work/GoPi/bin/gopi`, ppid=wrapper). Same shape,
   alive wrapper ⇒ legitimate running row. Running-vs-zombie differs ONLY by pid liveness
   + terminal writeback, which is exactly the zombie evidence chain's model
   (internal/tools/taskzombie.go: a+b both hit = confirmed zombie).
5. **Concurrent-window corroboration**: events 10–13 (06:13–06:24) are four `in_progress`
   writes from four parallel `task run` wrappers — 889942 (dead, orphan), 890006 (alive),
   890052 (dead, our row), 890282 (alive). `TaskStart` writes `in_progress` directly
   (internal/tools/tasks.go:2938 → taskUpdate), matching the event trail.

## Why `task run` refuses (the confusing part)

`TaskClaimExplicit` shape ② (internal/tools/tasks.go:2864): `UPDATE ... WHERE status='claimed'`
→ RowsAffected==0 on an in_progress row → the refusal
`"task %s is %s (holder %s) — refusing to run: only pending or claimed (crash-recovery) rows are claimable"`.
This is the **P0 double-exec guard working as intended** (c0605b8): double-running would
execute the briefing twice. The refusal is correct; the row just needs zombie close-out.

## Why nothing cleaned it up (trigger island, root cause of the outage shape)

Stale-sweep triggers before `feat/hp-zombie-closeout` (019c464): serve pass, `task run`
entry, `resume`. HP is one-shot CLI, no daemon, no worker — after the wrapper died, no
gopi process ever touched the ledger again, so `DetectStaleTasks` (with its zombie
evidence chain: pid dead ⇒ confirmed) never got a trigger. This is the exact
"trigger-island" shape the sibling task fixed by adding the **query-side sweep on
`task list` reads** (maybeTaskListSweep, 10 min throttle, ledger re-arm dedup).

## Resolution

- **Close-out mechanism**: branch `feat/hp-zombie-closeout` (worktree
  `~/work/gopi-hp-zombie`), commits `019c464` (feat: list-read sweep) +
  `d967ba6` (RCA devdoc), merged master at `ad5d96e` (conflict: const block — both
  `taskChildFailTestEnv` and `taskListSweepThrottleMin` kept).
- **Verification on the merged branch**: `go build ./...` OK; 4 suites green
  (cmd/agent 13.6s, internal/tools 67.0s, internal/agent 10.7s, internal/config 0.1s),
  including `TestTaskListSweepReportsZombieOnRead` (the incident repro) and
  `TestTaskRunExplicitRefusesInProgress` (the double-exec refusal).
- **Manual cleanup for the existing zombie rows** (operator): after the branch's binary
  is deployed, any `gopi task list --status in_progress` fires the sweep and delivers the
  stale event; or `gopi task update <id> --status cancelled` per row.
- **Not done here**: deleting/patching the ledger rows by hand (append-only ledger —
  status moves only, via the CLI verbs).

## Lessons (Rule 5/10)

1. An `in_progress` row with `claimed_by host:pid` is **genuine by construction** — the
   first check is pid liveness, not "fake status".
2. The refusal message ("already live-claimed / refusing to run") is the double-exec
   guard, not a bug report.
3. Zombie close-out needs a trigger that exists on daemon-less nodes — the list-read
   sweep is that trigger (the operator's look IS the process).
4. Worktree E2E trap: `testutil.GopiBin` resolves `<module-root>/bin/gopi` — rebuild the
   binary inside the worktree after merging, or tests run against a stale build.
