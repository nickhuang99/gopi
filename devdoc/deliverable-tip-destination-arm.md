<!-- doc-class: historical -->
# The deliverable-tip arm learns the delivery DESTINATION (epic 01a19433)

A run that delivered its work correctly was told "the work is unreachable". This
records why, what was measured, and what the repair is careful NOT to do.

## 1. The measurement

Reproduced on a real ledger row (not a unit fixture) with the pre-fix binary —
`/tmp/gopi-prefix`, built from `master` at `291aa2277` — against a repository in
exactly the state the clone channel leaves behind: the destination ref
`refs/remotes/origin/master` carries the run's tip, the row's ADDRESS ref
`refs/heads/task/<id>` was never created, and the removal-audit sink is empty.

```
[task-notify] run 01a19441-6447-79b2-ac56-99f543ee08bb claims `completed` for
/tmp/tipreal01a19433/repo, but its deliverable's tip is reachable from no ref:
branch task/01a19441 resolves to no ref, no delivery point was recorded for it,
and no reachable commit names this task id (no removal-audit record kept the tip
the branch pointed at (the sink this run read was
/home/user/workspace/gopi-sandbox/sandbox-tiprun-72e27489/.gopi/logs/removal-audit.log,
and it names no deletion of this branch)); the work is unreachable and nothing
was deleted, moved or restored by this arm
```

The same row judged by the FIXED binary (identical fixture, row
`01a19442-c82c-7460-b4e1-a2032da75b7e`) writes `status: completed` and files NO
`deliverable-tip-*` record:

```
01a19441-6447-79b2-ac56-99f543ee08bb  deliverable-tip-unreachable          run 01a19441-… claims `completed` … reachable from no ref …   ← pre-fix
--- 1 deliverable-tip record(s)
```

The e2e that first surfaced it carried BOTH lines in one log —
`clone channel: delivery delivered — pushed master to origin after 1 attempt(s)`
and this arm's sentence — and two historical rows (`01a18daa`, `01a18d61`) sit
the same way while `git for-each-ref --contains c64595a87` answers
`refs/remotes/origin/master`.

## 2. The cause, in one sentence

`deliverable.branch` is the row's **ADDRESS** (`task/<id>`), while the clone
channel's delivery destination is the **INTEGRATION** branch — `publishGround`
pushes `HEAD:refs/heads/master` and never `refs/heads/task/<id>`. Every ref-shaped
read in the arm asked about the address, so on the clone channel it asked about a
ref that channel never writes.

Compounding it, on a run whose address ref was never created there was **no
subject at all**: `out.Tip` stayed empty (no removal-audit record exists when a
mere clone ground is `os.RemoveAll`ed), and the arm still rendered a verdict
about the work. That is the shape Rule 15 forbids: "I have no subject" printed
as "the subject is lost".

## 3. The repair — three narrow additions, none of them a disable

| step | what it adds | why it cannot be a blanket pass |
|---|---|---|
| ①b | the **ground's HEAD** as a tip witness | it names a tip; every containment arm still has to find that tip in a ref |
| ③ | the **ground** as a second ref namespace | a ref is a ref; the tip must be CONTAINED (`deliveryContentAncestryOrPatch`), so "the directory exists" is still not a pass |
| ④b | the **delivery destination** (`refs/remotes/origin/master`, then `refs/heads/master`) | containment is the sibling delivery-proof arm's own leg, reused rather than re-spelled; a destination that exists but does not carry the tip falls through to the defect verdict |

Two smaller changes keep the three-valued contract honest:

- a **namespace read failure** is now measured per directory — `unchecked`
  only when NO directory's refs could be read, never when one of two could not;
- `Destination` is a new outcome field naming the ref that carried the tip, so
  "some ref holds it" and "the ref this row delivers TO holds it" are told apart
  on the record.

Nothing is fetched, pushed, moved or deleted. The arm stays what it was: a
criterion at the writeback seam, silent for the overwhelming majority of rows.

## 4. The tests, and the one that keeps the repair from becoming a disable

`internal/tools/task_deliverable_tip_destination_test.go`

| test | shape | red when the lookup is reverted |
|---|---|---|
| `TestDeliverableTip_DestinationCarriesACloneChannelsDelivery` | address ref absent, `origin/master` carries the tip | **YES** |
| `TestDeliverableTip_UnlandedTipAtACloneShapeIsStillReported` | the push never happened, every ref removed | no (by design) |
| `TestDeliverableTip_ForeignRefIsNotADeliveryPoint` | the tip's only company is an unrelated ref | no (by design) |

The red proof was taken by reverting **both** ①b and ④b and re-running the
three: exactly one `--- FAIL`, from arm ①, carrying the false-positive sentence;
both reverse arms stayed green. Restore verified by content, not by an
index-relative diff:

```
$ sha256sum internal/tools/task_deliverable_tip.go
0b6639034d1da8d3326f3bc14c6edd6e73b79273acc9af4ff1398ad570398b55
```

## 5. What this does NOT do (honest)

- It does not make the arm question a **plain** repo's delivered state where the
  destination branch is named something other than `master`: the base is
  `IntegrationBranchName` because that is what the channel's ONE refspec
  publishes. A host that delivers to another branch is not covered — a row does
  not record its base branch, and inventing one per row would be a second
  definition of "where work is delivered" (Rules 1/14).
- It does not repair rows already carrying a false
  `deliverable-tip-unreachable` record; those are history and read as such.
- The `deliverable-tip-unreachable` record is still filed, unchanged, for a tip
  that really is in no ref — that is the arm's only job.
