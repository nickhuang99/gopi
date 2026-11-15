<!-- doc-class: normative -->
# An absent ledger is the empty answer — the ledger half of a retirement must not cancel the durable half

**Class:** historical (a past run's record). Task
`01a139c207f2762dab8db2132f03b467`, 2026-10-14, branch
`task/01a139c207f2762dab8db2132f03b467`.

## The defect, and how it was measured

`cmd/agent`'s `TestMergeUnrelatedHistoryBranchIsDiagnosedNotAnError` fails at
`merge_unrelated_history_test.go:156` on an uncontended package run at HEAD
`7377c4273`:

> **Retired mechanism (merge verb removal, 2026-11-01).** The output quoted below
> is the merge face's own, printed in 2026-10, and is kept verbatim as the record.
> The top-level `gopi merge` verb was removed: `./bin/gopi merge` exits **1** today,
> and `gopi task merge` runs the same engine and prints the same verdict.

```
gopi merge: … feat/orphan-<test> unrelated-history  shares no commit with master …;
  left as it is, NOT retired: archive retirement reconcile: no ledger at
  /dev/shm/suite-cmd-agent-run-…/003/memory.db
```

The report never names `archive/feat/orphan-<test>`, and the assertion at :156
(`the report does not name …`) is exactly the operator-visible symptom.

The engine is a chain of three facts that each looked right on its own:

1. `internal/tools/archive_row_reconcile.go`'s `archiveRetirementLedger` returned
   `fmt.Errorf("archive retirement reconcile: no ledger at %s", path)` when
   `ledgerMissingForRead(path)` was true — **in the same file whose header
   documents the opposite rule** ("An ABSENT ledger is the empty answer, never a
   refusal — the same read guard `taskOwnerAxesAll` obeys").
2. The error propagated out of `ReconcileUnrelatedHistoryRetirement` **after** the
   writer had already run, so the archive tag and the reason record were written.
3. `cmd/agent/merge_run.go`'s `mergeUnrelatedHistoryOutcome` renders *any* error
   from that call as `mergeUnrelatedHistoryLeftAsIsFmt` — "left as it is, NOT
   retired" — which replaced the `retired as a decided exclusion (tag …)` action
   line wholesale.

So a run that **did** retire the branch reported that it had not, and named no
tag. A test profile (`mergeTestEnv` pins `GOPI_MEMORY_DB` to a path under
`t.TempDir()` that nothing ever creates) hits this on every run; so does a fresh
sandbox profile, or any host whose ledger has not been built yet.

## Why the fix is where it is, and not in the renderer

The temptation is to special-case the message in `cmd/agent` — but the mechanism
already had the right rule written down, and **two** callers of the same opener in
the same file wanted two DIFFERENT readings of "no ledger":

| caller | reading of an absent ledger | why |
|---|---|---|
| the row read (`archiveFamilyRowsForBranch`) | the EMPTY SET | a machine with no ledger holds no row, so nothing names the branch — building one to answer "no rows" is a write with no subject |
| the trail write (`retiredBranchReconcileDB`) | SKIP, loudly (Rule 15b) | there is no ledger to hold the trail, so there is nothing to write |

`archiveRetirementLedger` is the **opener**, and it can only report what it can
still report: a ledger that exists and cannot be opened. The "is there anything
to look at?" question belongs to each caller through the package's ONE guard
(`ledgerMissingForRead`, `ledger_read_guard.go`) — which is also what every other
pure-read lookup in the package already does (`taskOwnerAxesAll`,
`TaskForBranch`, `task_for_worktree.go`). The guard was asked in the wrong layer,
so one layer's answer cancelled another layer's completed work.

## What changed

- `internal/tools/archive_row_reconcile.go` — the guard moved up into the two
  callers; the opener no longer answers a refusal for a missing file.
  `archiveFamilyRowsForBranch` now also returns **which ledger it asked** and
  **whether one existed**, carried out on the result.
- `internal/tools/archive_row_reconcile.go` — `ArchiveRetirementReconcile` gained
  `LedgerPath` + `LedgerAbsent`. The sibling lookup carries its ledger out the
  same way (`TaskOwnerAxes.LedgerPath`): "reconciled 0 row(s)" is a claim about
  ONE database, and Rule 15 forbids collapsing "the ledger held no row naming it"
  into "there was no ledger".
- `cmd/agent/merge_run.go` — a third reading of the ledger half,
  `mergeUnrelatedHistoryNoLedgerFmt`, appended as a **clause on the action line**
  that already says the retirement happened:
  `… delete that tag to re-arm it; no ledger at <path> — no open row could name
  it, so none was reconciled`. The retired line is never replaced by it.

## The regression test, and why it is a red proof of the real defect

`TestArchiveRetirementWithNoLedgerIsTheEmptyAnswer` (`internal/tools/`) pins four
things — none of which is the writer's return value alone:

1. no error;
2. the answer **says** it could not read (`LedgerAbsent` + the pinned path);
3. no rows in either arm;
4. the **durable half still happened** — the `archive/<branch>` tag resolves, the
   reason record carries `unrelated-history`, and the code was read back from it;
5. and the read did **not** build its subject (`fileExists(ledger)` is still
   false), which is the write `ledger_read_guard.go` exists to forbid.

(4) is what makes it more than an assertion about an error string: against the
unfixed production line the test fails at assertion (1), because the tag was
written and the reconcile still refused.

The ledger comes from the package's ONE scaffold (`testutil.NewTestDBPath`,
Rule 32) — a fresh per-test path that does **not** exist, off the `/` partition,
env pinned — never a hand-rolled path.

## The asymmetry worth remembering

A **missing file** and an **unreadable file** are different facts, and only the
second is a failure. `ledger_read_guard.go` had already drawn that line and paid
for it once (its own comment records an early cut that answered the empty result
for an UNOPENABLE ledger, caught by `TestTaskVerbForBranchRunsAsAdvertised` — exit
4 where the contract wants 1). This task is the mirror image of that bug: a
missing file answered as a REFUSAL, which turned a completed write into a
reported non-event. Both directions are the same mistake — a layer answering a
question it was not asked.
