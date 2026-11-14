<!-- doc-class: normative -->
# The delivery loop reads the ROW at its round boundary

Row `01a18392-84cc-741e-96e7-79d4a4287eb6`. The clone channel's `finish()` was
**unconditional**: once a run entered the delivery loop it spent all
`devworkspace.MaxDeliveryRounds` attempts and spawned an agent round between
every pair of them, whether or not the ledger row it was delivering still wanted
to be delivered.

## What that cost

An operator cancels a row precisely when they want it to stop. The loop kept
going: one cancelled row bought three delivery attempts and up to two
bare-prompt agent rounds — and each of those rounds registers a ledger row of its
own (that is the `01a1837e` seam's whole subject). One operator cancel therefore
produced NEW rows, which the next dispatch round then had to clean up. The
measured symptom on the live ledger was `in_progress` rows with no live process,
arriving in batches right after a cancel wave.

The second half is quieter and worse: `finish()`'s two terminal exits,
`exhausted()` and `nothingToDeliver()`, each **write a verdict sentence** onto
the row (`clone channel: delivery did not land in 3 attempt(s) …`). A cancelled
row was being told, by the machinery, that its delivery had failed — a
classification nothing measured, on a row whose owner had already decided.

## The fix: one read, at the boundary, before anything spends

`cloneRunState.finish` now calls `stopIfRowRetired` at **two** points per round:

1. **before the attempt** — a row already retired spends nothing at all (no
   attempt, no spawn);
2. **before the retry round** — a cancel that landed *while the attempt was
   running* must not buy the agent round that follows it. This is the one that
   matters: the retry round is the only place the loop starts a process.

Both call sites are *before* any spawn and *before* any attempt, and both return
the sentinel `errRowRetired` — so neither `exhausted()` nor `nothingToDeliver()`
runs on a retired row.

`cloneRowRetiredStatuses` is a named one-member set (`cancelled`). It is
deliberately not widened to `superseded`: no run has been measured doing it, and
a brake that stops a real delivery because of a status nobody measured is a
worse defect than the one this fixes.

**A status read that fails does not stop the loop.** `stopIfRowRetired` logs the
failure by name (`the ledger status could not be read …`) and lets the loop run
its usual rounds. A ledger hiccup must not silently abandon a delivery; the safe
direction is "keep doing what we did before", and the log makes the degraded
read visible rather than inventing a verdict from it (Rule 15).

## The caller: a retired row is not rewritten

`runTaskRunOnce` used to answer every non-`errNothingToDeliver` error from
`finish()` with `TaskUpdateReason(target, failed, …)`. On the new sentinel it
would have rewritten an operator's `cancelled` into `failed` — a write that
destroys the operator's decision. The `errRowRetired` arm therefore prints the
reason (facts stream + console) and returns it **without any status write**: the
row is already terminal, and the run reports that it stopped.

## What is unchanged

An ordinary row (`in_progress`, or a row whose close-out already wrote
`completed`, or an empty/legacy status) runs exactly the rounds it ran before:
`MaxDeliveryRounds` attempts, `MaxDeliveryRounds` agent rounds on the exhausted
path, the same sentences, the same log lines. The tests below pin both halves —
a fix that stopped every run would fail the control arm.

## Checks

```
go test ./cmd/agent/ -run 'TestDeliveryLoop' -count=1 -v
go test ./cmd/agent/ -run 'TestRetiredRow|TestLiveRowStill' -count=1 -v
```

- `TestDeliveryLoopStopsWhenTheRowIsAlreadyRetired` — retired before the loop:
  0 attempts, 0 spawns, neither terminal sentence, reason names the status and
  the kept workspace.
- `TestDeliveryLoopStopsBeforeTheRetryRoundIsSpawned` — cancelled *during* the
  first attempt: 1 attempt, **0 agent rounds**, and the status read is recorded
  at the spawn boundary (a read taken after a spawn would prove nothing about
  it).
- `TestDeliveryLoopUnretiredRowsKeepTheirRounds` — three live statuses, each
  still spends `MaxDeliveryRounds` attempts and `MaxDeliveryRounds` agent rounds
  and keeps the `delivery did not land` sentence.
- `TestDeliveryLoopUnreadableRowStatusKeepsTheLoopRunning` — a failed read is
  logged by name and the loop runs unchanged.
- `TestRetiredRowStopsTheLoopThroughTheRealLedger` /
  `TestLiveRowStillSpendsItsRoundsThroughTheRealLedger` — the same two facts
  through **production's** reader (`tools.TaskRowStatusAt`, `rowStatus` left
  nil) against a real private ledger, because the defect was that the loop never
  asked the ledger at all; a fake can only show it reacts to a string.

**Red proof** (recorded in the run's report): deleting the two
`stopIfRowRetired` call sites turned
`TestDeliveryLoopStopsWhenTheRowIsAlreadyRetired` and
`TestDeliveryLoopStopsBeforeTheRetryRoundIsSpawned` red — both then took the
`exhausted()` sentence (`delivery did not land in 3 attempt(s)`) with 3 attempts
spent. Restoring the file byte-for-byte returned them green
(`diff -q` against the pre-mutation copy: identical).
