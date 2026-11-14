<!-- doc-class: historical -->
# Task Report: backfill empty tasks.model from session JSONL

## Epic
`gopi task list` historical rows showed no model name ("(unknown)") even
after the model-column root-cause fix (commit 7881994) — because that column
only captures the model for NEW tasks; pre-persistence rows keep `model=''`.
This task backfilled those historical rows from each session's JSONL
`session_start` event (the authoritative model record).

## Investigation summary
- The tasks ledger lives in `~/.gopi/memory.db`, table `tasks`, column
  `model` (added by 7881994, `ensureTasksSchema` migrates in place).
- 11 total tasks, 9 with `model=''` at the start.
- For auto-registered tasks `task_id == session_id`, and the session JSONL
  is `~/.gopi/sessions/<session_id>.jsonl` whose **first line** is the
  `session_start` event carrying `"model":"..."` (NewSessionLogger writes +
  syncs it before any other event — same invariant gopi's own
  `taskModelFromSession` in `internal/tools/tasks.go` relies on).
- Result: 9/9 empty-model rows had a recoverable model — 6 deepseek-v4-flash,
  3 glm-5.3-flash. (The Epic's "banner in raw log" source: the raw logs did
  NOT contain a model banner; the JSONL `session_start.model` field is the
  true authoritative source and is what gopi's own code reads.)

## Completed
- `scripts/backfill-task-model.sh`: dry-run by default, `--apply` to commit.
  Reads `tasks WHERE model=''`, resolves each `session_start.model` from the
  session JSONL, writes `UPDATE tasks SET model=... WHERE task_id=...` in a
  single transaction. Refuses to apply without an existing
  `memory.db.bak-task-backfill` backup.
- Minimum-first workflow honored:
  1. Validated the script against a **copy** of the real DB
     (`GOPI_MEMDB=/tmp/memdb-test.db`): dry-run + apply, 9/9 resolved,
     `PRAGMA integrity_check` = ok, unrelated tables row-count unchanged.
  2. Applied to the real DB for ONE target task first (93fa7eb4), verified
     `gopi task show`/`list` now display `deepseek-v4-flash`.
  3. Batch `--apply` the remaining 8 rows against the real DB.
- Backup of the real DB created BEFORE any mutation:
  `~/.gopi/memory.db.bak-task-backfill`.

## Verification
- `sqlite3 ~/.gopi/memory.db "PRAGMA integrity_check;"` → `ok` (before and
  after backfill).
- 9 rows backfilled; 0 rows remain with `model=''`.
- `gopi task list` now renders `(model deepseek-v4-flash|glm-5.3-flash, ...)`
  for all 11 tasks — zero `(model unknown)`.
- `gopi task show <id>` renders the persisted model directly from the DB
  (no session-JSONL fallback needed anymore).
- **Only the `tasks.model` column changed**: a precise diff of every other
  tasks column (id/task_id/summary/status/session_id/created_at/updated_at/
  ended_at) between the live DB and the pre-backfill backup is **empty**.
- Unrelated tables unchanged: distilled_qa=1032, memory_feedback=301 (both
  identical to backup). FTS/tri triggers consistent (tasks/tasks_fts/
  tasks_tri all = 11).
- Build/run: `go build -buildvcs=false -o /tmp/gopi ./cmd/agent` OK;
  `gopi task list`/`task show` used for live verification.

## Not Done
- No task-show-model CLI change is needed — the persisted-model display
  (already in master via 7881994) now just works once the column is filled.
- No automation to auto-backfill FUTURE empty-model rows: the append-only
  ledger is never rewritten by gopi itself by design; a fresh backfill
  re-run of this script is the manual path if any new empty rows appear.

## Deliverable location
- Branch: `feat/task-model-backfill` (worktree:
  `/home/user/workspace/gopi-task-backfill-wt`)
- Recovery: `git show feat/task-model-backfill:scripts/backfill-task-model.sh`,
  `git show feat/task-model-backfill:devdoc/task-model-backfill.md`
- Commits are on the shared repo's `feat/task-model-backfill` branch (worktree
  workflow — no separate push needed).
- The script's live runtime artifacts (dry-run/apply validation copy) are in
  `/home/user/workspace/gopi-task-backfill/`.
