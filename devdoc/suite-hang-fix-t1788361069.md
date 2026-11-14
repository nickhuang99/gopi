<!-- doc-class: normative -->
# Task Report: Fix internal/tools full-suite hang (t-1788361069)

## Summary

The `go test ./internal/tools/` suite hung past 1800s with the panic dump
showing dozens of `database/sql.(*DB).connectionOpener` goroutines blocked
in `select` — historically striking whichever test opened a DB next
(`TestImportReRunIsIdempotent` in the epic; `TestAuditMergeBacklog` in an
earlier session's dump). Root cause found, fixed, and verified: the whole
suite now passes in **291s** (down from a >1800s hang).

## Root Cause

A fresh SQLite DB's schema init runs **40+ DDL statements**, each its own
autocommit. Under the DEFAULT rollback journal + `synchronous=FULL`, every
commit does a full `fsync`:

- Measured **~155ms per DDL** on a loaded disk → ~5s+ per fresh DB, and the
  internal/tools suite creates hundreds of fresh temp DBs.
- ONE stalled `fsync` can block for minutes while holding modernc's
  process-wide sqlite mutex — freezing every later `sql.Open` (its
  `connectionOpener` waits forever). The test that "hangs" is incidental:
  it is simply the next one to open a DB.

**Not a poisoner test.** A per-file bisect (every earlier test file + the
blamed test in one `-run`) proved no single file reproduces the hang. It is
cumulative: ~40 DDLs × 155ms per fresh DB, and under I/O contention one
fsync stalls minutes.

## The Fix

### 1. `enableWAL()` before any schema DDL (the real fix)

`PRAGMA journal_mode=WAL` + `PRAGMA synchronous=NORMAL` at the TOP of
`initKnowledgeBase` / `migrateDefaultTriggers`, before the first CREATE.

- Measured: rollback journal 155ms/DDL → WAL 29ms/DDL → **WAL+NORMAL
  0.2ms/DDL (775x)**. Fresh init: 1.6s+ → **230ms**.
- `synchronous=NORMAL` under WAL still fsyncs at checkpoint; durability is
  only relaxed for the WAL tail on power loss — the same tradeoff the
  end-of-schema best-effort WAL already accepted.

**Deliberately NOT in `openDB`:** openDB probes FOREIGN DBs (the ownership
check reads a file before knowing it is gopi's), and `journal_mode` is a
persistent header property — setting it in openDB mutated foreign files
(broke `TestMemorySearchForeignFileRejected`'s "foreign file was mutated").

### 2. Per-path init lock + bounded schema poll (race guard exposed by WAL)

WAL readers no longer block on the writer, so a concurrent first-opener can
read a **half-created schema** mid-init and wrongly reject a fresh file as
foreign (this surfaced as `TestMemorySearchNonBlocking` failing — 4
goroutines racing a fresh shared DB).

- `dbInitLocks` (sync.Map path→mutex) + `initKnowledgeBaseOnce` serialize
  first-open init per path.
- `waitForDistilledSchema` (20×25ms poll) gives a concurrent initializer a
  brief window before the foreign-file rejection.

## Files Changed

| File | Change |
|---|---|
| `internal/tools/memory.go` | `enableWAL()` helper; called at top of initKnowledgeBase + migrateDefaultTriggers; per-path init lock (`initKnowledgeBaseOnce`, `ensureKnowledgeBase`); `waitForDistilledSchema` wired into openSearchDB/openStoreDB reject paths |
| `internal/tools/tasks.go` | openTasksDB first-open routed through `ensureKnowledgeBase`; same wait-before-reject |
| `internal/tools/memory_db_init_regression_test.go` | NEW: TestFreshDBInitIsFast (<2s) + TestManyFreshDBsNoHang (40 fresh DBs) |

## Verification

- **Full suite** `go test ./internal/tools/`: **291.459s PASS** (was hanging
  >1800s) — on the default /tmp filesystem, the exact hang environment.
- **Whole repo** `go build ./...` + `go test ./internal/... ./cmd/...`:
  all OK (internal/tools 311s, cmd/agent 81s, all packages green).
- Targeted: TestImportReRunIsIdempotent 0.34s (was 3.8s solo, hung in
  suite); TestFreshDBInitIsFast 0.25s; TestManyFreshDBsNoHang 13.98s;
  TestMemorySearchNonBlocking, TestMemorySearchForeignFileRejected,
  TestMemorySearchDefaultPathHeals, procedural + sync families all PASS.
- Regression guards: both new tests pass post-fix; TestFreshDBInitIsFast
  would fail (>2s) on the pre-fix rollback-journal path.
- `go vet ./internal/tools/`: clean.

## Commits (branch `feat/fix-internal-tools-suite-hang`)

- `bcfe9d9` fix(suite-hang): WAL+NORMAL before schema DDL — kills per-DDL fsync storm (t-1788361069)
- `b9243b2` fix(suite-hang): route openTasksDB first-open through per-path init gate
- `5e8d8ea` test(suite-hang): regression guards for fresh-DB init speed and suite-order safety

## Not Done / Notes

- The trailing `PRAGMA journal_mode=WAL` at the end of ensureSchema /
  ensureProceduralSchema is now redundant (enableWAL runs first) but kept
  as harmless best-effort — it also migrates legacy rollback-journal DBs on
  their first heal.
- Root cause lesson stored in memory (id=1308) for future agents.
