<!-- doc-class: normative -->
# memsync import-tasks atomicity — the recovered fix

**Status:** implemented on `feat/memsync-import-atomic` (this worktree).
**Epic:** recover the lost import-atomicity fix (archived branch
`archive/fix-memsync-import-atomic-20260901`, tip `67264535`, 5 commits /
482 insertions, never merged — merge-backlog row `t-1788218295` went *verified*
while unmerged).

## The defect

`internal/tools/memory_sync_task.go`'s `SyncTaskImport` applied a patch through
helpers that each opened their **own** short-lived connection
(`taskSyncDB()` → `openTasksDB`), with per-statement **auto-commit** and no
transaction boundary. A mid-patch storage failure — `SQLITE_BUSY` while a peer
or a worker held the write lock, disk I/O, a crash — therefore left a **partial
patch** applied, and the residue was indistinguishable from real work.

The two failure modes were also conflated: every per-row error was appended to
`stats.RowsRefused` and the loop continued, so even a genuine storage failure
was reported as a row-level policy judgment.

## The fix

| mechanism | where |
|---|---|
| ONE transaction for the whole patch | `importTaskBeginTx` (`memory_sync_task.go`) |
| BEGIN IMMEDIATE (write lock at BEGIN) | `openTasksDB` → `openDB` DSN `_txlock=immediate` (already present) |
| commit / rollback | `tx.Commit()` at the end of the loop; `defer closeTx()` rolls back every early return |
| tx-scoped helpers | `importTaskInsertTx`, `taskByIDTx` (new, `tasks.go`), `syncTaskLiveDedupHitTx`, `taskTagsUnionTx`, `syncTaskMoveStatusTx`, `terminalConflictRulingTx`, `importTaskMirrorTx` |
| refusal vs storage triage | `errImportRefused` / `errImportIDTaken` sentinels + `isImportRefusal` |
| busy window | `dbBusyTimeoutMSec` 5000 → 15000 at its single definition (`memory.go`) |
| notify correctness | importer status moves buffered on `taskNotifyBus`, flushed **past** the commit |

### The BUSY-vs-partial contract (documented at the call site)

`importTaskBeginTx`'s doc comment carries the contract a future reader needs:

- **Contention queues, it does not fail mid-patch.** `BEGIN IMMEDIATE` is issued
  at BEGIN, so a concurrent writer makes the import *wait* before a single row
  is written (measured on this driver: 25.08 s wait against a foreign
  `BEGIN IMMEDIATE` holder, i.e. the `busy_timeout` pragma does not cap a BEGIN
  handoff — see "Not Done" below).
- **A storage failure aborts the whole batch.** Zero rows land and the error is
  explicit — `runMemsyncTaskImport` routes it through `memsyncExitFatal`, so the
  operator's shell sees a non-zero exit and the re-run applies the patch in
  full. (A *policy* refusal is scoped differently by design: the CLI prints the
  refused rows on stderr and exits `2` after the batch commits — the committed
  rows are good, only the refused ones were skipped.)
- **A retry is always safe.** A patch that did land committed in full, and
  re-importing it is idempotent: the payload-hash gate reports `duped`, the
  `UNIQUE(task_id)` arbiter reports `already`.
- **Exactly-once per `task_id` holds in every outcome** — the loser of a write
  race wrote nothing.

### Refusal vs storage failure — why sentinels, not message matching

The archived diff classified by substring (`SQLITE_BUSY`, `database is
locked`, `UNIQUE constraint`, …) and treated **everything else** as a refusal.
That is the defect shape wearing a new hat: an unanticipated mid-patch storage
error (a trigger/schema error, a driver message that changes spelling) would be
read as a *policy refusal*, the loop would record it and continue, and the rows
already inserted earlier in the transaction would **commit** — a partial import
with a green report.

The shipped triage inverts the default:

- a refusal is an explicit sentinel (`errImportRefused`), built by
  `importRefusal(...)` at the four policy sites (invalid source, empty summary,
  non-forwardable/id-collision refusals, a CAS that found no row) plus
  `errImportIDTaken` for a lost `task_id` race;
- **every other error is a storage failure** and aborts the tx.

A refusal writes nothing, so continuing past it cannot split a patch. A storage
failure may already have written rows, so continuing is exactly what the epic
exists to make impossible.

## Verification

All commands below ran in this worktree (`~/workspace/gopi-memsync-import-atomic`).

- **Build:** `go build ./...` → exit 0.
- **Gates:** `go test -count=1 ./internal/standards/` → `ok` (the machine gates
  of Rules 16–21 stay green; the first run red-flagged one of my own lines
  — `fmt.Errorf("%w: %v", …)` at the insert's `errImportIDTaken` wrap — fixed to
  `%w: %w` before commit). `go vet ./internal/tools/` → clean.
- **Targeted:** `go test ./internal/tools/ -run 'TestTaskSyncImport' -count=1`
  → `ok` (all import tests, old and new, green).
- **Acceptance lane** (`-run 'Sync|Import|Bidir' -timeout 25m`): green **except**
  `TestFirstOpenSyncModeDecisionTable`, which fails **identically on master at
  `17cec86b`** in a clean checkout (`firstOpenSyncMode(...) = "NORMAL", want
  "OFF"` — a tmpfs/device-classification mismatch of this host, unrelated to
  this change, and the only failure in the lane).
- **Red-proof** (`TestTaskSyncImportMidPatchStorageFailureLeavesNoRows`):
  temporarily restoring the pre-fix write pattern (one handle, **commit per
  row** — the injection is marked `REDPROOF-START`/`REDPROOF-END` so it can be
  replayed) and re-running the test produced, verbatim:

  ```
  memory_sync_task_test.go:1013: PARTIAL IMPORT: 3 of 6 rows landed (error reported:
    memsync import-tasks: row 3 (t-atomic-probe-3): insert: constraint failed:
    memsync probe: injected storage failure (1811)) — the patch must be all-or-nothing
  --- FAIL: TestTaskSyncImportMidPatchStorageFailureLeavesNoRows (0.04s)
  ```

  With the transaction in place the same test passes (zero rows, explicit
  error, and the retry applies all 6). The raw evidence is kept outside the
  branch: `~/workspace/gopi-memsync-evid/redproof.txt`.

### The tests

| test | what it pins |
|---|---|
| `TestTaskSyncImportMidPatchStorageFailureLeavesNoRows` | **the red-proof.** An injected mid-patch storage failure (a persistent `BEFORE INSERT` trigger raising `RAISE(ABORT, …)` on row 3 of 6) leaves **zero** rows and an explicit error; the refusal path must not absorb it; then the failure is removed and the same patch imports **whole** |
| `TestTaskSyncImportUnderWriteLockIsNeverPartial` | a foreign `BEGIN IMMEDIATE` holder (`testdata/memsync-midpatch-hold.sh`): the import may wait or fail, but a reported success means all rows and a failure means none |
| `TestTaskSyncImportThenRetryIsIdempotent` | the contract the atomic failure rests on: nothing to clean up, the re-run applies fully, a third run is a no-op (`4 duped`) |
| `TestTaskSyncImportConcurrentSamePatch` | two importers, one ledger: exactly one row per patch task; the loser either queues and reports `already`/`duped` or fails BUSY having written nothing |

Two notes for the next reader, both learned the hard way here:

1. **The probe rows must be textually distinct.** The import's M4.8 dedup ladder
   (exact → canonical → trigram-similar) reports near-identical work as twins;
   my first draft used `"atomic probe work %d"` and the lane *emptied the
   patch* into `duped` before it reached its write phase (visible as
   `RowsImported: 1, RowsDuped: 3`). `memsyncProbeTexts` now gives each row a
   different sentence.
2. **A bound parameter is not allowed in a trigger body** ("trigger cannot use
   variables"), so the injected condition is inlined as a quoted literal
   (test-owned constant).

## Not Done (honest)

- **A real mid-patch `SQLITE_BUSY` could not be produced on this host, and the
  red-proof does not fake one.** Measured: against a foreign `BEGIN IMMEDIATE`
  holder the import's own `BEGIN` waited **25.08 s** and then succeeded, *past*
  `busy_timeout=15000` on the handle (pragma verified inside the test process).
  So on this driver a lock handoff queues rather than expiring, and no honest
  test can make "the import fails BUSY mid-patch" happen by contending. The
  red-proof therefore injects a **storage failure** — the class the triage
  treats identically — which is deterministic, costs milliseconds, and is
  strictly stronger for regression purposes (it stays red if someone later
  re-introduces per-row commits, and it cannot be flaky by construction). The
  contention half is still asserted, in the form the host actually exhibits
  (`...UnderWriteLockIsNeverPartial`).
- **The archived branch's helpers are not all carried over verbatim.** Master
  drifted after 2026-09-01 and its import loop is the newer semantics; the
  archived `syncTaskLiveHashHitTx` is superseded by master's dedup ladder
  (`syncTaskLiveDedupHitTx`), and the archived `importTaskFresh`'s
  `TaskSubmit`-based enqueue is replaced by `importTaskInsertTx`, which mirrors
  the shared `insertTaskRow` column set (so the text-boundary guards and the
  priority derivation stay on ONE path). The functions the archived diff
  *deleted* (`syncTaskByID`, `syncTaskLiveHashHit`, `importTaskFresh`,
  `importTaskExisting`, terminalConflictRuling's old form, …) are still present
  on master's file in their original names/shapes; I left them where master's
  other callers may still need them rather than deleting on this branch.
- **The archived test `memory_sync_delta_test.go` pin (`t.Setenv(config.SyncPeersEnv,
  …)` in `TestSyncDeltaServerResolvesConflict`) is not in this diff** — master
  already carries it (`internal/tools/memory_sync_delta_test.go:323`).
- **The archived devdoc was not copied.** This file is written against the code
  actually shipped here; the archived text (`67264535:devdoc/memsync-import-atomic.md`)
  described the pre-drift tree.
- **`SyncTaskImportDelta` and the brew channel `SyncImport`** are out of scope
  (the latter was already fixed upstream by `1dcee44`); the delta channel still
  writes row-by-row and is left as its own task.
- **No merge performed** — merging is the PA's step (Rule 6/9).

## Recovery

- Branch: `feat/memsync-import-atomic` (commit `cf314174`)
- Evidence kept outside the branch: `~/workspace/gopi-memsync-evid/` (archived
  diffs, the archived devdoc, the red-proof transcript, the pre-injection file).
- Source of the recovered fix: `git show archive/fix-memsync-import-atomic-20260901:<path>`,
  `git diff master...archive/fix-memsync-import-atomic-20260901`.
