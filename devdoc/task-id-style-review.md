<!-- doc-class: historical -->
# Review: devdoc/task-id-style-analysis.md (task_id style — session-id vs `t-<unix>`)

Reviewer: gopi session 698a696d (hostb-Laptop), 2026-09-01.
Review target: devdoc/task-id-style-analysis.md @ 0a59749, against the live
implementation. Verdict: **agree with Option 2** (recommendation holds).
Three specific reasons follow, each checked against the code.

## Reason 1 — AGREE: Option 2 preserves the END-hook closeability without
duplicating the session id into the pk slot

The analysis (§1) correctly locates the root cause: the START hook
`maybeRegisterSessionTask` passes the session id into **both** id slots —
`tools.TaskNew(summary, sessionID, sessionID, a.Model)`
(internal/agent/taskregister.go:104). That choice exists solely so the END
hook can close the row with zero bookkeeping: `finishSessionTaskRecorded`
looks the row up by `a.SessionLog.SessionID()` in three consecutive places
— `TaskUpdateWithResult(a.SessionLog.SessionID(), …)` (taskregister.go:155),
terminal push `PushTerminalTaskSync(a.SessionLog.SessionID())`
(taskregister.go:172), and finish-cleanup
`TaskFinishCleanupSessionID(a.SessionLog.SessionID())`
(taskregister.go:186).

Option 2 (`TaskNew(summary, "", sessionID, …)` + retain the generated id on
the Agent) is the only proposal that fixes the *semantic* wrongness
(task ≠ session — tasks.go:1194 already documents that the `session_id`
column carries the submitter's session for llm rows) while keeping this
three-call chain working. The Agent already latches hook state in exactly
this shape — `autoTaskRegistered`/`autoTaskSummary`
(internal/agent/loop.go:1277-1278) — so a third field (`autoTaskID`) is a
pattern match, not new machinery. What the doc gets right and worth
stressing: the failure mode of a botched END-hook regression is silent
(rows stuck `in_progress` forever), so the retained-id field must be set
**before** the latch flips true (taskregister.go:105) and covered by
regression tests — the doc's §3 risk note is accurate, not hypothetical.

## Reason 2 — AGREE: no legacy backfill — sync merge paths are
identity-keyed and a rename diverges replicas

The doc's "recommend NOT backfilling" is correct and stronger than stated.
Every cross-node merge decides on the raw `task_id` string:

- delta insert writes the id verbatim into the replica
  (internal/tools/memory_sync_delta.go:408-411);
- harvest import likewise (internal/tools/memory_sync_task.go:1812-1820);
- collision handling on import is rename-only-on-true-collision via
  `taskIDRelabel` → `<id>-n<node>` (memory_sync_task.go:616-632, called at
  917) — i.e. the system's own answer to id churn is "don't churn; relabel
  only when forced".

A backfill UPDATE that renames 151+ UUID ids would make every replica that
already synced those rows see "same task, different id": terminal sync-back
migrations key on `task_id` (memory_sync_task.go:963, 980), so renamed ids
can no longer migrate to their peers — exactly the silent-loss class the
relabel logic was built to prevent (devdoc/task-id-collision.md). The
ledger is also not parent-heavy in a way that would tempt a backfill:
currently only 4 rows carry `parent_task_id`, and all 4 point at `t-*`
rows (ledger query on this node), so the data argues for append-forward,
rename-never.

## Reason 3 — AGREE (with one sharpening): the display-degradation claim is
real but slightly understated — and it feeds back into Option 2

`taskHashDisplay` (internal/tools/tasks.go:988-996) renders the compact
`#<n>` only when the id is `t-<unix>` with a parseable numeric body;
anything else (UUID rows, and also collision-form `t-<unix>-N` ids) falls
back to the **full id string** (tasks.go:999). So Option 1's "permanent
two-vocabulary" cost is not just cosmetics in `task list` — the `#`
column's documented purpose is *cross-node reconciliation* ("sync'd
replicas of the same task show different rowids; the t-<unix> task_id is
the shared primary key, so its digits are identical on every node",
tasks.go:1070-1074). A UUID row defeats that purpose entirely: its
"reconciliation key" is only valid on the minting node's ledger. That makes
Option 2 the consistent end-state, and it also means: when implementing
Option 2, the `-N` collision suffix (tasks.go:928) should be counted in the
regression tests, since a `t-<unix>-N` row degrades display exactly like a
UUID row does.

One sharpening to the doc: §2's Style-B con ("two peers can mint the same
id in the same second") is now *partially mitigated on master* — b988b64 /
taskIDRelabel handle sync-time collisions by relabel — but the doc is
right that this is a repair, not a design (the relabeled id then diverges
across nodes, re-entering the Reason-2 problem). This strengthens rather
than weakens Option 2: new-auto-row unification does not touch the
human/llm collision surface.

## Corrections to the doc (factual, minor)

- §4's ledger counts (151 auto / 778 t-*) were queried at analysis time;
  on this node now the counts are 152 / 792 — direction unchanged, but
  such counts in a doc should carry a "as of <commit>" stamp.
- §1 says chat's exported wrapper "reuses the same convention" — confirmed:
  `RegisterSessionTask` → `maybeRegisterSessionTask`
  (internal/agent/taskregister.go:223-226); but note the chat CLI has since
  moved its ledger row to the chats table (M4.6 hook split,
  cmd/agent/cli_chat.go:497-499 — the tasks-ledger [auto] row is retired
  for chat), so Option 2's "chat wrappers must switch too" §5 note now
  applies mainly to `gopi web`'s finalize path (cmd/agent/cli_web.go:628-631),
  which still calls `FinishSessionTask` at shutdown.

## Verdict

Option 2 stands: START hook → empty taskID (t-<unix> generated), retained
id on Agent for the three-call END chain, session link stays in the
`session_id` column (schema: internal/tools/tasks.go:149), legacy rows
never renamed. Follow-up epic per §5 of the analysis.
