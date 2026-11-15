<!-- doc-class: normative -->
# Design Analysis: task_id — session-id style vs `t-<unix>` style

Epic: task #e1b69fb4 (auto, task_id=session UUID) vs #1788252501 (human, task_id=`t-1788252501`).
Date: 2026-09-01. Status: analysis + recommendation (no code change without a follow-up epic).

## 1. Root cause — where each style is born

Both styles come from `internal/tools/tasks.go`:

- **`TaskNew(summary, taskID, sessionID, model)`** (tasks.go:907). If the
  caller passes a non-empty `taskID` it is used **verbatim** (after an
  exists-check → error on collision); if empty, an id is auto-generated:
  `t-<unix seconds>` with `-N` suffix loop on collision
  (`taskIDPrefix="t-"`, `taskIDMaxAttempts=10`, tasks.go:94-95, 921-939).

- **Auto-registered runs**: the START hook
  `maybeRegisterSessionTask` (internal/agent/taskregister.go:87) calls
  `tools.TaskNew(summary, sessionID, sessionID, a.Model)` — it passes the
  **session id in both slots**. That is the entire root cause: the hook
  *chooses* task_id=session_id so the END hook
  (`finishSessionTaskRecorded`, taskregister.go:129) can find the row by
  `a.SessionLog.SessionID()` and flip it in_progress→completed/failed,
  plus terminal sync push + finish-cleanup keyed off the same value.
  (Chat mode's exported `RegisterSessionTask` wrapper reuses the same
  convention.)

- **Human / LLM submissions**: `gopi task new` / `task_submit` leave
  `TaskID` empty → the `t-<unix>` generator runs. (LLM rows source=llm
  stamp the *submitter's* session into the `session_id` column only.)

So this is not two code paths drifting — it is **one deliberate choice**
made so the END hook needs no per-session bookkeeping field: the task
ledger's lookup key IS the session id. 151 rows in the current ledger
have task_id==session_id (auto style); 778 rows are `t-*` style.

## 2. Pros / cons

### Style A: task_id = session_id (auto rows)

Pros
- END hook closes the row with zero extra state (no "which task row does
  this session own" map/field); chat wrappers share it.
- `task show <id>` on an auto row directly finds the session JSONL
  (~/.gopi/sessions/<id>.jsonl) for briefing/model fallback — id is a
  **hard link** between ledger and log.
- Trivially collision-free (UUID v4); cross-node safe.

Cons
- **Two id vocabularies in one column** — display/sort/tab-completion
  inconsistency (UUID vs #1788252501 style).
- `taskHashDisplay` (tasks.go:988) renders `#<n>` only for pure-numeric
  `t-<unix>` bodies; auto rows degrade to the **full UUID** in list
  output — noisy, wide rows.
- Semantically wrong: task ≠ session. One task may span multiple
  sessions (resume creates a new session id), and the ledger's
  `session_id` column already carries this fact — the pk slot duplicates it.
- Human-discovered bugs (like this epic) get awkward short references
  (UUIDs don't tab-complete or get quoted in chat well).

### Style B: t-<unix> (generated)

Pros
- One uniform id shape for every row; `#1788252501` compact display
  everywhere; monotonic ≈ sort-by-creation.
- task and session concepts decoupled; resume/multi-session tasks safe.

Cons
- Unix-second ids collide across processes in the same second → the
  `-N` retry loop; across **nodes** (client/server sync) two peers can
  mint the same id in the same second — collision must be resolved at
  sync-merge time, which the uniform-id code paths were not designed for.
- The END hook would need the start hook to remember "the task row I
  created" (an Agent field already exists — `autoTaskRegistered` — but
  the row id itself is not retained; TaskUpdateWithResult is keyed by
  task_id).
- Legacy rows (151 of them) keep the UUID style forever unless
  backfilled — a "uniform" ledger requires data migration.

## 3. Reconcile options

**Option 1 — keep as-is, document (lowest cost).**
Codify the convention: source=auto rows use the session id as task_id;
source=human/llm rows use `t-<unix>`. Cost: 0 code, doc-only. Keeps the
END-hook simplicity and the hard ledger↔JSONL link. Cost: permanent
display inconsistency; every future consumer must handle both shapes.

**Option 2 — unify on `t-<unix>` for NEW auto rows; keep session link
in the `session_id` column.**
Start hook changes to `TaskNew(summary, "", sessionID, a.Model)` and
retains the generated id on the Agent (new field) for the END hook.
All consumers already have `session_id` for the JSONL link. Migration:
new rows only; legacy 151 rows can stay (or a one-shot backfill
UPDATE renaming ids — dangerous with parent_task_id/tags references
and sync peers; **recommend NOT backfilling**, the ledger is
append-ish and sync replicas would diverge).
Risk: TaskUpdateWithResult/PushTerminalTaskSync/finish-cleanup are all
keyed on the session id today — each needs the retained row id; a
regression here silently leaves rows in_progress (the #1788252501
class of confusion). Needs regression tests on the END hook.

**Option 3 — unify on UUID for ALL rows (including human/llm).**
Most uniform, keeps END-hook simplicity. Cost: breaks the `#<n>`
compact display and any human-facing tooling that assumes `t-`; loses
the chronological sortability; big backfill temptation (same dangers
as Option 2). Also makes human tasks harder to reference.

**Recommendation: Option 2** for new rows + a doc block (Option 1's
documentation) for legacy rows. It fixes the *semantic* wrongness
(task≠session) while keeping the actual pain points (END hook,
display) addressed; backfilling legacy data is explicitly NOT worth the
sync risk. If the team weighs the END-hook regression risk higher than
display consistency, Option 1 is a defensible permanent state — but it
must be written down in GOPI.md/devdoc (this file can serve).

## 4. Verification of the analysis

- taskregister.go:87-107 read directly: `TaskNew(summary, sessionID, sessionID, a.Model)`.
- tasks.go:907-939: empty-taskID → `t-<unix>` + collision loop; caller-supplied id used verbatim (exists-check error otherwise).
- Ledger query: `SELECT count(*) FROM tasks WHERE task_id = session_id AND task_id != ''` → **151** rows (auto style); `t-*`-style rows: 585 auto + 142 human + 51 llm.
- taskHashDisplay (tasks.go:983-999) confirms the display degradation of non-`t-<unix>` ids.
- END hook chain (taskregister.go:129-175) confirms the close/terminal-push/finish-cleanup path is keyed on `SessionLog.SessionID()`.

## 5. Not Done / follow-ups

- No code change made (this epic is analysis-only per goal).
- Option 2 implementation epic would touch: taskregister.go (start+end
  hooks), Agent field for retained task id, taskregister_test.go, plus
  a devdoc note; chat wrappers (RegisterSessionTask) must switch too.
- Legacy-row backfill: explicitly deferred/NEVER silently — sync peers
  and parent_task_id references make renaming risky.
