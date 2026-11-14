<!-- doc-class: historical -->
# Task Report: model name persisted as a tasks-table column (root-cause fix superseding 19a6eab)

## Epic context

The previous fix (19a6eab + report a323a21) made `gopi task show` resolve the
model from the session JSONL at display time. PA judged this a **subcommand
display fix, not a root-cause fix**: the model was never stored with the task.
The correct design — **model as ledger DATA, captured at task creation** — is
what this task implements.

## Completed

- **Schema**: `tasks` table gains a `model TEXT DEFAULT ''` column.
  - New DBs: the column is in the `CREATE TABLE` literal.
  - Existing DBs: `ensureTasksSchema` migrates in place via
    `ALTER TABLE tasks ADD COLUMN model TEXT DEFAULT ''` (idempotent; the
    "duplicate column name" error is the expected no-op on migrated DBs —
    same self-healing ensureSchema pattern as the rest of the ledger; no
    migration scripts).
  - Existing rows are untouched (append-only ledger) — they read back as
    `''` and render `(unknown)`.
- **Capture at creation**:
  - `tools.TaskNew(summary, taskID, sessionID, model)` — model is the 4th
    parameter, inserted into `tasks.model` in the same INSERT as the task
    itself. Confirmation line now prints `(status=..., model=...)`.
  - Auto-register harness (`internal/agent/taskregister.go`) passes
    `a.Model` — every session's auto row now records which model ran it,
    at the moment the task starts.
  - CLI: `gopi task new "<summary>" --model NAME` (optional; default empty).
- **Rendering from the DB**:
  - `gopi task list` renders `  (model <name>, created <ts>)` — DB truth
    only, no per-row JSONL reads (a list stays O(rows)).
  - `gopi task show` is DB-first: persisted column wins. **Legacy fallback**:
    pre-persistence rows (model='') still resolve from the session JSONL's
    `session_start` event (the 19a6eab lookup, `taskModelFromSession`, kept
    ONLY for history) so old tasks keep displaying after session-log pruning.
  - Pre-persistence rows in `list` render `(model (unknown), ...)` — list
    never touches JSONL.
- **Regression tests** (`internal/tools/tasks_test.go`):
  - `TestTaskNewModelPersisted` — DB-persisted model renders in list AND show
    with NO session file present (proves no JSONL dependency).
  - `TestTaskNewDefaultModelEmpty` — empty model renders `(unknown)`.
  - `TestEnsureTasksSchemaMigratesModelColumn` — pre-model table is widened
    in place; legacy row survives as `(unknown)`; new task on the migrated
    DB persists its model; migration is idempotent (re-open runs the ALTER
    again → duplicate-column no-op path).
  - `TestTaskShowModelFromSession` — repurposed: verifies the legacy
    fallback only (row created with model='') + list renders `(unknown)`.
  - All TaskNew call sites (17) updated for the new signature.

## Rule 11 — LLM adoption proof (real session, real tool call)

- Rebuilt binary: `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` OK.
- Ran a REAL session (`./bin/gopi "Reply with exactly: MODEL-COLUMN-OK"`):
  the auto-registered row was created **with the model captured at create
  time**:
  ```
  #1  0135c6d1-0b50-4efa-a0f9-8346f72fa7c6  [completed]  [auto] Reply with
      exactly: MODEL-COLUMN-OK — elapsed=29.4s iter=5 tokens=51228
      (model glm-5.3-flash, created 2026-08-29 05:28:39)
  ```
- Raw DB check: `SELECT task_id, model FROM tasks` →
  `('0135c6d1-…', 'glm-5.3-flash')` — the model is IN THE DATABASE, not a
  display-time artifact.
- `gopi task show` on that row → `model: glm-5.3-flash` (DB-first path).
- `gopi task new --model deepseek-v4-flash` → row persists and renders the
  model (CLI path verified live).

## Migration verified on the REAL ledger

- Copied `~/.gopi/memory.db` (pre-column: table_info shows no `model`) to
  `/tmp/tm-legacy.db`, then ran `task show` with the new binary:
  - migration added the column in place (table_info now ends with `model`),
  - the epic task row itself (65c1235c-…, created before persistence) still
    resolves `model: glm-5.3-flash` via the legacy JSONL fallback,
  - the real DB at `~/.gopi/memory.db` was NOT touched (test ran on a copy).

## Not Done

- No backfill of old rows' `model` values (append-only ledger is never
  rewritten; the show-time JSONL fallback covers history instead).
- `tasks_fts`/`tasks_tri` indexes intentionally do NOT include `model`
  (nobody searches by model today; add an index only on real demand).
- `gopi task update` cannot edit `model` (model is a create-time fact about
  which model RAN the task, not a mutable label).

## Verification

- Build: `go build ./...` OK; `go vet ./internal/tools ./cmd/agent ./internal/agent` OK
- Tests: `go test ./internal/tools/ -run 'TestTask'` PASS (all task-engine
  tests incl. the 4 model tests); full-suite `go test ./...` run see
  tasks/<task>-report (job log).
- Commits on `feat/task-model-db-column` (worktree `~/workspace/gopi-taskmodel-db`):
  - `7881994` feat(tasks): persist model in tasks table captured at creation
  - `8a9f2d3` fix(tasks-test): legacy-shape fixture must keep the tasks PK
    (CREATE TABLE AS SELECT drops PRIMARY KEY — the migration fixture now
    recreates the true pre-model shape and copies rows in)
