<!-- doc-class: normative -->
# `gopi task list` — the order surface (defect + decision)

**Epic:** task-list-order-surface · **Date:** 2026-09-26 · **Branch:** task/01a0dd4fb1fe7642807eecd6d128f1be

## The defect (measured, not inferred)

`gopi task list -h` promised **"List tasks, newest first"** — for the VERB, whose
own usage line carries `--status S`. The renderer disagreed with itself:

```go
// internal/tools/tasks.go (before)
if statusFilter == taskStatusPending {
        query += taskClaimOrderBy        // priority ASC, created_at ASC
} else {
        query += ` ORDER BY created_at DESC, id DESC`
}
```

so `--status pending` printed priority-first while the unfiltered verb printed
newest-first, and **nothing on the surface said so**. The cost is the SILENCE,
not the ordering — priority-first is defensible (it is the dispatch order).

Reproduced on the pre-change binary (see the task report's `Ran:` block): three
pending rows seeded p1(oldest)/p5/newest-docs gave

```
#t-p1-oldest  [pending] … [next]      ← priority-first
#t-p5-mid     [pending] …
#t-p8-newest  [pending] …
```

against a help text promising newest-first.

## The decision: render newest-first, NAME both orders

Two channels can be reconciled either way. We render **newest first** for every
view (the help's own promise, and the chronological-ledger convention every
other status filter already followed) and make the surface say BOTH orders:

- the filtered view prints an order note under its count header
  (`TaskListOrderNote`), naming the render order AND the claim order;
- the family help (`tasksUsage`) carries the same two, with the queue markers
  explained;
- the pending rows keep `[next]` / `[queue-skip]`.

### Why not "keep priority-first and document it"

That was the previous shape (2026-09-15), and it leaves the VERB's `-h` line
false for anyone reading the group help: one order per release is what the
surface can promise, and `--status` should select ROWS, not reorder the ledger.
The queue question the operator actually has — *which row runs next?* — is
answered by the marker, which works in either render order.

### The subtle part: `[next]` had to become order-INDEPENDENT

`[next]` used to be "the first claimable row **this render walks past**"
(`nextStamped`). With a newest-first render that stamps the NEWEST dispatchable
row while the funnel claims a different one — a direct Rule 33 violation. The
marker is now resolved by the funnel's own clause:

```go
nextClaimID, perr = taskNextClaimID(ctx, db)   // taskClaimOrderBy + taskRowClaimable
...
queue = taskQueuePositionSuffix(t, parentStatus, nextClaimID)
```

`taskNextClaimID` reuses `taskClaimOrderBy` and the SAME `taskRowClaimable`
predicate the funnel applies with no skip set, so the marker and the queue
cannot drift. It does NOT call `TaskNextFiltered`: that function CAPS
`created_at` (it claims), and `task list` is a pure read (no write, no notify).

**Deadlock note (kept from the existing code):** the ledger pins a single
connection, so `taskNextClaimID` collects its candidate ids, CLOSES the cursor,
and only then reads the parent-status index — the same ordering `TaskList`
already used for `parentStatus`.

## Consequences / honest gaps

- The 2026-09-15 operator expectation ("the top pending row is the row
  dispatched next") is explicitly GONE: the list is chronological, the marker
  carries the queue. A reader who wants the claim order can read `[next]`, or
  the note's own words.
- `TaskListOrderNote` deliberately does NOT contain the `[next]` token: the note
  says "marked with its queue position". Spelling the token there would put it
  in two places (Rule 13) and break marker counting over the render (it did,
  first run: a substring count read 2 markers for 1 row).
- The note rides the COUNT HEADER, so `FilterArchivedRows` keeps it: the rewrite
  replaces only the header's leading digits.
- Pre-existing, NOT fixed here: the claim-order-and-red-proof test pair
  (`internal/tools/tasklist_order_test.go`) was rewritten to the corrected
  contract in the same commit, since it pinned the now-removed render order.

## Gate refusals encountered

- `internal/stylegate` `RuleTestSleep` rejected the first version of the new
  cmd/agent test (a `time.Sleep` between submits to separate `created_at`
  seconds). Fixed by STAMPING the fixture's `created_at` (three rows, one stride
  apart) — deterministic, clock-free, no sleep. The gate itself is red on
  master (42 hits, ceiling 0), which is pre-existing: with the new file removed
  it reports the same 42 and no hit in this diff.
- The help gate caught a real drift while writing this: the const phrase
  `priority-first` vs the help's typed "priority first". The help now carries
  the const's exact spelling, so the agreement is checked, not hoped for.
