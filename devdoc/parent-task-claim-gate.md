<!-- doc-class: historical -->
# Devdoc: `parent_task_id` becomes a dependency GATE

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

Date: 2026-09-23 · branch `task/01a0c34408c97e15befec3c8dfa326de` (worktree
`/home/user/workspace/gopi-01a0c34408c97e15befec3c8dfa326de`) · Rule: GOPI.md Rule 33

## The gap

`parent_task_id` recorded lineage and nothing else. It is WRITTEN by the paths
that produce real chains — `TaskVerifyToReopen` (`internal/tools/tasks.go`,
the `<id>-r1` rework child), the verify-closure enqueue
(`internal/tools/task_verify_closure.go`), and reframe's roadmap fan-out
(`internal/agent/reframe.go`, `reframeSubmitOptions` → `ParentTaskID: parentID`)
— and it was READ only after the fact (`taskChildrenWithDB` for `task show`,
the closure dedup lookup). Nothing in the claim path consulted it, so a child
whose parent was still `pending`/`in_progress` — or cancelled, or failed — was
dispatched into a fresh session whose briefing denominates a premise that had
not happened yet.

## What changed (one predicate, two readers)

`taskRowClaimable` (`internal/tools/tasks.go`) gained a fourth arm. It remains
the ONE eligibility definition shared by the claim funnel `TaskNextFiltered`
(every dispatch entry point: worker round / `gopi task run next` / serve) and
the pending view's `[queue-skip]` marker (`taskQueuePositionSuffix`), so the
row the operator READS as next and the row the funnel CLAIMS stay the same row.

- `taskParentReleaseStatuses` = **{completed, verified}**, exactly. Every other
  parent status parks the child; a cancelled/failed parent parks it FOREVER.
  No override, no cascade cancel, no timeout/staleness release, no new status.
- `taskParentGateDenied(parentID, parentStatus)` is the single decision point
  (verdict + rendered reason), so the marker and the funnel cannot answer
  differently. `taskRowClaimable` calls it; `taskParentGateReason` is its
  read-only face for the renderer.
- Dangling `parent_task_id` (no such row) ⇒ denied, explicitly. `TaskSubmit`
  already refuses a dangling parent at submit time, so such a row arrives by
  direct ledger write, a peer replica whose parent has not synced, or a row
  predating that validation — all of which the conservative arm covers.
- nil parent index ⇒ a parented row is DENIED and says the status was
  unreadable (a caller that cannot read the ledger must not assume the
  dependency holds), never silently allowed.

### WHY a status map, not an EXISTS clause or a per-row lookup

The epic asked for an explicit decision here.

- **An `EXISTS` clause welded into `TaskNextFiltered`'s SELECT** would implement
  the gate in ONE reader and leave the pending view to re-derive it — the
  drift Rule 14 forbids, and the failure mode is a listing that marks `[next]`
  a row the funnel refuses.
- **A per-row handle lookup** (`SELECT status WHERE task_id = ?`) is one
  statement per parented row and cannot distinguish "no such row" from "not
  fetched" without a second error convention.
- **A whole-ledger map** (`taskParentStatuses`, one `SELECT task_id, status`),
  read LAZILY — only once a row in the claim order actually carries a
  `parent_task_id` — is exactly the EXISTS semantics materialised: membership
  IS row existence (the dangling arm) and the value IS the release test. It is
  one query for the whole walk, no per-row statement on the claim path, and it
  is free for a parentless queue (measured today: 0 of 23 pending rows are
  affected, so the live claim path pays nothing).

The map is rebuilt per claim attempt (not carried across retries): a retry
happens because another claimer moved the queue, so parent statuses must be as
fresh as the row set.

## Legibility (a park must never look like starvation)

`taskQueuePositionSuffix` now appends the gate's reason to `[queue-skip]`:

```
[queue-skip] parent <id> not released (status=pending; releases on completed|verified)
[queue-skip] parent <id> missing from the ledger
[queue-skip] parent status unreadable
```

The release set inside the prose is DERIVED from `taskParentReleaseStatuses`
(`taskParentReleaseSetLabel`), so the sentence an operator reads cannot
describe a different set than the gate enforces.

## Interaction worth knowing (measured, not assumed)

`TaskVerifyToReopen` gives the rework child `parent_task_id = <judged row>` and
the judged row is written back **failed** when verification fails — so under
this gate the rework child is PARKED, not claimable. That is the gate working
as specified (the release set is exactly {completed, verified}, and "failed" is
not in it), and it is deliberately NOT special-cased: the anti-loop semantics
of the reissue are untouched, the row is visible with its reason, and the
operator's resolution (release or cancel) is the ordinary one. This is the
class of interaction the epic's "no cascade cancel / no override" constraint
buys: the gate is a fence, not a scheduler.

## Not changed

`gopi worker <explicit task-id>` still runs a parked row: the gate governs the
DISPATCH funnel, and an operator who typed the row id has taken the decision
the gate refuses to a queue. (`gopi task run` was RETIRED — epic
worker-run-entry — so the explicit-id verb is `gopi worker <task-id>`, and the
A/B CLI proof exercises exactly that: the funnel drains while the parked row is
untouched, then `gopi worker <parked id>` runs it.) Reframe's
fan-out shape is unchanged. No new column, no schema change.
