<!-- doc-class: historical -->
# A Header That Contradicted Its Own Rows — the `[closeout-refused]` Sentence

**Task:** 01a12bad-a8ac-7b77-8964-227c9314ab75 (branch
`task/01a12bada8ac7b778964227c9314ab75`)
**Date:** 2026-10-12
**File:** `internal/tools/task_list_closeout_signal.go`
(`taskListCloseoutNoteFmt`), tests in `task_list_closeout_signal_test.go`

## The defect

Case ① of `devdoc/silent-degradation-four-cases.md` (epic 01a12af7) added a
header count line and a per-row tail to `gopi task list`, so an operator learns
from ONE invocation that rows are stranded. The marker and the reason are
honest. **The header sentence was not:**

```
close-out refused on 49 row(s) — the delivery chain stopped before the push, so
the branch was NOT removed; each affected row is marked [closeout-refused] with
its reason:
```

Measured on this host (2026-10-12, `gopi task list --all`):

```
header_count=49   marked_rows=49
reason histogram over the 49 marked rows:
     49 removal skipped
```

All 49 carried the SAME reason:
`removal skipped (delivery already attempted): the recorded worktree directory
is not there — nothing to remove`. That is the **removal** half's idempotent
no-op, produced by epic client-push-rebase's delivery-first ordering: the push
HAD happened, the branch ref was gone, the directory was already gone. The
header said "the push never ran and the branch is still here" — the opposite of
every row printed beneath it.

## Root cause

`taskListCloseoutSignals` keys on **one event name**
(`finishCleanupRemovalRefusedEvent` = `finish-cleanup-removal-refused`) and that
one name is written by **two arms** of `task_finish_cleanup.go`:

- the **removal-refusal** arm — dirt / a foreign binding / a gone directory; the
  delivery ran first, so the branch may or may not be on the peer;
- the **removal-not-reached** arm (`finishCleanupNoteRemovalNotReached`) — the
  chain stopped earlier, so the directory survived a delivery that FAILED.

The reader can therefore answer exactly one question — *"is the row's newest
recorded verdict a close-out refusal?"* — and the render made it answer a
different one (which half stopped) by **asserting** a consequence instead of
reporting what it read. Rule 41's shape: the surface claimed more than its
instrument measured.

## The fix

The header states the population it counted and where the why is; the per-row
detail keeps the diagnosis (unchanged — it was always honest, and the arms print
their own remedies):

```
close-out refused on 53 row(s) across the ledger — the newest recorded verdict
of each is a close-out refusal; a marked row carries [closeout-refused] with the
reason it recorded:
```

`across the ledger` is deliberate and is asserted: the count comes from ONE
ledger-wide query taken before the row cursor opens, so on a **filtered** view it
can exceed the marked rows on screen (measured: `--status completed` → header 53,
51 marked). Saying so makes the two readings distinguishable; scoping the query
to the filter would make the number depend on which view an operator happened to
open, which defeats the "how many are stranded" total the line exists for.

## Red proof

The three header tests were run against the pre-change sentence
(`--- FAIL` × 3, `go test ./internal/tools/ -run TestTaskListCloseoutHeader`):

- `TestTaskListCloseoutHeaderClaimsNoHalfOfTheChain` — the header carries no
  "before the push" / "NOT removed" claim, at 1/2/49 rows;
- `TestTaskListCloseoutHeaderCountMatchesItsOwnRows` — on a real render the count
  equals the rendered marked rows (2 of 3 seeded rows);
- `TestTaskListCloseoutHeaderCountIsLedgerWideNotViewScoped` — a filtered view
  shows the ledger-wide count and says so.

## What was NOT changed

No gate, no query predicate, no row tail, no event name. The marker and the
per-row reason were correct before and stay byte-identical; only the sentence
that summarized them changed.

## Verification recipe (reusable)

To check any "header counts X" line against the rows beneath it, count both from
ONE render and never by eye:

```bash
out=$(gopi task list --all)
echo "$out" | grep -oE 'close-out refused on [0-9]+ row'
echo "$out" | grep -cE ' \[closeout-refused\](: |$)'
```

The bare token is NOT a row marker — a row whose **summary** quotes
`[closeout-refused]` (this epic's own task row does) matches a naive
`grep -c closeout-refused` and inflates the count by one. Match the rendered
**tail** shape.
