<!-- doc-class: normative -->
# The delivery-proof criterion at close-out

A success terminal status (`completed`, `verified`) is a CLAIM ABOUT WORK, and
at close-out the write path must PROVE it. This page is the mechanism's one
window: what it measures, why the arms are shaped the way they are, and every
false-refusal trap that shaped them.

Implementation: `internal/tools/task_delivery_proof.go`.
Tests: `internal/tools/task_delivery_proof_test.go` (green) and
`internal/tools/zz_redproof_delivery_proof_test.go` (the A/B red proof).

## The incident

Row `01a163df-a105-71b8-a297-680c5d35d093` reads `status: completed`. Its
deliverable is

```json
{"branch":"master","repo":"/home/user/work/gopi",
 "worktree":"/home/user/workspace/01a163df-a105-71b8-a297-680c5d35d093"}
```

and the ground it names is a CLONE-channel workspace whose commits never
reached the delivery point:

```
$ git -C <ground> log --oneline -1                                → f26ab6f
$ git -C <ground> rev-parse origin/master                         → 882c645
$ git -C <ground> rev-list --left-right --count origin/master...HEAD
  0	3
```

The writeback was performed by a surviving CHILD process (its wrapper pid was
already gone) at the row's exact `ended_at`. Nothing in the ledger, and no
rendering surface, could tell that row apart from a delivered one.

## Why the neighbouring arms do not cover it

| arm | what it measures | why this incident passes it |
|---|---|---|
| `reclaimRunTrackedDirtRefusal` / `reclaimRunResidueRefusal` | the run's worktree is CLEAN | the work **was committed** — to the ground |
| `deliverableTipRefusal` (`task_deliverable_tip.go`) | the tip is reachable from SOME ref | the ground's own `refs/heads/master` holds it; the answer is true and about a different question |

Committed-and-undelivered is the gap between them, and that is exactly the
state this arm judges.

## The criterion

For a row that claims a delivery and records a `deliverable.branch`:

> is the TIP THE RUN PRODUCED contained in the DESTINATION THE ROW DECLARED?

- **tip** — the ground's `HEAD` (the commit the run actually made), falling
  back to the tip the removal-audit sink recorded when the ground is gone.
- **destination** — `refs/remotes/origin/<branch>`, read in the CLONE GROUND
  first (the only ref the channel's delivery loop refreshes every round), then
  in the recorded repo.

Anchoring on the ground's tracking ref rather than on `refs/heads/master` is
deliberate: a client host's `refs/heads/master` is a MANUAL ref that goes stale
by design (`devdoc/integration-claim-ref-staleness.md`), so a judgement anchored
there would refuse legitimate closes.

## Two arms per pair, and why that is not optional

This repository lands work by **cherry-pick / rebase** ("adoption: merge
<branch>"). The ground then stays AHEAD by commit identity while the
destination holds the identical patches. A guard with only
`merge-base --is-ancestor` would refuse exactly the closes the merger performed
correctly. So each (tip, destination) pair is judged by

1. **ancestry** — `git merge-base --is-ancestor <tip> <dest>`; and
2. **patch equivalence** — `git cherry <dest> <tip>` listing no `+` line.

Either is delivery.

## Three-valued, and Unknown is never collapsed

```
unknown      → RECORD (delivery-proof-unverified), the write PROCEEDS
delivered    → allow, silently
undelivered  → REFUSE (delivery-proof-owed), the row stays OPEN
```

Refusing what cannot be seen would wedge the ledger forever (a row whose ground
was reaped, a row whose destination ref never existed, a peer-host repo). The
non-answer is therefore **loud, on the trail**, and never a verdict: `unknown`
says "the question was never answered", which is not the same sentence as "it
did not land".

The observable cases that fall into `unknown`, each on purpose:

- the ground no longer exists and the audit sink kept no sha for the branch;
- no `origin/<branch>` resolves in either directory;
- the row records no branch at all (the control case — nothing claimed,
  nothing judged).

## The operator's escape

Two faces, ONE decision: `gopi task update <row> --force` and
`GOPI_TASK_DELIVERY_PROOF_ALLOW=1` (the shape is the dirty-close-out family's
own, `GOPI_TASK_DIRTY_CLOSEOUT_ALLOW`). They are resolved by a single function —
`deliveryProofAllowanceFor` in `task_delivery_proof.go` — into one boolean, so
there is no "the flag works and the variable does not" fork to discover later.

What the escape does NOT do is skip the measurement. The arm still reads the
row's tip and its declared destination; when the deliverable is provably not
where the row says, the release turns the REFUSAL into a loud RECORD and the
write proceeds. Three consequences, each observable:

- the trail carries `delivery-proof-unverified` — the same name an *unjudgeable*
  write gets, chosen on purpose: on this trail that name means "no proof was
  produced for this row", and a PROVEN close writes no such record at all, so
  "proven" and "waived" can never be confused on a row;
- the record's detail names the source that actually asked (`--force`, the
  variable, or both) and the note it waives;
- stderr carries one line saying so, so an operator watching the invocation is
  never left believing the close was verified.

A refusal that is NOT waived records `delivery-proof-owed` and leaves the row
OPEN (and on the delivery queue, see `task_delivery_owed_refusal.go`).

## Refusal shape (Rule 15 / Rule 30)

The refusal is a **refused write**, not a failed one: `status`, `ended_at` and
`reason` are byte-identical afterwards, the row is still OPEN, and the reason it
is open is on its own append-only trail. The arm is READ-only: it deletes
nothing, restores nothing, pushes nothing and moves no ref. The sentence names
the row, the declared repo/branch/ground, the tip (and where the tip came
from), every destination ref it read and in WHICH directory, and the two legal
exits.

## Where it sits

`internal/tools/tasks.go`, in `taskUpdateStampedCore`'s delivery state gate —
the ONE choke point every door passes (the run writeback via
`TaskRunWriteback` → `TaskUpdateWithResult`, the CLI `task update`, the
close-out chain, `task(action=commit)`). It runs immediately after
`deliverableTipRefusal`, so the two arms read as one ladder: *is the tip
reachable at all* → *is it at the destination it declared*.

## False-refusal traps, recorded so they are not re-learned

1. **The branch ref may already be deleted** — the close-out chain removes it
   BEFORE the writeback. The tip is therefore read from the ground's `HEAD` or
   the removal-audit record, never from `refs/heads/<branch>`.
2. **The linked-worktree channel does not push** (Rule 6c: the commit is
   already on the branch in the shared repo). A destination it never wrote is
   `unknown`, not `undelivered`.
3. **Cherry-pick/rebase landing** — see the two-arm section. This is the one
   that would produce daily false refusals.
4. **A stale `refs/heads/master` on a client host** — never the anchor.
5. **A ground that no longer exists** — no tip may be invented for it; audit
   record or `unknown`.

## The red proof

`zz_redproof_delivery_proof_test.go` is written against the PRE-FIX API only —
no symbol the fix introduces appears in it — so it compiles and runs on the
commit before the fix, where it FAILS with the symptom printed verbatim:

```
SYMPTOM: row zzrp-undelivered reached "completed" while its deliverable commits
are absent from the declared destination — ground HEAD a9d696e…
vs origin/master b839437…, `rev-list --left-right --count origin/master...HEAD`
= 0	1; writeback returned err=<nil> out="Task updated: zzrp-undelivered
(status=completed, reason=task finished)"
```

and PASSES on the branch. The A/B is the point: a test that only compiles
against the new code proves nothing about the defect it claims to have closed.

### The rebase half's two arms, and the self-guard that stops them rotting

`zz_redproof_delivery_proof_rebase_test.go` holds the pair that pins this
criterion's rebase path, and they are the reason the record arm exists at all:

```
TestZZRedProof_RebasedDeliveryIsUndeliveredWithoutTheChannelsRecord
TestZZRedProof_ARecordForAnotherTipDeliversNothing
```

Both went RED on `21d417a` and stayed red there until `8eb240c` — and the
adjudication matters, because the obvious reading ("the content arm is too
permissive") was **wrong twice over**. Measured on this ground (blob identities,
`git rev-parse <rev>:<path>`):

| rev | `task_delivery_proof_content.go` | `zz_redproof_delivery_proof_rebase_test.go` | the two arms |
|---|---|---|---|
| `700ce92` | `69c13a1e09b6689755423a12dfbef9fa0c4ccb1b` | `9ff747e23ef7d3cbc6929571da38f8d6804ec470` | **FAIL** |
| `21d417a` | `69c13a1e09b6689755423a12dfbef9fa0c4ccb1b` | `9ff747e23ef7d3cbc6929571da38f8d6804ec470` | **FAIL** |
| `a77d752` (HEAD) | `11c28cf54ebf3a089cc085498c47a9926205043c` | `ef3a7137eb7e8de4ddc20c3fb4d9d8cfc151054b` | **PASS** |

The production blob is **identical** at `700ce92` and `21d417a`, so the red at
`21d417a` cannot be a production regression introduced between them. The
decisive experiment is a two-way cross, and it is runnable again in a minute:

```
# OLD production (21d417a's blob) + HEAD's fixture  → PASS
# HEAD production (a77d752's blob) + OLD fixture     → FAIL
```

So the defect was in the **fixture**, not the arm. `8eb240c` measured the same
5-why: the pre-fix fixture's ONLY difference between the ground's table and the
delivery point's was the `# measured-at:` header line, and the content arm's
normaliser drops that line BY DESIGN (a rebase must rewrite the measured base).
A fixture that differs only there is answered by the normaliser, never reaching
the record arm the two tests exist to pin — they were green-for-the-wrong-reason
from `700ce92` (the commit that introduced them) onward.

**The residue, and how it is closed.** A fixture that can silently stop
reproducing its own shape is the actual defect, so `zzrRebasedDelivery` now
ASSERTS the property it claims, on the data, before any arm reads it: the run's
table carries the row the ground measured, the delivery point does **not** carry
that row, and it does carry the re-derived one. Mutation-proven (2026-10-28): with
the regeneration made to write the ground's own row, both arms fail with

```
fixture is wrong: the delivery point still carries the row the GROUND measured
("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"), so a content comparison WOULD match
and neither arm below pins the shape it claims
```

and restored, both pass. The guard is deliberately built from `git show`
invocations rather than production symbols, so the file still **compiles and
runs** on the pre-fix tree — which is what makes it a red proof rather than a
regression test that only exists after the fix.

**Reachability** — a red proof nobody runs is a comment. Both arms are ordinary
package tests with no build tag and no skip, so the branch-scoped run that the
merge/verify path already uses executes them:

```
scripts/test-budget.sh ./internal/tools/           # both arms are in this scope
scripts/test-budget.sh -run 'TestZZRedProof_' ./internal/tools/
```


## Not covered by this arm (honest)

- **The delivery ORDERING is unverified — and it is the first thing to measure
  when this arm goes live.** This run's own ground is a CLONE channel ground
  (`01a16409-…`, `.git` a DIRECTORY) sitting in exactly the refused shape:
  `rev-list --left-right --count refs/remotes/origin/master...HEAD` = `0 1`. So
  the arm would refuse THIS row until the channel's delivery loop (clean →
  pull --rebase → build → push) commits the push. That is correct behaviour for
  a stranded ground, but it means a *correct* run is refused-and-reopened until
  its channel delivers. This session could not measure that ordering: the `gopi`
  in force here is the main repo's binary built from `b56c4b1`, i.e. WITHOUT
  this arm. Measure it before trusting the arm in production.
- **The surviving-child half.** The writeback in the incident was performed by
  a CHILD process wearing `GOPI_TASK_RUN_OUTCOME=completed` after its wrapper
  was gone. This arm refuses that write if the deliverable is undelivered — it
  does not judge WHO is writing. Process-ownership machinery is filed as its
  own row (`01a1651d-76e5-71ff-a02f-af57789566ba`).
- **A `master`-named clone ground**, i.e. a run working directly on the
  integration branch in its own ground: the tip and the destination are then
  the same integration ref, and an undelivered commit is still caught (the
  tracking ref is a different commit), but the sentence reads in
  integration-branch terms rather than feature-branch terms.
- **The sync door** (`memory_sync_task.go`'s raw status UPDATE) does not call
  this arm. It adopts distributed truth and refusing there would break
  bilateral convergence — the same reasoning `mergeBacklogTerminalRecord`
  records. The measured incident came through the local door.

## The census over the live ledger (measured, read-only)

Run with a throwaway `package main` (NOT under `go test` — see the false
negative below), 25 most recent `completed` rows with a recorded branch:

```
CENSUS 25 rows: map[undelivered:2 unknown:23]
01a163e1-49c9-7ea0-b3fa-332e99171845   undelivered  branch=master
01a163df-a105-71b8-a297-680c5d35d093   undelivered  branch=master
```

**No false refusals**: all 23 `unknown` rows carry a reason naming what could
not be read (mostly `the tip this run produced cannot be named` — the ground was
already reaped, so no tip survives to judge).

**A second instance of the same defect was found by the census, not by the
epic's briefing**: `01a163e1-49c9-7ea0-b3fa-332e99171845` is `completed` with

```
$ git -C /home/user/workspace/01a163e1-… rev-parse HEAD                → dbd7e6c
$ git -C … rev-parse origin/master                                     → 61de49d
$ git -C … rev-list --left-right --count origin/master...HEAD          → 0	4
```

and the same trail sentence: `run closeout (child normal exit, wrapper
writeback never landed)`. Four commits stranded, same mechanism.

**Census false negative, recorded so it is not repeated**: the first attempt ran
the census as a Go TEST and reported all 25 rows as `records no deliverable
branch` — the test harness pins `GOPI_MEMORY_DB` to the shared READ-ONLY SAMPLE
database (Rule 32), so `taskRowDeliverable` resolved a row that does not exist
there and every answer came back "no claim". A census is run against the NAMED
live ledger by a non-test binary, or it measures nothing while reading as clean.

