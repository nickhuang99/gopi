<!-- doc-class: historical -->
# `delivery (read now):` — the clone channel's address is gone BY DESIGN

**Task:** `01a1aa67-5429-7e4e-84ab-63dd43aeccaf` (epic row `01a1a891-c437`).
**File repaired:** `internal/tools/task_ground_delivery_reading.go`.

## The defect, measured

`gopi task show` on a row the clone channel had **delivered**:

```
$ gopi task show 01a1a891-c437-7abd-a1ad-7a6efb484f42 | grep 'delivery (read now)'
delivery (read now): UNCHECKED — no ref in any place this ground can hold
task/01a1a891c4377abda1ad7a6efb484f42 carries it (asked
/home/user/workspace/01a1a891-c437-7abd-a1ad-7a6efb484f42 (missing),
/home/user/work/gopi (repo)), and the peer does not advertise it either — no tip
can be named for this row ...
```

while the commit was on the integration branch, on both hosts:

```
$ git merge-base --is-ancestor f43cd829fbc8c36ec961203418cf4d6e7e37ab89 origin/master; echo $?
0
```

`f43cd829…` is the tip the row's own `clone-delivery-landed` record names; the
epic named `dd369c8d…`, an earlier commit of the same run, and it answers 0 too.

## Root cause

The reading named the run's tip from the row's **ADDRESS ref** only —
`deliverable.branch`, i.e. `task/<id>`. The clone channel does not publish that
ref: it publishes the **integration** branch (`HEAD:refs/heads/<integration>`)
and its close-out **reclaims** `task/<id>`. So on every row it delivered the
address is absent **by design**, `taskGroundDeliveryTip` answered `""`, and
`verdict()` fell to its `default` arm — the "nothing named anywhere" sentence.
The arm was reading "this ref does not exist" as "this work does not exist".

The same fact, seen by the sibling arm, was already known: `deliveryProofTipOf`
(`task_delivery_proof.go`) takes the row's landed record as its **third witness**
precisely because the ground and its refs are gone. The two faces disagreed about
one row.

## The repair — a witness names IDENTITY, arrival is MEASURED

When the address ref yields no tip, the reading asks the row's own landed record
(`deliveryProofLandedTip` — the sibling arm's reader, never a second query,
Rule 14) and records the fact in `recorded`. Then, unchanged, it asks
`git merge-base --is-ancestor` against the recorded repo's integration refs
(`taskGroundDeliveryMergedInto`, via `taskGroundIntegrationRefsPresent`).

**The record is never allowed to decide arrival by itself** — the discipline the
sibling arm states as `!tipFromLandedRecord`, and the reason this cannot be a
blanket pass. Concretely, `verdict()` gained an arm that did not exist before:

```go
case d.recorded:
    if !d.tipReadableInRepo {
        return TaskShowDeliveryUnchecked, fmt.Sprintf(taskShowDeliveryRecordUnreadableFmt, ...)
    }
    return TaskShowDeliveryUnchecked, fmt.Sprintf(taskShowDeliveryRecordUnconfirmedFmt, ...)
```

i.e. a record whose tip **no** integration ref contains stays UNCHECKED, naming
the claim and the refs it asked. It sits BEFORE the "only in its own ground" arm
on purpose: no directory was read, so naming one as holding the work would be a
statement this reading never observed.

Two more small things had to become explicit so the new reading stays honest:

- `integrationAsked` (`taskGroundIntegrationRefsPresent`) and
  `tipReadableInRepo` (`deliveryProofCommitReadable`) are recorded separately,
  because "asked nothing" ≠ "asked and got no" ≠ "could not ask" (Rule 15).
- the "nothing anywhere" sentence now names the **third witness** too
  (`deliveryProofLandedWitnessText`), so a reader can tell "the record was
  consulted and named nothing" from "nobody asked it". The wording is true for
  both an EMPTY record and an UNREADABLE ledger, because `deliveryProofLandedTip`
  answers `""` for both and reports which to the log.

The new sentence (`taskShowDeliveryRecordedLandingFmt`) says out loud that the
record names a commit and the containment is what proves arrival — a reader who
saw only "recorded … landed" would take the record's own word for it, which is
exactly the inference this repair forbids.

## Regression arms (`task_ground_delivery_reading_recorded_test.go`)

All three drive the REAL `TaskShow` digest against real repositories (a clone
ground with `.git` a DIRECTORY, a non-bare recorded repo, a bare peer behind the
existing shim) and assert on the RENDERED line:

| arm | fixture | expected |
|---|---|---|
| (A) `TestTaskShowDeliveryReadsTheLandedRecordWhenTheAddressIsGone` | pushed to integration, address ref deleted, **ground removed**, landed record written by `TaskDeliveryRecordLanded` | `MERGED-LOCALLY`, naming the tip, `master`, and "landed record"; still says the peer's integration branch was **not asked** |
| (B) `TestTaskShowDeliveryNeverTrustsTheRecordedTipAsArrival` | same record, tip in the repo on a NON-integration ref, contained by no integration ref | `UNCHECKED`, naming the tip and `master`, and containing "NOT evidence of arrival" |
| (C) `TestTaskShowDeliveryNamesTheLandedRecordItAskedWhenNothingIsNamed` | no ref, no record, ground gone | `UNCHECKED`, naming both directories AND `deliveryProofLandedWitnessText` |

Arm (A) removes the ground directory deliberately: with it on disk the branch
axis would answer from the clone's HEAD for the wrong reason and the arm would
stop measuring the shape the defect was filed on.

## Red proof

The fix's own lookup block (18 lines) was deleted from
`task_ground_delivery_reading.go`, `go build ./...` stayed clean, and the new
arms turned red quoting the pre-fix sentence:

```
--- FAIL: TestTaskShowDeliveryReadsTheLandedRecordWhenTheAddressIsGone
    a delivered clone-channel row (address reclaimed, tip on master) must read
    MERGED-LOCALLY, got "UNCHECKED" ("... no ref in any place this ground can
    hold task/01a17df2recorded carries it ...")
```

Arm (C) stayed GREEN under the same mutation — it asserts an absence, and the
pre-fix code produced the same absence for a different reason. That is recorded
rather than hidden: (C) guards against a repair that silently drops the
witness-naming, not against the absence of the fix.

Restore was verified by content identity, then by the index-free comparison:

```
$ git hash-object internal/tools/task_ground_delivery_reading.go
9cae9313c0df2c7e57290fc67f40fc7072fa6a6f      ← equals the pre-mutation snapshot
$ grep -c RED-PROOF internal/tools/task_ground_delivery_reading.go
0
```

## Real surface (Rule 31)

One true binary pair, one true ledger, the same row:

| binary | `delivery (read now)` |
|---|---|
| `/usr/local/bin/gopi` (pre-fix) | `UNCHECKED — no ref in any place … carries it` |
| this ground's `bin/gopi` (post-fix) | `MERGED-LOCALLY — … names its tip f43cd829… and that tip is MEASURED to be contained in master of the recorded repo` |

## What this does NOT do

- It does **not** ask the peer whether the peer's own integration branch holds
  the tip (that needs a fetch, and this line writes nothing — the sentence says
  so). A row whose work is on the local integration ref but never reached the
  peer still reads MERGED-LOCALLY, exactly as the arm before it did.
- It does **not** repair `delivery-owed`, which still reads
  `UNCHECKED — the recorded ground no longer exists on disk` for the same row.
  That is a DIFFERENT axis (`task_delivery_owed_reading.go`) with its own
  witnesses, and this change does not touch it.
