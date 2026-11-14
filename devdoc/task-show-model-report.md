<!-- doc-class: historical -->
# Task Report: model name in `gopi task show`

## Completed
- `gopi task show <task_id>` now renders a `model:` line resolving the model
  that ran the task's session.
- Resolution source: the session JSONL's `session_start` event
  (`~/.gopi/sessions/<session-id>.jsonl` first line, written+fsynced first by
  `NewSessionLogger`). Fallback to `(unknown)` when the session file is missing
  or carries no model.
- **Retroactive**: works for ALL existing tasks — no schema migration; model is
  looked up at show-time from the ledger's `session_id`, so pre-existing rows
  (and manual `gopi task new --task-id X` rows with a session context) resolve
  their model without a backfill job.
- Read-only audit-layer lookup — never mutates the append-only ledger, reads
  only the first line of the session file (a long session is never fully
  parsed for a single show).
- Files changed:
  - `internal/tools/tasks.go`: `taskModelFromSession` + `taskFindSessionJSONL`
    (native ~/.gopi/sessions lookup). Importing `internal/gopilog` would have
    caused an import cycle (`gopilog` → `internal/agent` → `internal/tools`),
    so the minimal `FindSession` semantics are duplicated locally.
  - `internal/tools/tasks_test.go`: new regression test
    `TestTaskShowModelFromSession` (fixture session JSONL under a temp HOME);
    `TestTaskShow` asserts the `(unknown)` fallback.
  - `cmd/agent/cli_tasks.go`: usage text documents the model resolution.

## Not Done
- Model is NOT persisted as a `tasks` table column (unchanged schema). It is
  resolved from the session JSONL at show-time. If session logs are ever
  pruned, the model for old queries would fall to `(unknown)`. Persistence
  (a nullable `model` column captured at register-time) is the natural
  follow-up if durability against log rotation becomes a concern.

## Verification
- Build: `go build ./...` OK
- Tests: `go test ./internal/tools/ -run 'TestTaskShow...'` — 4/4 PASS
  (`go test ./internal/tools/ -run 'TestTask'` OK); `go test ./cmd/agent/ -run 'Tasks'` OK
- Commit: `19a6eab` feat(tasks): show session model in gopi task show
- Live check (real ledger): `gopi task show e3b39fcb-...` now prints
  `model:      deepseek-v4-flash` (matches the session_start event).

## Example output
```
$ gopi task show e3b39fcb-3ad1-48a2-b02d-ccbde3d0a2ba
task_id:    e3b39fcb-3ad1-48a2-b02d-ccbde3d0a2ba
status:     completed
summary:    [auto] ...
session_id: e3b39fcb-3ad1-48a2-b02d-ccbde3d0a2ba
model:      deepseek-v4-flash
created_at: 2026-08-29 04:44:23
...
```
