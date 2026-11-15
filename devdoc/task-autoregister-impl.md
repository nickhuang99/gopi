<!-- doc-class: normative -->
# Devdoc: tasks 台账自动登记 (task auto-registration)

Branch: `feat/task-autoregister-glm` — implements the PA-approved design:
启动即登记 (register at start) + 结束补统计 (stats at end) + FTS5 search.
Companion feasibility docs: devdoc/tasks-auto-register-design.md (DS),
devdoc/task-auto-register-glm-design.md (this repo's prior study).

## What was implemented

### 1. FTS5 index over the tasks table (`internal/tools/tasks.go`)

`ensureTasksSchema` now also creates, idempotently (the existing ensureSchema
pattern — a pre-FTS memory.db gains the index on first task use, zero
migration scripts), mirroring memory.go's `distilled_qa_fts`/`distilled_qa_tri`
external-content + trigram recipe:

- `tasks_fts` — FTS5, `content='tasks'`, `content_rowid='id'`, columns
  (task_id, session_id, summary) — unicode61 tier for word/ASCII queries.
- `tasks_tri` — same shape, `tokenize='trigram'` — CJK substring tier
  (unicode61 treats a contiguous CJK run as one token; trigram indexes every
  3-char substring).
- `tasks_ai` / `tasks_au` / `tasks_ad` triggers keep both indexes in sync on
  INSERT/UPDATE/DELETE. The ledger is append-only so `_ad` never fires in
  practice, but it is declared anyway (an external-content index silently
  corrupts if a row ever disappears — defense in depth, zero cost).

### 2. `TaskSearch` engine + `gopi task search` CLI (`internal/tools/tasks.go`, `cmd/agent/cli_tasks.go`)

- `tools.TaskSearch(query)` — three recall tiers, first hit-bearing tier wins
  (mixing would double-rank rows present in two indexes):
  1. `tasks_fts` MATCH, BM25-ranked;
  2. `tasks_tri` MATCH (≥3-char CJK substrings), BM25-ranked;
  3. LIKE scan with ESCAPE (recalls <3-char CJK fragments, the trigram lower
     bound).
- Every query is wrapped as a single FTS5 phrase via `ftsQuote` — user input
  can never be interpreted as FTS5 syntax (`-` inside `t-1737…` / UUID ids
  would otherwise act as NOT); output capped at `taskSearchMaxResults` (20).
- CLI: `gopi task search <keyword> [--db PATH]` added to
  `parseTasksArgs`/`taskFlagMember`/usage; existing list/show/new/update
  behavior untouched (no delete verb — append-only iron rule intact).

### 3. Auto-registration (`internal/agent/taskregister.go`, hooks in `internal/agent/loop.go`)

- **START hook** `maybeRegisterSessionTask(briefing)` — called in `runEngine`
  (non-resume only) after the epic user message is logged: inserts a ledger
  row via `tools.TaskNew` with `task_id = session_id = session id`,
  `summary = "[auto] " + briefing` (whitespace-collapsed, capped at 200
  runes, marked `[auto]` to distinguish from manual `gopi task new` rows).
  Sets `a.autoTaskRegistered` so the end hook knows there is a row to close;
  a failed insert leaves the latch false and the end hook becomes a no-op —
  never a half-registered session. Empty briefing skips registration.
- **END hook** `finishSessionTask(exitCode, iterations, tokens, elapsedSec)` —
  called in the runEngine defer right after `LogSessionEnd` (the stats are
  the same numbers that event carries, computed in place — no JSONL re-read):
  `tools.TaskUpdate(sessionID, completed|failed, summary + " — elapsed=…s
  iter=… tokens=…")`. Status: exit 0 → completed, else failed. One row per
  session: the end hook clears the latch before writing, so it cannot fire
  twice.
- **Kill switch** `GOPI_AUTO_TASK_REGISTER` — default ON; read in exactly one
  place (`autoTaskRegisterEnabled`, Rule 2); `0/off/no/false/disabled` turns
  it off (the rest of the boolEnvTrue truthiness convention, inverted).
- **Best-effort** (hard constraint): every DB failure goes through
  `logAutoTaskf` (agent logger or stderr) and is swallowed — the ledger
  records the run, it must not be able to kill it.
- **Rule 3 respected**: no new LLM tool, no prompt change — harness-side only,
  exposed via the existing CLI family. Runtime task state (the LLM's
  [TASK_LIST]) is untouched — the ledger stays an audit layer, Stateless
  philosophy intact.
- **No delete path**: the feature adds create + update only; verified by a
  regression test that greps the tasks engine for `DELETE FROM tasks`.

### Design notes

- `session_id` as task_id was the PA-approved choice (one row per run, join
  against the session JSONL; the memory-feedback dedup precedent already uses
  session ids). Trade-off accepted: a resumed session (`gopi resume`) does not
  re-register (the row exists; the end hook still closes it via the shared
  SessionLog) — actually resume runs create NO row (start hook skipped on
  resume), so nothing is left dangling.
- FTS5 external-content + triggers (not a standalone FTS table): the ledger
  rows stay the single source of truth, search costs one JOIN, and the
  memory.go recipe is reused verbatim — proven in production here.
- LIKE fallback tier exists because 2-char CJK queries ("网络") are below the
  trigram tokenizer's 3-char lower bound — without it, short CJK searches
  would silently miss.

## Verification

- Build: `go build ./...` OK; `go vet ./internal/tools/ ./internal/agent/ ./cmd/agent/` clean.
- Tests: `go test ./...` — all packages PASS (9/9), including new
  - `internal/tools/tasks_fts_test.go`: FTS/trigram/LIKE tiers + trigger sync
    through TaskUpdate, literal-phrase (injection-safe) queries, reopen
    idempotence.
  - `internal/agent/taskregister_test.go`: start hook registers in_progress,
    end hook stamps completed/failed + stats, kill switch, empty briefing,
    summary normalization, no-delete regression.
- CLI live runs (temp-db + real-run verification, GLM live binary):
  - Run 1 (endpoint refused): `gopi task list` shows `#1 <session-uuid>
    [failed] [auto] 演示任务… — elapsed=13.8s iter=1 tokens=0` — the full
    lifecycle (start hook in_progress → end hook failed + stats) on a real
    run.
  - Run 2: `gopi task search 自动登记` → 1 hit (CJK trigram tier);
    `gopi task search <full-uuid>` → 1 hit (task_id column); then
    `GOPI_AUTO_TASK_REGISTER=0 gopi "…"` → `task list` unchanged (kill
    switch honored, no new row).

## Files

- internal/tools/tasks.go — FTS5 companions + TaskSearch/ftsQuote/likeEscape
- cmd/agent/cli_tasks.go — search verb (usage/parse/dispatch)
- internal/agent/taskregister.go — NEW: hooks + kill switch
- internal/agent/loop.go — Agent latch fields + two call sites
- tests as listed above
