<!-- doc-class: historical -->
# Close-out chain: the re-swept terminal row, and the branch-delete record that could not answer "was this delivered?"

Task `01a12b80-98ed-7983-acfc-f6a97394239a` (system layer, Rule 28) — the code half
of the review filed from `01a12b6f-14f3-7b93-a921-ae8b6823d07d`. Two defects were
measured on the client host 2026-10-11 and this change fixes both:

* **D1 — an already-cleaned terminal row was re-swept, and every pass wrote a
  deletion record for a deletion git refused twice.**
* **D2 — the branch-delete record carried no tip and no delivery point, so "did
  this row's branch reach the server?" was answerable from no ledger at all.**

Both live in code that already runs: no new column, no new daemon, no new cron,
no new file (Rule 35). The record grows two OPTIONAL fields; the chain learns one
early return that it should always have had.

## D1 — the mechanism, in the chain's own terms

`internal/tools/task_finish_cleanup.go`, `finishCleanupDeleteDeliveredBranch` (④):

```bash
git branch -d <b>   # absent ref → exit 1, "branch '<b>' not found"
git branch -D <b>   # absent ref → exit 1, the SAME error
```

An absent ref and an unmerged ref are **indistinguishable at git's error check**.
The `-D` fallback needs standing, and its standing gate reads *this chain's own
push report* (`finishCleanupStepSucceeded(rep, finishCleanupPushVerb)`), not the
world. On the absent-ref arm the push step is recorded as a successful no-op
(`finishCleanupBranchAbsentDeliveryNote` — correct: there is nothing to hand
over), so the guard was told "yes, this sweep delivered" and the chain proceeded
to force-delete a ref that was not there.

Nothing recorded that this row's close-out had already run, and the sweep
re-selects terminal rows (`serveFinishCleanupSweep`, 10-minute throttle) — so a
terminal row is walked for ever. Measured on one row's branch, three identical
pairs from two pids (one of them the serve tick):

```
12721 2026-10-11T14:29:00Z branch-delete finish-cleanup-branch        pid=3656808
12722 2026-10-11T14:29:04Z branch-delete finish-cleanup-branch-force  pid=3656808
12750 2026-10-11T14:52:55Z branch-delete finish-cleanup-branch        pid=3656808
12751 2026-10-11T14:52:55Z branch-delete finish-cleanup-branch-force  pid=3656808
12771 2026-10-11T15:02:58Z branch-delete finish-cleanup-branch        pid=3896410
12772 2026-10-11T15:02:58Z branch-delete finish-cleanup-branch-force  pid=3896410
```

**The harm is not the repeated work — it is that the audit stream stopped
distinguishing a real deletion from an empty gesture.** Both read
`finish-cleanup-branch-force` with the identical sentence, which is exactly how a
healthy delivery came to be filed as one that never landed.

### The fix

④ now reads the tip **before** it deletes anything — the same reading that proves
the ref exists and fills the record's `sha` — and when it does not resolve it
records a STEP and returns:

```go
deletedTip := branchDeleteTipOf(ctx, branchDir, branch)
if strings.TrimSpace(deletedTip) == "" {
    rep.Steps = append(rep.Steps, FinishCleanupStep{Step: finishCleanupBranchVerb, Target: branch,
        Detail: finishCleanupBranchAbsentNote})   // named constant, finiteCleanupBranchAbsentNote
    return rep, nil
}
```

No ref ⇒ no git call, **no audit record** — a record for a deletion that did not
happen is a false record — and the disposition is still reported, never silent
(Rule 15). The tip captured here is also what the record now carries, so the
delete and the proof of what was deleted are one reading.

One record per real deletion now, not one record per sweep pass.

## D2 — the two fields

`internal/removalaudit/audit.go`:

```go
FieldSha           = "sha"
FieldDeliveryPoint = "delivery_point"
```

`Record` grows `Sha` and `DeliveryPoint`, both `omitempty`, and a new entry point
carries them:

```go
func BranchDeleteAt(arm Arm, repo, branch, taskID, tip, deliveryPoint, evidence string)
func BranchDelete(arm Arm, repo, branch, taskID, evidence string) {   // delegates with "", ""
    BranchDeleteAt(arm, repo, branch, taskID, "", "", evidence)
}
```

The old entry point **delegates** rather than being deleted: most arms learn about
a ref (a sweep pruning a spent branch) and genuinely cannot resolve a tip, and
inventing one would be worse than omitting it. The absence is readable as
"unknown", which is the honest state; a REQUIRED field would have to be filled
with something nobody measured.

`Version` is deliberately NOT bumped. The change is additive and optional — an
old reader of a new line sees a superset, and a new reader of an old line sees
the two fields absent, which is the truth about a line written before the fields
existed. A bump would signal a shape change that no consumer has to handle, and
the file is grepped, not version-dispatched.

The close-out chain's two sites now pass both facts:

```go
removalaudit.BranchDeleteAt(removalaudit.ArmFinishCleanupBranch, branchDir, branch, rep.TaskID,
    deletedTip, target, "delivery chain's branch-liveness gate did not refuse …")
```

`target` is the chain's own delivery point — the same value step ② pushed to, so
the record names where the work was kept, not merely that it was deleted.

**What an operator can now do, on one host** (`~/.gopi/logs/removal-audit.log`):

```bash
grep '<task-id-or-branch>' ~/.gopi/logs/removal-audit.log
# {"kind":"branch-delete","arm":"finish-cleanup-branch","branch":"task/<id>",
#  "sha":"e81f64fb2…","delivery_point":"peerbox:/home/user/work/GoPi", …}
```

`sha` + `delivery_point` present ⇒ the row's branch was handed over to a named
point, and the commit that was deleted is named. That is the sentence the ledger
could not say before, and its absence is what turned a healthy delivery into a
"fix never landed" report.

## Tests (both red-provable)

`internal/removalaudit/audit_test.go`

* `TestBranchDeleteAtCarriesTheTipAndTheDeliveryPoint` — GREEN half, asserted on
  **the raw line** as well as the decoded struct (the reader that exists today is
  `grep`).
* `TestBranchDeleteOmitsFactsItsCallerNeverResolved` — the other direction, so the
  fields cannot become a slot every arm fills with anything.
* `TestBranchDeleteAtIsTheEntryTheDeletionSitesUse` — with the emitter disabled
  the same assertion fails; the red text is logged.

`internal/tools/task_finish_cleanup_branch_absent_audit_test.go`

* `TestFinishCleanupCleanedRowRecordsTheDeliveryThenStops` — drives the REAL chain
  body (`runFinishCleanupChainTarget`) over a real repo three times. Pass 1 must
  leave exactly one `-d` record — carrying the tip and the push target — and
  passes 2 and 3 must add none, while each still records the absent-ref step.
  **Against the pre-fix chain this is RED by construction** (the pre-fix chain
  appended a fresh pair per pass — that is what the log measured).
* `TestFinishCleanupBranchDeleteRedProof` — the same removal with the emitter
  disabled; the deletion is asserted to have happened, so the red proves the
  assertion depends on the emit rather than on the chain not running.

## Rule 31 real-run evidence (both defects, one real chain)

The green suite proves the assertions; it does not prove the SURFACE. The real
run below drives the REAL entry point (`tools.TaskFinishCleanupSessionID` — the
function `gopi task finish-cleanup --apply` calls per candidate row) against a
TERMINAL ROW IN A REAL LEDGER, with a REAL push target
(`peerbox:~/work/GoPi`, the chain's own resolved target), twice.

Log: `~/.gopi/sessions/01a12b80-98ed-7983-acfc-f6a97394239a/artifacts/rule31-realrun.log`
(fixture under `/home/user/workspace/gopi-test-scratch/rule31-01a12b80-scratch-3952352/`).

**Pass 1** — the audit sink gained exactly the pair, each half carrying the facts:

```json
{"kind":"branch-delete","arm":"finish-cleanup-branch","op":"branch:delete",
 "repo":"/…/rule31-01a12b80-scratch-3952352/wire","target":"task/rule31-live",
 "branch":"task/rule31-live","task_id":"t-rule31-realrun",
 "evidence":"delivery chain's branch-liveness gate did not refuse (no live row owes this branch)",
 "sha":"35b3e7584f5d4b0c7ebbcc6022ed49f6d175a539","delivery_point":"peerbox:~/work/GoPi"}
{"kind":"branch-delete","arm":"finish-cleanup-branch-force", … same sha / same delivery_point …}
```

Counts for that branch in the sink: `finish-cleanup-branch` = **1**,
`finish-cleanup-branch-force` = **1** (one pair, not three).

**Pass 2 (the serve tick's re-walk of the SAME terminal row)** — the ④ step
records the disposition and appends NO record:

```
PASS 2 step=branch  ok=false detail=no local branch to delete: the ref is already gone
                                  (an earlier pass delivered and deleted it, or no ref
                                  was ever created) — nothing for step ④ to do
```

Counts after pass 2 are still **1 / 1** — the acceptance criterion ("at most one
pair per branch id") measured on a real chain, not asserted.

**The pair is verifiable against the ref it names.** The record's `sha` and the
commit standing at the delivery point are the same object:

```
$ git -C <fixture>/peer.git for-each-ref --format='%(objectname)' refs/heads/task/rule31-live
35b3e7584f5d4b0c7ebbcc6022ed49f6d175a539      # == the record's sha
```

`delivery_point` is the alias+path the chain resolved (`peerbox:~/work/GoPi`); the
bytes were diverted to the fixture's bare repo by shipping the transport through
the repository's own `internal/tools/testdata/ssh-sentinel-shim.sh`, which execs
`git-receive-pack` on a receiver of the caller's choosing and can reach no real
node. So the honest reading is: **the target string is the real one, the
transport was sentineled** — the pair is proven against the ref it names, and
nothing left this machine.

## Not done / honest scope
* **No "already closed out" marker was introduced.** The epics' own analysis
  (`devdoc/closeout-deletes-undelivered-branch-bug.md`) prefers skipping ④ when
  the ref is absent over a stored `closed_out` column, because a stored cache
  becomes a lie the moment reality moves. This change does exactly that.
* **The other `BranchDelete` sites are NOT converted.** The sweep, the merge
  lanes and the adoption chain each delete refs they learned about elsewhere; none
  of them resolved a tip at the moment of deletion, and converting them would mean
  inventing one. They keep delegating to the old shape, which now records the two
  fields' absence honestly.
* **`serveFinishCleanupSweep` still re-selects terminal rows.** Skipping ④ when
  the ref is absent removes the audit noise and the two failing git calls; it does
  not stop the scan itself, which also runs the dead-holder reclaim and the
  stranded-directory legs and must keep doing so. A row-level "nothing left to do"
  short-circuit is a separate decision with its own evidence, and it is not made
  here.
* **The `-d`-refused-but-`-D`-not-needed race** (a real deletion whose `-d` half
  takes the ordinary path and never reaches the force half) was already the
  behaviour: one record, `finish-cleanup-branch`. The acceptance wording ("at most
  one pair") covers it; this change does not alter it.
