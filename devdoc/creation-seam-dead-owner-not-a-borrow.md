<!-- doc-class: historical -->
# A DEAD Owner Is Not a Borrow — Reconciling the Creation Seam with the Delivery Recycle

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

**Epic**: `TestWorkerDeliveryRecyclesDespiteAStaleSiblingRow` master-red ·
**Date**: 2026-10-03 · **Branch**: `task/01a101eb4d0a70768ac941e212ee3ab5`
**Supersedes / extends**: `devdoc/creation-seam-one-owner-ground.md` (the seam
that introduced the refusal this document narrows).

## 1. The symptom, measured

From a clean master-derived worktree at tip `ec8f9b97` with a freshly built
`bin/gopi`, on master:

```
go test ./cmd/agent/ -run TestWorkerDeliveryRecyclesDespiteAStaleSiblingRow -count=1
[task-notify] task t-worker-stale: creation-time ground stamp REFUSED —
  …/001/wt is already named by row t-stale-sibling
    (a directory or branch another row already names is that row's;
     a borrowed ground is never recorded)
--- FAIL: TestWorkerDeliveryRecyclesDespiteAStaleSiblingRow (0.07s)
    cli_tasks_worker_recycle_e2e_test.go:540: the production backfill seam must
    stamp the row's ground: worktree="" branch="" (want "…/wt"/"feat/worker-stale-sibling")
```

The test died at its OWN fixture assertion, i.e. **before** the delivery-time
recycle it exists to guard ever ran: that recycle was, on master, unguarded.

## 2. The two landed rules that met

| rule | where | what it says |
|---|---|---|
| **ONE GROUND, ONE OWNING ROW** (creation seam, `taskCreationGroundBorrowed`, landed `4b5790c2`/`b16e7318`) | `task_deliverable_worktree.go` | a directory or branch **another row already names** is that row's; a run standing in it must not record it |
| **a dead owner does not block** (delivery recycle, `finishCleanupForeignOwnerReason`) | `task_finish_cleanup.go` | only a **LIVE** holder blocks; a row whose life is over and whose holder is provably gone is *history*, and the delivered row's recycle proceeds |

Both were right. They met on 2026-10-03 in exactly the shape the recycle was
built for: a delivered row taking the ground a terminal sibling still names.
The creation seam read "another row NAMES it" as "another row OWNS it", so the
delivered row lost the provenance of its own directory — and the delivery-time
recycle, denied of a row that records the directory, was left unexercised.

## 3. The decision — which rule wins

**`ONE GROUND, ONE LIVE OWNER`: a DEAD OWNER IS NOT A BORROW, so the creation
seam RECORDS the ground and only a LIVE owner refuses.**

Rationale, in the order that decides it:

1. The two rules ask the SAME question about a naming row — *is that row still
   an owner?* — and the family already has ONE answer for it:
   `finishCleanupOwnerRowLive` (the holder predicate the orphan epic
   consolidated). The creation seam was reading *names* where the rest of the
   family reads *holds*.
2. Refusing here does not protect anything the recycle protects: the dead row
   is not a holder (that is what `finishCleanupForeignOwnerReason` decided on
   2026-09-12 when it un-stranded the directory), while the refusal costs the
   LIVE row its own provenance — its `deliverable.worktree` stays empty while
   it works in a real directory, so every downstream consumer (the merger's
   branch attribution, the finish chain's gate ① "declared-vs-resolved") reads a
   row that owns nothing, and the recycle's ownership rule has nothing to
   consult.
3. Fail-closed is preserved on both sides: `HolderLive` is true for every
   **live status**, for an **unknowable foreign claim** (this host cannot prove
   it gone), and for an **unreadable ledger** (the load returns an error ⇒
   refusal). Only a **measured** dead local holder — the same evidence the
   recycle acts on — is history.

The chosen direction is therefore the *code* direction (the seam gains the
dead-owner predicate), not the fixture direction. A fixture rewritten around
the refusal would leave production unable to record a live run's ground on a
reused directory, which is the state the 2026-09-12 stranded-worktree fix
exists to end.

## 4. The implementation (one predicate per axis, Rules 13/14)

* `taskCreationGroundBorrowed` now asks **one** question of **one** reader:
  `taskGroundClaimRows` returns every row that NAMES a worktree or a branch
  **with its holder verdict** (`finishCleanupOwnerRowLive` — imported from the
  cleanup family, not re-implemented), and `taskGroundClaimRowNaming` picks the
  row that names the ground, **preferring a LIVE holder over a dead one** so the
  verdict never depends on scan order.
* A naming row that is `!HolderLive` ⇒ the ground **IS** recorded, and the
  observation is REPORTED once through the seam's own sink with
  `taskWorktreeDeadOwnerGroundLineFmt`, sharing the cleanup family's
  `finishCleanupOwnerStaleMarker` ("worktree holder not live") vocabulary.
* A naming row that is `HolderLive` ⇒ the refusal is **unchanged**, verbatim
  (`taskWorktreeBorrowedGroundLineFmt`).
* Branch axis scoping (per-repository, `sameWorkingPath`) and the worktree
  axis (`sameWorkingPath`) are unchanged.
* The **claim** seam (`taskWorktreeStampClaimer`, `taskWorktreeOwnerScan` /
  `taskBranchRowOwner`) is deliberately **untouched**: it stamps the row that
  is being claimed *right now*, so a dead naming row there means the row's own
  directory is a different question, and its two scans remain the package-level
  seams their tests rebind. The creation seam no longer calls them (one read
  instead of two scans); they keep their callers and their fail-closed
  contracts.

**A note on the `create` axis of the file** (`taskClaimGroundStampCandidate`,
switch `A`/`B` in `taskBackfillDeliverableWorktreeAt`): I confirmed by reading
the call site that path `B` (`claim_ref_resolved` ⇒ block) is reachable **only
when the row already names a worktree**, i.e. only when path `A`
(`row_already_bound` ⇒ take) did not fire — so the switch cannot hand-path the
dead-owner shape into the block. No change was needed there; the file records
this so the next reader does not re-derive it.

## 5. The fixture (test-side, honest)

`cmd/agent/cli_tasks_worker_recycle_e2e_test.go`:

* The stale sibling's claim stamp is now spelled **the way production spells
  it** (`host + ":" + pid`, the same form `claimHolderOfPid`/`deadHolderStamp`
  write). It previously stamped `os.Hostname()` under one `GOPI_TEST_HOME` and
  relied on an **invented** host name reaching the ledger, which the
  dead-owner verdict can never resolve (a foreign stamp is *unknowable*, never
  dead). That spelling was itself the reason the *old* seam appeared to work on
  it; with the corrected stamp, the fixture models the live 2026-09-12 shape
  (row `01a08cfd` completed, pinned behind cancelled sibling `01a08cd5`).
* No assertion was weakened or deleted: the read-back guard (the row must
  record `wt`/`feat`), the "the DEAD sibling must not block the recycle"
  assertion, the on-disk `os.Stat(wt)` check and the "branch survives
  (merger input)" check all stand.

## 6. Not done / scope

* `TestDocAnchorsResolveInNormativeDocs` (an `internal/agent` baseline-drift
  fact) is a **different subsystem** and is filed as its own row — see the
  hand-off task id in the session report.
* The seam's OTHER refusal arms (`taskWorktreeOwnerScan` returning an error on
  the claim path, the created-grounds plural axis) are unchanged; the plural
  axis now reaches the same reader, so it inherits the dead-owner relaxation.

## 7. Verification

* Build: `go build ./internal/tools/` + `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` — OK.
* Ran (verbatim): `go test ./cmd/agent/ -run TestWorkerDeliveryRecyclesDespiteAStaleSiblingRow -count=1`
  * on master's shape (this work's base, `ec8f9b97`): **FAIL** at
    `cli_tasks_worker_recycle_e2e_test.go:540` (worktree=""/branch="") with the
    seam's `REFUSED` line — reproduced above.
  * with this change: **ok github.com/nick/gopi/cmd/agent 2.6s**, and the run's
    own stderr shows BOTH the report line (`worktree holder not live: … the
    binding is history, not an owner … the ground IS recorded`) and the
    backfill line for `t-worker-stale`.
* `make test-budget PKGS='./internal/tools ./cmd/agent'` — the touched packages.
* Red-proof direction 2 (not vacuous): the precondition is checked by the
  fixture's own read-back guard; reaching the REFUSAL arm within the package is
  covered by `TestCreationSeamRefusesABorrowedWorktree` /
  `TestCreationSeamRefusesABorrowedBranch` (live/unprovable owner ⇒ refuse) and
  `TestCreationSeamStillRecordsAnUnownedGround` (nobody names it ⇒ record).
