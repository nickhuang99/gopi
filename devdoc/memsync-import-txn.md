<!-- doc-class: normative -->
# Devdoc: memsync import single-transaction atomicity (feat/memsync-import-txn)

Task: #280 (foresee-instead-of-fix) — memsync export/import writes via
openTasksDB with no cross-process lock. Question: import half-fails →
partial rows in DB? Where is the transaction boundary?

## Answer (before fix)
**No transaction boundary.** SyncImport walked rows one-by-one through the
store core on its own connection; a mid-patch hard DB error (SQLITE_BUSY
after busy_timeout, disk I/O, FTS corruption) left already-applied rows in
the DB. Per-row "refusals" and hard failures were indistinguishable in
SyncImportStats — a silent partial import.

## Fix (commit 1dcee44, branch feat/memsync-import-txn)
1. `openDB` appends `?_txlock=immediate`: BEGIN takes the write lock at
   BEGIN, not at first write. A concurrent writer queues on busy_timeout
   up front. Reads (search) never BEGIN — unaffected.
2. `SyncImport` wraps the WHOLE patch in ONE `BEGIN IMMEDIATE`
   transaction, handed into the store core via the new `QueryExecer`
   interface (both *sql.DB and *sql.Tx satisfy it — one core
   implementation, no duplicated write path).
3. Hard DB failure mid-patch → `tx.Rollback()` + explicit error
   "rolled back (no rows applied)" — the DB is exactly as before.
4. `dbBusyTimeoutMSec` 5s → 15s: a whole-patch import legitimately holds
   the write lock longer than a single-row write.
5. Per-row SEMANTIC refusals (garbage gate / validation / human-wins)
   still count as `RowsRefused` inside the committed tx — decisions, not
   failures. `ErrGarbageRefused` moved from const-block (invalid —
   errors.New is not constant) to a `var` sentinel.

## Verification
- `go build ./...` OK
- `go test ./internal/tools/ -run TestSync` OK (2.9s)
- `go test ./cmd/agent/` OK
- Full-suite failures (TestAgent_Run_ContextTrimReplaceSlimming,
  TestAgent_Run_ToolResultPayloadOnce, TestEnsureTagsRegeneratesForNewerTS)
  reproduce on CLEAN master → pre-existing baseline, not this change.

## Two-process concurrent acceptance (c2bda60, the Epic's acceptance bar)

`TestSyncImportTwoProcessConcurrent` (memory_sync_txn_test.go): two REAL
child processes run the real `SyncImport` against the SAME DB with the same
20-row patch, schema pre-seeded (the first-open race is a separate known
issue — this test isolates the transaction boundary).

- PRE-FIX baseline (same test on clean 308014a): **FAILS exactly as the
  Epic predicted** — 24 rows instead of 21; questions 00/02 stored twice
  (interleaved autocommit: each process's dedup SELECT missed the other's
  just-inserted row); per-process stats stored=19/4.
- POST-FIX: 5 consecutive runs green. Convergence pattern: one process
  stored=20, the other stored=0 deduped=20 — the loser queued on
  BEGIN IMMEDIATE (busy_timeout), then saw the winner's committed rows and
  deduped into them. Row count exactly 1 seed + 20, every question ×1.

## Deliverable
- Branch `feat/memsync-import-txn`, commits `1dcee44` (fix) + `cb4c6d4`
  (devdoc) + `c2bda60` (concurrency E2E test)
- Recovery: `git show feat/memsync-import-txn:internal/tools/memory_sync.go`
