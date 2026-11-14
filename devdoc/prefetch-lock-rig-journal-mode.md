<!-- doc-class: historical -->
# The lock rig's journal-mode flip needs exclusive access — build the mode in instead

Task `01a13db4-b931-7230-9da3-83566f5b06dc`, 2026-10-15, peerbox.
Branch `task/01a13db4b93172309da383566f5b06dc`, commit `4de8ca0bb`.

## The symptom, as filed

`TestPrefetchBusyIsRetriedWithinTheSameBudget` failed in a three-package budget
run and passed under `go test ./cmd/agent -run … -count=3`:

```
chat_prefetch_segments_test.go:470: rollback journal on the holder: database is locked (5) (SQLITE_BUSY)
```

Line 470 was `holder.Exec(tools.LedgerRollbackJournalPragma())`. The inference in
the briefing — "another connection to that ledger exists concurrently" — was
correct, and this is the measurement of it.

## The mechanism, measured (not inferred)

A probe drove the exact rig sequence (seed the fixture through `seedPrefetchLedger`,
open a holder, flip the journal), removing one variable at a time. Raw output:
`artifacts/mech-probe.txt`.

| arm | condition | result |
|---|---|---|
| A | the rig sequence, **no other handle ever opened**, 300 iterations | **0/300 refused** |
| B | the same flip with ONE **idle** sibling `sql.DB` merely OPEN on the file | `database is locked (5) (SQLITE_BUSY)` after **444 µs** |
| C | the same flip with that sibling **mid-read** | refused after **15 µs** |

So: `PRAGMA journal_mode = DELETE` needs EXCLUSIVE access to the file, and it is
refused while **any** other connection to that file is open — including one that
is pooled and doing nothing. 300/300 green alone is what makes the flip look
safe; the 444 µs refusal is what happens the moment a sibling exists.

### Which connection is it, and which phase

The sibling is **the fixture's own seed** — `seedPrefetchLedger` opens through
`sql.Open(...)` and closes the `*sql.DB`, but `database/sql` pools connections
and the file's connection is still being torn down (or is simply still open in
the pool) when the rig's flip runs microseconds later, in the same test.

The failure is therefore not about another *test*, and not about load: the pool
state and the scheduler decide whether the seed's connection has been released by
the time the flip runs. That is exactly the shape that appears only in the
full-package run — where allocation, GC and 10k other tests change that timing —
and never in an isolated `-run … -count=3`, where it is 0/300.

Cross-check from the filed stream
(`queue/test-budget/run-1792035812-2539841.json`): the test immediately before it
was the only other prefetch case in flight — `TestPrefetchTopNShapeKeepsProductionRanking`
(0.001 s later). No concurrent sibling test is required for the flip to be
refused; the seed alone is enough, which the probe's arm B reproduces with a
single idle handle.

## The fix: build the file in the mode, do not flip it

`internal/tools.LedgerRollbackJournalDSN(path)` — production's pragma list
(`busy_timeout`, `synchronous(NORMAL)`) with `journal_mode(DELETE)`. A file whose
**first** connection asks for the rollback journal is **created** in that mode
(measured: `PRAGMA journal_mode` reads back `delete` on the fresh file), so no
exclusive moment is ever needed:

* arm B of the probe: `BEGIN EXCLUSIVE` on that file takes **258 µs** with the
  sibling still attached — no refusal, because there was never anything to flip.

`ledgerPragmasIn(journalMode)` is the ONE renderer; `ledgerPragmas()` is it with
WAL, so the timeout value is still rendered in exactly one place (Rule 2).

### Why the mode belongs to the RIG, not to the seed

The same seed feeds rigs that measure WAL facts — `TestPrefetchSessionHandlePaysNoDial`
reads the warm handle's `journal_mode` back as the connection state. Giving
`seedPrefetchLedger` a rollback journal would change what those tests observe.
So: `seedPrefetchLedger` / `seedTurnLedger` stay on production's contract, and
the five lock rigs ask for their own mode through
`openPrefetchLockRigLedger` / `seedTurnLedgerInRollbackJournal`:

* `chat_prefetch_segments_test.go` — `TestPrefetchBusyIsRetriedWithinTheSameBudget`,
  `TestPrefetchBusyGiveUpIsReportedAsBusy` (the latter is `heavytest`-gated and
  skips on the merge path);
* `chat_prefetch_test.go` — `TestPrefetchGivesUpOnABlockedLedger`;
* `chat_prefetch_turn_test.go` — `TestTurnAdoptionNeverWaitsLongerThanItsBound`.

`tools.LedgerRollbackJournalPragma` is **kept**: `internal/tools`' own guard
(`TestLedgerRollbackJournalPragmaIsWhatMakesAHolderRefuse`) is its subject, and
that guard has no sibling, so its flip is the 300/300 case.

## What the fix does NOT weaken (Rule 13/14, and the guard)

1. **No second DSN spelling at a call site.** The new mode goes through a
   `tools` DSN helper, so every opener stays a `sql.Open(prefetchDriverName,
   tools.<Helper>(path))` call — the shape `cmd/agent`'s contract gate
   (`TestLedgerOpenersUseProductionDSNContract`) reads. The helper is admitted to
   its allow-list **by name, in the same commit**, exactly like
   `LedgerMemoryDSN` was. Measured after the first pass: the two sites that
   passed the DSN *as a function value* (`func(string) string`) were correctly
   flagged as bare openers — the gate is not vacuous, and the seeder plumbing was
   reshaped (an `open func(string) (*sql.DB, error)` closure carrying the helper
   call) rather than the predicate widened.
2. **The WAL guard still holds.** `assertLockRigLedgerIsInRollbackJournal` reads
   the file's own mode back through a fresh connection and fails loudly if it is
   not the rollback journal — the anti-vacuity property memory #9764 recorded for
   this family ("WAL makes lock-holder rigs vacuous"). Its readback opener uses
   `tools.LedgerReadOnlyDSN`, deliberately **not** `tools.LedgerDSN`: a
   connection that *declares* `journal_mode(WAL)` would put the file back in WAL
   and turn the guard into the defect it exists to catch (measured).
3. **The busy timeout and the single pooled connection** are untouched: the path
   still comes from `testutil.NewTestDBPath` (Rule 32), the DSN still carries
   `dbBusyTimeoutMSec`, and every rig keeps `SetMaxOpenConns(1)`.

## The regression guard

`TestPrefetchLockRigLedgerNeedsNoExclusiveFlip` (`chat_prefetch_dsn_rig_test.go`)
holds both arms in one test:

* **RED** — the old shape, on the file the old rigs used: the post-hoc flip with
  one idle sibling attached is refused **32/32** times. If this arm ever goes
  quiet the flip has become safe again and the fix's premise should be
  re-derived, not kept;
* **GREEN** — the new shape: the ledger created by `tools.LedgerRollbackJournalDSN`
  reads back in the rollback journal, and `BEGIN EXCLUSIVE` succeeds with that
  same sibling attached.

`assertLockRigLedgerIsInRollbackJournal` is called by each of the five rigs, so a
rig that quietly drifts back to WAL fails by name instead of passing vacuously.

## Honest limits

* The **original failure was not re-observed** on the pre-fix commit at the same
  load — a three-package pre-fix run takes ~11 minutes and the session budget
  forbade a controlled before/after pair. What IS measured is the mechanism: the
  flip is refused **32/32** times in the guard with one idle sibling, and
  444 µs under the probe. That is a deterministic reproduction of the condition,
  not of the run's timing.
* The guard's red arm is a fixture-level reproduction (idle sibling), not the
  seed-pool race; the pool race is measured by arm A vs arm B of the probe, whose
  log is archived rather than asserted.
* `TestPrefetchBusyGiveUpIsReportedAsBusy` is `heavytest`-gated and therefore
  **skips** on the merge/verify path; it was converted for consistency and is
  exercised only under `GOPI_HEAVY_TESTS=1`.
