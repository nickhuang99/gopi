<!-- doc-class: historical -->
# Task Report: uuid task_id generation + sessions/<uuid>/ (SPEC DB-cleanup Phase)

## Completed

Auto task_id generation now mints uuid v7 instead of t-<unix>. All auto-id paths
(TaskNew / TaskSubmit / TaskNewOID auto-register) converge on `taskIDGenerate`
(insertTaskRow empty-taskID path), so the swap is one function.

1. **Generator** (`internal/tools/tasks.go` `taskIDGenerate`): `uuid.NewV7()` per
   attempt; existence-check + retry loop preserved (defense-in-depth). Consts
   `taskIDPrefix`/`taskIDCollisionSep` are now LEGACY-only markers — used by
   display to recognize pre-uuid rows (append-only data untouched). `go.mod`:
   google/uuid promoted indirect → direct (v1.6.0).
2. **TaskSubmit** (`tasks.go`): mint uuid at top when no explicit --task-id;
   explicit ids keep working (test fixtures t-* unchanged). Every minted submit
   creates `sessions/<uuid>/` via `taskEnsureTaskSessionDirs` (attachments/ +
   metadata.json skeleton) and copies attachments into
   `sessions/<uuid>/attachments/`. Submit returns the uuid.
3. **task_session.go integration**: the unified container `sessions/<task_id>/`
   (S1 from the prior phase) is already the target of `TaskCopyAttachments` and
   `taskSessionDir`/`taskEnsureTaskSessionDirs` — works unchanged for uuid.
   New helper `taskWriteSubmitMetadata` writes the submit-time metadata.json
   (task_id/status/summary/source/submitted_at/priority/tags/payload/manifest).
4. **metadata merge**: run-time `writeMetadataJSON` (agent/session.go) was
   overwriting metadata.json; it now read-modify-write MERGES, so submit-time
   fields survive the run (regression test added).
5. **Display**: `taskHashDisplay` dual-render — legacy `t-<unix>` → digits
   (unchanged); uuid shape (36 chars, hyphens at 8/13/18/23) → `#<first-8>`.
   Everything else falls back to full id. task_events / session_conclusions
   store task_id opaquely (no shape assumption).
6. **Tests updated** for uuid shape / `Task queued:` parse where they asserted a
   t- prefix; uuid display cases added; dual t-*/36-char extraction helpers in
   adoption/notify/memory_sync tests.

## Not Done

- Legacy 1267 t-<unix> rows untouched (append-only) — still fully readable.
- No session-dir migration, no queue-semantic changes.
- No short-ref resolver (`task show <first8>`): submit returns the full uuid and
  lookups are exact-id. Deferred by design (SPEC did not require short refs).

## Verification

- Build: `go build ./...` OK (targeted runs + full suite compile).
- Tests: full `go test ./... -count=1 -timeout 2400s` on tree 8598c16 — **ALL
  14 packages PASS** (SUITE_EXIT=0): cmd/agent 134.9s, internal/agent 56.2s,
  internal/tools 377.6s (the slow real-sleep package that tripped go test's
  default 600s per-package ceiling in earlier attempts — explicit
  `-timeout 2400s` is required for a truthful full-suite run), testsuite,
  all others ok. ⑥ 全量 go test green.
- Targeted pre-suite runs (already PASS): internal/tools engine 14.4s, agent
  register 3.7s, agent session-logger 1.1s, acceptance container test.
- Commit: `1bdc451 test(uuid-taskid): acceptance — submit returns uuid +
  sessions/<uuid>/ container` (+ `2aa0bd0 feat(uuid-taskid): task_id
  auto-gen → uuid v7 + sessions/<uuid>/ submit skeleton`)
- Acceptance test `TestUuidTaskIDSubmitCreatesSessionContainer` PASS (①②⑤④).
  Full suite ⑥ certified by background job-4 (see above).

## Branch

- Branch: `feat/uuid-taskid` (worktree ~/workspace/gopi-uuid-taskid)
- Recovery: `git show feat/uuid-taskid:devdoc/uuid-taskid-phase.md`
