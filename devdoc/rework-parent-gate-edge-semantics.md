<!-- doc-class: normative -->
# Rule 33's second edge semantics — a rework row waits for the parent's VERDICT, not its success

> Class: design note (why a gate has two criteria, and why the release set
> was NOT widened). Related: `devdoc/task-parent-release-statuses.md` (the
> release set's own history), `internal/tools/rework_reparent_children.go`
> ("THE FIX IS THE EDGE, NEVER THE RELEASE SET").

## The defect, measured

`gopi task clone <id>` (and the verify FAIL verdict's rework lane) mints a NEW
pending row `<id>-r1` whose `parent_task_id` is the row it just judged
(`internal/tools/tasks.go`, `TaskReopenWithOptions`). The judged row is, in the
overwhelming case, `failed` — that is *why* the rework was asked for.

Rule 33's dependency gate judged every child with ONE criterion: *is the
parent's work SATISFIED?* — `taskParentReleaseStatuses = {completed, verified,
superseded}`. `failed` is not a member, and Rule 33 deliberately has no
override, no cascade cancel and no staleness release. So the successor was
`[queue-skip]` from the moment it was born, and stayed there forever; the only
exit was the operator's hand-run `gopi task update <id>-r1 --clear-parent`).

Measured on the baseline binary (`056f2f3`, sandbox
`sandbox-reworkproof-base-5de97c17`, evidence
`~/.gopi/sessions/01a159b2-.../evidence/cli-reopen-proof-BASELINE.txt`):

```
$ gopi task clone 01a159c6-8951-... --reason 'the run never produced a verdict'
Task re-enqueued: 01a159c6-8951-...-r1 (status=pending, parent=..., tags=bug) ...
$ gopi task list --status pending
#...-r1  [pending]  rework: the run never produced a verdict ...  [queue-skip] parent ... not released (status=failed; releases on completed|superseded|verified)
```

## Two questions about one parent, not one question with an exception

The gate had been answering two DIFFERENT dependency questions with one set:

| kind of child | what its edge actually means |
|---|---|
| an ordinary child (a split, a follow-up that builds on the deliverable) | "the parent's work is DONE and SHIPPED" |
| a REWORK successor (a `-r1` row, a branch-rework row) | "the parent row has reached its VERDICT — whatever it was" |

A rework exists *because* the parent failed. Its edge asserts currency, not
success: the parent must never re-run (its verdict stands), and the successor
carries the work. Judged by the release set, it is parked by the very verdict
that created it.

## The fix: a LINEAGE MARK, never a wider release set

Adding `failed` to `taskParentReleaseStatuses` would have "fixed" it by
releasing **every ordinary child of every failed row in the ledger** — rows
that were built on a deliverable the parent never delivered. That is the wrong
direction by construction, and it is the fix this change explicitly refuses.

Instead:

1. **A persisted lineage mark.** `tasktags.ReworkLineageTag = "rework"` is
   stamped on the row AT BIRTH, by the ONE minting funnel
   (`TaskReopenWithOptions`) and by the two verify-gate rework lanes
   (`MergeVerifyFileRework`, the verify-retry row). It is a tag, not a status
   and not a column: it says *which kind of edge the row holds*, which is
   exactly the fact the gate was missing.
2. **A second criterion, in a SECOND set.**
   `taskParentTerminalStatuses = {completed, failed, cancelled, verified,
   superseded}` — the ledger's own `terminalTaskStatus` notion, spelled from
   the status constants. `reopened` is excluded BY NAME (retired in M4.20-B;
   a legacy row carrying it means "reissued already", so treating it as a
   verdict would release a second successor onto the same failure).
3. **The gate takes the row's tags.** `taskParentGateDenied(rowTags, parentID,
   parentStatus)` — one predicate, so the claim funnel, the pending view's
   `[queue-skip]` marker and the transfer eligibility read all reach the
   verdict through the same door (Rule 14). A marked row is released when the
   parent is in the TERMINAL set; an unmarked row is released only by the
   RELEASE set.
4. **The reason a parked row renders is criterion-specific.** A parked rework
   row says `parent %s not released for this rework row (status=%s; a rework
   waits for the parent's verdict: releases on cancelled|completed|failed|
   superseded|verified)`. Rendering the release-set sentence there would send
   its reader to a repair that cannot work — the parent cannot become
   `completed`, it already reached its verdict and failed (Rule 15: a refusal
   names its OWN cause).

## What did NOT change, on purpose

- **`taskParentReleaseStatuses` is byte-identical to `056f2f3`** (proved with
  `diff <(git show 056f2f3:internal/tools/tasks.go | sed -n '/^var
  taskParentReleaseStatuses/,/^}/p') <(sed -n '/^var taskParentReleaseStatuses/,
  /^}/p' internal/tools/tasks.go)` → exit 0). An ordinary child of a `failed`
  parent is still parked, by both faces, asserted in the same test that proves
  the rework row is released.
- **The parent EDGE stays.** `FailedNeedsReissue` counts a `failed` row as an
  orphan debt iff it has NO successor row (`NOT EXISTS (SELECT 1 FROM tasks c
  WHERE c.parent_task_id = tasks.task_id)`). The rework row keeps pointing at
  the judged row, so the debt is discharged by the successor that was in fact
  filed — dropping the edge would have made every reissued failure look
  unaddressed forever.
- **No new machinery** (Rule 35): no process, no timer, no script, no column,
  no state file. The mark rides the tags column every row already carries.

## Where the mark is stamped

| lane | file |
|---|---|
| `gopi task clone` (the operator's door) | `internal/tools/task_clone.go` → `TaskCloneWithOptions` |
| verify-gate rework row (`MergeVerifyFileRework`) | `internal/tools/verify_gate.go` |
| verify-retry row (`MergeVerifyFileRetry`) | `internal/tools/verify_gate.go` |

The branch-rework family (`rework_reparent_children.go`'s callers) files rows
through those same paths and already carries `branch-rework`; those rows are
covered by the same stamp where they go through the two verify lanes.

## A tag value that already meant something

`"rework"` is not new vocabulary: `reworkLaneName` (`internal/tools/
repo_resolve_row.go`) is the merge hand-over recipe's own tag, and
`reworkSelectionTags` (`task_rework_successor.go`) already treats it as "this
row is a branch's rework successor". The new constant is the SHARED spelling of
that existing value (Rule 14), which is why the tag family reads coherently
rather than acquiring a near-synonym.

## Verification

- `go test ./internal/tools/ -run 'TestReworkParentGate'` — the red proof
  (reopen of a `failed` row → the funnel offers the `-r1` row; the view renders
  no `[queue-skip]`) and the half that must not relax (an ordinary child of the
  SAME failed parent stays parked, in the predicate, the funnel and the view).
- `TestReworkParentGateARMovesWithTheVerdict` — exhaustive over
  `taskStatusSet`: the two sets are decided status by status, the release set is
  never wider than the terminal set, and the two parked reasons never render
  alike.
- Real CLI run, both binaries, two private sandbox ledgers — see
  `task-report.md` (`## Verification`).
