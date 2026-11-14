<!-- doc-class: historical -->
# One owner criterion, two faces — the owner AXIS enumeration (epic 01a13214)

> **Retired mechanism (merge verb removal, 2026-11-01).** The `$ gopi merge
> --status` line below is the RECORD of what this run typed, kept verbatim; the
> top-level verb was removed afterwards and answers **exit 1** today (`gopi task
> merge` runs the same engine). The two-surface disagreement it records is in the
> ledger reads and is unchanged.

## The defect, as measured

Two operator surfaces disagreed about the owner of ONE branch, reading the SAME
ledger (2026-10-12, hosta, `base-peermaster-01a12b54`):

```
$ gopi merge --status
  BURIED base-peermaster-01a12b54 — 01a12b54-e636-796f-a97e-5cfccd3ebb06 (completed) — unowned
$ gopi task archive --check
    base-peermaster-01a12b54 — unowned — (none) — landed
```

Both owner cells say "unowned", and the two faces still formed DIFFERENT verdicts
about the same branch. The owner column of `merge --status` is resolved by
`mergeStatusOwnerRow` → `TaskForBranch`; the archive face resolves through
`archiveBranchOwners`. Those two resolved the owner through different MEMBERSHIP
sets:

- `TaskForBranch`'s matcher reads the row's PRIMARY axis (`deliverable.branch`)
  **and** its SECONDARY grounds (`deliverable.grounds`, the branches a run
  CREATED — `taskBackfillDeliverableGrounds`).
- The archive face read `deliverable.branch` only, through its own SQL
  (`LIKE '%"branch":"%'`) and its own JSON parse (`deliverableBranchOf`).

The row in question names `task/01a12b54e636796fa97e5cfccd3ebb06` on its primary
axis, and `base-peermaster-01a12b54` **only** on a ground it created — so one
face saw an owner and the other saw none.

## What this is NOT

This is **not** the documented divergence in ORDERING preference. Both faces
deliberately rank one candidate set differently, and both stay exactly as they
are:

| face | its own ranking question | order |
|---|---|---|
| archive (`archiveBranchOwners` → `archiveOwnerRank`) | "which reading is SAFEST to act on?" | LIVE > DECIDED > ARCHIVABLE, task id tie-break |
| `merge --status` (`TaskForBranchResolvedRow`) | "who owns this branch right now?" | LIVE first, then the newest other row |

Two faces may rank one candidate set differently. They may not disagree about
what the candidates **ARE** — because then "is this branch buried?" (and a
re-dispatch decision built on that verdict) depends on which surface the operator
happened to ask.

## The criterion chosen, and its written basis

**The membership is the union of BOTH axes: a row owns a branch if its primary
`deliverable.branch` names it, OR if a ground the row created names it.**

Why the ground axis is part of the MEMBERSHIP rather than an extra:

1. A ground is recorded **precisely so the ledger can be asked about the branch a
   run created**. `task_deliverable_grounds.go` states the gap it closed: the run
   minted a second worktree, no record named it, and `gopi task for-branch`
   answered "no owner" for a LIVE branch whose directory was registered. Reading
   the axis in one lookup and not in the other reinstates exactly that answer, one
   surface over.
2. **The safety direction is one-way.** Every downstream consumer treats a
   candidate as a reason to HOLD: the archive gate refuses to bury a branch a
   non-terminal row tracks (`repoSweepArchiveOwnerGate`), the reclaim arm keeps the
   ref (`archiveReclaimRowLiveness`), the classification reports the row instead of
   `(none)`, and `ArchiveDecide` refuses. Unifying the axes can therefore only make
   a consumer MORE cautious — it can never loosen one. That is the direction Rule
   15's default-deny asks for, and it is why the unification needed no gate widening
   and no exception path.
3. **One fact, one spelling (Rules 13/14).** The SQL and the blob parse were two
   independent readers of a fact the package already had ONE parser for
   (`taskDeliverableParse`) and ONE scan for (`taskDeliverableScanRows`). Two
   readers of one fact are free to disagree — and here they did.

## The implementation

`internal/tools/task_owner_axes.go` is the ONE enumeration:

- `taskDeliverableOwnerAxes(branch, grounds)` — the axes a row carries: its
  primary branch, then the branch of each ground it created, de-duplicated. Derived
  from the values the scan already parsed (never a second parse).
- `taskOwnerAxisRecord{branch, taskID, status}` — one `(axis value, row)` pair, the
  unit both faces fold from.
- `taskOwnerAxesAll()` — every `(axis, row)` the ledger holds, driven by the SHARED
  `taskDeliverableScanRows` with an accept-all matcher. It obeys the read guard
  (`ledger_read_guard.go`): an ABSENT ledger names no owner (the empty answer, never
  a created file); a ledger that exists but cannot be READ is an error, because
  folding it into an empty list is the reading that lets a consumer delete a live
  deliverable's ground.
- `taskOwnerBranchMatchesAxis(axis, asked)` — the ONE spelling of "does this axis
  NAME the branch asked about" (trimmed equality), called by BOTH matchers:
  `TaskForBranch`'s scan matcher and the archive/reclaim side.

Consumers:

- `archiveBranchOwners()` (the archive gate, the classification, the backlog, the
  sweep gate) now folds `taskOwnerAxesAll()` through the UNCHANGED `archiveOwnerSafer`
  / `archiveOwnerRank`.
- `archiveReclaimAllOwners()` (the reclaim arm's fallback enumeration) is now the
  same enumeration re-projected into its own record shape — its private SQL and its
  private `LIKE` needle are gone. The needle was a narrowing hint even there, and a
  needle on a JSON blob additionally matches a branch name occurring in the row's
  repo or worktree string.
- `deliverableBranchOf` is DELETED (it had no other caller) — that second reader WAS
  the divergence.
- `TaskForBranchOwner` carries an unexported `axes []string` (outside the wire
  shape), populated by `taskDeliverableScanRows`, so the enumeration can be
  projected from the row objects the ONE scan already built.
- `archiveReclaimMatchesBranch` keeps its own `taskIDNormalize` tolerance (that arm's
  own question: "may this name refer to the same task?") and now takes its equality
  half from `taskOwnerBranchMatchesAxis`.

## Red proof

The mutation is `taskDeliverableOwnerAxes` returning the primary axis alone — the OLD
membership restored in ONE place, which is the point of a shared enumeration. On the
mutated tree the regression suite fails with an assertion failure, and the failure
message is the measured divergence verbatim:

```
--- FAIL: TestOwnerAxesBothFacesNameTheSameOwnerForTheMeasuredSample (0.01s)
    task_owner_axes_test.go:90: THE DIVERGENCE IS BACK: the archive face has no owner for the
    ground branch base-peermaster-01a12b54 — the two faces disagree about which rows ARE
    candidates (owners=map[task/01a12b54e636796fa97e5cfccd3ebb06:{… status:completed}])
--- FAIL: TestOwnerAxesEnumerateBothAxesOfARow (0.01s)
--- FAIL: TestArchiveOwnerRankSurvivesOnAGroundAxis (0.02s)
```

The restore is verified against an explicit revision, not against a historical
copy: `test "$(git hash-object internal/tools/task_owner_axes.go)" =
"$(git rev-parse HEAD:internal/tools/task_owner_axes.go)"` → `OK`, and
`grep -c "RED-PROOF MUTATION"` → `0`. (A `cmp` against the pre-mutation copy is
NOT the criterion and reads exit 1 here — that snapshot predates a later
legitimate edit to the same file; see `devdoc/redproof-restore-criterion.md`.)

## Ran on the real ledger (Rule 31)

`gopi merge --status` and `gopi task archive --check`, run against
`/home/user/work/GoPi` and the operator's own ledger, before and after the change
(full transcript: `evidence/rule31-real-ledger-before-after.txt`):

```
### OLD binary (bin/gopi, built from the pre-change tree)
$ ./bin/gopi merge --status | grep base-peermaster-01a12b54
  BURIED base-peermaster-01a12b54 — 01a12b54-e636-796f-a97e-5cfccd3ebb06 (completed) — unowned
$ ./bin/gopi task archive --check | grep base-peermaster-01a12b54
    base-peermaster-01a12b54 — unowned — (none) — landed          ← the divergence

### NEW binary (built from this branch)
$ <branch>/gopi merge --status | grep base-peermaster-01a12b54
  BURIED base-peermaster-01a12b54 — 01a12b54-e636-796f-a97e-5cfccd3ebb06 (completed) — unowned
$ <branch>/gopi task archive --check | grep base-peermaster-01a12b54
    base-peermaster-01a12b54 — unowned — 01a12b54-e636-796f-a97e-5cfccd3ebb06 (completed) — landed
```

Both faces now name the same row (`01a12b54-e636-796f-a97e-5cfccd3ebb06`,
`completed`, re-readable with `gopi task show`). The `unowned` word in the REASON
column is the archive decision's recorded effective reason (`ArchiveReasonUnowned`,
written into `refs/gopi/archive-reason/base-peermaster-01a12b54` on 2026-10-12 by
the hand-archive path) — it is history, not a second owner reading, and it is
deliberately not rewritten.

## Not done (honest)

- **`gopi archive --check` remains a full enumeration**: every read of this kind is,
  and the two faces now do it the same way. The cost is unchanged in kind (the old
  face also opened the ledger and read every deliverable row) and slightly larger in
  rows considered (the scan no longer skips `task/*`). Measuring and, if needed,
  narrowing the walk's candidate set is a separate task — it must keep BOTH axes.
- **The reclaim arm's "held by a worktree" guard is still repo-keyed** (one
  `git worktree list` of the repo being swept), so a `task/<id>` branch held by a
  worktree in a DIFFERENT repo is not vetoed by it. That is not a membership
  divergence (both faces ask the same question) and it predates this change; the
  ledger-side membership unification closed the class this epic was filed for.
- **Rule 28 says the system layer belongs to peerbox.** This row was dispatched on
  hosta under the local policy (bug filed and executed on the client), so the fix
  lives on a hosta branch; whoever merges should follow the ordinary merge path.
