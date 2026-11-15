<!-- doc-class: normative -->
# Task Report: TaskIngest mirror write CAS guard (bug031 / epic t-1788063844)

Branch: `feat/taskingest-cas` (base: master e1449e9)

## Context / incident

- Epic: the operator flagged the ingest mirror path as non-atomic toggle: SELECT read
  → Go-side forward-migration check → unguarded `UPDATE tasks SET status=...
  WHERE task_id=?`. A local writer (task serve claim / writeback) could win
  the row inside the check→write gap and be blind-overwritten (e.g. local
  `failed` fact lost to a late remote `completed`, or claimed_by left dangling).
- Reference pattern: TaskNext's atomic claim `UPDATE ... WHERE status='pending'`
  (tasks.go:1900) — CAS, exactly one winner.
- **Duplicate dispatch**: this epic was enqueued twice (#138 bug031 at 16:22,
  #150 at 19:00). The #138 worker ran in the same checkout and, at 19:23
  (commit e1449e9), archived this session's in-progress test file to
  `devdoc/archive/taskingest-cas-test-archived.go` believing ingest was being
  retired — no retirement code or devdoc exists; TaskIngest remains live
  (cmd/agent/cli_tasks.go:681, help.go:213). This session re-applied the fix
  on a branch per Rule 6. Lesson: single-checkout concurrent agents need
  worktree separation (Rule 6a) — see memory.
- The archive commit also **broke `go build ./...` on master**: a raw `.go`
  file under `devdoc/` is compiled by the package walk. Fixed here with
  `//go:build ignore`.

## Completed

- `internal/tools/taskingest.go`: mirror UPDATE now carries the SQL
  forward-migration guard `AND status IN ('pending','claimed','in_progress','reopened')`
  (constant `taskIngestMirrorableSourceStatuses`; reopened is non-terminal
  and mirrorable). RowsAffected=0 → re-read → explicit policy:
  - terminal now: same status → idempotent "already"; different → 409
    conflict naming the current state (failure fact never silently lost).
  - non-terminal now (claim advanced inside the window): legal, refresh
    snapshot (`ingestMirrorFields`) and retry, capped by
    `taskIngestSQLRaceRetries` (2) → 409 "kept moving" beyond that
    (no livelock).
  - All magic numbers / status literals are named constants (Rule 1/2).
- `internal/tools/taskingest_test.go` (new): deterministic race-injection
  via test-only `taskIngestRaceHook` (nil in production):
  - `TestTaskIngestRaceWithLocalClaim`: claim lands in the gap → mirror
    absorbs it (claimed is non-terminal), **claimed_by provenance survives**
    (mirror UPDATE never touches claim columns).
  - `TestTaskIngestRaceTerminalConflict`: local `failed` lands in the gap →
    409 conflict, failed fact + summary survive.
  - `TestTaskIngestConcurrentClaimVsIngest`: real goroutine race (TaskNext
    claim vs ingest), 3 rounds, both orderings end consistent.
  - Existing `tasks_ingest_test.go` (HTTP handler, idempotent redelivery)
    still passes unchanged — Epic ② no regression.
- `devdoc/archive/taskingest-cas-test-archived.go`: `//go:build ignore`
  (fixes master's broken build).

## Not Done

- Local TaskUpdate (tasks.go:1291) shares the same read-then-write shape but
  is CLI single-operator; per epic, CAS there is deferred (noted, not
  implemented).
- The parallel worker's M4.9 "forward-migration design" has no devdoc yet —
  if a different design lands later, this branch's guard remains compatible
  (it only narrows the WHERE clause).

## Verification

- Build: `go build ./...` OK
- Tests: `go test ./internal/tools/ -run TestTaskIngest -race -count=1` → ok
  (includes idempotent redelivery ② and both race ① tests)
- Full suite: see "Full suite" below (background run at commit time)
- Commit: <this branch, feat/taskingest-cas>
