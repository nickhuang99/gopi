<!-- doc-class: historical -->
# Task Report: M4.11 queue-priority epic — verification & acceptance (t-1788087991)

## Context
M4.11 was implemented on branch `feat/task-priority-tags` (commit 66e9d24) and
merged to master at **308014a** (branch since deleted — inspect the merge diff).
This session = post-merge verification of the full Epic against the merged tree.

## Completed
- **Feature audit (merge diff 607d8c5..308014a)** — all 5 Epic rules present:
  - ① tags 含 bug → priority=1 (`taskTagPriority`, tokens starting with `bug`
    count; exact-word guard prevents `debugbug`-style false hits)
  - ② feature/P1.5/新能力/unmapped → 5 (default tier)
  - ③ docs/doc-only → 8
  - ④ explicit `--priority` (CLI) / `priority` arg (task_submit tool) always
    overrides tags (checked in TaskSubmit: `if opts.Priority <= 0`)
  - ⑤ LLM self-submits (source=llm, tags=bug) flow through the same
    TaskSubmit → same bug=highest mapping
- **One mapping, one place**: `taskTagPriority` in internal/tools/tasks.go is
  the single tags→priority function; schema backfill, TaskSubmit, memsync
  delta inserts (memory_sync_delta.go) and sync imports
  (memory_sync_task.go importTaskFresh) all call it — no SQL/Go parity drift.
- **TaskNextFiltered ORDER BY** `COALESCE(priority,5) ASC, created_at ASC, id ASC`
  (M4.11 main body) — verified in source.
- **Legacy backfill**: `ensureTasksSchema` re-derives every NULL-priority row
  through `taskTagPriority` (idempotent, never touches non-NULL rows) —
  existing pending bug rows re-tier to 1 on first DB open.
- **Unit tests**: 6 tests in internal/tools/tasks_priority_test.go (mapping,
  derive-from-tags, explicit-override, next-ordering, legacy backfill, show
  line). Full `go test ./internal/tools/ -run TestTask -count=1`:
  **ok 137.2s, exit=0**.
- **Build**: `go build ./...` OK; `CGO_ENABLED=0 go build -o bin/gopi-m411 ./cmd/agent/` OK.
- **E2E (real binary, scratch ledger /tmp-m411/t.db)**:
  - ① bug submit → `priority: 1`; feature → 5; `doc-only --priority 3` → **3**
    (show line verified — explicit beats tags).
  - ② Claim order via `gopi task run next`: bug(1) → prio-3 doc → feature(5)
    — bug jumps the line; explicit --priority 3 docs row slots between tiers.
  - ③ Legacy NULL-priority bug row: on next DB open the backfill stamped
    priority=1 and `task run next` claimed it **first** (before newer rows).
  - `task show` renders the `priority:` line; legacy NULL displays as
    default tier (prioOr fallback), not bare 0.

## Not Done
- No code changes were needed — M4.11 was already complete on master. This is
  a verification-only task; nothing new committed (tree untouched, scratch
  dir removed).
- `gopi task next` CLI verb is gone since M4.5 (manual claiming removed) —
  E2E ordering was exercised through `task run next` (the supported path).

## Verification
- Build: `go build ./...` OK; CGO binary OK
- Tests: `go test ./internal/tools/ -run 'TestTask' -count=1` → ok 137.2s (exit 0)
- E2E: 3 submits (bug/feature/doc+prio3) + 3 claims → 1 → 3 → 5 order exact;
  legacy NULL bug row backfilled to 1 and claimed first
- Commit: no new commit (verification of master@308014a)

## E2E Gotchas (for future testers)
- `gopi task run next` claims AND executes — a pending row with a real
  payload spawns a child gopi run. Use scratch ledgers; don't point
  GOPI_MEMORY_DB at your real memory.db.
- Payload-less rows are deliberately skipped by TaskNextFiltered (bug028
  anti-bare-briefing) — a "queue looks empty" test fixture with empty
  payloads is a fixture bug, not an ordering bug.
- `--db PATH` accepts the tasks ledger only if it carries the gopi
  memory_feedback table (openTasksDB foreign-DB guard); simplest E2E is
  `GOPI_MEMORY_DB=<scratch>` without --db.
